package deploy_test

import (
	"reflect"
	"testing"

	"github.com/nearmap/cvmanager/deploy"
	"github.com/nearmap/cvmanager/deploy/fake"
	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
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
	cv := &cv1.ContainerVersion{
		Spec: cv1.ContainerVersionSpec{
			Container: cv1.ContainerSpec{
				Name: containerName,
			},
		},
	}
	version := "version-string"
	namespace := "test-namespace"
	target := fake.NewRolloutTarget()

	cs := gofake.NewSimpleClientset()

	// SUT
	deployer := deploy.NewSimpleDeployer(cs, events.NewFakeRecorder(100), namespace)

	target.FakePodSpec.Containers = []corev1.Container{}
	err := deployer.Deploy(cv, version, target)
	if err == nil {
		t.Errorf("Expected error when podspec does not contain any containers")
	}

	target.FakePodSpec.Containers = []corev1.Container{
		corev1.Container{
			Name: containerName,
		},
	}
	pps := fake.NewInvocationPatchPodSpec()
	target.Invocations <- pps
	err = deployer.Deploy(cv, version, target)
	if err != nil {
		t.Errorf("Expected no error when PodSpec contains a container with the correct container name. Got %v", err)
	}
	if !reflect.DeepEqual(pps.Received.CV, cv) {
		t.Errorf("Expected received CV instance to equal the provided instance. Got %+v.", pps.Received.CV)
	}
	if !reflect.DeepEqual(pps.Received.Container, target.FakePodSpec.Containers[0]) {
		t.Errorf("Expected received container to equal the provided matching container. Got %+v.", pps.Received.Container)
	}
	if pps.Received.Version != version {
		t.Errorf("Expected received version to equal the provided version. Got %+v.", pps.Received.Version)
	}

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
	err = deployer.Deploy(cv, version, target)
	if err != nil {
		t.Errorf("Expected no error when PodSpec contains a container with the correct container name. Got %v", err)
	}
	if !reflect.DeepEqual(pps.Received.CV, cv) {
		t.Errorf("Expected received CV instance to equal the provided instance. Got %+v.", pps.Received.CV)
	}
	if !reflect.DeepEqual(pps.Received.Container, target.FakePodSpec.Containers[1]) {
		t.Errorf("Expected received container to equal the provided matching container. Got %+v.", pps.Received.Container)
	}
	if pps.Received.Version != version {
		t.Errorf("Expected received version to equal the provided version. Got %+v.", pps.Received.Version)
	}

	pps = fake.NewInvocationPatchPodSpec()
	pps.Error = errors.New("an error occurred")
	target.Invocations <- pps
	err = deployer.Deploy(cv, version, target)
	if err == nil {
		t.Errorf("Expected error when PatchPodSpec returns an error that is not a conflict")
	}

	pps = fake.NewInvocationPatchPodSpec()
	pps.Error = apimacherrors.NewConflict(schema.GroupResource{}, "", errors.New(""))
	target.Invocations <- pps
	target.Invocations <- fake.NewInvocationPatchPodSpec()
	err = deployer.Deploy(cv, version, target)
	if err != nil {
		t.Errorf("Expected no error when PatchPodSpec returns an error that IS conflict")
	}
}
