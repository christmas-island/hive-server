package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/christmas-island/hive-server/internal/model"
	"github.com/christmas-island/hive-server/internal/timing"
)

// EnqueueClaim adds an agent to the waiting queue for a resource.
// Returns the waiter record and the agent's position in the queue (1-based).
func (s *Store) EnqueueClaim(ctx context.Context, w *model.ClaimWaiter) (*model.ClaimWaiter, int, error) {
	defer timing.TrackDB(ctx, time.Now())
	w.ID = uuid.New().String()
	now := time.Now().UTC()
	w.QueuedAt = now

	metaJSON, err := json.Marshal(w.Metadata)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal metadata: %w", err)
	}
	if w.Metadata == nil {
		metaJSON = []byte(`{}`)
	}
	if w.ExpiresInSec <= 0 {
		w.ExpiresInSec = 3600
	}

	var position int
	err = s.RetryTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO claim_queue (id, resource, agent_id, type, metadata,
			 session_key, session_id, channel, sender_id, sender_is_owner, sandboxed,
			 expires_in_sec, queued_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			w.ID, w.Resource, w.AgentID, string(w.Type), string(metaJSON),
			w.SessionContext.SessionKey, w.SessionContext.SessionID, w.SessionContext.Channel,
			w.SessionContext.SenderID, w.SessionContext.SenderIsOwner, w.SessionContext.Sandboxed,
			w.ExpiresInSec, now.Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("insert queue entry: %w", err)
		}

		// Compute 1-based position inside the same transaction for a consistent
		// snapshot. Count by id (stable) rather than by timestamp to avoid
		// duplicate-position issues under concurrent inserts at the same ms.
		row := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM claim_queue WHERE resource = $1`,
			w.Resource,
		)
		return row.Scan(&position)
	})
	if err != nil {
		return nil, 0, err
	}
	return w, position, nil
}

// PopNextWaiter removes and returns the earliest waiter for a resource,
// or returns nil if the queue is empty. Must be called inside a transaction
// that is also promoting the waiter to claim holder (for atomicity).
func (s *Store) PopNextWaiter(ctx context.Context, tx *sql.Tx, resource string) (*model.ClaimWaiter, error) {
	defer timing.TrackDB(ctx, time.Now())
	// Find the earliest queued entry.
	row := tx.QueryRowContext(ctx,
		`SELECT id, resource, agent_id, type, metadata,
		        session_key, session_id, channel, sender_id, sender_is_owner, sandboxed,
		        expires_in_sec, queued_at
		 FROM claim_queue WHERE resource = $1
		 ORDER BY queued_at ASC LIMIT 1`,
		resource,
	)
	w, err := scanWaiterRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("pop next waiter: %w", err)
	}

	// Remove from queue.
	if _, err := tx.ExecContext(ctx, `DELETE FROM claim_queue WHERE id = $1`, w.ID); err != nil {
		return nil, fmt.Errorf("delete queue entry: %w", err)
	}
	return w, nil
}

// sweepExpiredWaiters deletes claim_queue entries whose TTL has elapsed.
// Called inside the ExpireOldClaims transaction; errors are non-fatal.
func sweepExpiredWaiters(ctx context.Context, tx *sql.Tx, now time.Time) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, expires_in_sec, queued_at FROM claim_queue`,
	)
	if err != nil {
		return err
	}
	var staleIDs []string
	for rows.Next() {
		var id, queuedStr string
		var expSec int
		if err := rows.Scan(&id, &expSec, &queuedStr); err != nil {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, queuedStr)
		if err != nil {
			continue
		}
		if now.After(t.Add(time.Duration(expSec) * time.Second)) {
			staleIDs = append(staleIDs, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range staleIDs {
		tx.ExecContext(ctx, `DELETE FROM claim_queue WHERE id = $1`, id) //nolint:errcheck
	}
	return nil
}

// QueueDepth returns the number of agents waiting for a resource.
func (s *Store) QueueDepth(ctx context.Context, resource string) (int, error) {
	defer timing.TrackDB(ctx, time.Now())
	var n int
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM claim_queue WHERE resource = $1`, resource)
	return n, row.Scan(&n)
}

func scanWaiterRow(row *sql.Row) (*model.ClaimWaiter, error) {
	var w model.ClaimWaiter
	var metaRaw, queuedStr string
	err := row.Scan(
		&w.ID, &w.Resource, &w.AgentID, &w.Type, &metaRaw,
		&w.SessionContext.SessionKey, &w.SessionContext.SessionID, &w.SessionContext.Channel,
		&w.SessionContext.SenderID, &w.SessionContext.SenderIsOwner, &w.SessionContext.Sandboxed,
		&w.ExpiresInSec, &queuedStr,
	)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(metaRaw), &w.Metadata); err != nil {
		w.Metadata = nil
	}
	t, err := time.Parse(time.RFC3339Nano, queuedStr)
	if err == nil {
		w.QueuedAt = t
	}
	return &w, nil
}
