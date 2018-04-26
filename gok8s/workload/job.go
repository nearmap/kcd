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

/*
const job = "Job"

func (k *K8sProvider) syncJobs(cv *cv1.ContainerVersion, version string, listOpts metav1.ListOptions) error {
	ds, err := k.cs.BatchV1().Jobs(k.namespace).List(listOpts)
	if err != nil {
		k.Recorder.Event(events.Warning, "CRSyncFailed", "Failed to get dependent jobs")
		return errors.Wrap(err, "failed to read jobs ")
	}
	for _, d := range ds.Items {
		if ci, err := k.checkPodSpec(d.Spec.Template, d.Name, version, cv); err != nil {
			if err == errs.ErrVersionMismatch {
				return k.deployer.Deploy(d.Spec.Template, d.Name, ci, cv, func(i int) error {
					_, err := k.cs.BatchV1().Jobs(k.namespace).Patch(d.ObjectMeta.Name, types.StrategicMergePatchType,
						[]byte(fmt.Sprintf(podTemplateSpec, d.Spec.Template.Spec.Containers[i].Name, cv.Spec.ImageRepo, version)))
					return err
				}, func() string { return job })
			} else {
				k.raiseSyncPodErrEvents(err, job, d.Name, cv.Spec.Tag, version)
			}
		}
	}
	return nil
}

func (k *K8sProvider) cvJobs(cv *cv1.ContainerVersion, listOpts metav1.ListOptions) ([]*Resource, error) {
	ds, err := k.cs.BatchV1().Jobs(cv.Namespace).List(listOpts)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to fetch job")
	}
	var cvsList []*Resource
	for _, dd := range ds.Items {
		for _, c := range dd.Spec.Template.Spec.Containers {
			if cv.Spec.Container == c.Name {
				cvsList = append(cvsList, &Resource{
					Namespace: cv.Namespace,
					Name:      dd.Name,
					Type:      job,
					Container: c.Name,
					Version:   strings.SplitAfterN(c.Image, ":", 2)[1],
					CV:        cv.Name,
					Tag:       cv.Spec.Tag,
				})
			}
		}
	}

	return cvsList, nil
}
*/
