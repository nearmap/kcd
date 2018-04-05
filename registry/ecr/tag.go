package ecr

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
)

type tagger struct {
	sess  *session.Session
	stats stats.Stats
}

// NewTagger provides reference to Tagger and offers capability
// to add/remove/get tags of ECR repos
func NewTagger(sess *session.Session, stats stats.Stats) *tagger {

	return &tagger{
		sess:  sess,
		stats: stats,
	}
}

// Add adds list of tags to the image identified with version
func (t *tagger) Add(ecrARN string, version string, tags ...string) error {
	repoName, accountID, region, err := nameAccountRegionFromARN(ecrARN)
	if err != nil {
		return errors.Wrap(err, "failed to read ECR repository ARN")
	}

	ecrClient := ecr.New(t.sess, aws.NewConfig().WithRegion(region))

	for _, tag := range tags {
		fmt.Printf("Tags are %s \n", tags)
		getReq := &ecr.BatchGetImageInput{
			ImageIds: []*ecr.ImageIdentifier{
				{
					ImageTag: aws.String(version),
				},
			},
			RegistryId:     aws.String(accountID),
			RepositoryName: aws.String(repoName),
		}

		getRes, err := ecrClient.BatchGetImage(getReq)
		if err != nil {
			t.stats.IncCount(fmt.Sprintf("ecr.batchget.%s.failure", repoName))
			return errors.Wrap(err, fmt.Sprintf("failed to get images of tag %s", tag))
		}

		for _, img := range getRes.Images {
			putReq := &ecr.PutImageInput{
				ImageManifest:  img.ImageManifest,
				ImageTag:       aws.String(tag),
				RegistryId:     aws.String(accountID),
				RepositoryName: aws.String(repoName),
			}

			_, err = ecrClient.PutImage(putReq)
			if err != nil {
				t.stats.IncCount(fmt.Sprintf("ecr.putimage.%s.failure", repoName))
				return errors.Wrap(err, fmt.Sprintf("failed to add tag %s to image manifest %s",
					tag, aws.StringValue(img.ImageManifest)))
			}

		}
	}
	return nil
}

// Remove removes the list of tags from ECR repository such that no image contains these
// tags
func (t *tagger) Remove(ecrARN string, tags ...string) error {
	repoName, accountID, region, err := nameAccountRegionFromARN(ecrARN)
	if err != nil {
		return errors.Wrap(err, "failed to read ECR repository ARN")
	}

	ecrClient := ecr.New(t.sess, aws.NewConfig().WithRegion(region))

	for _, tag := range tags {
		getReq := &ecr.BatchGetImageInput{
			ImageIds: []*ecr.ImageIdentifier{
				{
					ImageTag: aws.String(tag),
				},
			},
			RegistryId:     aws.String(accountID),
			RepositoryName: aws.String(repoName),
		}

		getRes, err := ecrClient.BatchGetImage(getReq)
		if err != nil {
			t.stats.IncCount(fmt.Sprintf("ecr.batchget.%s.failure", repoName))
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
				RegistryId:     aws.String(accountID),
				RepositoryName: aws.String(repoName),
			}

			_, err = ecrClient.BatchDeleteImage(delReq)
			if err != nil {
				t.stats.IncCount(fmt.Sprintf("ecr.batchdelete.%s.failure", repoName))
				return errors.Wrap(err, fmt.Sprintf("failed to perform batch delete image by tag %s and digest %s",
					tag, aws.StringValue(img.ImageId.ImageDigest)))
			}

		}
	}
	return nil
}

// Get gets the list of tags to the image identified with version
func (t *tagger) Get(ecrARN string, version string) ([]string, error) {
	repoName, accountID, region, err := nameAccountRegionFromARN(ecrARN)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read ECR repository ARN")
	}

	ecrClient := ecr.New(t.sess, aws.NewConfig().WithRegion(region))

	getReq := &ecr.DescribeImagesInput{
		ImageIds: []*ecr.ImageIdentifier{
			{
				ImageTag: aws.String(version),
			},
		},
		RegistryId:     aws.String(accountID),
		RepositoryName: aws.String(repoName),
	}

	getRes, err := ecrClient.DescribeImages(getReq)
	if err != nil {
		t.stats.IncCount(fmt.Sprintf("ecr.descimg.%s.failure", repoName))
		return nil, errors.Wrap(err, fmt.Sprintf("failed to get images of tag %s", version))
	}

	if len(getRes.ImageDetails) > 1 {
		return nil, errors.New("More than one image with version tag was found ... bad state!")
	}

	return aws.StringValueSlice(getRes.ImageDetails[0].ImageTags), nil

}
