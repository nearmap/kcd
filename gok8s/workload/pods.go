package k8s

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/registry/errs"
	"github.com/pkg/errors"
	"k8s.io/api/core/v1"
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

// checkPodSpec checks whether the current version tag of the container
// in the given pod spec with the given container name has the given
// version. Return a nil error if the versions match. If not matching,
// an errs.ErrorVersionMismatch is returned.
func checkPodSpec(cv *cv1.ContainerVersion, version string, podTemplateSpec v1.PodTemplateSpec) error {
	match := false
	for _, c := range podTemplateSpec.Spec.Containers {
		if c.Name == cv.Spec.Container {
			match = true
			parts := strings.SplitN(c.Image, ":", 2)
			if len(parts) > 2 {
				return errors.New("invalid image on container")
			}
			if parts[0] != cv.Spec.ImageRepo {
				return errors.Errorf("ECR repo mismatch present %s and requested  %s don't match",
					parts[0], cv.Spec.ImageRepo)
			}
			if version != parts[1] {
				if validate(version) != nil {
					return errors.Errorf("failed to validate image with tag %s", version)
				}
				return errs.ErrVersionMismatch
			}
		}
	}

	if !match {
		return errors.Errorf("no container of name %s was found in workload", cv.Spec.Container)
	}

	return nil
}

// raiseSyncPodErrEvents raises k8s and stats events indicating sync failure
func (k *K8sProvider) raiseSyncPodErrEvents(err error, typ, name, tag, version string) {
	log.Printf("Failed sync %s with image: digest=%v, tag=%v, err=%v", typ, version, tag, err)
	k.stats.Event(fmt.Sprintf("%s.sync.failure", name),
		fmt.Sprintf("Failed to sync pod spec with %s", version), "", "error",
		time.Now().UTC())
	k.Recorder.Event(events.Warning, "CRSyncFailed", fmt.Sprintf("Error syncing %s name:%s", typ, name))
}
