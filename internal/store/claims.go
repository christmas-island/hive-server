package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/christmas-island/hive-server/internal/model"
	"github.com/christmas-island/hive-server/internal/timing"
)

// CreateClaim inserts a new claim and returns it.
// Returns model.ErrConflict if an active claim already exists on the same resource.
func (s *Store) CreateClaim(ctx context.Context, c *model.Claim) (*model.Claim, error) {
	defer timing.TrackDB(ctx, time.Now())
	c.ID = uuid.New().String()
	c.Status = model.ClaimStatusActive
	now := time.Now().UTC()
	c.ClaimedAt = now
	c.UpdatedAt = now

	metaJSON, err := json.Marshal(c.Metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}
	if c.Metadata == nil {
		metaJSON = []byte(`{}`)
	}

	err = s.RetryTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO claims (id, type, resource, agent_id, status, metadata,
			 session_key, session_id, channel, sender_id, sender_is_owner, sandboxed,
			 claimed_at, expires_at, updated_at)
             VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
			c.ID, string(c.Type), c.Resource, c.AgentID, string(c.Status), string(metaJSON),
			c.SessionKey, c.SessionID, c.Channel,
			c.SenderID, c.SenderIsOwner, c.Sandboxed,
			now.Format(time.RFC3339Nano), c.ExpiresAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		)
		return err
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, model.ErrConflict
		}
		return nil, fmt.Errorf("insert claim: %w", err)
	}

	return c, nil
}

// GetClaim retrieves a claim by ID.
func (s *Store) GetClaim(ctx context.Context, id string) (*model.Claim, error) {
	defer timing.TrackDB(ctx, time.Now())
	row := s.db.QueryRowContext(ctx,
		`SELECT id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at
         FROM claims WHERE id = $1`,
		id,
	)
	return scanClaimRow(row)
}

// ListClaims returns claims matching the filter.
func (s *Store) ListClaims(ctx context.Context, f model.ClaimFilter) ([]*model.Claim, error) {
	defer timing.TrackDB(ctx, time.Now())
	q := `SELECT id, type, resource, agent_id, status, metadata, session_key, session_id, channel, sender_id, sender_is_owner, sandboxed, claimed_at, expires_at, updated_at
          FROM claims WHERE 1=1`
	args := []any{}
	argIdx := 1

	if f.Type != "" {
		q += fmt.Sprintf(` AND type = $%d`, argIdx)
		args = append(args, f.Type)
		argIdx++
	}
	if f.AgentID != "" {
		q += fmt.Sprintf(` AND agent_id = $%d`, argIdx)
		args = append(args, f.AgentID)
		argIdx++
	}
	if f.Resource != "" {
		q += fmt.Sprintf(` AND resource = $%d`, argIdx)
		args = append(args, f.Resource)
		argIdx++
	}
	if f.Status != "" {
		q += fmt.Sprintf(` AND status = $%d`, argIdx)
		args = append(args, f.Status)
		argIdx++
	}
	if f.SessionKey != "" {
		q += fmt.Sprintf(` AND session_key = $%d`, argIdx)
		args = append(args, f.SessionKey)
		argIdx++
	}
	q += ` ORDER BY claimed_at DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	} else {
		q += ` LIMIT 50`
	}
	if f.Offset > 0 {
		q += fmt.Sprintf(` OFFSET %d`, f.Offset)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list claims: %w", err)
	}

	var claims []*model.Claim
	for rows.Next() {
		c, err := scanClaimRows(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		claims = append(claims, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	return claims, nil
}

// ReleaseClaim sets a claim's status to released and atomically promotes the
// next waiter in the queue (if any) to a new active claim on the same resource.
// Returns a ClaimReleaseResult with the released claim and the next holder (if any).
func (s *Store) ReleaseClaim(ctx context.Context, id string) (*model.ClaimReleaseResult, error) {
	defer timing.TrackDB(ctx, time.Now())
	var released *model.Claim
	var next *model.ClaimWaiter

	err := s.RetryTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().UTC()

		// Release the current claim.
		res, err := tx.ExecContext(ctx,
			`UPDATE claims SET status = $1, updated_at = $2 WHERE id = $3`,
			string(model.ClaimStatusReleased), now.Format(time.RFC3339Nano), id,
		)
		if err != nil {
			return fmt.Errorf("release claim: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return model.ErrNotFound
		}

		// Fetch the released claim to learn the resource.
		row := tx.QueryRowContext(ctx,
			`SELECT id, type, resource, agent_id, status, metadata,
			        session_key, session_id, channel, sender_id, sender_is_owner, sandboxed,
			        claimed_at, expires_at, updated_at
			 FROM claims WHERE id = $1`, id,
		)
		released, err = scanClaimRowTx(row)
		if err != nil {
			return fmt.Errorf("fetch released claim: %w", err)
		}

		// Pop the next waiter from the queue (FIFO).
		next, err = s.PopNextWaiter(ctx, tx, released.Resource)
		if err != nil {
			return fmt.Errorf("pop next waiter: %w", err)
		}
		if next == nil {
			return nil // queue empty, nothing to promote
		}

		// Promote the next waiter to active claim holder.
		metaJSON, _ := json.Marshal(next.Metadata)
		if next.Metadata == nil {
			metaJSON = []byte(`{}`)
		}
		newClaimID := uuid.New().String()
		expiresAt := now.Add(time.Duration(next.ExpiresInSec) * time.Second)
		_, err = tx.ExecContext(ctx,
			`INSERT INTO claims (id, type, resource, agent_id, status, metadata,
			 session_key, session_id, channel, sender_id, sender_is_owner, sandboxed,
			 claimed_at, expires_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
			newClaimID, string(next.Type), next.Resource, next.AgentID,
			string(model.ClaimStatusActive), string(metaJSON),
			next.SessionContext.SessionKey, next.SessionContext.SessionID,
			next.SessionContext.Channel, next.SessionContext.SenderID,
			next.SessionContext.SenderIsOwner, next.SessionContext.Sandboxed,
			now.Format(time.RFC3339Nano), expiresAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		)
		return err
	})
	if err != nil {
		return nil, err
	}

	return &model.ClaimReleaseResult{
		Released: true,
		Claim:    released,
		Next:     next,
	}, nil
}

