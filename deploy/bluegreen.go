package deploy

import (
	"log"

	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/history"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

type BlueGreenDeploySpec interface {
}

type BlueGreenDeployer struct {
	simpleDeployer *SimpleDeployer

	namespace string

	hp            history.Provider
	recordHistory bool

	cs       kubernetes.Interface
	recorder events.Recorder

	stats stats.Stats
}

func NewBlueGreenDeployer(cs kubernetes.Interface, eventRecorder events.Recorder, stats stats.Stats, namespace string) *BlueGreenDeployer {
	return &BlueGreenDeployer{
		simpleDeployer: NewSimpleDeployer(cs, eventRecorder, stats, namespace),
		namespace:      namespace,
		cs:             cs,
		recorder:       eventRecorder,
	}
}

func (bgd *BlueGreenDeployer) Deploy(cv *cv1.ContainerVersion, version string, spec DeploySpec) error {
	if cv.Spec.Strategy.BlueGreen == nil {
		return errors.Errorf("no blue-green spec provided for cv resource %s", cv.Name)
	}
	if cv.Spec.Strategy.BlueGreen.ServiceName == "" {
		return errors.Errorf("no service defined for blue-green strategy in cv resource %s", cv.Name)
	}
	if cv.Spec.Strategy.BlueGreen.LabelName == "" {
		return errors.Errorf("no label name defined for blue-green strategy in cv resource %s", cv.Name)
	}

	log.Printf("Beginning bluegreen deployment for workload %s with version %s in namespace %s", spec.Name(), version, bgd.namespace)

	service, err := bgd.getService(cv, cv.Spec.Strategy.BlueGreen.ServiceName)
	if err != nil {
		return errors.Wrapf(err, "failed to find service for cv spec %s", cv.Name)
	}

	// temp
	log.Printf("Got service %+v", service)

	isCurrent, err := bgd.isCurrentDeploySpec(spec, service)
	if err != nil {
		return errors.WithStack(err)
	}
	if isCurrent {
		log.Printf("Spec %s is current live workload for service %s. Not changing.", spec.Name(), service.Name)
		return nil
	}

	if err := bgd.simpleDeployer.Deploy(cv, version, spec); err != nil {
		return errors.Wrapf(err, "failed to patch pod spec for blue-green strategy %s", cv.Name)
	}

	if err := bgd.updateTestServiceSelector(cv, spec); err != nil {
		return errors.Wrapf(err, "failed to update test service for cv spec %s", cv.Name)
	}

	if err := bgd.verify(cv); err != nil {
		return errors.Wrapf(err, "failed verification step for cv spec %s", cv.Name)
	}

	if err := bgd.updateServiceSelector(cv, spec, cv.Spec.Strategy.BlueGreen.ServiceName); err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func (bgd *BlueGreenDeployer) getService(cv *cv1.ContainerVersion, serviceName string) (*v1.Service, error) {
	service, err := bgd.cs.CoreV1().Services(bgd.namespace).Get(serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get service with name %s", serviceName)
	}

	return service, nil
}

func (bgd *BlueGreenDeployer) isCurrentDeploySpec(spec DeploySpec, service *v1.Service) (bool, error) {
	// temp
	log.Printf("Checking is current deployment spec...")

	podTemplates, err := bgd.selectPodTemplates(service.Spec.Selector)
	if err != nil {
		return false, errors.WithStack(err)
	}

	/////
	// temp
	if len(podTemplates) != 1 {
		bgd.testPodSelection(service.Spec.Selector)
	}

	/////

	if len(podTemplates) != 1 {
		return false, errors.Errorf("unexpected number of pod templates selected for service %s, found %d",
			service.Name, len(podTemplates))
	}

	return podTemplates[0].Template.UID == spec.PodTemplateSpec().UID, nil
}

func (bgd *BlueGreenDeployer) selectPodTemplates(selector map[string]string) ([]v1.PodTemplate, error) {
	// temp
	log.Printf("selecting pod templates with selector %+v", selector)

	set := labels.Set(selector)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}

	podTemplates, err := bgd.cs.CoreV1().PodTemplates(bgd.namespace).List(listOpts)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to select pod templates with selector %s", selector)
	}

	return podTemplates.Items, nil
}

////////////////////////////////////////////////
// temp

func (bgd *BlueGreenDeployer) testPodSelection(selector map[string]string) {
	set := labels.Set(selector)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}

	pods, err := bgd.cs.CoreV1().Pods(bgd.namespace).List(listOpts)
	if err != nil {
		log.Printf("error listing pods: %v", err)
	} else {
		log.Printf("Selected %d pods: %+v", len(pods.Items), pods.Items)
	}
}

///////////////////////////////////////////////

func (bgd *BlueGreenDeployer) updateTestServiceSelector(cv *cv1.ContainerVersion, spec DeploySpec) error {
	if cv.Spec.Strategy.BlueGreen.TestServiceName == "" {
		log.Printf("No test service defined for cv spec %s", cv.Name)
		return nil
	}

	if err := bgd.updateServiceSelector(cv, spec, cv.Spec.Strategy.BlueGreen.TestServiceName); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

func (bgd *BlueGreenDeployer) updateServiceSelector(cv *cv1.ContainerVersion, spec DeploySpec, serviceName string) error {
	labelName := cv.Spec.Strategy.BlueGreen.LabelName

	testService, err := bgd.getService(cv, serviceName)
	if err != nil {
		return errors.Wrapf(err, "failed to find test service for cv spec %s", cv.Name)
	}

	targetLabel, has := spec.PodTemplateSpec().Labels[labelName]
	if !has {
		return errors.Errorf("pod template spec for %s is missing label name %s in cv spec %s",
			spec.Name(), labelName, cv.Name)
	}

	testService.Spec.Selector[labelName] = targetLabel

	// TODO: is update appropriate?
	if _, err := bgd.cs.CoreV1().Services(bgd.namespace).Update(testService); err != nil {
		return errors.Wrapf(err, "failed to update test service %s while processing blue-green deployment for %s",
			testService.Name, cv.Name)
	}

	return nil
}

func (bgd *BlueGreenDeployer) verify(cv *cv1.ContainerVersion) error {
	if cv.Spec.Strategy == nil || cv.Spec.Strategy.Verify == nil {
		return nil
	}

	// TODO:
	return nil
}

//////////////////////////////////

func (bgd *BlueGreenDeployer) getCreateDeployments(cv *cv1.ContainerVersion, spec DeploySpec) (
	current, other BlueGreenDeploySpec, err error) {

	specs, err := spec.Select(cv.Spec.Selector)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to select deploy specs for blue green deployment")
	}

	// specs should have the same name
	var blueGreen []DeploySpec
	for _, sp := range specs {
		if sp.Name() == spec.Name() {
			blueGreen = append(blueGreen, sp)
		}
	}

	if len(blueGreen) == 0 || len(blueGreen) > 2 {
		return nil, nil,
			errors.Errorf("unexpected number of specs found in blue green deployment with name %d, found %d",
				spec.Name(), len(blueGreen))
	}

	if len(blueGreen) == 1 {
		other, err := blueGreen[0].Duplicate()
		if err != nil {
			return nil, nil, errors.WithStack(err)
		}
		blueGreen = append(blueGreen, other)
	}

	return nil, nil, nil
}
