package deploy_test

import (
	"testing"

	"github.com/nearmap/kcd/deploy"
	"github.com/nearmap/kcd/deploy/fake"
	kcd1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	"github.com/nearmap/kcd/gok8s/workload"
	"github.com/nearmap/kcd/registry"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gofake "k8s.io/client-go/kubernetes/fake"
)

func TestBlueGreenDeployErrorCases(t *testing.T) {
	serviceName := "test-service"
	kcd := &kcd1.KCD{
		Spec: kcd1.KCDSpec{
			Container: kcd1.ContainerSpec{
				Name: containerName,
			},
			Strategy: &kcd1.StrategySpec{
				BlueGreen: &kcd1.BlueGreenSpec{
					ServiceName: serviceName,
					LabelNames:  []string{"test-label"},
				},
			},
		},
	}
	version := "version-string"
	namespace := "test-namespace"

	target1 := fake.NewTemplateRolloutTarget()
	target1.FakePodTemplateSpec = corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"service-selector": "primary",
			},
		},
	}
	target2 := fake.NewTemplateRolloutTarget()
	target2.FakePodTemplateSpec = corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"service-selector": "secondary",
			},
		},
	}

	targets := []deploy.RolloutTarget{
		target1,
		target2,
	}

	cs := gofake.NewSimpleClientset()
	_, err := cs.CoreV1().Services(namespace).Create(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceName,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"service-selector": "primary",
			},
		},
	})
	if err != nil {
		t.Errorf("unexpected error when creating service: %v", err)
	}

	workloadProvider := workload.NewFakeProvider(cs, namespace, targets)

	// TODO:
	var registryProvider registry.Provider

	// SUT
	deployer, err := deploy.NewBlueGreenDeployer(workloadProvider, registryProvider, kcd, version)
	if err != nil {
		t.Errorf("unexpected error for new bluegreen deployer")
	}

	if deployer == nil {
		t.Errorf("expected deployer not to be nil")
	}

}
