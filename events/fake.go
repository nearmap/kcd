package events

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FakeRecorder is used as a fake during tests. It is thread safe. It is usable
// when created manually and not by NewFakeRecorder, however all events may be
// thrown away in this case.
type FakeRecorder struct {
	Events chan string
}

// Event implements the Recorder interface.
func (f *FakeRecorder) Event(eventtype, reason, message string) {
	if f.Events != nil {
		select {
		case f.Events <- fmt.Sprintf("%s %s %s", eventtype, reason, message):
		default:
		}
	}
}

// Eventf implements the Recorder interface.
func (f *FakeRecorder) Eventf(eventtype, reason, messageFmt string, args ...interface{}) {
	if f.Events != nil {
		select {
		case f.Events <- fmt.Sprintf(eventtype+" "+reason+" "+messageFmt, args...):
		default:
		}
	}
}

// PastEventf implements the Recorder interface.
func (f *FakeRecorder) PastEventf(timestamp metav1.Time, eventtype, reason, messageFmt string, args ...interface{}) {
	// TODO: ??
}

// NewFakeRecorder creates new fake event recorder with event channel with
// buffer of given size.
func NewFakeRecorder(bufferSize int) *FakeRecorder {
	return &FakeRecorder{
		Events: make(chan string, bufferSize),
	}
}
