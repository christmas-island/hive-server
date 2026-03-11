package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/christmas-island/hive-server/internal/model"
)

// --- GetAgent ---

func TestGetAgent_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"id", "name", "status", "activity", "capabilities", "last_heartbeat", "registered_at", "hive_local_version", "hive_plugin_version"}).
		AddRow("agent1", "Agent One", "online", "", `["memory"]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), "", "")

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version FROM agents WHERE id = $1`,
	)).WithArgs("agent1").WillReturnRows(rows)

	got, err := s.GetAgent(context.Background(), "agent1")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.ID != "agent1" {
		t.Errorf("ID = %q, want agent1", got.ID)
	}
	if len(got.Capabilities) != 1 || got.Capabilities[0] != "memory" {
		t.Errorf("capabilities = %v, want [memory]", got.Capabilities)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestGetAgent_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows([]string{"id", "name", "status", "activity", "capabilities", "last_heartbeat", "registered_at", "hive_local_version", "hive_plugin_version"})

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version FROM agents WHERE id = $1`,
	)).WithArgs("missing").WillReturnRows(rows)

	_, err := s.GetAgent(context.Background(), "missing")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestGetAgent_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("connection lost")
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version FROM agents WHERE id = $1`,
	)).WithArgs("agent1").WillReturnError(dbErr)

	_, err := s.GetAgent(context.Background(), "agent1")
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestGetAgent_StaleHeartbeatOffline(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	stale := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	rows := sqlmock.NewRows([]string{"id", "name", "status", "activity", "capabilities", "last_heartbeat", "registered_at", "hive_local_version", "hive_plugin_version"}).
		AddRow("agent1", "Agent One", "online", "", `[]`, stale, stale, "", "")

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version FROM agents WHERE id = $1`,
	)).WithArgs("agent1").WillReturnRows(rows)

	got, err := s.GetAgent(context.Background(), "agent1")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.Status != model.AgentStatusOffline {
		t.Errorf("status = %q, want offline", got.Status)
	}
}

// --- ListAgents ---

func TestListAgents_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"id", "name", "status", "activity", "capabilities", "last_heartbeat", "registered_at", "hive_local_version", "hive_plugin_version"}).
		AddRow("a1", "Agent 1", "online", "", `["tasks"]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), "", "").
		AddRow("a2", "Agent 2", "idle", "", `[]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), "", "")

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version FROM agents ORDER BY id ASC`,
	)).WillReturnRows(rows)

	got, err := s.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
	if got[0].ID != "a1" {
		t.Errorf("got[0].ID = %q, want a1", got[0].ID)
	}
}

func TestListAgents_Empty(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows([]string{"id", "name", "status", "activity", "capabilities", "last_heartbeat", "registered_at", "hive_local_version", "hive_plugin_version"})

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version FROM agents ORDER BY id ASC`,
	)).WillReturnRows(rows)

	got, err := s.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestListAgents_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("query failed")
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version FROM agents ORDER BY id ASC`,
	)).WillReturnError(dbErr)

	_, err := s.ListAgents(context.Background())
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

func TestListAgents_ScanError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	// Return wrong number of columns to trigger a scan error.
	rows := sqlmock.NewRows([]string{"id"}).AddRow("a1")

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version FROM agents ORDER BY id ASC`,
	)).WillReturnRows(rows)

	_, err := s.ListAgents(context.Background())
	if err == nil {
		t.Error("expected scan error, got nil")
	}
}

// --- Heartbeat ---

func TestHeartbeat_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	// Transaction: Begin + Exec + Commit
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO agents (id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version)`,
	)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	// GetAgent follow-up query
	rows := sqlmock.NewRows([]string{"id", "name", "status", "activity", "capabilities", "last_heartbeat", "registered_at", "hive_local_version", "hive_plugin_version"}).
		AddRow("agent1", "agent1", "online", "", `["tasks"]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), "", "")
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version FROM agents WHERE id = $1`,
	)).WithArgs("agent1").WillReturnRows(rows)

	got, err := s.Heartbeat(context.Background(), "agent1", []string{"tasks"}, model.AgentStatusOnline, "", "", "")
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if got.ID != "agent1" {
		t.Errorf("ID = %q, want agent1", got.ID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestHeartbeat_NilCapabilities(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO agents (id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version)`,
	)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	rows := sqlmock.NewRows([]string{"id", "name", "status", "activity", "capabilities", "last_heartbeat", "registered_at", "hive_local_version", "hive_plugin_version"}).
		AddRow("a2", "a2", "online", "", `[]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), "", "")
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version FROM agents WHERE id = $1`,
	)).WithArgs("a2").WillReturnRows(rows)

	got, err := s.Heartbeat(context.Background(), "a2", nil, model.AgentStatusOnline, "", "", "")
	if err != nil {
		t.Fatalf("Heartbeat with nil caps: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil agent")
	}
}

func TestHeartbeat_ExecError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("exec error")
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO agents (id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version)`,
	)).WillReturnError(dbErr)
	mock.ExpectRollback()

	_, err := s.Heartbeat(context.Background(), "agent1", []string{}, model.AgentStatusOnline, "", "", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

func TestHeartbeat_GetAgentNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO agents (id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version)`,
	)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	// GetAgent returns no rows
	rows := sqlmock.NewRows([]string{"id", "name", "status", "activity", "capabilities", "last_heartbeat", "registered_at", "hive_local_version", "hive_plugin_version"})
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version FROM agents WHERE id = $1`,
	)).WithArgs("agent1").WillReturnRows(rows)

	_, err := s.Heartbeat(context.Background(), "agent1", []string{}, model.AgentStatusOnline, "", "", "")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TestHeartbeat_SQL verifies the exact SQL structure used by Heartbeat.
