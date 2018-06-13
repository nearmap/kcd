package config

import (
	"github.com/nearmap/cvmanager/events"
	"github.com/nearmap/cvmanager/stats"
)

// Options contains additional (optional) configuration for the controller
type Options struct {
	Stats    stats.Stats
	Recorder events.Recorder

	UseRollback bool
}

// WithStats applies the stats instance as configuration.
func WithStats(instance stats.Stats) func(*Options) {
	return func(opts *Options) {
		opts.Stats = instance
	}
}

// WithRecorder applies the given recorder as configuration.
func WithRecorder(recorder events.Recorder) func(*Options) {
	return func(opts *Options) {
		opts.Recorder = recorder
	}
}

// WithUseRollback applies the rollback config to controller
func WithUseRollback(r bool) func(*Options) {
	return func(opts *Options) {
		opts.UseRollback = r
	}
}

// NewOptions returns an Options intance with defaults.
func NewOptions() *Options {
	return &Options{
		Stats:       stats.NewFake(),
		Recorder:    events.NewFakeRecorder(100),
		UseRollback: false,
	}
}

// WithOptions returns an options func that sets all the defined options.
func WithOptions(options *Options) func(*Options) {
	return func(opts *Options) {
		opts.Stats = options.Stats
		opts.Recorder = options.Recorder
		opts.UseRollback = options.UseRollback
	}
}
