package state

import (
	"context"
	"time"
)

// State represents the state of the current state machine.
type State interface {
	Do(ctx context.Context) (States, error)
}

// OnFailure is invoked when a permanent failure occurs during processing.
type OnFailure interface {
	Fail(ctx context.Context, err error) States
}

// States represents a collection of states returned from a particular machine state.
type States struct {
	// States contains the states that will be independently executed by the state machine.
	States []State

	// OnFailure will be invoked if non-nil if any of the states or their following states
	// fail permanently (i.e. after all retry attempts).
	// If additional OnFailure functions are defined by later states then all functions
	// will be invoked in reverse order (i.e. last defined will be invoked first).
	OnFailure OnFailure
}

// NewStates returns a States instance with the given state operations.
func NewStates(states ...State) States {
	var sts []State
	for _, st := range states {
		if st != nil {
			sts = append(sts, st)
		}
	}
	return States{
		States: sts,
	}
}

// Empty returns true if the States instance is empty.
func (ss States) Empty() bool {
	return len(ss.States) == 0
}

// StateFunc defines a function that implements the State interface.
type StateFunc func(ctx context.Context) (States, error)

// Do implements the State interface.
func (sf StateFunc) Do(ctx context.Context) (States, error) {
	return sf(ctx)
}

// OnFailureFunc defines a function that implements the OnFailure interface.
type OnFailureFunc func(ctx context.Context, err error) States

// Fail implements the OnFailure interface.
func (off OnFailureFunc) Fail(ctx context.Context, err error) States {
	return off(ctx, err)
}

// HasAfter is an interface defining a State that is to be executed after a given time.
type HasAfter interface {
	After() time.Time
}

// AfterState defines a state operation that is invoked after a given duration.
type AfterState struct {
	t     time.Time
	state State
}

// NewAfterState returns a State instance that invokes the given state operation at or after
// the given time.
func NewAfterState(t time.Time, state State) *AfterState {
	// skip any nested AfterState instances.
	for {
		if as, ok := state.(*AfterState); ok {
			state = as.state
		} else {
			break
		}
	}

	return &AfterState{
		t:     t,
		state: state,
	}
}

// Do implements the State interface.
func (as AfterState) Do(ctx context.Context) (States, error) {
	return as.state.Do(ctx)
}

// After implements the HasAfter interface.
func (as AfterState) After() time.Time {
	return as.t
}

// NewErrorState returns a StateFunc that will return the given error.
func NewErrorState(err error) StateFunc {
	return func(ctx context.Context) (States, error) {
		return Error(err)
	}
}

// After performs the state function after a given duration.
func After(d time.Duration, state State) (States, error) {
	return Single(NewAfterState(time.Now().UTC().Add(d), state))
}

// None returns a state that has no additional operations.
func None() (States, error) {
	return NewStates(), nil
}

// Single returns a state with a single specified operation.
func Single(state State) (States, error) {
	return NewStates(state), nil
}

// Many returns a state with multiple operations that are executed independently.
func Many(states ...State) (States, error) {
	return NewStates(states...), nil
}

// Error returns a state that simply returns the given error.
func Error(err error) (States, error) {
	return NewStates(), err
}

// WithFailure returns a state that handles the failure of the given state by invoking
// the given OnFailure func.
func WithFailure(state State, onFailure OnFailure) StateFunc {
	return func(ctx context.Context) (States, error) {
		sts := NewStates(state)
		sts.OnFailure = onFailure
		return sts, nil
	}
}
