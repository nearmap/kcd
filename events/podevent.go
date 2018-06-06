package events

import (
	"os"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// newPodEventRecorder creates an event record that attaches event of a pod
// identified by name specified
func newPodEventRecorder(cs kubernetes.Interface, ns string, podName string) Recorder {
	var recorder Recorder
	pod, err := cs.CoreV1().Pods(ns).Get(podName, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("failed to get pod with name %s for event recorder: %v", podName, err)
		recorder = &FakeRecorder{}
	} else {
		recorder = NewRecorder(cs, ns, pod)
	}
	return recorder
}

// PodEventRecorder creates an event record that attaches event of a pod
// The name of pod is derived from downward API and assumed to be set
// as env variable "NAME"
func PodEventRecorder(cs kubernetes.Interface, ns string) Recorder {
	podName := os.Getenv("NAME")
	return newPodEventRecorder(cs, ns, podName)
}
