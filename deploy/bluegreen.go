package deploy

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"
	kcd1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	"github.com/nearmap/kcd/gok8s/workload"
	"github.com/nearmap/kcd/registry"
	"github.com/nearmap/kcd/state"
	"github.com/nearmap/kcd/verify"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

const (
	// KindServieBlueGreen defines a deployment type that performs a blue-green rollout
	// at the service level.
	KindServieBlueGreen = "ServiceBlueGreen"
)

// InvalidTargetError indicates that a blue green deployment failed because the target workloads
// were not in a valid state.
type InvalidTargetError struct {
	message string
}

// Error implements the Error interface.
func (ite *InvalidTargetError) Error() string {
	return ite.message
}

// BlueGreenDeployer is a Deployer that implements the blue-green rollout strategy at
// the service level.
type BlueGreenDeployer struct {
	cs        kubernetes.Interface
	namespace string

	registryProvider registry.Provider

	kcd       *kcd1.KCD
	blueGreen *kcd1.BlueGreenSpec
	version   string

	// primary is the current live workload (before the rollout) and secondary is the
	// workload that will be updated and made live.
	primary   TemplateRolloutTarget
	secondary TemplateRolloutTarget
}

// NewBlueGreenDeployer returns a Deployer for performing blue-green rollouts.
func NewBlueGreenDeployer(workloadProvider workload.Provider, registryProvider registry.Provider, kcd *kcd1.KCD,
	version string) (*BlueGreenDeployer, error) {

	glog.V(2).Infof("Creating BlueGreenDeployer: namespace=%s, kcd=%s, version=%s",
		workloadProvider.Namespace(), kcd.Name, version)

	if kcd.Spec.Strategy.BlueGreen == nil {
		return nil, errors.Errorf("no blue-green spec provided for kcd resource %s", kcd.Name)
	}
	if kcd.Spec.Strategy.BlueGreen.ServiceName == "" {
		return nil, errors.Errorf("no service defined for blue-green strategy in kcd resource %s", kcd.Name)
	}
	if len(kcd.Spec.Strategy.BlueGreen.LabelNames) == 0 {
		return nil, errors.Errorf("no label names defined for blue-green strategy in kcd resource %s", kcd.Name)
	}

	targets, err := workloadProvider.Workloads(kcd, workload.TypeDeployment)
	if err != nil {
		return nil, errors.Wrapf(err, "blue-green deployer failed to obtain workloads for kcd=%s", kcd.Name)
	}
	if len(targets) != 2 {
		return nil, errors.Errorf("blue-green deployer for %s requires exactly 2 rollout targets, found %d", kcd.Name, len(targets))
	}

	var tTargets []TemplateRolloutTarget
	for _, target := range targets {
		tTarget, ok := target.(TemplateRolloutTarget)
		if !ok {
			glog.Errorf("BlueGreen deployer for %s requires targets of type TemplateRolloutTarget", kcd.Name)
			return nil, &InvalidTargetError{
				message: fmt.Sprintf("blue-green deployer for %s requires targets of type TemplateRolloutTarget", kcd.Name),
			}
		}
		tTargets = append(tTargets, tTarget)
	}

	bgd := &BlueGreenDeployer{
		cs:               workloadProvider.Client(),
		namespace:        workloadProvider.Namespace(),
		registryProvider: registryProvider,
		kcd:              kcd,
		blueGreen:        kcd.Spec.Strategy.BlueGreen,
		version:          version,
	}

	service, err := bgd.getService(kcd.Spec.Strategy.BlueGreen.ServiceName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to find service for kcd spec %s", kcd.Name)
	}

	bgd.primary, bgd.secondary, err = bgd.getBlueGreenTargets(service, tTargets)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return bgd, nil
}

// Workloads implements the Deployer interface.
func (bgd *BlueGreenDeployer) Workloads() []workload.Workload {
	return []workload.Workload{bgd.primary}
}

// AsState implements the Deployer interface.
func (bgd *BlueGreenDeployer) AsState(next state.State) state.State {
	return state.StateFunc(func(ctx context.Context) (state.States, error) {
		glog.V(2).Infof("Beginning blue-green deployment for kcd=%s, version=%s, namespace=%s",
			bgd.kcd.Name, bgd.version, bgd.namespace)

		return state.Single(
			bgd.updateVersion(bgd.secondary,
				bgd.updateVerificationServiceSelector(bgd.secondary,
					bgd.ensureHasPods(bgd.secondary,
						verify.NewVerifiers(bgd.cs, bgd.registryProvider, bgd.namespace, bgd.version, bgd.kcd.Spec.Strategy.Verify,
							bgd.scaleUpSecondary(bgd.primary, bgd.secondary,
								bgd.updateServiceSelector(bgd.blueGreen.ServiceName, bgd.secondary,
									bgd.scaleDown(bgd.primary, next))))))))
	})
}

