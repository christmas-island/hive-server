//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/christmas-island/hive-server/internal/model"
	"github.com/christmas-island/hive-server/internal/store"
)

func makeTestSession(agentID, repo string) *model.CapturedSession {
	now := time.Now().UTC()
	started := now.Add(-5 * time.Minute)
	finished := now
	return &model.CapturedSession{
		AgentID:    agentID,
		SessionKey: "agent:main:discord:channel:123",
		Model:      "claude-sonnet-4",
		Provider:   "anthropic",
		StartedAt:  &started,
		FinishedAt: &finished,
		Repo:       repo,
		Paths:      []string{"internal/store/claims.go", "internal/model/claim.go"},
		Summary:    "Implemented conch queue FIFO logic",
		Turns: []model.CapturedTurn{
			{Role: "user", Content: "implement the queue"},
			{Role: "assistant", Content: "done"},
		},
		ToolCalls: []model.CapturedToolCall{
			{Tool: "exec", Input: `{"command":"go build ./..."}`},
		},
		Usage: &model.CapturedUsage{
			InputTokens:  5000,
			OutputTokens: 2000,
			TotalTokens:  7000,
		},
	}
}

func TestCreateAndGetCapturedSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cs := makeTestSession("test-agent", "christmas-island/hive-server")
	created, err := s.CreateCapturedSession(ctx, cs)
	if err != nil {
		t.Fatalf("CreateCapturedSession: %v", err)
	}
	if created.ID == "" {
		t.Fatal("ID is empty")
	}
	if created.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}

	// Retrieve it.
	got, err := s.GetCapturedSession(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetCapturedSession: %v", err)
	}
	if got.AgentID != "test-agent" {
		t.Errorf("AgentID = %q, want test-agent", got.AgentID)
	}
	if got.Repo != "christmas-island/hive-server" {
		t.Errorf("Repo = %q", got.Repo)
	}
	if len(got.Paths) != 2 {
		t.Errorf("paths len = %d, want 2", len(got.Paths))
	}
	if len(got.Turns) != 2 {
		t.Errorf("turns len = %d, want 2", len(got.Turns))
	}
	if len(got.ToolCalls) != 1 {
		t.Errorf("tool_calls len = %d, want 1", len(got.ToolCalls))
	}
	if got.Usage == nil || got.Usage.InputTokens != 5000 {
		t.Errorf("usage.input_tokens unexpected: %+v", got.Usage)
	}
}

func TestGetCapturedSession_NotFound_Integration(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetCapturedSession(context.Background(), "no-such-id")
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListCapturedSessions_Integration(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create two sessions for different agents.
	_, err := s.CreateCapturedSession(ctx, makeTestSession("agent-a", "christmas-island/hive-server"))
	if err != nil {
		t.Fatalf("create session a: %v", err)
	}
	_, err = s.CreateCapturedSession(ctx, makeTestSession("agent-b", "christmas-island/hive-local"))
	if err != nil {
		t.Fatalf("create session b: %v", err)
	}

	// Filter by agent.
	sessions, err := s.ListCapturedSessions(ctx, model.SessionFilter{AgentID: "agent-a"})
	if err != nil {
		t.Fatalf("ListCapturedSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("got %d sessions for agent-a, want 1", len(sessions))
	}

	// Filter by repo.
	sessions, err = s.ListCapturedSessions(ctx, model.SessionFilter{Repo: "christmas-island/hive-local"})
	if err != nil {
		t.Fatalf("ListCapturedSessions repo filter: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("got %d sessions for hive-local repo, want 1", len(sessions))
	}

	// Filter by path.
	sessions, err = s.ListCapturedSessions(ctx, model.SessionFilter{Path: "internal/store/"})
	if err != nil {
		t.Fatalf("ListCapturedSessions path filter: %v", err)
	}
	if len(sessions) < 2 {
		t.Errorf("got %d sessions with path filter, want >= 2", len(sessions))
	}

	// Filter by since (future = no results).
	sessions, err = s.ListCapturedSessions(ctx, model.SessionFilter{Since: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatalf("ListCapturedSessions since filter: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("got %d sessions for future since, want 0", len(sessions))
	}
}

func TestCreateCapturedSession_WithSubagentHierarchy(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create parent session.
	parent := makeTestSession("parent-agent", "christmas-island/hive-server")
	createdParent, err := s.CreateCapturedSession(ctx, parent)
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}

	// Create child session referencing parent.
	child := makeTestSession("child-agent", "christmas-island/hive-server")
	child.ParentSessionID = createdParent.ID
	createdChild, err := s.CreateCapturedSession(ctx, child)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	got, err := s.GetCapturedSession(ctx, createdChild.ID)
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if got.ParentSessionID != createdParent.ID {
		t.Errorf("ParentSessionID = %q, want %q", got.ParentSessionID, createdParent.ID)
	}
}

// Ensure the store.Store implements the store.Store interface for captured sessions.
var _ interface {
	CreateCapturedSession(ctx interface{ Deadline() (interface{}, bool); Done() <-chan struct{}; Err() error; Value(interface{}) interface{} }, s *model.CapturedSession) (*model.CapturedSession, error)
} = (*store.Store)(nil)
