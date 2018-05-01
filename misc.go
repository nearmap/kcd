package main

import (
	"fmt"
	"log"
	"os"
	"time"

	conf "github.com/nearmap/cvmanager/config"
	"github.com/nearmap/cvmanager/cv"
	"github.com/nearmap/cvmanager/events"
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

		root.stopChan = signals.SetupTwoWaySignalHandler()

		return nil
	}

	return root
}

type crSyncParams struct {
	k8sConfig string

	namespace string
	cvName    string
	version   string

	history, rollback bool
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
	cmd.Flags().StringVar(&params.version, "version", "", "Indicates version of cv resources to use in CR Syncer")
	cmd.Flags().BoolVar(&params.history, "history", false, "If true, stores the release history in configmap <cv_resource_name>_history")
	cmd.Flags().BoolVar(&params.rollback, "rollback", false, "If true, on failed deployment, the version update is automatically rolled back")

	cmd.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if params.cvName == "" || params.namespace == "" {
			return errors.New("CV  and namespace to watch on must be provided.")
		}

		return nil
	}

	cmd.PostRun = func(cmd *cobra.Command, args []string) {
		registry.CleanupSyncStart()
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

		cv, err := customCS.CustomV1().ContainerVersions(params.namespace).Get(params.cvName,
			metav1.GetOptions{
				ResourceVersion: params.version,
			})
		if err != nil {
			scStatus = 2
			log.Printf("Failed to find CV resource in namespace=%s, name=%s, error=%v", params.namespace, params.cvName, err)
			return errors.Wrap(err, "Failed to find CV resource")
		}

		if root.params.provider == registry.ProviderByRepo(cv.Spec.ImageRepo) {
			return errors.Errorf("Container registry provider invalid. Check provider and image repository arg.")
		}

		// CRD does not allow us to specify default type on OpenAPISpec
		// TODO: this needs a better strategy but hacking it for now
		//
		if cv.Spec.VersionSyntax == "" {
			cv.Spec.VersionSyntax = "[0-9a-f]{5,40}"
		}
		var crProvider registry.Registry
		switch root.params.provider {
		case "ecr":
			crProvider, err = ecr.NewECR(cv.Spec.ImageRepo, cv.Spec.VersionSyntax, stats)
		case "dockerhub":
			crProvider, err = dh.NewDH(cv.Spec.ImageRepo, cv.Spec.VersionSyntax, dh.WithStats(stats))
		}
		if err != nil {
			log.Printf("Failed to create syncer in namespace=%s for cv name=%s, error=%v",
				params.namespace, params.cvName, err)
			return errors.Wrap(err, "Failed to create syncer")
		}

		crSyncer := registry.NewSyncer(k8sClient, cv, params.namespace, crProvider,
			conf.WithStats(stats), conf.WithUseRollback(params.rollback), conf.WithHistory(params.history))

		log.Printf("Starting cr syncer with snamespace=%s for cv name=%s, error=%v",
			params.namespace, params.cvName, err)

		stats.ServiceCheck("crsync.exec", "", scStatus, time.Now())
		go func() {
			err := registry.SetSyncStatus()
			if err == nil {
				err = crSyncer.Sync()
			}
			if err != nil {
				scStatus = 2
				log.Printf("Server error during cr sync: %v", err)
				root.stopChan <- os.Interrupt
			}
		}()

		<-root.stopChan
		log.Printf("crsync Server gracefully stopped")

		return nil
	}

	state := &cobra.Command{
		Use:   "status",
		Short: "Checks whether sync was run recently",
		Long:  "Checks whether sync was run recently",
	}

	var by time.Duration
	state.Flags().DurationVar(&by, "by", time.Duration(int64(time.Minute*5)), "Duration to check sync for ")
	state.RunE = func(cmd *cobra.Command, args []string) error {
		return registry.SyncCheck(by)
	}

	cmd.AddCommand(state)

	return cmd
}

type crTagParams struct {
	tags    []string
	version string

	username string
	pwd      string
	verPat   string
}

// newCRTagCommand is CLI interface to managing tags on cr images
func newCRTagCommand(root *crRoot) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tags",
		Short: "Manages tags of cr repository",
		Long:  "Manages adds/removes tags on cr repositories",
	}

	var crProvider registry.Tagger
	var params crTagParams
	cmd.PersistentFlags().StringSliceVar(&params.tags, "tags", nil, "list of tags that needs to be added or removed")
	cmd.PersistentFlags().StringVar(&params.verPat, "version-pattern", "[0-9a-f]{5,40}", "Regex pattern for container version")
	cmd.PersistentFlags().StringVar(&params.version, "version", "", "sha/version tag of cr image that is being tagged")
	cmd.PersistentFlags().StringVar(&params.username, "username", "", "username of dockerhub registry")
	cmd.PersistentFlags().StringVar(&params.pwd, "passsword", "", "password of user of dockerhub registry")
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) (err error) {

		root.stats, err = root.params.stats.stats("crtagger")
		if err != nil {
			return errors.Wrap(err, "failed to initialize stats")
		}

		if root.params.provider == registry.ProviderByRepo(root.params.cr) {
			return errors.Errorf("Container registry provider invalid. Check provider and image repository arg.")
		}
		switch root.params.provider {
		case "ecr":
			crProvider, err = ecr.NewECR(root.params.cr, params.verPat, root.stats)
		case "dockerhub":
			crProvider, err = dh.NewDH(root.params.cr, params.verPat, dh.WithStats(root.stats))
		}
		if err != nil {
			return err
		}

		return nil
	}

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
		return crProvider.Add(params.version, params.tags...)
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
		return crProvider.Remove(params.tags...)
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
		ts, err := crProvider.Get(params.version)
		if err != nil {
			return err
		}
		fmt.Printf("Found tags %s on requested cr repository of image %s \n", ts, params.version)
		return nil
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

		k8sProvider := k8s.NewK8sProvider(k8sClient, "", &events.FakeRecorder{}, conf.WithStats(stats.NewFake()))

		return cv.GetAllContainerVersion(os.Stdout, "json", k8sProvider, customClient)
	}

	cmd.AddCommand(listCmd)

	return cmd
}
