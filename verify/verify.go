package verify

import (
	"context"

	kcd1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	"github.com/nearmap/kcd/registry"
	"github.com/nearmap/kcd/state"
	"k8s.io/client-go/kubernetes"
)

var (
	// ErrFailed is an error indicating that the verification process failed.
	ErrFailed = state.NewFailed("Verification failed")
)

// NewVerifier returns a state instance that implements a verifier, as defined in the verify spec.
func NewVerifier(cs kubernetes.Interface, registryProvider registry.Provider, namespace, version string,
	spec kcd1.VerifySpec, next state.State) (state.States, error) {

	var verifier state.State
	switch spec.Kind {
	case KindImage:
		verifier = NewImageVerifier(cs, registryProvider, namespace, spec, next)
	default:
		return state.Error(state.NewFailed("unknown verify type: %v", spec.Kind))
	}

	return state.Single(verifier)
}

// NewVerifiers returns a state function that invokes verify operations for the given verify specs.
func NewVerifiers(cs kubernetes.Interface, registryProvider registry.Provider, namespace, version string,
	kcdvs []kcd1.VerifySpec, next state.State) state.StateFunc {

	return newVerifiers(cs, registryProvider, namespace, version, kcdvs, next, 0)
}

func newVerifiers(cs kubernetes.Interface, registryProvider registry.Provider, namespace, version string,
	kcdvs []kcd1.VerifySpec, next state.State, idx int) state.StateFunc {

	return func(ctx context.Context) (state.States, error) {
		if idx >= len(kcdvs) {
			return state.Single(next)
		}

		return NewVerifier(cs, registryProvider, namespace, version, kcdvs[idx],
			newVerifiers(cs, registryProvider, namespace, version, kcdvs, next, idx+1))
	}
}
