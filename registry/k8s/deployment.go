package k8s

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/nearmap/cvmanager/registry/config"
	errs "github.com/nearmap/cvmanager/registry/errs"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
)

type K8sProvider struct {
	cs       *kubernetes.Clientset
	Recorder record.EventRecorder
	Pod      *v1.Pod

	namespace string

	stats stats.Stats
}

// NewK8sProvider abstracts operation performed against Kubernetes resources such as syncing deployments
// config maps etc
func NewK8sProvider(cs *kubernetes.Clientset, ns string, stats stats.Stats) (*K8sProvider, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(log.Printf)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: cs.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(k8sscheme.Scheme, corev1.EventSource{Component: "container-version-controller"})

	// We set INSTANCENAME as ENV variable using downward api on the container that maps to pod name
	// TODO: Need to use faker to handle running locally
	pod, err := cs.CoreV1().Pods(ns).Get(os.Getenv("INSTANCENAME"), metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get ECR sync pod")
	}

	return &K8sProvider{
		cs:       cs,
		Recorder: recorder,
		Pod:      pod,

		namespace: ns,

		stats: stats,
	}, nil

}

// SyncDeployment checks if deployment is up to date with the version thats requested, if not its
// performs a rollout with specified roll-out strategy on deployment
func (k *K8sProvider) SyncDeployment(name, version string, c *config.SyncConfig) error {
	// Check deployment
	d, err := k.cs.AppsV1().Deployments(k.namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "CRSyncFailed", "Failed to get dependent deployment")
		return errors.Wrap(err, "failed to read deployment ")
	}
	if ci, err := k.checkDeployment(version, d, c); err != nil {
		if err == errs.ErrVersionMismatch {
			return k.deploy(ci, d, c)
		} else {
			// Check version raises events as deemed necessary .. for other issues log is ok for now
			// and continue checking
			log.Printf("Failed sync with image: digest=%v, tag=%v, err=%v", version, c.Tag, err)
		}
	}
	return nil
}

func (k *K8sProvider) validate(v string) error {
	//TODO later regression check etc
	return nil
}

func (k *K8sProvider) checkDeployment(tag string, d *appsv1.Deployment, conf *config.SyncConfig) (string, error) {
	log.Printf("Checking deployment version %s from ECR %s for deployment %s", tag, conf.RepoName, conf.Deployment)
	match := false
	for _, c := range d.Spec.Template.Spec.Containers {
		if c.Name == conf.Container {
			match = true
			parts := strings.SplitN(c.Image, ":", 2)
			if len(parts) > 2 {
				k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "CRSyncFailed", "Invalid image on container")
				return "", errors.New("Invalid image found on container")
			}
			if parts[0] != conf.RepoARN {
				k.stats.Event(fmt.Sprintf("%s.%s.%s.sync.failure", k.namespace, conf.RepoName, conf.Deployment),
					fmt.Sprintf("ECR repo mismatch present %s and requested  %s don't match", parts[0], conf.RepoARN), "", "error",
					time.Now().UTC(), conf.Tag)
				k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "CRSyncFailed", "ECR Repository mismatch was found")
				return "", errs.ErrValidation
			}
			if tag != parts[1] {
				if k.validate(tag) != nil {
					k.stats.Event(fmt.Sprintf("%s.%s.%s.sync.failure", k.namespace, conf.RepoName, conf.Deployment),
						fmt.Sprintf("Failed to validate image with tag %s", tag), "", "error",
						time.Now().UTC(), conf.Tag)
					k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "CRSyncFailed", "Candidate version failed validation")
					return "", errs.ErrValidation
				}
				return tag, errs.ErrVersionMismatch
			}
		}
	}

	if !match {
		k.stats.Event(fmt.Sprintf("%s.%s.%s.sync.failure", k.namespace, conf.RepoName, conf.Deployment),
			"No matching container found", "", "error",
			time.Now().UTC(), conf.Tag)
		k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "CRSyncFailed", "No matching container found")

		return "", errors.Errorf("No container of name %s was found in deployment %s", conf.Container, conf.Deployment)
	}

	log.Printf("Deployment %s is upto date deployment", conf.Deployment)
	return "", nil
}

