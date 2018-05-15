package deploy

import (
	"context"
	"log"

	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/history"
	"github.com/nearmap/cvmanager/state"
	"github.com/pkg/errors"
	"k8s.io/client-go/util/retry"
)

// SimpleDeployer implements a rollout strategy by patching the target's pod spec with a new version.
type SimpleDeployer struct {
	// TODO: ??
	namespace string

	cv      *cv1.ContainerVersion
	version string
	target  RolloutTarget
	next    state.State

	// TODO: ??
	hp history.Provider

	//opts *conf.Options
}

// NewSimpleDeployer returns a new SimpleDeployer instance, which triggers rollouts
// by patching the target's pod spec with a new version and using the default
// Kubernetes deployment strategy for the workload.
func NewSimpleDeployer(namespace string, cv *cv1.ContainerVersion, version string, target RolloutTarget, next state.State) *SimpleDeployer {
	return &SimpleDeployer{
		namespace: namespace,
		cv:        cv,
		version:   version,
		target:    target,
		next:      next,
	}
}

// Do implements the state interface.
func (sd *SimpleDeployer) Do(ctx context.Context) (state.States, error) {
	log.Printf("Performing simple deployment: target=%s, version=%s", sd.target.Name(), sd.version)

	found := false
	podSpec := sd.target.PodSpec()

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		for _, c := range podSpec.Containers {
			if c.Name == sd.cv.Spec.Container.Name {
				found = true
				if updateErr := sd.target.PatchPodSpec(sd.cv, c, sd.version); updateErr != nil {
					log.Printf("Failed to update container version: version=%v, target=%v, error=%v",
						sd.version, sd.target.Name(), updateErr)

					if updateErr != nil {
						return updateErr
					}

					// TODO:
					/*
						if sd.opts.UseRollback && sd.target.RollbackAfter() != nil {
							time.Sleep(*sd.target.RollbackAfter())
							if !sd.target.ProgressHealth() {
								currentVersion := strings.SplitAfterN(c.Image, ":", 2)[1]
								return retry.RetryOnConflict(retry.DefaultRetry, func() error {
									if rbErr := sd.target.PatchPodSpec(sd.cv, c, currentVersion); rbErr != nil {
										log.Printf(`Failed to rollback container version (will retry):
											from version=%s, to version=%s, target=%s, error=%v`,
											sd.version, currentVersion, sd.target.Name(), updateErr)
									}
									return nil
								})
							}
						}
					*/
					return nil
				}
			}
		}
		return nil
	})
	if err == nil && !found {
		err = errors.Errorf("container with name %s not found in PodSpec for target %s", sd.cv.Spec.Container, sd.target.Name())
	}
	if err != nil {
		log.Printf("Failed to rollout: target=%s, version=%s, error=%v", sd.target.Name(), sd.version, err)
		return state.Error(err)
	}

	return state.Single(sd.next)
}

/*
/////

// Deploy implements the Deployer interface.
func (sd *SimpleDeployer) Deploy(cv *cv1.ContainerVersion, version string, target RolloutTarget) error {
	log.Printf("Performing simple deployment on %s with version %s", target.Name(), version)

	found := false
	podSpec := target.PodSpec()

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		for _, c := range podSpec.Containers {
			if c.Name == cv.Spec.Container.Name {
				found = true
				if updateErr := target.PatchPodSpec(cv, c, version); updateErr != nil {
					log.Printf("Failed to update container version: version=%v, target=%v, error=%v",
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
	if err == nil && !found {
		err = errors.Errorf("container with name %s not found in PodSpec for target %s", cv.Spec.Container, target.Name())
	}
	if err == nil {
		if sd.opts.UseHistory {
			err := sd.hp.Add(sd.namespace, target.Name(), &history.Record{
				Type:    target.Type(),
				Name:    target.Name(),
				Version: version,
				Time:    time.Now(),
			})
			if err != nil {
				sd.opts.Stats.IncCount(fmt.Sprintf("crsyn.%s.history.save.failure", target.Name()))
				sd.opts.Recorder.Event(events.Warning, "SaveHistoryFailed", "Failed to record update history")
			}
		}

		log.Printf("Update completed: target=%v", target.Name())
		sd.opts.Stats.IncCount(fmt.Sprintf("crsyn.%s.sync.success", target.Name()))
		sd.opts.Recorder.Event(events.Normal, "Success", "Updated completed successfully")
	} else {
		sd.opts.Stats.Event(fmt.Sprintf("%s.sync.failure", target.Name()),
			fmt.Sprintf("Failed to validate image with %s", version), "", "error",
			time.Now().UTC())
		log.Printf("Failed to update container version after maximum retries: version=%v, target=%v, error=%v",
			version, target.Name(), err)
		sd.opts.Recorder.Event(events.Warning, "CRSyncFailed", "Failed to deploy the target")
		return errors.WithStack(err)
	}
	return nil
}
*/
