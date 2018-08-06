package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/golang/glog"
	conf "github.com/nearmap/kcd/config"
	"github.com/nearmap/kcd/events"
	clientset "github.com/nearmap/kcd/gok8s/client/clientset/versioned"
	"github.com/nearmap/kcd/gok8s/workload"
	"github.com/nearmap/kcd/history"
	"github.com/nearmap/kcd/registry"
	dh "github.com/nearmap/kcd/registry/dockerhub"
	"github.com/nearmap/kcd/registry/ecr"
	"github.com/nearmap/kcd/resource"
	svc "github.com/nearmap/kcd/service"
	"github.com/nearmap/kcd/signals"
	"github.com/nearmap/kcd/state"
	"github.com/nearmap/kcd/stats"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type regRoot struct {
	*cobra.Command

	stats stats.Stats

	stopChan chan os.Signal

	params *crParams
}

type crParams struct {
	tag            string
	registry       string
	providerUnused string

	stats statsParams
}

func newCRCommands() *cobra.Command {
	regRoot := newCRRootCommand()
	regRoot.AddCommand(newKCDSyncCommand(regRoot))
	regRoot.AddCommand(newCRTagCommand(regRoot))
	return regRoot.Command
}

func newCRRootCommand() *regRoot {
	var params crParams

	root := &regRoot{
		params: &params,
		Command: &cobra.Command{
			Use:   "registry",
			Short: "Command to perform container registry operations",
			Long:  "Command to perform container registry operations such as registry sync, tag images etc",
		},
	}
	root.PersistentFlags().StringVar(&params.tag, "tag", "", "Tag name to monitor on")
	root.PersistentFlags().StringVar(&params.registry, "repo", "", "Container repository ARN of Docker or registry  ex. nearmap/kcd")
	root.PersistentFlags().StringVar(&params.providerUnused, "provider", "ecr", "unused")

	(&params.stats).addFlags(root.Command)

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) (err error) {
		// prevent glog complaining about flags not being parsed
		flag.CommandLine.Parse([]string{})

		root.stats, err = root.params.stats.stats("kcd")
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
	kcdName   string
	version   string
}

