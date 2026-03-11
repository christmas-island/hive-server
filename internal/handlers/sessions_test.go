package handlers_test

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/christmas-island/hive-server/internal/model"
)

func TestSessionCreate(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	now := time.Now().UTC()
	resp := request(t, srv, http.MethodPost, "/api/v1/sessions", map[string]any{
		"agent_id":    testAgent,
		"session_key": "agent:main:discord:channel:123",
		"model":       "claude-sonnet-4",
		"started_at":  now.Add(-5 * time.Minute),
		"finished_at": now,
		"summary":     "Fixed a bug in store/claims.go",
		"usage": map[string]any{
			"input_tokens":  1000,
			"output_tokens": 500,
		},
	}, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 201; body: %s", resp.StatusCode, b)
	}
	var out model.CapturedSession
	decodeJSON(t, resp, &out)
	if out.ID == "" {
		t.Error("ID is empty")
	}
	if out.AgentID != testAgent {
		t.Errorf("agent_id = %q, want %q", out.AgentID, testAgent)
	}
}

func TestSessionCreate_AgentIDFromHeader(t *testing.T) {
	// If body has no agent_id, it should be filled from X-Agent-ID header.
	srv := newMockServerWithToken(t, testToken)

	now := time.Now().UTC()
	resp := request(t, srv, http.MethodPost, "/api/v1/sessions", map[string]any{
		"agent_id":    "",
		"started_at":  now.Add(-time.Minute),
		"finished_at": now,
	}, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 201; body: %s", resp.StatusCode, b)
	}
	var out model.CapturedSession
	decodeJSON(t, resp, &out)
	if out.AgentID != testAgent {
		t.Errorf("agent_id = %q, want %q (from header)", out.AgentID, testAgent)
	}
}

func TestSessionCreate_StoreError(t *testing.T) {
	srv, ms := newMockServerWithStore(t, testToken)
	ms.injectErr("CreateCapturedSession", errTest)

	resp := request(t, srv, http.MethodPost, "/api/v1/sessions", map[string]any{
		"agent_id": testAgent,
	}, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestSessionGet(t *testing.T) {
	srv, ms := newMockServerWithStore(t, testToken)

	now := time.Now().UTC()
	t1 := now.Add(-time.Hour)
	ms.sessions = map[string]*model.CapturedSession{
		"sess-1": {
			ID:         "sess-1",
			AgentID:    testAgent,
			Summary:    "did some stuff",
			StartedAt:  &t1,
			FinishedAt: &now,
		},
	}

	resp := request(t, srv, http.MethodGet, "/api/v1/sessions/sess-1", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out model.CapturedSession
	decodeJSON(t, resp, &out)
	if out.ID != "sess-1" {
		t.Errorf("ID = %q, want sess-1", out.ID)
	}
}

func TestSessionGet_NotFound(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)
	resp := request(t, srv, http.MethodGet, "/api/v1/sessions/no-such", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestSessionGet_StoreError(t *testing.T) {
	srv, ms := newMockServerWithStore(t, testToken)
	ms.injectErr("GetCapturedSession", errTest)
	resp := request(t, srv, http.MethodGet, "/api/v1/sessions/some-id", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestSessionList(t *testing.T) {
	srv, ms := newMockServerWithStore(t, testToken)
	now := time.Now().UTC()
	t1, t2 := now.Add(-2*time.Hour), now.Add(-time.Hour)
	ms.sessions = map[string]*model.CapturedSession{
		"s1": {ID: "s1", AgentID: testAgent, StartedAt: &t1},
		"s2": {ID: "s2", AgentID: "other-agent", StartedAt: &t2},
	}

	resp := request(t, srv, http.MethodGet, "/api/v1/sessions?agent_id="+testAgent, nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out []*model.CapturedSession
	decodeJSON(t, resp, &out)
	if len(out) != 1 {
		t.Errorf("got %d sessions, want 1", len(out))
	}
}

func TestSessionList_Since(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)
	since := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)

	resp := request(t, srv, http.MethodGet, "/api/v1/sessions?since="+since, nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestSessionList_InvalidSince(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)
	resp := request(t, srv, http.MethodGet, "/api/v1/sessions?since=not-a-date", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestSessionList_StoreError(t *testing.T) {
	srv, ms := newMockServerWithStore(t, testToken)
	ms.injectErr("ListCapturedSessions", errTest)
	resp := request(t, srv, http.MethodGet, "/api/v1/sessions", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}
