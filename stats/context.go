package stats

import "context"

type ctxKey int

const (
	statsKey ctxKey = iota
)

// NewContext returns a context populated with the given stats instance.
func NewContext(ctx context.Context, stats Stats) context.Context {
	return context.WithValue(ctx, statsKey, stats)
}

// FromContext returns a Stats instance stored in context, or nil if not exists.
func FromContext(ctx context.Context) Stats {
	if st, has := ctx.Value(statsKey).(Stats); has {
		return st
	}
	return nil
}
