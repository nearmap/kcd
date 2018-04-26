package k8s

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/registry/errs"
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

func (p *Pod) Name() string {
	return p.pod.Name
}

func (p *Pod) Type() string {
	return TypePod
}

func (p *Pod) PodSpec() corev1.PodSpec {
	return p.pod.Spec
}

func (p *Pod) PatchPodSpec(cv *cv1.ContainerVersion, container corev1.Container, version string) error {
	_, err := p.client.Patch(p.pod.ObjectMeta.Name, types.StrategicMergePatchType,
		[]byte(fmt.Sprintf(podTemplateSpecJSON, container.Name, cv.Spec.ImageRepo, version)))
	if err != nil {
		return errors.Wrapf(err, "failed to patch pod template spec container for Pod %s", p.pod.Name)
	}
	return nil
}

func (p *Pod) AsResource(cv *cv1.ContainerVersion) *Resource {
	for _, c := range p.pod.Spec.Containers {
		if cv.Spec.Container == c.Name {
			return &Resource{
				Namespace: cv.Namespace,
				Name:      p.pod.Name,
				Type:      TypePod,
				Container: c.Name,
				Version:   strings.SplitAfterN(c.Image, ":", 2)[1],
				CV:        cv.Name,
				Tag:       cv.Spec.Tag,
			}
		}
	}

	return nil
}

// checkPodSpec checks whether the current version tag of the container
// in the given pod spec with the given container name has the given
// version. Return a nil error if the versions match. If not matching,
// an errs.ErrorVersionMismatch is returned.
func checkPodSpec(cv *cv1.ContainerVersion, version string, podSpec corev1.PodSpec) error {
	match := false
	for _, c := range podSpec.Containers {
		if c.Name == cv.Spec.Container {
			match = true
			parts := strings.SplitN(c.Image, ":", 2)
			if len(parts) > 2 {
				return errors.New("invalid image on container")
			}
			if parts[0] != cv.Spec.ImageRepo {
				return errors.Errorf("ECR repo mismatch present %s and requested  %s don't match",
					parts[0], cv.Spec.ImageRepo)
			}
			if version != parts[1] {
				if validate(version) != nil {
					return errors.Errorf("failed to validate image with tag %s", version)
				}
				return errs.ErrVersionMismatch
			}
		}
	}

	if !match {
		return errors.Errorf("no container of name %s was found in workload", cv.Spec.Container)
	}

	return nil
}

// raiseSyncPodErrEvents raises k8s and stats events indicating sync failure
func (k *K8sProvider) raiseSyncPodErrEvents(err error, typ, name, tag, version string) {
	log.Printf("Failed sync %s with image: digest=%v, tag=%v, err=%v", typ, version, tag, err)
	k.stats.Event(fmt.Sprintf("%s.sync.failure", name),
		fmt.Sprintf("Failed to sync pod spec with %s", version), "", "error",
		time.Now().UTC())
	k.Recorder.Event(events.Warning, "CRSyncFailed", fmt.Sprintf("Error syncing %s name:%s", typ, name))
}
