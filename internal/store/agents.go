package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/christmas-island/hive-server/internal/model"
	"github.com/christmas-island/hive-server/internal/timing"
)

// offlineThreshold is the duration after which an agent is considered offline.
const offlineThreshold = 5 * time.Minute

// Heartbeat upserts an agent record, updating its last_heartbeat and status.
// Uses RetryTx to handle CockroachDB serialization conflicts.
func (s *Store) Heartbeat(ctx context.Context, id string, capabilities []string, status model.AgentStatus, hiveLocalVersion string) (*model.Agent, error) {
	defer timing.TrackDB(ctx, time.Now())
	now := time.Now().UTC()
	capsJSON, err := json.Marshal(capabilities)
	if err != nil {
		return nil, fmt.Errorf("marshal capabilities: %w", err)
	}
	if capabilities == nil {
		capsJSON = []byte(`[]`)
	}

	err = s.RetryTx(ctx, func(tx *sql.Tx) error {
		// Upsert: insert or update, preserving registered_at on conflict.
		_, err := tx.ExecContext(ctx, `
			INSERT INTO agents (id, name, status, capabilities, last_heartbeat, registered_at, hive_local_version)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (id) DO UPDATE SET
				status             = EXCLUDED.status,
				capabilities       = EXCLUDED.capabilities,
				last_heartbeat     = EXCLUDED.last_heartbeat,
				hive_local_version = EXCLUDED.hive_local_version
		`, id, id, string(status), string(capsJSON), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), hiveLocalVersion)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("heartbeat upsert: %w", err)
	}

	return s.GetAgent(ctx, id)
}

// GetAgent retrieves a single agent by ID, applying the offline threshold.
func (s *Store) GetAgent(ctx context.Context, id string) (*model.Agent, error) {
	defer timing.TrackDB(ctx, time.Now())
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, status, capabilities, last_heartbeat, registered_at, hive_local_version FROM agents WHERE id = $1`,
		id,
	)
	return scanAgentRow(row)
}

// ListAgents returns all known agents, computing presence from last_heartbeat.
func (s *Store) ListAgents(ctx context.Context) ([]*model.Agent, error) {
	defer timing.TrackDB(ctx, time.Now())
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, status, capabilities, last_heartbeat, registered_at, hive_local_version FROM agents ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	var agents []*model.Agent
	for rows.Next() {
		a, err := scanAgentRows(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}
