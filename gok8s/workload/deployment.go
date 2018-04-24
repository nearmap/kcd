package k8s

import (
	"fmt"

	"github.com/nearmap/cvmanager/deploy"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/pkg/errors"
	"k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	appsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
)

const (
	TypeDeployment = "Deployment"
)

type Deployment struct {
	//cs        kubernetes.Interface
	//namespace string

	deployment *v1.Deployment

	client appsv1.DeploymentInterface
}

func NewDeployment(cs kubernetes.Interface, namespace string, deployment *v1.Deployment) *Deployment {
	client := cs.AppsV1().Deployments(namespace)

	return &Deployment{
		deployment: deployment,
		client:     client,
	}
}

func (d *Deployment) String() string {
	return fmt.Sprintf("%+v", d.deployment)
}

func (d *Deployment) Name() string {
	return d.deployment.Name
}

func (d *Deployment) Type() string {
	return TypeDeployment
}

func (d *Deployment) PodTemplateSpec() corev1.PodTemplateSpec {
	return d.deployment.Spec.Template
}

func (d *Deployment) PatchPodSpec(cv *cv1.ContainerVersion, container corev1.Container, version string) error {
	_, err := d.client.Patch(d.deployment.ObjectMeta.Name, types.StrategicMergePatchType,
		[]byte(fmt.Sprintf(podTemplateSpec, container.Name, cv.Spec.ImageRepo, version)))
	return err
}

func (d *Deployment) Select(selector map[string]string) ([]deploy.DeploySpec, error) {
	set := labels.Set(selector)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}

	var result []deploy.DeploySpec

	wls, err := d.client.List(listOpts)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	for _, wl := range wls.Items {
		deployment := wl
		result = append(result, &Deployment{
			deployment: &deployment,
			client:     d.client,
		})
	}

	return result, nil
}

/////
/*
const deployment = "Deployment"

func (k *K8sProvider) syncDeployments(cv *cv1.ContainerVersion, version string, listOpts metav1.ListOptions) error {
	ds, err := k.cs.AppsV1().Deployments(k.namespace).List(listOpts)
	if err != nil {
		k.Recorder.Event(events.Warning, "CRSyncFailed", "Failed to get dependent deployment")
		return errors.Wrap(err, "failed to read deployment ")
	}
	for _, d := range ds.Items {
		if ci, err := k.checkPodSpec(d.Spec.Template, d.Name, version, cv); err != nil {
			if err == errs.ErrVersionMismatch {
				err := k.deployer.Deploy(d.Spec.Template, d.Name, ci, cv, func(i int) error {
					_, err := k.cs.AppsV1().Deployments(k.namespace).Patch(d.ObjectMeta.Name, types.StrategicMergePatchType,
						[]byte(fmt.Sprintf(podTemplateSpec, d.Spec.Template.Spec.Containers[i].Name, cv.Spec.ImageRepo, version)))
					return err
				}, func() string { return deployment })
				// Check if rollback is opted in
				// Add status check upuntil progress time, or rollback
				return err
			} else {
				k.raiseSyncPodErrEvents(err, deployment, d.Name, cv.Spec.Tag, version)
			}
		}
	}
	return nil
}

func (k *K8sProvider) cvDeployment(cv *cv1.ContainerVersion, listOpts metav1.ListOptions) ([]*Resource, error) {
	ds, err := k.cs.AppsV1().Deployments(cv.Namespace).List(listOpts)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to fetch deployment")
	}
	var cvsList []*Resource
	for _, dd := range ds.Items {
		for _, c := range dd.Spec.Template.Spec.Containers {
			if cv.Spec.Container == c.Name {
				cvsList = append(cvsList, &Resource{
					Namespace:     cv.Namespace,
					Name:          dd.Name,
					Type:          deployment,
					Container:     c.Name,
					Version:       strings.SplitAfterN(c.Image, ":", 2)[1],
					AvailablePods: dd.Status.AvailableReplicas,
					CV:            cv.Name,
					Tag:           cv.Spec.Tag,
				})
			}
		}
	}

	return cvsList, nil
}
*/
