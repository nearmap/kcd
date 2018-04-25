package events

import (
	"log"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)


func newPodEventRecorder(podName string) Recorder {
	var recorder Recorder
	// We set INSTANCENAME as ENV variable using downward api on the container that maps to pod name
	pod, err := cs.CoreV1().Pods("").Get(podName), metav1.GetOptions{})
	if err != nil {
		log.Printf("failed to get pod with name %s for event recorder: %v", podName, err)
		recorder = &events.FakeRecorder{}
	} else {
		recorder = events.NewRecorder(cs, "", pod)
	}
	return recorder
}

func PodEventRecorder() Recorder {
	podName := os.Getenv("NAME")
	return newPodEventRecorder(podName)
}
