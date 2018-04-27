package config

import "github.com/nearmap/cvmanager/stats"

// Options contains additional (optional) configuration for the controller
type Options struct {
	Stats stats.Stats

	UseHistory bool

	UseRollback bool
}

// WithStats applies the stats type to the controller
func WithStats(instance stats.Stats) func(*Options) {
	return func(opts *Options) {
		opts.Stats = instance
	}
}

// WithRollback applies the rollback config to controller
func WithRollback(r bool) func(*Options) {
	return func(opts *Options) {
		opts.UseRollback = r
	}
}

// WithHistory applies the history config to controller
func WithHistory(r bool) func(*Options) {
	return func(opts *Options) {
		opts.UseHistory = r
	}
}

func NewOptions() *Options {
	return &Options{
		Stats:       stats.NewFake(),
		UseHistory:  false,
		UseRollback: false,
	}
}
