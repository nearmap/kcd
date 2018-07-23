package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/nearmap/kcd/config"
	"github.com/nearmap/kcd/deploy"
	"github.com/nearmap/kcd/events"
	kcd1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	k8s "github.com/nearmap/kcd/gok8s/workload"
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

	k8sProvider     *k8s.Provider
	historyProvider history.Provider

	registry         registry.Registry // provides version information for the current kcd resource
	registryProvider registry.Provider // used to obtain version information for other registry resoures

	options *config.Options
}

// NewSyncer creates a Syncer instance for handling the main sync loop.
func NewSyncer(k8sProvider *k8s.Provider, kcd *kcd1.KCD, registryProvider registry.Provider,
	hp history.Provider, options ...func(*config.Options)) (*Syncer, error) {

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
		k8sProvider:      k8sProvider,
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
		glog.V(4).Info("Starting initial state")

		kcd, err := s.k8sProvider.CV(s.kcd.Name)
		if err != nil {
			return state.Error(errors.Wrapf(err, "failed to get KCD instance with name %s", s.kcd.Name))
		}
		s.kcd = kcd

		version, err := s.registry.Version(ctx, kcd.Spec.Tag)
		if err != nil {
			s.options.Recorder.Event(events.Warning, "KCDSyncFailed", "Failed to get version from registry")
			return state.Error(errors.Wrap(err, "failed to get version from registry"))
		}

		glog.V(4).Infof("Current registry version: %v", version)

		workloads, err := s.k8sProvider.Workloads(kcd)
		if err != nil {
			s.options.Recorder.Event(events.Warning, "KCDSyncFailed", "Failed to obtain workloads for kcd resource")
			return state.Error(errors.Wrapf(err, "failed to obtain workloads for kcd resource %s", kcd.Name))
		}

		if version == kcd.Status.CurrVersion && kcd.Status.CurrStatus == k8s.StatusFailed {
			glog.V(4).Infof("Not attempting %s rollout of version %s: %+v", kcd.Name, version, kcd.Status)
			return state.None()
		}

		var toUpdate []k8s.Workload
		for _, wl := range workloads {
			// if we're still progressing the rollout then add all workloads
			if version == kcd.Status.CurrVersion && kcd.Status.CurrStatus == k8s.StatusProgressing {
				toUpdate = append(toUpdate, wl)
				continue
			}

			// otherwise check current version vs expected version
			eq, err := k8s.CheckPodSpecKCDs(kcd, version, wl.PodSpec())
			if err != nil {
				return state.Error(errors.Wrapf(err, "failed to check podspec versions for kcd resource %s", kcd.Name))
			}
			if !eq {
				toUpdate = append(toUpdate, wl)
			}
		}

		glog.V(4).Infof("Found %d workloads to update", len(toUpdate))

		var states []state.State
		for _, wl := range toUpdate {
			st := s.verify(version,
				s.updateRolloutStatus(version, k8s.StatusProgressing,
					s.deploy(version, wl,
						s.successfulDeploymentStats(wl,
							s.syncVersionConfig(version,
								s.addHistory(version, wl,
									s.updateRolloutStatus(version, k8s.StatusSuccess, nil)))))))

			states = append(states, state.WithFailure(st, s.handleFailure(wl, version)))
		}

		return state.Many(states...)
	}
}

// handleFailure is a state invoked when a sync permanently fails. It is responsible for updating
// the rollout status and generating relevant stats and events.
func (s *Syncer) handleFailure(workload k8s.Workload, version string) state.OnFailureFunc {
	return func(ctx context.Context, err error) {
		glog.V(1).Infof("Failed to process container version: version=%v, target=%v, error=%v",
			version, workload.Name(), err)

		s.options.Stats.Event(fmt.Sprintf("%s.sync.failure", workload.Name()),
			fmt.Sprintf("Failed to validate image with %s", version), "", "error",
			time.Now().UTC())
		s.options.Recorder.Event(events.Warning, "KCDSyncFailed", "Failed to deploy the target")

		_, uErr := s.k8sProvider.UpdateRolloutStatus(s.kcd.Name, version, k8s.StatusFailed, time.Now().UTC())
		if uErr != nil {
			glog.Errorf("Failed to update kcd %s status as failed rollout for version %s: %v", s.kcd.Name, version, uErr)
			// TODO: something else?
		}
	}
}

func (s *Syncer) verify(version string, next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		if version == s.kcd.Status.CurrVersion && s.kcd.Status.CurrStatus == k8s.StatusProgressing {
			// we've already run the verify step
			return state.Single(next)
		}

		return state.Single(
			verify.NewVerifiers(s.k8sProvider.Client(), s.registryProvider, s.k8sProvider.Namespace(),
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

		kcd, err := s.k8sProvider.UpdateRolloutStatus(s.kcd.Name, version, status, time.Now().UTC())
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
func (s *Syncer) deploy(version string, target deploy.RolloutTarget, next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		glog.V(4).Info("creating new deployer state")

		return state.Single(
			deploy.NewDeployState(s.k8sProvider.Client(), s.registryProvider, s.k8sProvider.Namespace(), s.kcd, version, target, next))
	}
}

// successfulDeploymentStats generates stats for a successful rollout.
func (s *Syncer) successfulDeploymentStats(workload k8s.Workload, next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		glog.V(4).Info("Updating stats for successful deployment")

		s.options.Stats.IncCount(fmt.Sprintf("crsyn.%s.sync.success", workload.Name()))
		s.options.Recorder.Eventf(events.Normal, "Success", "%s updated completed successfully", workload.Name())
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

		client := s.k8sProvider.Client()
		namespace := s.k8sProvider.Namespace()

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

// addHistory adds the successful rollout of the target to the given version to the
// history provider.
func (s *Syncer) addHistory(version string, target deploy.RolloutTarget, next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		if !s.kcd.Spec.History.Enabled {
			glog.V(4).Infof("Not adding version history for kcd=%s, version=%s", s.kcd.Name, version)
			return state.Single(next)
		}

		name := s.kcd.Spec.History.Name
		if name == "" {
			name = target.Name()
		}

		glog.V(4).Infof("Adding version history for kcd=%s, name=%s, version=%s", s.kcd.Name, name, version)

		err := s.historyProvider.Add(s.k8sProvider.Namespace(), name, &history.Record{
			Type:    target.Type(),
			Name:    target.Name(),
			Version: version,
			Time:    time.Now().UTC(),
		})
		if err != nil {
			s.options.Stats.IncCount(fmt.Sprintf("crsyn.%s.history.save.failure", target.Name()))
			s.options.Recorder.Event(events.Warning, "SaveHistoryFailed", "Failed to record update history")
		}

		return state.Single(next)
	}
}
