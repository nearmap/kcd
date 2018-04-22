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

type PatchPodSpecFn func(i int) error
type TypeFn func() string

type Deployer interface {
	Deploy(d corev1.PodTemplateSpec, name, tag string, cv *cv1.ContainerVersion, ppfn PatchPodSpecFn, typFn TypeFn) error
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
	}
}

func (sd *SimpleDeployer) Deploy(d corev1.PodTemplateSpec, name, tag string, cv *cv1.ContainerVersion,
	ppfn PatchPodSpecFn, typFn TypeFn) error {

	log.Printf("Beginning rollout for workload %s with version %s", name, tag)

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		for i, c := range d.Spec.Containers {
			if c.Name == cv.Spec.Container {
				if updateErr := ppfn(i); updateErr != nil {
					log.Printf("Failed to update container version (will retry): version=%v, workload=%v, error=%v",
						tag, d.Name, updateErr)

					return updateErr
				}

			}
		}
		return nil
	})
	if retryErr != nil {
		sd.stats.Event(fmt.Sprintf("%s.sync.failure", name),
			fmt.Sprintf("Failed to validate image with %s", tag), "", "error",
			time.Now().UTC())
		log.Printf("Failed to update container version after maximum retries: version=%v, workload=%v, error=%v",
			tag, name, retryErr)
		sd.recorder.Event(events.Warning, "CRSyncFailed", "Failed to perform the workload")
	}

	if sd.recordHistory {
		err := sd.hp.Add(sd.namespace, name, &history.Record{
			Type:    typFn(),
			Name:    name,
			Version: tag,
			Time:    time.Now(),
		})
		if err != nil {
			sd.stats.IncCount(fmt.Sprintf("cvc.%s.history.save.failure", name))
			sd.recorder.Event(events.Warning, "SaveHistoryFailed", "Failed to record update history")
		}
	}

	log.Printf("Update completed: workload=%v", name)
	sd.stats.IncCount(fmt.Sprintf("%s.sync.success", name))
	sd.recorder.Event(events.Normal, "Success", "Updated completed successfully")
	return nil
}
