package store

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"time"

	stdlog "log"
)

const (
	maxRetries     = 5
	baseRetryDelay = 10 * time.Millisecond
	maxRetryDelay  = 1 * time.Second
	// CockroachDB serialization failure SQLSTATE.
	crdbRetryCode = "40001"
)

// RetryTx executes fn within a transaction, retrying on CockroachDB
// serialization errors (SQLSTATE 40001) with exponential backoff and jitter.
// The function fn receives a *sql.Tx and must not call Commit or Rollback.
func (s *Store) RetryTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}

		err = fn(tx)
		if err != nil {
			_ = tx.Rollback()
			if isRetryable(err) && attempt < maxRetries {
				delay := backoff(attempt)
				stdlog.Printf("WARN: CRDB retry (attempt %d/%d, backoff %s): %v", attempt+1, maxRetries, delay, err)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
				continue
			}
			return err
		}

		if err := tx.Commit(); err != nil {
			if isRetryable(err) && attempt < maxRetries {
				delay := backoff(attempt)
				stdlog.Printf("WARN: CRDB retry on commit (attempt %d/%d, backoff %s): %v", attempt+1, maxRetries, delay, err)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
				continue
			}
			return fmt.Errorf("commit: %w", err)
		}
		return nil
	}

	return fmt.Errorf("transaction failed after %d retries", maxRetries)
}

// isRetryable checks whether err is a CockroachDB serialization error (40001).
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	// pgx wraps errors with the SQLSTATE code accessible via .SQLState() or
	// embedded in the error string. Check both approaches.
	type pgError interface {
		SQLState() string
	}
	if pe, ok := err.(pgError); ok {
		return pe.SQLState() == crdbRetryCode
	}
	// Unwrap for wrapped errors.
	type unwrapper interface {
		Unwrap() error
	}
	if uw, ok := err.(unwrapper); ok {
		return isRetryable(uw.Unwrap())
	}
	// Multiple unwrap (errors.Join).
	type multiUnwrapper interface {
		Unwrap() []error
	}
	if mu, ok := err.(multiUnwrapper); ok {
		for _, e := range mu.Unwrap() {
			if isRetryable(e) {
				return true
			}
		}
	}
	return false
}

// backoff returns the delay for the given attempt with exponential backoff and jitter.
func backoff(attempt int) time.Duration {
	delay := baseRetryDelay << uint(attempt)
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	// Add jitter: 50-150% of the base delay.
	jitter := time.Duration(float64(delay) * (0.5 + rand.Float64()))
	return jitter
}
