package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/nearmap/cvmanager/cv"
	clientset "github.com/nearmap/cvmanager/gok8s/client/clientset/versioned"
	"github.com/nearmap/cvmanager/registry"
	"github.com/nearmap/cvmanager/registry/config"
	"github.com/nearmap/cvmanager/signals"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type crRoot struct {
	*cobra.Command

	stats stats.Stats
	sess  *session.Session

	crProvider registry.CRProvider
	stopChan   chan os.Signal

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
	root.PersistentFlags().StringVar(&params.provider, "provider", "cr", "Identifier for docker registry provider. Supported values are cr/dockerhub")
	(&params.stats).addFlags(root.Command)

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if params.cr == "" {
			return errors.New("cr repository name/URI must be provided")
		}

		root.stats, err = root.params.stats.stats("cr")
		if err != nil {
			return errors.Wrap(err, "failed to initialize stats")
		}

		root.sess, err = session.NewSession()
		if err != nil {
			return errors.Wrap(err, "failed to obtain AWS session")
		}

		crTyp, err := registry.NewCRType(root.params.provider)
		if err != nil {
			log.Printf("Error identifying container registry: %v", err)
			return errors.Wrap(err, "Invalid Registry type provided")
		}

		root.crProvider = registry.NewCRProvider(root.sess, root.stats, registry.Registry(crTyp))

		root.stopChan = signals.SetupTwoWaySignalHandler()

		return nil
	}

	return root
}

type crSyncParams struct {
	k8sConfig string
	namespace string

	syncFreq int

	deployment string
	container  string

	configKey string
}

func newCRSyncCommand(root *crRoot) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Polls container registry to check for deployoments",
		Long:  "Continuously polls container registry to check if the a service deployment needs updates and if so, performs the update via k8s APIs",
	}

	var params crSyncParams
	cmd.Flags().IntVar(&params.syncFreq, "sync", 5, "Sync frequency in minutes")
	cmd.Flags().StringVar(&params.k8sConfig, "k8s-config", "", "Path to the kube config file. Only required for running outside k8s cluster. In cluster, pods credentials are used")
	cmd.Flags().StringVar(&params.namespace, "namespace", "", "namespace")
	cmd.Flags().StringVar(&params.deployment, "deployment", "", "name of the deployment to monitor")
	cmd.Flags().StringVar(&params.container, "container", "", "name of the container in specified deployment to monitor")
	cmd.Flags().StringVar(&params.configKey, "configKey", "", "full path key of configmap containing version in format <configmapname>/<key> eg photos/version")

	cmd.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if root.params.tag == "" || params.deployment == "" || params.namespace == "" || params.container == "" {
			return errors.New("Deployment, cr repository name, tag, namespace, container to watch on must be provided.")
		}
		return nil
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		log.Print("Starting cr Sync")

		stats, err := root.params.stats.stats(fmt.Sprintf("%s/%s", params.namespace, params.deployment))
		if err != nil {
			return errors.Wrap(err, "failed to initialize stats")
		}
		// Syncer sends stats with different prefixes thats why it gets overwritten here
		root.crProvider.Stats(stats)

		scStatus := 0
		defer stats.ServiceCheck("crsync.exec", "", scStatus, time.Now())

		// sess, err := session.NewSession()
		// if err != nil {
		// 	return errors.Wrap(err, "failed to obtain AWS session")
		// }
		// stopChan := signals.SetupTwoWaySignalHandler()

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

		crSyncer, err := root.crProvider.Syncer(k8sClient, params.namespace, &config.SyncConfig{
			Freq:       params.syncFreq,
			Tag:        root.params.tag,
			RepoARN:    root.params.cr,
			Deployment: params.deployment,
			Container:  params.container,
			ConfigKey:  params.configKey,
		})
		if err != nil {
			log.Printf("Failed to create syncer with syncfrequnecy=%v, crName=%v, tag=%v, deployment=%v, error=%v",
				params.syncFreq, root.params.cr, root.params.tag, params.deployment, err)
			return errors.Wrap(err, "Failed to create syncer")
		}

		log.Printf("Starting cr syncer with syncfrequnecy=%v, crName=%v, tag=%v, deployment=%v",
			params.syncFreq, root.params.cr, root.params.tag, params.deployment)

		stats.ServiceCheck("crsync.exec", "", scStatus, time.Now())
		go func() {
			if err := crSyncer.Sync(); err != nil {
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
		if params.tags == nil || len(params.tags) == 0 || params.version == "" {
			return errors.New("cr image version is required.")
		}
		return nil
	}
	addTagCmd.RunE = func(cmd *cobra.Command, args []string) error {
		tagger, err := root.crProvider.Tagger()
		if err != nil {
			return errors.Wrap(err, "failed to initialize docker registry tagger")
		}
		return tagger.Add(root.params.cr, params.version, params.tags...)
	}

	rmTagCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove tag to image in given cr repository",
		Long:  "Remove tag to image in given cr repository",
	}
	rmTagCmd.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if params.tags == nil || len(params.tags) == 0 {
			return errors.New("tags are required.")
		}
		return nil
	}
	rmTagCmd.RunE = func(cmd *cobra.Command, args []string) error {
		tagger, err := root.crProvider.Tagger()
		if err != nil {
			return errors.Wrap(err, "failed to initialize docker registry tagger")
		}

		return tagger.Remove(root.params.cr, params.tags...)
	}

	getTagCmd := &cobra.Command{
		Use:   "get",
		Short: "get tags of image by its version tagin given cr repository",
		Long:  "Remove tag to image in given cr repository",
	}
	getTagCmd.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if params.version == "" {
			return errors.New("version is required.")
		}
		return nil
	}
	getTagCmd.RunE = func(cmd *cobra.Command, args []string) error {
		tagger, err := root.crProvider.Tagger()
		if err != nil {
			return errors.Wrap(err, "failed to initialize docker registry tagger")
		}

		t, err := tagger.Get(root.params.cr, params.version)
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

		return cv.ExecuteWorkloadsList(os.Stdout, "json", k8sClient, customClient)
	}

	cmd.AddCommand(listCmd)

	return cmd
}
