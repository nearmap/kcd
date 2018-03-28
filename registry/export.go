package registry

import (
	"errors"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/nearmap/cvmanager/registry/config"
	dh "github.com/nearmap/cvmanager/registry/dockerhub"
	"github.com/nearmap/cvmanager/registry/ecr"
	"github.com/nearmap/cvmanager/stats"
	"k8s.io/client-go/kubernetes"
)

type Type int

const (
	ECR Type = iota
	DOCKERHUB
)

func (dr Type) String() string {
	switch dr {
	case ECR:
		return "ecr"
	case DOCKERHUB:
		return "dockerhub"
	default:
		return "ecr"
	}
}

func NewDRType(typ string) (Type, error) {
	switch typ {
	case "ecr":
		return ECR, nil
	case "dockerhub":
		return DOCKERHUB, nil
	default:
		return ECR, errors.New("Request docker registry is not supported")
	}
}

// Registry selects the specified registry type
func Registry(typ Type) func(opts *Options) {
	return func(opts *Options) {
		opts.RegistryType = typ
	}
}

// Syncer offers capability to periodically sync with docker registry
type Syncer interface {
	Sync() error
}

// Tagger provides capability of adding/removing environment tags on ECR
// This interface is purely designed for CI/CD purposes such that the version
// tag ex git SHA is unique on images (images can be uniquely identified by such version tags).
// Environment tags or any other tags are then added or removed from the ECR images.
type Tagger interface {
	// Add adds list of tags to the image identified with version
	Add(ecr string, version string, tags ...string) error
	// Remove removes the list of tags from ECR repository such that no image contains these
	// tags
	Remove(ecr string, tags ...string) error
	// Get gets the list of tags to the image identified with version
	Get(ecr string, version string) ([]string, error)
}

// DRProvider offers interfaces to interact with docker registry for syncing and tagging purposes
// Conforms to Syncer and Tagger
type DRProvider interface {
	Syncer(cs *kubernetes.Clientset, ns string, syncConf *config.SyncConfig) (Syncer, error)
	Tagger() (Tagger, error)
	Stats(sts stats.Stats)
}

type drProvider struct {
	registryType Type
	sess         *session.Session
	stats        stats.Stats
}

type Options struct {
	RegistryType Type
}

func NewDRProvider(sess *session.Session, stats stats.Stats, options ...func(*Options)) *drProvider {

	opts := &Options{
		RegistryType: ECR,
	}
	for _, option := range options {
		option(opts)
	}

	return &drProvider{
		registryType: opts.RegistryType,
		sess:         sess,
		stats:        stats,
	}
}

func (dr *drProvider) Syncer(cs *kubernetes.Clientset, ns string, syncConf *config.SyncConfig) (Syncer, error) {
	switch dr.registryType {
	case ECR:
		return ecr.NewSyncer(dr.sess, cs, ns, syncConf, dr.stats)
	case DOCKERHUB:
		return dh.NewSyncer(cs, ns, syncConf, dr.stats)
	default:
		return nil, errors.New("Invalid type specified")
	}

}

func (dr *drProvider) Tagger() (Tagger, error) {
	switch dr.registryType {
	case ECR:
		return ecr.NewTagger(dr.sess, dr.stats), nil
	case DOCKERHUB:
		return dh.NewTagger(), nil
	default:
		return nil, errors.New("Invalid type specified")
	}

}

func (dr *drProvider) Stats(sts stats.Stats) {
	dr.stats = sts
}
