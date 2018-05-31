package verify

import (
	"context"

	cv1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"github.com/nearmap/cvmanager/state"
	"k8s.io/client-go/kubernetes"
)

var (
	// ErrFailed is an error indicating that the verification process failed.
	ErrFailed = state.NewFailed("Verification failed")
)

// NewVerifier returns a state instance that implements a verifier, as defined in the verify spec.
func NewVerifier(cs kubernetes.Interface, namespace, version string, spec cv1.VerifySpec, next state.State) (state.States, error) {
	var verifier state.State
	switch spec.Kind {
	case KindImage:
		verifier = NewImageVerifier(cs, namespace, spec, next)
	default:
		return state.Error(state.NewFailed("unknown verify type: %v", spec.Kind))
	}

	return state.Single(verifier)
}

// NewVerifiers returns a state function that invokes verify operations for the given verify specs.
func NewVerifiers(cs kubernetes.Interface, namespace, version string, cvvs []cv1.VerifySpec, next state.State) state.StateFunc {
	return newVerifiers(cs, namespace, version, cvvs, next, 0)
}

func newVerifiers(cs kubernetes.Interface, namespace, version string, cvvs []cv1.VerifySpec, next state.State, idx int) state.StateFunc {
	return func(ctx context.Context) (state.States, error) {
		if idx >= len(cvvs) {
			return state.Single(next)
		}

		return NewVerifier(cs, namespace, version, cvvs[idx], newVerifiers(cs, namespace, version, cvvs, next, idx+1))
	}
}
