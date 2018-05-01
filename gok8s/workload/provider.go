package k8s

import (
	"fmt"
	"log"
	"strings"
	"time"

	conf "github.com/nearmap/cvmanager/config"
	"github.com/nearmap/cvmanager/deploy"
	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/history"
	"github.com/nearmap/cvmanager/registry/errs"
	"github.com/nearmap/cvmanager/stats"
	"github.com/nearmap/cvmanager/verify"
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
	namespace string

	hp   history.Provider
	opts *conf.Options

	stats stats.Stats

	Recorder events.Recorder
}

// NewK8sProvider abstracts operation performed against Kubernetes resources such as syncing deployments
// config maps etc
func NewK8sProvider(cs kubernetes.Interface, ns string, recorder events.Recorder, options ...func(*conf.Options)) *K8sProvider {

	opts := conf.NewOptions()
	for _, opt := range options {
		opt(opts)
	}

	return &K8sProvider{
		cs:        cs,
		namespace: ns,

		Recorder: events.PodEventRecorder(cs, ns),
		opts:     opts,

		hp: history.NewProvider(cs, opts.Stats),
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
				if k.validate(version, cv.Spec.Container.Verify) != nil {
					return errors.Errorf("failed to validate image with tag %s", version)
				}
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
	var kind string

	if cv.Spec.Strategy != nil {
		kind = cv.Spec.Strategy.Kind
	}

	var deployer deploy.Deployer

	switch kind {
	case deploy.KindServieBlueGreen:
		deployer = deploy.NewBlueGreenDeployer(k.cs, k.Recorder, k.stats, k.namespace)
	default:
		deployer = deploy.NewSimpleDeployer(k.cs, k.Recorder, k.namespace, conf.WithStats(k.stats))
	}

	if err := deployer.Deploy(cv, version, target); err != nil {
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

	cronJobs, err := k.cs.BatchV1beta1().CronJobs(k.namespace).List(listOpts)
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

func (k *K8sProvider) validate(v string, cvvs []*cv1.VerifySpec) error {
	for _, v := range cvvs {
		verifier, err := verify.NewVerifier(k.cs, k.Recorder, k.opts.Stats, k.namespace, v)
		if err != nil {
			return errors.WithStack(err)
		}
		if err = verifier.Verify(); err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

func version(img string) string {
	return strings.SplitAfterN(img, ":", 2)[1]
}
