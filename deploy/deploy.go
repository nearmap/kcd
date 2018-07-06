package deploy

import (
	"github.com/golang/glog"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	k8s "github.com/nearmap/cvmanager/gok8s/workload"
	"github.com/nearmap/cvmanager/registry"
	"github.com/nearmap/cvmanager/state"
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
	Deploy(cv *cv1.ContainerVersion, version string, spec RolloutTarget) error
}

// NewDeployState returns a state that performs a deployment operation according to the
// ContainerVersion spec.
func NewDeployState(cs kubernetes.Interface, registryProvider registry.Provider, namespace string, cv *cv1.ContainerVersion,
	version string, target RolloutTarget, next state.State) state.State {

	glog.V(2).Infof("Creating deployment for cv=%+v, version=%s, rolloutTarget=%s", cv, version, target.Name())

	var kind string
	if cv.Spec.Strategy != nil {
		kind = cv.Spec.Strategy.Kind
	}

	switch kind {
	case KindServieBlueGreen:
		return NewBlueGreenDeployer(cs, registryProvider, namespace, cv, version, target, next)
	default:
		return NewSimpleDeployer(cv, version, target, next)
	}
}
