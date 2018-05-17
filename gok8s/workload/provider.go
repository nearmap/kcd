package k8s

import (
	"log"
	"strings"

	"github.com/nearmap/cvmanager/config"
	"github.com/nearmap/cvmanager/deploy"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	clientset "github.com/nearmap/cvmanager/gok8s/client/clientset/versioned"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// Workload defines an interface for something deployable, such as a Deployment, DaemonSet, Pod, etc.
type Workload interface {
	deploy.RolloutTarget

	// AsResource returns a Resource struct defining the current state of the workload.
	AsResource(cv *cv1.ContainerVersion) *Resource
}

// Resource maintains a high level status of deployments managed by
// CV resources including version of current deploy and number of available pods
// from this deployment/replicaset
type Resource struct {
	Namespace     string
	Name          string
	Type          string
	Container     string
	Version       string
	AvailablePods int32

	CV  string
	Tag string
}

// Provider manages workloads.
type Provider struct {
	cs        kubernetes.Interface
	cvcs      clientset.Interface
	namespace string

	options *config.Options
}

// NewProvider abstracts operations performed against Kubernetes resources such as syncing deployments
// config maps etc
func NewProvider(cs kubernetes.Interface, cvcs clientset.Interface, ns string, options ...func(*config.Options)) *Provider {
	opts := config.NewOptions()
	for _, opt := range options {
		opt(opts)
	}

	return &Provider{
		cs:        cs,
		cvcs:      cvcs,
		namespace: ns,
		options:   opts,
	}
}

// Namespace returns the namespace that this K8sProvider is operating within.
func (k *Provider) Namespace() string {
	return k.namespace
}

// Client returns a kubernetes client interface for working directly with the kubernetes API.
// The client will only work within the namespace of the provider.
func (k *Provider) Client() kubernetes.Interface {
	return k.cs
}

// CanRollout returns true if we can attempt a rollout for the cv resource at the given version.
func (k *Provider) CanRollout(cv *cv1.ContainerVersion, version string) bool {
	return cv.Status.LastFailedVersion != version
}

// UpdateFailedRollout updates the cv status to indicate that the rollout of the given
// version has failed and thus should not be reattempted.
func (k *Provider) UpdateFailedRollout(cv *cv1.ContainerVersion, version string) (*cv1.ContainerVersion, error) {
	client := k.cvcs.CustomV1().ContainerVersions(k.namespace)

	// ensure state is up to date
	var err error
	cv, err = client.Get(cv.Name, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get ContainerVersion instance with name %s", cv.Name)
	}

	cv.Status.LastFailedVersion = version

	result, err := client.Update(cv)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to update ContainerVersion spec %s", cv.Name)
	}

	log.Printf("Updated last failed version for cv spec=%s, version=%s", cv.Name, version)
	return result, nil
}

// AllResources returns all resources managed by container versions in the current namespace.
func (k *Provider) AllResources() ([]*Resource, error) {
	cvs, err := k.cvcs.CustomV1().ContainerVersions("").List(metav1.ListOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "Failed to generate template of CV list")
	}

	var cvsList []*Resource
	for _, cv := range cvs.Items {
		cvs, err := k.CVResources(&cv)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to generate template of CV list")
		}
		cvsList = append(cvsList, cvs...)
	}

	return cvsList, nil
}

// CVResources returns the resources managed by the given cv instance.
func (k *Provider) CVResources(cv *cv1.ContainerVersion) ([]*Resource, error) {
	var resources []*Resource

	specs, err := k.Workloads(cv)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	for _, spec := range specs {
		resources = append(resources, spec.AsResource(cv))
	}

	return resources, nil
}

// Workloads returns the workload instances that match the given container version resource.
func (k *Provider) Workloads(cv *cv1.ContainerVersion) ([]Workload, error) {
	var result []Workload

	set := labels.Set(cv.Spec.Selector)
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

	return result, nil
}

func (k *Provider) handleError(err error, typ string) error {
	//k.options.Recorder.Event(events.Warning, "CRSyncFailed", "Failed to get workload")
	return errors.Wrapf(err, "failed to get %s", typ)
}

func version(img string) string {
	return strings.SplitAfterN(img, ":", 2)[1]
}
