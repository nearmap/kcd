package events

import "context"

type ctxKey int

const (
	recorderKey ctxKey = iota
)

// NewContext returns a context populated with the given recorder instance.
func NewContext(ctx context.Context, recorder Recorder) context.Context {
	return context.WithValue(ctx, recorderKey, recorder)
}

// FromContext returns a Recorder instance stored in context, or nil if not exists.
func FromContext(ctx context.Context) Recorder {
	if rec, has := ctx.Value(recorderKey).(Recorder); has {
		return rec
	}
	return nil
}
