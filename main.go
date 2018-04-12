package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/nearmap/cvmanager/cv"
	clientset "github.com/nearmap/cvmanager/gok8s/client/clientset/versioned"
	informer "github.com/nearmap/cvmanager/gok8s/client/informers/externalversions"
	"github.com/nearmap/cvmanager/handler"
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
		Long:  "Container Version Manager (cvmanager): a custom controller and tooling to manage CI/CD on kubernetes clusters",
	}

	root.AddCommand(newRunCommand())
	root.AddCommand(newCRCommands())
	root.AddCommand(newCVCommand())

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

func (sp *statsParams) stats(namespace string, tags ...string) (stats.Stats, error) {
	if sp.host == "" {
		return stats.NewFake(), nil
	}

	switch sp.provider {
	case "datadog":
		return datadog.New(sp.host, namespace, tags...)
	default:
		return stats.NewFake(), nil
	}
}

type runParams struct {
	k8sConfig    string
	configMapKey string

	cvImgRepo string

	port int

	history bool

	stats statsParams
}

func newRunCommand() *cobra.Command {
	var params runParams
	rc := &cobra.Command{
		Use:   "run",
		Short: "Runs the cv controoler service",
		Long:  fmt.Sprintf(`Runs the service as a HTTP server`),
	}

	rc.Flags().StringVar(&params.k8sConfig, "k8s-config", "", "Path to the kube config file. Only required for running outside k8s cluster. In cluster, pods credentials are used")
	rc.Flags().StringVar(&params.configMapKey, "configmap-key", "kube-system/cvmanager", "Namespaced key of configmap that container version and region config defined")
	rc.Flags().StringVar(&params.cvImgRepo, "cv-img-repo", "nearmap/cvmanager", "Name of the docker registry to used be controller. defaults to nearmap/cvmanager")
	rc.Flags().BoolVar(&params.history, "history", false, "If true, stores the release history in configmap <cv_resource_name>_history")
	rc.Flags().IntVar(&params.port, "port", 8081, "Port to run http server on")
	(&params.stats).addFlags(rc)

	rc.RunE = func(cmd *cobra.Command, args []string) (err error) {
		stats, err := params.stats.stats("cvmanager")
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
		cvc, err := cv.NewCVController(params.configMapKey, params.cvImgRepo, k8sClient, customClient,
			k8sInformerFactory, customInformerFactory, stats, params.history)
		if err != nil {
			return errors.Wrap(err, "Failed to create controller")
		}

		k8sInformerFactory.Start(stopCh)
		customInformerFactory.Start(stopCh)
		log.Printf("Started informer factory")

		stats.ServiceCheck("cvmanager.exec", "", scStatus, time.Now())

		go func() {
			if err = cvc.Run(2, stopCh); err != nil {
				log.Printf("Shutting down container version controller: %v", err)
				//return errors.Wrap(err, "Shutting down container version controller")
			}
		}()
		handler.NewServer(params.port, Version, k8sClient, customClient, stopCh)

		return nil
	}

	return rc
}
