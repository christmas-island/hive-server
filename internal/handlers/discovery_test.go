package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/christmas-island/hive-server/internal/handlers"
	"github.com/christmas-island/hive-server/internal/model"
)

// newMockServer creates a test server backed by an in-memory mock store.
// No database required.
func newMockServer(t *testing.T) (*httptest.Server, *mockStore) {
	t.Helper()
	ms := newMockStore()
	h := handlers.New(ms, "", nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, ms
}

func TestDiscoveryListAgents(t *testing.T) {
	srv, ms := newMockServer(t)

	// Seed two agents via heartbeat in mock.
	ctx := t.Context()
	_, _ = ms.Heartbeat(ctx, "agent-a", []string{"memory"}, model.AgentStatusOnline)
	_, _ = ms.Heartbeat(ctx, "agent-b", []string{"tasks"}, model.AgentStatusIdle)

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/agents", nil, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var agents []map[string]any
	decodeJSON(t, resp, &agents)
	if len(agents) != 2 {
		t.Errorf("len = %d, want 2", len(agents))
	}
}

func TestDiscoveryGetAgent(t *testing.T) {
	srv, ms := newMockServer(t)

	ctx := t.Context()
	_, _ = ms.Heartbeat(ctx, "smokeyclaw", []string{"memory"}, model.AgentStatusOnline)

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/agents/smokeyclaw", nil, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var da map[string]any
	decodeJSON(t, resp, &da)
	if da["id"] != "smokeyclaw" {
		t.Errorf("id = %v, want smokeyclaw", da["id"])
	}
}

func TestDiscoveryGetAgentNotFound(t *testing.T) {
	srv, _ := newMockServer(t)

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/agents/no-such-agent", nil, "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestDiscoveryGetAgent_StoreError(t *testing.T) {
	srv, ms := newMockServer(t)
	ms.injectErr("GetDiscoveryAgent", errors.New("db unavailable"))

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/agents/any-agent", nil, "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestDiscoveryListAgents_StoreError(t *testing.T) {
	srv, ms := newMockServer(t)
	ms.injectErr("ListDiscoveryAgents", errors.New("db unavailable"))

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/agents", nil, "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestDiscoveryPutAgentMeta(t *testing.T) {
	srv, ms := newMockServer(t)

	// Seed agent first.
	ctx := t.Context()
	_, _ = ms.Heartbeat(ctx, "smokeyclaw", []string{"memory"}, model.AgentStatusOnline)

	resp := request(t, srv, http.MethodPut, "/api/v1/discovery/agents/smokeyclaw", map[string]any{
		"discord_user_id": "111222333",
		"home_channel":    "smokeyclaw",
		"mention_format":  "@SmokeyClaw",
		"channels":        []string{"allclaws", "smokeyclaw"},
	}, "", "")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var da map[string]any
	decodeJSON(t, resp, &da)
	if da["mention_format"] != "@SmokeyClaw" {
		t.Errorf("mention_format = %v, want @SmokeyClaw", da["mention_format"])
	}
}

func TestDiscoveryPutAgentMeta_AgentNotFound(t *testing.T) {
	srv, _ := newMockServer(t)

	resp := request(t, srv, http.MethodPut, "/api/v1/discovery/agents/ghost-agent", map[string]any{
		"mention_format": "@Ghost",
	}, "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestDiscoveryListChannels(t *testing.T) {
	srv, ms := newMockServer(t)

	ctx := t.Context()
	_, _ = ms.UpsertChannel(ctx, &model.DiscoveryChannel{ID: "allclaws", Name: "All Claws"})
	_, _ = ms.UpsertChannel(ctx, &model.DiscoveryChannel{ID: "smokeyclaw", Name: "SmokeyClaw"})

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/channels", nil, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var channels []map[string]any
	decodeJSON(t, resp, &channels)
	if len(channels) != 2 {
		t.Errorf("len = %d, want 2", len(channels))
	}
}

func TestDiscoveryPutChannel(t *testing.T) {
	srv, _ := newMockServer(t)

	resp := request(t, srv, http.MethodPut, "/api/v1/discovery/channels/allclaws", map[string]any{
		"name":       "All Claws",
		"discord_id": "123456789",
		"purpose":    "General agent coordination",
		"category":   "Agents",
		"members":    []string{"smokeyclaw"},
	}, "", "")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var ch map[string]any
	decodeJSON(t, resp, &ch)
	if ch["id"] != "allclaws" {
		t.Errorf("id = %v, want allclaws", ch["id"])
	}
	if ch["name"] != "All Claws" {
		t.Errorf("name = %v, want All Claws", ch["name"])
	}
}

func TestDiscoveryGetChannel(t *testing.T) {
	srv, ms := newMockServer(t)

	ctx := t.Context()
	_, _ = ms.UpsertChannel(ctx, &model.DiscoveryChannel{ID: "allclaws", Name: "All Claws"})

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/channels/allclaws", nil, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var ch map[string]any
	decodeJSON(t, resp, &ch)
	if ch["id"] != "allclaws" {
		t.Errorf("id = %v, want allclaws", ch["id"])
	}
}

func TestDiscoveryGetChannelNotFound(t *testing.T) {
	srv, _ := newMockServer(t)

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/channels/nonexistent", nil, "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestDiscoveryGetChannel_StoreError(t *testing.T) {
	srv, ms := newMockServer(t)
	ms.injectErr("GetChannel", errors.New("db unavailable"))

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/channels/any-channel", nil, "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestDiscoveryListChannels_StoreError(t *testing.T) {
	srv, ms := newMockServer(t)
	ms.injectErr("ListChannels", errors.New("db unavailable"))

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/channels", nil, "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestDiscoveryListRoles(t *testing.T) {
	srv, ms := newMockServer(t)

	ctx := t.Context()
	_, _ = ms.UpsertRole(ctx, &model.DiscoveryRole{ID: "agents", Name: "Agents"})

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/roles", nil, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var roles []map[string]any
	decodeJSON(t, resp, &roles)
	if len(roles) != 1 {
		t.Errorf("len = %d, want 1", len(roles))
	}
}

func TestDiscoveryPutRole(t *testing.T) {
	srv, _ := newMockServer(t)

	resp := request(t, srv, http.MethodPut, "/api/v1/discovery/roles/agents", map[string]any{
		"name":       "Agents",
		"discord_id": "999888777",
		"members":    []string{"smokeyclaw", "jakeclaw"},
	}, "", "")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var role map[string]any
	decodeJSON(t, resp, &role)
	if role["id"] != "agents" {
		t.Errorf("id = %v, want agents", role["id"])
	}
}

func TestDiscoveryGetRole(t *testing.T) {
	srv, ms := newMockServer(t)

	ctx := t.Context()
	_, _ = ms.UpsertRole(ctx, &model.DiscoveryRole{ID: "agents", Name: "Agents"})

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/roles/agents", nil, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var role map[string]any
	decodeJSON(t, resp, &role)
	if role["id"] != "agents" {
		t.Errorf("id = %v, want agents", role["id"])
	}
}

func TestDiscoveryGetRoleNotFound(t *testing.T) {
	srv, _ := newMockServer(t)

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/roles/nonexistent", nil, "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestDiscoveryGetRole_StoreError(t *testing.T) {
	srv, ms := newMockServer(t)
	ms.injectErr("GetRole", errors.New("db unavailable"))

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/roles/any-role", nil, "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestDiscoveryListRoles_StoreError(t *testing.T) {
	srv, ms := newMockServer(t)
	ms.injectErr("ListRoles", errors.New("db unavailable"))

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/roles", nil, "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestDiscoveryGetRouting(t *testing.T) {
	srv, ms := newMockServer(t)

	ctx := t.Context()
	_, _ = ms.Heartbeat(ctx, "routeagent", nil, model.AgentStatusOnline)
	_, _ = ms.UpsertAgentMeta(ctx, "routeagent", &model.DiscoveryAgentMeta{
		MentionFormat: "@RouteAgent",
		HomeChannel:   "routeagent",
		Channels:      []string{"allclaws", "routeagent"},
	})

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/routing/routeagent", nil, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var routing map[string]any
	decodeJSON(t, resp, &routing)
	if routing["agent_id"] != "routeagent" {
		t.Errorf("agent_id = %v, want routeagent", routing["agent_id"])
	}
	if routing["mention_format"] != "@RouteAgent" {
		t.Errorf("mention_format = %v, want @RouteAgent", routing["mention_format"])
	}
	if routing["home_channel"] != "routeagent" {
		t.Errorf("home_channel = %v, want routeagent", routing["home_channel"])
	}
}

func TestDiscoveryGetRoutingNotFound(t *testing.T) {
	srv, _ := newMockServer(t)

	resp := request(t, srv, http.MethodGet, "/api/v1/discovery/routing/no-agent", nil, "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
