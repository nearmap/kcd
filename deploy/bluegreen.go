package deploy

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/history"
	"github.com/nearmap/cvmanager/stats"
	"github.com/nearmap/cvmanager/verify"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

type BlueGreenDeployer struct {
	namespace string

	hp            history.Provider
	recordHistory bool

	cs       kubernetes.Interface
	recorder events.Recorder

	stats stats.Stats
}

func NewBlueGreenDeployer(cs kubernetes.Interface, eventRecorder events.Recorder, stats stats.Stats, namespace string) *BlueGreenDeployer {
	return &BlueGreenDeployer{
		namespace: namespace,
		cs:        cs,
		recorder:  eventRecorder,
		stats:     stats,
	}
}

func (bgd *BlueGreenDeployer) Deploy(cv *cv1.ContainerVersion, version string, spec DeploySpec) error {
	err := bgd.doDeploy(cv, version, spec)
	if err != nil {
		bgd.stats.Event(fmt.Sprintf("%s.sync.failure", spec.Name()),
			fmt.Sprintf("Failed to blue-green deploy cv spec %s with version %s", cv.Name, version), "", "error",
			time.Now().UTC())
		log.Printf("Failed to blue-green deploy cv spec %s: version=%v, workload=%v, error=%v",
			cv.Name, version, spec.Name(), err)
		bgd.recorder.Event(events.Warning, "CRSyncFailed", "Failed to blue-green deploy workload")
	}

	if bgd.recordHistory {
		err := bgd.hp.Add(bgd.namespace, spec.Name(), &history.Record{
			Type:    spec.Type(),
			Name:    spec.Name(),
			Version: version,
			Time:    time.Now(),
		})
		if err != nil {
			bgd.stats.IncCount(fmt.Sprintf("cvc.%s.history.save.failure", spec.Name()))
			bgd.recorder.Event(events.Warning, "SaveHistoryFailed", "Failed to record update history")
		}
	}

	log.Printf("Update for cv spec %s completed: workload=%v", cv.Name, spec.Name())
	bgd.stats.IncCount(fmt.Sprintf("%s.sync.success", spec.Name()))
	bgd.recorder.Event(events.Normal, "Success", "Updated completed successfully")
	return nil
}

