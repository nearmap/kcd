package sync

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nearmap/cvmanager/verify"

	"github.com/nearmap/cvmanager/config"
	"github.com/nearmap/cvmanager/deploy"
	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	k8s "github.com/nearmap/cvmanager/gok8s/workload"
	"github.com/nearmap/cvmanager/registry"
	"github.com/nearmap/cvmanager/state"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Syncer struct {
	machine *state.Machine

	k8sProvider *k8s.Provider
	cv          *cv1.ContainerVersion
	registry    registry.Registry
	options     *config.Options
}

func (s *Syncer) Stop() error {
	return s.machine.Stop()
}

func NewSyncer(k8sProvider *k8s.Provider, cv *cv1.ContainerVersion, reg registry.Registry, options ...func(*config.Options)) *Syncer {
	opts := config.NewOptions()
	for _, opt := range options {
		opt(opts)
	}

	dur := time.Duration(cv.Spec.PollIntervalSeconds) * time.Second
	log.Printf("Syncing every %s", dur)

	s := &Syncer{
		k8sProvider: k8sProvider,
		cv:          cv,
		registry:    reg,
		options:     opts,
	}
	s.machine = state.NewMachine(s)
	return s
}

func (s *Syncer) Do(ctx context.Context) (state.States, error) {
	version, err := s.registry.Version(s.cv.Spec.Tag)
	if err != nil {
		s.options.Recorder.Event(events.Warning, "CRSyncFailed", "Failed to get version from registry")
		return state.Error(errors.Wrap(err, "failed to get version from registry"))
	}

	workloads, err := s.k8sProvider.Workloads(s.cv)
	if err != nil {
		s.options.Recorder.Event(events.Warning, "CRSyncFailed", "Failed to obtain workloads for cv resource")
		return state.Error(errors.Wrapf(err, "failed to obtain workloads for cv resource %s", s.cv.Name))
	}

	var toUpdate []k8s.Workload
	for _, wl := range workloads {
		currVersion, err := s.containerVersion(wl)
		if err != nil {
			return state.Error(errors.Wrapf(err, "failed to obtain container version for workload %s", wl.Name()))
		}
		if currVersion != version {
			toUpdate = append(toUpdate, wl)
		}
	}

	var states []state.State
	for _, wl := range toUpdate {
		st := verify.NewVerifiers(s.k8sProvider.Client(), s.k8sProvider.Namespace(), version, s.cv.Spec.Container.Verify,
			deploy.NewDeployState(s.k8sProvider.Client(), s.k8sProvider.Namespace(), s.cv, version, wl,
				s.successfulDeploymentStats(wl,
					s.syncVersionConfig(version, nil))))

		// TODO: add steps to handle error

		states = append(states, st)
	}

	return state.Many(states...)
}

func (s *Syncer) containerVersion(workload k8s.Workload) (string, error) {
	for _, c := range workload.PodSpec().Containers {
		if c.Name == s.cv.Spec.Container.Name {
			parts := strings.SplitN(c.Image, ":", 2)
			if len(parts) > 2 {
				return "", errors.New("invalid image on container: '%s', c.Image")
			}
			if parts[0] != s.cv.Spec.ImageRepo {
				return "", errors.Errorf("Repository mismatch present: %s and requested %s don't match",
					parts[0], s.cv.Spec.ImageRepo)
			}
			return parts[1], nil
		}
	}

	return "", errors.Errorf("no container of name %s was found in workload %s", s.cv.Spec.Container.Name, workload.Name())
}

func (s *Syncer) successfulDeploymentStats(workload k8s.Workload, next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		s.options.Stats.IncCount(fmt.Sprintf("crsyn.%s.sync.success", workload.Name()))
		s.options.Recorder.Eventf(events.Normal, "Success", "%s updated completed successfully", workload.Name())
		return state.Single(next)
	}
}

