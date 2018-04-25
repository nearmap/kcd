package ecr

import (
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	k8s "github.com/nearmap/cvmanager/gok8s/workload"
	rgs "github.com/nearmap/cvmanager/registry"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
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

	sess      *session.Session
	ecr       *ecr.ECR
	repoName  string
	accountID string

	stats stats.Stats
}

// NewSyncer provides new reference of dhSyncer
// to manage AWS ECR repository and sync deployments periodically
func NewSyncer(sess *session.Session, cs *kubernetes.Clientset, ns string,
	cv *cv1.ContainerVersion, stats stats.Stats, recordHistory bool) (*syncer, error) {

	repoName, accountID, region, err := nameAccountRegionFromARN(cv.Spec.ImageRepo)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	k8sProvider := k8s.NewK8sProvider(cs, ns, stats, recordHistory)

	syncer := &syncer{
		k8sProvider: k8sProvider,
		namespace:   ns,
		cv:          cv,

		repoName:  repoName,
		accountID: accountID,
		sess:      sess,
		ecr:       ecr.New(sess, aws.NewConfig().WithRegion(region)),

		stats: stats,
	}

	return syncer, nil
}

// Sync starts the periodic action of checking with ECR repository
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
			// TODO: revert !!!!!!!!!!!!!!!!!!!!!!!!
			// for now, don't crash on error
			//return err

			// temp
			log.Printf("ERROR in doSync: %v", err)
			log.Printf("This would have resulted in the pod crashing!!!!!!!!!!!")
		}
	}
	return nil
}

func (s *syncer) doSync() error {
	// temp
	log.Printf("Starting SyncWorkload...")

	currentVersion, err := s.getVersionFromRegistry()
	if err != nil {
		return errors.WithStack(err)
	}
	if currentVersion == "" {
		s.stats.IncCount(fmt.Sprintf("%s.%s.%s.badsha.failure", s.namespace, s.repoName, s.cv.Spec.Selector[cv1.CVAPP]))
		s.k8sProvider.Recorder.Event(events.Warning, "CRSyncFailed", "Tagged image missing SHA1")
		return nil
	}

	// Check deployment
	if err := s.k8sProvider.SyncWorkload(s.cv, currentVersion); err != nil {
		return errors.Wrapf(err, "Failed to sync deployments %s", s.cv.Spec.Selector[cv1.CVAPP])
	}

	// temp
	log.Printf("Finished SyncWorkload.")

	// Syncup config if unspecified
	if err := s.k8sProvider.SyncVersionConfig(s.cv, currentVersion); err != nil {
		log.Printf("Failed sync config: %v", err)
		s.stats.IncCount(fmt.Sprintf("%s.%s.configsyn.failure", s.namespace, s.cv.Spec.Config.Name))
		return errors.Wrapf(err, "Failed to sync config version %s", s.cv.Spec.Selector[cv1.CVAPP])
	}

	return nil
}

func (s *syncer) getVersionFromRegistry() (string, error) {
	req := &ecr.DescribeImagesInput{
		ImageIds: []*ecr.ImageIdentifier{
			{
				ImageTag: aws.String(s.cv.Spec.Tag),
			},
		},
		RegistryId:     aws.String(s.accountID),
		RepositoryName: aws.String(s.repoName),
	}
	result, err := s.ecr.DescribeImages(req)
	if err != nil {
		log.Printf("Failed to get ECR: %v", err)
		return "", errors.Wrap(err, "failed to get ecr")
	}
	if len(result.ImageDetails) != 1 {
		// TODO: this is going to keep crashing the pod and restarting. Is this what we want?
		s.stats.Event(fmt.Sprintf("%s.%s.%s.sync.failure", s.namespace, s.repoName, s.cv.Spec.Selector[cv1.CVAPP]),
			fmt.Sprintf("Failed to sync with ECR for tag %s", s.cv.Spec.Tag), "", "error",
			time.Now().UTC(), s.cv.Spec.Tag, s.accountID)
		s.k8sProvider.Recorder.Event(events.Warning, "CRSyncFailed", "More than one image with tag was found")
		return "", errors.Errorf("Bad state: More than one image was tagged with %s", s.cv.Spec.Tag)
	}

	img := result.ImageDetails[0]
	return s.currentVersion(img), nil
}

func (s *syncer) currentVersion(img *ecr.ImageDetail) string {
	var tag string
	for _, t := range aws.StringValueSlice(img.ImageTags) {

		if sha1Regex.MatchString(t) {
			tag = t
			break
		}
	}
	return tag
}
