package workload

import (
	"fmt"
	"time"

	kcd1 "github.com/wish/kcd/gok8s/apis/custom/v1"
	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	gobatchv1 "k8s.io/client-go/kubernetes/typed/batch/v1"
)

const (
	TypeJob = "Job"
)

type Job struct {
	job *batchv1.Job

	client gobatchv1.JobInterface
}

func NewJob(cs kubernetes.Interface, namespace string, job *batchv1.Job) *Job {
	client := cs.BatchV1().Jobs(namespace)
	return newJob(job, client)
}

func newJob(job *batchv1.Job, client gobatchv1.JobInterface) *Job {
	return &Job{
		job:    job,
		client: client,
	}
}

func (j *Job) String() string {
	return fmt.Sprintf("%+v", j.job)
}

// Name implements the Workload interface.
func (j *Job) Name() string {
	return j.job.Name
}

// Namespace implements the Workload interface.
func (j *Job) Namespace() string {
	return j.job.Namespace
}

// Type implements the Workload interface.
func (j *Job) Type() string {
	return TypeJob
}

// PodSpec implements the Workload interface.
func (j *Job) PodSpec() corev1.PodSpec {
	return j.job.Spec.Template.Spec
}

// RollbackAfter implements the Workload interface.
func (j *Job) RollbackAfter() *time.Duration {
	return nil
}

// ProgressHealth implements the Workload interface.
func (j *Job) ProgressHealth(startTime time.Time) (*bool, error) {
	result := true
	return &result, nil
}

// RolloutFailed implements the Workload interface.
func (j *Job) RolloutFailed(rolloutTime time.Time) (bool, error) {
	return false, nil
}

// PodSelector implements the Workload interface.
func (j *Job) PodSelector() string {
	set := labels.Set(j.job.Spec.Template.Labels)
	return set.AsSelector().String()
}

// PodTemplateSpec implements the TemplateRolloutTarget interface.
func (j *Job) PodTemplateSpec() corev1.PodTemplateSpec {
	return j.job.Spec.Template
}

// PatchPodSpec implements the Workload interface.
func (j *Job) PatchPodSpec(kcd *kcd1.KCD, container corev1.Container, version string) error {
	_, err := j.client.Patch(j.job.ObjectMeta.Name, types.StrategicMergePatchType,
		[]byte(fmt.Sprintf(podTemplateSpecJSON, container.Name, kcd.Spec.ImageRepo, version)))
	if err != nil {
		return errors.Wrapf(err, "failed to patch pod template spec container for Job %s", j.job.Name)
	}
	return nil
}