func newKCDSyncCommand(root *regRoot) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Polls container registry to check for deployoments",
		Long:  "Continuously polls container registry to check if the a service deployment needs updates and if so, performs the update via k8s APIs",
	}

	var params crSyncParams
	cmd.Flags().StringVar(&params.k8sConfig, "k8s-config", "", "Path to the kube config file. Only required for running outside k8s cluster. In cluster, pods credentials are used")
	cmd.Flags().StringVar(&params.namespace, "namespace", "", "namespace of container version resource that the syncer is based on.")
	cmd.Flags().StringVar(&params.kcdName, "kcd", "", "name of container version resource that the syncer is based on")
	cmd.Flags().StringVar(&params.version, "version", "", "Indicates version of kcd resources to use in CR Syncer")

	cmd.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if params.kcdName == "" || params.namespace == "" {
			return errors.New("kcd and namespace to watch on must be provided")
		}

		return nil
	}

	cmd.PostRun = func(cmd *cobra.Command, args []string) {
		state.CleanupHealthStatus()
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		glog.V(1).Info("Starting registry Sync")

		stats, err := root.params.stats.stats("kcd", params.namespace)
		if err != nil {
			return errors.Wrap(err, "failed to initialize stats")
		}

		scStatus := 0
		defer stats.ServiceCheck("kcdsync.exec", "", scStatus, time.Now())

		var cfg *rest.Config
		if params.k8sConfig != "" {
			cfg, err = clientcmd.BuildConfigFromFlags("", params.k8sConfig)
		} else {
			cfg, err = rest.InClusterConfig()
		}
		if err != nil {
			scStatus = 2
			glog.Errorf("Failed to get k8s config: %v", err)
			return errors.Wrap(err, "error building k8s config: either run in cluster or provide config file")
		}

		k8sClient, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			scStatus = 2
			glog.Errorf("Error building k8s clientset: %v", err)
			return errors.Wrap(err, "Error building k8s clientset")
		}

		customCS, err := clientset.NewForConfig(cfg)
		if err != nil {
			scStatus = 2
			glog.Errorf("Error building k8s container version clientset: %v", err)
			return errors.Wrap(err, "Error building k8s container version clientset")
		}

		recorder := events.PodEventRecorder(k8sClient, params.namespace)

		workloadProvider := workload.NewProvider(k8sClient, customCS, params.namespace,
			conf.WithRecorder(recorder), conf.WithStats(stats))

		resourceProvider := resource.NewK8sProvider(params.namespace, customCS, workloadProvider)

		kcd, err := customCS.CustomV1().KCDs(params.namespace).Get(params.kcdName, metav1.GetOptions{})
		if err != nil {
			scStatus = 2
			glog.Errorf("Failed to find CV resource in namespace=%s, name=%s, error=%v", params.namespace, params.kcdName, err)
			return errors.Wrap(err, "Failed to find CV resource")
		}

		// CRD does not allow us to specify default type on OpenAPISpec
		// TODO: this needs a better strategy but hacking it for now
		//
		if kcd.Spec.VersionSyntax == "" {
			kcd.Spec.VersionSyntax = "[0-9a-f]{5,40}"
		}
		var registryProvider registry.Provider
		switch registry.ProviderByRepo(kcd.Spec.ImageRepo) {
		case "ecr":
			registryProvider, err = ecr.NewECR(kcd.Spec.ImageRepo, kcd.Spec.VersionSyntax, stats)
		case "dockerhub":
			registryProvider, err = dh.NewDHV2(kcd.Spec.ImageRepo, kcd.Spec.VersionSyntax, dh.WithStats(stats))
		}
		if err != nil {
			glog.Errorf("Failed to create registry provider in namespace=%s for kcd name=%s, error=%v",
				params.namespace, params.kcdName, err)
			return errors.Wrap(err, "Failed to create registry provider")
		}

		historyProvider := history.NewProvider(k8sClient, stats)

		crSyncer, err := resource.NewSyncer(resourceProvider, workloadProvider, registryProvider, historyProvider, kcd,
			conf.WithRecorder(recorder), conf.WithStats(stats))
		if err != nil {
			glog.Errorf("Failed to create syncer in namespace=%s for kcd name=%s, error=%v",
				params.namespace, params.kcdName, err)
			return errors.Wrap(err, "Failed to create syncer")
		}

		glog.V(1).Infof("Starting registry syncer with namespace=%s for kcd name=%s, error=%v",
			params.namespace, params.kcdName, err)

		stats.ServiceCheck("kcdsync.exec", "", scStatus, time.Now())

		go func() {
			crSyncer.Start()
		}()

		<-root.stopChan
		if err = crSyncer.Stop(); err != nil {
			glog.Errorf("error received while stopping state machine: %v", err)
		}
		glog.V(1).Info("kcdsync Server gracefully stopped")

		return nil
	}

	status := &cobra.Command{
		Use:   "status",
		Short: "Checks whether sync was run recently",
		Long:  "Checks whether sync was run recently",
	}

	var by time.Duration
	status.Flags().DurationVar(&by, "by", time.Duration(int64(time.Minute*5)), "Duration to check sync for ")
	status.RunE = func(cmd *cobra.Command, args []string) error {
		glog.V(4).Info("Performing health status check")

		if err := state.CheckHealth(by); err != nil {
			glog.Errorf("health status returned error: %v", err)
			return err
		}
		glog.V(4).Info("health status check was successful")
		return nil
	}

	cmd.AddCommand(status)

	return cmd
}

type regTagParams struct {
	tags    []string
	version string

	username string
	pwd      string
	verPat   string
}

