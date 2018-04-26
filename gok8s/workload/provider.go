package k8s

import (
	"fmt"
	"log"
	"time"

	"github.com/nearmap/cvmanager/deploy"
	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/history"
	"github.com/nearmap/cvmanager/registry/errs"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// WorkloadSpec defines an interface for something deployable, such as a Deployment, DaemonSet, Pod, etc.
type WorkloadSpec interface {
	deploy.DeploySpec

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

type K8sProvider struct {
	cs        kubernetes.Interface
	namespace string
	//deployer  deploy.Deployer

	hp            history.Provider
	recordHistory bool

	stats    stats.Stats
	Recorder events.Recorder
}

// NewK8sProvider abstracts operation performed against Kubernetes resources such as syncing deployments
// config maps etc
func NewK8sProvider(cs kubernetes.Interface, ns string, stats stats.Stats, recordHistory bool) *K8sProvider {
	var recorder events.Recorder
	var err error
	recorder, err = events.NewRecorder(cs, ns)
	if err != nil {
		log.Print("No INSTANCENAME was set hence cant record events on pod.. falling back to using fake event recorder")
		recorder = &events.FakeRecorder{}
	}

	return &K8sProvider{
		cs:        cs,
		namespace: ns,
		//deployer:  deploy.NewSimpleDeployer(cs, recorder, stats, ns),

		hp:            history.NewProvider(cs, stats),
		recordHistory: recordHistory,

		Recorder: recorder,
		stats:    stats,
	}
}

// SyncWorkload checks if container version of all workload that matches CV selector
// is up to date with the version that is requested, if not it
// performs a rollout with specified roll-out strategy on deployment.
func (k *K8sProvider) SyncWorkload(cv *cv1.ContainerVersion, version string) error {
	specs, err := k.getMatchingWorkloadSpecs(cv)
	if err != nil {
		return errors.WithStack(err)
	}

	for _, spec := range specs {
		if err := checkPodSpec(cv, version, spec.PodSpec()); err != nil {
			if err == errs.ErrVersionMismatch {
				// temp
				log.Printf("Spec %s version mismatch - performing deploy", spec.Name())

				if err := k.deploy(cv, version, spec); err != nil {
					return errors.WithStack(err)
				}
			} else {
				k.stats.Event(fmt.Sprintf("%s.sync.failure", spec.Name()), err.Error(), "", "error", time.Now().UTC())
				k.Recorder.Event(events.Warning, "CRSyncFailed", err.Error())
				return errors.Wrapf(err, "failed to check pod spec %s", spec.Name())
			}
		} else {
			// temp
			log.Printf("Spec %s versions match %s", spec.Name(), version)
		}
	}

	return nil
}

func (k *K8sProvider) deploy(cv *cv1.ContainerVersion, version string, spec deploy.DeploySpec) error {
	var strategyType string

	if cv.Spec.Strategy != nil {
		strategyType = cv.Spec.Strategy.Type
	}

	var deployer deploy.Deployer

	switch strategyType {
	case "blue-green":
		deployer = deploy.NewBlueGreenDeployer(k.cs, k.Recorder, k.stats, k.namespace)
	default:
		deployer = deploy.NewSimpleDeployer(k.cs, k.Recorder, k.stats, k.namespace)
	}

	if err := deployer.Deploy(cv, version, spec); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// CVWorkload generates details about current workload resources managed by CVManager
func (k *K8sProvider) CVWorkload(cv *cv1.ContainerVersion) ([]*Resource, error) {
	var resources []*Resource

	specs, err := k.getMatchingWorkloadSpecs(cv)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	for _, spec := range specs {
		resources = append(resources, spec.AsResource(cv))
	}

	return resources, nil
}

func (k *K8sProvider) getMatchingWorkloadSpecs(cv *cv1.ContainerVersion) ([]WorkloadSpec, error) {
	var result []WorkloadSpec

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

	cronJobs, err := k.cs.BatchV2alpha1().CronJobs(k.namespace).List(listOpts)
	if err != nil {
		return nil, k.handleError(err, "cronJobs")
	}
	for _, item := range cronJobs.Items {
		wl := item
		result = append(result, NewCronJob(k.cs, k.namespace, &wl))
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

func (k *K8sProvider) handleError(err error, typ string) error {
	k.Recorder.Event(events.Warning, "CRSyncFailed", "Failed to get dependent deployment")
	return errors.Wrapf(err, "failed to get %s", typ)
}

func validate(v string) error {
	//TODO later regression check etc
	return nil
}
