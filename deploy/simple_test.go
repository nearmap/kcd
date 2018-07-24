package deploy_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/nearmap/kcd/deploy"
	"github.com/nearmap/kcd/deploy/fake"
	kcd1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apimacherrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	containerName = "test-container-name"
)

func TestSimpleDeploy(t *testing.T) {
	kcd := &kcd1.KCD{
		Spec: kcd1.KCDSpec{
			Container: kcd1.ContainerSpec{
				Name: containerName,
			},
		},
	}
	version := "version-string"
	target := fake.NewRolloutTarget()

	target.FakePodSpec.Containers = []corev1.Container{}
	_, err := deploy.NewSimpleDeployer(kcd, version, target, nil).Do(context.Background())
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

	_, err = deploy.NewSimpleDeployer(kcd, version, target, nil).Do(context.Background())
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
	_, err = deploy.NewSimpleDeployer(kcd, version, target, nil).Do(context.Background())
	if err != nil {
		t.Errorf("Expected no error when PodSpec contains a container with the correct container name. Got %v", err)
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

	pps = fake.NewInvocationPatchPodSpec()
	pps.Error = errors.New("an error occurred")
	target.Invocations <- pps
	_, err = deploy.NewSimpleDeployer(kcd, version, target, nil).Do(context.Background())
	if err == nil {
		t.Errorf("Expected error when PatchPodSpec returns an error that is not a conflict")
	}

	pps = fake.NewInvocationPatchPodSpec()
	pps.Error = apimacherrors.NewConflict(schema.GroupResource{}, "", errors.New(""))
	target.Invocations <- pps
	target.Invocations <- fake.NewInvocationPatchPodSpec()
	_, err = deploy.NewSimpleDeployer(kcd, version, target, nil).Do(context.Background())
	if err != nil {
		t.Errorf("Expected no error when PatchPodSpec returns an error that IS conflict")
	}
}
