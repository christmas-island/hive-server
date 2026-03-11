// Package store provides the CockroachDB (PostgreSQL-compatible) persistence layer for hive-server.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver for database/sql
)

// Store is the central data access object.
type Store struct {
	db *sql.DB
}

// New opens a PostgreSQL/CockroachDB connection at databaseURL and runs schema migrations.
func New(databaseURL string) (*Store, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	// Verify connectivity.
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error { return s.db.Close() }

// DB returns the underlying *sql.DB (useful for testing).
func (s *Store) DB() *sql.DB { return s.db }

// Ping verifies database connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// migrate creates all required tables if they don't exist.
func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS memory (
    key        TEXT    NOT NULL PRIMARY KEY,
    value      TEXT    NOT NULL DEFAULT '',
    agent_id   TEXT    NOT NULL DEFAULT '',
    tags       JSONB   NOT NULL DEFAULT '[]',
    version    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
    id          TEXT    NOT NULL PRIMARY KEY,
    title       TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    status      TEXT    NOT NULL DEFAULT 'open',
    creator     TEXT    NOT NULL,
    assignee    TEXT    NOT NULL DEFAULT '',
    priority    INTEGER NOT NULL DEFAULT 0,
    tags        JSONB   NOT NULL DEFAULT '[]',
    created_at  TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS task_notes (
    id         BIGSERIAL PRIMARY KEY,
    task_id    TEXT    NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    note       TEXT    NOT NULL,
    agent_id   TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS agents (
    id             TEXT NOT NULL PRIMARY KEY,
    name           TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'offline',
    capabilities   JSONB NOT NULL DEFAULT '[]',
    last_heartbeat TEXT NOT NULL,
    registered_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS discovery_channels (
    id         TEXT NOT NULL PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    discord_id TEXT NOT NULL DEFAULT '',
    purpose    TEXT NOT NULL DEFAULT '',
    category   TEXT NOT NULL DEFAULT '',
    members    JSONB NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS discovery_roles (
    id         TEXT NOT NULL PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    discord_id TEXT NOT NULL DEFAULT '',
    members    JSONB NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

ALTER TABLE agents ADD COLUMN IF NOT EXISTS discord_user_id    TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN IF NOT EXISTS home_channel        TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN IF NOT EXISTS mention_format      TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN IF NOT EXISTS channels            JSONB NOT NULL DEFAULT '[]';
ALTER TABLE agents ADD COLUMN IF NOT EXISTS hive_local_version  TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_memory_agent ON memory(agent_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status   ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_assignee ON tasks(assignee);
CREATE INDEX IF NOT EXISTS idx_tasks_creator  ON tasks(creator);
CREATE INDEX IF NOT EXISTS idx_task_notes_task ON task_notes(task_id);
CREATE INDEX IF NOT EXISTS idx_discovery_channels_discord ON discovery_channels(discord_id);
CREATE INDEX IF NOT EXISTS idx_discovery_roles_discord    ON discovery_roles(discord_id);

CREATE TABLE IF NOT EXISTS claims (
    id          TEXT    NOT NULL PRIMARY KEY,
    type        TEXT    NOT NULL,
    resource    TEXT    NOT NULL,
    agent_id    TEXT    NOT NULL,
    status      TEXT    NOT NULL DEFAULT 'active',
    metadata    JSONB   NOT NULL DEFAULT '{}',
    claimed_at  TEXT    NOT NULL,
    expires_at  TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_claims_active_resource
    ON claims (resource) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_claims_agent ON claims(agent_id);
CREATE INDEX IF NOT EXISTS idx_claims_status ON claims(status);

-- Session context columns for memory, tasks, and claims.
ALTER TABLE memory ADD COLUMN IF NOT EXISTS session_key     TEXT    NOT NULL DEFAULT '';
ALTER TABLE memory ADD COLUMN IF NOT EXISTS session_id      TEXT    NOT NULL DEFAULT '';
ALTER TABLE memory ADD COLUMN IF NOT EXISTS channel         TEXT    NOT NULL DEFAULT '';
ALTER TABLE memory ADD COLUMN IF NOT EXISTS sender_id       TEXT    NOT NULL DEFAULT '';
ALTER TABLE memory ADD COLUMN IF NOT EXISTS sender_is_owner BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE memory ADD COLUMN IF NOT EXISTS sandboxed       BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE tasks ADD COLUMN IF NOT EXISTS session_key     TEXT    NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS session_id      TEXT    NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS channel         TEXT    NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS sender_id       TEXT    NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS sender_is_owner BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS sandboxed       BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE claims ADD COLUMN IF NOT EXISTS session_key     TEXT    NOT NULL DEFAULT '';
ALTER TABLE claims ADD COLUMN IF NOT EXISTS session_id      TEXT    NOT NULL DEFAULT '';
ALTER TABLE claims ADD COLUMN IF NOT EXISTS channel         TEXT    NOT NULL DEFAULT '';
ALTER TABLE claims ADD COLUMN IF NOT EXISTS sender_id       TEXT    NOT NULL DEFAULT '';
ALTER TABLE claims ADD COLUMN IF NOT EXISTS sender_is_owner BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE claims ADD COLUMN IF NOT EXISTS sandboxed       BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_memory_session ON memory(session_key);
CREATE INDEX IF NOT EXISTS idx_tasks_session  ON tasks(session_key);
CREATE INDEX IF NOT EXISTS idx_claims_session ON claims(session_key);

-- Claim queue: agents waiting for a resource that is currently held.
-- When the holder releases the claim, the first waiter (by queued_at) is
-- promoted to holder atomically. TTL on waiters prevents stale queue entries.
CREATE TABLE IF NOT EXISTS claim_queue (
    id             TEXT    NOT NULL PRIMARY KEY,
    resource       TEXT    NOT NULL,
    agent_id       TEXT    NOT NULL,
    type           TEXT    NOT NULL,
    metadata       JSONB   NOT NULL DEFAULT '{}',
    session_key    TEXT    NOT NULL DEFAULT '',
    session_id     TEXT    NOT NULL DEFAULT '',
    channel        TEXT    NOT NULL DEFAULT '',
    sender_id      TEXT    NOT NULL DEFAULT '',
    sender_is_owner BOOLEAN NOT NULL DEFAULT false,
    sandboxed      BOOLEAN NOT NULL DEFAULT false,
    expires_in_sec INTEGER NOT NULL DEFAULT 3600,
    queued_at      TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_claim_queue_resource ON claim_queue(resource, queued_at);
CREATE INDEX IF NOT EXISTS idx_claim_queue_agent    ON claim_queue(agent_id);

-- Captured sessions: recorded agent sessions shipped by OpenClaw/ACP harness.
CREATE TABLE IF NOT EXISTS captured_sessions (
    id               TEXT      NOT NULL PRIMARY KEY,
    agent_id         TEXT      NOT NULL DEFAULT '',
    session_key      TEXT      NOT NULL DEFAULT '',
    session_id       TEXT      NOT NULL DEFAULT '',
    channel          TEXT      NOT NULL DEFAULT '',
    sender_id        TEXT      NOT NULL DEFAULT '',
    model            TEXT      NOT NULL DEFAULT '',
    provider         TEXT      NOT NULL DEFAULT '',
    started_at       TEXT      NOT NULL DEFAULT '',
    finished_at      TEXT      NOT NULL DEFAULT '',
    repo             TEXT      NOT NULL DEFAULT '',
    paths            JSONB     NOT NULL DEFAULT '[]',
    summary          TEXT      NOT NULL DEFAULT '',
    turns            JSONB     NOT NULL DEFAULT '[]',
    tool_calls       JSONB     NOT NULL DEFAULT '[]',
    metadata         JSONB     NOT NULL DEFAULT '{}',
    parent_session_id TEXT     NOT NULL DEFAULT '',
    usage            JSONB     NOT NULL DEFAULT '{}',
    created_at       TEXT      NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_captured_sessions_agent   ON captured_sessions(agent_id);
CREATE INDEX IF NOT EXISTS idx_captured_sessions_repo    ON captured_sessions(repo);
CREATE INDEX IF NOT EXISTS idx_captured_sessions_started ON captured_sessions(started_at);
`
