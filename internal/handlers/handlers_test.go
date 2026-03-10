package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/christmas-island/hive-server/internal/handlers"
	"github.com/christmas-island/hive-server/internal/store"
)

// testDatabaseURL returns the DATABASE_URL for handler tests.
// Tests are skipped if the env var is not set (no live DB required in CI without CRDB).
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping handler integration test")
	}
	return url
}

// newTestServer creates an httptest server backed by a real CockroachDB/PostgreSQL store.
func newTestServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	url := testDatabaseURL(t)
	s, err := store.New(url)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		cleanTestDB(t, s)
		_ = s.Close()
	})
	h := handlers.New(s, token)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// cleanTestDB removes all rows inserted during a test to keep the DB clean between runs.
func cleanTestDB(t *testing.T, s *store.Store) {
	t.Helper()
	db := s.DB()
	for _, tbl := range []string{"task_notes", "tasks", "memory", "claims", "discovery_channels", "discovery_roles", "agents"} {
		if _, err := db.Exec("DELETE FROM " + tbl); err != nil {
			t.Logf("cleanup %s: %v", tbl, err)
		}
	}
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
	srv := newTestServer(t, "secret")
	resp := request(t, srv, http.MethodGet, "/api/v1/memory", nil, "", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestAuth_WrongToken verifies that an invalid token is rejected.
func TestAuth_WrongToken(t *testing.T) {
	srv := newTestServer(t, "secret")
	resp := request(t, srv, http.MethodGet, "/api/v1/memory", nil, "wrong", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestAuth_NoTokenConfigured verifies that no auth check occurs when token is empty.
func TestAuth_NoTokenConfigured(t *testing.T) {
	srv := newTestServer(t, "")
	resp := request(t, srv, http.MethodGet, "/api/v1/memory", nil, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
}
