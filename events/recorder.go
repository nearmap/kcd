package events

import (
	"log"
	"os"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

const (
	Normal  = corev1.EventTypeNormal
	Warning = corev1.EventTypeWarning
)

// Recorder records Kubernetes events for the current pod.
type Recorder interface {
	// Event constructs an event from the given information and puts it in the queue for sending.
	Event(eventType, reason, message string)

	// Eventf is just like Event, but with Sprintf for the message field.
	Eventf(eventType, reason, messageFmt string, args ...interface{})

	// PastEventf is just like Eventf, but with an option to specify the event's 'timestamp' field.
	PastEventf(timestamp metav1.Time, eventType, reason, messageFmt string, args ...interface{})
}

type SimpleRecorder struct {
	cs       kubernetes.Interface
	recorder record.EventRecorder
	pod      *corev1.Pod
}

func NewRecorder(cs kubernetes.Interface, namespace string) (*SimpleRecorder, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(log.Printf)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: cs.CoreV1().Events("")})

	recorder := eventBroadcaster.NewRecorder(k8sscheme.Scheme, corev1.EventSource{Component: "container-version-controller"})

	// We set INSTANCENAME as ENV variable using downward api on the container that maps to pod name
	// TODO: Need to use faker to handle running locally
	pod, err := cs.CoreV1().Pods(namespace).Get(os.Getenv("INSTANCENAME"), metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "event recorder failed to get pod instance")
	}

	return &SimpleRecorder{
		cs:       cs,
		recorder: recorder,
		pod:      pod,
	}, nil
}

// Event implements EventRecorder.
func (sr *SimpleRecorder) Event(eventtype, reason, message string) {
	sr.recorder.Event(sr.pod, eventtype, reason, message)

}

// Event implements EventRecorder.
func (sr *SimpleRecorder) Eventf(eventtype, reason, messageFmt string, args ...interface{}) {
	sr.recorder.Eventf(sr.pod, eventtype, reason, messageFmt, args...)
}

// Event implements EventRecorder.
func (sr *SimpleRecorder) PastEventf(timestamp metav1.Time, eventtype, reason, messageFmt string, args ...interface{}) {
	sr.recorder.PastEventf(sr.pod, timestamp, eventtype, reason, messageFmt, args...)
}
