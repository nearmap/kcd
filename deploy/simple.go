package deploy

import (
	"fmt"
	"log"
	"strings"
	"time"

	conf "github.com/nearmap/cvmanager/config"
	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/history"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

// SimpleDeployer implements a rollout strategy by patching the target's pod spec with a new version.
type SimpleDeployer struct {
	namespace string

	hp history.Provider

	cs       kubernetes.Interface
	recorder events.Recorder

	opts *conf.Options
}

// NewSimpleDeployer returns a new SimpleDeployer instance, which triggers rollouts
// by patching the target's pod spec with a new version and using the default
// Kubernetes deployment strategy for the workload.
func NewSimpleDeployer(cs kubernetes.Interface, eventRecorder events.Recorder, namespace string,
	options ...func(*conf.Options)) *SimpleDeployer {

	opts := conf.NewOptions()
	for _, opt := range options {
		opt(opts)
	}

	return &SimpleDeployer{
		namespace: namespace,
		cs:        cs,
		recorder:  eventRecorder,
	}
}

// Deploy implements the Deployer interface.
func (sd *SimpleDeployer) Deploy(cv *cv1.ContainerVersion, version string, target RolloutTarget) error {
	log.Printf("Performing simple deployment on %s with version %s", target.Name(), version)

	podSpec := target.PodSpec()

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		for _, c := range podSpec.Containers {
			if c.Name == cv.Spec.Container.Name {
				if updateErr := target.PatchPodSpec(cv, c, version); updateErr != nil {
					log.Printf("Failed to update container version (will retry): version=%v, target=%v, error=%v",
						version, target.Name(), updateErr)

					if updateErr != nil {
						return updateErr
					}
					if sd.opts.UseRollback && target.RollbackAfter() != nil {
						time.Sleep(*target.RollbackAfter())
						if !target.ProgressHealth() {
							currentVersion := strings.SplitAfterN(c.Image, ":", 2)[1]
							return retry.RetryOnConflict(retry.DefaultRetry, func() error {
								if rbErr := target.PatchPodSpec(cv, c, currentVersion); rbErr != nil {
									log.Printf(`Failed to rollback container version (will retry):
										from version=%v, to version=%v, target=%v, error=%v`,
										version, currentVersion, target.Name(), updateErr)
								}
								return nil
							})
						}
					}
					return nil
				}
			}
		}
		return nil
	})
	if retryErr == nil {
		if sd.opts.UseHistory {
			err := sd.hp.Add(sd.namespace, target.Name(), &history.Record{
				Type:    target.Type(),
				Name:    target.Name(),
				Version: version,
				Time:    time.Now(),
			})
			if err != nil {
				sd.opts.Stats.IncCount(fmt.Sprintf("crsyn.%s.history.save.failure", target.Name()))
				sd.recorder.Event(events.Warning, "SaveHistoryFailed", "Failed to record update history")
			}
		}

		log.Printf("Update completed: target=%v", target.Name())
		sd.opts.Stats.IncCount(fmt.Sprintf("crsyn.%s.sync.success", target.Name()))
		sd.recorder.Event(events.Normal, "Success", "Updated completed successfully")
	} else {
		sd.opts.Stats.Event(fmt.Sprintf("%s.sync.failure", target.Name()),
			fmt.Sprintf("Failed to validate image with %s", version), "", "error",
			time.Now().UTC())
		log.Printf("Failed to update container version after maximum retries: version=%v, target=%v, error=%v",
			version, target.Name(), retryErr)
		sd.recorder.Event(events.Warning, "CRSyncFailed", "Failed to deploy the target")
	}
	return nil
}
