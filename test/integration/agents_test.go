//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/christmas-island/hive-server/pkg/testharness"
)

// TestAgentHeartbeatRoundTrip verifies the agent registration + heartbeat lifecycle
// through the full stack (HTTP handler → CockroachDB → response).
func TestAgentHeartbeatRoundTrip(t *testing.T) {
	base := testharness.NewTestServer(t)
	cli := newClient(base, "test-token")
	aid := "integ-agent-heartbeat"

	// First heartbeat registers the agent.
	hb := map[string]any{
		"capabilities": []string{"memory", "tasks"},
		"status":       "online",
		"activity":     "running integration tests",
	}
	status, body, err := cli.do("POST", "/api/v1/agents/"+aid+"/heartbeat", hb, agentID(aid))
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("heartbeat: want 200, got %d (%s)", status, body)
	}

	type agentResp struct {
		ID           string   `json:"id"`
		Status       string   `json:"status"`
		Activity     string   `json:"activity"`
		Capabilities []string `json:"capabilities"`
	}
	agent, err := decode[agentResp](body)
	if err != nil {
		t.Fatalf("decode heartbeat response: %v", err)
	}
	if agent.ID != aid {
		t.Errorf("id: want %q, got %q", aid, agent.ID)
	}
	if agent.Status != "online" {
		t.Errorf("status: want online, got %s", agent.Status)
	}
	if len(agent.Capabilities) != 2 {
		t.Errorf("capabilities: want 2, got %d", len(agent.Capabilities))
	}

	// Second heartbeat updates last_heartbeat.
	hb["activity"] = "still testing"
	status, body, err = cli.do("POST", "/api/v1/agents/"+aid+"/heartbeat", hb, agentID(aid))
	if err != nil {
		t.Fatalf("second heartbeat: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("second heartbeat: want 200, got %d (%s)", status, body)
	}

	// GET agent.
	status, body, err = cli.do("GET", "/api/v1/agents/"+aid, nil)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("get agent: want 200, got %d (%s)", status, body)
	}
	got, err := decode[agentResp](body)
	if err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.Activity != "still testing" {
		t.Errorf("activity not updated: want 'still testing', got %q", got.Activity)
	}

	// List agents — our agent should be present.
	status, body, err = cli.do("GET", "/api/v1/agents", nil)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("list agents: want 200, got %d (%s)", status, body)
	}
	agents, err := decode[[]agentResp](body)
	if err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	found := false
	for _, a := range agents {
		if a.ID == aid {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("agent %q not in list (%d agents)", aid, len(agents))
	}
}
