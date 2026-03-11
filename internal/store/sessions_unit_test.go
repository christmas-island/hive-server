package store

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/christmas-island/hive-server/internal/model"
)

var capturedSessionCols = []string{
	"id", "agent_id", "session_key", "session_id", "channel", "sender_id",
	"model", "provider", "started_at", "finished_at",
	"repo", "paths", "summary", "turns", "tool_calls", "metadata",
	"parent_session_id", "usage", "created_at",
}

func makeSessionRow(id, agentID string, now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows(capturedSessionCols).AddRow(
		id, agentID, "", "", "", "",
		"claude-sonnet-4", "anthropic",
		now.Add(-time.Hour).Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		"christmas-island/hive-server", `["internal/store/claims.go"]`, "did stuff",
		`[]`, `[]`, `{}`,
		"", `{"input_tokens":1000}`,
		now.Format(time.RFC3339Nano),
	)
}

func TestCreateCapturedSession_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	started := now.Add(-time.Hour)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO captured_sessions`,
	)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	cs := &model.CapturedSession{
		AgentID:    "test-agent",
		StartedAt:  &started,
		FinishedAt: &now,
		Repo:       "christmas-island/hive-server",
	}
	result, err := s.CreateCapturedSession(context.Background(), cs)
	if err != nil {
		t.Fatalf("CreateCapturedSession: %v", err)
	}
	if result.ID == "" {
		t.Error("ID is empty")
	}
	if result.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCreateCapturedSession_InsertError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO captured_sessions`,
	)).WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	_, err := s.CreateCapturedSession(context.Background(), &model.CapturedSession{AgentID: "a"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetCapturedSession_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, agent_id, session_key, session_id, channel, sender_id,
		        model, provider, started_at, finished_at,
		        repo, paths, summary, turns, tool_calls, metadata, parent_session_id,
		        usage, created_at
		 FROM captured_sessions WHERE id = $1`,
	)).WithArgs("sess-1").WillReturnRows(makeSessionRow("sess-1", "test-agent", now))

	cs, err := s.GetCapturedSession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("GetCapturedSession: %v", err)
	}
	if cs.ID != "sess-1" {
		t.Errorf("ID = %q, want sess-1", cs.ID)
	}
	if cs.AgentID != "test-agent" {
		t.Errorf("AgentID = %q, want test-agent", cs.AgentID)
	}
	if cs.Usage == nil || cs.Usage.InputTokens != 1000 {
		t.Errorf("usage.input_tokens unexpected: %+v", cs.Usage)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestGetCapturedSession_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, agent_id`,
	)).WithArgs("gone").WillReturnRows(sqlmock.NewRows(capturedSessionCols))

	_, err := s.GetCapturedSession(context.Background(), "gone")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListCapturedSessions_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT id, agent_id`).
		WillReturnRows(
			makeSessionRow("s1", "agent1", now).
				AddRow("s2", "agent2", "", "", "", "", "", "", "", "", "", `[]`, "", `[]`, `[]`, `{}`, "", `{}`, now.Format(time.RFC3339Nano)),
		)

	sessions, err := s.ListCapturedSessions(context.Background(), model.SessionFilter{})
	if err != nil {
		t.Fatalf("ListCapturedSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("got %d sessions, want 2", len(sessions))
	}
}

func TestListCapturedSessions_WithFilters(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT id, agent_id`).
		WillReturnRows(makeSessionRow("s1", "agent1", now))

	f := model.SessionFilter{
		AgentID: "agent1",
		Repo:    "christmas-island/hive-server",
		Path:    "internal/store/",
		Since:   now.Add(-24 * time.Hour),
		Limit:   10,
	}
	sessions, err := s.ListCapturedSessions(context.Background(), f)
	if err != nil {
		t.Fatalf("ListCapturedSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("got %d sessions, want 1", len(sessions))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestListCapturedSessions_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectQuery(`SELECT id, agent_id`).
		WillReturnError(errors.New("db down"))

	_, err := s.ListCapturedSessions(context.Background(), model.SessionFilter{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFinishCapturedSessionScan_InvalidJSON(t *testing.T) {
	cs := &model.CapturedSession{}
	result, err := finishCapturedSessionScan(cs, "not-json", "[]", "[]", "{}", "{}", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Invalid paths JSON → nil paths (graceful)
	if result.Paths != nil {
		t.Error("expected nil paths for invalid JSON")
	}
}

func TestFinishCapturedSessionScan_WithTurns(t *testing.T) {
	now := time.Now().UTC()
	turns, _ := json.Marshal([]model.CapturedTurn{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	})
	tools, _ := json.Marshal([]model.CapturedToolCall{
		{Tool: "exec", Input: `{"command":"ls"}`},
	})
	cs := &model.CapturedSession{}
	result, err := finishCapturedSessionScan(
		cs, `["internal/store/"]`, string(turns), string(tools), `{"key":"val"}`, `{}`,
		now.Add(-time.Hour).Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Turns) != 2 {
		t.Errorf("turns = %d, want 2", len(result.Turns))
	}
	if len(result.ToolCalls) != 1 {
		t.Errorf("tool_calls = %d, want 1", len(result.ToolCalls))
	}
	if result.StartedAt == nil {
		t.Error("StartedAt is nil")
	}
}
