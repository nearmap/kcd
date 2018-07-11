package k8s

import (
	"github.com/nearmap/kcd/events"
	cv1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SyncVersionConfig sync the config map referenced by CV resource - creates if absent and updates if required
// The controller is not responsible for managing the config resource it reference but only for updating
// and ensuring its present. If the reference to config was removed from CV resource its not the responsibility
// of controller to remove it .. it assumes the configMap is external resource and not owned by cv resource
func (k *Provider) SyncVersionConfig(cv *cv1.ContainerVersion, version string) error {
	if cv.Spec.Config == nil {
		return nil
	}
	cm, err := k.cs.CoreV1().ConfigMaps(k.namespace).Get(cv.Spec.Config.Name, metav1.GetOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			_, err = k.cs.CoreV1().ConfigMaps(k.namespace).Create(
				newVersionConfig(k.namespace, cv.Spec.Config.Name, cv.Spec.Config.Key, version))
			if err != nil {
				k.options.Recorder.Event(events.Warning, "FailedCreateVersionConfigMap", "Failed to create version configmap")
				return errors.Wrapf(err, "failed to create version configmap from %s/%s:%s",
					k.namespace, cv.Spec.Config.Name, cv.Spec.Config.Key)
			}
			return nil
		}
		return errors.Wrapf(err, "failed to get version configmap from %s/%s:%s",
			k.namespace, cv.Spec.Config.Name, cv.Spec.Config.Key)
	}

	if version == cm.Data[cv.Spec.Config.Key] {
		return nil
	}

	cm.Data[cv.Spec.Config.Key] = version

	// TODO enable this when patchstretegy is supported on config map https://github.com/kubernetes/client-go/blob/7ac1236/pkg/api/v1/types.go#L3979
	// _, err = s.k8sClient.CoreV1().ConfigMaps(s.namespace).Patch(cm.ObjectMeta.Name, types.StrategicMergePatchType, []byte(fmt.Sprintf(`{
	// 	"Data": {
	// 		"%s": "%s",
	// 	},
	// }`, s.Config.ConfigMap.Key, version)))
	_, err = k.cs.CoreV1().ConfigMaps(k.namespace).Update(cm)
	if err != nil {
		k.options.Recorder.Event(events.Warning, "FailedUpdateVersionConfigMao", "Failed to update version configmap")
		return errors.Wrapf(err, "failed to update version configmap from %s/%s:%s",
			k.namespace, cv.Spec.Config.Name, cv.Spec.Config.Key)
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
