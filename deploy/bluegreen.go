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

const (
	KindServieBlueGreen = "ServiceBlueGreen"
)

// BlueGreenDeployer is a Deployer that implements the blue-green rollout strategy.
type BlueGreenDeployer struct {
	namespace string

	hp            history.Provider
	recordHistory bool

	cs       kubernetes.Interface
	recorder events.Recorder

	stats stats.Stats
}

// NewBlueGreenDeployer returns a Deployer for performing blue-green rollouts.
func NewBlueGreenDeployer(cs kubernetes.Interface, eventRecorder events.Recorder, stats stats.Stats, namespace string) *BlueGreenDeployer {
	return &BlueGreenDeployer{
		namespace: namespace,
		cs:        cs,
		recorder:  eventRecorder,
		stats:     stats,
	}
}

// Deploy implements the Deployer interface.
func (bgd *BlueGreenDeployer) Deploy(cv *cv1.ContainerVersion, version string, target RolloutTarget) error {
	err := bgd.doDeploy(cv, version, target)
	if err != nil {
		bgd.stats.Event(fmt.Sprintf("%s.sync.failure", target.Name()),
			fmt.Sprintf("Failed to blue-green deploy cv spec %s with version %s", cv.Name, version), "", "error",
			time.Now().UTC())
		log.Printf("Failed to blue-green deploy cv target %s: version=%v, workload=%v, error=%v",
			cv.Name, version, target.Name(), err)
		bgd.recorder.Event(events.Warning, "CRSyncFailed", "Failed to blue-green deploy workload")
	}

	if bgd.recordHistory {
		err := bgd.hp.Add(bgd.namespace, target.Name(), &history.Record{
			Type:    target.Type(),
			Name:    target.Name(),
			Version: version,
			Time:    time.Now(),
		})
		if err != nil {
			bgd.stats.IncCount(fmt.Sprintf("crsyn.%s.history.save.failure", target.Name()))
			bgd.recorder.Event(events.Warning, "SaveHistoryFailed", "Failed to record update history")
		}
	}

	log.Printf("Update for cv spec %s completed: workload=%v", cv.Name, target.Name())
	bgd.stats.IncCount(fmt.Sprintf("crsyn.%s.sync.success", target.Name()))
	bgd.recorder.Event(events.Normal, "Success", "Updated completed successfully")
	return nil
}

