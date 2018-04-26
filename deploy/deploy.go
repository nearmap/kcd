package deploy

import (
	"fmt"
	"log"
	"time"

	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/history"
	"github.com/nearmap/cvmanager/stats"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

// DeploySpec defines an interface for something deployable, such as a Deployment, DaemonSet, Pod, etc.
type DeploySpec interface {
	Name() string

	// Type returns the type of the spec.
	Type() string

	PodSpec() corev1.PodSpec

	// PatchPodSpec receives a pod spec and container which is to be patched
	// according to an appropriate strategy for the type.
	PatchPodSpec(cv *cv1.ContainerVersion, container corev1.Container, version string) error
}

// TemplateDeploySpec defines methods for deployable resources that manage a collection
// of pods, defined by a resource. More deployment options are available for such
// resources.
type TemplateDeploySpec interface {
	DeploySpec

	PodTemplateSpec() corev1.PodTemplateSpec

	// Select all Workloads of this type with the given selector. May return
	// the same WorkloadSpec if it matches the selector.
	Select(selector map[string]string) ([]TemplateDeploySpec, error)

	SelectOwnPods(pods []corev1.Pod) ([]corev1.Pod, error)

	NumReplicas() int32

	PatchNumReplicas(num int32) error
}

//type DeployReplicas interface {
//	NumReplicas() int32
//
//	PatchNumReplicas(num int32) error
//}

type Deployer interface {
	Deploy(cv *cv1.ContainerVersion, version string, spec DeploySpec) error
}

type SimpleDeployer struct {
	namespace string

	hp            history.Provider
	recordHistory bool

	cs       kubernetes.Interface
	recorder events.Recorder

	stats stats.Stats
}

// NewSimpleDeployer returns a new SimpleDeployer instance, which triggers deployments
// using the default Kubernetes deployment strategy for the workload.
func NewSimpleDeployer(cs kubernetes.Interface, eventRecorder events.Recorder, stats stats.Stats, namespace string) *SimpleDeployer {
	return &SimpleDeployer{
		namespace: namespace,
		cs:        cs,
		recorder:  eventRecorder,
		stats:     stats,
	}
}

func (sd *SimpleDeployer) Deploy(cv *cv1.ContainerVersion, version string, spec DeploySpec) error {
	log.Printf("Performing simple deployment on %s with version %s", spec.Name(), version)

	podSpec := spec.PodSpec()

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		for _, c := range podSpec.Containers {
			if c.Name == cv.Spec.Container {
				if updateErr := spec.PatchPodSpec(cv, c, version); updateErr != nil {
					log.Printf("Failed to update container version (will retry): version=%v, workload=%v, error=%v",
						version, spec.Name(), updateErr)

					return updateErr
				}
			}
		}
		return nil
	})
	if retryErr != nil {
		sd.stats.Event(fmt.Sprintf("%s.sync.failure", spec.Name()),
			fmt.Sprintf("Failed to validate image with %s", version), "", "error",
			time.Now().UTC())
		log.Printf("Failed to update container version after maximum retries: version=%v, workload=%v, error=%v",
			version, spec.Name(), retryErr)
		sd.recorder.Event(events.Warning, "CRSyncFailed", "Failed to perform the workload")
	}

	if sd.recordHistory {
		err := sd.hp.Add(sd.namespace, spec.Name(), &history.Record{
			Type:    spec.Type(),
			Name:    spec.Name(),
			Version: version,
			Time:    time.Now(),
		})
		if err != nil {
			sd.stats.IncCount(fmt.Sprintf("cvc.%s.history.save.failure", spec.Name()))
			sd.recorder.Event(events.Warning, "SaveHistoryFailed", "Failed to record update history")
		}
	}

	log.Printf("Update completed: workload=%v", spec.Name())
	sd.stats.IncCount(fmt.Sprintf("%s.sync.success", spec.Name()))
	sd.recorder.Event(events.Normal, "Success", "Updated completed successfully")
	return nil
}
