//go:build e2e

package e2e

import (
	"net/http"
	"testing"
)

// TestAuthRequired verifies that API endpoints return 401 when called without
// a valid Bearer token.
//
// This test is skipped when E2E_TOKEN is empty, which means the server was
// started with HIVE_TOKEN unset (auth disabled for local dev).
func TestAuthRequired(t *testing.T) {
	if authToken == "" {
		t.Skip("E2E_TOKEN not set; server running with auth disabled — skipping auth tests")
	}

	endpoints := []struct {
		method string
		path   string
		body   any
	}{
		{"GET", "/api/v1/memory", nil},
		{"GET", "/api/v1/tasks", nil},
		{"GET", "/api/v1/agents", nil},
	}

	for _, ep := range endpoints {
		ep := ep

		// No Authorization header.
		t.Run("no-auth/"+ep.method+ep.path, func(t *testing.T) {
			status, body, err := cli.do(ep.method, ep.path, ep.body, withNoAuth())
			if err != nil {
				t.Fatalf("%s %s: %v", ep.method, ep.path, err)
			}
			if status != http.StatusUnauthorized {
				t.Errorf("no-auth %s %s: want 401, got %d (body: %s)", ep.method, ep.path, status, body)
			}
		})

		// Bad Bearer token.
		t.Run("bad-token/"+ep.method+ep.path, func(t *testing.T) {
			status, body, err := cli.do(ep.method, ep.path, ep.body, withBadToken())
			if err != nil {
				t.Fatalf("%s %s: %v", ep.method, ep.path, err)
			}
			if status != http.StatusUnauthorized {
				t.Errorf("bad-token %s %s: want 401, got %d (body: %s)", ep.method, ep.path, status, body)
			}
		})
	}
}
