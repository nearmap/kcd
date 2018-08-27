package workload

import (
	"fmt"
	"time"

	kcd1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	"github.com/pkg/errors"
	v1beta1 "k8s.io/api/batch/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	goappsv1beta1 "k8s.io/client-go/kubernetes/typed/batch/v1beta1"
)

const (
	TypeCronJob = "CronJob"
)

// CronJob defines a workload for managing CronJobs.
type CronJob struct {
	cronJob *v1beta1.CronJob

	client goappsv1beta1.CronJobInterface
}

// NewCronJob returns an instance for managing CronJob workloads.
func NewCronJob(cs kubernetes.Interface, namespace string, cronJob *v1beta1.CronJob) *CronJob {
	client := cs.BatchV1beta1().CronJobs(namespace)
	return newCronJob(cronJob, client)
}

func newCronJob(cronJob *v1beta1.CronJob, client goappsv1beta1.CronJobInterface) *CronJob {
	return &CronJob{
		cronJob: cronJob,
		client:  client,
	}
}

func (cj *CronJob) String() string {
	return fmt.Sprintf("%+v", cj.cronJob)
}

// Name implements the Workload interface.
func (cj *CronJob) Name() string {
	return cj.cronJob.Name
}

// Namespace implements the Workload interface.
func (cj *CronJob) Namespace() string {
	return cj.cronJob.Namespace
}

// Type implements the Workload interface.
func (cj *CronJob) Type() string {
	return TypeCronJob
}

// PodSpec implements the Workload interface.
func (cj *CronJob) PodSpec() corev1.PodSpec {
	return cj.cronJob.Spec.JobTemplate.Spec.Template.Spec
}

// RollbackAfter implements the Workload interface.
func (cj *CronJob) RollbackAfter() *time.Duration {
	return nil
}

// ProgressHealth implements the Workload interface.
func (cj *CronJob) ProgressHealth(startTime time.Time) (*bool, error) {
	result := true
	return &result, nil
}

// PodTemplateSpec implements the TemplateRolloutTarget interface.
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

// PatchPodSpec implements the Workload interface.
func (cj *CronJob) PatchPodSpec(kcd *kcd1.KCD, container corev1.Container, version string) error {
	_, err := cj.client.Patch(cj.cronJob.ObjectMeta.Name, types.StrategicMergePatchType,
		[]byte(fmt.Sprintf(cronJobPatchPodJSON, container.Name, kcd.Spec.ImageRepo, version)))
	if err != nil {
		return errors.Wrapf(err, "failed to patch pod template spec container for CronJOb %s", cj.cronJob.Name)
	}
	return nil
}
