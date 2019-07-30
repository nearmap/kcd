package workload

import (
	"fmt"
	"time"

	kcd1 "github.com/eric1313/kcd/gok8s/apis/custom/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	gocorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	podTemplateSpecJSON = `
						{
							"spec": {
								"template": {
									"spec": {
										"containers": [
												{
													"name":  "%s",
													"image": "%s:%s"
												}
											]
										}
									}
								}
							}
						}
						`
)

const (
	TypePod = "Pod"
)

type Pod struct {
	pod *corev1.Pod

	client gocorev1.PodInterface
}

func NewPod(cs kubernetes.Interface, namespace string, pod *corev1.Pod) *Pod {
	client := cs.CoreV1().Pods(namespace)
	return newPod(pod, client)
}

func newPod(pod *corev1.Pod, client gocorev1.PodInterface) *Pod {
	return &Pod{
		pod:    pod,
		client: client,
	}
}

func (p *Pod) String() string {
	return fmt.Sprintf("%+v", p.pod)
}

// Name implements the Workload interface.
func (p *Pod) Name() string {
	return p.pod.Name
}

// Namespace implements the Workload interface.
func (p *Pod) Namespace() string {
	return p.pod.Namespace
}

// Type implements the Workload interface.
func (p *Pod) Type() string {
	return TypePod
}

// PodSpec implements the Workload interface.
func (p *Pod) PodSpec() corev1.PodSpec {
	return p.pod.Spec
}

// RollbackAfter implements the Workload interface.
func (p *Pod) RollbackAfter() *time.Duration {
	return nil
}

// ProgressHealth implements the Workload interface.
func (p *Pod) ProgressHealth(startTime time.Time) (*bool, error) {
	result := true
	return &result, nil
}

// RolloutFailed implements the Workload interface.
func (p *Pod) RolloutFailed(rolloutTime time.Time) (bool, error) {
	return false, nil
}

// PodSelector implements the Workload interface.
func (p *Pod) PodSelector() string {
	set := labels.Set(p.pod.Labels)
	return set.AsSelector().String()
}

// PatchPodSpec implements the Workload interface.
func (p *Pod) PatchPodSpec(kcd *kcd1.KCD, container corev1.Container, version string) error {
	_, err := p.client.Patch(p.pod.ObjectMeta.Name, types.StrategicMergePatchType,
		[]byte(fmt.Sprintf(podTemplateSpecJSON, container.Name, kcd.Spec.ImageRepo, version)))
	if err != nil {
		return errors.Wrapf(err, "failed to patch pod template spec container for Pod %s", p.pod.Name)
	}
	return nil
}
