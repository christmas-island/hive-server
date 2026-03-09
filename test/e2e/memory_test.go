//go:build e2e

package e2e

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"github.com/christmas-island/hive-server/internal/store"
)

// TestMemoryCRUD exercises the full memory lifecycle:
// Create → Read → Update → Read → Delete.
func TestMemoryCRUD(t *testing.T) {
	runID := uuid.New().String()
	key := "__e2e__-mem-" + runID
	agentID := "__e2e__agent-mem-" + runID

	// Best-effort cleanup — individual steps also verify deletion at the end.
	defer func() {
		cli.do("DELETE", "/api/v1/memory/"+url.PathEscape(key), nil) //nolint:errcheck
	}()

	// --- Create (upsert) ---
	createBody := map[string]any{
		"key":   key,
		"value": "initial value",
		"tags":  []string{"e2e", "test"},
	}
	status, resp, err := cli.do("POST", "/api/v1/memory", createBody, withAgentID(agentID))
	if err != nil {
		t.Fatalf("POST /api/v1/memory: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("create: want 200, got %d (body: %s)", status, resp)
	}
	entry, err := decodeJSON[*store.MemoryEntry](resp)
	if err != nil {
		t.Fatalf("create: decode response: %v", err)
	}
	if entry.Key != key {
		t.Errorf("create: key: want %q, got %q", key, entry.Key)
	}
	if entry.Value != "initial value" {
		t.Errorf("create: value: want %q, got %q", "initial value", entry.Value)
	}
	if entry.Version != 1 {
		t.Errorf("create: version: want 1, got %d", entry.Version)
	}
	if entry.AgentID != agentID {
		t.Errorf("create: agent_id: want %q, got %q", agentID, entry.AgentID)
	}
	t.Logf("created memory entry %q (version=%d)", key, entry.Version)

	// --- Read ---
	status, resp, err = cli.do("GET", "/api/v1/memory/"+url.PathEscape(key), nil)
	if err != nil {
		t.Fatalf("GET /api/v1/memory/{key}: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("read: want 200, got %d (body: %s)", status, resp)
	}
	got, err := decodeJSON[*store.MemoryEntry](resp)
	if err != nil {
		t.Fatalf("read: decode response: %v", err)
	}
	if got.Value != "initial value" {
		t.Errorf("read: value: want %q, got %q", "initial value", got.Value)
	}

	// --- Update (upsert with new value) ---
	updateBody := map[string]any{
		"key":   key,
		"value": "updated value",
		"tags":  []string{"e2e", "test", "updated"},
	}
	status, resp, err = cli.do("POST", "/api/v1/memory", updateBody, withAgentID(agentID))
	if err != nil {
		t.Fatalf("POST /api/v1/memory (update): %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("update: want 200, got %d (body: %s)", status, resp)
	}
	updated, err := decodeJSON[*store.MemoryEntry](resp)
	if err != nil {
		t.Fatalf("update: decode response: %v", err)
	}
	if updated.Value != "updated value" {
		t.Errorf("update: value: want %q, got %q", "updated value", updated.Value)
	}
	if updated.Version != 2 {
		t.Errorf("update: version: want 2, got %d", updated.Version)
	}
	t.Logf("updated memory entry %q (version=%d)", key, updated.Version)

	// --- Read after update ---
	status, resp, err = cli.do("GET", "/api/v1/memory/"+url.PathEscape(key), nil)
	if err != nil {
		t.Fatalf("GET /api/v1/memory/{key} (after update): %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("read-after-update: want 200, got %d (body: %s)", status, resp)
	}
	got2, err := decodeJSON[*store.MemoryEntry](resp)
	if err != nil {
		t.Fatalf("read-after-update: decode response: %v", err)
	}
	if got2.Value != "updated value" {
		t.Errorf("read-after-update: value: want %q, got %q", "updated value", got2.Value)
	}

	// --- Delete ---
	status, resp, err = cli.do("DELETE", "/api/v1/memory/"+url.PathEscape(key), nil)
	if err != nil {
		t.Fatalf("DELETE /api/v1/memory/{key}: %v", err)
	}
	if status != http.StatusNoContent {
		t.Fatalf("delete: want 204, got %d (body: %s)", status, resp)
	}

	// --- Verify gone ---
	status, _, err = cli.do("GET", "/api/v1/memory/"+url.PathEscape(key), nil)
	if err != nil {
		t.Fatalf("GET /api/v1/memory/{key} (verify gone): %v", err)
	}
	if status != http.StatusNotFound {
		t.Errorf("verify-gone: want 404, got %d", status)
	}
	t.Logf("memory CRUD complete for key %q", key)
}