// rollback tag logic is not needed revisionHistoryLimit automatically maintains 6 revisions limits
func (k *K8sProvider) deploy(tag string, d *appsv1.Deployment, conf *config.SyncConfig) error {
	log.Printf("Beginning deploy for deployment %s with version %s", conf.Deployment, tag)

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		for i, c := range d.Spec.Template.Spec.Containers {
			if c.Name == conf.Container {
				if _, updateErr := k.cs.AppsV1().Deployments(k.namespace).Patch(d.ObjectMeta.Name, types.StrategicMergePatchType,
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
						`, d.Spec.Template.Spec.Containers[i].Name, conf.RepoARN, tag))); updateErr != nil {

					log.Printf("Failed to update container version (will retry): version=%v, deployment=%v, error=%v",
						tag, conf.Deployment, updateErr)

					return updateErr
				}

			}
		}
		return nil
	})
	if retryErr != nil {
		k.stats.Event(fmt.Sprintf("%s.%s.%s.sync.deploy.failure", k.namespace, conf.RepoName, conf.Deployment),
			fmt.Sprintf("Failed to validate image with %s", tag), "", "error",
			time.Now().UTC(), conf.Tag)
		log.Printf("Failed to update container version after maximum retries: version=%v, deployment=%v, error=%v",
			tag, conf.Deployment, retryErr)
		k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "DeploymentFailed", "Failed to perform the deployment")
	}

	log.Printf("Update completed: deployment=%v", conf.Deployment)
	k.stats.IncCount(fmt.Sprintf("%s.%s.%s.success", k.namespace, conf.RepoName, conf.Deployment))
	k.Recorder.Event(k.Pod, corev1.EventTypeNormal, "DeploymentSuccess", "Deployment completed successfully")
	return nil
}

// SyncConfig sync the config map referenced by CV resource - creates if absent and updates if required
// The controller is not responsible for managing the config resource it reference but only for updating
// and ensuring its present. If the reference to config was removed from CV resource its not the responsibility
// of controller to remove it .. it assumes the configMap is external resource and not owned by cv resource
func (k *K8sProvider) SyncConfig(version string, conf *config.SyncConfig) error {
	if conf.ConfigMap == nil {
		return nil
	}
	cm, err := k.cs.CoreV1().ConfigMaps(k.namespace).Get(conf.ConfigMap.Name, metav1.GetOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			_, err = k.cs.CoreV1().ConfigMaps(k.namespace).Create(
				newVersionConfig(k.namespace, conf.ConfigMap.Name, conf.ConfigMap.Key, version))
			if err != nil {
				k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "FailedCreateVersionConfigMap", "Failed to create version configmap")
				return errors.Wrapf(err, "failed to create version configmap from %s/%s:%s",
					k.namespace, conf.ConfigMap.Name, conf.ConfigMap.Key)
			}
			return nil
		}
		return errors.Wrapf(err, "failed to get version configmap from %s/%s:%s",
			k.namespace, conf.ConfigMap.Name, conf.ConfigMap.Key)
	}

	if version == cm.Data[conf.ConfigMap.Key] {
		return nil
	}

	cm.Data[conf.ConfigMap.Key] = version

	// TODO enable this when patchstretegy is supported on config map https://github.com/kubernetes/client-go/blob/7ac1236/pkg/api/v1/types.go#L3979
	// _, err = s.k8sClient.CoreV1().ConfigMaps(s.namespace).Patch(cm.ObjectMeta.Name, types.StrategicMergePatchType, []byte(fmt.Sprintf(`{
	// 	"Data": {
	// 		"%s": "%s",
	// 	},
	// }`, s.Config.ConfigMap.Key, version)))
	_, err = k.cs.CoreV1().ConfigMaps(k.namespace).Update(cm)
	if err != nil {
		k.Recorder.Event(k.Pod, corev1.EventTypeWarning, "FailedUpdateVersionConfigMao", "Failed to update version configmap")
		return errors.Wrapf(err, "failed to update version configmap from %s/%s:%s",
			k.namespace, conf.ConfigMap.Name, conf.ConfigMap.Key)
	}
	return nil
}

// newVersionConfig creates a new configmap for a version if specified in CV resource.
func newVersionConfig(namespace, name, key, version string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			key: version,
		},
	}
}