// getService returns the service with the given name.
func (bgd *BlueGreenDeployer) getService(serviceName string) (*corev1.Service, error) {
	service, err := bgd.cs.CoreV1().Services(bgd.namespace).Get(serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get service with name %s", serviceName)
	}

	return service, nil
}

// getBlueGreenTargets returns the primary and secondary rollout targets based on whether
// the live service (as specified) is currently selecting them.
func (bgd *BlueGreenDeployer) getBlueGreenTargets(service *corev1.Service,
	targets []TemplateRolloutTarget) (primary, secondary TemplateRolloutTarget, err error) {

	selector := labels.Set(service.Spec.Selector).AsSelector()
	for _, target := range targets {
		ptLabels := labels.Set(target.PodTemplateSpec().Labels)
		if selector.Matches(ptLabels) {
			if primary != nil {
				return nil, nil, errors.Errorf("unexpected state: found 2 primary blue-green workloads for kcd spec %s", bgd.kcd.Name)
			}
			primary = target
		} else {
			if secondary != nil {
				return nil, nil, errors.Errorf("unexpected state: found 2 secondary blue-green workloads for kcd spec %s", bgd.kcd.Name)
			}
			secondary = target
		}
	}

	return primary, secondary, nil
}

// updateVersion patches the container version of the given rollout target.
func (bgd *BlueGreenDeployer) updateVersion(target TemplateRolloutTarget, next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		glog.V(1).Infof("Updating version of %s to %s", target.Name(), bgd.version)

		podSpec := target.PodSpec()

		retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			for _, c := range podSpec.Containers {
				if c.Name == bgd.kcd.Spec.Container.Name {
					if updateErr := target.PatchPodSpec(bgd.kcd, c, bgd.version); updateErr != nil {
						glog.V(2).Infof("Failed to update container version (will retry): version=%v, target=%v, error=%v",
							bgd.version, target.Name(), updateErr)
						return updateErr
					}
				}
			}
			return nil
		})
		if retryErr != nil {
			return state.Error(errors.Wrapf(retryErr, "failed to patch pod spec for target %s", target.Name()))
		}

		glog.V(4).Infof("Successfully updated version of %s to %s", target.Name(), bgd.version)

		return state.Single(next)
	}
}

// updateVerificationServiceSelector updates the verification service defined in the KCD
// to point to the given rollout target.
func (bgd *BlueGreenDeployer) updateVerificationServiceSelector(target TemplateRolloutTarget, next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		if bgd.kcd.Spec.Strategy.BlueGreen.VerificationServiceName == "" {
			glog.V(1).Infof("No test service defined for kcd spec %s", bgd.kcd.Name)
			return state.Single(next)
		}

		return state.Single(bgd.updateServiceSelector(bgd.blueGreen.VerificationServiceName, target, next))
	}
}

// updateServiceSelector updates the selector of the service with the given name to point to
// the current rollout target, based on the label names defined in the KCD.
func (bgd *BlueGreenDeployer) updateServiceSelector(serviceName string, target TemplateRolloutTarget,
	next state.State) state.StateFunc {

	return func(ctx context.Context) (state.States, error) {
		labelNames := bgd.kcd.Spec.Strategy.BlueGreen.LabelNames

		service, err := bgd.getService(serviceName)
		if err != nil {
			return state.Error(errors.Wrapf(err, "failed to find test service for kcd spec %s", bgd.kcd.Name))
		}

		for _, labelName := range labelNames {
			targetLabel, has := target.PodTemplateSpec().Labels[labelName]
			if !has {
				return state.Error(errors.Errorf("pod template spec for target %s is missing label name %s in kcd spec %s",
					target.Name(), labelName, bgd.kcd.Name))
			}

			service.Spec.Selector[labelName] = targetLabel
		}

		glog.V(2).Infof("Updating service %s with selectors %v", serviceName, service.Spec.Selector)

		// TODO: is update appropriate?
		if _, err := bgd.cs.CoreV1().Services(bgd.namespace).Update(service); err != nil {
			return state.Error(errors.Wrapf(err, "failed to update test service %s while processing blue-green deployment for %s",
				service.Name, bgd.kcd.Name))
		}

		return state.Single(next)
	}
}

// ensureHasPods will set the target's number of replicas to a positive value
// if it currently has none.
func (bgd *BlueGreenDeployer) ensureHasPods(target TemplateRolloutTarget, next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		// ensure at least 1 pod
		numReplicas, err := target.NumReplicas()
		if err != nil {
			return state.Error(errors.Wrapf(err, "failed to get num replicas for target %s", target.Name()))
		}

		glog.V(2).Infof("Target %s has %d replicas", target.Name(), numReplicas)

		if numReplicas == 0 {
			glog.V(1).Infof("Increasing replicas of %s to 1", target.Name())

			err := target.PatchNumReplicas(1)
			if err != nil {
				return state.Error(errors.Wrapf(err, "failed to patch number of replicas for target %s", target.Name()))
			}
		}

		return state.Single(bgd.waitForAllPods(target, next))
	}
}

