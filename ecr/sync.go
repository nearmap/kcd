package ecr

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
)

var ecrRule, _ = regexp.Compile("([0-9]*).dkr.ecr.([a-z0-9-]*).amazonaws.com/([a-zA-Z0-9/\\_-]*)")
var sha1Regex, _ = regexp.Compile("[0-9a-f]{5,40}")
var configRule, _ = regexp.Compile("([0-9A-Za-z_]*)/([0-9A-Za-z_]*)")

// NameAccountRegionFromARN returns the name of the repo, the AWS Account ID and region
// that the repo belongs to from a given repo ARN.
func NameAccountRegionFromARN(arn string) (repoName, accountID, region string, err error) {
	rs := ecrRule.FindStringSubmatch(arn)
	if len(rs) != 4 {
		return "", "", "", errors.Errorf("ecr repo %s is not valid", arn)
	}
	return rs[3], rs[1], rs[2], nil
}

// Syncer offers capability to periodically sync with docker registry
type Syncer interface {
	Sync() error
}

// ECRSyncer is responsible to syncing with the ecr repository and
// ensuring that the deployment it is monitoring is up to date. If it finds
// the deployment outdated from what Tag is indicating the deployment version should be.
// it performs an update in deployment which then based on update strategy of deployment
// is further rolled out.
// In cases, where it cant resolves
type ECRSyncer struct {
	Config *SyncConfig

	sess *session.Session
	ecr  *ecr.ECR

	namespace string

	k8sClient *kubernetes.Clientset
	recorder  record.EventRecorder
	pod       *v1.Pod

	stats stats.Stats
}

// SyncConfig describes the arguments required by Syncer
type SyncConfig struct {
	Freq    int
	Tag     string
	RepoARN string

	Deployment string
	Container  string

	ConfigKey string

	accountID string
	repoName  string

	configMap *configKey
}

type configKey struct {
	name string
	key  string
}

func (sc *SyncConfig) validate() bool {
	return sc.RepoARN != "" && sc.Tag != "" && sc.Deployment != "" && sc.Container != ""
}

// NewSyncer provides new reference to AWS ECR for periodic Sync
func NewSyncer(sess *session.Session, cs *kubernetes.Clientset, ns string, sc *SyncConfig,
	stats stats.Stats) (*ECRSyncer, error) {

	if !sc.validate() {
		return nil, errors.Errorf("Invalid sync configurations found %v", sc)
	}

	var err error
	var region string
	sc.repoName, sc.accountID, region, err = NameAccountRegionFromARN(sc.RepoARN)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(log.Printf)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: cs.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(k8sscheme.Scheme, corev1.EventSource{Component: "container-version-controller"})

	// We set INSTANCENAME as ENV variable using downward api on the container that maps to pod name
	pod, err := cs.CoreV1().Pods(ns).Get(os.Getenv("INSTANCENAME"), metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get ECR sync pod")
	}

	sc.configMap, err = getConfigMap(ns, sc.ConfigKey)
	if err != nil {
		recorder.Event(pod, corev1.EventTypeWarning, "ConfigKeyMisconfigiured", "ConfigKey is misconfigured")
		return nil, errors.Wrap(err, "bad config key")
	}

	syncer := &ECRSyncer{
		Config: sc,

		sess:      sess,
		ecr:       ecr.New(sess, aws.NewConfig().WithRegion(region)),
		namespace: ns,

		k8sClient: cs,
		recorder:  recorder,
		pod:       pod,
		stats:     stats,
	}

	return syncer, nil
}

// Sync starts the periodic action of checking with ECR repository
// and acting if differences are found. In case of different expected
// version is identified, deployment roll-out is performed
func (s *ECRSyncer) Sync() error {
	log.Printf("Beginning sync....at every %dm", s.Config.Freq)
	d, _ := time.ParseDuration(fmt.Sprintf("%dm", s.Config.Freq))
	for range time.Tick(d) {
		if err := s.doSync(); err != nil {
			return err
		}
	}
	return nil
}

