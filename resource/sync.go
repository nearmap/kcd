package resource

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/nearmap/kcd/config"
	"github.com/nearmap/kcd/deploy"
	"github.com/nearmap/kcd/events"
	kcd1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	"github.com/nearmap/kcd/gok8s/workload"
	"github.com/nearmap/kcd/history"
	"github.com/nearmap/kcd/registry"
	"github.com/nearmap/kcd/state"
	"github.com/nearmap/kcd/verify"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Syncer is responsible for handling the main sync loop.
type Syncer struct {
	machine *state.Machine

	kcd *kcd1.KCD

	resourceProvider Provider
	workloadProvider workload.Provider
	historyProvider  history.Provider

	registry         registry.Registry // provides version information for the current kcd resource
	registryProvider registry.Provider // used to obtain version information for other registry resoures

	options *config.Options
}

// NewSyncer creates a Syncer instance for handling the main sync loop.
func NewSyncer(resourceProvider Provider, workloadProvider workload.Provider, registryProvider registry.Provider,
	hp history.Provider, kcd *kcd1.KCD, options ...func(*config.Options)) (*Syncer, error) {

	opts := config.NewOptions()
	for _, opt := range options {
		opt(opts)
	}

	dur := time.Duration(kcd.Spec.PollIntervalSeconds) * time.Second
	glog.V(1).Infof("Syncing every %s", dur)

	opTimeout := time.Minute * 15
	if kcd.Spec.TimeoutSeconds > 0 {
		opTimeout = time.Second * time.Duration(kcd.Spec.TimeoutSeconds)
	}

	registry, err := registryProvider.RegistryFor(kcd.Spec.ImageRepo)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	s := &Syncer{
		resourceProvider: resourceProvider,
		workloadProvider: workloadProvider,
		kcd:              kcd,
		registryProvider: registryProvider,
		registry:         registry,
		historyProvider:  hp,
		options:          opts,
	}
	s.machine = state.NewMachine(s.initialState(), state.WithStartWaitTime(dur), state.WithTimeout(opTimeout))
	return s, nil
}

// Start begins the sync process.
func (s *Syncer) Start() {
	s.machine.Start()
}

// Stop shuts down the sync operation.
func (s *Syncer) Stop() error {
	return s.machine.Stop()
}

// initialState returns a state that starts a sync process.
func (s *Syncer) initialState() state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		if glog.V(4) {
			glog.V(4).Infof("Starting initial state: kcd=%+v", s.kcd)
		}

		kcd, err := s.resourceProvider.KCD(s.kcd.Namespace, s.kcd.Name)
		if err != nil {
			glog.Errorf("Syncer failed to obtain KCD resource with name %s: %v", s.kcd.Name, err)
			return state.Error(errors.Wrapf(err, "failed to obtain KCD instance with name %s", s.kcd.Name))
		}

		// refresh kcd state
		s.kcd = kcd

		version, err := s.registry.Version(ctx, s.kcd.Spec.Tag)
		if err != nil {
			glog.Errorf("Syncer failed to get version from registry, kcd=%s, tag=%s: %v", s.kcd.Name, kcd.Spec.Tag, err)
			s.options.Recorder.Event(events.Warning, "KCDSyncFailed", "Failed to get version from registry")
			return state.Error(errors.Wrap(err, "failed to get version from registry"))
		}

		if glog.V(4) {
			glog.V(4).Infof("Got registry version for kcd=%s, tag=%s, version=%v", s.kcd.Name, kcd.Spec.Tag, version)
		}

		/*
			workloads, err := s.workloadProvider.Workloads(kcd)
			if err != nil {
				s.options.Recorder.Event(events.Warning, "KCDSyncFailed", "Failed to obtain workloads for kcd resource")
				return state.Error(errors.Wrapf(err, "failed to obtain workloads for kcd resource %s", kcd.Name))
			}
			if len(workloads) == 0 {
				s.options.Recorder.Event(events.Warning, "KCDSyncFailed", fmt.Sprintf("No workloads found for kcd resource %s", kcd.Name))
				return state.Error(errors.Wrapf(err, "no workloads found for kcd resource %s", kcd.Name))
			}
		*/

		deployer, err := deploy.New(s.workloadProvider, s.registryProvider, s.kcd, version)
		if err != nil {
			glog.Errorf("Failed to create deployer for kcd=%s: %v", s.kcd.Name, err)
			return state.Error(errors.Wrap(err, "failed to create deployer"))
		}

		process, err := s.shouldProcess(deployer, s.kcd, version)
		if err != nil {
			glog.Errorf("Failed to determine whether workloads should be processed for kcd=%s: %v", s.kcd.Name, err)
			return state.Error(errors.Wrapf(err, "failed to determine whether workloads should be processed for kcd=%s", s.kcd.Name))
		}
		if !process {
			glog.V(4).Infof("Not attempting %s rollout of version %s: %+v", s.kcd.Name, version, s.kcd.Status)
			return state.None()
		}

		glog.V(4).Infof("Creating rollout state for kcd=%s", s.kcd.Name)

		syncState := s.verify(version,
			s.updateRolloutStatus(version, StatusProgressing,
				s.deploy(deployer,
					s.successfulDeploymentStats(
						s.syncVersionConfig(version,
							s.addHistory(deployer, version,
								s.updateRolloutStatus(version, StatusSuccess, nil)))))))

		return state.Single(state.WithFailure(syncState, s.handleFailure(version)))
	}
}

