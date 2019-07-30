package deploy_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/eric1313/kcd/deploy"
	"github.com/eric1313/kcd/deploy/fake"
	kcd1 "github.com/eric1313/kcd/gok8s/apis/custom/v1"
	"github.com/eric1313/kcd/gok8s/workload"
	"github.com/eric1313/kcd/registry"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apimacherrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gofake "k8s.io/client-go/kubernetes/fake"
)

const (
	containerName = "test-container-name"
)

func TestSimpleDeploy(t *testing.T) {
	cs := gofake.NewSimpleClientset()

	kcd := &kcd1.KCD{
		Spec: kcd1.KCDSpec{
			Container: kcd1.ContainerSpec{
				Name: containerName,
			},
		},
	}
	version := "version-string"
	targets := []deploy.RolloutTarget{}
	workloadProvider := workload.NewFakeProvider(cs, "test-namespace", targets)

	// TODO:
	var registryProvider registry.Provider

	_, err := deploy.NewSimpleDeployer(workloadProvider, registryProvider, kcd, version)
	if err == nil {
		t.Errorf("Expected error when no workloads provided")
	}

	/////

	target := fake.NewRolloutTarget()
	target.FakePodSpec.Containers = []corev1.Container{}

	targets = []deploy.RolloutTarget{target}
	workloadProvider = workload.NewFakeProvider(cs, "test-namespace", targets)

	sd, err := deploy.NewSimpleDeployer(workloadProvider, registryProvider, kcd, version)
	if err != nil {
		t.Errorf("Unexpected error creating NewSimpleDeployer")
	}
	_, err = sd.AsState(nil).Do(context.Background())
	if err == nil {
		t.Errorf("Expected error when PodSpec does not have a container with the expected container name.")
	}

	/////

	target = fake.NewRolloutTarget()
	target.FakePodSpec.Containers = []corev1.Container{
		corev1.Container{
			Name: containerName,
		},
	}
	pps := fake.NewInvocationPatchPodSpec()
	target.Invocations <- pps

	targets = []deploy.RolloutTarget{target}
	workloadProvider = workload.NewFakeProvider(cs, "test-namespace", targets)

	sd, err = deploy.NewSimpleDeployer(workloadProvider, registryProvider, kcd, version)
	if err != nil {
		t.Errorf("Unexpected error creating NewSimpleDeployer")
	}
	_, err = sd.AsState(nil).Do(context.Background())
	if err != nil {
		t.Errorf("Expected no error when PodSpec contains a container with the correct container name. Got %v", err)
	}
	if !reflect.DeepEqual(pps.Received.CV, kcd) {
		t.Errorf("Expected received CV instance to equal the provided instance. Got %+v.", pps.Received.CV)
	}
	if !reflect.DeepEqual(pps.Received.Container, target.FakePodSpec.Containers[0]) {
		t.Errorf("Expected received container to equal the provided matching container. Got %+v.", pps.Received.Container)
	}
	if pps.Received.Version != version {
		t.Errorf("Expected received version to equal the provided version. Got %+v.", pps.Received.Version)
	}

	/////

	target = fake.NewRolloutTarget()
	target.FakePodSpec.Containers = []corev1.Container{
		corev1.Container{
			Name: "first-name",
		},
		corev1.Container{
			Name: containerName,
		},
		corev1.Container{
			Name: "other-name",
		},
	}
	pps = fake.NewInvocationPatchPodSpec()
	target.Invocations <- pps

	targets = []deploy.RolloutTarget{target}
	workloadProvider = workload.NewFakeProvider(cs, "test-namespace", targets)

	sd, err = deploy.NewSimpleDeployer(workloadProvider, registryProvider, kcd, version)
	if err != nil {
		t.Errorf("Unexpected error creating NewSimpleDeployer")
	}
	_, err = sd.AsState(nil).Do(context.Background())
	if err != nil {
		t.Errorf("Unexpected error when PodSpec contains a container with the correct container name. Got %v", err)
	}
	if !reflect.DeepEqual(pps.Received.CV, kcd) {
		t.Errorf("Expected received CV instance to equal the provided instance. Got %+v.", pps.Received.CV)
	}
	if !reflect.DeepEqual(pps.Received.Container, target.FakePodSpec.Containers[1]) {
		t.Errorf("Expected received container to equal the provided matching container. Got %+v.", pps.Received.Container)
	}
	if pps.Received.Version != version {
		t.Errorf("Expected received version to equal the provided version. Got %+v.", pps.Received.Version)
	}

	/////

	pps = fake.NewInvocationPatchPodSpec()
	pps.Error = errors.New("an error occurred")
	target.Invocations <- pps

	sd, err = deploy.NewSimpleDeployer(workloadProvider, registryProvider, kcd, version)
	if err != nil {
		t.Errorf("Unexpected error creating NewSimpleDeployer")
	}
	_, err = sd.AsState(nil).Do(context.Background())
	if err == nil {
		t.Errorf("Expected error when PatchPodSpec returns an error that is not a conflict")
	}

	/////

	pps = fake.NewInvocationPatchPodSpec()
	pps.Error = apimacherrors.NewConflict(schema.GroupResource{}, "", errors.New(""))
	target.Invocations <- pps
	target.Invocations <- fake.NewInvocationPatchPodSpec()

	sd, err = deploy.NewSimpleDeployer(workloadProvider, registryProvider, kcd, version)
	if err != nil {
		t.Errorf("Unexpected error creating NewSimpleDeployer")
	}
	_, err = sd.AsState(nil).Do(context.Background())
	if err != nil {
		t.Errorf("Expected no error when PatchPodSpec returns an error that IS conflict")
	}
}
