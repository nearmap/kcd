package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/nearmap/cvmanager/cv"
	clientset "github.com/nearmap/cvmanager/gok8s/client/clientset/versioned"
	k8s "github.com/nearmap/cvmanager/gok8s/workload"
	"github.com/nearmap/cvmanager/registry"
	dh "github.com/nearmap/cvmanager/registry/dockerhub"
	"github.com/nearmap/cvmanager/registry/ecr"
	"github.com/nearmap/cvmanager/signals"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type crRoot struct {
	*cobra.Command

	stats stats.Stats
	sess  *session.Session

	stopChan chan os.Signal

	params *crParams
}

type crParams struct {
	tag      string
	cr       string
	provider string

	stats statsParams
}

func newCRCommands() *cobra.Command {
	crRoot := newCRRootCommand()
	crRoot.AddCommand(newCRSyncCommand(crRoot))
	crRoot.AddCommand(newCRTagCommand(crRoot))
	return crRoot.Command
}

func newCRRootCommand() *crRoot {
	var params crParams

	root := &crRoot{
		params: &params,
		Command: &cobra.Command{
			Use:   "cr",
			Short: "Command to perform container registry operations",
			Long:  "Command to perform container registry operations such as registry sync, tag images etc",
		},
	}
	root.PersistentFlags().StringVar(&params.tag, "tag", "", "Tag name to monitor on")
	root.PersistentFlags().StringVar(&params.cr, "repo", "", "Container repository ARN of Docker or cr  ex. nearmap/cvmanager")
	root.PersistentFlags().StringVar(&params.provider, "provider", "ecr", "Identifier for docker registry provider. Supported values are ecr/dockerhub")
	(&params.stats).addFlags(root.Command)

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) (err error) {

		root.stats, err = root.params.stats.stats("crsync")
		if err != nil {
			return errors.Wrap(err, "failed to initialize stats")
		}

		switch root.params.provider {
		case "ecr":
			root.sess, err = session.NewSession()
			if err != nil {
				return errors.Wrap(err, "failed to obtain AWS session")
			}
			log.Printf("AWS ECR docker registry in use")
		case "dockerhub":
			log.Printf("Dockerhub/Docker.io docker registry in use")
		default:
			return errors.Errorf("Requested docker registry %s is not supported", root.params.provider)
		}

		root.stopChan = signals.SetupTwoWaySignalHandler()

		return nil
	}

	return root
}

type crSyncParams struct {
	k8sConfig string

	namespace string
	cvName    string

	history bool
}

func newCRSyncCommand(root *crRoot) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Polls container registry to check for deployoments",
		Long:  "Continuously polls container registry to check if the a service deployment needs updates and if so, performs the update via k8s APIs",
	}

	var params crSyncParams
	cmd.Flags().StringVar(&params.k8sConfig, "k8s-config", "", "Path to the kube config file. Only required for running outside k8s cluster. In cluster, pods credentials are used")
	cmd.Flags().StringVar(&params.namespace, "namespace", "", "namespace of container version resource that the syncer is based on.")
	cmd.Flags().StringVar(&params.cvName, "cv", "", "name of container version resource that the syncer is based on")
	cmd.Flags().BoolVar(&params.history, "history", false, "If true, stores the release history in configmap <cv_resource_name>_history")

	cmd.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if params.cvName == "" || params.namespace == "" {
			return errors.New("CV  and namespace to watch on must be provided.")
		}

		return nil
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		log.Print("Starting cr Sync")

		stats, err := root.params.stats.stats(fmt.Sprintf("crsync.%s", params.cvName), params.namespace)
		if err != nil {
			return errors.Wrap(err, "failed to initialize stats")
		}

		scStatus := 0
		defer stats.ServiceCheck("crsync.exec", "", scStatus, time.Now())

		var cfg *rest.Config
		if params.k8sConfig != "" {
			cfg, err = clientcmd.BuildConfigFromFlags("", params.k8sConfig)
		} else {
			cfg, err = rest.InClusterConfig()
		}
		if err != nil {
			scStatus = 2
			log.Printf("Failed to get k8s config: %v", err)
			return errors.Wrap(err, "Error building k8s configs either run in cluster or provide config file")
		}

		k8sClient, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			scStatus = 2
			log.Printf("Error building k8s clientset: %v", err)
			return errors.Wrap(err, "Error building k8s clientset")
		}

		customCS, err := clientset.NewForConfig(cfg)
		if err != nil {
			scStatus = 2
			log.Printf("Error building k8s container version clientset: %v", err)
			return errors.Wrap(err, "Error building k8s container version clientset")
		}

		cv, err := customCS.CustomV1().ContainerVersions(params.namespace).Get(params.cvName, metav1.GetOptions{})
		if err != nil {
			scStatus = 2
			log.Printf("Failed to find CV resource in namespace=%s, name=%s, error=%v", params.namespace, params.cvName, err)
			return errors.Wrap(err, "Failed to find CV resource")
		}

		var crSyncer registry.Syncer
		switch root.params.provider {
		case "ecr":
			crSyncer, err = ecr.NewSyncer(root.sess, k8sClient, params.namespace, cv, stats, params.history)
		case "dockerhub":
			crSyncer, err = dh.NewSyncer(k8sClient, params.namespace, cv, stats, params.history)
		}
		if err != nil {
			log.Printf("Failed to create syncer in namespace=%s for cv name=%s, error=%v",
				params.namespace, params.cvName, err)
			return errors.Wrap(err, "Failed to create syncer")
		}

		log.Printf("Starting cr syncer with snamespace=%s for cv name=%s, error=%v",
			params.namespace, params.cvName, err)

		stats.ServiceCheck("crsync.exec", "", scStatus, time.Now())
		go func() {
			if err := crSyncer.Sync(); err != nil {
				scStatus = 2
				log.Printf("Server error during cr sync: %v", err)
				root.stopChan <- os.Interrupt
			}
		}()

		<-root.stopChan
		log.Printf("crsync Server gracefully stopped")

		return nil
	}

	return cmd
}

