package verify

import (
	"github.com/pkg/errors"
)

var (
	Failed = errors.New("Verification failed")
)

type Verifier interface {
	Verify() error
}
