package k8s

import (
	"fmt"
	"log"
	"strings"
	"time"

	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	errs "github.com/nearmap/cvmanager/registry/errs"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
)

func (k *K8sProvider) syncJobs(cv *cv1.ContainerVersion, version string, listOpts metav1.ListOptions) error {
	ds, err := k.cs.BatchV1().Jobs(k.namespace).List(listOpts)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}

		k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "CRSyncFailed", "Failed to get dependent jobs")
		return errors.Wrap(err, "failed to read jobs ")
	}
	for _, d := range ds.Items {
		if ci, err := k.checkPodSpec(d.Spec.Template, d.Name, version, cv); err != nil {
			if err == errs.ErrVersionMismatch {
				return k.patchPodSpec(d.Spec.Template, d.Name, ci, cv, func(i int) error {
					_, err := k.cs.BatchV1().Jobs(k.namespace).Patch(d.ObjectMeta.Name, types.StrategicMergePatchType,
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
						`, d.Spec.Template.Spec.Containers[i].Name, cv.Spec.ImageRepo, version)))
					return err
				})
			} else {
				// Check version raises events as deemed necessary ..
				// for other issues log is ok for now and continue checking
				log.Printf("Failed sync job with image: digest=%v, tag=%v, err=%v", version, cv.Spec.Tag, err)
				k.stats.Event(fmt.Sprintf("%s.%s.sync.failure", k.namespace, d.Name),
					fmt.Sprintf("Failed to sync pod spec with %s", version), "", "error",
					time.Now().UTC())
				k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "StatefulSetFailed", "Error syncing job")
			}
		}
	}
	return nil
}

func (k *K8sProvider) cvJobs(cv *cv1.ContainerVersion, listOpts metav1.ListOptions) ([]*Resource, error) {
	ds, err := k.cs.BatchV1().Jobs(cv.Namespace).List(listOpts)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "Failed to fetch cronjob")
	}
	var cvsList []*Resource
	for _, dd := range ds.Items {
		for _, c := range dd.Spec.Template.Spec.Containers {
			if cv.Spec.Container == c.Name {
				cvsList = append(cvsList, &Resource{
					Namespace: cv.Namespace,
					Name:      dd.Name,
					Type:      "Job",
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
