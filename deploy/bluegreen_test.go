package deploy_test

import (
	"context"
	"testing"

	"github.com/nearmap/cvmanager/deploy"
	"github.com/nearmap/cvmanager/deploy/fake"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	gofake "k8s.io/client-go/kubernetes/fake"
)

func TestBlueGreenDeployErrorCases(t *testing.T) {
	cv := &cv1.ContainerVersion{
		Spec: cv1.ContainerVersionSpec{
			Container: cv1.ContainerSpec{
				Name: containerName,
			},
			Strategy: &cv1.StrategySpec{
				BlueGreen: &cv1.BlueGreenSpec{},
			},
		},
	}
	version := "version-string"
	namespace := "test-namespace"
	target := fake.NewRolloutTarget()

	cs := gofake.NewSimpleClientset()

	// SUT
	deployer := deploy.NewBlueGreenDeployer(cs, namespace, cv, version, target, nil)

	_, err := deployer.Do(context.Background())
	if err == nil {
		t.Errorf("expected error for target that does not implement TemplateRolloutTarget")
	}
}
