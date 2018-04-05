package ecr

import (
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/nearmap/cvmanager/registry/config"
	"github.com/nearmap/cvmanager/registry/k8s"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

var ecrRule, _ = regexp.Compile("([0-9]*).dkr.ecr.([a-z0-9-]*).amazonaws.com/([a-zA-Z0-9/\\_-]*)")
var sha1Regex, _ = regexp.Compile("[0-9a-f]{5,40}")

// nameAccountRegionFromARN returns the name of the repo, the AWS Account ID and region
// that the repo belongs to from a given repo ARN.
func nameAccountRegionFromARN(arn string) (repoName, accountID, region string, err error) {
	rs := ecrRule.FindStringSubmatch(arn)
	if len(rs) != 4 {
		return "", "", "", errors.Errorf("ecr repo %s is not valid", arn)
	}
	return rs[3], rs[1], rs[2], nil
}

// ecrSyncer is responsible to syncing with the ecr repository and
// ensuring that the deployment it is monitoring is up to date. If it finds
// the deployment outdated from what Tag is indicating the deployment version should be.
// it performs an update in deployment which then based on update strategy of deployment
// is further rolled out.
// In cases, where it cant resolves
type ecrSyncer struct {
	Config *config.SyncConfig

	sess *session.Session
	ecr  *ecr.ECR

	namespace string

	k8sProvider *k8s.K8sProvider

	stats stats.Stats
}

// NewSyncer provides new reference of dhSyncer
// to manage AWS ECR repository and sync deployments periodically
func NewSyncer(sess *session.Session, cs *kubernetes.Clientset,
	ns string,
	sc *config.SyncConfig,
	stats stats.Stats) (*ecrSyncer, error) {

	if !sc.Validate() {
		return nil, errors.Errorf("Invalid sync configurations found %v", sc)
	}

	var err error
	var region string
	sc.RepoName, sc.AccountID, region, err = nameAccountRegionFromARN(sc.RepoARN)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	k8sProvider, err := k8s.NewK8sProvider(cs, ns, stats)
	if err != nil {
		return nil, errors.Wrap(err, "Could not initialize k8s client/provider")
	}

	sc.ConfigMap, err = config.ConfigMap(ns, sc.ConfigKey)
	if err != nil {
		k8sProvider.Recorder.Event(k8sProvider.Pod, corev1.EventTypeWarning, "ConfigKeyMisconfigiured", "ConfigKey is misconfigured")
		return nil, errors.Wrap(err, "bad config key")
	}

	syncer := &ecrSyncer{
		Config: sc,

		sess:      sess,
		ecr:       ecr.New(sess, aws.NewConfig().WithRegion(region)),
		namespace: ns,

		k8sProvider: k8sProvider,
		stats:       stats,
	}

	return syncer, nil
}

// Sync starts the periodic action of checking with ECR repository
// and acting if differences are found. In case of different expected
// version is identified, deployment roll-out is performed
func (s *ecrSyncer) Sync() error {
	log.Printf("Beginning sync....at every %dm", s.Config.Freq)
	d, _ := time.ParseDuration(fmt.Sprintf("%dm", s.Config.Freq))
	for range time.Tick(d) {
		if err := s.doSync(); err != nil {
			return err
		}
	}
	return nil
}

func (s *ecrSyncer) doSync() error {
	req := &ecr.DescribeImagesInput{
		ImageIds: []*ecr.ImageIdentifier{
			{
				ImageTag: aws.String(s.Config.Tag),
			},
		},
		RegistryId:     aws.String(s.Config.AccountID),
		RepositoryName: aws.String(s.Config.RepoName),
	}
	result, err := s.ecr.DescribeImages(req)
	if err != nil {
		log.Printf("Failed to get ECR: %v", err)
		return errors.Wrap(err, "failed to get ecr")
	}
	if len(result.ImageDetails) != 1 {
		s.stats.Event(fmt.Sprintf("%s.%s.%s.sync.failure", s.namespace, s.Config.RepoName, s.Config.Deployment),
			fmt.Sprintf("Failed to sync with ECR for tag %s", s.Config.Tag), "", "error",
			time.Now().UTC(), s.Config.Tag, s.Config.AccountID)
		s.k8sProvider.Recorder.Event(s.k8sProvider.Pod, corev1.EventTypeWarning, "CRSyncFailed", "More than one image with tag was found")
		return errors.Errorf("Bad state: More than one image was tagged with %s", s.Config.Tag)
	}

	img := result.ImageDetails[0]

	currentVersion := s.currentVersion(img)
	if currentVersion == "" {
		s.stats.IncCount(fmt.Sprintf("%s.%s.%s.badsha.failure", s.namespace, s.Config.RepoName, s.Config.Deployment))
		s.k8sProvider.Recorder.Event(s.k8sProvider.Pod, corev1.EventTypeWarning, "CRSyncFailed", "Tagged image missing SHA1")
		return nil
	}
	// Check deployment
	if err := s.k8sProvider.SyncDeployment(s.Config.Deployment, currentVersion, s.Config); err != nil {
		return errors.Wrapf(err, "Failed to sync deployments %s", s.Config.Deployment)
	}

	// Syncup config if unspecified
	if err := s.k8sProvider.SyncConfig(currentVersion, s.Config); err != nil {
		// Check version raises events as deemed necessary
		log.Printf("Failed sync config: %v", err)
		s.stats.IncCount(fmt.Sprintf("%s.%s.configsyn.failure", s.namespace, s.Config.ConfigKey))
		return errors.Wrapf(err, "Failed to sync config version %s", s.Config.ConfigKey)
	}

	return nil
}

func (s *ecrSyncer) currentVersion(img *ecr.ImageDetail) string {
	var tag string
	for _, t := range aws.StringValueSlice(img.ImageTags) {

		if sha1Regex.MatchString(t) {
			tag = t
			break
		}
	}
	return tag
}
