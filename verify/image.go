package verify

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/state"
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
	client gocorev1.PodInterface
	spec   cv1.VerifySpec
	next   state.State
}

// NewImageVerifier runs a verification action by initializing a pod
// with the given container immage and checking its exit code.
// The Verify action passes if the image has a zero exit code.
func NewImageVerifier(cs kubernetes.Interface, namespace string, spec cv1.VerifySpec, next state.State) *ImageVerifier {
	client := cs.CoreV1().Pods(namespace)

	return &ImageVerifier{
		client: client,
		spec:   spec,
		next:   next,
	}
}

// Do implements the State interface.
func (iv *ImageVerifier) Do(ctx context.Context) (state.States, error) {
	glog.V(2).Info("ImageVerifier with spec %+v", iv.spec)

	pod, err := iv.createPod()
	if err != nil {
		return state.Error(errors.WithStack(err))
	}

	return state.After(15*time.Second, iv.waitForPodState(pod.Name))
}

// createPod creates a pod with the spec's container image and waits
// for it to complete. Returns a Failed error if the pod does completes
// with a non-zero status.
func (iv *ImageVerifier) createPod() (*corev1.Pod, error) {
	if iv.spec.Image == "" {
		return nil, errors.New("verify spec does not have a valid container image")
	}

	id := uuid.Formatter(uuid.NewV4(), uuid.FormatCanonical)
	name := fmt.Sprintf("cv-verifier-%s", uuid.Formatter(uuid.NewV4(), uuid.FormatCanonical))

	glog.V(2).Info("Creating verifier pod with name %s", name)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				corev1.Container{
					Name:  fmt.Sprintf("cv-verifier-container-%s", id),
					Image: iv.spec.Image,
				},
			},
		},
	}

	p, err := iv.client.Create(pod)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create pod with name %s", name)
	}

	return p, nil
}

func (iv *ImageVerifier) waitForPodState(name string) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		pod, err := iv.client.Get(name, metav1.GetOptions{})
		if err != nil {
			return state.Error(errors.Wrapf(err, "failed to get pod with name %s", name))
		}

		glog.V(4).Info("Pod %s status: %v", name, pod.Status.Phase)
		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			glog.V(4).Info("verification pod %s succeeded", name)
			return state.Single(iv.next)
		case corev1.PodFailed:
			glog.V(4).Info("verification pod %s failed", name)
			return state.Error(ErrFailed)
		}

		return state.After(15*time.Second, iv.waitForPodState(name))
	}
}
