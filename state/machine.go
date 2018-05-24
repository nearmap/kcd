package state

import (
	"context"
	"log"
	"time"

	"github.com/nearmap/cvmanager/events"
	"github.com/nearmap/cvmanager/stats"
	"github.com/twinj/uuid"
)

const (
	maxSleepSeconds = 60
)

// Options contains optional state machine parameters.
type Options struct {
	// StartWaitTime is the time to wait before beginning a new "start" operation
	StartWaitTime time.Duration

	OperationTimeout time.Duration
	MaxRetries       int

	Stats    stats.Stats
	Recorder events.Recorder
}

// WithStartWaitTime sets a StartWaitTime duration as options.
func WithStartWaitTime(dur time.Duration) func(*Options) {
	return func(op *Options) {
		op.StartWaitTime = dur
	}
}

// WithStats sets a stats instance for options.
func WithStats(st stats.Stats) func(*Options) {
	return func(op *Options) {
		op.Stats = st
	}
}

// WithRecorder sets a recorder instance for options.
func WithRecorder(rec events.Recorder) func(*Options) {
	return func(op *Options) {
		op.Recorder = rec
	}
}

type op struct {
	state  State
	ctx    context.Context
	cancel context.CancelFunc

	retries      int
	failureFuncs []OnFailure
}

func (o *op) new(state State, failureFunc OnFailure) *op {
	newOp := &op{
		state:        state,
		ctx:          o.ctx,
		cancel:       o.cancel,
		retries:      0,
		failureFuncs: o.failureFuncs,
	}

	if failureFunc != nil {
		newOp.failureFuncs = append(newOp.failureFuncs, failureFunc)
	}
	return newOp
}

// Machine implements the main state machine loop.
type Machine struct {
	start State
	ops   chan *op
	stop  chan chan error
	ctx   context.Context

	options *Options
}

// NewMachine creates a new Machine instance with a given start state
// and timeout durations for operations.
func NewMachine(start State, options ...func(*Options)) *Machine {
	opts := &Options{
		StartWaitTime:    5 * time.Minute,
		OperationTimeout: 10 * time.Minute,
		MaxRetries:       5,
		Stats:            stats.NewFake(),
		Recorder:         events.NewFakeRecorder(100),
	}
	for _, opt := range options {
		opt(opts)
	}

	ctx := stats.NewContext(context.Background(), opts.Stats)
	ctx = events.NewContext(ctx, opts.Recorder)

	return &Machine{
		start:   start,
		ops:     make(chan *op, 100),
		ctx:     ctx,
		options: opts,
	}
}

// Start the state machine.
func (m *Machine) Start() {
	m.newOp()

	for i := uint64(0); ; i++ {
		select {
		case o := <-m.ops:
			if err := UpdateHealthStatus(); err != nil {
				log.Printf("Failed to update health status: %v", err)
			}
			if m.executeOp(o) {
				i = 0
			} else {
				m.scheduleOps(o)
				m.sleep(i)
			}
		case ch := <-m.stop:
			ch <- nil
			return
		default:
			m.sleep(i)
		}
	}
}

func (m *Machine) sleep(i uint64) {
	if i == 0 {
		return
	}

	sleep := time.Duration(i*5) * time.Second
	if sleep > maxSleepSeconds*time.Second {
		sleep = maxSleepSeconds * time.Second
	}
	time.Sleep(sleep)
}

// canExecute returns true if the operation is in a state that can be executed.
func (m *Machine) canExecute(o *op) bool {
	if aft, ok := o.state.(HasAfter); ok {
		return time.Now().UTC().After(aft.After())
	}
	return true
}

// scheduleOps schedules the given operations on the state machine.
func (m *Machine) scheduleOps(ops ...*op) {
	log.Printf("scheduleOps:")
	for _, o := range ops {
		log.Printf(" -- id=%s, %+v", ID(o.ctx), o)
	}

	go func() {
		for _, op := range ops {
			m.ops <- op
		}
	}()
}

// executeOp executes the given operation. Returns true if the operation was
// executed (either successfully or failed) or false if it could not be executed
// and needs to be rescheduled.
func (m *Machine) executeOp(o *op) bool {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic processing operation %s: %+v", ID(o.ctx), r)
		}
	}()

	log.Printf("Executing operation: %+v", o)

	if err := o.ctx.Err(); err != nil {
		log.Printf("Operation %s context error: %+v", ID(o.ctx), err)
		go m.newOp()
		return true
	}

	if !m.canExecute(o) {
		return false
	}

	states, err := o.state.Do(o.ctx)
	if err != nil && IsPermanent(err) {
		log.Printf("Operation %s failed with permanent error: %+v", ID(o.ctx), err)
		for i := len(o.failureFuncs) - 1; i >= 0; i-- {
			o.failureFuncs[i].Fail(o.ctx, err)
		}
		states = NewStates()
	}
	if err != nil && !IsPermanent(err) {
		log.Printf("Operation %s failed with error: %+v", ID(o.ctx), err)
		o.retries++
		if o.retries > m.options.MaxRetries {
			log.Printf("Operation %s reached maximum number of retries (%d). Giving up.", ID(o.ctx), m.options.MaxRetries)
			states = NewStates()
		} else {
			log.Printf("Retrying operation %s with retry attempt %d", ID(o.ctx), o.retries)
			states = NewStates(NewAfterState(time.Now().UTC().Add(5*time.Second), o.state))
		}
	}

	if states.Empty() {
		o.cancel()
		go m.newOp()
		return true
	}

	var ops []*op
	for _, st := range states.States {
		ops = append(ops, o.new(st, states.OnFailure))
	}
	m.scheduleOps(ops...)

	return true
}

func (m *Machine) newOp() {
	var cancel context.CancelFunc
	ctx := context.WithValue(m.ctx, ctxID, uuid.Formatter(uuid.NewV4(), uuid.FormatCanonical))
	ctx = context.WithValue(ctx, ctxStartTime, time.Now().UTC())
	ctx, cancel = context.WithTimeout(ctx, m.options.OperationTimeout)

	o := &op{
		ctx:    ctx,
		cancel: cancel,
		state:  NewAfterState(time.Now().UTC().Add(m.options.StartWaitTime), m.start),
	}

	log.Printf("newOp: created %+v", o)

	m.ops <- o
}

type ctxKey int

const (
	ctxID ctxKey = iota
	ctxStartTime
)

// ID returns the unique identifier of an operation from its context.
func ID(ctx context.Context) string {
	idVal := ctx.Value(ctxID)
	id, ok := idVal.(string)
	if !ok {
		return "UNKNOWN"
	}
	return id
}

// StartTime returns the time the operation started.
func StartTime(ctx context.Context) time.Time {
	stVal := ctx.Value(ctxStartTime)
	st, ok := stVal.(time.Time)
	if !ok {
		return time.Time{}
	}
	return st
}

// Stop stops the state machine, returning any errors encountered.
func (m *Machine) Stop() error {
	ch := make(chan error)
	m.stop <- ch
	return <-ch
}
