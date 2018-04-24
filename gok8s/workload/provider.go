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
		if err := checkPodSpec(cv, version, spec.PodTemplateSpec()); err != nil {
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

	/*
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
	*/
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

func (k *K8sProvider) getMatchingWorkloadSpecs(cv *cv1.ContainerVersion) ([]WorkloadSpec, error) {
	var result []WorkloadSpec

	set := labels.Set(cv.Spec.Selector)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}

	wls, err := k.cs.AppsV1().Deployments(k.namespace).List(listOpts)
	if err != nil {
		return nil, k.handleError(err, "deployments")
	}
	for _, wl := range wls.Items {
		result = append(result, NewDeployment(k.cs, k.namespace, &wl))
	}

	// TODO: other types

	return result, nil
}

func (k *K8sProvider) handleError(err error, typ string) error {
	k.Recorder.Event(events.Warning, "CRSyncFailed", "Failed to get dependent deployment")
	return errors.Wrapf(err, "failed to get %s", typ)
}

// CVWorkload generates details about current workload resources managed by CVManager
func (k *K8sProvider) CVWorkload(cv *cv1.ContainerVersion) ([]*Resource, error) {

	// TODO:
	return nil, nil

	/*
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
	*/
}

func validate(v string) error {
	//TODO later regression check etc
	return nil
}
