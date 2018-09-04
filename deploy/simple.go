package deploy

import (
	"context"
	"time"

	"github.com/golang/glog"
	kcd1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	"github.com/nearmap/kcd/gok8s/workload"
	"github.com/nearmap/kcd/registry"
	"github.com/nearmap/kcd/state"
	"github.com/nearmap/kcd/verify"
	"github.com/pkg/errors"
	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

// SimpleDeployer implements a rollout strategy by patching the target's pod spec with a new version.
type SimpleDeployer struct {
	cs        kubernetes.Interface
	namespace string

	registryProvider registry.Provider

	kcd     *kcd1.KCD
	version string
	targets []RolloutTarget
}

// NewSimpleDeployer returns a new SimpleDeployer instance, which triggers rollouts
// by patching the target's pod spec with a new version and using the default
// Kubernetes deployment strategy for the workload.
func NewSimpleDeployer(workloadProvider workload.Provider, registryProvider registry.Provider,
	kcd *kcd1.KCD, version string) (*SimpleDeployer, error) {

	if glog.V(2) {
		glog.V(2).Infof("Creating SimpleDeployer: kcd=%s, version=%s", kcd.Name, version)
	}

	workloads, err := workloadProvider.Workloads(kcd)
	if err != nil {
		return nil, errors.Wrap(err, "failed to obtain workloads for simple deployer")
	}
	if len(workloads) == 0 {
		return nil, errors.New("simple deployer found no workloads found to process")
	}

	return &SimpleDeployer{
		cs:               workloadProvider.Client(),
		namespace:        workloadProvider.Namespace(),
		registryProvider: registryProvider,
		kcd:              kcd,
		version:          version,
		targets:          workloads,
	}, nil
}

// Workloads implements the Deployer interface.
func (sd *SimpleDeployer) Workloads() []workload.Workload {
	return sd.targets
}

// AsState implements the Deployer interface.
func (sd *SimpleDeployer) AsState(next state.State) state.State {
	return state.StateFunc(func(ctx context.Context) (state.States, error) {
		for _, target := range sd.targets {
			glog.V(2).Infof("Performing simple deployment: target=%s, version=%s", target.Name(), sd.version)

			err := sd.patchPodSpec(target, sd.version)
			if err != nil {
				return state.Error(errors.WithStack(err))
			}
		}

		return state.Single(
			sd.checkRolloutState(
				verify.NewVerifiers(sd.cs, sd.registryProvider, sd.namespace, sd.version, sd.kcd.Spec.Strategy.Verify,
					next)))
	})
}

// patchPodSpec patches the rollout target's pod spec with the given version.
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

// checkRolloutState checks whether the state of each target workload successfully deploys,
// initiating a rollback if not and the kcd spec indicates that rollback is enabled.
// If rollback is not enabled then the deployment goes into a failed state.
func (sd *SimpleDeployer) checkRolloutState(next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		for _, target := range sd.targets {
			if glog.V(2) {
				glog.V(2).Infof("Checking rollout state: target=%s, version=%s", target.Name(), sd.version)
			}

			ok, err := sd.checkRollout(target)
			if err != nil {
				return state.Error(errors.WithStack(err))
			}
			if ok == nil {
				return state.After(time.Second*15, sd.checkRolloutState(next))
			}
			if *ok {
				continue
			}

			return state.Error(state.NewFailed("Rollout state was not ok for target=%s, version=%s", target.Name(), sd.version))
		}

		glog.V(1).Infof("All rollouts succeeded for kcd=%s, version=%s", sd.kcd.Name, sd.version)
		return state.Single(next)
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

// Rollback rolls the version of the workload back to its previous state.
func (sd *SimpleDeployer) Rollback(prevVersion string, next state.State) state.State {
	return state.StateFunc(func(ctx context.Context) (state.States, error) {
		var firstErr error
		for _, target := range sd.targets {
			glog.V(2).Infof("Performing rollback: target=%s, version=%s", target.Name(), prevVersion)

			err := sd.patchPodSpec(target, prevVersion)
			if err != nil {
				err = errors.Wrapf(err, "failed to patch podspec while rolling back target=%s, version=%s", target.Name(), prevVersion)
				glog.Errorf("failed to patch pod spec during rollback: %v", err)
				if firstErr == nil {
					firstErr = err
				}
			}
		}

		if firstErr != nil {
			glog.Error("Failed to rollback at least one workload: %v", firstErr)
			return state.Error(firstErr)
		}

		return state.Single(next)
	})
}
