//go:build integration

package store_test

import (
	"testing"

	"github.com/christmas-island/hive-server/internal/model"
)

func TestUpsertAndGetChannel(t *testing.T) {
	s := newTestStore(t)

	ch := &model.DiscoveryChannel{
		ID:        "allclaws",
		Name:      "All Claws",
		DiscordID: "123456789",
		Purpose:   "General agent chat",
		Category:  "Agents",
		Members:   []string{"smokeyclaw", "jakeclaw"},
	}

	got, err := s.UpsertChannel(t.Context(), ch)
	if err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if got.ID != "allclaws" {
		t.Errorf("ID = %q, want allclaws", got.ID)
	}
	if got.Name != "All Claws" {
		t.Errorf("Name = %q, want All Claws", got.Name)
	}
	if len(got.Members) != 2 {
		t.Errorf("Members len = %d, want 2", len(got.Members))
	}

	// Verify GetChannel.
	fetched, err := s.GetChannel(t.Context(), "allclaws")
	if err != nil {
		t.Fatalf("GetChannel: %v", err)
	}
	if fetched.DiscordID != "123456789" {
		t.Errorf("DiscordID = %q, want 123456789", fetched.DiscordID)
	}

	// Upsert again (update).
	ch.Name = "All Claws Updated"
	updated, err := s.UpsertChannel(t.Context(), ch)
	if err != nil {
		t.Fatalf("UpsertChannel update: %v", err)
	}
	if updated.Name != "All Claws Updated" {
		t.Errorf("updated Name = %q, want All Claws Updated", updated.Name)
	}
}

func TestListChannels(t *testing.T) {
	s := newTestStore(t)

	// Pre-clean discovery_channels so prior test data doesn't leak.
	if _, err := s.DB().ExecContext(t.Context(), "DELETE FROM discovery_channels"); err != nil {
		t.Fatalf("pre-clean discovery_channels: %v", err)
	}

	for _, id := range []string{"ch1", "ch2", "ch3"} {
		_, err := s.UpsertChannel(t.Context(), &model.DiscoveryChannel{
			ID:   id,
			Name: "Channel " + id,
		})
		if err != nil {
			t.Fatalf("UpsertChannel %s: %v", id, err)
		}
	}

	channels, err := s.ListChannels(t.Context())
	if err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	if len(channels) != 3 {
		t.Errorf("len = %d, want 3", len(channels))
	}
}

func TestDeleteChannel(t *testing.T) {
	s := newTestStore(t)

	_, err := s.UpsertChannel(t.Context(), &model.DiscoveryChannel{ID: "todelete", Name: "Delete Me"})
	if err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}

	if err := s.DeleteChannel(t.Context(), "todelete"); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}

	// Verify gone.
	_, err = s.GetChannel(t.Context(), "todelete")
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}

	// Delete non-existent → ErrNotFound.
	if err := s.DeleteChannel(t.Context(), "nosuchchannel"); err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound for missing channel, got %v", err)
	}
}

func TestUpsertAndGetRole(t *testing.T) {
	s := newTestStore(t)

	role := &model.DiscoveryRole{
		ID:        "agents",
		Name:      "Agents",
		DiscordID: "987654321",
		Members:   []string{"smokeyclaw"},
	}

	got, err := s.UpsertRole(t.Context(), role)
	if err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if got.ID != "agents" {
		t.Errorf("ID = %q, want agents", got.ID)
	}
	if len(got.Members) != 1 {
		t.Errorf("Members len = %d, want 1", len(got.Members))
	}

	fetched, err := s.GetRole(t.Context(), "agents")
	if err != nil {
		t.Fatalf("GetRole: %v", err)
	}
	if fetched.DiscordID != "987654321" {
		t.Errorf("DiscordID = %q, want 987654321", fetched.DiscordID)
	}
}

func TestUpsertAgentMeta(t *testing.T) {
	s := newTestStore(t)

	// Create the agent via Heartbeat first.
	_, err := s.Heartbeat(t.Context(), "meta-agent", []string{"test"}, model.AgentStatusOnline)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	meta := &model.DiscoveryAgentMeta{
		DiscordUserID: "111222333",
		HomeChannel:   "smokeyclaw",
		MentionFormat: "@MetaAgent",
		Channels:      []string{"allclaws", "smokeyclaw"},
	}

	da, err := s.UpsertAgentMeta(t.Context(), "meta-agent", meta)
	if err != nil {
		t.Fatalf("UpsertAgentMeta: %v", err)
	}

	if da.Agent.ID != "meta-agent" {
		t.Errorf("Agent.ID = %q, want meta-agent", da.Agent.ID)
	}
	if da.DiscoveryAgentMeta.DiscordUserID != "111222333" {
		t.Errorf("DiscordUserID = %q, want 111222333", da.DiscoveryAgentMeta.DiscordUserID)
	}
	if da.DiscoveryAgentMeta.MentionFormat != "@MetaAgent" {
		t.Errorf("MentionFormat = %q, want @MetaAgent", da.DiscoveryAgentMeta.MentionFormat)
	}
	if len(da.DiscoveryAgentMeta.Channels) != 2 {
		t.Errorf("Channels len = %d, want 2", len(da.DiscoveryAgentMeta.Channels))
	}

	// GetDiscoveryAgent round-trip.
	fetched, err := s.GetDiscoveryAgent(t.Context(), "meta-agent")
	if err != nil {
		t.Fatalf("GetDiscoveryAgent: %v", err)
	}
	if fetched.HomeChannel != "smokeyclaw" {
		t.Errorf("HomeChannel = %q, want smokeyclaw", fetched.HomeChannel)
	}
}

func TestListDiscoveryAgents(t *testing.T) {
	s := newTestStore(t)

	// Pre-clean agents so prior test heartbeats don't leak.
	if _, err := s.DB().ExecContext(t.Context(), "DELETE FROM agents"); err != nil {
		t.Fatalf("pre-clean agents: %v", err)
	}

	for _, id := range []string{"da1", "da2"} {
		_, err := s.Heartbeat(t.Context(), id, nil, model.AgentStatusOnline)
		if err != nil {
			t.Fatalf("Heartbeat %s: %v", id, err)
		}
	}

	agents, err := s.ListDiscoveryAgents(t.Context())
	if err != nil {
		t.Fatalf("ListDiscoveryAgents: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("len = %d, want 2", len(agents))
	}
	for _, da := range agents {
		if da.Agent == nil {
			t.Error("expected non-nil Agent")
		}
		if da.DiscoveryAgentMeta == nil {
			t.Error("expected non-nil DiscoveryAgentMeta")
		}
	}
}
