package workload

import (
	"strings"
	"time"

	kcdv1 "github.com/eric1313/kcd/gok8s/apis/custom/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
)

// Workload defines an interface for something deployable, such as a Deployment, DaemonSet, Pod, etc.
type Workload interface {
	// Name is the name of the workload (without the namespace).
	Name() string

	// Namespace returns the namespace the workload belongs to.
	Namespace() string

	// Type returns the type of the spec.
	Type() string

	// PodSpec returns the PodSpec for the workload.
	PodSpec() corev1.PodSpec

	// PatchPodSpec receives a pod spec and container which is to be patched
	// according to an appropriate strategy for the type.
	PatchPodSpec(kcd *kcdv1.KCD, container corev1.Container, version string) error

	// RollbackAfter indicates duration after which a failed rollout
	// should attempt rollback
	// TODO: needed?
	//RollbackAfter() *time.Duration

	// ProgressHealth indicates weather the current status of progress healthy or not.
	// The start time of the deployment operation is provided.
	// TODO: needed?
	//ProgressHealth(startTime time.Time) (*bool, error)

	// RolloutFailed returns true if the rollout starting at rolloutTime has failed,
	// according to conditions registered on the workload. A value of false
	// indicates that the workload succeeded or is still progressing.
	RolloutFailed(rolloutTime time.Time) (bool, error)

	// PodSelector returns a label selector as a string for obtaining a list of pods
	// managed by this workload.
	PodSelector() string
}

// TemplateWorkload defines methods for deployable resources that manage a collection
// of pods via a pod template. More deployment options are available for such resources.
type TemplateWorkload interface {
	Workload

	// PodTemplateSpec returns the PodTemplateSpec for this workload.
	PodTemplateSpec() corev1.PodTemplateSpec

	// Select all Workloads of this type with the given selector. May return
	// the current spec if it matches the selector.
	//Select(selector map[string]string) ([]TemplateWorkload, error)

	// SelectOwnPods returns a list of pods that are managed by this workload.
	//SelectOwnPods(pods []corev1.Pod) ([]corev1.Pod, error)

	// NumReplicas returns the current number of replicas for this workload.
	NumReplicas() (int32, error)

	// PatchNumReplicas modifies the number of replicas for this workload.
	PatchNumReplicas(num int32) error
}

// CheckPodSpecVersion tests whether all containers in the pod spec with container
// names that match the kcd spec have the given version.
// Returns false if at least one container's version does not match at least one
// specified version.
// Returns an error if no containers in the pod spec match the container name
// defined by the KCD resource.
func CheckPodSpecVersion(podSpec corev1.PodSpec, kcd *kcdv1.KCD, versions ...string) (bool, error) {
	match := false
	for _, c := range podSpec.Containers {
		if c.Name == kcd.Spec.Container.Name {
			match = true
			parts := strings.SplitN(c.Image, ":", 2)
			if len(parts) > 2 {
				return false, errors.Errorf("invalid image found in container %s: %v", c.Name, c.Image)
			}
			if parts[0] != kcd.Spec.ImageRepo {
				return false, errors.Errorf("Repository mismatch for container %s: %s and requested %s don't match",
					c.Name, parts[0], kcd.Spec.ImageRepo)
			}

			found := false
			cver := parts[1]
			for _, version := range versions {
				if cver == version {
					found = true
					break
				}
			}
			if !found {
				return false, nil
			}
		}
	}

	if !match {
		return false, errors.Errorf("no container of name %s was found in workload", kcd.Spec.Container.Name)
	}

	return true, nil
}
