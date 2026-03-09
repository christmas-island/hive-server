package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// AgentStatus represents an agent's presence state.
type AgentStatus string

const (
	AgentStatusOnline  AgentStatus = "online"
	AgentStatusIdle    AgentStatus = "idle"
	AgentStatusOffline AgentStatus = "offline"
)

// offlineThreshold is the duration after which an agent is considered offline.
const offlineThreshold = 5 * time.Minute

// Agent represents a registered agent.
type Agent struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Status        AgentStatus `json:"status"`
	Capabilities  []string    `json:"capabilities"`
	LastHeartbeat time.Time   `json:"last_heartbeat"`
	RegisteredAt  time.Time   `json:"registered_at"`
}

// Heartbeat upserts an agent record, updating its last_heartbeat and status.
// Uses RetryTx to handle CockroachDB serialization conflicts.
func (s *Store) Heartbeat(ctx context.Context, id string, capabilities []string, status AgentStatus) (*Agent, error) {
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
			INSERT INTO agents (id, name, status, capabilities, last_heartbeat, registered_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (id) DO UPDATE SET
				status         = EXCLUDED.status,
				capabilities   = EXCLUDED.capabilities,
				last_heartbeat = EXCLUDED.last_heartbeat
		`, id, id, string(status), string(capsJSON), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("heartbeat upsert: %w", err)
	}

	return s.GetAgent(ctx, id)
}

// GetAgent retrieves a single agent by ID, applying the offline threshold.
func (s *Store) GetAgent(ctx context.Context, id string) (*Agent, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, status, capabilities, last_heartbeat, registered_at FROM agents WHERE id = $1`,
		id,
	)
	return scanAgentRow(row)
}

// ListAgents returns all known agents, computing presence from last_heartbeat.
func (s *Store) ListAgents(ctx context.Context) ([]*Agent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, status, capabilities, last_heartbeat, registered_at FROM agents ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	var agents []*Agent
	for rows.Next() {
		a, err := scanAgentRows(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

func scanAgentRow(row *sql.Row) (*Agent, error) {
	var a Agent
	var capsRaw, hbStr, regStr string
	err := row.Scan(&a.ID, &a.Name, &a.Status, &capsRaw, &hbStr, &regStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan agent: %w", err)
	}
	return finishAgentScan(&a, capsRaw, hbStr, regStr)
}

func scanAgentRows(rows *sql.Rows) (*Agent, error) {
	var a Agent
	var capsRaw, hbStr, regStr string
	if err := rows.Scan(&a.ID, &a.Name, &a.Status, &capsRaw, &hbStr, &regStr); err != nil {
		return nil, fmt.Errorf("scan agent row: %w", err)
	}
	return finishAgentScan(&a, capsRaw, hbStr, regStr)
}

func finishAgentScan(a *Agent, capsRaw, hbStr, regStr string) (*Agent, error) {
	if err := json.Unmarshal([]byte(capsRaw), &a.Capabilities); err != nil {
		a.Capabilities = []string{}
	}
	if a.Capabilities == nil {
		a.Capabilities = []string{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, hbStr); err == nil {
		a.LastHeartbeat = ts
	} else if ts, err := time.Parse(time.RFC3339, hbStr); err == nil {
		a.LastHeartbeat = ts
	}
	if ts, err := time.Parse(time.RFC3339Nano, regStr); err == nil {
		a.RegisteredAt = ts
	} else if ts, err := time.Parse(time.RFC3339, regStr); err == nil {
		a.RegisteredAt = ts
	}

	// Apply offline threshold override.
	if time.Since(a.LastHeartbeat) > offlineThreshold && a.Status != AgentStatusOffline {
		a.Status = AgentStatusOffline
	}
	return a, nil
}
