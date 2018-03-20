package ecr

import (
	"github.com/pkg/errors"
)

var (
	VersionMismatchErr error = errors.New("VersionMismatch")
	ValidationErr      error = errors.New("ValidationFailed")
)
