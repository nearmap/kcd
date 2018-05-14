package verify

import (
	"github.com/nearmap/cvmanager/events"
	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/stats"
	"github.com/pkg/errors"
	"k8s.io/client-go/kubernetes"
)

var (
	// ErrFailed is an error indicating that the verification process failed.
	ErrFailed = errors.New("Verification failed")
)

// Verifier is an interface used for verifying deployments or container images.
type Verifier interface {
	// Verify runs a verification determined by the underlying implementation.
	// It returns a Failed verification error if the verification process ran
	// successfully but it was determined that verification failed. Other errors
	// indicate some other temporary failure, such that the verification process
	// can be re-run. A nil error returned indicates that verification was
	// successful.
	Verify() error
}

func NewVerifier(cs kubernetes.Interface, recorder events.Recorder, stats stats.Stats, ns string,
	spec *cv1.VerifySpec) (Verifier, error) {
	var verifier Verifier
	switch spec.Kind {
	case KindImage:
		verifier = NewImageVerifier(cs, recorder, stats, ns, spec)
	default:
		return nil, errors.Errorf("unknown verify type: %v", spec.Kind)
	}
	return verifier, nil
}
