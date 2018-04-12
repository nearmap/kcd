package errs

import (
	"github.com/pkg/errors"
)

var (
	// ErrVersionMismatch indicates error when version mismatch occurs
	ErrVersionMismatch error = errors.New("VersionMismatch")
	// ErrValidation indicates error when validation has failed
	ErrValidation error = errors.New("ValidationFailed")
)
