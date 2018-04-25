package k8s

import (
	"fmt"
	"log"

	"github.com/nearmap/cvmanager/deploy"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	goappsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
)

const (
	TypeDeployment = "Deployment"
)

type Deployment struct {
	deployment *appsv1.Deployment

	client           goappsv1.DeploymentInterface
	replicasetClient goappsv1.ReplicaSetInterface
}

func NewDeployment(cs kubernetes.Interface, namespace string, deployment *appsv1.Deployment) *Deployment {
	client := cs.AppsV1().Deployments(namespace)
	replicasetClient := cs.AppsV1().ReplicaSets(namespace)

	return newDeployment(deployment, client, replicasetClient)
}

func newDeployment(deployment *appsv1.Deployment, client goappsv1.DeploymentInterface,
	replicasetClient goappsv1.ReplicaSetInterface) *Deployment {

	return &Deployment{
		deployment:       deployment,
		client:           client,
		replicasetClient: replicasetClient,
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
		[]byte(fmt.Sprintf(podTemplateSpecJSON, container.Name, cv.Spec.ImageRepo, version)))
	if err != nil {
		return errors.Wrapf(err, "failed to patch pod template spec container for deployment %s", d.deployment.Name)
	}
	return nil
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
		result = append(result, newDeployment(&deployment, d.client, d.replicasetClient))
	}

	return result, nil
}

func (d *Deployment) SelectOwnPods(pods []corev1.Pod) ([]corev1.Pod, error) {
	var result []corev1.Pod
	for _, pod := range pods {
		for _, podOwner := range pod.OwnerReferences {
			switch podOwner.Kind {
			case TypeReplicaSet:
				rs, err := d.replicaSetForName(podOwner.Name)
				if err != nil {
					return nil, errors.Wrap(err, "failed to get pod's owner replicaset")
				}
				for _, rsOwner := range rs.OwnerReferences {
					switch rsOwner.Kind {
					case TypeDeployment:
						if rsOwner.Name == d.deployment.Name {
							result = append(result, pod)
						} else {
							// temp
							log.Printf("Replicaset owner name %s did not match deployment name %s", rsOwner.Name, d.deployment.Name)
						}
					default:
						log.Printf("Ignoring unknown replicaset owner kind: %v", rsOwner.Kind)
					}
				}
			default:
				log.Printf("Ignoring unknown pod owner kind: %v", podOwner.Kind)
			}
		}
	}

	log.Printf("SelectOwnPods returning %d pods", len(result))
	return result, nil
}

func (d *Deployment) replicaSetForName(name string) (*appsv1.ReplicaSet, error) {
	rs, err := d.replicasetClient.Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get replicaset with name %s", name)
	}

	// temp
	log.Printf("ReplicaSetsForName %s returned %+v", name, rs)
	log.Printf("\n=====\n")

	return rs, nil
}

func (d *Deployment) NumReplicas() int32 {
	return d.deployment.Status.Replicas
}

const deploymentReplicaSetPatchJSON = `
	{
		"spec": {
			"replicas": %d
		}
	}`

func (d *Deployment) PatchNumReplicas(num int32) error {
	_, err := d.client.Patch(d.deployment.ObjectMeta.Name, types.StrategicMergePatchType,
		[]byte(fmt.Sprintf(deploymentReplicaSetPatchJSON, num)))
	if err != nil {
		return errors.Wrapf(err, "failed to patch pod template spec replicas for deployment %s", d.deployment.Name)
	}
	return nil
}

/*
func (d *Deployment) Pods(podClient gocorev1.PodInterface) ([]corev1.Pod, error) {
	set := labels.Set(d.deployment.Spec.Labels)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}

	pods, err := podClient.List(listOpts)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list pods with selector %v", listOpts)
	}

}
*/
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
