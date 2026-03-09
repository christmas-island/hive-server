//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/christmas-island/hive-server/internal/model"
)

func TestHeartbeat_Register(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	agent, err := s.Heartbeat(ctx, "agent-1", []string{"memory", "tasks"}, model.AgentStatusOnline)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if agent.ID != "agent-1" {
		t.Errorf("ID = %q, want agent-1", agent.ID)
	}
	if agent.Status != model.AgentStatusOnline {
		t.Errorf("Status = %q, want online", agent.Status)
	}
	if len(agent.Capabilities) != 2 {
		t.Errorf("Capabilities len = %d, want 2", len(agent.Capabilities))
	}
	if agent.LastHeartbeat.IsZero() {
		t.Error("LastHeartbeat is zero")
	}
	if agent.RegisteredAt.IsZero() {
		t.Error("RegisteredAt is zero")
	}
}

func TestHeartbeat_Update(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a1, err := s.Heartbeat(ctx, "agent-upd", []string{"a"}, model.AgentStatusOnline)
	if err != nil {
		t.Fatalf("first heartbeat: %v", err)
	}

	time.Sleep(2 * time.Millisecond)

	a2, err := s.Heartbeat(ctx, "agent-upd", []string{"a", "b"}, model.AgentStatusIdle)
	if err != nil {
		t.Fatalf("second heartbeat: %v", err)
	}

	if a2.Status != model.AgentStatusIdle {
		t.Errorf("Status = %q, want idle", a2.Status)
	}
	if len(a2.Capabilities) != 2 {
		t.Errorf("Capabilities len = %d, want 2", len(a2.Capabilities))
	}
	// registered_at should be preserved.
	if !a2.RegisteredAt.Equal(a1.RegisteredAt) {
		t.Errorf("RegisteredAt changed: %v → %v", a1.RegisteredAt, a2.RegisteredAt)
	}
	// last_heartbeat should be updated.
	if !a2.LastHeartbeat.After(a1.LastHeartbeat) && !a2.LastHeartbeat.Equal(a1.LastHeartbeat) {
		t.Errorf("LastHeartbeat did not update: %v vs %v", a1.LastHeartbeat, a2.LastHeartbeat)
	}
}

func TestHeartbeat_NilCapabilities(t *testing.T) {
	s := newTestStore(t)
	agent, err := s.Heartbeat(context.Background(), "nocaps", nil, model.AgentStatusOnline)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if agent.Capabilities == nil {
		t.Error("Capabilities should not be nil")
	}
}

func TestGetAgent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Heartbeat(ctx, "get-agent", []string{}, model.AgentStatusOnline)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	agent, err := s.GetAgent(ctx, "get-agent")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if agent.ID != "get-agent" {
		t.Errorf("ID = %q, want get-agent", agent.ID)
	}
}

func TestGetAgent_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetAgent(context.Background(), "no-such")
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListAgents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, id := range []string{"a1", "a2", "a3"} {
		_, err := s.Heartbeat(ctx, id, nil, model.AgentStatusOnline)
		if err != nil {
			t.Fatalf("Heartbeat %q: %v", id, err)
		}
	}

	agents, err := s.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 3 {
		t.Errorf("len = %d, want 3", len(agents))
	}
}

func TestListAgents_Empty(t *testing.T) {
	s := newTestStore(t)
	agents, err := s.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	_ = agents // may be nil or empty
}

func TestAgentOfflineThreshold(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Register agent with a stale heartbeat by directly manipulating the DB.
	_, err := s.Heartbeat(ctx, "stale-agent", nil, model.AgentStatusOnline)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	// Set last_heartbeat to 10 minutes ago.
	staleTime := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339Nano)
	_, err = s.DB().ExecContext(ctx,
		`UPDATE agents SET last_heartbeat = $1 WHERE id = 'stale-agent'`, staleTime)
	if err != nil {
		t.Fatalf("update heartbeat: %v", err)
	}

	agent, err := s.GetAgent(ctx, "stale-agent")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if agent.Status != model.AgentStatusOffline {
		t.Errorf("Status = %q, want offline (stale heartbeat)", agent.Status)
	}
}
