package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/christmas-island/hive-server/internal/model"
)

// UpsertMemory creates or updates a memory entry.
// If version > 0 in entry, it performs an optimistic concurrency check.
// Uses RetryTx to handle CockroachDB serialization conflicts.
func (s *Store) UpsertMemory(ctx context.Context, entry *model.MemoryEntry) (*model.MemoryEntry, error) {
	now := time.Now().UTC()
	if entry.Tags == nil {
		entry.Tags = []string{}
	}
	tagsJSON, err := json.Marshal(entry.Tags)
	if err != nil {
		return nil, fmt.Errorf("marshal tags: %w", err)
	}

	err = s.RetryTx(ctx, func(tx *sql.Tx) error {
		// Check if entry exists already.
		var existing model.MemoryEntry
		var tagsRaw, createdStr, updatedStr string
		err := tx.QueryRowContext(ctx,
			`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE key = $1`,
			entry.Key,
		).Scan(&existing.Key, &existing.Value, &existing.AgentID, &tagsRaw, &existing.Version, &createdStr, &updatedStr)

		switch {
		case errors.Is(err, sql.ErrNoRows):
			// Insert new entry.
			createdAt := now
			if !entry.CreatedAt.IsZero() {
				createdAt = entry.CreatedAt
			}
			_, err = tx.ExecContext(ctx,
				`INSERT INTO memory (key, value, agent_id, tags, version, created_at, updated_at)
                 VALUES ($1, $2, $3, $4, 1, $5, $6)`,
				entry.Key, entry.Value, entry.AgentID, string(tagsJSON),
				createdAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
			)
			if err != nil {
				return fmt.Errorf("insert memory: %w", err)
			}
			entry.Version = 1
			entry.CreatedAt = createdAt
			entry.UpdatedAt = now

		case err == nil:
			// Parse existing timestamps.
			if t, err := time.Parse(time.RFC3339Nano, createdStr); err == nil {
				existing.CreatedAt = t
			} else if t, err := time.Parse(time.RFC3339, createdStr); err == nil {
				existing.CreatedAt = t
			}

			// Optimistic concurrency check.
			if entry.Version > 0 && existing.Version != entry.Version {
				return model.ErrConflict
			}
			_, err = tx.ExecContext(ctx,
				`UPDATE memory SET value = $1, agent_id = $2, tags = $3, version = version + 1, updated_at = $4
                 WHERE key = $5`,
				entry.Value, entry.AgentID, string(tagsJSON),
				now.Format(time.RFC3339Nano), entry.Key,
			)
			if err != nil {
				return fmt.Errorf("update memory: %w", err)
			}
			entry.Version = existing.Version + 1
			entry.CreatedAt = existing.CreatedAt
			entry.UpdatedAt = now

		default:
			return fmt.Errorf("query memory: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// GetMemory retrieves a single memory entry by key.
func (s *Store) GetMemory(ctx context.Context, key string) (*model.MemoryEntry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE key = $1`,
		key,
	)
	return scanMemoryRow(row)
}

// ListMemory returns memory entries matching the filter.
func (s *Store) ListMemory(ctx context.Context, f model.MemoryFilter) ([]*model.MemoryEntry, error) {
	q := `SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE 1=1`
	args := []any{}
	argIdx := 1

	if f.Tag != "" {
		// Use JSONB @> operator to check if the tags array contains the given tag.
		q += fmt.Sprintf(` AND tags @> jsonb_build_array($%d::text)`, argIdx)
		args = append(args, f.Tag)
		argIdx++
	}
	if f.Agent != "" {
		q += fmt.Sprintf(` AND agent_id = $%d`, argIdx)
		args = append(args, f.Agent)
		argIdx++
	}
	if f.Prefix != "" {
		q += fmt.Sprintf(` AND key LIKE $%d`, argIdx)
		args = append(args, f.Prefix+"%")
		argIdx++
	}
	q += ` ORDER BY updated_at DESC`

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
		return nil, fmt.Errorf("list memory: %w", err)
	}
	defer rows.Close()

	var entries []*model.MemoryEntry
	for rows.Next() {
		entry, err := scanMemoryRows(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// DeleteMemory removes a memory entry by key.
// Uses RetryTx to handle CockroachDB serialization conflicts.
func (s *Store) DeleteMemory(ctx context.Context, key string) error {
	return s.RetryTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM memory WHERE key = $1`, key)
		if err != nil {
			return fmt.Errorf("delete memory: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return model.ErrNotFound
		}
		return nil
	})
}


// tagsContain is a helper for in-memory tag filtering (used in tests).
func tagsContain(tags []string, tag string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}