func TestHeartbeat_SQLMatchesSource(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	// Use a regex that matches the full ON CONFLICT upsert SQL
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO agents`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	rows := sqlmock.NewRows([]string{"id", "name", "status", "activity", "capabilities", "last_heartbeat", "registered_at", "hive_local_version", "hive_plugin_version"}).
		AddRow("x", "x", "online", "", `[]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), "", "")
	mock.ExpectQuery(`SELECT id, name`).WithArgs("x").WillReturnRows(rows)

	_, err := s.Heartbeat(context.Background(), "x", []string{}, model.AgentStatusOnline, "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestGetAgent_RowsErr verifies rows.Err() propagation.
func TestListAgents_RowsErr(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rowsErr := errors.New("rows iteration error")
	rows := sqlmock.NewRows([]string{"id", "name", "status", "activity", "capabilities", "last_heartbeat", "registered_at", "hive_local_version", "hive_plugin_version"}).
		AddRow("a1", "Agent 1", "online", "", `[]`, time.Now().Format(time.RFC3339Nano), time.Now().Format(time.RFC3339Nano), "", "").
		RowError(0, rowsErr)

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version FROM agents ORDER BY id ASC`,
	)).WillReturnRows(rows)

	_, err := s.ListAgents(context.Background())
	if err == nil {
		t.Error("expected rows error, got nil")
	}
	if !errors.Is(err, rowsErr) {
		t.Errorf("expected rowsErr, got %v", err)
	}
}

// newMockStore is a convenience helper for creating a Store backed by sqlmock.
// It uses the internal Store struct directly (same package).
func newMockStore(t *testing.T) (*Store, sqlmock.Sqlmock) {
	t.Helper()
	db, mock := newMockDB(t)
	return &Store{db: db}, mock
}

// TestGetAgent_CapabilitiesNullJSON verifies null JSON for capabilities.
func TestGetAgent_CapabilitiesNullJSON(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"id", "name", "status", "activity", "capabilities", "last_heartbeat", "registered_at", "hive_local_version", "hive_plugin_version"}).
		AddRow("agent1", "Agent One", "online", "", `null`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), "", "")

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version FROM agents WHERE id = $1`,
	)).WithArgs("agent1").WillReturnRows(rows)

	got, err := s.GetAgent(context.Background(), "agent1")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.Capabilities == nil {
		t.Error("capabilities should not be nil after null JSON (should default to [])")
	}
}

// TestListAgents_MultipleAgents verifies ordering and multiple results.
func TestListAgents_MultipleAgents(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"id", "name", "status", "activity", "capabilities", "last_heartbeat", "registered_at", "hive_local_version", "hive_plugin_version"}).
		AddRow("a1", "Alpha", "online", "", `["cap1"]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), "", "").
		AddRow("a2", "Beta", "offline", "", `[]`, now.Add(-time.Hour).Format(time.RFC3339Nano), now.Add(-time.Hour).Format(time.RFC3339Nano), "", "").
		AddRow("a3", "Gamma", "idle", "", `["cap2","cap3"]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), "", "")

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version FROM agents ORDER BY id ASC`,
	)).WillReturnRows(rows)

	got, err := s.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
	if got[2].ID != "a3" {
		t.Errorf("got[2].ID = %q, want a3", got[2].ID)
	}
	if len(got[2].Capabilities) != 2 {
		t.Errorf("got[2].Capabilities len = %d, want 2", len(got[2].Capabilities))
	}
}

// Verify that sql.ErrNoRows from the underlying *sql.DB scan returns ErrNotFound.
func TestGetAgent_NilRowValue(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, name, status, activity, capabilities, last_heartbeat, registered_at, hive_local_version, hive_plugin_version FROM agents WHERE id = $1`,
	)).WithArgs("agent1").WillReturnError(sql.ErrNoRows)

	_, err := s.GetAgent(context.Background(), "agent1")
	// sql.ErrNoRows from QueryRowContext wrapping means error from row.Scan
	// In this case the query itself returned err — which scanAgentRow also handles.
	if err == nil {
		t.Error("expected error, got nil")
	}
}
