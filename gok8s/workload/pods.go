package k8s

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/nearmap/kcd/events"
	kcd1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
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

//ProgressHealth implements the Workload interface.
func (p *Pod) ProgressHealth(startTime time.Time) (*bool, error) {
	result := true
	return &result, nil
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

// raiseSyncPodErrEvents raises k8s and stats events indicating sync failure
func (k *Provider) raiseSyncPodErrEvents(err error, typ, name, tag, version string) {
	glog.Errorf("Failed sync %s with image: digest=%v, tag=%v, err=%v", typ, version, tag, err)
	k.options.Stats.Event(fmt.Sprintf("%s.sync.failure", name),
		fmt.Sprintf("Failed to sync pod spec with %s", version), "", "error",
		time.Now().UTC())
	k.options.Recorder.Event(events.Warning, "KCDSyncFailed", fmt.Sprintf("Error syncing %s name:%s", typ, name))
}

// CheckPodSpecKCDs tests whether all containers in the pod spec with container
// names that match the kcd spec have the given version.
// Returns false if at least one container's version does not match.
func CheckPodSpecKCDs(kcd *kcd1.KCD, version string, podSpec corev1.PodSpec) (bool, error) {
	match := false
	for _, c := range podSpec.Containers {
		if c.Name == kcd.Spec.Container.Name {
			match = true
			parts := strings.SplitN(c.Image, ":", 2)
			if len(parts) > 2 {
				return false, errors.New("invalid image on container")
			}
			if parts[0] != kcd.Spec.ImageRepo {
				return false, errors.Errorf("Repository mismatch for container %s: %s and requested %s don't match",
					kcd.Spec.Container.Name, parts[0], kcd.Spec.ImageRepo)
			}
			if version != parts[1] {
				return false, nil
			}
		}
	}

	if !match {
		return false, errors.Errorf("no container of name %s was found in workload", kcd.Spec.Container.Name)
	}

	return true, nil
}
