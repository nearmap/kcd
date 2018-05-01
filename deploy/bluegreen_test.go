package deploy_test

import (
	"testing"

	"github.com/nearmap/cvmanager/deploy"
	"github.com/nearmap/cvmanager/deploy/fake"
	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/stats"
	gofake "k8s.io/client-go/kubernetes/fake"
)

func TestBlueGreenDeployErrorCases(t *testing.T) {
	cv := &cv1.ContainerVersion{
		Spec: cv1.ContainerVersionSpec{
			Container: containerName,
		},
	}
	version := "version-string"
	namespace := "test-namespace"
	target := fake.NewRolloutTarget()

	cs := gofake.NewSimpleClientset()

	// SUT
	deployer := deploy.NewBlueGreenDeployer(cs, events.NewFakeRecorder(100), stats.NewFake(), namespace)

	err := deployer.Deploy(cv, version, target)
	if err == nil {
		t.Errorf("expected error for target that does not implement TemplateRolloutTarget")
	}
}

func TestBlueGreenDeployWithSecondary(t *testing.T) {
	cv := &cv1.ContainerVersion{
		Spec: cv1.ContainerVersionSpec{
			Container: containerName,
		},
	}
	version := "version-string"
	namespace := "test-namespace"
	target := fake.NewRolloutTarget()

	cs := gofake.NewSimpleClientset()

	// SUT
	deployer := deploy.NewBlueGreenDeployer(cs, events.NewFakeRecorder(100), stats.NewFake(), namespace)

	// TODO:
}
