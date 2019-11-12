package verify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"
	kcd1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	"github.com/nearmap/kcd/registry"
	"github.com/nearmap/kcd/state"
	"github.com/pkg/errors"
	"github.com/twinj/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	gocorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	// KindImage represents the Image Verifier kind.
	KindImage = "Image"
)

// ImageVerifier is a Verifier implementation that runs a container image that
// tests the verification of a deployment or other image.
type ImageVerifier struct {
	client           gocorev1.PodInterface
	spec             kcd1.VerifySpec
	registryProvider registry.Provider
	next             state.State
}

// NewImageVerifier runs a verification action by initializing a pod
// with the given container immage and checking its exit code.
// The Verify action passes if the image has a zero exit code.
func NewImageVerifier(cs kubernetes.Interface, registryProvider registry.Provider, namespace string,
	spec kcd1.VerifySpec, next state.State) *ImageVerifier {

	client := cs.CoreV1().Pods(namespace)

	return &ImageVerifier{
		client:           client,
		spec:             spec,
		registryProvider: registryProvider,
		next:             next,
	}
}

// Do implements the State interface.
func (iv *ImageVerifier) Do(ctx context.Context) (state.States, error) {
	glog.V(2).Infof("ImageVerifier with spec %+v", iv.spec)

	pod, err := iv.createPod(ctx)
	if err != nil {
		return state.Error(errors.WithStack(err))
	}

	return state.After(15*time.Second, iv.waitForPodState(pod.Name))
}

// createPod creates a pod with the spec's container image and waits
// for it to complete. Returns a Failed error if the pod does completes
// with a non-zero status.
func (iv *ImageVerifier) createPod(ctx context.Context) (*corev1.Pod, error) {
	image, err := iv.getImage(ctx, iv.spec)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get image for spec %v", iv.spec)
	}

	id := uuid.Formatter(uuid.NewV4(), uuid.FormatCanonical)
	name := fmt.Sprintf("kcd-verifier-%s", uuid.Formatter(uuid.NewV4(), uuid.FormatCanonical))

	glog.V(2).Infof("Creating verifier pod with name=%s, image=%s", name, image)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: iv.spec.Annotations,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				corev1.Container{
					Name:  fmt.Sprintf("kcd-verifier-container-%s", id),
					Image: image,
					Env: []corev1.EnvVar{
						corev1.EnvVar{
							Name: "NAMESPACE",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: "metadata.namespace",
								},
							},
						},
					},
				},
			},
		},
	}

	p, err := iv.client.Create(pod)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create pod with name=%s, image=%s", name, image)
	}

	return p, nil
}

func (iv *ImageVerifier) getImage(ctx context.Context, spec kcd1.VerifySpec) (string, error) {
	if spec.Image == "" {
		return "", errors.New("verify spec does not have a valid container image")
	}

	if spec.Tag == "" {
		return spec.Image, nil
	}

	registry, err := iv.registryProvider.RegistryFor(spec.Image)
	if err != nil {
		return "", errors.Wrapf(err, "failed to get registry for %s", spec.Image)
	}

	version, err := registry.Version(ctx, spec.Tag)
	if err != nil {
		return "", errors.Wrapf(err, "failed to get version from registry for %+v", spec)
	}

	parts := strings.SplitN(spec.Image, ":", 2)
	return fmt.Sprintf("%s:%s", parts[0], version), nil
}

func (iv *ImageVerifier) waitForPodState(name string) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		pod, err := iv.client.Get(name, metav1.GetOptions{})
		if err != nil {
			return state.Error(errors.Wrapf(err, "failed to get pod with name %s", name))
		}

		glog.V(4).Infof("Pod %s status: %v", name, pod.Status.Phase)
		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			glog.V(4).Infof("verification pod %s succeeded", name)
			return state.Single(iv.next)
		case corev1.PodFailed:
			glog.V(4).Infof("verification pod %s failed", name)
			return state.Error(ErrFailed)
		}

		return state.After(15*time.Second, iv.waitForPodState(name))
	}
}
