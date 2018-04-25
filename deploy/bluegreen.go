package deploy

import (
	"log"
	"strings"
	"time"

	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/history"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
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
		stats:          stats,
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

	log.Printf("Beginning blue-green deployment for workload %s with version %s in namespace %s", spec.Name(), version, bgd.namespace)

	defer timeTrack(time.Now(), "blue-green deployment")

	// temp
	log.Printf("Processing spec %s", spec)
	log.Printf("-----")
	log.Printf("with PodTemplateSpec: %+v", spec.PodTemplateSpec())
	log.Printf("-----")

	service, err := bgd.getService(cv, cv.Spec.Strategy.BlueGreen.ServiceName)
	if err != nil {
		return errors.Wrapf(err, "failed to find service for cv spec %s", cv.Name)
	}

	// temp
	log.Printf("Got service %+v", service)
	log.Printf("-----")

	isCurrent, err := bgd.isCurrentDeploySpec(cv, spec, service)
	if err != nil {
		return errors.WithStack(err)
	}

	// TODO: if we're not the current spec, but the current spec is the correct version
	// then we shouldn't do anything.

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

	if err := bgd.waitForAllPods(cv, version, spec); err != nil {
		return errors.WithStack(err)
	}

	if err := bgd.verify(cv); err != nil {
		return errors.Wrapf(err, "failed verification step for cv spec %s", cv.Name)
	}

	if err := bgd.updateServiceSelector(cv, spec, cv.Spec.Strategy.BlueGreen.ServiceName); err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func (bgd *BlueGreenDeployer) getService(cv *cv1.ContainerVersion, serviceName string) (*corev1.Service, error) {
	service, err := bgd.cs.CoreV1().Services(bgd.namespace).Get(serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get service with name %s", serviceName)
	}

	return service, nil
}

func (bgd *BlueGreenDeployer) isCurrentDeploySpec(cv *cv1.ContainerVersion, spec DeploySpec, service *corev1.Service) (bool, error) {
	// temp
	log.Printf("Checking is current deployment spec...")

	// get all the workloads managed by this cv spec
	workloads, err := spec.Select(cv.Spec.Selector)
	if err != nil {
		return false, errors.WithStack(err)
	}

	// temp
	log.Printf("Got %d workloads", len(workloads))
	for _, wl := range workloads {
		log.Printf("Workload: %s", wl)
		log.Printf("-----")
	}
	log.Printf("\n=====\n")

	if len(workloads) != 2 {
		return false, errors.Errorf("blue-green strategy requires exactly 2 workloads to be managed by a cv spec")
	}

	selector := labels.Set(service.Spec.Selector).AsSelector()

	// temp
	log.Printf("testing with selector %+v", selector)

	for _, wl := range workloads {
		ptLabels := labels.Set(wl.PodTemplateSpec().Labels)

		// temp
		log.Printf("checking whether %+v matches %+v", ptLabels, selector)

		if selector.Matches(ptLabels) {
			// this workload is the current live version
			return wl.Name() == spec.Name(), nil
		}
	}

	return false, errors.Errorf("blue-green strategy could not find a current live workload")
}

func (bgd *BlueGreenDeployer) selectPodTemplates(selector map[string]string) ([]corev1.PodTemplate, error) {
	// temp
	log.Printf("selecting pod templates with selector %+v", selector)

	set := labels.Set(selector)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}

	podTemplates, err := bgd.cs.CoreV1().PodTemplates(bgd.namespace).List(listOpts)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to select pod templates with selector %s", selector)
	}

	// temp
	log.Printf("selected %d pod templates:", len(podTemplates.Items))
	for _, pt := range podTemplates.Items {
		log.Printf("PodTemplate: %+v", pt)
		log.Printf("-----")
	}
	log.Printf("\n=====\n")

	return podTemplates.Items, nil
}

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

// waitForAllPods checks that all pods tied to the given DeploySpec are at the specified
// version, and waits polling if not the case.
// Returns an error if a timeout value is reached.
func (bgd *BlueGreenDeployer) waitForAllPods(cv *cv1.ContainerVersion, version string, spec DeploySpec) error {
	defer timeTrack(time.Now(), "waitForAllPods")

	// TODO: redo timeout logic
outer:
	for i := 0; i < 10; i++ {
		time.Sleep(15 * time.Second)

		// temp
		log.Printf("Attempt %d", i)

		pods, err := PodsForSpec(bgd.cs, bgd.namespace, spec)
		if err != nil {
			log.Printf("ERROR: failed to get pods for spec %s: %v", spec.Name(), err)
			// TODO: handle?
			continue outer
		}
		if len(pods) == 0 {
			// TODO: handle?
			log.Printf("ERROR: no pods found for spec %s", spec.Name())
			continue outer
		}

		for _, pod := range pods {
			ok, err := CheckContainerVersions(cv, version, pod.Spec)
			if err != nil {
				// TODO: handle?
				log.Printf("ERROR: failed to check container version for spec %s: %v", spec.Name(), err)
				continue outer
			}
			if !ok {
				log.Printf("Still waiting for rollout: pod %s is wrong version", pod.Name)
				continue outer
			}
		}

		log.Printf("All pods have been updated to version %s", version)
		return nil
	}

	return errors.Errorf("spec %s blue-green deployment timeout waiting for all pods to be deployed", spec.Name)
}

