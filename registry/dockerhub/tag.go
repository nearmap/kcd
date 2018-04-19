package dockerhub

import (
	"github.com/heroku/docker-registry-client/registry"
	"github.com/pkg/errors"
)

type tagger struct {
	username, pwd string
}

// NewTagger provides reference to Tagger and offers capability
// to add/remove/get tags of ECR repos
func NewTagger(username, pwd string) *tagger {
	return &tagger{
		username: username,
		pwd:      pwd,
	}
}

// Add adds list of tags to the image identified with version
func (t *tagger) Add(repo string, version string, tags ...string) error {
	return t.addTagsOnImg(repo, version, tags...)
}

// Remove removes the list of tags from ECR repository such that no image contains these
// tags
func (t *tagger) Remove(repo string, tags ...string) error {
	return errors.New(`Dockerhub does not support multiple tags an image and
		thus removing a subset from it is not supported`)
}

// Get gets the list of tags to the image identified with version
func (t *tagger) Get(repo string, version string) ([]string, error) {
	digest, err := getDigest(repo, t.username, t.pwd, version)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to connect to dockerhun")
	}
	return []string{version, digest}, nil
}

// addTagsOnImg fetches the manifest of container image of specified version tag
// and tag additional tags to the same manifest
// TODO:  allow using credentials to support private dockerhub repos too
// for now uses anonymous access
func (t *tagger) addTagsOnImg(repo, version string, tags ...string) error {
	hub, err := registry.New(dockerhubURL, t.username, t.pwd)
	if err != nil {
		return errors.Wrap(err, "Failed to connect to dockerhun")
	}
	manifest, err := hub.Manifest(repo, version)
	if err != nil {
		return errors.Wrapf(err, "Failed to find manifest for image version %s on repository %s", version, repo)
	}

	for _, tag := range tags {
		err := hub.PutManifest(repo, tag, manifest)
		if err != nil {
			return errors.Wrapf(err, "Failed to add tags %s on image version %s on repository %s", tag, version, repo)
		}
	}
	return nil
}
