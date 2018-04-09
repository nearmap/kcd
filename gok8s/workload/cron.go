package k8s

import (
	"fmt"
	"strings"

	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	errs "github.com/nearmap/cvmanager/registry/errs"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
)

func (k *K8sProvider) syncCronJobs(cv *cv1.ContainerVersion, version string, listOpts metav1.ListOptions) error {
	// BatchV2alpha1 is 10.x onwards
	ds, err := k.cs.BatchV1beta1().CronJobs(k.namespace).List(listOpts)
	if err != nil {
		k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "CRSyncFailed", "Failed to get dependent cronjobs")
		return errors.Wrap(err, "failed to read cronjobs ")
	}
	for _, d := range ds.Items {
		if ci, err := k.checkPodSpec(d.Spec.JobTemplate.Spec.Template, d.Name, version, cv); err != nil {
			if err == errs.ErrVersionMismatch {
				return k.patchPodSpec(d.Spec.JobTemplate.Spec.Template, d.Name, ci, cv, func(i int) error {
					_, err := k.cs.BatchV2alpha1().CronJobs(k.namespace).Patch(d.ObjectMeta.Name, types.StrategicMergePatchType,
						[]byte(fmt.Sprintf(`
						{
							"spec": {
								"template": {
									"spec": {
										"containers": [
												{
													"name":  "%s",
													"image": "%s:%s"
												}
											]
										}
									}
								}
							}
						}
						`, d.Spec.JobTemplate.Spec.Template.Spec.Containers[i].Name, cv.Spec.ImageRepo, version)))
					return err
				})
			} else {
				k.raiseSyncPodErrEvents(err, "CronJob", d.Name, cv.Spec.Tag, version)
			}
		}
	}
	return nil
}

func (k *K8sProvider) cvCronJobs(cv *cv1.ContainerVersion, listOpts metav1.ListOptions) ([]*Resource, error) {
	ds, err := k.cs.BatchV1beta1().CronJobs(cv.Namespace).List(listOpts)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "Failed to fetch cronjob")
	}
	var cvsList []*Resource
	for _, dd := range ds.Items {
		for _, c := range dd.Spec.JobTemplate.Spec.Template.Spec.Containers {
			if cv.Spec.Container == c.Name {
				cvsList = append(cvsList, &Resource{
					Namespace: cv.Namespace,
					Name:      dd.Name,
					Type:      "CronJob",
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