// syncVersionConfig sync the config map referenced by CV resource - creates if absent and updates if required
// The controller is not responsible for managing the config resource it reference but only for updating
// and ensuring its present. If the reference to config was removed from CV resource its not the responsibility
// of controller to remove it .. it assumes the configMap is external resource and not owned by cv resource
func (s *Syncer) syncVersionConfig(version string, next state.State) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		if s.cv.Spec.Config == nil {
			return state.Single(next)
		}

		client := s.k8sProvider.Client()
		namespace := s.k8sProvider.Namespace()

		cm, err := client.CoreV1().ConfigMaps(namespace).Get(s.cv.Spec.Config.Name, metav1.GetOptions{})
		if err != nil {
			if k8serr.IsNotFound(err) {
				_, err = client.CoreV1().ConfigMaps(namespace).Create(
					newVersionConfig(namespace, s.cv.Spec.Config.Name, s.cv.Spec.Config.Key, version))
				if err != nil {
					events.FromContext(ctx).Event(events.Warning, "FailedCreateVersionConfigMap", "Failed to create version configmap")
					return state.Error(errors.Wrapf(err, "failed to create version configmap from %s/%s:%s",
						namespace, s.cv.Spec.Config.Name, s.cv.Spec.Config.Key))
				}
				return state.Single(next)
			}
			return state.Error(errors.Wrapf(err, "failed to get version configmap from %s/%s:%s",
				namespace, s.cv.Spec.Config.Name, s.cv.Spec.Config.Key))
		}

		if version == cm.Data[s.cv.Spec.Config.Key] {
			return state.Single(next)
		}

		cm.Data[s.cv.Spec.Config.Key] = version

		// TODO enable this when patchstretegy is supported on config map https://github.com/kubernetes/client-go/blob/7ac1236/pkg/api/v1/types.go#L3979
		// _, err = s.k8sClient.CoreV1().ConfigMaps(s.namespace).Patch(cm.ObjectMeta.Name, types.StrategicMergePatchType, []byte(fmt.Sprintf(`{
		// 	"Data": {
		// 		"%s": "%s",
		// 	},
		// }`, s.Config.ConfigMap.Key, version)))
		_, err = client.CoreV1().ConfigMaps(namespace).Update(cm)
		if err != nil {
			events.FromContext(ctx).Event(events.Warning, "FailedUpdateVersionConfigMao", "Failed to update version configmap")
			return state.Error(errors.Wrapf(err, "failed to update version configmap from %s/%s:%s",
				namespace, s.cv.Spec.Config.Name, s.cv.Spec.Config.Key))
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

/*
func (s *Syncer) validate(v string, cvvs []*cv1.VerifySpec) error {
	for _, v := range cvvs {
		verifier, err := verify.NewVerifier(k.cs, k.options.Recorder, k.options.Stats, k.namespace, v)
		if err != nil {
			return errors.WithStack(err)
		}
		if err = verifier.Verify(); err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}
*/

/*
func (s *Syncer) newDeployer(cv *cv1.ContainerVersion) deploy.Deployer {
	var kind string
	if cv.Spec.Strategy != nil {
		kind = cv.Spec.Strategy.Kind
	}

	switch kind {
	case deploy.KindServieBlueGreen:
		return deploy.NewBlueGreenDeployer(s.k8sProvider.Client(), s.k8sProvider.Namespace(), config.WithOptions(s.options))
	default:
		return deploy.NewSimpleDeployer(s.k8sProvider.Namespace(), config.WithOptions(s.options))
	}
}
*/
/*
func (s *Syncer) newVerifyState(version string, cvvs []*cv1.VerifySpec, idx int) state.StateFunc {
	return func(ctx context.Context) (State, error) {
		if idx >= len(cvvs) {
			// TODO: move onto next state
			return nil, nil
		}

		return verify.NewVerifier(s.client, s.namespace, cvvs[idx], s.newVerifyState(version, cvvs, idx++),
			config.WithOptions(s.options)), nil
	}
}
*/