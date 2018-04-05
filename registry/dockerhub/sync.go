package dockerhub

import (
	"fmt"
	"log"
	"time"

	"github.com/heroku/docker-registry-client/registry"
	"github.com/nearmap/cvmanager/registry/config"
	"github.com/nearmap/cvmanager/registry/k8s"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

const dockerhubURL = "https://registry-1.docker.io/"

// dhSyncer is responsible to syncing with the docker registry (dr) repository and
// ensuring that the deployment it is monitoring is up to date. If it finds
// the deployment outdated from what Tag is indicating the deployment version should be.
// it performs an update in deployment which then based on update strategy of deployment
// is further rolled out.
// In cases, where it cant resolves
type dhSyncer struct {
	Config *config.SyncConfig

	namespace string

	k8sProvider *k8s.K8sProvider

	stats stats.Stats
}

// NewSyncer provides new reference of dhSyncer
// to manage Dockerhub repository and sync deployments periodically
func NewSyncer(cs *kubernetes.Clientset, ns string,
	sc *config.SyncConfig,
	stats stats.Stats) (*dhSyncer, error) {

	if !sc.Validate() {
		return nil, errors.Errorf("Invalid sync configurations found %v", sc)
	}

	var err error
	k8sProvider, err := k8s.NewK8sProvider(cs, ns, stats)
	if err != nil {
		return nil, errors.Wrap(err, "Could not initialize k8s client/provider")
	}
	sc.ConfigMap, err = config.ConfigMap(ns, sc.ConfigKey)
	if err != nil {
		k8sProvider.Recorder.Event(k8sProvider.Pod, corev1.EventTypeWarning, "ConfigKeyMisconfigiured", "ConfigKey is misconfigured")
		return nil, errors.Wrap(err, "bad config key")
	}

	syncer := &dhSyncer{
		Config:    sc,
		namespace: ns,

		k8sProvider: k8sProvider,
		stats:       stats,
	}

	return syncer, nil
}

// Sync starts the periodic action of checking with docker repository
// and acting if differences are found. In case of different expected
// version is identified, deployment roll-out is performed
func (s *dhSyncer) Sync() error {
	log.Printf("Beginning sync....at every %dm", s.Config.Freq)
	d, _ := time.ParseDuration(fmt.Sprintf("%dm", s.Config.Freq))
	for range time.Tick(d) {
		if err := s.doSync(); err != nil {
			return err
		}
	}
	return nil
}

func (s *dhSyncer) doSync() error {
	currentVersion, err := getDigest(s.Config.RepoARN, s.Config.Tag)
	if err != nil {
		s.stats.IncCount(fmt.Sprintf("%s.%s.%s.badsha.failure", s.namespace, s.Config.RepoName, s.Config.Deployment))
		s.k8sProvider.Recorder.Event(s.k8sProvider.Pod, corev1.EventTypeWarning, "CRSyncFailed", "No image found with correct tags")
		return nil
	}
	if err := s.k8sProvider.SyncDeployment(s.Config.Deployment, currentVersion, s.Config); err != nil {
		return errors.Wrapf(err, "Failed to sync deployments %s", s.Config.Deployment)
	}

	if err := s.k8sProvider.SyncConfig(currentVersion, s.Config); err != nil {
		// Check version raises events as deemed necessary .. for other issues log is ok for now
		// and continue checking
		log.Printf("Failed sync config: %v", err)
		s.stats.IncCount(fmt.Sprintf("%s.%s.configsyn.failure", s.namespace, s.Config.ConfigKey))
		return errors.Wrapf(err, "Failed to sync config version %s", s.Config.ConfigKey)
	}
	return nil
}

// getDigest fetches the digest of dockerhub image of requested repository and tag
// TODO:  allow using credentials to support private dockerhub repos too
// for now uses anonymous access
func getDigest(repo, tag string) (string, error) {
	hub, err := registry.New(dockerhubURL, "", "")
	if err != nil {
		return "", errors.Wrap(err, "Failed to connect to dockerhun")
	}
	digest, err := hub.ManifestDigest(repo, tag)
	if err != nil {
		return "", errors.Wrapf(err, "Failed to get tag %s on repository %s", tag, repo)
	}
	return digest.String(), nil
}
