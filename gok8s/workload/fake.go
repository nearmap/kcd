package workload

import (
	kcdv1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	"k8s.io/client-go/kubernetes"
)

type FakeProvider struct {
	namespace string
	client    kubernetes.Interface
	workloads []Workload
}

func NewFakeProvider(client kubernetes.Interface, namespace string, workloads []Workload) *FakeProvider {
	return &FakeProvider{
		client:    client,
		namespace: namespace,
		workloads: workloads,
	}
}

func (fp *FakeProvider) Namespace() string {
	return fp.namespace
}

func (fp *FakeProvider) Client() kubernetes.Interface {
	return fp.client
}

func (fp *FakeProvider) Workloads(kcd *kcdv1.KCD, types ...string) ([]Workload, error) {
	return fp.workloads, nil
}
