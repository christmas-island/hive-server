package store

import (
	"testing"

	"github.com/christmas-island/hive-server/internal/model"
)

// TestIsRetryable_NilError checks that nil errors are not retryable.
func TestIsRetryable_NilError(t *testing.T) {
	if isRetryable(nil) {
		t.Error("nil error should not be retryable")
	}
}

// TestIsRetryable_GenericError checks that generic errors are not retryable.
func TestIsRetryable_GenericError(t *testing.T) {
	err := model.ErrNotFound
	if isRetryable(err) {
		t.Error("ErrNotFound should not be retryable")
	}
}

// mockPGError simulates a pgx error with a SQLSTATE code.
type mockPGError struct {
	code string
}

func (e *mockPGError) Error() string    { return "pg error: " + e.code }
func (e *mockPGError) SQLState() string { return e.code }

func TestIsRetryable_SerializationError(t *testing.T) {
	err := &mockPGError{code: "40001"}
	if !isRetryable(err) {
		t.Error("40001 should be retryable")
	}
}

func TestIsRetryable_OtherPGError(t *testing.T) {
	err := &mockPGError{code: "23505"} // unique violation
	if isRetryable(err) {
		t.Error("23505 should not be retryable")
	}
}

// wrappedError wraps another error.
type wrappedError struct {
	inner error
}

func (e *wrappedError) Error() string { return "wrapped: " + e.inner.Error() }
func (e *wrappedError) Unwrap() error { return e.inner }

func TestIsRetryable_WrappedSerializationError(t *testing.T) {
	inner := &mockPGError{code: "40001"}
	err := &wrappedError{inner: inner}
	if !isRetryable(err) {
		t.Error("wrapped 40001 should be retryable")
	}
}

func TestBackoff(t *testing.T) {
	// Just verify it doesn't panic and returns positive durations.
	for i := 0; i < maxRetries; i++ {
		d := backoff(i)
		if d <= 0 {
			t.Errorf("backoff(%d) = %s, want > 0", i, d)
		}
		if d > maxRetryDelay*2 {
			t.Errorf("backoff(%d) = %s, too large", i, d)
		}
	}
}
