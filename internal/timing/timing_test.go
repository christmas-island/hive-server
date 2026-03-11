package timing_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/christmas-island/hive-server/internal/timing"
)

func TestAccumulator_AddAndDBMs(t *testing.T) {
	var acc timing.Accumulator
	if acc.DBMs() != 0 {
		t.Errorf("initial DBMs = %d, want 0", acc.DBMs())
	}
	acc.Add(10)
	acc.Add(25)
	if acc.DBMs() != 35 {
		t.Errorf("DBMs = %d, want 35", acc.DBMs())
	}
}

func TestNewContext(t *testing.T) {
	base := context.Background()
	ctx, acc := timing.NewContext(base)

	if timing.FromContext(base) != nil {
		t.Error("original context should not carry accumulator")
	}
	if got := timing.FromContext(ctx); got != acc {
		t.Error("new context should carry the accumulator")
	}
}

func TestFromContext_NoAccumulator(t *testing.T) {
	ctx := httptest.NewRequest("GET", "/", nil).Context()
	if timing.FromContext(ctx) != nil {
		t.Error("expected nil for context without accumulator")
	}
}

func TestTrackDB_NilSafe(t *testing.T) {
	ctx := context.Background() // no accumulator
	// Should not panic.
	timing.TrackDB(ctx, time.Now())
}

func TestTrackDB_RecordsTime(t *testing.T) {
	ctx, acc := timing.NewContext(context.Background())

	// Record a known delay.
	start := time.Now().Add(-10 * time.Millisecond) // fake 10ms start
	timing.TrackDB(ctx, start)

	if acc.DBMs() < 10 {
		t.Errorf("DBMs = %d, want >= 10", acc.DBMs())
	}
}

func TestTrackDB_Concurrent(t *testing.T) {
	ctx, acc := timing.NewContext(context.Background())
	done := make(chan struct{})

	for i := 0; i < 10; i++ {
		go func() {
			timing.TrackDB(ctx, time.Now().Add(-time.Millisecond))
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	// Each goroutine adds ~1ms; total should be >= 10.
	if acc.DBMs() < 10 {
		t.Errorf("concurrent DBMs = %d, want >= 10", acc.DBMs())
	}
}
