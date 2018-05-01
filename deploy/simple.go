package deploy

import (
	"fmt"
	"log"
	"time"

	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/history"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	"k8s.io/client-go/util/retry"
)

// SimpleDeployer implements a rollout strategy by patching the target's pod spec with a new version.
type SimpleDeployer struct {
	namespace string

	hp            history.Provider
	recordHistory bool

	recorder events.Recorder
	stats    stats.Stats
}

// NewSimpleDeployer returns a new SimpleDeployer instance, which triggers rollouts
// by patching the target's pod spec with a new version and using the default
// Kubernetes deployment strategy for the workload.
func NewSimpleDeployer(eventRecorder events.Recorder, stats stats.Stats, namespace string) *SimpleDeployer {
	return &SimpleDeployer{
		namespace: namespace,
		recorder:  eventRecorder,
		stats:     stats,
	}
}

// Deploy implements the Deployer interface.
func (sd *SimpleDeployer) Deploy(cv *cv1.ContainerVersion, version string, target RolloutTarget) error {
	log.Printf("Performing simple deployment on %s with version %s", target.Name(), version)

	found := false
	podSpec := target.PodSpec()

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		for _, c := range podSpec.Containers {
			if c.Name == cv.Spec.Container {
				found = true
				if updateErr := target.PatchPodSpec(cv, c, version); updateErr != nil {
					log.Printf("Failed to update container version: version=%v, target=%v, error=%v",
						version, target.Name(), updateErr)
					return updateErr
				}
			}
		}
		return nil
	})
	if err == nil && !found {
		err = errors.Errorf("container with name %s not found in PodSpec for target %s", cv.Spec.Container, target.Name())
	}
	if err != nil {
		sd.stats.Event(fmt.Sprintf("%s.sync.failure", target.Name()),
			fmt.Sprintf("Failed to validate image with %s", version), "", "error",
			time.Now().UTC())
		log.Printf("Failed to update container version after maximum retries: version=%v, target=%v, error=%v",
			version, target.Name(), err)
		sd.recorder.Event(events.Warning, "CRSyncFailed", "Failed to deploy the target")
		return errors.WithStack(err)
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
