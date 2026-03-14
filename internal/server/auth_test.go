package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/christmas-island/hive-server/internal/model"
)

// mockAgentLookup implements AgentLookup for testing.
type mockAgentLookup struct {
	agents map[string]*model.Agent
}

func (m *mockAgentLookup) GetAgent(_ context.Context, id string) (*model.Agent, error) {
	a, ok := m.agents[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return a, nil
}

// echoHandler is a test handler that writes 200 OK.
var echoHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestAuthMiddleware_NoTokenConfigured_NoHeader(t *testing.T) {
	mw := AuthMiddleware("", &mockAgentLookup{})
	srv := httptest.NewServer(mw(echoHandler))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestAuthMiddleware_GlobalToken_Valid(t *testing.T) {
	mw := AuthMiddleware("secret", &mockAgentLookup{})
	srv := httptest.NewServer(mw(echoHandler))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestAuthMiddleware_GlobalToken_Invalid(t *testing.T) {
	mw := AuthMiddleware("secret", &mockAgentLookup{})
	srv := httptest.NewServer(mw(echoHandler))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}

	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "unauthorized" {
		t.Errorf("error = %q, want unauthorized", body["error"])
	}
}

func TestAuthMiddleware_NoHeader_TokenRequired(t *testing.T) {
	mw := AuthMiddleware("secret", &mockAgentLookup{})
	srv := httptest.NewServer(mw(echoHandler))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAuthMiddleware_PerAgentToken_Valid(t *testing.T) {
	agents := &mockAgentLookup{
		agents: map[string]*model.Agent{
			"agent-1": {ID: "agent-1", Token: "agent-secret"},
		},
	}
	mw := AuthMiddleware("global", agents)
	srv := httptest.NewServer(mw(echoHandler))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer agent-secret")
	req.Header.Set("X-Agent-ID", "agent-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestAuthMiddleware_PerAgentToken_WrongAgent(t *testing.T) {
	agents := &mockAgentLookup{
		agents: map[string]*model.Agent{
			"agent-1": {ID: "agent-1", Token: "agent-secret"},
		},
	}
	mw := AuthMiddleware("global", agents)
	srv := httptest.NewServer(mw(echoHandler))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer agent-secret")
	req.Header.Set("X-Agent-ID", "agent-2") // wrong agent
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAuthMiddleware_PerAgentToken_NoAgentID(t *testing.T) {
	agents := &mockAgentLookup{
		agents: map[string]*model.Agent{
			"agent-1": {ID: "agent-1", Token: "agent-secret"},
		},
	}
	mw := AuthMiddleware("global", agents)
	srv := httptest.NewServer(mw(echoHandler))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer agent-secret")
	// No X-Agent-ID header
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAuthMiddleware_AgentIDInContext(t *testing.T) {
	var gotAgentID string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAgentID = model.AgentIDFromCtx(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	mw := AuthMiddleware("secret", &mockAgentLookup{})
	srv := httptest.NewServer(mw(handler))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-Agent-ID", "test-agent")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if gotAgentID != "test-agent" {
		t.Errorf("agent ID = %q, want test-agent", gotAgentID)
	}
}

func TestAuthMiddleware_SessionContextInContext(t *testing.T) {
	var gotSession model.SessionContext
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSession = model.SessionFromCtx(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	mw := AuthMiddleware("secret", &mockAgentLookup{})
	srv := httptest.NewServer(mw(handler))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-Session-Key", "sk-123")
	req.Header.Set("X-Session-ID", "sid-456")
	req.Header.Set("X-Channel", "slack")
	req.Header.Set("X-Sender-ID", "user1")
	req.Header.Set("X-Sender-Is-Owner", "true")
	req.Header.Set("X-Sandboxed", "true")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if gotSession.SessionKey != "sk-123" {
		t.Errorf("SessionKey = %q, want sk-123", gotSession.SessionKey)
	}
	if gotSession.SessionID != "sid-456" {
		t.Errorf("SessionID = %q, want sid-456", gotSession.SessionID)
	}
	if gotSession.Channel != "slack" {
		t.Errorf("Channel = %q, want slack", gotSession.Channel)
	}
	if gotSession.SenderID != "user1" {
		t.Errorf("SenderID = %q, want user1", gotSession.SenderID)
	}
	if !gotSession.SenderIsOwner {
		t.Error("SenderIsOwner = false, want true")
	}
	if !gotSession.Sandboxed {
		t.Error("Sandboxed = false, want true")
	}
}
