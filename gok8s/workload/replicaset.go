package k8s

import (
	"fmt"
	"strings"

	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	goappsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
)

const (
	TypeReplicaSet = "ReplicaSet"
)

type ReplicaSet struct {
	replicaSet *appsv1.ReplicaSet

	client goappsv1.ReplicaSetInterface
}

func NewReplicaSet(cs kubernetes.Interface, namespace string, replicaSet *appsv1.ReplicaSet) *ReplicaSet {
	client := cs.AppsV1().ReplicaSets(namespace)
	return newReplicaSet(replicaSet, client)
}

func newReplicaSet(replicaSet *appsv1.ReplicaSet, client goappsv1.ReplicaSetInterface) *ReplicaSet {
	return &ReplicaSet{
		replicaSet: replicaSet,
		client:     client,
	}
}

func (rs *ReplicaSet) String() string {
	return fmt.Sprintf("%+v", rs.replicaSet)
}

func (rs *ReplicaSet) Name() string {
	return rs.replicaSet.Name
}

func (rs *ReplicaSet) Type() string {
	return TypeReplicaSet
}

func (rs *ReplicaSet) PodSpec() corev1.PodSpec {
	return rs.replicaSet.Spec.Template.Spec
}

func (rs *ReplicaSet) PodTemplateSpec() corev1.PodTemplateSpec {
	return rs.replicaSet.Spec.Template
}

func (rs *ReplicaSet) PatchPodSpec(cv *cv1.ContainerVersion, container corev1.Container, version string) error {
	_, err := rs.client.Patch(rs.replicaSet.ObjectMeta.Name, types.StrategicMergePatchType,
		[]byte(fmt.Sprintf(podTemplateSpecJSON, container.Name, cv.Spec.ImageRepo, version)))
	if err != nil {
		return errors.Wrapf(err, "failed to patch pod template spec container for ReplicaSet %s", rs.replicaSet.Name)
	}
	return nil
}

func (rs *ReplicaSet) AsResource(cv *cv1.ContainerVersion) *Resource {
	for _, c := range rs.replicaSet.Spec.Template.Spec.Containers {
		if cv.Spec.Container == c.Name {
			return &Resource{
				Namespace: cv.Namespace,
				Name:      rs.replicaSet.Name,
				Type:      TypeReplicaSet,
				Container: c.Name,
				Version:   strings.SplitAfterN(c.Image, ":", 2)[1],
				CV:        cv.Name,
				Tag:       cv.Spec.Tag,
			}
		}
	}

	return nil
}
