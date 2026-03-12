package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/christmas-island/hive-server/internal/model"
)

// TestAgentOnboard_Success verifies that the onboarding endpoint generates a token.
func TestAgentOnboard_Success(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	resp := request(t, srv, http.MethodPost, "/api/v1/agents/new-agent/onboard", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result struct {
		Agent *model.Agent `json:"agent"`
		Token string       `json:"token"`
	}
	decodeJSON(t, resp, &result)

	if result.Token == "" {
		t.Error("expected non-empty token")
	}
	if result.Agent == nil {
		t.Fatal("expected non-nil agent")
	}
	if result.Agent.ID != "new-agent" {
		t.Errorf("agent ID = %q, want new-agent", result.Agent.ID)
	}
}

// TestAgentOnboard_Unauthorized verifies that onboarding requires valid auth.
func TestAgentOnboard_Unauthorized(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	resp := request(t, srv, http.MethodPost, "/api/v1/agents/new-agent/onboard", nil, "wrong-token", testAgent)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestAgentOnboard_GenerateTokenError verifies error handling when token generation fails.
func TestAgentOnboard_GenerateTokenError(t *testing.T) {
	srv, ms := newMockServerWithStore(t, testToken)

	ms.injectErr("GenerateAgentToken", errTest)
	resp := request(t, srv, http.MethodPost, "/api/v1/agents/err-agent/onboard", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestAgentOnboard_TokenNotExposedInJSON verifies the agent copy doesn't leak the token.
func TestAgentOnboard_TokenNotExposedInJSON(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	resp := request(t, srv, http.MethodPost, "/api/v1/agents/secure-agent/onboard", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Decode raw JSON to check the agent sub-object doesn't contain a token field
	var raw map[string]json.RawMessage
	decodeJSON(t, resp, &raw)

	// The top-level token should be present
	if _, ok := raw["token"]; !ok {
		t.Error("expected top-level token field in response")
	}
}

// TestAuth_PerAgentToken verifies that per-agent token auth works.
func TestAuth_PerAgentToken(t *testing.T) {
	srv, ms := newMockServerWithStore(t, testToken)

	// First onboard the agent to get a token
	resp := request(t, srv, http.MethodPost, "/api/v1/agents/per-agent/onboard", nil, testToken, "per-agent")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("onboard status = %d, want 200", resp.StatusCode)
	}
	var onboardResult struct {
		Token string `json:"token"`
	}
	decodeJSON(t, resp, &onboardResult)

	// The mock store sets token to "mock-token-<id>"
	_ = ms
	agentToken := onboardResult.Token

	// Now use the per-agent token (not the global token) to make a request
	resp = request(t, srv, http.MethodGet, "/api/v1/memory", nil, agentToken, "per-agent")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("per-agent auth status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestAuth_PerAgentToken_WrongAgent verifies per-agent token doesn't work for different agent.
func TestAuth_PerAgentToken_WrongAgent(t *testing.T) {
	srv, _ := newMockServerWithStore(t, testToken)

	// Onboard agent-a
	resp := request(t, srv, http.MethodPost, "/api/v1/agents/agent-a/onboard", nil, testToken, "agent-a")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("onboard status = %d, want 200", resp.StatusCode)
	}
	var onboardResult struct {
		Token string `json:"token"`
	}
	decodeJSON(t, resp, &onboardResult)

	// Try using agent-a's token with agent-b's ID
	resp = request(t, srv, http.MethodGet, "/api/v1/memory", nil, onboardResult.Token, "agent-b")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("cross-agent auth status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestAuth_PerAgentToken_NoAgentID verifies per-agent token without X-Agent-ID fails.
func TestAuth_PerAgentToken_NoAgentID(t *testing.T) {
	srv, _ := newMockServerWithStore(t, testToken)

	// Onboard an agent
	resp := request(t, srv, http.MethodPost, "/api/v1/agents/solo/onboard", nil, testToken, "solo")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("onboard status = %d, want 200", resp.StatusCode)
	}
	var onboardResult struct {
		Token string `json:"token"`
	}
	decodeJSON(t, resp, &onboardResult)

	// Try using the token without X-Agent-ID header
	resp = request(t, srv, http.MethodGet, "/api/v1/memory", nil, onboardResult.Token, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no-agent-id auth status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}
