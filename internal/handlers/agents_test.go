package handlers_test

import (
	"net/http"
	"testing"

	"github.com/christmas-island/hive-server/internal/model"
)

func TestAgentHeartbeat(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	resp := request(t, srv, http.MethodPost, "/api/v1/agents/jake-claw/heartbeat", map[string]any{
		"capabilities": []string{"memory", "tasks"},
		"status":       "online",
	}, testToken, testAgent)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var agent model.Agent
	decodeJSON(t, resp, &agent)

	if agent.ID != "jake-claw" {
		t.Errorf("ID = %q, want jake-claw", agent.ID)
	}
	if agent.Status != model.AgentStatusOnline {
		t.Errorf("Status = %q, want online", agent.Status)
	}
	if len(agent.Capabilities) != 2 {
		t.Errorf("Capabilities len = %d, want 2", len(agent.Capabilities))
	}
}

func TestAgentHeartbeat_Idle(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	resp := request(t, srv, http.MethodPost, "/api/v1/agents/idle-agent/heartbeat", map[string]any{
		"status": "idle",
	}, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var agent model.Agent
	decodeJSON(t, resp, &agent)
	if agent.Status != model.AgentStatusIdle {
		t.Errorf("Status = %q, want idle", agent.Status)
	}
}

func TestAgentHeartbeat_InvalidStatus_DefaultsOnline(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	resp := request(t, srv, http.MethodPost, "/api/v1/agents/weird/heartbeat", map[string]any{
		"status": "banana",
	}, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var agent model.Agent
	decodeJSON(t, resp, &agent)
	if agent.Status != model.AgentStatusOnline {
		t.Errorf("Status = %q, want online (default)", agent.Status)
	}
}

func TestAgentHeartbeat_Update(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	// First heartbeat.
	r1 := request(t, srv, http.MethodPost, "/api/v1/agents/updagent/heartbeat", map[string]any{
		"status": "online",
	}, testToken, testAgent)
	var a1 model.Agent
	decodeJSON(t, r1, &a1)

	// Second heartbeat with updated caps.
	r2 := request(t, srv, http.MethodPost, "/api/v1/agents/updagent/heartbeat", map[string]any{
		"capabilities": []string{"new-cap"},
		"status":       "idle",
	}, testToken, testAgent)
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d, want 200", r2.StatusCode)
	}
	var a2 model.Agent
	decodeJSON(t, r2, &a2)

	if a2.Status != model.AgentStatusIdle {
		t.Errorf("Status = %q, want idle", a2.Status)
	}
	if len(a2.Capabilities) != 1 || a2.Capabilities[0] != "new-cap" {
		t.Errorf("Capabilities = %v, want [new-cap]", a2.Capabilities)
	}
}

func TestAgentGet(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	request(t, srv, http.MethodPost, "/api/v1/agents/myagent/heartbeat", map[string]any{
		"status": "online",
	}, testToken, testAgent).Body.Close()

	resp := request(t, srv, http.MethodGet, "/api/v1/agents/myagent", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var agent model.Agent
	decodeJSON(t, resp, &agent)
	if agent.ID != "myagent" {
		t.Errorf("ID = %q, want myagent", agent.ID)
	}
}

func TestAgentGet_NotFound(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)
	resp := request(t, srv, http.MethodGet, "/api/v1/agents/no-such", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestAgentList(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	for _, id := range []string{"a1", "a2", "a3"} {
		request(t, srv, http.MethodPost, "/api/v1/agents/"+id+"/heartbeat", map[string]any{
			"status": "online",
		}, testToken, testAgent).Body.Close()
	}

	resp := request(t, srv, http.MethodGet, "/api/v1/agents", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var agents []model.Agent
	decodeJSON(t, resp, &agents)
	if len(agents) != 3 {
		t.Errorf("len = %d, want 3", len(agents))
	}
}

func TestAgentList_Empty(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)
	resp := request(t, srv, http.MethodGet, "/api/v1/agents", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var agents []model.Agent
	decodeJSON(t, resp, &agents)
	if agents == nil {
		t.Error("expected non-nil agents slice")
	}
}
