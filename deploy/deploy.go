package deploy

import (
	"github.com/golang/glog"
	kcd1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	"github.com/nearmap/kcd/gok8s/workload"
	k8s "github.com/nearmap/kcd/gok8s/workload"
	"github.com/nearmap/kcd/registry"
	"github.com/nearmap/kcd/state"
)

// RolloutTarget defines an interface for something deployable, such as a Deployment, DaemonSet, Pod, etc.
type RolloutTarget = k8s.Workload

// TemplateRolloutTarget defines methods for deployable resources that manage a collection
// of pods via a pod template. More deployment options are available for such
// resources.
type TemplateRolloutTarget = k8s.TemplateWorkload

// Deployer is an interface for rollout strategies.
type Deployer interface {
	// Workloads returns all the workload instances that are the target for this deployer.
	Workloads() []workload.Workload

	// AsState returns the deployment workflow state.
	AsState(next state.State) state.State
}

// SupportsRollback is implemented by deployers that support a Rollback mechanism.
type SupportsRollback interface {
	// Rollback performs a rollback of the deployment to the given previous version.
	// Rollback always calls next, even on failure since it is assumed we are already
	// in a failure date.
	Rollback(prevVersion string, next state.State) state.State
}

// New returns a Deployer instance based on the "kind" of the kcd resource.
func New(workloadProvider workload.Provider, registryProvider registry.Provider, kcd *kcd1.KCD, version string) (Deployer, error) {
	if glog.V(2) {
		glog.V(2).Infof("Creating deployment for kcd=%+v, version=%s", kcd, version)
	}

	switch kcd.Spec.Strategy.Kind {
	case KindServieBlueGreen:
		return NewBlueGreenDeployer(workloadProvider, registryProvider, kcd, version)
	default:
		return NewSimpleDeployer(workloadProvider, registryProvider, kcd, version)
	}
}
