package state

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// stateImpl is an implementation of the State interface for testing purposes.
type stateImpl struct {
	id string

	invoked bool
}

func (si *stateImpl) Do(ctx context.Context) (States, error) {
	si.invoked = true
	return States{}, nil
}

func newStateImpl(id string) *stateImpl {
	return &stateImpl{
		id: id,
	}
}

var st State = &stateImpl{}

func TestNewStates(t *testing.T) {
	state1 := newStateImpl("1")
	state2 := newStateImpl("2")

	var newStatesTests = []struct {
		message  string
		in       []State
		expected States
		isEmpty  bool
	}{
		{"no state", []State{}, States{}, true},
		{"nil state", nil, States{}, true},
		{"single state", []State{state1}, States{[]State{state1}, nil}, false},
		{"multiple states", []State{state1, state2}, States{[]State{state1, state2}, nil}, false},
		{"multiple states with nils", []State{nil, state1, nil, state2, nil}, States{[]State{state1, state2}, nil}, false},
	}

	for _, tst := range newStatesTests {
		t.Run(tst.message, func(t *testing.T) {
			result := NewStates(tst.in...)
			if !reflect.DeepEqual(result, tst.expected) {
				t.Errorf("expected %+v, got %+v", tst.expected, result)
			}
			emptyResult := result.Empty()
			if emptyResult != tst.isEmpty {
				t.Errorf("expected empty result to be %v, got %v", tst.isEmpty, emptyResult)
			}
		})
	}
}

func TestAfter(t *testing.T) {
	state1 := newStateImpl("1")
	dur := time.Second * 23

	states, err := After(dur, state1)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(states.States) != 1 {
		t.Errorf("expected states to have one entry, had %d", len(states.States))
	}

	after, ok := states.States[0].(HasAfter)
	if !ok {
		t.Errorf("expected resulting states to be of type HasAfter")
	}

	expectedAfter := time.Now().UTC().Add(dur)
	if !after.After().Before(expectedAfter.Add(time.Second)) ||
		!after.After().After(expectedAfter.Add(-1*time.Second)) {

		t.Errorf("expected after time to be %v, got %v", expectedAfter, after.After())
	}
}