// shouldProcess returns whether a rollout should be performed on the workloads defined
// by the KCD resource.
func (s *Syncer) shouldProcess(deployer deploy.Deployer, kcd *kcd1.KCD, version string) (bool, error) {
	if version != kcd.Status.CurrVersion {
		return true, nil
	}

	// versions to compare workloads against. If at least one workload is not at one of the
	// specified versions then we process the deployment.
	var versions []string

	switch kcd.Status.CurrStatus {
	case StatusProgressing:
		// always process when status is progressing.
		return true, nil
	case StatusFailed:
		// for failed case we don't process when workloads are at current or previously successful versions
		versions = []string{kcd.Status.CurrVersion, kcd.Status.SuccessVersion}
	default:
		// otherwise we process when workloads are not at current version
		versions = []string{kcd.Status.CurrVersion}
	}

	for _, wl := range deployer.Workloads() {
		ok, err := workload.CheckPodSpecVersion(wl.PodSpec(), kcd, versions...)
		if err != nil {
			return false, errors.Wrapf(err, "failed to check pod spec version for kcd=%v, wlname=%v", kcd.Name, wl.Name())
		}
		if !ok {
			return true, nil
		}
	}
	return false, nil
}

// handleFailure is a state invoked when a sync permanently fails. It is responsible for updating
// the rollout status and generating relevant stats and events.
func (s *Syncer) handleFailure(version string) state.OnFailureFunc {
	return func(ctx context.Context, err error) {
		glog.V(1).Infof("Failed to process kcd=%v, version=%v, error=%v", s.kcd.Name, version, err)

		s.options.Stats.Event("kcdsync.failure",
			fmt.Sprintf("Failed to deploy %s with version %s", s.kcd.Name, version), "", "error",
			time.Now().UTC(), s.kcd.Name)
		s.options.Recorder.Event(events.Warning, "KCDSyncFailed", "Failed to deploy the target")

		_, uErr := s.resourceProvider.UpdateStatus(s.kcd.Namespace, s.kcd.Name, version, StatusFailed, time.Now().UTC())
		if uErr != nil {
			glog.Errorf("Failed to update kcd %s status as failed rollout for version %s: %v", s.kcd.Name, version, uErr)
			// TODO: something else?
		}
	}
}

func (s *Syncer) verify(version string, next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		if version == s.kcd.Status.CurrVersion && s.kcd.Status.CurrStatus == StatusProgressing {
			// we've already run the verify step
			return state.Single(next)
		}

		return state.Single(
			verify.NewVerifiers(s.workloadProvider.Client(), s.registryProvider, s.workloadProvider.Namespace(),
				version, s.kcd.Spec.Container.Verify, next))
	}
}

// updateRolloutStatus updates the KCD resource status with the given version and status
// value and sets the current kcd instance in the syncer with the updated values.
func (s *Syncer) updateRolloutStatus(version, status string, next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		if version == s.kcd.Status.CurrVersion && status == s.kcd.Status.CurrStatus {
			return state.Single(next)
		}

		glog.V(2).Infof("Updating rollout status: kcd=%s, version=%s, status=%s", s.kcd.Name, version, status)

		kcd, err := s.resourceProvider.UpdateStatus(s.kcd.Namespace, s.kcd.Name, version, status, time.Now().UTC())
		if err != nil {
			glog.Errorf("Failed to update Rollout Status status for kcd=%s, version=%s, status=%s: %v", s.kcd.Name, version, status, err)
			events.FromContext(ctx).Event(events.Warning, "FailedUpdateRolloutStatus", "Failed to update version status")
			return state.Error(errors.Wrapf(err, "failed to update Rollout status for kcd=%s, version=%s, status=%s",
				s.kcd.Name, version, status))
		}

		s.kcd = kcd
		return state.Single(next)
	}
}

