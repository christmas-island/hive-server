// Package timing provides request-scoped DB timing accumulation for the hive
// stack. Each HTTP handler injects a *Accumulator into the request context via
// NewContext; store methods call TrackDB to record query latency. The timing
// middleware reads the accumulated value and emits X-DB-Ms / X-Processing-Ms
// response headers.
package timing

import (
	"context"
	"sync/atomic"
	"time"
)

// ctxKey is the private context key for the Accumulator.
type ctxKey struct{}

// Accumulator tracks cumulative DB query time for a single request.
// It is safe for concurrent use.
type Accumulator struct {
	dbMs atomic.Int64
}

// Add increments the DB timer by delta milliseconds.
func (a *Accumulator) Add(delta int64) {
	a.dbMs.Add(delta)
}

// DBMs returns the total accumulated DB time in milliseconds.
func (a *Accumulator) DBMs() int64 {
	return a.dbMs.Load()
}

// NewContext returns a new context carrying a fresh Accumulator.
func NewContext(ctx context.Context) (context.Context, *Accumulator) {
	acc := &Accumulator{}
	return context.WithValue(ctx, ctxKey{}, acc), acc
}

// FromContext extracts the Accumulator from ctx. Returns nil if not set.
func FromContext(ctx context.Context) *Accumulator {
	acc, _ := ctx.Value(ctxKey{}).(*Accumulator)
	return acc
}

// TrackDB records the elapsed time since start in the context's Accumulator.
// It is a no-op when ctx carries no Accumulator (e.g. direct store tests).
// Usage:
//
//	defer timing.TrackDB(ctx, time.Now())
func TrackDB(ctx context.Context, start time.Time) {
	if acc := FromContext(ctx); acc != nil {
		acc.Add(time.Since(start).Milliseconds())
	}
}
