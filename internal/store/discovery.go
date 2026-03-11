package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/christmas-island/hive-server/internal/model"
	"github.com/christmas-island/hive-server/internal/timing"
)

// --- Channels ---

// UpsertChannel inserts or updates a discovery channel record.
func (s *Store) UpsertChannel(ctx context.Context, ch *model.DiscoveryChannel) (*model.DiscoveryChannel, error) {
	defer timing.TrackDB(ctx, time.Now())
	now := time.Now().UTC()
	membersJSON, err := json.Marshal(ch.Members)
	if err != nil {
		return nil, fmt.Errorf("marshal members: %w", err)
	}
	if ch.Members == nil {
		membersJSON = []byte(`[]`)
	}

	err = s.RetryTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO discovery_channels (id, name, discord_id, purpose, category, members, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (id) DO UPDATE SET
				name       = EXCLUDED.name,
				discord_id = EXCLUDED.discord_id,
				purpose    = EXCLUDED.purpose,
				category   = EXCLUDED.category,
				members    = EXCLUDED.members,
				updated_at = EXCLUDED.updated_at
		`, ch.ID, ch.Name, ch.DiscordID, ch.Purpose, ch.Category, string(membersJSON),
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("upsert channel: %w", err)
	}
	return s.GetChannel(ctx, ch.ID)
}

// GetChannel retrieves a single channel by ID.
func (s *Store) GetChannel(ctx context.Context, id string) (*model.DiscoveryChannel, error) {
	defer timing.TrackDB(ctx, time.Now())
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, discord_id, purpose, category, members, created_at, updated_at
		 FROM discovery_channels WHERE id = $1`,
		id,
	)
	return scanChannelRow(row)
}

