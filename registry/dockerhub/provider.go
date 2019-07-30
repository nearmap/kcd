package dockerhub

import (
	"context"

	kcdregistry "github.com/eric1313/kcd/registry"
	"github.com/eric1313/kcd/stats"
	"github.com/heroku/docker-registry-client/registry"
	"github.com/pkg/errors"
)

const dockerhubURL = "https://registry-1.docker.io/"

// Options contains additional (optional) configuration for the controller
type Options struct {
	Stats stats.Stats

	HubURL, User, Password string
}

// WithStats applies the stats type to the controller
func WithStats(instance stats.Stats) func(*Options) {
	return func(opts *Options) {
		opts.Stats = instance
	}
}

// WithCreds applies the stats type to the controller
func WithCreds(user, password string) func(*Options) {
	return func(opts *Options) {
		opts.User = user
		opts.Password = password
	}
}

// V2Provider is responsible to syncing with the docker registry (dr) repository and
// ensuring that the deployment it is monitoring is up to date. If it finds
// the deployment outdated from what Tag is indicating the deployment version should be.
// it performs an update in deployment which then based on update strategy of deployment
// is further rolled out.
// In cases, where it cant resolves
// TODO: Dockerhub syncer does not support private repositories at the moment
// - Need to work on storing credentials in secret or similar more secure approach
// for user/password. For now uses anonymous access
// - Need to define auth mechanism
type V2Provider struct {
	repository string
	client     *registry.Registry
	opts       *Options
}

// NewDHV2 returns a DockerHub V2 registry provider.
func NewDHV2(repository, versionExp string, options ...func(*Options)) (*V2Provider, error) {
	opts := &Options{
		Stats:    stats.NewFake(),
		User:     "",
		Password: "",
		HubURL:   dockerhubURL,
	}

	for _, opt := range options {
		opt(opts)
	}

	client, err := registry.New(opts.HubURL, opts.User, opts.Password)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to connect to dockerhub")
	}

	return &V2Provider{
		client:     client,
		repository: repository,
		opts:       opts,
	}, nil
}

// RegistryFor implements the registry.Provider interface.
func (vp *V2Provider) RegistryFor(imageRepo string) (kcdregistry.Registry, error) {
	return &V2Provider{
		client:     vp.client,
		repository: imageRepo,
		opts:       vp.opts,
	}, nil
}

// Versions implements the Registry interface.
func (vp *V2Provider) Versions(ctx context.Context, tag string) ([]string, error) {
	tags := make([]string, 0, 5)
	newVersion, err := vp.getDigest(tag)
	if err != nil {
		vp.opts.Stats.IncCount("registry.failure", vp.repository)
		return tags, errors.Errorf("No version found for tag %s", tag)
	}
	tags = append(tags, newVersion)
	return tags, nil
}

// Add adds list of tags to the image identified with version
func (vp *V2Provider) Add(version string, tags ...string) error {
	return vp.addTagsOnImg(version, tags...)
}

// Remove removes the list of tags from dockerhub repository such that no image contains these
// tags
func (vp *V2Provider) Remove(tags ...string) error {
	return errors.New(`Dockerhub does not support multiple tags an image and
		thus removing a subset from it is not supported`)
}

// Get gets the list of tags to the image identified with version
func (vp *V2Provider) Get(version string) ([]string, error) {
	digest, err := vp.getDigest(version)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to connect to dockerhub")
	}
	return []string{version, digest}, nil
}

// addTagsOnImg fetches the manifest of container image of specified version tag
// and tag additional tags to the same manifest
// TODO:  allow using credentials to support private dockerhub repos too
// for now uses anonymous access
func (vp *V2Provider) addTagsOnImg(version string, tags ...string) error {
	manifest, err := vp.client.Manifest(vp.repository, version)
	if err != nil {
		return errors.Wrapf(err, "Failed to find manifest for image version %s on repository %s", version, vp.repository)
	}

	for _, tag := range tags {
		err := vp.client.PutManifest(vp.repository, tag, manifest)
		if err != nil {
			return errors.Wrapf(err, "Failed to add tags %s on image version %s on repository %s", tag, version, vp.repository)
		}
	}
	return nil
}

// getDigest fetches the digest of dockerhub image of requested repository and tag
func (vp *V2Provider) getDigest(tag string) (string, error) {
	digest, err := vp.client.ManifestDigest(vp.repository, tag)
	if err != nil {
		return "", errors.Wrapf(err, "Failed to get tag %s on repository %s", tag, vp.repository)
	}
	return digest.String(), nil
}