// newCRTagCommand is CLI interface to managing tags on registry images
func newCRTagCommand(root *regRoot) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tags",
		Short: "Manages tags of registry repository",
		Long:  "Manages adds/removes tags on registry repositories",
	}

	var crProvider registry.Tagger
	var params regTagParams
	cmd.PersistentFlags().StringSliceVar(&params.tags, "tags", nil, "list of tags that needs to be added or removed")
	cmd.PersistentFlags().StringVar(&params.verPat, "version-pattern", "[0-9a-f]{5,40}", "Regex pattern for container version")
	cmd.PersistentFlags().StringVar(&params.version, "version", "", "sha/version tag of registry image that is being tagged")
	cmd.PersistentFlags().StringVar(&params.username, "username", "", "username of dockerhub registry")
	cmd.PersistentFlags().StringVar(&params.pwd, "passsword", "", "password of user of dockerhub registry")
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) (err error) {
		root.stats, err = root.params.stats.stats("kcdtagger")
		if err != nil {
			return errors.Wrap(err, "failed to initialize stats")
		}

		switch registry.ProviderByRepo(root.params.registry) {
		case "ecr":
			crProvider, err = ecr.NewECR(root.params.registry, params.verPat, root.stats)
		case "dockerhub":
			crProvider, err = dh.NewDHV2(root.params.registry, params.verPat, dh.WithStats(root.stats))
		}
		if err != nil {
			return err
		}

		return nil
	}

	addTagCmd := &cobra.Command{
		Use:   "add",
		Short: "Add tag to image in given registry repository",
		Long:  "Add tag to image in given registry repository",
	}
	addTagCmd.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if root.params.registry == "" || params.tags == nil || len(params.tags) == 0 || params.version == "" {
			return errors.New("registry repository name/URI and registry image version is required")
		}

		return nil
	}
	addTagCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return crProvider.Add(params.version, params.tags...)
	}

	rmTagCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove tag to image in given registry repository",
		Long:  "Remove tag to image in given registry repository",
	}
	rmTagCmd.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if root.params.registry == "" || params.tags == nil || len(params.tags) == 0 {
			return errors.New("registry repository name/URI and tags are required")
		}

		return nil
	}
	rmTagCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return crProvider.Remove(params.tags...)
	}

	getTagCmd := &cobra.Command{
		Use:   "get",
		Short: "get tags of image by its version tagin given registry repository",
		Long:  "Remove tag to image in given registry repository",
	}
	getTagCmd.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if root.params.registry == "" || params.version == "" {
			return errors.New("registry repository name/URI and version is required")
		}

		return nil
	}
	getTagCmd.RunE = func(cmd *cobra.Command, args []string) error {
		ts, err := crProvider.Get(params.version)
		if err != nil {
			return err
		}
		fmt.Printf("Found tags %s on requested registry repository of image %s \n", ts, params.version)
		return nil
	}

	cmd.AddCommand(addTagCmd)
	cmd.AddCommand(rmTagCmd)
	cmd.AddCommand(getTagCmd)

	return cmd
}

// newCVListCommand is CLI interface to list the current status of KCD resource definitions
func newCVCommand() *cobra.Command {
	var k8sConfig string
	cmd := &cobra.Command{
		Use:   "rd",
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
			glog.Errorf("Failed to get k8s config: %v", err)
			return errors.Wrap(err, "Error building k8s configs either run in cluster or provide config file via k8s-config arg")
		}

		k8sClient, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			glog.Errorf("Error building k8s clientset: %v", err)
			return errors.Wrap(err, "Error building k8s clientset")
		}

		customClient, err := clientset.NewForConfig(cfg)
		if err != nil {
			glog.Errorf("Error building k8s container version clientset: %v", err)
			return errors.Wrap(err, "Error building k8s container version clientset")
		}

		workloadProvider := workload.NewProvider(k8sClient, customClient, "")
		resourceProvider := resource.NewK8sProvider("", customClient, workloadProvider)

		return svc.AllKCDs(os.Stdout, "json", "", resourceProvider, false)
	}

	cmd.AddCommand(listCmd)

	return cmd
}
