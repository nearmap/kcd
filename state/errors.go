package state

import "fmt"

// ErrorFailed indicates that an operation failed for a permanent reason,
// such as verification failure. Such operations should not be retried.
type ErrorFailed struct {
	message string
	cause   error
}

// Error implements the error interface.
func (ef *ErrorFailed) Error() string {
	return fmt.Sprintf("%s: %s", ef.message, ef.cause.Error())
}

// Cause implements the errors.Cause interface
func (ef *ErrorFailed) Cause() error {
	return ef.cause
}

// NewFailed returns a permanent error of type ErrorFailed, indicating that the operation
// failed for a known reason.
func NewFailed(message string, args ...interface{}) *ErrorFailed {
	return &ErrorFailed{
		message: fmt.Sprintf(message, args...),
	}
}

// NewFailedError returns a permanent error of type ErrorFailed that wraps an existing error,
// indicating that the operation failed for a known reason.
func NewFailedError(err error, message string, args ...interface{}) *ErrorFailed {
	return &ErrorFailed{
		cause:   err,
		message: fmt.Sprintf(message, args...),
	}
}

// IsPermanent returns true if the error returned by an operation indicates
// a permanent failure, which should not be retried.
func IsPermanent(err error) bool {
	type causer interface {
		Cause() error
	}

	for err != nil {
		if _, ok := err.(*ErrorFailed); ok {
			return true
		}
		cause, ok := err.(causer)
		if !ok {
			break
		}
		err = cause.Cause()
	}
	return false
}
