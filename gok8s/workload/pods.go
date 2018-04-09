package k8s

import (
	"fmt"
	"log"
	"strings"
	"time"

	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/history"
	errs "github.com/nearmap/cvmanager/registry/errs"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
)

const podTemplateSpec = `
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
						`

type patchPodSpecFn func(i int) error
type typeFn func() string

func (k *K8sProvider) checkPodSpec(d v1.PodTemplateSpec, name, tag string, cv *cv1.ContainerVersion) (string, error) {
	log.Printf("Checking daemonSet version %s from ECR for daemonSet %s", tag, name)
	match := false
	for _, c := range d.Spec.Containers {
		if c.Name == cv.Spec.Container {
			match = true
			parts := strings.SplitN(c.Image, ":", 2)
			if len(parts) > 2 {
				k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "CRSyncFailed", "Invalid image on container")
				return "", errors.New("Invalid image found on container")
			}
			if parts[0] != cv.Spec.ImageRepo {
				k.stats.Event(fmt.Sprintf("%s.%s.sync.failure", k.namespace, name),
					fmt.Sprintf("ECR repo mismatch present %s and requested  %s don't match", parts[0], cv.Spec.ImageRepo), "", "error",
					time.Now().UTC())
				k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "CRSyncFailed", "ECR Repository mismatch was found")
				return "", errs.ErrValidation
			}
			if tag != parts[1] {
				if k.validate(tag) != nil {
					k.stats.Event(fmt.Sprintf("%s.%s.sync.failure", k.namespace, name),
						fmt.Sprintf("Failed to validate image with tag %s", tag), "", "error",
						time.Now().UTC())
					k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "CRSyncFailed", "Candidate version failed validation")
					return "", errs.ErrValidation
				}
				return tag, errs.ErrVersionMismatch
			}
		}
	}

	if !match {
		k.stats.Event(fmt.Sprintf("%s.%s.sync.failure", k.namespace, name), "No matching container found", "",
			"error", time.Now().UTC())
		k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "CRSyncFailed", "No matching container found")

		return "", errors.Errorf("No container of name %s was found in daemonSet %s", cv.Spec.Container, name)
	}

	log.Printf("workload resource %s is upto date", name)
	return "", nil
}

// rollback tag logic is not needed revisionHistoryLimit automatically maintains 6 revisions limits
func (k *K8sProvider) patchPodSpec(d v1.PodTemplateSpec, name, tag string, cv *cv1.ContainerVersion, ppfn patchPodSpecFn,
	typFn typeFn) error {

	log.Printf("Beginning deploy for deployment %s with version %s", name, tag)

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		for i, c := range d.Spec.Containers {
			if c.Name == cv.Spec.Container {
				if updateErr := ppfn(i); updateErr != nil {
					log.Printf("Failed to update container version (will retry): version=%v, deployment=%v, error=%v",
						tag, d.Name, updateErr)

					return updateErr
				}

			}
		}
		return nil
	})
	if retryErr != nil {
		k.stats.Event(fmt.Sprintf("%s.%s.sync.deploy.failure", k.namespace, name),
			fmt.Sprintf("Failed to validate image with %s", tag), "", "error",
			time.Now().UTC())
		log.Printf("Failed to update container version after maximum retries: version=%v, deployment=%v, error=%v",
			tag, name, retryErr)
		k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "StatefulSetFailed", "Failed to perform the deployment")
	}

	err := k.hp.Add(k.namespace, name, &history.Record{
		Type:    typFn(),
		Name:    name,
		Version: tag,
		Time:    time.Now(),
	})
	if err != nil {
		k.stats.IncCount(fmt.Sprintf("cvc.%s.%s.history.save.failure", k.namespace, name))
		k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "SaveHistoryFailed", "Failed to record update history")
	}

	log.Printf("Update completed: deployment=%v", name)
	k.stats.IncCount(fmt.Sprintf("%s.%s.success", k.namespace, name))
	k.Recorder.Event(k.Pod, corev1.EventTypeNormal, "StatefulSetSuccess", "StatefulSet completed successfully")
	return nil
}

// raiseSyncPodErrEvents raises k8s and stats events indicating sync failure
func (k *K8sProvider) raiseSyncPodErrEvents(err error, typ, name, tag, version string) {
	log.Printf("Failed sync %s with image: digest=%v, tag=%v, err=%v", typ, version, tag, err)
	k.stats.Event(fmt.Sprintf("%s.%s.sync.failure", k.namespace, name),
		fmt.Sprintf("Failed to sync pod spec with %s", version), "", "error",
		time.Now().UTC())
	k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "StatefulSetFailed", fmt.Sprintf("Error syncing %s name:%s", typ, name))

}