func (s *ECRSyncer) doSync() error {
	req := &ecr.DescribeImagesInput{
		ImageIds: []*ecr.ImageIdentifier{
			{
				ImageTag: aws.String(s.Config.Tag),
			},
		},
		RegistryId:     aws.String(s.Config.accountID),
		RepositoryName: aws.String(s.Config.repoName),
	}
	result, err := s.ecr.DescribeImages(req)
	if err != nil {
		log.Printf("Failed to get ECR: %v", err)
		return errors.Wrap(err, "failed to get ecr")
	}
	if len(result.ImageDetails) != 1 {
		s.stats.Event(fmt.Sprintf("%s.%s.%s.sync.failure", s.namespace, s.Config.repoName, s.Config.Deployment),
			fmt.Sprintf("Failed to sync with ECR for tag %s", s.Config.Tag), "", "error",
			time.Now().UTC(), s.Config.Tag, s.Config.accountID)
		s.recorder.Event(s.pod, corev1.EventTypeWarning, "ECRSyncFailed", "More than one image with tag was found")
		return errors.Errorf("Bad state: More than one image was tagged with %s", s.Config.Tag)
	}

	img := result.ImageDetails[0]

	currentVersion := s.currentVersion(img)
	if currentVersion == "" {
		s.stats.IncCount(fmt.Sprintf("%s.%s.%s.badsha.failure", s.namespace, s.Config.repoName, s.Config.Deployment))
		s.recorder.Event(s.pod, corev1.EventTypeWarning, "ECRSyncFailed", "Tagged image missing SHA1")
		return nil
	}
	// Check deployment
	d, err := s.k8sClient.AppsV1().Deployments(s.namespace).Get(s.Config.Deployment, metav1.GetOptions{})
	if err != nil {
		s.recorder.Event(s.pod, corev1.EventTypeWarning, "ECRSyncFailed", "Failed to get dependent deployment")
		return errors.Wrap(err, "failed to read deployment ")
	}
	if ci, err := s.checkDeployment(currentVersion, d); err != nil {
		if err == ErrVersionMismatch {
			s.deploy(ci, d)
		} else {
			// Check version raises events as deemed necessary .. for other issues log is ok for now
			// and continue checking
			log.Printf("Failed sync with image: digest=%v, tag=%v, err=%v",
				aws.StringValue(img.ImageDigest), aws.StringValueSlice(img.ImageTags), err)
		}
	}
	// Syncup config if unspecified
	if err := s.syncConfig(currentVersion); err != nil {
		// Check version raises events as deemed necessary .. for other issues log is ok for now
		// and continue checking
		log.Printf("Failed sync config: %v", err)
	}
	return nil
}

// syncConfig sync the config map referenced by CV resource - creates if absent and updates if required
// The controller is not responsible for managing the config resource it reference but only for updating
// and ensuring its present. If the reference to config was removed from CV resource its not the responsibility
// of controller to remove it .. it assumes the configMap is external resource and not owned by cv resource
func (s *ECRSyncer) syncConfig(version string) error {
	if s.Config.configMap == nil {
		return nil
	}
	cm, err := s.k8sClient.CoreV1().ConfigMaps(s.namespace).Get(s.Config.configMap.name, metav1.GetOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			_, err = s.k8sClient.CoreV1().ConfigMaps(s.namespace).Create(
				newVersionConfig(s.namespace, s.Config.configMap.name, s.Config.configMap.key, version))
			if err != nil {
				s.recorder.Event(s.pod, corev1.EventTypeWarning, "FailedCreateVersionConfigMap", "Failed to create version configmap")
				return errors.Wrapf(err, "failed to create version configmap from %s/%s:%s",
					s.namespace, s.Config.configMap.name, s.Config.configMap.key)
			}
			return nil
		}
		return errors.Wrapf(err, "failed to get version configmap from %s/%s:%s",
			s.namespace, s.Config.configMap.name, s.Config.configMap.key)
	}

	if version == cm.Data[s.Config.configMap.key] {
		return nil
	}

	cm.Data[s.Config.configMap.key] = version

	// TODO enable this when patchstretegy is supported on config map https://github.com/kubernetes/client-go/blob/7ac1236/pkg/api/v1/types.go#L3979
	// _, err = s.k8sClient.CoreV1().ConfigMaps(s.namespace).Patch(cm.ObjectMeta.Name, types.StrategicMergePatchType, []byte(fmt.Sprintf(`{
	// 	"Data": {
	// 		"%s": "%s",
	// 	},
	// }`, s.Config.configMap.key, version)))
	_, err = s.k8sClient.CoreV1().ConfigMaps(s.namespace).Update(cm)
	if err != nil {
		s.recorder.Event(s.pod, corev1.EventTypeWarning, "FailedUpdateVersionConfigMao", "Failed to update version configmap")
		return errors.Wrapf(err, "failed to update version configmap from %s/%s:%s",
			s.namespace, s.Config.configMap.name, s.Config.configMap.key)
	}
	return nil
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

func (s *ECRSyncer) currentVersion(img *ecr.ImageDetail) string {
	var tag string
	for _, t := range aws.StringValueSlice(img.ImageTags) {

		if sha1Regex.MatchString(t) {
			tag = t
			break
		}
	}
	return tag
}