type crTagParams struct {
	tags    []string
	version string

	stats statsParams
}

// newCRTagCommand is CLI interface to managing tags on cr images
func newCRTagCommand(root *crRoot) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tags",
		Short: "Manages tags of cr repository",
		Long:  "Manages adds/removes tags on cr repositories",
	}

	var params crTagParams
	cmd.PersistentFlags().StringSliceVar(&params.tags, "tags", nil, "list of tags that needs to be added or removed")
	cmd.PersistentFlags().StringVar(&params.version, "version", "", "sha/version tag of cr image that is being tagged")

	addTagCmd := &cobra.Command{
		Use:   "add",
		Short: "Add tag to image in given cr repository",
		Long:  "Add tag to image in given cr repository",
	}
	addTagCmd.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if root.params.cr == "" || params.tags == nil || len(params.tags) == 0 || params.version == "" {
			return errors.New("cr repository name/URI and cr image version is required.")
		}

		return nil
	}
	addTagCmd.RunE = func(cmd *cobra.Command, args []string) error {
		switch root.params.provider {
		case "ecr":
			return ecr.NewTagger(root.sess, root.stats).Add(root.params.cr, params.version, params.tags...)
		case "dockerhub":
			return dh.NewTagger().Add(root.params.cr, params.version, params.tags...)
		}
		return nil
	}

	rmTagCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove tag to image in given cr repository",
		Long:  "Remove tag to image in given cr repository",
	}
	rmTagCmd.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if root.params.cr == "" || params.tags == nil || len(params.tags) == 0 {
			return errors.New("cr repository name/URI and tags are required.")
		}

		return nil
	}
	rmTagCmd.RunE = func(cmd *cobra.Command, args []string) error {

		switch root.params.provider {
		case "ecr":
			return ecr.NewTagger(root.sess, root.stats).Remove(root.params.cr, params.tags...)
		case "dockerhub":
			return dh.NewTagger().Remove(root.params.cr, params.tags...)
		}

		return nil
	}

	getTagCmd := &cobra.Command{
		Use:   "get",
		Short: "get tags of image by its version tagin given cr repository",
		Long:  "Remove tag to image in given cr repository",
	}
	getTagCmd.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if root.params.cr == "" || params.version == "" {
			return errors.New("cr repository name/URI and version is required.")
		}

		return nil
	}
	getTagCmd.RunE = func(cmd *cobra.Command, args []string) error {
		var err error
		var t []string
		switch root.params.provider {
		case "ecr":
			t, err = ecr.NewTagger(root.sess, root.stats).Get(root.params.cr, params.version)
		case "dockerhub":
			t, err = dh.NewTagger().Get(root.params.cr, params.version)
		}
		fmt.Printf("Found tags %s on requested cr repository of image %s \n", t, params.version)
		return err
	}

	cmd.AddCommand(addTagCmd)
	cmd.AddCommand(rmTagCmd)
	cmd.AddCommand(getTagCmd)

	return cmd
}

// newCVListCommand is CLI interface to list the current status of CV resources
func newCVCommand() *cobra.Command {
	var k8sConfig string
	cmd := &cobra.Command{
		Use:   "cv",
		Short: "Manages current status (version and status) of deployments managed by CV resources",
		Long:  "Manages current status (version and status) of deployments managed by CV resources",
	}

	cmd.PersistentFlags().StringVar(&k8sConfig, "k8s-config", "", "Path to the kube config file. Only required for running outside k8s cluster. In cluster, pods credentials are used")

	listCmd := &cobra.Command{
		Use:   "get",
		Short: "Get current status (version and status) of deployments managed by CV resources",
		Long:  "Get current status (version and status) of deployments managed by CV resources",
	}

	listCmd.RunE = func(cmd *cobra.Command, args []string) error {
		var cfg *rest.Config
		var err error
		if k8sConfig != "" {
			cfg, err = clientcmd.BuildConfigFromFlags("", k8sConfig)
		} else {
			cfg, err = rest.InClusterConfig()
		}
		if err != nil {
			log.Printf("Failed to get k8s config: %v", err)
			return errors.Wrap(err, "Error building k8s configs either run in cluster or provide config file via k8s-config arg")
		}

		k8sClient, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			log.Printf("Error building k8s clientset: %v", err)
			return errors.Wrap(err, "Error building k8s clientset")
		}

		customClient, err := clientset.NewForConfig(cfg)
		if err != nil {
			log.Printf("Error building k8s container version clientset: %v", err)
			return errors.Wrap(err, "Error building k8s container version clientset")
		}

		k8sProvider := k8s.NewK8sProvider(k8sClient, "", stats.NewFake(), false)

		return cv.ExecuteWorkloadsList(os.Stdout, "json", k8sProvider, customClient)
	}

	cmd.AddCommand(listCmd)

	return cmd
}
