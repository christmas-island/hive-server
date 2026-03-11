package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/christmas-island/hive-server/internal/handlers"
	"github.com/christmas-island/hive-server/internal/relay"
)

// errTest is a sentinel error used in store error injection tests.
var errTest = errors.New("injected test error")

// newMockServerWithToken creates an httptest server backed by an in-memory mockStore
// with the given bearer token. No database connection required.
func newMockServerWithToken(t *testing.T, token string) *httptest.Server {
	t.Helper()
	h := handlers.New(newMockStore(), token, nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// newMockServerWithStore creates an httptest server and returns both the server
// and the underlying mockStore for error injection.
func newMockServerWithStore(t *testing.T, token string) (*httptest.Server, *mockStore) {
	t.Helper()
	ms := newMockStore()
	h := handlers.New(ms, token, nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, ms
}

// newMockServerWithRelay creates an httptest server with both a mock store and a relay client.
func newMockServerWithRelay(t *testing.T, token string, relayURL string) *httptest.Server {
	t.Helper()
	rc := relay.New(relayURL, "relay-token")
	h := handlers.New(newMockStore(), token, rc)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// request is a helper to make HTTP requests to the test server.
func request(t *testing.T, srv *httptest.Server, method, path string, body any, token, agentID string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if agentID != "" {
		req.Header.Set("X-Agent-ID", agentID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// requestWithHeaders is like request but also sets extra headers (e.g. session context).
func requestWithHeaders(t *testing.T, srv *httptest.Server, method, path string, body any, token, agentID string, headers map[string]string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if agentID != "" {
		req.Header.Set("X-Agent-ID", agentID)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}

// TestAuth_NoToken verifies that requests without a bearer token are rejected.
func TestAuth_NoToken(t *testing.T) {
	srv := newMockServerWithToken(t, "secret")
	resp := request(t, srv, http.MethodGet, "/api/v1/memory", nil, "", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestAuth_WrongToken verifies that an invalid token is rejected.
func TestAuth_WrongToken(t *testing.T) {
	srv := newMockServerWithToken(t, "secret")
	resp := request(t, srv, http.MethodGet, "/api/v1/memory", nil, "wrong", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestAuth_NoTokenConfigured verifies that no auth check occurs when token is empty.
func TestAuth_NoTokenConfigured(t *testing.T) {
	srv := newMockServerWithToken(t, "")
	resp := request(t, srv, http.MethodGet, "/api/v1/memory", nil, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestAuth_ValidToken verifies that a correct token is accepted.
func TestAuth_ValidToken(t *testing.T) {
	srv := newMockServerWithToken(t, "secret")
	resp := request(t, srv, http.MethodGet, "/api/v1/memory", nil, "secret", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestAuth_ValidToken_WithAgentID verifies that X-Agent-ID flows through to handlers
// when a valid token is provided.
func TestAuth_ValidToken_WithAgentID(t *testing.T) {
	srv := newMockServerWithToken(t, "secret")
	resp := request(t, srv, http.MethodGet, "/api/v1/memory", nil, "secret", "smokeyclaw")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestAuth_NoTokenConfigured_WithAgentID verifies that X-Agent-ID is injected into
// context even when no bearer token is configured (local dev path).
func TestAuth_NoTokenConfigured_WithAgentID(t *testing.T) {
	srv := newMockServerWithToken(t, "")
	resp := request(t, srv, http.MethodGet, "/api/v1/memory", nil, "", "jakeclaw")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
}
