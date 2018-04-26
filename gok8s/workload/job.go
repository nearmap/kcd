package k8s

import (
	"fmt"
	"strings"

	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
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

func (j *Job) Name() string {
	return j.job.Name
}

func (j *Job) Type() string {
	return TypeJob
}

func (j *Job) PodSpec() corev1.PodSpec {
	return j.job.Spec.Template.Spec
}

func (j *Job) PodTemplateSpec() corev1.PodTemplateSpec {
	return j.job.Spec.Template
}

func (j *Job) PatchPodSpec(cv *cv1.ContainerVersion, container corev1.Container, version string) error {
	_, err := j.client.Patch(j.job.ObjectMeta.Name, types.StrategicMergePatchType,
		[]byte(fmt.Sprintf(podTemplateSpecJSON, container.Name, cv.Spec.ImageRepo, version)))
	if err != nil {
		return errors.Wrapf(err, "failed to patch pod template spec container for Job %s", j.job.Name)
	}
	return nil
}

func (j *Job) AsResource(cv *cv1.ContainerVersion) *Resource {
	for _, c := range j.job.Spec.Template.Spec.Containers {
		if cv.Spec.Container == c.Name {
			return &Resource{
				Namespace: cv.Namespace,
				Name:      j.job.Name,
				Type:      TypeJob,
				Container: c.Name,
				Version:   strings.SplitAfterN(c.Image, ":", 2)[1],
				CV:        cv.Name,
				Tag:       cv.Spec.Tag,
			}
		}
	}

	return nil
}
