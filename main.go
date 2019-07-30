package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/golang/glog"
	conf "github.com/Eric1313/kcd/config"
	"github.com/Eric1313/kcd/events"
	clientset "github.com/Eric1313/kcd/gok8s/client/clientset/versioned"
	informer "github.com/Eric1313/kcd/gok8s/client/informers/externalversions"
	"github.com/Eric1313/kcd/gok8s/workload"
	"github.com/Eric1313/kcd/handler"
	"github.com/Eric1313/kcd/history"
	"github.com/Eric1313/kcd/resource"
	svc "github.com/Eric1313/kcd/service"
	"github.com/Eric1313/kcd/signals"
	"github.com/Eric1313/kcd/stats"
	"github.com/Eric1313/kcd/stats/datadog"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextCS "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/server/options"
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
func withExitCode() (code int) {
	defer func() {
		if r := recover(); r != nil {
			glog.Errorf("Panic: %v", r)
			code = 2
		}
		glog.Flush()
	}()

	// add glog flags
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	rootCmd := &cobra.Command{
		Short: "kcd",
		Long:  "Kubernetes Continuous Delivery (kcd): a custom controller and tooling to manage CI/CD on kubernetes clusters",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// prevent glog complaining about flags not being parsed
			flag.CommandLine.Parse([]string{})
		},
	}
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	rootCmd.AddCommand(newRunCommand())
	rootCmd.AddCommand(newCRCommands())
	rootCmd.AddCommand(newCVCommand())

	err := rootCmd.Execute()
	if err != nil {
		glog.Errorf("Error executing command: %v", err)
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

	kcdImgRepo string

	port int

	history  bool // unused
	rollback bool // unused

	stats statsParams
}

func newRunCommand() *cobra.Command {
	var params runParams
	rc := &cobra.Command{
		Use:   "run",
		Short: "Runs the kcd controller service",
		Long:  fmt.Sprintf(`Runs the service as a HTTP server`),
	}

	rc.Flags().StringVar(&params.k8sConfig, "k8s-config", "", "Path to the kube config file. Only required for running outside k8s cluster. In cluster, pods credentials are used")
	rc.Flags().StringVar(&params.configMapKey, "configmap-key", "kube-system/kcd", "Namespaced key of configmap that container version and region config defined")
	rc.Flags().StringVar(&params.kcdImgRepo, "kcd-img-repo", "nearmap/kcd", "Name of the docker registry to used be controller. defaults to nearmap/kcd")
	rc.Flags().BoolVar(&params.history, "history", false, "unused")
	rc.Flags().BoolVar(&params.rollback, "rollback", false, "unused")
	rc.Flags().IntVar(&params.port, "port", 8081, "Port to run http server on")
	(&params.stats).addFlags(rc)

	rc.RunE = func(cmd *cobra.Command, args []string) (err error) {
		stats, err := params.stats.stats("kcd")
		if err != nil {
			return errors.Wrap(err, "failed to initialize stats")
		}

		scStatus := 0
		defer stats.ServiceCheck("kcd.exec", "", scStatus, time.Now())

		stopCh := signals.SetupSignalHandler()

		var cfg *rest.Config
		if params.k8sConfig != "" {
			cfg, err = clientcmd.BuildConfigFromFlags("", params.k8sConfig)
		} else {
			cfg, err = rest.InClusterConfig()
		}
		if err != nil {
			scStatus = 2
			glog.Errorf("Failed to get k8s config: %v", err)
			return errors.Wrap(err, "Error building k8s configs either run in cluster or provide config file")
		}

		k8sClient, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			scStatus = 2
			glog.Errorf("Error building k8s clientset: %v", err)
			return errors.Wrap(err, "Error building k8s clientset")
		}

		customClient, err := clientset.NewForConfig(cfg)
		if err != nil {
			scStatus = 2
			glog.Errorf("Error building k8s container version clientset: %v", err)
			return errors.Wrap(err, "Error building k8s container version clientset")
		}

		// // TODO: this is dodgy it expects k8s files to always be available from runtime directory
		// // need to packae the yaml n version file using tool chains properly
		// There is also problem in how its updated .. it corrupts the definition
		// err = updateCVCRDSpec(cfg)
		// if err != nil {
		// 	glog.Errorf("Failed to update CRD spec: %v. Apply manually using \n 'kubectl apply k8s/crd.yaml'", err)
		// 	//return errors.Wrap(err, "Failed to read CV CRD specification")
		// }

		k8sInformerFactory := k8sinformers.NewSharedInformerFactory(k8sClient, time.Second*30)
		customInformerFactory := informer.NewSharedInformerFactory(customClient, time.Second*30)

		// Controllers here
		kcdc, err := svc.NewCVController(params.configMapKey, params.kcdImgRepo,
			k8sClient, customClient,
			k8sInformerFactory, customInformerFactory,
			conf.WithStats(stats))
		if err != nil {
			return errors.Wrap(err, "Failed to create controller")
		}

		k8sInformerFactory.Start(stopCh)
		customInformerFactory.Start(stopCh)
		glog.V(1).Info("Started informer factory")

		stats.ServiceCheck("kcd.exec", "", scStatus, time.Now())

		recorder := events.PodEventRecorder(k8sClient, "")
		workloadProvider := workload.NewProvider(k8sClient, customClient, "", conf.WithStats(stats), conf.WithRecorder(recorder))
		historyProvider := history.NewProvider(k8sClient, stats)
		resourceProvider := resource.NewK8sProvider("", customClient, workloadProvider)

		authOptions := options.NewDelegatingAuthenticationOptions()
		if params.k8sConfig != "" {
			authOptions.RemoteKubeConfigFile = params.k8sConfig
		}

		go func() {
			if err = kcdc.Run(2, stopCh); err != nil {
				glog.V(1).Infof("Shutting down container version controller: %v", err)
				//return errors.Wrap(err, "Shutting down container version controller")
			}
		}()
		err = handler.NewServer(params.port, Version, resourceProvider, historyProvider, authOptions, stopCh)
		if err != nil {
			return errors.Wrap(err, "failed to start new server")
		}

		return nil
	}

	return rc
}

func updateCVCRDSpec(cfg *rest.Config) error {
	apiExtCS, err := apiextCS.NewForConfig(cfg)
	if err != nil {
		return errors.Wrap(err, "Error building api extension clientset")
	}
	v, err := ioutil.ReadFile("k8s/crd.yaml")
	if err != nil {
		return errors.Wrap(err, "Failed to read CV CRD specification")
	}

	scheme := runtime.NewScheme()
	apiextv1beta1.AddToScheme(scheme)
	codec := serializer.NewCodecFactory(scheme)
	decode := codec.UniversalDeserializer().Decode

	obj, _, err := decode(v, nil, nil)
	if err != nil {
		return errors.Wrap(err, "Failed to read CV CRD specification")
	}

	crd := obj.(*apiextv1beta1.CustomResourceDefinition)

	crdSrc, err := apiExtCS.ApiextensionsV1beta1().CustomResourceDefinitions().Get(crd.Name, metav1.GetOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			if _, err = apiExtCS.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd); err != nil {
				return errors.WithStack(err)
			}
		}
		return errors.WithStack(err)
	}
	crd.ObjectMeta = crdSrc.ObjectMeta
	_, err = apiExtCS.ApiextensionsV1beta1().CustomResourceDefinitions().Update(crd)
	if err != nil {
		return errors.WithStack(err)
	}
	return err
}
