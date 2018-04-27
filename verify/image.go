package verify

import (
	"fmt"
	"log"
	"time"

	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	"github.com/twinj/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	gocorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	TypeImage = "image"
)

type ImageVerifier struct {
	client gocorev1.PodInterface

	eventRecorder events.Recorder
	stats         stats.Stats

	spec *cv1.VerifySpec
}

// NewImageVerifier runs a verification action by initializing a pod
// with the given container immage and checking its exit code.
// The Verify action passes if the image has a zero exit code.
func NewImageVerifier(cs kubernetes.Interface, eventRecorder events.Recorder, stats stats.Stats, namespace string,
	spec *cv1.VerifySpec) *ImageVerifier {

	client := cs.CoreV1().Pods(namespace)

	return &ImageVerifier{
		client:        client,
		eventRecorder: eventRecorder,
		stats:         stats,
		spec:          spec,
	}
}

// Verify implements the Verifier interface.
func (iv *ImageVerifier) Verify() error {
	pod, err := iv.createPod()
	if err != nil {
		return errors.WithStack(err)
	}
	defer iv.client.Delete(pod.Name, &metav1.DeleteOptions{})

	return iv.waitForPod(pod.Name)
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

	log.Printf("Creating verifier pod with name %s", name)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.PodSpec{
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

func (iv *ImageVerifier) waitForPod(name string) error {
	defer timeTrack(time.Now(), "verification waitForPod")
	timeout := time.Minute * 5
	if iv.spec.TimeoutSeconds > 0 {
		timeout = time.Second * time.Duration(iv.spec.TimeoutSeconds)
	}
	start := time.Now()

	for {
		if time.Now().After(start.Add(timeout)) {
			break
		}
		time.Sleep(15 * time.Second)

		pod, err := iv.client.Get(name, metav1.GetOptions{})
		if err != nil {
			log.Printf("failed to get pod with name=%s: %v", name, err)
			continue
		}

		log.Printf("Pod %s status: %v", name, pod.Status.Phase)
		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			log.Printf("verification pod %s succeeded", name)
			return nil
		case corev1.PodFailed:
			log.Printf("verification pod %s failed", name)
			return Failed
		}
	}

	log.Printf("Verification timed out")
	return Failed
}

func timeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	log.Printf("%s took %s", name, elapsed)
}
