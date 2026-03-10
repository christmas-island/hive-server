package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/christmas-island/hive-server/internal/model"
)

// newMockDB creates a *sql.DB backed by go-sqlmock and returns it along with
// the Sqlmock controller. The DB is automatically closed on test cleanup.
func newMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, mock
}

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

// TestIsRetryable_MultiUnwrap verifies that isRetryable walks errors.Join output.
func TestIsRetryable_MultiUnwrap(t *testing.T) {
	retryable := &mockPGError{code: "40001"}
	other := model.ErrNotFound
	joined := errors.Join(other, retryable)
	if !isRetryable(joined) {
		t.Error("joined error containing 40001 should be retryable")
	}
}

// TestIsRetryable_MultiUnwrap_NoneRetryable verifies non-retryable joined errors.
func TestIsRetryable_MultiUnwrap_NoneRetryable(t *testing.T) {
	joined := errors.Join(model.ErrNotFound, model.ErrConflict)
	if isRetryable(joined) {
		t.Error("joined error with no 40001 should not be retryable")
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

// TestBackoff_Cap verifies that backoff never exceeds maxRetryDelay (with jitter).
// We use a very high attempt number to force the delay cap branch.
func TestBackoff_Cap(t *testing.T) {
	// attempt=20 → baseRetryDelay << 20 >> maxRetryDelay, so cap must kick in.
	d := backoff(20)
	if d <= 0 {
		t.Errorf("backoff(20) = %s, want > 0", d)
	}
	// With 50-150% jitter applied to maxRetryDelay, upper bound is 2×maxRetryDelay.
	if d > maxRetryDelay*2 {
		t.Errorf("backoff(20) = %s, exceeds 2×maxRetryDelay", d)
	}
}

// --- RetryTx unit tests ---

// TestRetryTx_Success verifies that a fn that succeeds on the first attempt
// commits the transaction and returns nil.
func TestRetryTx_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectCommit()

	called := 0
	err := s.RetryTx(context.Background(), func(_ *sql.Tx) error {
		called++
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if called != 1 {
		t.Errorf("fn called %d times, want 1", called)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestRetryTx_RetryThenSucceed verifies that RetryTx retries on serialization
// errors and eventually succeeds when the fn stops returning retryable errors.
func TestRetryTx_RetryThenSucceed(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	retryable := &mockPGError{code: "40001"}

	// Attempts 1 and 2: fn returns retryable → Begin + Rollback each.
	mock.ExpectBegin()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectRollback()
	// Attempt 3: fn succeeds → Begin + Commit.
	mock.ExpectBegin()
	mock.ExpectCommit()

	attempt := 0
	err := s.RetryTx(context.Background(), func(_ *sql.Tx) error {
		attempt++
		if attempt < 3 {
			return retryable
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error after retries, got %v", err)
	}
	if attempt != 3 {
		t.Errorf("fn called %d times, want 3", attempt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestRetryTx_ExhaustsRetries verifies that RetryTx returns an error when
// the fn keeps returning retryable errors beyond maxRetries.
func TestRetryTx_ExhaustsRetries(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	retryable := &mockPGError{code: "40001"}

	// fn is called maxRetries+1 times (attempts 0..maxRetries), each → Begin + Rollback.
	for range maxRetries + 1 {
		mock.ExpectBegin()
		mock.ExpectRollback()
	}

	attempt := 0
	err := s.RetryTx(context.Background(), func(_ *sql.Tx) error {
		attempt++
		return retryable
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries, got nil")
	}
	if attempt != maxRetries+1 {
		t.Errorf("fn called %d times, want %d", attempt, maxRetries+1)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestRetryTx_NonRetryableError verifies that non-retryable errors are returned
// immediately without retrying.
func TestRetryTx_NonRetryableError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectRollback()

	boom := errors.New("non-retryable")
	attempt := 0
	err := s.RetryTx(context.Background(), func(_ *sql.Tx) error {
		attempt++
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom error, got %v", err)
	}
	if attempt != 1 {
		t.Errorf("fn called %d times, want 1 (no retries)", attempt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestRetryTx_ContextCancelled verifies that RetryTx respects context cancellation
// during the backoff sleep between retries.
func TestRetryTx_ContextCancelled(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	// Only one attempt before context cancellation aborts the backoff.
	mock.ExpectBegin()
	mock.ExpectRollback()

	ctx, cancel := context.WithCancel(context.Background())
	retryable := &mockPGError{code: "40001"}
	attempt := 0
	err := s.RetryTx(ctx, func(_ *sql.Tx) error {
		attempt++
		cancel() // cancel immediately so next backoff returns ctx.Err()
		return retryable
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if attempt != 1 {
		t.Errorf("fn called %d times, want 1", attempt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestRetryTx_BeginError verifies that an error from BeginTx is returned immediately.
func TestRetryTx_BeginError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	beginErr := errors.New("connection refused")
	mock.ExpectBegin().WillReturnError(beginErr)

	err := s.RetryTx(context.Background(), func(_ *sql.Tx) error {
		t.Fatal("fn should not be called when begin fails")
		return nil
	})
	if err == nil {
		t.Fatal("expected error from failed BeginTx, got nil")
	}
	if !errors.Is(err, beginErr) {
		t.Errorf("expected beginErr wrapped, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestRetryTx_CommitRetryThenSucceed verifies that a retryable commit error
// also triggers a retry.
func TestRetryTx_CommitRetryThenSucceed(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	retryable := &mockPGError{code: "40001"}

	// Attempt 1: fn succeeds but commit fails with retryable error.
	mock.ExpectBegin()
	mock.ExpectCommit().WillReturnError(retryable)
	// Attempt 2: fn succeeds and commit succeeds.
	mock.ExpectBegin()
	mock.ExpectCommit()

	fnCalled := 0
	err := s.RetryTx(context.Background(), func(_ *sql.Tx) error {
		fnCalled++
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil after commit retry, got %v", err)
	}
	if fnCalled != 2 {
		t.Errorf("fn called %d times, want 2 (one commit retry)", fnCalled)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
