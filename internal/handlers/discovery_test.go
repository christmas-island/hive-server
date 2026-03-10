package handlers_test

import (
	"net/http"
	"testing"

	"github.com/christmas-island/hive-server/internal/model"
)

func TestDiscoveryUpsertAgent(t *testing.T) {
	srv := newTestServer(t, testToken)

	resp := request(t, srv, http.MethodPut, "/api/v1/discovery/agents/jake-claw", map[string]any{
		"discord_user_id": "U123",
		"home_channel":    "general",
		"status":          "active",
	}, testToken, "jake-claw")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var agent model.DiscoveryAgent
	decodeJSON(t, resp, &agent)

	if agent.Name != "jake-claw" {
		t.Errorf("Name = %q, want jake-claw", agent.Name)
	}
	if agent.DiscordUserID != "U123" {
		t.Errorf("DiscordUserID = %q, want U123", agent.DiscordUserID)
	}
	if agent.HomeChannel != "general" {
		t.Errorf("HomeChannel = %q, want general", agent.HomeChannel)
	}
}

func TestDiscoveryUpsertAgent_WrongAgentID(t *testing.T) {
	srv := newTestServer(t, testToken)

	resp := request(t, srv, http.MethodPut, "/api/v1/discovery/agents/jake-claw", map[string]any{
		"status": "active",
	}, testToken, "other-agent")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestDiscoveryUpsertAgent_Update(t *testing.T) {
	srv := newTestServer(t, testToken)

	request(t, srv, http.MethodPut, "/api/v1/discovery/agents/upd-agent", map[string]any{
		"discord_user_id": "OLD",
	}, testToken, "upd-agent").Body.Close()

	resp := request(t, srv, http.MethodPut, "/api/v1/discovery/agents/upd-agent", map[string]any{
		"discord_user_id": "NEW",
	}, testToken, "upd-agent")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var agent model.DiscoveryAgent
	decodeJSON(t, resp, &agent)
	if agent.DiscordUserID != "NEW" {
		t.Errorf("DiscordUserID = %q, want NEW", agent.DiscordUserID)
	}
}

func TestDiscoveryGetAgent(t *testing.T) {
	srv := newTestServer(t, testToken)

	request(t, srv, http.MethodPut, "/api/v1/discovery/agents/find-me", map[string]any{
		"status": "online",
	}, testToken, "find-me").Body.Close()

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/agents/find-me", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var agent model.DiscoveryAgent
	decodeJSON(t, resp, &agent)
	if agent.Name != "find-me" {
		t.Errorf("Name = %q, want find-me", agent.Name)
	}
}

func TestDiscoveryGetAgent_NotFound(t *testing.T) {
	srv := newTestServer(t, testToken)
	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/agents/no-such", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestDiscoveryListAgents(t *testing.T) {
	srv := newTestServer(t, testToken)

	for _, name := range []string{"alpha", "beta", "gamma"} {
		request(t, srv, http.MethodPut, "/api/v1/discovery/agents/"+name, map[string]any{
			"status": "active",
		}, testToken, name).Body.Close()
	}

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/agents", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var agents []model.DiscoveryAgent
	decodeJSON(t, resp, &agents)
	if len(agents) != 3 {
		t.Errorf("len = %d, want 3", len(agents))
	}
}

func TestDiscoveryListAgents_Empty(t *testing.T) {
	srv := newTestServer(t, testToken)
	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/agents", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var agents []model.DiscoveryAgent
	decodeJSON(t, resp, &agents)
	if agents == nil {
		t.Error("expected non-nil agents slice")
	}
}

func TestDiscoveryUpsertChannel(t *testing.T) {
	srv := newTestServer(t, testToken)

	resp := request(t, srv, http.MethodPut, "/api/v1/discovery/channels/general", map[string]any{
		"discord_channel_id": "C999",
		"purpose":            "main chat",
	}, testToken, testAgent)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var ch model.DiscoveryChannel
	decodeJSON(t, resp, &ch)
	if ch.Name != "general" {
		t.Errorf("Name = %q, want general", ch.Name)
	}
	if ch.DiscordChannelID != "C999" {
		t.Errorf("DiscordChannelID = %q, want C999", ch.DiscordChannelID)
	}
}

func TestDiscoveryGetChannel(t *testing.T) {
	srv := newTestServer(t, testToken)

	request(t, srv, http.MethodPut, "/api/v1/discovery/channels/my-ch", map[string]any{
		"purpose": "testing",
	}, testToken, testAgent).Body.Close()

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/channels/my-ch", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var ch model.DiscoveryChannel
	decodeJSON(t, resp, &ch)
	if ch.Name != "my-ch" {
		t.Errorf("Name = %q, want my-ch", ch.Name)
	}
}

func TestDiscoveryGetChannel_NotFound(t *testing.T) {
	srv := newTestServer(t, testToken)
	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/channels/no-such", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestDiscoveryListChannels(t *testing.T) {
	srv := newTestServer(t, testToken)

	for _, name := range []string{"ch-a", "ch-b"} {
		request(t, srv, http.MethodPut, "/api/v1/discovery/channels/"+name, map[string]any{}, testToken, testAgent).Body.Close()
	}

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/channels", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var channels []model.DiscoveryChannel
	decodeJSON(t, resp, &channels)
	if len(channels) != 2 {
		t.Errorf("len = %d, want 2", len(channels))
	}
}

func TestDiscoveryListRoles(t *testing.T) {
	srv := newTestServer(t, testToken)
	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/roles", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var roles []model.DiscoveryRole
	decodeJSON(t, resp, &roles)
	if roles == nil {
		t.Error("expected non-nil roles slice")
	}
}

func TestDiscoveryRouting(t *testing.T) {
	srv := newTestServer(t, testToken)

	request(t, srv, http.MethodPut, "/api/v1/discovery/agents/routed-agent", map[string]any{
		"discord_user_id": "U777",
		"home_channel":    "ops",
	}, testToken, "routed-agent").Body.Close()

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/routing/routed-agent", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var routing model.RoutingInfo
	decodeJSON(t, resp, &routing)
	if routing.Mention != "<@U777>" {
		t.Errorf("Mention = %q, want <@U777>", routing.Mention)
	}
	if routing.HomeChannel != "ops" {
		t.Errorf("HomeChannel = %q, want ops", routing.HomeChannel)
	}
	if routing.SessionKeyFormat != "routed-agent-ai[bot]" {
		t.Errorf("SessionKeyFormat = %q, want routed-agent-ai[bot]", routing.SessionKeyFormat)
	}
}

func TestDiscoveryRouting_NotFound(t *testing.T) {
	srv := newTestServer(t, testToken)
	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/routing/no-such", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
