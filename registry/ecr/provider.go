package ecr

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/eric1313/kcd/registry"
	"github.com/eric1313/kcd/stats"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/golang/glog"
	"github.com/pkg/errors"
)

var ecrRule, _ = regexp.Compile("([0-9]*).dkr.ecr.([a-z0-9-]*).amazonaws.com/([a-zA-Z0-9/\\_-]*)")

// nameAccountRegionFromARN returns the name of the repo, the AWS Account ID and region
// that the repo belongs to from a given repo ARN.
func nameAccountRegionFromARN(arn string) (repoName, accountID, region string, err error) {
	rs := ecrRule.FindStringSubmatch(arn)
	if len(rs) != 4 {
		return "", "", "", errors.Errorf("ecr repo %s is not valid", arn)
	}
	return rs[3], rs[1], rs[2], nil
}

// Provider is responsible to syncing with the ecr repository and
// ensuring that the deployment it is monitoring is up to date. If it finds
// the deployment outdated from what Tag is indicating the deployment version should be.
// it performs an update in deployment which then based on update strategy of deployment
// is further rolled out.
// In cases, where it cant resolves
type Provider struct {
	sess      *session.Session
	ecr       *ecr.ECR
	repoName  string
	accountID string

	vRegex *regexp.Regexp

	stats stats.Stats
}

// NewECR returns an ECR provider that implements the Registry interface, and used
// to check an AWS ECR repository and sync deployments periodically.
func NewECR(imageRepo, versionExp string, stats stats.Stats) (*Provider, error) {
	vRegex, err := regexp.Compile(versionExp)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	repoName, accountID, region, err := nameAccountRegionFromARN(imageRepo) //kcd.Spec.ImageRepo
	if err != nil {
		return nil, errors.WithStack(err)
	}

	sess, err := session.NewSession()
	if err != nil {
		return nil, errors.Wrap(err, "failed to obtain AWS session")
	}

	ep := &Provider{
		repoName:  repoName,
		accountID: accountID,
		sess:      sess,
		ecr:       ecr.New(sess, aws.NewConfig().WithRegion(region)),

		vRegex: vRegex,
		stats:  stats,
	}

	return ep, nil
}

// RegistryFor implements the registry.Provider interface.
func (ep *Provider) RegistryFor(imageRepo string) (registry.Registry, error) {
	repoName, accountID, region, err := nameAccountRegionFromARN(imageRepo)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &Provider{
		repoName:  repoName,
		accountID: accountID,
		sess:      ep.sess,
		ecr:       ecr.New(ep.sess, aws.NewConfig().WithRegion(region)),

		vRegex: ep.vRegex,
		stats:  ep.stats,
	}, nil
}

// Version implements the Registry interface.
func (ep *Provider) Versions(ctx context.Context, tag string) ([]string, error) {
	// TODO: parameterize timeout
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, time.Second*15)
	defer cancel()

	if glog.V(4) {
		glog.V(4).Infof("Making ECR DescribeImages request for repository=%s, registry=%s, tag=%s",
			ep.repoName, ep.accountID, tag)
	}

	req := &ecr.DescribeImagesInput{
		ImageIds: []*ecr.ImageIdentifier{
			{
				ImageTag: aws.String(tag),
			},
		},
		RegistryId:     aws.String(ep.accountID),
		RepositoryName: aws.String(ep.repoName),
	}
	result, err := ep.ecr.DescribeImagesWithContext(ctx, req)
	if err != nil {
		glog.Errorf("Failed to get ECR: %v", err)
		return nil, errors.Wrap(err, "failed to get ecr")
	}
	if len(result.ImageDetails) != 1 {
		ep.stats.Event(fmt.Sprintf("registry.%s.sync.failure", ep.repoName),
			fmt.Sprintf("Failed to sync with ECR for tag %s", tag), "", "error",
			time.Now().UTC(), tag)
		return nil, errors.Errorf("Bad state: More than one image was tagged with %s", tag)
	}

	img := result.ImageDetails[0]

	versions := ep.currentVersions(img)
	if len(versions) == 0 {
		ep.stats.IncCount("registry.failure", ep.repoName)
		return nil, errors.Errorf("No version found for tag %s", tag)
	}

	glog.V(2).Infof("Got currentVersions=%s from ECR", strings.Join(versions, ", "))

	return versions, nil
}

