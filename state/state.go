package state

import (
	"context"
	"time"
)

// State represents the state of the current state machine.
type State interface {
	Do(ctx context.Context) (States, error)
}

// States represents a collection of states returned from a particular machine state.
// Implemented as a struct so that additional semantics can be added in the future.
type States struct {
	States []State
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

// HasAfter is an interface defining a State that is to be executed after a given duration.
type HasAfter interface {
	After() time.Duration
}

// AfterState defines a state operation that is invoked after a given duration.
type AfterState struct {
	dur   time.Duration
	state State
}

// NewAfterState returns a State instance that invokes the given state operation after
// the given duration.
func NewAfterState(d time.Duration, state State) State {
	return &AfterState{
		dur:   d,
		state: state,
	}
}

// Do implements the State interface.
func (as AfterState) Do(ctx context.Context) (States, error) {
	return as.state.Do(ctx)
}

// After implements the HasAfter interface.
func (as AfterState) After(ctx context.Context) time.Duration {
	return as.dur
}

// NewErrorState returns a StateFunc that will return the given error.
func NewErrorState(err error) StateFunc {
	return func(ctx context.Context) (States, error) {
		return Error(err)
	}
}

// After performs the state function after a given duration.
func After(d time.Duration, state State) (States, error) {
	return Single(NewAfterState(d, state))
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
