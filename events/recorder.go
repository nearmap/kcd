package events

import (
	"log"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

// SimpleRecorder implements the Recorder interface.
type SimpleRecorder struct {
	cs       kubernetes.Interface
	recorder record.EventRecorder
	object   runtime.Object
}

// NewRecorder returns an event recorder that encapsulates the current namespace and runtime object.
func NewRecorder(cs kubernetes.Interface, namespace string, object runtime.Object) *SimpleRecorder {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(log.Printf)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: cs.CoreV1().Events("")})

	recorder := eventBroadcaster.NewRecorder(k8sscheme.Scheme, corev1.EventSource{Component: "container-version-controller"})

	return &SimpleRecorder{
		cs:       cs,
		recorder: recorder,
		object:   object,
	}
}

// Event implements EventRecorder.
func (sr *SimpleRecorder) Event(eventtype, reason, message string) {
	sr.recorder.Event(sr.object, eventtype, reason, message)

}

// Event implements EventRecorder.
func (sr *SimpleRecorder) Eventf(eventtype, reason, messageFmt string, args ...interface{}) {
	sr.recorder.Eventf(sr.object, eventtype, reason, messageFmt, args...)
}

// Event implements EventRecorder.
func (sr *SimpleRecorder) PastEventf(timestamp metav1.Time, eventtype, reason, messageFmt string, args ...interface{}) {
	sr.recorder.PastEventf(sr.object, timestamp, eventtype, reason, messageFmt, args...)
}