func (s *ECRSyncer) checkDeployment(tag string, d *appsv1.Deployment) (string, error) {
	log.Printf("Checking deployment version %s from ECR %s for deployment %s", tag, s.Config.repoName, s.Config.Deployment)
	match := false
	for _, c := range d.Spec.Template.Spec.Containers {
		if c.Name == s.Config.Container {
			match = true
			parts := strings.Split(c.Image, ":")
			if len(parts) != 2 {
				s.recorder.Event(s.pod, corev1.EventTypeWarning, "ECRSyncFailed", "Invalid image on container")
				return "", errors.New("Invalid image found on container")
			}
			if parts[0] != s.Config.RepoARN {
				s.stats.Event(fmt.Sprintf("%s.%s.%s.sync.failure", s.namespace, s.Config.repoName, s.Config.Deployment),
					fmt.Sprintf("ECR repo mismatch present %s and requested  %s don't match", parts[0], s.Config.RepoARN), "", "error",
					time.Now().UTC(), s.Config.Tag, s.Config.accountID)
				s.recorder.Event(s.pod, corev1.EventTypeWarning, "ECRSyncFailed", "ECR Repository mismatch was found")
				return "", ErrValidation
			}
			if tag != parts[1] {
				if s.validate(tag) != nil {
					s.stats.Event(fmt.Sprintf("%s.%s.%s.sync.failure", s.namespace, s.Config.repoName, s.Config.Deployment),
						fmt.Sprintf("Failed to validate image with tag %s", tag), "", "error",
						time.Now().UTC(), s.Config.Tag, s.Config.accountID)
					s.recorder.Event(s.pod, corev1.EventTypeWarning, "ECRSyncFailed", "Candidate version failed validation")
					return "", ErrValidation
				}
				return tag, ErrVersionMismatch
			}
		}
	}

	if !match {
		s.stats.Event(fmt.Sprintf("%s.%s.%s.sync.failure", s.namespace, s.Config.repoName, s.Config.Deployment),
			"No matching container found", "", "error",
			time.Now().UTC(), s.Config.Tag, s.Config.accountID)
		s.recorder.Event(s.pod, corev1.EventTypeWarning, "ECRSyncFailed", "No matching container found")

		return "", errors.Errorf("No container of name %s was found in deployment %s", s.Config.Container, s.Config.Deployment)
	}

	log.Printf("Deployment %s is upto date deployment", s.Config.Deployment)
	return "", nil
}

func (s *ECRSyncer) validate(v string) error {
	//TODO later regression check etc
	return nil
}

// rollback tag logic is not needed revisionHistoryLimit automatically maintains 6 revisions limits
func (s *ECRSyncer) deploy(tag string, d *appsv1.Deployment) error {
	log.Printf("Beginning deploy for deployment %s with version %s", s.Config.Deployment, tag)

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		for i, c := range d.Spec.Template.Spec.Containers {
			if c.Name == s.Config.Container {
				// // Update modifies other config as well
				// if _, updateErr := s.k8sClient.AppsV1().Deployments(s.namespace).Update(d); updateErr != nil {
				// 	log.WithField("version", tag).
				// 		WithField("deployment", s.Config.Deployment).
				// 		WithError(updateErr).
				// 		Error("Failed update container version.. will retry ")
				// 	return updateErr
				// }
				if _, updateErr := s.k8sClient.AppsV1().Deployments(s.namespace).Patch(d.ObjectMeta.Name, types.StrategicMergePatchType,
					[]byte(fmt.Sprintf(`
						{
							"spec": {
								"template": {
									"spec": {
										"containers": [
												{
													"name":  "%s",
													"image": "%s:%s"
												}
											]
										}
									}
								}
							}
						}
						`, d.Spec.Template.Spec.Containers[i].Name, s.Config.RepoARN, tag))); updateErr != nil {

					log.Printf("Failed to update container version (will retry): version=%v, deployment=%v, error=%v",
						tag, s.Config.Deployment, updateErr)

					return updateErr
				}

			}
		}
		return nil
	})
	if retryErr != nil {
		s.stats.Event(fmt.Sprintf("%s.%s.%s.sync.deploy.failure", s.namespace, s.Config.repoName, s.Config.Deployment),
			fmt.Sprintf("Failed to validate image with %s", tag), "", "error",
			time.Now().UTC(), s.Config.Tag, s.Config.accountID)
		log.Printf("Failed to update container version after maximum retries: version=%v, deployment=%v, error=%v",
			tag, s.Config.Deployment, retryErr)
		s.recorder.Event(s.pod, corev1.EventTypeWarning, "DeploymentFailed", "Failed to perform the deployment")
	}

	log.Printf("Update completed: deployment=%v", s.Config.Deployment)
	s.stats.IncCount(fmt.Sprintf("%s.%s.%s.success", s.namespace, s.Config.repoName, s.Config.Deployment))
	s.recorder.Event(s.pod, corev1.EventTypeNormal, "DeploymentSuccess", "Deployment completed successfully")
	return nil
}

func getConfigMap(namespace, key string) (*configKey, error) {
	if key == "" {
		return nil, nil
	}
	cms := configRule.FindStringSubmatch(key)
	if len(cms) != 3 {
		return nil, errors.New("Invalid config map key for version passed")
	}
	return &configKey{
		name: cms[1],
		key:  cms[2],
	}, nil
}