// deploy the rollout target to the given version according to the deployment strategy
// defined in the kcd definition.
func (s *Syncer) deploy(deployer deploy.Deployer, next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		glog.V(4).Info("creating new deployer state")

		return state.Single(deployer.AsState(next))
	}
}

// successfulDeploymentStats generates stats for a successful rollout.
func (s *Syncer) successfulDeploymentStats(next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		glog.V(4).Info("Updating stats for successful deployment")

		s.options.Stats.IncCount("kcdsync.success", s.kcd.Name)
		s.options.Recorder.Eventf(events.Normal, "Success", "%s completed successfully", s.kcd.Name)
		return state.Single(next)
	}
}

// syncVersionConfig syncs the config map referenced by CV resource - creates if absent and updates if required
// The controller is not responsible for managing the config resource it reference but only for updating
// and ensuring its present. If the reference to config was removed from CV resource its not the responsibility
// of controller to remove it .. it assumes the configMap is external resource and not owned by kcd resource
func (s *Syncer) syncVersionConfig(version string, next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		kcd := s.kcd
		glog.V(4).Infof("syncVersionConfig: kcd=%s, version=%s", kcd.Name, version)

		if kcd.Spec.Config == nil {
			return state.Single(next)
		}

		client := s.workloadProvider.Client()
		namespace := s.workloadProvider.Namespace()

		cm, err := client.CoreV1().ConfigMaps(namespace).Get(kcd.Spec.Config.Name, metav1.GetOptions{})
		if err != nil {
			if k8serr.IsNotFound(err) {
				_, err = client.CoreV1().ConfigMaps(namespace).Create(
					newVersionConfig(namespace, kcd.Spec.Config.Name, kcd.Spec.Config.Key, version))
				if err != nil {
					events.FromContext(ctx).Event(events.Warning, "FailedCreateVersionConfigMap", "Failed to create version configmap")
					return state.Error(errors.Wrapf(err, "failed to create version configmap from %s/%s:%s",
						namespace, kcd.Spec.Config.Name, kcd.Spec.Config.Key))
				}
				return state.Single(next)
			}
			return state.Error(errors.Wrapf(err, "failed to get version configmap from %s/%s:%s",
				namespace, kcd.Spec.Config.Name, kcd.Spec.Config.Key))
		}

		if version == cm.Data[kcd.Spec.Config.Key] {
			return state.Single(next)
		}

		cm.Data[kcd.Spec.Config.Key] = version

		// TODO enable this when patchstretegy is supported on config map https://github.com/kubernetes/client-go/blob/7ac1236/pkg/api/v1/types.go#L3979
		// _, err = s.k8sClient.CoreV1().ConfigMaps(s.namespace).Patch(cm.ObjectMeta.Name, types.StrategicMergePatchType, []byte(fmt.Sprintf(`{
		// 	"Data": {
		// 		"%s": "%s",
		// 	},
		// }`, s.Config.ConfigMap.Key, version)))
		_, err = client.CoreV1().ConfigMaps(namespace).Update(cm)
		if err != nil {
			events.FromContext(ctx).Event(events.Warning, "FailedUpdateVersionConfigMap", "Failed to update version configmap")
			return state.Error(errors.Wrapf(err, "failed to update version configmap from %s/%s:%s",
				namespace, kcd.Spec.Config.Name, kcd.Spec.Config.Key))
		}
		return state.Single(next)
	}
}

// newVersionConfig creates a new configmap for a version if specified in CV resource.
func newVersionConfig(namespace, name, key, version string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			key: version,
		},
	}
}

// addHistory adds the successful rollout of the targets to the given version to the
// history provider.
func (s *Syncer) addHistory(deployer deploy.Deployer, version string, next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		if !s.kcd.Spec.History.Enabled {
			glog.V(4).Infof("Not adding version history for kcd=%s, version=%s", s.kcd.Name, version)
			return state.Single(next)
		}

		for _, target := range deployer.Workloads() {
			// TODO: remove this spec field???
			//name := s.kcd.Spec.History.Name
			//if name == "" {
			//	name = target.Name()
			//}
			name := target.Name()

			glog.V(4).Infof("Adding version history for kcd=%s, name=%s, version=%s", s.kcd.Name, name, version)

			err := s.historyProvider.Add(s.workloadProvider.Namespace(), name, &history.Record{
				Type:    target.Type(),
				Name:    target.Name(),
				Version: version,
				Time:    time.Now().UTC(),
			})
			if err != nil {
				glog.Error("Failed to save history: %v", err)
				s.options.Recorder.Event(events.Warning, "SaveHistoryFailed", "Failed to record update history")
			}
		}

		return state.Single(next)
	}
}
