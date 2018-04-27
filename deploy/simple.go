package deploy

import (
	"fmt"
	"log"
	"time"

	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/history"
	"github.com/nearmap/cvmanager/stats"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

// SimpleDeployer implements a rollout strategy by patching the target's pod spec with a new version.
type SimpleDeployer struct {
	namespace string

	hp            history.Provider
	recordHistory bool

	cs       kubernetes.Interface
	recorder events.Recorder

	stats stats.Stats
}

// NewSimpleDeployer returns a new SimpleDeployer instance, which triggers rollouts
// by patching the target's pod spec with a new version and using the default
// Kubernetes deployment strategy for the workload.
func NewSimpleDeployer(cs kubernetes.Interface, eventRecorder events.Recorder, stats stats.Stats, namespace string) *SimpleDeployer {
	return &SimpleDeployer{
		namespace: namespace,
		cs:        cs,
		recorder:  eventRecorder,
		stats:     stats,
	}
}

// Deploy implements the Deployer interface.
func (sd *SimpleDeployer) Deploy(cv *cv1.ContainerVersion, version string, target RolloutTarget) error {
	log.Printf("Performing simple deployment on %s with version %s", target.Name(), version)

	podSpec := target.PodSpec()

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		for _, c := range podSpec.Containers {
			if c.Name == cv.Spec.Container {
				if updateErr := target.PatchPodSpec(cv, c, version); updateErr != nil {
					log.Printf("Failed to update container version (will retry): version=%v, target=%v, error=%v",
						version, target.Name(), updateErr)

					return updateErr
				}
			}
		}
		return nil
	})
	if retryErr != nil {
		sd.stats.Event(fmt.Sprintf("%s.sync.failure", target.Name()),
			fmt.Sprintf("Failed to validate image with %s", version), "", "error",
			time.Now().UTC())
		log.Printf("Failed to update container version after maximum retries: version=%v, target=%v, error=%v",
			version, target.Name(), retryErr)
		sd.recorder.Event(events.Warning, "CRSyncFailed", "Failed to deploy the target")
	}

	if sd.recordHistory {
		err := sd.hp.Add(sd.namespace, target.Name(), &history.Record{
			Type:    target.Type(),
			Name:    target.Name(),
			Version: version,
			Time:    time.Now(),
		})
		if err != nil {
			sd.stats.IncCount(fmt.Sprintf("crsyn.%s.history.save.failure", target.Name()))
			sd.recorder.Event(events.Warning, "SaveHistoryFailed", "Failed to record update history")
		}
	}

	log.Printf("Update completed: target=%v", target.Name())
	sd.stats.IncCount(fmt.Sprintf("crsyn.%s.sync.success", target.Name()))
	sd.recorder.Event(events.Normal, "Success", "Updated completed successfully")
	return nil
}
