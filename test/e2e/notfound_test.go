//go:build e2e

package e2e

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
)

// TestNotFound verifies that requesting a non-existent resource returns 404.
func TestNotFound(t *testing.T) {
	// Use UUIDs to ensure these keys/IDs don't accidentally collide with real data.
	nonexistentMemKey := "__e2e__-notfound-" + uuid.New().String()
	nonexistentTaskID := uuid.New().String()
	nonexistentAgentID := "__e2e__agent-notfound-" + uuid.New().String()

	t.Run("memory", func(t *testing.T) {
		status, body, err := cli.do("GET", "/api/v1/memory/"+url.PathEscape(nonexistentMemKey), nil)
		if err != nil {
			t.Fatalf("GET /api/v1/memory/{key}: %v", err)
		}
		if status != http.StatusNotFound {
			t.Errorf("want 404, got %d (body: %s)", status, body)
		}
	})

	t.Run("task", func(t *testing.T) {
		status, body, err := cli.do("GET", "/api/v1/tasks/"+nonexistentTaskID, nil)
		if err != nil {
			t.Fatalf("GET /api/v1/tasks/{id}: %v", err)
		}
		if status != http.StatusNotFound {
			t.Errorf("want 404, got %d (body: %s)", status, body)
		}
	})

	t.Run("agent", func(t *testing.T) {
		status, body, err := cli.do("GET", "/api/v1/agents/"+nonexistentAgentID, nil)
		if err != nil {
			t.Fatalf("GET /api/v1/agents/{id}: %v", err)
		}
		if status != http.StatusNotFound {
			t.Errorf("want 404, got %d (body: %s)", status, body)
		}
	})
}
