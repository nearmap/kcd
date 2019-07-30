package resource

import (
	"time"

	"github.com/golang/glog"
	kcdv1 "github.com/Eric1313/kcd/gok8s/apis/custom/v1"
	clientset "github.com/Eric1313/kcd/gok8s/client/clientset/versioned"
	"github.com/Eric1313/kcd/gok8s/workload"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	StatusFailed      = "Failed"
	StatusSuccess     = "Success"
	StatusProgressing = "Progressing"
)

// Resource maintains a high level status of deployments managed by
// CV resources including version of current deploy and number of available pods
// from this deployment/replicaset
type Resource struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Container string `json:"container"`
	Tag       string `json:"tag"`

	Status      string    `json:"status"`
	CurrVersion string    `json:"currVersion"`
	LiveVersion string    `json:"liveVersion"`
	LastUpdated time.Time `json:"lastUpdated"`

	Recent bool `json:"-"`
}

type Provider interface {
	KCD(namespace, name string) (*kcdv1.KCD, error)
	Resource(kcd *kcdv1.KCD) *Resource
	AllResources(namespace string) ([]*Resource, error)
	UpdateStatus(namespace, kcdName, version, status string, tm time.Time) (*kcdv1.KCD, error)
}

type K8sProvider struct {
	kcdcs            clientset.Interface
	workloadProvider workload.Provider
}

func NewK8sProvider(namespace string, kcdcs clientset.Interface, workloadProvider workload.Provider) *K8sProvider {
	return &K8sProvider{
		kcdcs:            kcdcs,
		workloadProvider: workloadProvider,
	}
}

// KCD returns a KCD resource with the given name.
func (p *K8sProvider) KCD(namespace, name string) (*kcdv1.KCD, error) {
	glog.V(2).Infof("Getting KCD with name=%s", name)

	client := p.kcdcs.CustomV1().KCDs(namespace)
	kcd, err := client.Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get KCD instance with name %s", name)
	}

	if glog.V(4) {
		glog.V(4).Infof("Got KCD: %+v", kcd)
	}
	return kcd, nil
}

// AllResources returns all resources managed by container versions in the current namespace.
func (p *K8sProvider) AllResources(namespace string) ([]*Resource, error) {
	kcds, err := p.kcdcs.CustomV1().KCDs(namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "Failed to generate template of CV list")
	}

	var resources []*Resource
	for _, kcd := range kcds.Items {
		resources = append(resources, p.Resource(&kcd))
	}

	return resources, nil
}

// Resource returns a resource summary for the given kcd instance.
func (p *K8sProvider) Resource(kcd *kcdv1.KCD) *Resource {
	return &Resource{
		Namespace:   kcd.Namespace,
		Name:        kcd.Name,
		Container:   kcd.Spec.Container.Name,
		Tag:         kcd.Spec.Tag,
		Status:      kcd.Status.CurrStatus,
		CurrVersion: kcd.Status.CurrVersion,
		LiveVersion: kcd.Status.SuccessVersion,
		LastUpdated: kcd.Status.CurrStatusTime.Time,
		Recent:      kcd.Status.CurrStatusTime.Time.After(time.Now().UTC().Add(time.Hour * -1)),
	}
}

// UpdateStatus updates the KCD with the given name to indicate a
// status for the given version and time. Returns the updated KCD.
func (p *K8sProvider) UpdateStatus(namespace, kcdName, version, status string, tm time.Time) (*kcdv1.KCD, error) {
	glog.V(2).Infof("Updating status for kcd=%s, version=%s, status=%s, time=%v", kcdName, version, status, tm)

	client := p.kcdcs.CustomV1().KCDs(namespace)

	kcd, err := client.Get(kcdName, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get KCD instance with name %s", kcd.Name)
	}

	if version != "" {
		kcd.Status.CurrVersion = version
	}
	if status != "" {
		kcd.Status.CurrStatus = status
	}
	if !tm.IsZero() {
		kcd.Status.CurrStatusTime = metav1.NewTime(tm)
	}
	if status == StatusSuccess && version != "" {
		kcd.Status.SuccessVersion = version
	}

	result, err := client.Update(kcd)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to update KCD spec %s", kcd.Name)
	}

	glog.V(2).Infof("Successfully updated KCD status: %+v", result)
	return result, nil
}
