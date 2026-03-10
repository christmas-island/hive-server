//go:build integration

package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/christmas-island/hive-server/internal/model"
)

// --- Discovery Agent tests ---

func TestUpsertDiscoveryAgent_Create(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a := &model.DiscoveryAgent{
		Name:          "jake-claw",
		DiscordUserID: "U123",
		HomeChannel:   "general",
		Status:        "active",
	}
	got, err := s.UpsertDiscoveryAgent(ctx, a)
	if err != nil {
		t.Fatalf("UpsertDiscoveryAgent: %v", err)
	}
	if got.Name != "jake-claw" {
		t.Errorf("Name = %q, want jake-claw", got.Name)
	}
	if got.DiscordUserID != "U123" {
		t.Errorf("DiscordUserID = %q, want U123", got.DiscordUserID)
	}
	if got.ID == "" {
		t.Error("ID should not be empty")
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

func TestUpsertDiscoveryAgent_Update(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a1 := &model.DiscoveryAgent{Name: "upd-agent", DiscordUserID: "old-id"}
	got1, err := s.UpsertDiscoveryAgent(ctx, a1)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	a2 := &model.DiscoveryAgent{Name: "upd-agent", DiscordUserID: "new-id"}
	got2, err := s.UpsertDiscoveryAgent(ctx, a2)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if got2.DiscordUserID != "new-id" {
		t.Errorf("DiscordUserID = %q, want new-id", got2.DiscordUserID)
	}
	// ID should be preserved (not replaced).
	if got2.ID != got1.ID {
		t.Errorf("ID changed: %q → %q", got1.ID, got2.ID)
	}
}

func TestUpsertDiscoveryAgent_WithCapabilities(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	caps, _ := json.Marshal([]string{"memory", "tasks"})
	a := &model.DiscoveryAgent{
		Name:         "caps-agent",
		Capabilities: json.RawMessage(caps),
	}
	got, err := s.UpsertDiscoveryAgent(ctx, a)
	if err != nil {
		t.Fatalf("UpsertDiscoveryAgent: %v", err)
	}
	var gotCaps []string
	if err := json.Unmarshal(got.Capabilities, &gotCaps); err != nil {
		t.Fatalf("unmarshal capabilities: %v", err)
	}
	if len(gotCaps) != 2 {
		t.Errorf("capabilities len = %d, want 2", len(gotCaps))
	}
}

func TestGetDiscoveryAgent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.UpsertDiscoveryAgent(ctx, &model.DiscoveryAgent{Name: "find-me"})
	if err != nil {
		t.Fatalf("UpsertDiscoveryAgent: %v", err)
	}

	got, err := s.GetDiscoveryAgent(ctx, "find-me")
	if err != nil {
		t.Fatalf("GetDiscoveryAgent: %v", err)
	}
	if got.Name != "find-me" {
		t.Errorf("Name = %q, want find-me", got.Name)
	}
}

func TestGetDiscoveryAgent_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetDiscoveryAgent(context.Background(), "no-such")
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListDiscoveryAgents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, name := range []string{"alpha", "beta", "gamma"} {
		_, err := s.UpsertDiscoveryAgent(ctx, &model.DiscoveryAgent{Name: name})
		if err != nil {
			t.Fatalf("UpsertDiscoveryAgent %q: %v", name, err)
		}
	}

	agents, err := s.ListDiscoveryAgents(ctx)
	if err != nil {
		t.Fatalf("ListDiscoveryAgents: %v", err)
	}
	if len(agents) != 3 {
		t.Errorf("len = %d, want 3", len(agents))
	}
}

func TestListDiscoveryAgents_Empty(t *testing.T) {
	s := newTestStore(t)
	agents, err := s.ListDiscoveryAgents(context.Background())
	if err != nil {
		t.Fatalf("ListDiscoveryAgents: %v", err)
	}
	_ = agents // may be nil or empty
}

// --- Discovery Channel tests ---

func TestUpsertDiscoveryChannel_Create(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &model.DiscoveryChannel{
		Name:             "general",
		DiscordChannelID: "C999",
		Purpose:          "main chat",
	}
	got, err := s.UpsertDiscoveryChannel(ctx, c)
	if err != nil {
		t.Fatalf("UpsertDiscoveryChannel: %v", err)
	}
	if got.Name != "general" {
		t.Errorf("Name = %q, want general", got.Name)
	}
	if got.DiscordChannelID != "C999" {
		t.Errorf("DiscordChannelID = %q, want C999", got.DiscordChannelID)
	}
}

func TestUpsertDiscoveryChannel_Update(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.UpsertDiscoveryChannel(ctx, &model.DiscoveryChannel{Name: "updates-ch", Purpose: "old"})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	got, err := s.UpsertDiscoveryChannel(ctx, &model.DiscoveryChannel{Name: "updates-ch", Purpose: "new"})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if got.Purpose != "new" {
		t.Errorf("Purpose = %q, want new", got.Purpose)
	}
}

func TestGetDiscoveryChannel_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetDiscoveryChannel(context.Background(), "no-such")
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListDiscoveryChannels(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, name := range []string{"ch-a", "ch-b"} {
		_, err := s.UpsertDiscoveryChannel(ctx, &model.DiscoveryChannel{Name: name})
		if err != nil {
			t.Fatalf("UpsertDiscoveryChannel %q: %v", name, err)
		}
	}

	channels, err := s.ListDiscoveryChannels(ctx)
	if err != nil {
		t.Fatalf("ListDiscoveryChannels: %v", err)
	}
	if len(channels) != 2 {
		t.Errorf("len = %d, want 2", len(channels))
	}
}

// --- Discovery Role tests ---

func TestListDiscoveryRoles_Empty(t *testing.T) {
	s := newTestStore(t)
	roles, err := s.ListDiscoveryRoles(context.Background())
	if err != nil {
		t.Fatalf("ListDiscoveryRoles: %v", err)
	}
	_ = roles
}
