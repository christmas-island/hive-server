//go:build e2e

package e2e

import (
	"net/http"
	"testing"
)

// TestHealth verifies that GET /health returns 200 OK without authentication.
func TestHealth(t *testing.T) {
	status, body, err := cli.do("GET", "/health", nil, withNoAuth())
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("GET /health: want 200, got %d (body: %s)", status, body)
	}
}

// TestReady verifies that GET /ready returns 200 OK without authentication.
func TestReady(t *testing.T) {
	status, body, err := cli.do("GET", "/ready", nil, withNoAuth())
	if err != nil {
		t.Fatalf("GET /ready: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("GET /ready: want 200, got %d (body: %s)", status, body)
	}
}
