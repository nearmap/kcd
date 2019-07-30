package workload

import (
	"github.com/golang/glog"
	"github.com/Eric1313/kcd/config"
	kcdv1 "github.com/Eric1313/kcd/gok8s/apis/custom/v1"
	clientset "github.com/Eric1313/kcd/gok8s/client/clientset/versioned"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// Provider defines methods for working with workloads.
type Provider interface {
	// Namespace returns the namespace that this workload provider is operating in.
	Namespace() string

	// Client provides access to an underlying kubernetes client (TODO: remove)
	Client() kubernetes.Interface

	// Workloads returns all workloads that the given KCD resource selects (via label selectors).
	// Optionally filters by resource type (if no types are present then all workloads are returned).
	Workloads(kcd *kcdv1.KCD, types ...string) ([]Workload, error)
}

// K8sProvider is a Kubernetes implementation of a workload provider.
type K8sProvider struct {
	cs        kubernetes.Interface
	kcdcs     clientset.Interface
	namespace string

	options *config.Options
}

// NewProvider abstracts operations performed against Kubernetes resources such as syncing deployments
// config maps etc
func NewProvider(cs kubernetes.Interface, kcdcs clientset.Interface, ns string, options ...func(*config.Options)) *K8sProvider {
	opts := config.NewOptions()
	for _, opt := range options {
		opt(opts)
	}

	return &K8sProvider{
		cs:        cs,
		kcdcs:     kcdcs,
		namespace: ns,
		options:   opts,
	}
}

// Namespace returns the namespace that this K8sProvider is operating within.
func (k *K8sProvider) Namespace() string {
	return k.namespace
}

// Client returns a kubernetes client interface for working directly with the kubernetes API.
// The client will only work within the namespace of the provider.
func (k *K8sProvider) Client() kubernetes.Interface {
	return k.cs
}

// Workloads returns the workload instances that match the given container version resource.
func (k *K8sProvider) Workloads(kcd *kcdv1.KCD, types ...string) ([]Workload, error) {
	var result []Workload

	glog.V(4).Infof("Retrieving Workloads for kcd=%s", kcd.Name)

	set := labels.Set(kcd.Spec.Selector)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}

	if contains(types, TypeDeployment) {
		deployments, err := k.cs.AppsV1().Deployments(k.namespace).List(listOpts)
		if err != nil {
			return nil, k.handleError(err, "deployments")
		}
		for _, item := range deployments.Items {
			wl := item
			result = append(result, NewDeployment(k.cs, k.namespace, &wl))
		}
	}

	if contains(types, TypeCronJob) {
		cronJobs, err := k.cs.BatchV1beta1().CronJobs(k.namespace).List(listOpts)
		if err != nil {
			return nil, k.handleError(err, "cronJobs")
		} else {
			for _, item := range cronJobs.Items {
				wl := item
				result = append(result, NewCronJob(k.cs, k.namespace, &wl))
			}
		}
	}

	if contains(types, TypeDaemonSet) {
		daemonSets, err := k.cs.AppsV1().DaemonSets(k.namespace).List(listOpts)
		if err != nil {
			return nil, k.handleError(err, "daemonSets")
		}
		for _, item := range daemonSets.Items {
			wl := item
			result = append(result, NewDaemonSet(k.cs, k.namespace, &wl))
		}
	}

	if contains(types, TypeJob) {
		jobs, err := k.cs.BatchV1().Jobs(k.namespace).List(listOpts)
		if err != nil {
			return nil, k.handleError(err, "jobs")
		}
		for _, item := range jobs.Items {
			wl := item
			result = append(result, NewJob(k.cs, k.namespace, &wl))
		}
	}

	if contains(types, TypePod) {
		pods, err := k.cs.CoreV1().Pods(k.namespace).List(listOpts)
		if err != nil {
			return nil, k.handleError(err, "pods")
		}
		for _, item := range pods.Items {
			wl := item
			result = append(result, NewPod(k.cs, k.namespace, &wl))
		}
	}

	if contains(types, TypeReplicaSet) {
		replicaSets, err := k.cs.AppsV1().ReplicaSets(k.namespace).List(listOpts)
		if err != nil {
			return nil, k.handleError(err, "replicaSets")
		}
		for _, item := range replicaSets.Items {
			wl := item
			result = append(result, NewReplicaSet(k.cs, k.namespace, &wl))
		}
	}

	if contains(types, TypeStatefulSet) {
		statefulSets, err := k.cs.AppsV1().StatefulSets(k.namespace).List(listOpts)
		if err != nil {
			return nil, k.handleError(err, "statefulSets")
		}
		for _, item := range statefulSets.Items {
			wl := item
			result = append(result, NewStatefulSet(k.cs, k.namespace, &wl))
		}
	}

	glog.V(2).Infof("Retrieved %d workloads", len(result))

	return result, nil
}

func contains(types []string, typ string) bool {
	if len(types) == 0 {
		return true
	}
	for _, t := range types {
		if t == typ {
			return true
		}
	}
	return false
}

func (k *K8sProvider) handleError(err error, typ string) error {
	//k.options.Recorder.Event(events.Warning, "KCDSyncFailed", "Failed to get workload")
	return errors.Wrapf(err, "failed to get %s", typ)
}
