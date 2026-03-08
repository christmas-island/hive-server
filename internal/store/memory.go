package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when an optimistic concurrency check fails.
var ErrConflict = errors.New("version conflict")

// MemoryEntry represents a shared memory entry.
type MemoryEntry struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	AgentID   string    `json:"agent_id"`
	Tags      []string  `json:"tags"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MemoryFilter holds optional filter parameters for listing memory entries.
type MemoryFilter struct {
	Tag    string
	Agent  string
	Prefix string
	Limit  int
	Offset int
}

// UpsertMemory creates or updates a memory entry.
// If version > 0 in entry, it performs an optimistic concurrency check.
func (s *Store) UpsertMemory(ctx context.Context, entry *MemoryEntry) (*MemoryEntry, error) {
	now := time.Now().UTC()
	if entry.Tags == nil {
		entry.Tags = []string{}
	}
	tagsJSON, err := json.Marshal(entry.Tags)
	if err != nil {
		return nil, fmt.Errorf("marshal tags: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Check if entry exists already.
	var existing MemoryEntry
	var tagsRaw, createdStr, updatedStr string
	err = tx.QueryRowContext(ctx,
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE key = ?`,
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
             VALUES (?, ?, ?, ?, 1, ?, ?)`,
			entry.Key, entry.Value, entry.AgentID, string(tagsJSON),
			createdAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		)
		if err != nil {
			return nil, fmt.Errorf("insert memory: %w", err)
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
			return nil, ErrConflict
		}
		_, err = tx.ExecContext(ctx,
			`UPDATE memory SET value = ?, agent_id = ?, tags = ?, version = version + 1, updated_at = ?
             WHERE key = ?`,
			entry.Value, entry.AgentID, string(tagsJSON),
			now.Format(time.RFC3339Nano), entry.Key,
		)
		if err != nil {
			return nil, fmt.Errorf("update memory: %w", err)
		}
		entry.Version = existing.Version + 1
		entry.CreatedAt = existing.CreatedAt
		entry.UpdatedAt = now

	default:
		return nil, fmt.Errorf("query memory: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return entry, nil
}

// GetMemory retrieves a single memory entry by key.
func (s *Store) GetMemory(ctx context.Context, key string) (*MemoryEntry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE key = ?`,
		key,
	)
	return scanMemoryRow(row)
}

// ListMemory returns memory entries matching the filter.
func (s *Store) ListMemory(ctx context.Context, f MemoryFilter) ([]*MemoryEntry, error) {
	q := `SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE 1=1`
	args := []any{}

	if f.Tag != "" {
		q += ` AND json_each.value = ?`
		// We use json_each to search tags.
		q = `SELECT m.key, m.value, m.agent_id, m.tags, m.version, m.created_at, m.updated_at
             FROM memory m, json_each(m.tags)
             WHERE json_each.value = ?`
		args = append(args, f.Tag)
		if f.Agent != "" {
			q += ` AND m.agent_id = ?`
			args = append(args, f.Agent)
		}
		if f.Prefix != "" {
			q += ` AND m.key LIKE ?`
			args = append(args, f.Prefix+"%")
		}
		q += ` GROUP BY m.key ORDER BY m.updated_at DESC`
	} else {
		if f.Agent != "" {
			q += ` AND agent_id = ?`
			args = append(args, f.Agent)
		}
		if f.Prefix != "" {
			q += ` AND key LIKE ?`
			args = append(args, f.Prefix+"%")
		}
		q += ` ORDER BY updated_at DESC`
	}

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

	var entries []*MemoryEntry
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
func (s *Store) DeleteMemory(ctx context.Context, key string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM memory WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// scanMemoryRow scans a *sql.Row into a MemoryEntry.
func scanMemoryRow(row *sql.Row) (*MemoryEntry, error) {
	var e MemoryEntry
	var tagsRaw string
	var createdStr, updatedStr string
	err := row.Scan(&e.Key, &e.Value, &e.AgentID, &tagsRaw, &e.Version, &createdStr, &updatedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan memory: %w", err)
	}
	return finishMemoryScan(&e, tagsRaw, createdStr, updatedStr)
}

// scanMemoryRows scans a *sql.Rows into a MemoryEntry.
func scanMemoryRows(rows *sql.Rows) (*MemoryEntry, error) {
	var e MemoryEntry
	var tagsRaw string
	var createdStr, updatedStr string
	if err := rows.Scan(&e.Key, &e.Value, &e.AgentID, &tagsRaw, &e.Version, &createdStr, &updatedStr); err != nil {
		return nil, fmt.Errorf("scan memory row: %w", err)
	}
	return finishMemoryScan(&e, tagsRaw, createdStr, updatedStr)
}

func finishMemoryScan(e *MemoryEntry, tagsRaw, createdStr, updatedStr string) (*MemoryEntry, error) {
	if err := json.Unmarshal([]byte(tagsRaw), &e.Tags); err != nil {
		// Fall back to empty slice on bad JSON.
		e.Tags = []string{}
	}
	if e.Tags == nil {
		e.Tags = []string{}
	}
	t, err := time.Parse(time.RFC3339Nano, createdStr)
	if err != nil {
		t, err = time.Parse(time.RFC3339, createdStr)
	}
	if err == nil {
		e.CreatedAt = t
	}
	t, err = time.Parse(time.RFC3339Nano, updatedStr)
	if err != nil {
		t, err = time.Parse(time.RFC3339, updatedStr)
	}
	if err == nil {
		e.UpdatedAt = t
	}
	return e, nil
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
