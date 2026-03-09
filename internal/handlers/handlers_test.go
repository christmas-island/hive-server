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

// newTestServer creates an httptest server backed by an in-memory mock store.
// No external database required.
func newTestServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	s := newMockStore()
	h := handlers.New(s, token)
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
