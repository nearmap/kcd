package verify

import (
	"github.com/pkg/errors"
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