func PodsForSpec(cs kubernetes.Interface, namespace string, spec DeploySpec) ([]corev1.Pod, error) {
	set := labels.Set(spec.PodTemplateSpec().Labels)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}

	pods, err := cs.CoreV1().Pods(namespace).List(listOpts)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to select pods for spec %s", spec.Name())
	}

	// temp
	log.Printf("Initial Pods selected return %d pods", len(pods.Items))
	for _, p := range pods.Items {
		log.Printf("Pod: %+v", p)
		log.Printf("-----")
	}
	log.Printf("\n=====\n")

	result, err := spec.SelectOwnPods(pods.Items)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to filter pods for DepoySpec %s", spec.Name())
	}

	// temp
	log.Printf("PodsForSpec return %d pods", len(result))
	for _, p := range result {
		log.Printf("Pod: %+v", p)
		log.Printf("-----")
	}
	log.Printf("\n=====\n")

	return result, nil
}

func (bgd *BlueGreenDeployer) verify(cv *cv1.ContainerVersion) error {
	if cv.Spec.Strategy == nil || cv.Spec.Strategy.Verify == nil {
		log.Printf("No verification defined for %s", cv.Name)
		return nil
	}

	// TODO:
	return nil
}

// TODO: put this somewhere more general
// CheckContainerVersions tests whether all containers in the pod spec with container
// names that match the cv spec have the given version.
// Returns false if at least one container's version does not match.
func CheckContainerVersions(cv *cv1.ContainerVersion, version string, podSpec corev1.PodSpec) (bool, error) {
	match := false
	for _, c := range podSpec.Containers {
		if c.Name == cv.Spec.Container {
			match = true
			parts := strings.SplitN(c.Image, ":", 2)
			if len(parts) > 2 {
				return false, errors.New("invalid image on container")
			}
			if parts[0] != cv.Spec.ImageRepo {
				return false, errors.Errorf("Repository mismatch for container %s: %s and requested %s don't match",
					cv.Spec.Container, parts[0], cv.Spec.ImageRepo)
			}
			if version != parts[1] {
				return false, nil
			}
		}
	}

	if !match {
		return false, errors.Errorf("no container of name %s was found in workload", cv.Spec.Container)
	}

	return true, nil
}

////////////////////////////////////////////////
// temp

func (bgd *BlueGreenDeployer) isCurrentDeploySpec_OLD(spec DeploySpec, service *corev1.Service) (bool, error) {
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

func (bgd *BlueGreenDeployer) selectPodTemplates_OLD(selector map[string]string) ([]corev1.PodTemplate, error) {
	// temp
	log.Printf("selecting pod templates with selector %+v", selector)

	set := labels.Set(selector)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}

	podTemplates, err := bgd.cs.CoreV1().PodTemplates(bgd.namespace).List(listOpts)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to select pod templates with selector %s", selector)
	}

	// temp
	log.Printf("selected %d pod templates:", len(podTemplates.Items))
	for _, pt := range podTemplates.Items {
		log.Printf("PodTemplate: %+v", pt)
		log.Printf("-----")
	}
	log.Printf("\n=====\n")

	return podTemplates.Items, nil
}

func (bgd *BlueGreenDeployer) testPodSelection(selector map[string]string) {
	set := labels.Set(selector)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}

	pods, err := bgd.cs.CoreV1().Pods(bgd.namespace).List(listOpts)
	if err != nil {
		log.Printf("error listing pods: %v", err)
	} else {
		log.Printf("Selected %d pods:", len(pods.Items))
		for _, p := range pods.Items {
			log.Printf("Pod: %+v", p)
			log.Printf("-----")
		}
	}
	log.Printf("\n=====\n")

	deployments, err := bgd.cs.AppsV1().Deployments(bgd.namespace).List(listOpts)
	if err != nil {
		log.Printf("error listing deployments: %v", err)
	} else {
		log.Printf("Selected %d deployments:", len(deployments.Items))
		for _, d := range deployments.Items {
			log.Printf("Deployment: %+v", d)
			log.Printf("-----")
		}
	}
	log.Printf("\n=====\n")

	podTemplates, err := bgd.cs.CoreV1().PodTemplates(bgd.namespace).List(metav1.ListOptions{})
	if err != nil {
		log.Printf("error listing podtemplates: %v", err)
	} else {
		log.Printf("Selected %d PodTemplates:", len(deployments.Items))
		for _, pt := range podTemplates.Items {
			log.Printf("PodTemplate: %+v", pt)
			log.Printf("-----")
		}
	}
	log.Printf("\n=====\n")

}

func timeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	log.Printf("%s took %s", name, elapsed)
}
