package k8s

import (
	"log"
	"os"

	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

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

type K8sProvider struct {
	cs       kubernetes.Interface
	Recorder record.EventRecorder
	Pod      *v1.Pod

	namespace string

	stats stats.Stats
}

// NewK8sProvider abstracts operation performed against Kubernetes resources such as syncing deployments
// config maps etc
func NewK8sProvider(cs kubernetes.Interface, ns string, stats stats.Stats) *K8sProvider {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(log.Printf)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: cs.CoreV1().Events("")})

	var recorder record.EventRecorder
	pod, err := cs.CoreV1().Pods(ns).Get(os.Getenv("INSTANCENAME"), metav1.GetOptions{})
	if err != nil {
		log.Print("No INSTANCENAME was set hence cant record events on pod.. falling back to using fake event recorder")
		recorder = record.NewFakeRecorder(50)
	} else {
		recorder = eventBroadcaster.NewRecorder(k8sscheme.Scheme, corev1.EventSource{Component: "container-version-controller"})
	}

	return &K8sProvider{
		cs:       cs,
		Recorder: recorder,
		Pod:      pod,

		namespace: ns,

		stats: stats,
	}

}

// SyncWorkload checks if container version of all workload that matches CV selector
// is up to date with the version thats requested, if not its
// performs a rollout with specified roll-out strategy on deployment
func (k *K8sProvider) SyncWorkload(cv *cv1.ContainerVersion, version string) error {
	set := labels.Set(cv.Spec.Selector)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}
	// Alpha workload
	err := k.syncDeployments(cv, version, listOpts)
	if err != nil {
		errors.Wrap(err, "failed to sync deployments")
	}
	err = k.syncDaemonSets(cv, version, listOpts)
	if err != nil {
		errors.Wrap(err, "failed to sync syncDaemonSets")
	}
	err = k.syncReplicaSet(cv, version, listOpts)
	if err != nil {
		errors.Wrap(err, "failed to sync syncReplicaSets")
	}
	err = k.syncStatefulSet(cv, version, listOpts)
	if err != nil {
		errors.Wrap(err, "failed to sync syncStatefulSets")
	}
	// batch workloads
	err = k.syncJobs(cv, version, listOpts)
	if err != nil {
		errors.Wrap(err, "failed to sync jobs")
	}
	err = k.syncCronJobs(cv, version, listOpts)
	if err != nil {
		errors.Wrap(err, "failed to sync cronjobs")
	}

	return err
}

// CVWorkload generates details about current workload resources managed by CVManager
func (k *K8sProvider) CVWorkload(cv *cv1.ContainerVersion) ([]*Resource, error) {
	var cvsList []*Resource
	set := labels.Set(cv.Spec.Selector)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}

	// workloads
	rs, err := k.cvDeployment(cv, listOpts)
	if err != nil {
		log.Printf("Error: %v", err)
		return nil, errors.Errorf("Failed to fetch all Deployments managed by CV resources")
	}
	cvsList = append(cvsList, rs...)

	rs, err = k.cvDaemonSets(cv, listOpts)
	if err != nil {
		log.Printf("Error: %v", err)
		return nil, errors.Errorf("Failed to fetch all DaemonSets managed by CV resources")
	}
	cvsList = append(cvsList, rs...)

	rs, err = k.cvStatefulSets(cv, listOpts)
	if err != nil {
		log.Printf("Error: %v", err)
		return nil, errors.Errorf("Failed to fetch all StatefulSets managed by CV resources")
	}
	cvsList = append(cvsList, rs...)

	rs, err = k.cvReplicaSets(cv, listOpts)
	if err != nil {
		log.Printf("Error: %v", err)
		return nil, errors.Errorf("Failed to fetch all ReplicaSets managed by CV resources")
	}
	cvsList = append(cvsList, rs...)

	// Batch workload
	rs, err = k.cvCronJobs(cv, listOpts)
	if err != nil {
		log.Printf("Error: %v", err)
		return nil, errors.Errorf("Failed to fetch all CronJobs managed by CV resources")
	}
	cvsList = append(cvsList, rs...)

	rs, err = k.cvJobs(cv, listOpts)
	if err != nil {
		log.Printf("Error: %v", err)
		return nil, errors.Errorf("Failed to fetch all Jobs managed by CV resources")
	}
	cvsList = append(cvsList, rs...)

	return cvsList, nil
}

func (k *K8sProvider) validate(v string) error {
	//TODO later regression check etc
	return nil
}
