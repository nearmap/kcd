package deploy

import (
	"context"
	"time"

	"github.com/golang/glog"
	kcd1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	"github.com/nearmap/kcd/state"
	"github.com/pkg/errors"
	"k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
)

// SimpleDeployer implements a rollout strategy by patching the target's pod spec with a new version.
type SimpleDeployer struct {
	kcd     *kcd1.KCD
	version string
	targets []RolloutTarget

	next state.State
}

// NewSimpleDeployer returns a new SimpleDeployer instance, which triggers rollouts
// by patching the target's pod spec with a new version and using the default
// Kubernetes deployment strategy for the workload.
func NewSimpleDeployer(kcd *kcd1.KCD, version string, targets []RolloutTarget, next state.State) *SimpleDeployer {
	glog.V(2).Infof("Creating SimpleDeployer: kcd=%s, version=%s, targets=%s", kcd.Name, version, len(targets))

	return &SimpleDeployer{
		kcd:     kcd,
		version: version,
		targets: targets,
		next:    next,
	}
}

// Do implements the state interface.
func (sd *SimpleDeployer) Do(ctx context.Context) (state.States, error) {
	for _, target := range sd.targets {
		glog.V(2).Infof("Performing simple deployment: target=%s, version=%s", target.Name(), sd.version)

		err := sd.patchPodSpec(target, sd.version)
		if err != nil {
			return state.Error(errors.WithStack(err))
		}
	}

	if sd.kcd.Spec.Rollback.Enabled {
		return state.Single(sd.checkRolloutState())
	}

	glog.V(2).Infof("Not checking rollback state")
	return state.Single(sd.next)
}

func (sd *SimpleDeployer) patchPodSpec(target RolloutTarget, version string) error {
	var container *v1.Container
	podSpec := target.PodSpec()

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		for _, c := range podSpec.Containers {
			if c.Name == sd.kcd.Spec.Container.Name {
				if updateErr := target.PatchPodSpec(sd.kcd, c, version); updateErr != nil {
					glog.V(2).Infof("Failed to update container version: version=%v, target=%v, error=%v",
						version, target.Name(), updateErr)
					return updateErr
				}
				container = &c
			}
		}
		return nil
	})
	if err == nil && container == nil {
		err = errors.Errorf("container with name %s not found in PodSpec for target %s",
			sd.kcd.Spec.Container, target.Name())
	}
	if err != nil {
		glog.V(2).Infof("Failed to rollout: target=%s, version=%s, error=%v", target.Name(), sd.version, err)
		return errors.Wrapf(err, "failed to roll out target=%s, version=%s", target.Name(), sd.version)
	}

	return nil
}

func (sd *SimpleDeployer) checkRolloutState() state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		for _, target := range sd.targets {
			glog.V(2).Infof("Checking rollout state: target=%s, version=%s", target.Name(), sd.version)

			ok, err := sd.checkRollout(target)
			if err != nil {
				return state.Error(errors.WithStack(err))
			}
			if ok == nil {
				return state.After(time.Second*15, sd.checkRolloutState())
			}
			if *ok {
				continue
			}

			return state.Single(sd.performRollbackState())
		}

		glog.V(1).Infof("All rollouts succeeded for kcd=%s, version=%s", sd.kcd.Name, sd.version)
		return state.Single(sd.next)
	}
}

// checkRollout determines whether the rollout of the target has completed successfully,
// or needs to be rolled back.
// Returns nil if the rollout is still progressing.
// Returns true if the target doesn't be checked or has been successfully rolled out.
// Returns false if the rollout faied and the workload needs to be rolled back.
func (sd *SimpleDeployer) checkRollout(target RolloutTarget) (ok *bool, err error) {
	if target.RollbackAfter() == nil {
		glog.V(2).Infof("Cannot check for rollback: target %s does not define a progress deadline.", target.Name())
		*ok = true
		return ok, nil
	}

	ok, err = target.ProgressHealth(sd.kcd.Status.CurrStatusTime.Time)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to check progress health for %s", target.Name())
	}
	if ok == nil {
		glog.V(4).Infof("Waiting for rollout state of target %s", target.Name())
	} else if *ok {
		glog.V(1).Info("Target rollout succeeded, target=%s", target.Name())
	} else {
		glog.V(1).Infof("Target rollout failed, target=%s", target.Name())
	}

	return ok, nil
}

func (sd *SimpleDeployer) performRollbackState() state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		prevVersion := sd.kcd.Status.SuccessVersion

		var err error
		for _, target := range sd.targets {
			glog.V(2).Infof("Performing rollback: target=%s, version=%s", target.Name(), prevVersion)

			err = sd.patchPodSpec(target, prevVersion)
			if err != nil {
				err = errors.Wrapf(err, "failed to pathc podspec while rolling back target=%s, version=%s", target.Name(), prevVersion)
				glog.Errorf("failed to patch pod spec during rollback: %v", err)
			}
		}
		if err != nil {
			return state.Error(err)
		}

		return state.Error(state.NewFailed("deployment failed health state check and was rolled back"))
	}
}
