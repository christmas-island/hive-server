package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/christmas-island/hive-server/internal/handlers"
)

// newMockServerWithToken creates an httptest server backed by an in-memory mockStore
// with the given bearer token. No database connection required.
func newMockServerWithToken(t *testing.T, token string) *httptest.Server {
	t.Helper()
	h := handlers.New(newMockStore(), token, nil)
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
