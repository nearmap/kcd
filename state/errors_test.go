package state

import (
	"errors"
	"testing"
)

func TestErrorFailed(t *testing.T) {
	existingErr := errors.New("test error")
	failedErr := NewFailedError(existingErr, "test message %s", "and values")

	if failedErr.Cause() != existingErr {
		t.Error("unexpected failed error cause")
	}

	if failedErr.Error() != "test message and values: test error" {
		t.Errorf("unexpected failed error message: '%v'", failedErr.Error())
	}

	if !IsPermanent(failedErr) {
		t.Error("expected FailedError to be a permanent error")
	}
}

type testError struct {
	cause error
}

func (te *testError) Error() string {
	return "test error"
}

func (te *testError) Cause() error {
	return te.cause
}

func TestIsPermanent(t *testing.T) {
	terr := &testError{}

	if IsPermanent(terr) {
		t.Error("expected nil cause not to be permanent")
	}

	terr.cause = errors.New("error without cause")
	if IsPermanent(terr) {
		t.Error("expected error with cause that is an error without cause not to be permanent")
	}

	terr.cause = NewFailedError(errors.New("inner cause"), "failed error message")
	if !IsPermanent(terr) {
		t.Error("expected error with cause that is of type *ErrorFailed to be permanent")
	}
}
