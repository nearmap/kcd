package registry

import (
	"fmt"
	"log"
	"time"

	conf "github.com/nearmap/cvmanager/config"
	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	k8s "github.com/nearmap/cvmanager/gok8s/workload"
	"github.com/pkg/errors"
	"k8s.io/client-go/kubernetes"
)

// syncer is responsible to syncing with the ecr repository and
// ensuring that the deployment it is monitoring is up to date. If it finds
// the deployment outdated from what Tag is indicating the deployment version should be.
// it performs an update in deployment which then based on update strategy of deployment
// is further rolled out.
// In cases, where it cant resolves
type syncer struct {
	k8sProvider *k8s.K8sProvider
	namespace   string
	cv          *cv1.ContainerVersion
	recorder    events.Recorder

	opts *conf.Options

	registry Registry
}

// NewSyncer provides new reference of dhSyncer
// to manage AWS ECR repository and sync deployments periodically
func NewSyncer(cs *kubernetes.Clientset, cv *cv1.ContainerVersion, ns string,
	registry Registry, options ...func(*conf.Options)) *syncer {

	opts := conf.NewOptions()
	for _, opt := range options {
		opt(opts)
	}

	recorder := events.PodEventRecorder(cs, ns)

	k8sProvider := k8s.NewK8sProvider(cs, ns, recorder, conf.WithStats(opts.Stats),
		conf.WithHistory(opts.UseHistory),
		conf.WithUseRollback(opts.UseRollback))

	syncer := &syncer{
		k8sProvider: k8sProvider,
		namespace:   ns,
		cv:          cv,
		registry:    registry,
		opts:        opts,
		recorder:    recorder,
	}

	return syncer
}

// Sync starts the periodic action of checking with ECR repository
// and acting if differences are found. In case of different expected
// version is identified, deployment roll-out is performed
func (s *syncer) Sync() error {
	log.Printf("Beginning sync....at every %dm", s.cv.Spec.PollIntervalSeconds)
	d, _ := time.ParseDuration(fmt.Sprintf("%dm", s.cv.Spec.PollIntervalSeconds))
	for range time.Tick(d) {
		if err := SetSyncStatus(); err != nil {
			return err
		}
		if err := s.doSync(); err != nil {
			return err
		}
	}
	return nil
}

func (s *syncer) doSync() error {
	newVersion, err := s.registry.Version(s.cv.Spec.Tag)
	if err != nil {
		s.recorder.Event(events.Warning, "CRSyncFailed", "Failed to get latest version from registry")
		return err
	}
	// Check deployment
	if err := s.k8sProvider.SyncWorkload(s.cv, newVersion); err != nil {
		s.recorder.Event(events.Warning, "CRSyncFailed", "failed to sync config")
		return errors.Wrapf(err, "Failed to sync deployments %s", s.cv.Spec.Selector[cv1.CVAPP])
	}

	// Syncup config if unspecified
	if err := s.k8sProvider.SyncVersionConfig(s.cv, newVersion); err != nil {
		log.Printf("Failed sync config: %v", err)
		s.recorder.Event(events.Warning, "CRSyncFailed", "failed to sync config")
		s.opts.Stats.IncCount(fmt.Sprintf("%s.%s.configsyn.failure", s.namespace, s.cv.Spec.Config.Name))
		return errors.Wrapf(err, "Failed to sync config version %s", s.cv.Spec.Selector[cv1.CVAPP])
	}

	return nil
}
