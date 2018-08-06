package workload

import (
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/nearmap/kcd/config"
	kcdv1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	clientset "github.com/nearmap/kcd/gok8s/client/clientset/versioned"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// Workload defines an interface for something deployable, such as a Deployment, DaemonSet, Pod, etc.
type Workload interface {
	// Name is the name of the workload (without the namespace).
	Name() string

	// Namespace returns the namespace the workload belongs to.
	Namespace() string

	// Type returns the type of the spec.
	Type() string

	// PodSpec returns the PodSpec for the workload.
	PodSpec() corev1.PodSpec

	// PatchPodSpec receives a pod spec and container which is to be patched
	// according to an appropriate strategy for the type.
	PatchPodSpec(kcd *kcdv1.KCD, container corev1.Container, version string) error

	// RollbackAfter indicates duration after which a failed rollout
	// should attempt rollback
	RollbackAfter() *time.Duration

	// ProgressHealth indicates weather the current status of progress healthy or not.
	// The start time of the deployment operation is provided.
	ProgressHealth(startTime time.Time) (*bool, error)
}

// TemplateWorkload defines methods for deployable resources that manage a collection
// of pods via a pod template. More deployment options are available for such resources.
type TemplateWorkload interface {
	Workload

	// PodTemplateSpec returns the PodTemplateSpec for this workload.
	PodTemplateSpec() corev1.PodTemplateSpec

	// Select all Workloads of this type with the given selector. May return
	// the current spec if it matches the selector.
	Select(selector map[string]string) ([]TemplateWorkload, error)

	// SelectOwnPods returns a list of pods that are managed by this workload.
	SelectOwnPods(pods []corev1.Pod) ([]corev1.Pod, error)

	// NumReplicas returns the current number of running replicas for this workload.
	NumReplicas() int32

	// PatchNumReplicas modifies the number of replicas for this workload.
	PatchNumReplicas(num int32) error
}

// Provider defines methods for working with workloads.
type Provider interface {
	Namespace() string
	Client() kubernetes.Interface
	Workloads(kcd *kcdv1.KCD) ([]Workload, error)
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
func (k *K8sProvider) Workloads(kcd *kcdv1.KCD) ([]Workload, error) {
	var result []Workload

	glog.V(4).Infof("Retrieving Workloads for kcd=%s", kcd.Name)

	set := labels.Set(kcd.Spec.Selector)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}

	deployments, err := k.cs.AppsV1().Deployments(k.namespace).List(listOpts)
	if err != nil {
		return nil, k.handleError(err, "deployments")
	}
	for _, item := range deployments.Items {
		wl := item
		result = append(result, NewDeployment(k.cs, k.namespace, &wl))
	}

	cronJobs, err := k.cs.BatchV1beta1().CronJobs(k.namespace).List(listOpts)
	if err != nil {
		return nil, k.handleError(err, "cronJobs")
	} else {
		for _, item := range cronJobs.Items {
			wl := item
			result = append(result, NewCronJob(k.cs, k.namespace, &wl))
		}
	}

	daemonSets, err := k.cs.AppsV1().DaemonSets(k.namespace).List(listOpts)
	if err != nil {
		return nil, k.handleError(err, "daemonSets")
	}
	for _, item := range daemonSets.Items {
		wl := item
		result = append(result, NewDaemonSet(k.cs, k.namespace, &wl))
	}

	jobs, err := k.cs.BatchV1().Jobs(k.namespace).List(listOpts)
	if err != nil {
		return nil, k.handleError(err, "jobs")
	}
	for _, item := range jobs.Items {
		wl := item
		result = append(result, NewJob(k.cs, k.namespace, &wl))
	}

	pods, err := k.cs.CoreV1().Pods(k.namespace).List(listOpts)
	if err != nil {
		return nil, k.handleError(err, "pods")
	}
	for _, item := range pods.Items {
		wl := item
		result = append(result, NewPod(k.cs, k.namespace, &wl))
	}

	replicaSets, err := k.cs.AppsV1().ReplicaSets(k.namespace).List(listOpts)
	if err != nil {
		return nil, k.handleError(err, "replicaSets")
	}
	for _, item := range replicaSets.Items {
		wl := item
		result = append(result, NewReplicaSet(k.cs, k.namespace, &wl))
	}

	statefulSets, err := k.cs.AppsV1().StatefulSets(k.namespace).List(listOpts)
	if err != nil {
		return nil, k.handleError(err, "statefulSets")
	}
	for _, item := range statefulSets.Items {
		wl := item
		result = append(result, NewStatefulSet(k.cs, k.namespace, &wl))
	}

	glog.V(2).Infof("Retrieved %d workloads", len(result))

	return result, nil
}

func (k *K8sProvider) handleError(err error, typ string) error {
	//k.options.Recorder.Event(events.Warning, "KCDSyncFailed", "Failed to get workload")
	return errors.Wrapf(err, "failed to get %s", typ)
}

func version(img string) string {
	return strings.SplitAfterN(img, ":", 2)[1]
}
