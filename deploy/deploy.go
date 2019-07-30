package deploy

import (
	"github.com/golang/glog"
	kcd1 "github.com/eric1313/kcd/gok8s/apis/custom/v1"
	"github.com/eric1313/kcd/gok8s/workload"
	k8s "github.com/eric1313/kcd/gok8s/workload"
	"github.com/eric1313/kcd/registry"
	"github.com/eric1313/kcd/state"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// ActivePodsForTarget returns the pods managed by the given rollout target.
func ActivePodsForTarget(cs kubernetes.Interface, namespace string, target RolloutTarget) ([]corev1.Pod, error) {
	listOpts := metav1.ListOptions{
		LabelSelector: target.PodSelector(),
	}

	podList, err := cs.CoreV1().Pods(namespace).List(listOpts)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to select pods for target %s", target.Name())
	}

	var pods = make([]corev1.Pod, 0, len(podList.Items))
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			continue
		}
		pods = append(pods, pod)
	}

	return pods, nil
}

// CheckPods checks whether the target has at least num pods and that every pod has the
// specified version and that every kcd managed container (defined by the kcd resource)
// within each pod is in a ready state.
func CheckPods(cs kubernetes.Interface, namespace string, target RolloutTarget, num int32, kcd *kcd1.KCD, version string) (bool, error) {
	pods, err := ActivePodsForTarget(cs, namespace, target)
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

		ok, err := workload.CheckPodSpecVersion(pod.Spec, kcd, version)
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