func (bgd *BlueGreenDeployer) doDeploy(cv *cv1.ContainerVersion, version string, spec DeploySpec) error {
	tSpec, ok := spec.(TemplateDeploySpec)
	if !ok {
		return errors.Errorf("blue-green deployment not available for deploy spec %s of type %v. Falling back to simple deployer.", spec.Name(), spec.Type())
	}

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

	service, err := bgd.getService(cv, cv.Spec.Strategy.BlueGreen.ServiceName)
	if err != nil {
		return errors.Wrapf(err, "failed to find service for cv spec %s", cv.Name)
	}

	primary, secondary, err := bgd.getBlueGreenDeploySpecs(cv, tSpec, service)
	if err != nil {
		return errors.WithStack(err)
	}

	// if we're not the primary live version then nothing to do.
	if tSpec.Name() != primary.Name() {
		log.Printf("Spec %s is not the primary live workload for service %s. Not changing.", spec.Name(), service.Name)
		return nil
	}

	// if we're the primary live workload and our version mismatches then we want to initiate deployment
	// on the non-live workload.

	if err := bgd.updateVersion(cv, version, secondary); err != nil {
		return errors.Wrapf(err, "failed to patch pod spec for blue-green strategy %s", cv.Name)
	}

	if err := bgd.updateTestServiceSelector(cv, secondary); err != nil {
		return errors.Wrapf(err, "failed to update test service for cv spec %s", cv.Name)
	}

	if err := bgd.ensureHasPods(secondary); err != nil {
		return errors.WithStack(err)
	}

	if err := bgd.waitForAllPods(cv, version, secondary); err != nil {
		return errors.WithStack(err)
	}

	if err := bgd.verify(cv); err != nil {
		return errors.Wrapf(err, "failed verification step for cv spec %s", cv.Name)
	}

	if err := bgd.scaleUpSecondary(cv, primary, secondary); err != nil {
		return errors.WithStack(err)
	}

	if err := bgd.updateServiceSelector(cv, secondary, cv.Spec.Strategy.BlueGreen.ServiceName); err != nil {
		return errors.WithStack(err)
	}

	if cv.Spec.Strategy.BlueGreen.ScaleDown {
		if err := bgd.scaleDown(primary); err != nil {
			return errors.WithStack(err)
		}
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

func (bgd *BlueGreenDeployer) getBlueGreenDeploySpecs(cv *cv1.ContainerVersion, spec TemplateDeploySpec,
	service *corev1.Service) (current, secondary TemplateDeploySpec, err error) {

	// get all the workloads managed by this cv spec
	workloads, err := spec.Select(cv.Spec.Selector)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to get all workloads for cv spec %v", cv.Name)
	}
	if len(workloads) != 2 {
		return nil, nil, errors.Errorf("blue-green strategy requires exactly 2 workloads to be managed by a cv spec")
	}

	selector := labels.Set(service.Spec.Selector).AsSelector()
	for _, wl := range workloads {
		ptLabels := labels.Set(wl.PodTemplateSpec().Labels)
		if selector.Matches(ptLabels) {
			if current != nil {
				return nil, nil, errors.Errorf("unexpected state: found 2 current blue-green workloads for cv spec %s", cv.Name)
			}
			current = wl
		} else {
			if secondary != nil {
				return nil, nil, errors.Errorf("unexpected state: found 2 secondary blue-green workloads for cv spec %s", cv.Name)
			}
			secondary = wl
		}
	}

	return current, secondary, nil
}

func (bgd *BlueGreenDeployer) selectPodTemplates(selector map[string]string) ([]corev1.PodTemplate, error) {
	set := labels.Set(selector)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}

	podTemplates, err := bgd.cs.CoreV1().PodTemplates(bgd.namespace).List(listOpts)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to select pod templates with selector %s", selector)
	}

	return podTemplates.Items, nil
}

func (bgd *BlueGreenDeployer) updateVersion(cv *cv1.ContainerVersion, version string, spec DeploySpec) error {
	podSpec := spec.PodSpec()

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		for _, c := range podSpec.Containers {
			if c.Name == cv.Spec.Container {
				if updateErr := spec.PatchPodSpec(cv, c, version); updateErr != nil {
					log.Printf("Failed to update container version (will retry): version=%v, workload=%v, error=%v",
						version, spec.Name(), updateErr)
					return updateErr
				}
			}
		}
		return nil
	})
	if retryErr != nil {
		return errors.Wrapf(retryErr, "failed to patch pod spec for spec %s", spec.Name())
	}
	return nil
}