// Add a list of tags to the image identified with version
func (ep *Provider) Add(version string, tags ...string) error {
	for _, tag := range tags {
		fmt.Printf("Tags are %s \n", tags)
		getReq := &ecr.BatchGetImageInput{
			ImageIds: []*ecr.ImageIdentifier{
				{
					ImageTag: aws.String(version),
				},
			},
			RegistryId:     aws.String(ep.accountID),
			RepositoryName: aws.String(ep.repoName),
		}

		getRes, err := ep.ecr.BatchGetImage(getReq)
		if err != nil {
			ep.stats.IncCount("registry.failure", ep.repoName)
			return errors.Wrap(err, fmt.Sprintf("failed to get images of tag %s", tag))
		}

		for _, img := range getRes.Images {
			putReq := &ecr.PutImageInput{
				ImageManifest:  img.ImageManifest,
				ImageTag:       aws.String(tag),
				RegistryId:     aws.String(ep.accountID),
				RepositoryName: aws.String(ep.repoName),
			}

			_, err = ep.ecr.PutImage(putReq)
			if err != nil {
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					case ecr.ErrCodeImageAlreadyExistsException:
						continue
					}
				}
				ep.stats.IncCount("registry.failure", ep.repoName)
				return errors.Wrap(err, fmt.Sprintf("failed to add tag %s to image manifest %s",
					tag, aws.StringValue(img.ImageManifest)))
			}
		}
	}
	return nil
}

// Remove the list of tags from ECR repository such that no image contains these tags.
func (ep *Provider) Remove(tags ...string) error {
	for _, tag := range tags {
		getReq := &ecr.BatchGetImageInput{
			ImageIds: []*ecr.ImageIdentifier{
				{
					ImageTag: aws.String(tag),
				},
			},
			RegistryId:     aws.String(ep.accountID),
			RepositoryName: aws.String(ep.repoName),
		}

		getRes, err := ep.ecr.BatchGetImage(getReq)
		if err != nil {
			ep.stats.IncCount("registry.failure", ep.repoName)
			return errors.Wrap(err, fmt.Sprintf("failed to get images of tag %s", tag))
		}

		for _, img := range getRes.Images {

			delReq := &ecr.BatchDeleteImageInput{
				ImageIds: []*ecr.ImageIdentifier{
					{
						ImageTag:    aws.String(tag),
						ImageDigest: img.ImageId.ImageDigest,
					},
				},
				RegistryId:     aws.String(ep.accountID),
				RepositoryName: aws.String(ep.repoName),
			}

			_, err = ep.ecr.BatchDeleteImage(delReq)
			if err != nil {
				ep.stats.IncCount("registry.failure", ep.repoName)
				return errors.Wrap(err, fmt.Sprintf("failed to perform batch delete image by tag %s and digest %s",
					tag, aws.StringValue(img.ImageId.ImageDigest)))
			}
		}
	}
	return nil
}

// Get a list of tags a version is currently identified with.
func (ep *Provider) Get(version string) ([]string, error) {
	getReq := &ecr.DescribeImagesInput{
		ImageIds: []*ecr.ImageIdentifier{
			{
				ImageTag: aws.String(version),
			},
		},
		RegistryId:     aws.String(ep.accountID),
		RepositoryName: aws.String(ep.repoName),
	}

	getRes, err := ep.ecr.DescribeImages(getReq)
	if err != nil {
		ep.stats.IncCount("registry.failure", ep.repoName)
		return nil, errors.Wrap(err, fmt.Sprintf("failed to get images of tag %s", version))
	}

	if len(getRes.ImageDetails) > 1 {
		return nil, errors.New("more than one image with version tag was found")
	}

	return aws.StringValueSlice(getRes.ImageDetails[0].ImageTags), nil

}

func (ep *Provider) currentVersion(img *ecr.ImageDetail) string {
	var tag string
	for _, t := range aws.StringValueSlice(img.ImageTags) {

		if ep.vRegex.MatchString(t) {
			tag = t
			break
		}
	}
	return tag
}

func (ep *Provider) currentVersions(img *ecr.ImageDetail) []string {
	tags := make([]string, 0, 5)
	for _, t := range aws.StringValueSlice(img.ImageTags) {

		if ep.vRegex.MatchString(t) {
			tags = append(tags, t)
		}
	}
	return tags
}