// ListChannels returns all discovery channels ordered by ID.
func (s *Store) ListChannels(ctx context.Context) ([]*model.DiscoveryChannel, error) {
	defer timing.TrackDB(ctx, time.Now())
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, discord_id, purpose, category, members, created_at, updated_at
		 FROM discovery_channels ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close()

	var channels []*model.DiscoveryChannel
	for rows.Next() {
		ch, err := scanChannelRows(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

// DeleteChannel removes a channel by ID, returning ErrNotFound if it doesn't exist.
func (s *Store) DeleteChannel(ctx context.Context, id string) error {
	defer timing.TrackDB(ctx, time.Now())
	res, err := s.db.ExecContext(ctx, `DELETE FROM discovery_channels WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete channel: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.ErrNotFound
	}
	return nil
}

// --- Roles ---

// UpsertRole inserts or updates a discovery role record.
func (s *Store) UpsertRole(ctx context.Context, role *model.DiscoveryRole) (*model.DiscoveryRole, error) {
	defer timing.TrackDB(ctx, time.Now())
	now := time.Now().UTC()
	membersJSON, err := json.Marshal(role.Members)
	if err != nil {
		return nil, fmt.Errorf("marshal members: %w", err)
	}
	if role.Members == nil {
		membersJSON = []byte(`[]`)
	}

	err = s.RetryTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO discovery_roles (id, name, discord_id, members, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (id) DO UPDATE SET
				name       = EXCLUDED.name,
				discord_id = EXCLUDED.discord_id,
				members    = EXCLUDED.members,
				updated_at = EXCLUDED.updated_at
		`, role.ID, role.Name, role.DiscordID, string(membersJSON),
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("upsert role: %w", err)
	}
	return s.GetRole(ctx, role.ID)
}

// GetRole retrieves a single role by ID.
func (s *Store) GetRole(ctx context.Context, id string) (*model.DiscoveryRole, error) {
	defer timing.TrackDB(ctx, time.Now())
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, discord_id, members, created_at, updated_at
		 FROM discovery_roles WHERE id = $1`,
		id,
	)
	return scanRoleRow(row)
}

// ListRoles returns all discovery roles ordered by ID.
func (s *Store) ListRoles(ctx context.Context) ([]*model.DiscoveryRole, error) {
	defer timing.TrackDB(ctx, time.Now())
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, discord_id, members, created_at, updated_at
		 FROM discovery_roles ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()

	var roles []*model.DiscoveryRole
	for rows.Next() {
		r, err := scanRoleRows(rows)
		if err != nil {
			return nil, err
		}
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

// DeleteRole removes a role by ID, returning ErrNotFound if it doesn't exist.
func (s *Store) DeleteRole(ctx context.Context, id string) error {
	defer timing.TrackDB(ctx, time.Now())
	res, err := s.db.ExecContext(ctx, `DELETE FROM discovery_roles WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.ErrNotFound
	}
	return nil
}

// --- Agent Discovery Metadata ---

// UpsertAgentMeta updates the discovery metadata columns on an existing agent record.
// Returns ErrNotFound if no agent with the given ID exists.
func (s *Store) UpsertAgentMeta(ctx context.Context, id string, meta *model.DiscoveryAgentMeta) (*model.DiscoveryAgent, error) {
	defer timing.TrackDB(ctx, time.Now())
	channelsJSON, err := json.Marshal(meta.Channels)
	if err != nil {
		return nil, fmt.Errorf("marshal channels: %w", err)
	}
	if meta.Channels == nil {
		channelsJSON = []byte(`[]`)
	}

	var rowsAffected int64
	err = s.RetryTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE agents SET
				discord_user_id = $2,
				home_channel    = $3,
				mention_format  = $4,
				channels        = $5
			WHERE id = $1
		`, id, meta.DiscordUserID, meta.HomeChannel, meta.MentionFormat, string(channelsJSON))
		if err != nil {
			return err
		}
		rowsAffected, _ = res.RowsAffected()
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("upsert agent meta: %w", err)
	}
	if rowsAffected == 0 {
		return nil, model.ErrNotFound
	}
	return s.GetDiscoveryAgent(ctx, id)
}

// GetDiscoveryAgent retrieves a single agent with its discovery metadata by ID.
func (s *Store) GetDiscoveryAgent(ctx context.Context, id string) (*model.DiscoveryAgent, error) {
	defer timing.TrackDB(ctx, time.Now())
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, status, capabilities, last_heartbeat, registered_at,
		       discord_user_id, home_channel, mention_format, channels
		FROM agents WHERE id = $1`,
		id,
	)
	return scanDiscoveryAgentRow(row)
}

// ListDiscoveryAgents returns all agents with their discovery metadata.
func (s *Store) ListDiscoveryAgents(ctx context.Context) ([]*model.DiscoveryAgent, error) {
	defer timing.TrackDB(ctx, time.Now())
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, status, capabilities, last_heartbeat, registered_at,
		       discord_user_id, home_channel, mention_format, channels
		FROM agents ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list discovery agents: %w", err)
	}
	defer rows.Close()

	var agents []*model.DiscoveryAgent
	for rows.Next() {
		da, err := scanDiscoveryAgentRows(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, da)
	}
	return agents, rows.Err()
}

// --- Scan helpers ---

func scanChannelRow(row *sql.Row) (*model.DiscoveryChannel, error) {
	var ch model.DiscoveryChannel
	var membersRaw, createdStr, updatedStr string
	err := row.Scan(&ch.ID, &ch.Name, &ch.DiscordID, &ch.Purpose, &ch.Category,
		&membersRaw, &createdStr, &updatedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan channel: %w", err)
	}
	return finishChannelScan(&ch, membersRaw, createdStr, updatedStr)
}

func scanChannelRows(rows *sql.Rows) (*model.DiscoveryChannel, error) {
	var ch model.DiscoveryChannel
	var membersRaw, createdStr, updatedStr string
	if err := rows.Scan(&ch.ID, &ch.Name, &ch.DiscordID, &ch.Purpose, &ch.Category,
		&membersRaw, &createdStr, &updatedStr); err != nil {
		return nil, fmt.Errorf("scan channel row: %w", err)
	}
	return finishChannelScan(&ch, membersRaw, createdStr, updatedStr)
}

