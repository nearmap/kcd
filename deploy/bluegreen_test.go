package deploy_test

import (
	"context"
	"testing"

	"github.com/nearmap/kcd/deploy"
	"github.com/nearmap/kcd/deploy/fake"
	kcd1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	"github.com/nearmap/kcd/registry"
	gofake "k8s.io/client-go/kubernetes/fake"
)

func TestBlueGreenDeployErrorCases(t *testing.T) {
	kcd := &kcd1.KCD{
		Spec: kcd1.KCDSpec{
			Container: kcd1.ContainerSpec{
				Name: containerName,
			},
			Strategy: &kcd1.StrategySpec{
				BlueGreen: &kcd1.BlueGreenSpec{},
			},
		},
	}
	version := "version-string"
	namespace := "test-namespace"
	target := fake.NewRolloutTarget()

	cs := gofake.NewSimpleClientset()

	// TODO:
	var registryProvider registry.Provider

	// SUT
	deployer := deploy.NewBlueGreenDeployer(cs, registryProvider, namespace, kcd, version, target, nil)

	_, err := deployer.Do(context.Background())
	if err == nil {
		t.Errorf("expected error for target that does not implement TemplateRolloutTarget")
	}
}
