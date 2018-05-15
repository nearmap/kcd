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
	HealthFilename   string
	OperationTimeout time.Duration
	MaxRetries       int

	Stats    stats.Stats
	Recorder events.Recorder
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
	state   State
	ctx     context.Context
	cancel  context.CancelFunc
	retries int
}

func (o *op) new(state State) *op {
	return &op{
		state:   state,
		ctx:     o.ctx,
		cancel:  o.cancel,
		retries: 0,
	}
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
		HealthFilename:   "/statemachine/health",
		OperationTimeout: 5 * time.Minute,
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

	for i := uint64(1); ; i++ {
		m.updateSyncStatus()

		select {
		case op := <-m.ops:
			m.doOp(op)
			i = 1
		case ch := <-m.stop:
			ch <- nil
			return
		default:
			sleep := time.Duration(i*5) * time.Second
			if sleep > maxSleepSeconds*time.Second {
				sleep = maxSleepSeconds * time.Second
			}
			time.Sleep(sleep)
		}
	}
}

func (m *Machine) doOp(o *op) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic processing operation %s: %+v", ID(o.ctx), r)
		}
	}()

	if err := o.ctx.Err(); err != nil {
		log.Printf("Operation %s context error: %+v", ID(o.ctx), err)
		return
	}

	states, err := o.state.Do(o.ctx)
	if err != nil && IsPermanent(err) {
		log.Printf("Operation %s failed with permanent error: %+v", ID(o.ctx), err)
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
			states = NewStates(NewAfterState(2*time.Second, o.state))
		}
	}

	if states.Empty() {
		o.cancel()
		go m.newOp()
		return
	}

	var ops []*op
	for _, st := range states.States {
		ops = append(ops, o.new(st))
	}

	for _, op := range ops {
		opVal := op
		go func() {
			if after, ok := opVal.state.(HasAfter); ok {
				time.Sleep(after.After())
			}
			m.ops <- opVal
		}()
	}
}

func (m *Machine) newOp() {
	var cancel context.CancelFunc
	ctx := context.WithValue(m.ctx, ctxID, uuid.Formatter(uuid.NewV4(), uuid.FormatCanonical))
	ctx, cancel = context.WithTimeout(ctx, m.options.OperationTimeout)

	m.ops <- &op{
		ctx:    ctx,
		cancel: cancel,
		state:  m.start,
	}
}

type ctxKey int

const (
	ctxID ctxKey = iota
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

// Stop stops the state machine, returning any errors encountered.
func (m *Machine) Stop() error {
	ch := make(chan error)
	m.stop <- ch
	return <-ch
}