// waitForAllPods checks that all pods tied to the given TemplateDeploySpec are at the specified
// version, and starts polling if not the case.
func (bgd *BlueGreenDeployer) waitForAllPods(target TemplateRolloutTarget, next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		ok, err := bgd.checkPods(target, 1)
		if err != nil {
			return state.Error(errors.Wrapf(err, "failed to check pods in waitForAllPods for %s", target.Name()))
		}
		if ok {
			return state.Single(next)
		}
		return state.After(15*time.Second, bgd.waitForAllPods(target, next))
	}
}

// checkPods checks whether the target has at least num pods and that every pod
// is the current version (as defined by this deployer) and that every container
// within each pod is in a ready state.
func (bgd *BlueGreenDeployer) checkPods(target TemplateRolloutTarget, num int32) (bool, error) {
	pods, err := PodsForTarget(bgd.cs, bgd.namespace, target)
	if err != nil {
		return false, errors.Wrapf(err, "failed to get pods for target %s", target.Name())
	}
	if len(pods) < int(num) {
		glog.V(2).Infof("insufficient pods found for target %s: found %d but need %d", target.Name(), len(pods), num)
		return false, nil
	}

	for _, pod := range pods {
		if pod.Status.Phase != corev1.PodRunning {
			glog.V(2).Infof("Still waiting for rollout: pod %s phase is %v", pod.Name, pod.Status.Phase)
			return false, nil
		}

		for _, cs := range pod.Status.ContainerStatuses {
			if !cs.Ready {
				glog.V(2).Infof("Still waiting for rollout: pod=%s, container=%s is not ready", pod.Name, cs.Name)
				return false, nil
			}
		}

		ok, err := workload.CheckPodSpecVersion(pod.Spec, bgd.kcd, bgd.version)
		if err != nil {
			return false, errors.Wrapf(err, "failed to check container version for target %s", target.Name())
		}
		if !ok {
			glog.V(2).Infof("Still waiting for rollout: pod %s is wrong version", pod.Name)
			return false, nil
		}
	}

	glog.V(2).Info("All pods and containers are ready")

	return true, nil
}

// scaleUpSecondary scales up the secondary deployment to be the same as the primary.
// This should be done before cutting the service over to the secondary to ensure there
// is sufficient capacity.
func (bgd *BlueGreenDeployer) scaleUpSecondary(primary, secondary TemplateRolloutTarget, next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		primaryNum, err := primary.NumReplicas()
		if err != nil {
			return state.Error(errors.Wrapf(err, "failed to obtain ready replicas for workload %s", primary.Name()))
		}

		secondaryNum, err := secondary.NumReplicas()
		if err != nil {
			return state.Error(errors.Wrapf(err, "failed to obtain ready replicas for workload %s", secondary.Name()))
		}

		if secondaryNum < primaryNum {
			glog.V(1).Infof("Scaling up secondary pods to %d to match current primary", primaryNum)
			if err := secondary.PatchNumReplicas(primaryNum); err != nil {
				return state.Error(errors.Wrapf(err, "failed to patch number of replicas for secondary spec %s", secondary.Name()))
			}
		}

		ok, err := bgd.checkPods(secondary, primaryNum)
		if err != nil {
			return state.Error(errors.Wrapf(err, "failed to check pods while scaling up sceondary %s", secondary.Name()))
		}

		if ok {
			return state.Single(next)
		}
		return state.After(15*time.Second, bgd.scaleUpSecondary(primary, secondary, next))
	}
}

// scaleDown scales the rollout target down to zero replicas.
func (bgd *BlueGreenDeployer) scaleDown(target TemplateRolloutTarget, next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		if !bgd.kcd.Spec.Strategy.BlueGreen.ScaleDown {
			return state.Single(next)
		}

		if err := target.PatchNumReplicas(0); err != nil {
			return state.Error(errors.WithStack(err))
		}
		return state.Single(next)
	}
}

// PodsForTarget returns the pods managed by the given rollout target.
func PodsForTarget(cs kubernetes.Interface, namespace string, target TemplateRolloutTarget) ([]corev1.Pod, error) {
	set := labels.Set(target.PodTemplateSpec().Labels)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}

	pods, err := cs.CoreV1().Pods(namespace).List(listOpts)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to select pods for target %s", target.Name())
	}

	result, err := target.SelectOwnPods(pods.Items)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to filter pods for target %s", target.Name())
	}

	return result, nil
}
