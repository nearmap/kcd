package k8s

import (
	"fmt"
	"strings"

	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/pkg/errors"
	batchv2alpha1 "k8s.io/api/batch/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	goappsv2alpha1 "k8s.io/client-go/kubernetes/typed/batch/v2alpha1"
)

const (
	TypeCronJob = "CronJob"
)

type CronJob struct {
	cronJob *batchv2alpha1.CronJob

	client goappsv2alpha1.CronJobInterface
}

func NewCronJob(cs kubernetes.Interface, namespace string, cronJob *batchv2alpha1.CronJob) *CronJob {
	client := cs.BatchV2alpha1().CronJobs(namespace)
	return newCronJob(cronJob, client)
}

func newCronJob(cronJob *batchv2alpha1.CronJob, client goappsv2alpha1.CronJobInterface) *CronJob {
	return &CronJob{
		cronJob: cronJob,
		client:  client,
	}
}

func (cj *CronJob) String() string {
	return fmt.Sprintf("%+v", cj.cronJob)
}

func (cj *CronJob) Name() string {
	return cj.cronJob.Name
}

func (cj *CronJob) Type() string {
	return TypeCronJob
}

func (cj *CronJob) PodSpec() corev1.PodSpec {
	return cj.cronJob.Spec.JobTemplate.Spec.Template.Spec
}

func (cj *CronJob) PodTemplateSpec() corev1.PodTemplateSpec {
	return cj.cronJob.Spec.JobTemplate.Spec.Template
}

var (
	cronJobPatchPodJSON = fmt.Sprintf(`
	{
		"spec": {
			"jobTemplate": %s
			}
		}
	}
	`, podTemplateSpecJSON)
)

func (cj *CronJob) PatchPodSpec(cv *cv1.ContainerVersion, container corev1.Container, version string) error {
	_, err := cj.client.Patch(cj.cronJob.ObjectMeta.Name, types.StrategicMergePatchType,
		[]byte(fmt.Sprintf(cronJobPatchPodJSON, container.Name, cv.Spec.ImageRepo, version)))
	if err != nil {
		return errors.Wrapf(err, "failed to patch pod template spec container for CronJOb %s", cj.cronJob.Name)
	}
	return nil
}

func (cj *CronJob) AsResource(cv *cv1.ContainerVersion) *Resource {
	for _, c := range cj.cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers {
		if cv.Spec.Container == c.Name {
			return &Resource{
				Namespace: cv.Namespace,
				Name:      cj.cronJob.Name,
				Type:      TypeCronJob,
				Container: c.Name,
				Version:   strings.SplitAfterN(c.Image, ":", 2)[1],
				CV:        cv.Name,
				Tag:       cv.Spec.Tag,
			}
		}
	}

	return nil
}
