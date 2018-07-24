package deploy

import (
	"github.com/golang/glog"
	kcd1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	k8s "github.com/nearmap/kcd/gok8s/workload"
	"github.com/nearmap/kcd/registry"
	"github.com/nearmap/kcd/state"
	"k8s.io/client-go/kubernetes"
)

// RolloutTarget defines an interface for something deployable, such as a Deployment, DaemonSet, Pod, etc.
type RolloutTarget = k8s.Workload

// TemplateRolloutTarget defines methods for deployable resources that manage a collection
// of pods via a pod template. More deployment options are available for such
// resources.
type TemplateRolloutTarget = k8s.TemplateWorkload

// Deployer is an interface for rollout strategies.
type Deployer interface {
	// Deploy initiates a rollout for a target spec based on the underlying strategy implementation.
	Deploy(kcd *kcd1.KCD, version string, spec RolloutTarget) error
}

// NewDeployState returns a state that performs a deployment operation according to the
// KCD spec.
func NewDeployState(cs kubernetes.Interface, registryProvider registry.Provider, namespace string, kcd *kcd1.KCD,
	version string, target RolloutTarget, next state.State) state.State {

	glog.V(2).Infof("Creating deployment for kcd=%+v, version=%s, rolloutTarget=%s", kcd, version, target.Name())

	var kind string
	if kcd.Spec.Strategy != nil {
		kind = kcd.Spec.Strategy.Kind
	}

	switch kind {
	case KindServieBlueGreen:
		return NewBlueGreenDeployer(cs, registryProvider, namespace, kcd, version, target, next)
	default:
		return NewSimpleDeployer(kcd, version, target, next)
	}
}
