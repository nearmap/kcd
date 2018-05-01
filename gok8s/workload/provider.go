package k8s

import (
	"fmt"
	"log"
	"time"

	"github.com/nearmap/cvmanager/deploy"
	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	clientset "github.com/nearmap/cvmanager/gok8s/client/clientset/versioned"
	"github.com/nearmap/cvmanager/history"
	"github.com/nearmap/cvmanager/registry/errs"
	"github.com/nearmap/cvmanager/stats"
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

// K8sProvider manages workloads.
type K8sProvider struct {
	cs        kubernetes.Interface
	cvcs      clientset.Interface
	namespace string

	hp            history.Provider
	recordHistory bool

	stats    stats.Stats
	Recorder events.Recorder
}

// NewK8sProvider abstracts operation performed against Kubernetes resources such as syncing deployments
// config maps etc
func NewK8sProvider(cs kubernetes.Interface, cvcs clientset.Interface, ns string,
	recorder events.Recorder, stats stats.Stats, recordHistory bool) *K8sProvider {

	return &K8sProvider{
		cs:        cs,
		cvcs:      cvcs,
		namespace: ns,

		hp:            history.NewProvider(cs, stats),
		recordHistory: recordHistory,

		Recorder: recorder,
		stats:    stats,
	}
}

// Namespace returns the namespace that this K8sProvider is operating within.
func (k *K8sProvider) Namespace() string {
	return k.namespace
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
				if err := k.deploy(cv, version, spec); err != nil {
					return errors.WithStack(err)
				}
			} else {
				k.stats.Event(fmt.Sprintf("%s.sync.failure", spec.Name()), err.Error(), "", "error", time.Now().UTC())
				k.Recorder.Event(events.Warning, "CRSyncFailed", err.Error())
				return errors.Wrapf(err, "failed to check pod spec %s", spec.Name())
			}
		}
	}

	return nil
}

func (k *K8sProvider) deploy(cv *cv1.ContainerVersion, version string, target deploy.RolloutTarget) error {
	if ok := k.checkRolloutStatus(cv, version, target); !ok {
		return nil
	}

	var deployer deploy.Deployer

	var kind string
	if cv.Spec.Strategy != nil {
		kind = cv.Spec.Strategy.Kind
	}

	switch kind {
	case deploy.KindServieBlueGreen:
		deployer = deploy.NewBlueGreenDeployer(k.cs, k.Recorder, k.stats, k.namespace)
	default:
		deployer = deploy.NewSimpleDeployer(k.Recorder, k.stats, k.namespace)
	}

	if err := deployer.Deploy(cv, version, target); err != nil {
		if deploy.IsPermanent(err) {
			log.Printf("Permanent failure from deploy operation: %v", err)
			if _, e := k.updateFailedRollouts(cv, version); e != nil {
				log.Printf("Failed to update state for container version %s: %v", cv.Name, e)
			}
		}
		return errors.WithStack(err)
	}
	return nil
}

// checkRolloutStatus determines whether a rollout should occur for the target.
// TODO: put the version check and this logic in a single place with a single
// shouldPerformRollout() style method.
func (k *K8sProvider) checkRolloutStatus(cv *cv1.ContainerVersion, version string, target deploy.RolloutTarget) bool {
	maxAttempts := cv.Spec.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	if numFailed := cv.Status.FailedRollouts[version]; numFailed >= maxAttempts {
		log.Printf("Not attempting rollout: cv spec %s for version %s has failed %d times.", cv.Name, version, numFailed)
		return false
	}

	return true
}

func (k *K8sProvider) updateFailedRollouts(cv *cv1.ContainerVersion, version string) (*cv1.ContainerVersion, error) {
	client := k.cvcs.CustomV1().ContainerVersions(k.namespace)

	// ensure state is up to date
	spec, err := client.Get(cv.Name, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get ContainerVersion instance with name %s", cv.Name)
	}
	if spec.Status.FailedRollouts == nil {
		spec.Status.FailedRollouts = map[string]int{}
	}
	spec.Status.FailedRollouts[version] = spec.Status.FailedRollouts[version] + 1

	result, err := client.Update(spec)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to update ContainerVersion spec %s", cv.Name)
	}

	log.Printf("Updated failed rollouts for cv spec=%s, version=%s, numFailures=%d",
		cv.Name, version, cv.Status.FailedRollouts[version])

	return result, nil
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

func (k *K8sProvider) getMatchingWorkloadSpecs(cv *cv1.ContainerVersion) ([]Workload, error) {
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

	cronJobs, err := k.cs.BatchV2alpha1().CronJobs(k.namespace).List(listOpts)
	if err != nil {
		// ignore this error - cron jobs may not be available in cluster
		log.Printf("failed to query cron jobs: %v", err)
		//return nil, k.handleError(err, "cronJobs")
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

func (k *K8sProvider) handleError(err error, typ string) error {
	k.Recorder.Event(events.Warning, "CRSyncFailed", "Failed to get dependent deployment")
	return errors.Wrapf(err, "failed to get %s", typ)
}

func validate(v string) error {
	//TODO later regression check etc
	return nil
}
