//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/christmas-island/hive-server/internal/store"
)

// TestAgentHeartbeat verifies the agent presence lifecycle:
// Register → heartbeat → verify via Get → verify via List.
//
// Note: there is no agent delete endpoint, so __e2e__agent-* records persist
// in the server DB. They are isolated by name and harmless.
func TestAgentHeartbeat(t *testing.T) {
	agentID := "__e2e__agent-hb-" + uuid.New().String()

	hbBody := map[string]any{
		"capabilities": []string{"e2e", "smoke"},
		"status":       "online",
	}

	// --- First heartbeat (registers agent) ---
	status, resp, err := cli.do("POST", "/api/v1/agents/"+agentID+"/heartbeat", hbBody)
	if err != nil {
		t.Fatalf("POST /api/v1/agents/{id}/heartbeat: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("heartbeat: want 200, got %d (body: %s)", status, resp)
	}
	agent, err := decodeJSON[*store.Agent](resp)
	if err != nil {
		t.Fatalf("heartbeat: decode response: %v", err)
	}
	if agent.ID != agentID {
		t.Errorf("heartbeat: id: want %q, got %q", agentID, agent.ID)
	}
	if agent.Status != store.AgentStatusOnline {
		t.Errorf("heartbeat: status: want %q, got %q", store.AgentStatusOnline, agent.Status)
	}
	if len(agent.Capabilities) != 2 {
		t.Errorf("heartbeat: capabilities: want 2, got %d (%v)", len(agent.Capabilities), agent.Capabilities)
	}
	t.Logf("registered agent %s (status=%s, caps=%v)", agentID, agent.Status, agent.Capabilities)

	// --- Second heartbeat (updates last_heartbeat) ---
	status, resp, err = cli.do("POST", "/api/v1/agents/"+agentID+"/heartbeat", hbBody)
	if err != nil {
		t.Fatalf("POST /api/v1/agents/{id}/heartbeat (second): %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("second heartbeat: want 200, got %d (body: %s)", status, resp)
	}

	// --- Get agent ---
	status, resp, err = cli.do("GET", "/api/v1/agents/"+agentID, nil)
	if err != nil {
		t.Fatalf("GET /api/v1/agents/{id}: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("get agent: want 200, got %d (body: %s)", status, resp)
	}
	got, err := decodeJSON[*store.Agent](resp)
	if err != nil {
		t.Fatalf("get agent: decode response: %v", err)
	}
	if got.ID != agentID {
		t.Errorf("get agent: id: want %q, got %q", agentID, got.ID)
	}
	// The heartbeat was just sent, so the agent should still be online.
	if got.Status != store.AgentStatusOnline {
		t.Errorf("get agent: status: want %q, got %q", store.AgentStatusOnline, got.Status)
	}
	if got.LastHeartbeat.IsZero() {
		t.Error("get agent: last_heartbeat is zero")
	}
	t.Logf("verified agent %s: status=%s, last_heartbeat=%s", agentID, got.Status, got.LastHeartbeat)

	// --- Verify agent appears in list ---
	status, resp, err = cli.do("GET", "/api/v1/agents", nil)
	if err != nil {
		t.Fatalf("GET /api/v1/agents: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("list agents: want 200, got %d (body: %s)", status, resp)
	}
	agents, err := decodeJSON[[]*store.Agent](resp)
	if err != nil {
		t.Fatalf("list agents: decode response: %v", err)
	}
	found := false
	for _, a := range agents {
		if a.ID == agentID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("list agents: agent %q not found in list (%d total)", agentID, len(agents))
	}
	t.Logf("agent heartbeat test complete for %s", agentID)
}
