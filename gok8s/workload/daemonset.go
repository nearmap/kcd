package workload

import (
	"fmt"
	"time"

	kcd1 "github.com/wish/kcd/gok8s/apis/custom/v1"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	goappsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
)

const (
	TypeDaemonSet = "DaemonSet"
)

type DaemonSet struct {
	daemonSet *appsv1.DaemonSet

	client goappsv1.DaemonSetInterface
}

func NewDaemonSet(cs kubernetes.Interface, namespace string, daemonSet *appsv1.DaemonSet) *DaemonSet {
	client := cs.AppsV1().DaemonSets(namespace)
	return newDaemonSet(daemonSet, client)
}

func newDaemonSet(daemonSet *appsv1.DaemonSet, client goappsv1.DaemonSetInterface) *DaemonSet {
	return &DaemonSet{
		daemonSet: daemonSet,
		client:    client,
	}
}

func (ds *DaemonSet) String() string {
	return fmt.Sprintf("%+v", ds.daemonSet)
}

// Name implements the Workload interface.
func (ds *DaemonSet) Name() string {
	return ds.daemonSet.Name
}

// Namespace implements the Workload interface.
func (ds *DaemonSet) Namespace() string {
	return ds.daemonSet.Namespace
}

// Type implements the Workload interface.
func (ds *DaemonSet) Type() string {
	return TypeDaemonSet
}

// PodSpec implements the Workload interface.
func (ds *DaemonSet) PodSpec() corev1.PodSpec {
	return ds.daemonSet.Spec.Template.Spec
}

// RollbackAfter implements the Workload interface.
func (ds *DaemonSet) RollbackAfter() *time.Duration {
	return nil
}

// ProgressHealth implements the Workload interface.
func (ds *DaemonSet) ProgressHealth(startTime time.Time) (*bool, error) {
	result := true
	return &result, nil
}

// RolloutFailed implements the Workload interface.
func (ds *DaemonSet) RolloutFailed(rolloutTime time.Time) (bool, error) {
	return false, nil
}

// PodSelector implements the Workload interface.
func (ds *DaemonSet) PodSelector() string {
	set := labels.Set(ds.daemonSet.Spec.Template.Labels)
	return set.AsSelector().String()
}

// PodTemplateSpec implements the TemplateRolloutTarget interface.
func (ds *DaemonSet) PodTemplateSpec() corev1.PodTemplateSpec {
	return ds.daemonSet.Spec.Template
}

// PatchPodSpec implements the Workload interface.
func (ds *DaemonSet) PatchPodSpec(kcd *kcd1.KCD, container corev1.Container, version string) error {
	_, err := ds.client.Patch(ds.daemonSet.ObjectMeta.Name, types.StrategicMergePatchType,
		[]byte(fmt.Sprintf(podTemplateSpecJSON, container.Name, kcd.Spec.ImageRepo, version)))
	if err != nil {
		return errors.Wrapf(err, "failed to patch pod template spec container for DaemonSet %s", ds.daemonSet.Name)
	}
	return nil
}
