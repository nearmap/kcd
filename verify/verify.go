package verify

import (
	"fmt"
	"log"
	"time"

	"github.com/nearmap/cvmanager/events"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	"github.com/twinj/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var (
	Failed = errors.New("Verification failed")
)

type Verifier interface {
	Verify() error
}

type BasicVerifier struct {
	cs            kubernetes.Interface
	namespace     string
	eventRecorder events.Recorder
	stats         stats.Stats

	containerImage string
}

// NewBasicVerifier runs a verification action by initializing a pod
// with the given container immage and checking its exit code.
// The Verify action passes if the image has a zero exit code.
func NewBasicVerifier(cs kubernetes.Interface, eventRecorder events.Recorder, stats stats.Stats, namespace, containerImage string) *BasicVerifier {
	return &BasicVerifier{
		cs:             cs,
		namespace:      namespace,
		eventRecorder:  eventRecorder,
		stats:          stats,
		containerImage: containerImage,
	}
}

func (bv *BasicVerifier) Verify() error {
	pod, err := bv.createPod()
	if err != nil {
		return errors.WithStack(err)
	}

	return bv.waitForPod(pod.Name)
}

func (bv *BasicVerifier) createPod() (*corev1.Pod, error) {
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
					Image: bv.containerImage,
				},
			},
		},
	}

	p, err := bv.cs.CoreV1().Pods(bv.namespace).Create(pod)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create pod with name %s", name)
	}

	return p, nil
}

func (bv *BasicVerifier) waitForPod(name string) error {
	client := bv.cs.CoreV1().Pods(bv.namespace)

	// TODO: better timeout
	for i := 0; i < 20; i++ {
		time.Sleep(15 * time.Second)

		pod, err := client.Get(name, metav1.GetOptions{})
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
