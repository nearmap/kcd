package workload

import (
	"fmt"
	"time"

	kcd1 "github.com/eric1313/kcd/gok8s/apis/custom/v1"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	goappsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
)

const (
	TypeStatefulSet = "StatefulSet"
)

type StatefulSet struct {
	statefulSet *appsv1.StatefulSet

	client goappsv1.StatefulSetInterface
}

func NewStatefulSet(cs kubernetes.Interface, namespace string, statefulSet *appsv1.StatefulSet) *StatefulSet {
	client := cs.AppsV1().StatefulSets(namespace)
	return newStatefulSet(statefulSet, client)
}

func newStatefulSet(statefulSet *appsv1.StatefulSet, client goappsv1.StatefulSetInterface) *StatefulSet {
	return &StatefulSet{
		statefulSet: statefulSet,
		client:      client,
	}
}

func (ss *StatefulSet) String() string {
	return fmt.Sprintf("%+v", ss.statefulSet)
}

// Name implements the Workload interface.
func (ss *StatefulSet) Name() string {
	return ss.statefulSet.Name
}

// Namespace implements the Workload interface.
func (ss *StatefulSet) Namespace() string {
	return ss.statefulSet.Namespace
}

// Type implements the Workload interface.
func (ss *StatefulSet) Type() string {
	return TypeStatefulSet
}

// PodSpec implements the Workload interface.
func (ss *StatefulSet) PodSpec() corev1.PodSpec {
	return ss.statefulSet.Spec.Template.Spec
}

// RollbackAfter implements the Workload interface.
func (ss *StatefulSet) RollbackAfter() *time.Duration {
	return nil
}

// ProgressHealth implements the Workload interface.
func (ss *StatefulSet) ProgressHealth(startTime time.Time) (*bool, error) {
	result := true
	return &result, nil
}

// RolloutFailed implements the Workload interface.
func (ss *StatefulSet) RolloutFailed(rolloutTime time.Time) (bool, error) {
	return false, nil
}

// PodSelector implements the Workload interface.
func (ss *StatefulSet) PodSelector() string {
	set := labels.Set(ss.statefulSet.Spec.Template.Labels)
	return set.AsSelector().String()
}

// PodTemplateSpec implements the TemplateRolloutTarget interface.
func (ss *StatefulSet) PodTemplateSpec() corev1.PodTemplateSpec {
	return ss.statefulSet.Spec.Template
}

// PatchPodSpec implements the Workload interface.
func (ss *StatefulSet) PatchPodSpec(kcd *kcd1.KCD, container corev1.Container, version string) error {
	_, err := ss.client.Patch(ss.statefulSet.ObjectMeta.Name, types.StrategicMergePatchType,
		[]byte(fmt.Sprintf(podTemplateSpecJSON, container.Name, kcd.Spec.ImageRepo, version)))
	if err != nil {
		return errors.Wrapf(err, "failed to patch pod template spec container for StatefulSet %s", ss.statefulSet.Name)
	}
	return nil
}