func (bgd *BlueGreenDeployer) updateTestServiceSelector(cv *cv1.ContainerVersion, spec TemplateDeploySpec) error {
	if cv.Spec.Strategy.BlueGreen.TestServiceName == "" {
		log.Printf("No test service defined for cv spec %s", cv.Name)
		return nil
	}

	if err := bgd.updateServiceSelector(cv, spec, cv.Spec.Strategy.BlueGreen.TestServiceName); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

func (bgd *BlueGreenDeployer) updateServiceSelector(cv *cv1.ContainerVersion, spec TemplateDeploySpec, serviceName string) error {
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

	log.Printf("Updating service %s with selector %s:%s", serviceName, labelName, targetLabel)

	// TODO: is update appropriate?
	if _, err := bgd.cs.CoreV1().Services(bgd.namespace).Update(testService); err != nil {
		return errors.Wrapf(err, "failed to update test service %s while processing blue-green deployment for %s",
			testService.Name, cv.Name)
	}

	return nil
}

func (bgd *BlueGreenDeployer) ensureHasPods(spec TemplateDeploySpec) error {
	// ensure at least 1 pod
	numReplicas := spec.NumReplicas()
	if numReplicas > 0 {
		log.Printf("DeploySpec %s has %d replicas", spec.Name(), numReplicas)
		return nil
	}

	if err := spec.PatchNumReplicas(1); err != nil {
		return errors.Wrapf(err, "failed to patch number of replicas for spec %s", spec.Name())
	}
	return nil
}

// waitForAllPods checks that all pods tied to the given TemplateDeploySpec are at the specified
// version, and starts polling if not the case.
// Returns an error if a timeout value is reached.
func (bgd *BlueGreenDeployer) waitForAllPods(cv *cv1.ContainerVersion, version string, spec TemplateDeploySpec) error {
	defer timeTrack(time.Now(), "waitForAllPods")
	timeout := time.Minute * 5
	if cv.Spec.Strategy.BlueGreen.TimeoutSecs > 0 {
		timeout = time.Second * time.Duration(cv.Spec.Strategy.BlueGreen.TimeoutSecs)
	}
	start := time.Now()

outer:
	for {
		if time.Now().After(start.Add(timeout)) {
			break
		}
		time.Sleep(15 * time.Second)

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
			if pod.Status.Phase != corev1.PodRunning {
				log.Printf("Still waiting for rollout: pod %s phase is %v", pod.Name, pod.Status.Phase)
				continue outer
			}
			if version == "" {
				continue
			}

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

func PodsForSpec(cs kubernetes.Interface, namespace string, spec TemplateDeploySpec) ([]corev1.Pod, error) {
	set := labels.Set(spec.PodTemplateSpec().Labels)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}

	pods, err := cs.CoreV1().Pods(namespace).List(listOpts)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to select pods for spec %s", spec.Name())
	}

	result, err := spec.SelectOwnPods(pods.Items)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to filter pods for DepoySpec %s", spec.Name())
	}

	return result, nil
}

func (bgd *BlueGreenDeployer) verify(cv *cv1.ContainerVersion) error {
	if cv.Spec.Strategy == nil || cv.Spec.Strategy.Verify == nil || cv.Spec.Strategy.Verify.Type == "" {
		log.Printf("No verification defined for %s", cv.Name)
		return nil
	}

	var verifier verify.Verifier
	switch cv.Spec.Strategy.Verify.Type {
	case verify.TypeImage:
		verifier = verify.NewImageVerifier(bgd.cs, bgd.recorder, bgd.stats, bgd.namespace, cv.Spec.Strategy.Verify)
	default:
		return errors.Errorf("unknown verify type: %v", cv.Spec.Strategy.Verify.Type)
	}

	return verifier.Verify()
}

func (bgd *BlueGreenDeployer) scaleUpSecondary(cv *cv1.ContainerVersion, current, secondary TemplateDeploySpec) error {
	currentNum := current.NumReplicas()
	secondaryNum := secondary.NumReplicas()

	if secondaryNum >= currentNum {
		log.Printf("Secondary spec %s has sufficient replicas (%d)", secondary.Name(), secondaryNum)
		return nil
	}

	if err := secondary.PatchNumReplicas(currentNum); err != nil {
		return errors.Wrapf(err, "failed to patch number of replicas for secondary spec %s", secondary.Name())
	}

	// TODO: is this the best way to ensure the secondary is fully scaled?
	if err := bgd.waitForAllPods(cv, "", secondary); err != nil {
		return errors.Wrapf(err, "failed to wait for all pods after scaling up secondary %s", secondary.Name())
	}
	return nil
}

func (bgd *BlueGreenDeployer) scaleDown(spec TemplateDeploySpec) error {
	if err := spec.PatchNumReplicas(0); err != nil {
		return errors.WithStack(err)
	}
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

func timeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	log.Printf("%s took %s", name, elapsed)
}
