package dockerhub

import (
	"github.com/pkg/errors"
)

type tagger struct {
}

// NewTagger provides reference to Tagger and offers capability
// to add/remove/get tags of ECR repos
func NewTagger() *tagger {
	return &tagger{}
}

// Add adds list of tags to the image identified with version
func (t *tagger) Add(repo string, version string, tags ...string) error {
	return errors.New("Dockerhub does not support multiple tags on same image")
}

// Remove removes the list of tags from ECR repository such that no image contains these
// tags
func (t *tagger) Remove(repo string, tags ...string) error {
	return errors.New(`Dockerhub does not support multiple tags an image and
		thus removing a subset from it is not supported`)
}

// Get gets the list of tags to the image identified with version
func (t *tagger) Get(repo string, version string) ([]string, error) {
	digest, err := getDigest(repo, version)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to connect to dockerhun")
	}
	return []string{version, digest}, nil
}
