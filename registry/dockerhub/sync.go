package dockerhub

import (
	"fmt"
	"log"
	"time"

	"github.com/heroku/docker-registry-client/registry"
	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	k8s "github.com/nearmap/cvmanager/gok8s/workload"
	rgs "github.com/nearmap/cvmanager/registry"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	"k8s.io/client-go/kubernetes"
)

const dockerhubURL = "https://registry-1.docker.io/"

// syncer is responsible to syncing with the docker registry (dr) repository and
// ensuring that the deployment it is monitoring is up to date. If it finds
// the deployment outdated from what Tag is indicating the deployment version should be.
// it performs an update in deployment which then based on update strategy of deployment
// is further rolled out.
// In cases, where it cant resolves
// TODO: Dockerhub syncer does not support private repositories at the moment
// Need to work on storing credentials in secret or similar more secure approach
// for user/password. For now uses anonymous access
type syncer struct {
	k8sProvider *k8s.K8sProvider
	namespace   string
	cv          *cv1.ContainerVersion

	stats stats.Stats
}

// NewSyncer provides new reference of syncer
// to manage Dockerhub repository and sync deployments periodically
func NewSyncer(cs *kubernetes.Clientset, ns string, cv *cv1.ContainerVersion,
	stats stats.Stats, recordHistory bool) (*syncer, error) {

	k8sProvider := k8s.NewK8sProvider(cs, ns, stats, recordHistory)

	syncer := &syncer{
		k8sProvider: k8sProvider,
		namespace:   ns,
		cv:          cv,

		stats: stats,
	}

	return syncer, nil
}

// Sync starts the periodic action of checking with docker repository
// and acting if differences are found. In case of different expected
// version is identified, deployment roll-out is performed
func (s *syncer) Sync() error {
	log.Printf("Beginning sync....at every %dm", s.cv.Spec.CheckFrequency)
	d, _ := time.ParseDuration(fmt.Sprintf("%dm", s.cv.Spec.CheckFrequency))
	for range time.Tick(d) {
		if err := rgs.SetSyncStatus(); err != nil {
			return err
		}
		if err := s.doSync(); err != nil {
			return err
		}
	}
	return nil
}

func (s *syncer) doSync() error {
	// TODO this need more work, only allows anonymous access at the moment
	currentVersion, err := getDigest(s.cv.Spec.ImageRepo, "", "", s.cv.Spec.Tag)
	if err != nil {
		s.stats.IncCount(fmt.Sprintf("%s.%s.badsha.failure", s.cv.Spec.ImageRepo, s.cv.Spec.Selector[cv1.CVAPP]))
		s.k8sProvider.Recorder.Event(events.Warning, "CRSyncFailed", "No image found with correct tags")
		return nil
	}
	if err := s.k8sProvider.SyncWorkload(s.cv, currentVersion); err != nil {
		return errors.Wrapf(err, "Failed to sync deployments %s", s.cv.Spec.Selector[cv1.CVAPP])
	}

	if err := s.k8sProvider.SyncVersionConfig(s.cv, currentVersion); err != nil {
		log.Printf("Failed sync config: %v", err)
		s.stats.IncCount(fmt.Sprintf("%s.configsyn.failure", s.cv.Spec.Config.Name))
		return errors.Wrapf(err, "Failed to sync config version %s", s.cv.Spec.Selector[cv1.CVAPP])
	}
	return nil
}

// getDigest fetches the digest of dockerhub image of requested repository and tag
func getDigest(repo, username, pwd, tag string) (string, error) {
	hub, err := registry.New(dockerhubURL, username, pwd)
	if err != nil {
		return "", errors.Wrap(err, "Failed to connect to dockerhun")
	}
	digest, err := hub.ManifestDigest(repo, tag)
	if err != nil {
		return "", errors.Wrapf(err, "Failed to get tag %s on repository %s", tag, repo)
	}
	return digest.String(), nil
}