func (bgd *BlueGreenDeployer) doDeploy(cv *cv1.ContainerVersion, version string, target RolloutTarget) error {
	tTarget, ok := target.(TemplateRolloutTarget)
	if !ok {
		return errors.Errorf("blue-green deployment not available for rollout target %s of type %v.",
			target.Name(), target.Type())
	}

	if cv.Spec.Strategy.BlueGreen == nil {
		return errors.Errorf("no blue-green spec provided for cv resource %s", cv.Name)
	}
	if cv.Spec.Strategy.BlueGreen.ServiceName == "" {
		return errors.Errorf("no service defined for blue-green strategy in cv resource %s", cv.Name)
	}
	if len(cv.Spec.Strategy.BlueGreen.LabelNames) == 0 {
		return errors.Errorf("no label names defined for blue-green strategy in cv resource %s", cv.Name)
	}

	log.Printf("Beginning blue-green deployment for target %s with version %s in namespace %s",
		target.Name(), version, bgd.namespace)
	defer timeTrack(time.Now(), "blue-green deployment")

	service, err := bgd.getService(cv, cv.Spec.Strategy.BlueGreen.ServiceName)
	if err != nil {
		return errors.Wrapf(err, "failed to find service for cv spec %s", cv.Name)
	}

	primary, secondary, err := bgd.getBlueGreenTargets(cv, tTarget, service)
	if err != nil {
		return errors.WithStack(err)
	}

	// if we're not the primary live version then nothing to do.
	if tTarget.Name() != primary.Name() {
		log.Printf("Spec %s is not the primary live workload for service %s. Not changing.", target.Name(), service.Name)
		return nil
	}

	// if we're the primary live workload and our version mismatches then we want to initiate deployment
	// on the non-live workload.

	if err := bgd.updateVersion(cv, version, secondary); err != nil {
		return errors.Wrapf(err, "failed to patch pod spec for blue-green strategy %s", cv.Name)
	}

	if err := bgd.updateVerificationServiceSelector(cv, secondary); err != nil {
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

func (bgd *BlueGreenDeployer) getBlueGreenTargets(cv *cv1.ContainerVersion, target TemplateRolloutTarget,
	service *corev1.Service) (current, secondary TemplateRolloutTarget, err error) {

	// get all the workloads managed by this cv spec
	workloads, err := target.Select(cv.Spec.Selector)
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

func (bgd *BlueGreenDeployer) updateVersion(cv *cv1.ContainerVersion, version string, target TemplateRolloutTarget) error {
	podSpec := target.PodSpec()

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		for _, c := range podSpec.Containers {
			if c.Name == cv.Spec.Container {
				if updateErr := target.PatchPodSpec(cv, c, version); updateErr != nil {
					log.Printf("Failed to update container version (will retry): version=%v, target=%v, error=%v",
						version, target.Name(), updateErr)
					return updateErr
				}
			}
		}
		return nil
	})
	if retryErr != nil {
		return errors.Wrapf(retryErr, "failed to patch pod spec for target %s", target.Name())
	}
	return nil
}

func (bgd *BlueGreenDeployer) updateVerificationServiceSelector(cv *cv1.ContainerVersion, target TemplateRolloutTarget) error {
	if cv.Spec.Strategy.BlueGreen.VerificationServiceName == "" {
		log.Printf("No test service defined for cv spec %s", cv.Name)
		return nil
	}

	if err := bgd.updateServiceSelector(cv, target, cv.Spec.Strategy.BlueGreen.VerificationServiceName); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

func (bgd *BlueGreenDeployer) updateServiceSelector(cv *cv1.ContainerVersion,
	target TemplateRolloutTarget, serviceName string) error {

	labelNames := cv.Spec.Strategy.BlueGreen.LabelNames

	testService, err := bgd.getService(cv, serviceName)
	if err != nil {
		return errors.Wrapf(err, "failed to find test service for cv spec %s", cv.Name)
	}

	for _, labelName := range labelNames {
		targetLabel, has := target.PodTemplateSpec().Labels[labelName]
		if !has {
			return errors.Errorf("pod template spec for target %s is missing label name %s in cv spec %s",
				target.Name(), labelName, cv.Name)
		}

		testService.Spec.Selector[labelName] = targetLabel
	}

	log.Printf("Updating service %s with selectors %v", serviceName, testService.Spec.Selector)

	// TODO: is update appropriate?
	if _, err := bgd.cs.CoreV1().Services(bgd.namespace).Update(testService); err != nil {
		return errors.Wrapf(err, "failed to update test service %s while processing blue-green deployment for %s",
			testService.Name, cv.Name)
	}

	return nil
}

func (bgd *BlueGreenDeployer) ensureHasPods(target TemplateRolloutTarget) error {
	// ensure at least 1 pod
	numReplicas := target.NumReplicas()
	if numReplicas > 0 {
		log.Printf("DeploySpec %s has %d replicas", target.Name(), numReplicas)
		return nil
	}

	if err := target.PatchNumReplicas(1); err != nil {
		return errors.Wrapf(err, "failed to patch number of replicas for target %s", target.Name())
	}
	return nil
}

// waitForAllPods checks that all pods tied to the given TemplateDeploySpec are at the specified
// version, and starts polling if not the case.
// Returns an error if a timeout value is reached.
func (bgd *BlueGreenDeployer) waitForAllPods(cv *cv1.ContainerVersion, version string,
	target TemplateRolloutTarget) error {

	defer timeTrack(time.Now(), "waitForAllPods")
	timeout := time.Minute * 5
	if cv.Spec.Strategy.BlueGreen.TimeoutSeconds > 0 {
		timeout = time.Second * time.Duration(cv.Spec.Strategy.BlueGreen.TimeoutSeconds)
	}
	start := time.Now()

	// TODO: use informer factory on pods.

outer:
	for {
		if time.Now().After(start.Add(timeout)) {
			break
		}
		time.Sleep(15 * time.Second)

		pods, err := PodsForTarget(bgd.cs, bgd.namespace, target)
		if err != nil {
			log.Printf("ERROR: failed to get pods for target %s: %v", target.Name(), err)
			// TODO: handle?
			continue outer
		}
		if len(pods) == 0 {
			// TODO: handle?
			log.Printf("ERROR: no pods found for target %s", target.Name())
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
				log.Printf("ERROR: failed to check container version for target %s: %v", target.Name(), err)
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

	return errors.Errorf("target %s blue-green deployment timeout waiting for all pods to be deployed", target.Name)
}

// PodsForTarget returns the pods managed by the given rollout target.
func PodsForTarget(cs kubernetes.Interface, namespace string, target TemplateRolloutTarget) ([]corev1.Pod, error) {
	set := labels.Set(target.PodTemplateSpec().Labels)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}

	pods, err := cs.CoreV1().Pods(namespace).List(listOpts)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to select pods for target %s", target.Name())
	}

	result, err := target.SelectOwnPods(pods.Items)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to filter pods for target %s", target.Name())
	}

	return result, nil
}

func (bgd *BlueGreenDeployer) verify(cv *cv1.ContainerVersion) error {
	if cv.Spec.Strategy == nil || cv.Spec.Strategy.Verify == nil || cv.Spec.Strategy.Verify.Kind == "" {
		log.Printf("No verification defined for %s", cv.Name)
		return nil
	}

	var verifier verify.Verifier
	switch cv.Spec.Strategy.Verify.Kind {
	case verify.KindImage:
		verifier = verify.NewImageVerifier(bgd.cs, bgd.recorder, bgd.stats, bgd.namespace, cv.Spec.Strategy.Verify)
	default:
		return errors.Errorf("unknown verify type: %v", cv.Spec.Strategy.Verify.Kind)
	}

	return verifier.Verify()
}

func (bgd *BlueGreenDeployer) scaleUpSecondary(cv *cv1.ContainerVersion, current, secondary TemplateRolloutTarget) error {
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

func (bgd *BlueGreenDeployer) scaleDown(spec TemplateRolloutTarget) error {
	if err := spec.PatchNumReplicas(0); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// CheckContainerVersions tests whether all containers in the pod spec with container
// names that match the cv spec have the given version.
// Returns false if at least one container's version does not match.
// TODO: put this somewhere more general
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