func finishChannelScan(ch *model.DiscoveryChannel, membersRaw, createdStr, updatedStr string) (*model.DiscoveryChannel, error) {
	if err := json.Unmarshal([]byte(membersRaw), &ch.Members); err != nil {
		ch.Members = []string{}
	}
	if ch.Members == nil {
		ch.Members = []string{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, createdStr); err == nil {
		ch.CreatedAt = ts
	} else if ts, err := time.Parse(time.RFC3339, createdStr); err == nil {
		ch.CreatedAt = ts
	}
	if ts, err := time.Parse(time.RFC3339Nano, updatedStr); err == nil {
		ch.UpdatedAt = ts
	} else if ts, err := time.Parse(time.RFC3339, updatedStr); err == nil {
		ch.UpdatedAt = ts
	}
	return ch, nil
}

func scanRoleRow(row *sql.Row) (*model.DiscoveryRole, error) {
	var r model.DiscoveryRole
	var membersRaw, createdStr, updatedStr string
	err := row.Scan(&r.ID, &r.Name, &r.DiscordID, &membersRaw, &createdStr, &updatedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan role: %w", err)
	}
	return finishRoleScan(&r, membersRaw, createdStr, updatedStr)
}

func scanRoleRows(rows *sql.Rows) (*model.DiscoveryRole, error) {
	var r model.DiscoveryRole
	var membersRaw, createdStr, updatedStr string
	if err := rows.Scan(&r.ID, &r.Name, &r.DiscordID, &membersRaw, &createdStr, &updatedStr); err != nil {
		return nil, fmt.Errorf("scan role row: %w", err)
	}
	return finishRoleScan(&r, membersRaw, createdStr, updatedStr)
}

func finishRoleScan(r *model.DiscoveryRole, membersRaw, createdStr, updatedStr string) (*model.DiscoveryRole, error) {
	if err := json.Unmarshal([]byte(membersRaw), &r.Members); err != nil {
		r.Members = []string{}
	}
	if r.Members == nil {
		r.Members = []string{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, createdStr); err == nil {
		r.CreatedAt = ts
	} else if ts, err := time.Parse(time.RFC3339, createdStr); err == nil {
		r.CreatedAt = ts
	}
	if ts, err := time.Parse(time.RFC3339Nano, updatedStr); err == nil {
		r.UpdatedAt = ts
	} else if ts, err := time.Parse(time.RFC3339, updatedStr); err == nil {
		r.UpdatedAt = ts
	}
	return r, nil
}

func scanDiscoveryAgentRow(row *sql.Row) (*model.DiscoveryAgent, error) {
	var a model.Agent
	var meta model.DiscoveryAgentMeta
	var capsRaw, hbStr, regStr, channelsRaw string
	err := row.Scan(&a.ID, &a.Name, &a.Status, &capsRaw, &hbStr, &regStr,
		&meta.DiscordUserID, &meta.HomeChannel, &meta.MentionFormat, &channelsRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan discovery agent: %w", err)
	}
	agent, err := finishAgentScan(&a, capsRaw, hbStr, regStr)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(channelsRaw), &meta.Channels); err != nil {
		meta.Channels = []string{}
	}
	if meta.Channels == nil {
		meta.Channels = []string{}
	}
	return &model.DiscoveryAgent{Agent: agent, DiscoveryAgentMeta: &meta}, nil
}

func scanDiscoveryAgentRows(rows *sql.Rows) (*model.DiscoveryAgent, error) {
	var a model.Agent
	var meta model.DiscoveryAgentMeta
	var capsRaw, hbStr, regStr, channelsRaw string
	if err := rows.Scan(&a.ID, &a.Name, &a.Status, &capsRaw, &hbStr, &regStr,
		&meta.DiscordUserID, &meta.HomeChannel, &meta.MentionFormat, &channelsRaw); err != nil {
		return nil, fmt.Errorf("scan discovery agent row: %w", err)
	}
	agent, err := finishAgentScan(&a, capsRaw, hbStr, regStr)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(channelsRaw), &meta.Channels); err != nil {
		meta.Channels = []string{}
	}
	if meta.Channels == nil {
		meta.Channels = []string{}
	}
	return &model.DiscoveryAgent{Agent: agent, DiscoveryAgentMeta: &meta}, nil
}