// scanClaimRowTx scans a claim row from a *sql.Row inside a transaction.
func scanClaimRowTx(row *sql.Row) (*model.Claim, error) {
	return scanClaimRow(row)
}

// RenewClaim extends the expiry of an active claim.
// Returns model.ErrNotFound if the claim does not exist or is not active.
func (s *Store) RenewClaim(ctx context.Context, id string, expiresAt time.Time) (*model.Claim, error) {
	defer timing.TrackDB(ctx, time.Now())
	err := s.RetryTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().UTC()
		res, err := tx.ExecContext(ctx,
			`UPDATE claims SET expires_at = $1, updated_at = $2 WHERE id = $3 AND status = $4`,
			expiresAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), id, string(model.ClaimStatusActive),
		)
		if err != nil {
			return fmt.Errorf("renew claim: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return model.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetClaim(ctx, id)
}

// ExpireOldClaims marks all active claims past their expiry as expired, and for
// each expired resource promotes the next queued waiter to active holder.
// Also purges stale queue entries whose queue TTL has elapsed.
func (s *Store) ExpireOldClaims(ctx context.Context) (int64, error) {
	var count int64
	err := s.RetryTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().UTC()

		// 1. Purge stale queue entries: fetch all, delete those past their TTL.
		// queued_at is stored as RFC3339 text; we do the arithmetic in Go for
		// portability across CockroachDB versions and test drivers.
		if err := sweepExpiredWaiters(ctx, tx, now); err != nil {
			_ = err // non-fatal; orphaned entries are an annoyance, not a correctness bug
		}

		// 2. Find all resources with active claims that have now expired.
		rows, err := tx.QueryContext(ctx,
			`SELECT DISTINCT resource FROM claims
			 WHERE status = $1 AND expires_at < $2`,
			string(model.ClaimStatusActive), now.Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("query expired resources: %w", err)
		}
		var resources []string
		for rows.Next() {
			var r string
			if err := rows.Scan(&r); err != nil {
				rows.Close()
				return err
			}
			resources = append(resources, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		// 3. Expire active claims on those resources.
		res, err := tx.ExecContext(ctx,
			`UPDATE claims SET status = $1, updated_at = $2 WHERE status = $3 AND expires_at < $4`,
			string(model.ClaimStatusExpired), now.Format(time.RFC3339Nano),
			string(model.ClaimStatusActive), now.Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("expire claims: %w", err)
		}
		count, _ = res.RowsAffected()

		// 4. For each expired resource, promote the next queue waiter (if any).
		for _, resource := range resources {
			next, err := s.PopNextWaiter(ctx, tx, resource)
			if err != nil || next == nil {
				continue // empty queue or transient error; skip promotion
			}
			metaJSON, _ := json.Marshal(next.Metadata)
			if next.Metadata == nil {
				metaJSON = []byte(`{}`)
			}
			expiresAt := now.Add(time.Duration(next.ExpiresInSec) * time.Second)
			_, _ = tx.ExecContext(ctx,
				`INSERT INTO claims (id, type, resource, agent_id, status, metadata,
				 session_key, session_id, channel, sender_id, sender_is_owner, sandboxed,
				 claimed_at, expires_at, updated_at)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
				uuid.New().String(), string(next.Type), next.Resource, next.AgentID,
				string(model.ClaimStatusActive), string(metaJSON),
				next.SessionContext.SessionKey, next.SessionContext.SessionID,
				next.SessionContext.Channel, next.SessionContext.SenderID,
				next.SessionContext.SenderIsOwner, next.SessionContext.Sandboxed,
				now.Format(time.RFC3339Nano), expiresAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
			)
		}
		return nil
	})
	return count, err
}
