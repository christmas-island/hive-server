package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/christmas-island/hive-server/internal/model"
)

// nullableStr returns a sql.NullString that is NULL when s is empty.
func nullableStr(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// --- Discovery Agents ---

// UpsertDiscoveryAgent inserts or updates a discovery agent by name.
func (s *Store) UpsertDiscoveryAgent(ctx context.Context, a *model.DiscoveryAgent) (*model.DiscoveryAgent, error) {
	now := time.Now().UTC()
	if a.ID == "" {
		a.ID = uuid.New().String()
	}

	capsJSON := sql.NullString{}
	if len(a.Capabilities) > 0 {
		capsJSON = sql.NullString{String: string(a.Capabilities), Valid: true}
	}
	metaJSON := sql.NullString{}
	if len(a.Metadata) > 0 {
		metaJSON = sql.NullString{String: string(a.Metadata), Valid: true}
	}

	err := s.RetryTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO discovery_agents
				(id, name, discord_user_id, home_channel, capabilities, status, metadata, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (name) DO UPDATE SET
				discord_user_id = EXCLUDED.discord_user_id,
				home_channel    = EXCLUDED.home_channel,
				capabilities    = EXCLUDED.capabilities,
				status          = EXCLUDED.status,
				metadata        = EXCLUDED.metadata,
				updated_at      = EXCLUDED.updated_at
		`, a.ID, a.Name,
			nullableStr(a.DiscordUserID), nullableStr(a.HomeChannel),
			capsJSON, nullableStr(a.Status), metaJSON,
			now, now,
		)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("upsert discovery agent: %w", err)
	}
	return s.GetDiscoveryAgent(ctx, a.Name)
}

// GetDiscoveryAgent retrieves a discovery agent by name.
func (s *Store) GetDiscoveryAgent(ctx context.Context, name string) (*model.DiscoveryAgent, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, discord_user_id, home_channel, capabilities, status, metadata, created_at, updated_at
		 FROM discovery_agents WHERE name = $1`, name)
	return scanDiscoveryAgentRow(row)
}

// ListDiscoveryAgents returns all discovery agents ordered by name.
func (s *Store) ListDiscoveryAgents(ctx context.Context) ([]*model.DiscoveryAgent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, discord_user_id, home_channel, capabilities, status, metadata, created_at, updated_at
		 FROM discovery_agents ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list discovery agents: %w", err)
	}
	defer rows.Close()

	var agents []*model.DiscoveryAgent
	for rows.Next() {
		a, err := scanDiscoveryAgentRows(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// --- Discovery Channels ---

// UpsertDiscoveryChannel inserts or updates a discovery channel by name.
func (s *Store) UpsertDiscoveryChannel(ctx context.Context, c *model.DiscoveryChannel) (*model.DiscoveryChannel, error) {
	now := time.Now().UTC()
	if c.ID == "" {
		c.ID = uuid.New().String()
	}

	metaJSON := sql.NullString{}
	if len(c.Metadata) > 0 {
		metaJSON = sql.NullString{String: string(c.Metadata), Valid: true}
	}

	err := s.RetryTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO discovery_channels
				(id, name, discord_channel_id, purpose, metadata, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (name) DO UPDATE SET
				discord_channel_id = EXCLUDED.discord_channel_id,
				purpose            = EXCLUDED.purpose,
				metadata           = EXCLUDED.metadata,
				updated_at         = EXCLUDED.updated_at
		`, c.ID, c.Name,
			nullableStr(c.DiscordChannelID), nullableStr(c.Purpose),
			metaJSON, now, now,
		)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("upsert discovery channel: %w", err)
	}
	return s.GetDiscoveryChannel(ctx, c.Name)
}

// GetDiscoveryChannel retrieves a discovery channel by name.
func (s *Store) GetDiscoveryChannel(ctx context.Context, name string) (*model.DiscoveryChannel, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, discord_channel_id, purpose, metadata, created_at, updated_at
		 FROM discovery_channels WHERE name = $1`, name)
	return scanDiscoveryChannelRow(row)
}

// ListDiscoveryChannels returns all discovery channels ordered by name.
func (s *Store) ListDiscoveryChannels(ctx context.Context) ([]*model.DiscoveryChannel, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, discord_channel_id, purpose, metadata, created_at, updated_at
		 FROM discovery_channels ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list discovery channels: %w", err)
	}
	defer rows.Close()

	var channels []*model.DiscoveryChannel
	for rows.Next() {
		c, err := scanDiscoveryChannelRows(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, c)
	}
	return channels, rows.Err()
}

// --- Discovery Roles ---

// ListDiscoveryRoles returns all discovery roles ordered by name.
func (s *Store) ListDiscoveryRoles(ctx context.Context) ([]*model.DiscoveryRole, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, discord_role_id, metadata, created_at, updated_at
		 FROM discovery_roles ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list discovery roles: %w", err)
	}
	defer rows.Close()

	var roles []*model.DiscoveryRole
	for rows.Next() {
		r, err := scanDiscoveryRoleRows(rows)
		if err != nil {
			return nil, err
		}
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

// --- Scan helpers ---

func scanDiscoveryAgentRow(row *sql.Row) (*model.DiscoveryAgent, error) {
	var a model.DiscoveryAgent
	var discordUserID, homeChannel, capsRaw, status, metaRaw sql.NullString
	err := row.Scan(&a.ID, &a.Name, &discordUserID, &homeChannel, &capsRaw, &status, &metaRaw, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan discovery agent: %w", err)
	}
	return finishDiscoveryAgentScan(&a, discordUserID, homeChannel, capsRaw, status, metaRaw)
}

func scanDiscoveryAgentRows(rows *sql.Rows) (*model.DiscoveryAgent, error) {
	var a model.DiscoveryAgent
	var discordUserID, homeChannel, capsRaw, status, metaRaw sql.NullString
	if err := rows.Scan(&a.ID, &a.Name, &discordUserID, &homeChannel, &capsRaw, &status, &metaRaw, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, fmt.Errorf("scan discovery agent row: %w", err)
	}
	return finishDiscoveryAgentScan(&a, discordUserID, homeChannel, capsRaw, status, metaRaw)
}

func finishDiscoveryAgentScan(a *model.DiscoveryAgent, discordUserID, homeChannel, capsRaw, status, metaRaw sql.NullString) (*model.DiscoveryAgent, error) {
	if discordUserID.Valid {
		a.DiscordUserID = discordUserID.String
	}
	if homeChannel.Valid {
		a.HomeChannel = homeChannel.String
	}
	if status.Valid {
		a.Status = status.String
	}
	if capsRaw.Valid && capsRaw.String != "" {
		a.Capabilities = json.RawMessage(capsRaw.String)
	}
	if metaRaw.Valid && metaRaw.String != "" {
		a.Metadata = json.RawMessage(metaRaw.String)
	}
	return a, nil
}

func scanDiscoveryChannelRow(row *sql.Row) (*model.DiscoveryChannel, error) {
	var c model.DiscoveryChannel
	var discordChannelID, purpose, metaRaw sql.NullString
	err := row.Scan(&c.ID, &c.Name, &discordChannelID, &purpose, &metaRaw, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan discovery channel: %w", err)
	}
	return finishDiscoveryChannelScan(&c, discordChannelID, purpose, metaRaw)
}

func scanDiscoveryChannelRows(rows *sql.Rows) (*model.DiscoveryChannel, error) {
	var c model.DiscoveryChannel
	var discordChannelID, purpose, metaRaw sql.NullString
	if err := rows.Scan(&c.ID, &c.Name, &discordChannelID, &purpose, &metaRaw, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, fmt.Errorf("scan discovery channel row: %w", err)
	}
	return finishDiscoveryChannelScan(&c, discordChannelID, purpose, metaRaw)
}

func finishDiscoveryChannelScan(c *model.DiscoveryChannel, discordChannelID, purpose, metaRaw sql.NullString) (*model.DiscoveryChannel, error) {
	if discordChannelID.Valid {
		c.DiscordChannelID = discordChannelID.String
	}
	if purpose.Valid {
		c.Purpose = purpose.String
	}
	if metaRaw.Valid && metaRaw.String != "" {
		c.Metadata = json.RawMessage(metaRaw.String)
	}
	return c, nil
}

func scanDiscoveryRoleRows(rows *sql.Rows) (*model.DiscoveryRole, error) {
	var r model.DiscoveryRole
	var discordRoleID, metaRaw sql.NullString
	if err := rows.Scan(&r.ID, &r.Name, &discordRoleID, &metaRaw, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, fmt.Errorf("scan discovery role row: %w", err)
	}
	if discordRoleID.Valid {
		r.DiscordRoleID = discordRoleID.String
	}
	if metaRaw.Valid && metaRaw.String != "" {
		r.Metadata = json.RawMessage(metaRaw.String)
	}
	return &r, nil
}
