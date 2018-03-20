package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/nearmap/cvmanager/controller"
	"github.com/nearmap/cvmanager/ecr"
	clientset "github.com/nearmap/cvmanager/gok8s/client/clientset/versioned"
	informer "github.com/nearmap/cvmanager/gok8s/client/informers/externalversions"
	"github.com/nearmap/cvmanager/signals"
	"github.com/nearmap/cvmanager/stats"
	"github.com/nearmap/cvmanager/stats/datadog"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	k8sinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	os.Exit(withExitCode())
}

// withExitCode executes as the main method but returns the status code
// that will be returned to the operating system. This is done to ensure
// all deferred functions are completed before calling os.Exit().
func withExitCode() int {
	root := &cobra.Command{
		Short: "cvmanager",
		Long:  "Controller for k8s",
	}

	root.AddCommand(newRunCommand())
	root.AddCommand(newECRSyncCommand())
	root.AddCommand(newECRTagCommand())

	// TODO: recover
	err := root.Execute()
	if err != nil {
		log.Printf("Error executing command: %v", err)
		return 1
	}

	return 0
}

type statsParams struct {
	provider string
	host     string
}

func (sp *statsParams) addFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&sp.provider, "stats-provider", "datadog", "Name of the stats provider, e.g. datadog.")
	cmd.PersistentFlags().StringVar(&sp.host, "stats-host", os.Getenv("STATS_HOST"), "Host to send stats.")
}

func (sp *statsParams) stats(namespace string) (stats.Stats, error) {
	if sp.host == "" {
		return stats.NewFake(), nil
	}

	switch sp.provider {
	case "datadog":
		return datadog.New(sp.host, namespace)
	default:
		return stats.NewFake(), nil
	}
}

type runParams struct {
	k8sConfig    string
	configMapKey string
	stats        statsParams
}

func newRunCommand() *cobra.Command {
	var params runParams
	rc := &cobra.Command{
		Use:   "run",
		Short: "Runs the service",
		Long:  fmt.Sprintf(`Runs the service as a HTTP server`),
	}

	rc.Flags().StringVar(&params.k8sConfig, "k8s-config", "", "Path to the kube config file. Only required for running outside k8s cluster. In cluster, pods credentials are used")
	rc.Flags().StringVar(&params.configMapKey, "configmap-key", "kube-system/cvmanager", "Namespaced key of configmap that container version and region config defined")
	(&params.stats).addFlags(rc)

	rc.RunE = func(cmd *cobra.Command, args []string) (err error) {
		stats, err := params.stats.stats(params.configMapKey)
		if err != nil {
			return errors.Wrap(err, "failed to initialize stats")
		}

		scStatus := 0
		defer stats.ServiceCheck("cvmanager.exec", "", scStatus, time.Now())

		stopCh := signals.SetupSignalHandler()

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

		customClient, err := clientset.NewForConfig(cfg)
		if err != nil {
			scStatus = 2
			log.Printf("Error building k8s container version clientset: %v", err)
			return errors.Wrap(err, "Error building k8s container version clientset")
		}

		k8sInformerFactory := k8sinformers.NewSharedInformerFactory(k8sClient, time.Second*30)
		customInformerFactory := informer.NewSharedInformerFactory(customClient, time.Second*30)

		//Controllers here
		cvc, err := controller.NewCVController(params.configMapKey, k8sClient, customClient,
			k8sInformerFactory, customInformerFactory, stats)
		if err != nil {
			return errors.Wrap(err, "Failed to create controller")
		}

		k8sInformerFactory.Start(stopCh)
		customInformerFactory.Start(stopCh)
		log.Printf("Started informer factory")

		stats.ServiceCheck("cvmanager.exec", "", scStatus, time.Now())

		if err = cvc.Run(2, stopCh); err != nil {
			scStatus = 2
			log.Printf("Shutting down container version controller: %v", err)
			return errors.Wrap(err, "Shutting down container version controller")
		}

		return nil
	}

	return rc
}

type ecrSyncParams struct {
	k8sConfig string
	namespace string

	syncFreq int
	tag      string
	ecr      string

	deployment string
	container  string

	configKey string

	stats statsParams
}

func newECRSyncCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ecr-sync",
		Short: "Poll ECR to check for deployoments",
		Long:  "Continuously polls ECR to check if the a service deployment needs updates and if so, performs the update via k8s APIs",
	}

	var params ecrSyncParams
	cmd.Flags().IntVar(&params.syncFreq, "sync", 5, "Sync frequency in minutes")
	cmd.Flags().StringVar(&params.tag, "tag", "", "Tag name to monitor on")
	cmd.Flags().StringVar(&params.ecr, "ecr", "", "ECR repository ARN ex. 973383851042.dkr.ecr.ap-southeast-2.amazonaws.com/nearmap/cvmanager")
	cmd.Flags().StringVar(&params.k8sConfig, "k8s-config", "", "Path to the kube config file. Only required for running outside k8s cluster. In cluster, pods credentials are used")
	cmd.Flags().StringVar(&params.namespace, "namespace", "", "namespace")
	cmd.Flags().StringVar(&params.deployment, "deployment", "", "name of the deployment to monitor")
	cmd.Flags().StringVar(&params.container, "container", "", "name of the container in specified deployment to monitor")
	cmd.Flags().StringVar(&params.configKey, "configKey", "", "full path key of configmap containing version in format <configmapname>/<key> eg photos/version")
	(&params.stats).addFlags(cmd)

	cmd.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if params.ecr == "" || params.tag == "" || params.deployment == "" || params.namespace == "" || params.container == "" {
			return errors.New("Deployment, ecr repository name, tag, namespace, container to watch on must be provided.")
		}
		return nil
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		log.Print("Starting ECR Sync")

		stats, err := params.stats.stats(fmt.Sprintf("%s/%s", params.namespace, params.deployment))
		if err != nil {
			return errors.Wrap(err, "failed to initialize stats")
		}

		scStatus := 0
		defer stats.ServiceCheck("ecrsync.exec", "", scStatus, time.Now())

		sess, err := session.NewSession()
		if err != nil {
			return errors.Wrap(err, "failed to obtain AWS session")
		}
		stopChan := signals.SetupTwoWaySignalHandler()

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

		ecrSyncer, err := ecr.NewSyncer(sess, k8sClient, params.namespace, &ecr.SyncConfig{
			Freq:       params.syncFreq,
			Tag:        params.tag,
			RepoARN:    params.ecr,
			Deployment: params.deployment,
			Container:  params.container,
			ConfigKey:  params.configKey,
		}, stats)
		if err != nil {
			log.Printf("Failed to create syncer with syncfrequnecy=%v, ecrName=%v, tag=%v, deployment=%v, error=%v",
				params.syncFreq, params.ecr, params.tag, params.deployment, err)
			return errors.Wrap(err, "Failed to create syncer")
		}

		log.Printf("Starting ECR syncer with syncfrequnecy=%v, ecrName=%v, tag=%v, deployment=%v",
			params.syncFreq, params.ecr, params.tag, params.deployment)

		stats.ServiceCheck("ecrsync.exec", "", scStatus, time.Now())
		go func() {
			if err := ecrSyncer.Sync(); err != nil {
				log.Printf("Server error during ecr sync: %v", err)
				stopChan <- os.Interrupt
			}
		}()

		<-stopChan
		log.Printf("ECRsync Server gracefully stopped")

		return nil
	}

	return cmd
}
