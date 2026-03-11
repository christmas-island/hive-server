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

// CreateCapturedSession persists a new captured session and returns it with
// hive-assigned ID and created_at.
func (s *Store) CreateCapturedSession(ctx context.Context, cs *model.CapturedSession) (*model.CapturedSession, error) {
	defer timing.TrackDB(ctx, time.Now())
	cs.ID = uuid.New().String()
	now := time.Now().UTC()
	cs.CreatedAt = now

	pathsJSON, err := json.Marshal(cs.Paths)
	if err != nil {
		return nil, fmt.Errorf("marshal paths: %w", err)
	}
	turnsJSON, err := json.Marshal(cs.Turns)
	if err != nil {
		return nil, fmt.Errorf("marshal turns: %w", err)
	}
	toolsJSON, err := json.Marshal(cs.ToolCalls)
	if err != nil {
		return nil, fmt.Errorf("marshal tool_calls: %w", err)
	}
	metaJSON, err := json.Marshal(cs.Metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}
	var usageToMarshal any = cs.Usage
	if cs.Usage == nil {
		usageToMarshal = map[string]any{}
	}
	usageJSON, err := json.Marshal(usageToMarshal)
	if err != nil {
		return nil, fmt.Errorf("marshal usage: %w", err)
	}

	var startedAt, finishedAt string
	if cs.StartedAt != nil {
		startedAt = cs.StartedAt.Format(time.RFC3339Nano)
	}
	if cs.FinishedAt != nil {
		finishedAt = cs.FinishedAt.Format(time.RFC3339Nano)
	}

	err = s.RetryTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO captured_sessions
			 (id, agent_id, session_key, session_id, channel, sender_id,
			  model, provider, started_at, finished_at,
			  repo, paths, summary, turns, tool_calls, metadata, parent_session_id,
			  usage, created_at)
			 VALUES
			 ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)`,
			cs.ID, cs.AgentID, cs.SessionKey, cs.SessionID, cs.Channel, cs.SenderID,
			cs.Model, cs.Provider, startedAt, finishedAt,
			cs.Repo, string(pathsJSON), cs.Summary,
			string(turnsJSON), string(toolsJSON), string(metaJSON),
			cs.ParentSessionID, string(usageJSON), now.Format(time.RFC3339Nano),
		)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("insert captured_session: %w", err)
	}
	return cs, nil
}

// GetCapturedSession retrieves a single captured session by ID.
func (s *Store) GetCapturedSession(ctx context.Context, id string) (*model.CapturedSession, error) {
	defer timing.TrackDB(ctx, time.Now())
	row := s.db.QueryRowContext(ctx,
		`SELECT id, agent_id, session_key, session_id, channel, sender_id,
		        model, provider, started_at, finished_at,
		        repo, paths, summary, turns, tool_calls, metadata, parent_session_id,
		        usage, created_at
		 FROM captured_sessions WHERE id = $1`,
		id,
	)
	cs, err := scanCapturedSession(row)
	if err == sql.ErrNoRows {
		return nil, model.ErrNotFound
	}
	return cs, err
}

// ListCapturedSessions returns sessions matching the given filter.
func (s *Store) ListCapturedSessions(ctx context.Context, f model.SessionFilter) ([]*model.CapturedSession, error) {
	defer timing.TrackDB(ctx, time.Now())
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `SELECT id, agent_id, session_key, session_id, channel, sender_id,
	                 model, provider, started_at, finished_at,
	                 repo, paths, summary, turns, tool_calls, metadata, parent_session_id,
	                 usage, created_at
	          FROM captured_sessions WHERE 1=1`
	args := []any{}
	i := 1

	if f.AgentID != "" {
		query += fmt.Sprintf(" AND agent_id = $%d", i)
		args = append(args, f.AgentID)
		i++
	}
	if f.Repo != "" {
		query += fmt.Sprintf(" AND repo = $%d", i)
		args = append(args, f.Repo)
		i++
	}
	if f.Path != "" {
		// paths is a JSONB array; check if any element contains the path substring.
		query += fmt.Sprintf(" AND paths::text LIKE $%d", i)
		args = append(args, "%"+f.Path+"%")
		i++
	}
	if !f.Since.IsZero() {
		query += fmt.Sprintf(" AND started_at >= $%d", i)
		args = append(args, f.Since.Format(time.RFC3339Nano))
		i++
	}

	query += fmt.Sprintf(" ORDER BY started_at DESC LIMIT $%d", i)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list captured_sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*model.CapturedSession
	for rows.Next() {
		cs, err := scanCapturedSessionRow(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, cs)
	}
	return sessions, rows.Err()
}

// scanCapturedSession scans a single row from QueryRowContext.
func scanCapturedSession(row *sql.Row) (*model.CapturedSession, error) {
	var cs model.CapturedSession
	var pathsJSON, turnsJSON, toolsJSON, metaJSON, usageJSON string
	var startedAt, finishedAt, createdAt string

	err := row.Scan(
		&cs.ID, &cs.AgentID, &cs.SessionKey, &cs.SessionID, &cs.Channel, &cs.SenderID,
		&cs.Model, &cs.Provider, &startedAt, &finishedAt,
		&cs.Repo, &pathsJSON, &cs.Summary, &turnsJSON, &toolsJSON, &metaJSON,
		&cs.ParentSessionID, &usageJSON, &createdAt,
	)
	if err != nil {
		return nil, err
	}
	return finishCapturedSessionScan(&cs, pathsJSON, turnsJSON, toolsJSON, metaJSON, usageJSON, startedAt, finishedAt, createdAt)
}

// scanCapturedSessionRow scans a row from QueryContext.
func scanCapturedSessionRow(rows *sql.Rows) (*model.CapturedSession, error) {
	var cs model.CapturedSession
	var pathsJSON, turnsJSON, toolsJSON, metaJSON, usageJSON string
	var startedAt, finishedAt, createdAt string

	err := rows.Scan(
		&cs.ID, &cs.AgentID, &cs.SessionKey, &cs.SessionID, &cs.Channel, &cs.SenderID,
		&cs.Model, &cs.Provider, &startedAt, &finishedAt,
		&cs.Repo, &pathsJSON, &cs.Summary, &turnsJSON, &toolsJSON, &metaJSON,
		&cs.ParentSessionID, &usageJSON, &createdAt,
	)
	if err != nil {
		return nil, err
	}
	return finishCapturedSessionScan(&cs, pathsJSON, turnsJSON, toolsJSON, metaJSON, usageJSON, startedAt, finishedAt, createdAt)
}

func finishCapturedSessionScan(
	cs *model.CapturedSession,
	pathsJSON, turnsJSON, toolsJSON, metaJSON, usageJSON,
	startedAt, finishedAt, createdAt string,
) (*model.CapturedSession, error) {
	if err := json.Unmarshal([]byte(pathsJSON), &cs.Paths); err != nil {
		cs.Paths = nil
	}
	if err := json.Unmarshal([]byte(turnsJSON), &cs.Turns); err != nil {
		cs.Turns = nil
	}
	if err := json.Unmarshal([]byte(toolsJSON), &cs.ToolCalls); err != nil {
		cs.ToolCalls = nil
	}
	if err := json.Unmarshal([]byte(metaJSON), &cs.Metadata); err != nil {
		cs.Metadata = nil
	}
	var u model.CapturedUsage
	if err := json.Unmarshal([]byte(usageJSON), &u); err == nil {
		cs.Usage = &u
	}
	if t, err := time.Parse(time.RFC3339Nano, startedAt); err == nil {
		cs.StartedAt = &t
	}
	if t, err := time.Parse(time.RFC3339Nano, finishedAt); err == nil {
		cs.FinishedAt = &t
	}
	if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		cs.CreatedAt = t
	}
	return cs, nil
}
