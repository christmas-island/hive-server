package handlers_test

import (
	"net/http"
	"testing"

	"github.com/christmas-island/hive-server/internal/store"
)

const testToken = "test-token"
const testAgent = "test-agent"

func TestMemoryUpsert_Create(t *testing.T) {
	srv := newTestServer(t, testToken)

	resp := request(t, srv, http.MethodPost, "/api/v1/memory", map[string]any{
		"key":   "foo.bar",
		"value": "hello",
		"tags":  []string{"a", "b"},
	}, testToken, testAgent)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var entry store.MemoryEntry
	decodeJSON(t, resp, &entry)

	if entry.Key != "foo.bar" {
		t.Errorf("Key = %q, want foo.bar", entry.Key)
	}
	if entry.Version != 1 {
		t.Errorf("Version = %d, want 1", entry.Version)
	}
	if entry.AgentID != testAgent {
		t.Errorf("AgentID = %q, want %q", entry.AgentID, testAgent)
	}
	if len(entry.Tags) != 2 {
		t.Errorf("Tags len = %d, want 2", len(entry.Tags))
	}
}

func TestMemoryUpsert_Update(t *testing.T) {
	srv := newTestServer(t, testToken)

	// Create.
	r1 := request(t, srv, http.MethodPost, "/api/v1/memory", map[string]any{
		"key": "upd.key", "value": "v1",
	}, testToken, testAgent)
	var e1 store.MemoryEntry
	decodeJSON(t, r1, &e1)

	// Update with correct version.
	resp := request(t, srv, http.MethodPost, "/api/v1/memory", map[string]any{
		"key": "upd.key", "value": "v2", "version": e1.Version,
	}, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d, want 200", resp.StatusCode)
	}
	var e2 store.MemoryEntry
	decodeJSON(t, resp, &e2)
	if e2.Version != 2 {
		t.Errorf("Version = %d, want 2", e2.Version)
	}
}

func TestMemoryUpsert_Conflict(t *testing.T) {
	srv := newTestServer(t, testToken)

	request(t, srv, http.MethodPost, "/api/v1/memory", map[string]any{
		"key": "oc.key", "value": "v1",
	}, testToken, testAgent).Body.Close()

	resp := request(t, srv, http.MethodPost, "/api/v1/memory", map[string]any{
		"key": "oc.key", "value": "stale", "version": 99,
	}, testToken, testAgent)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

func TestMemoryUpsert_MissingKey(t *testing.T) {
	srv := newTestServer(t, testToken)
	resp := request(t, srv, http.MethodPost, "/api/v1/memory", map[string]any{"value": "v"}, testToken, testAgent)
	defer resp.Body.Close()
	// Huma validates input before the handler runs; missing/empty required
	// string fields return 422 Unprocessable Entity.
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestMemoryUpsert_MissingValue(t *testing.T) {
	srv := newTestServer(t, testToken)
	resp := request(t, srv, http.MethodPost, "/api/v1/memory", map[string]any{"key": "k"}, testToken, testAgent)
	defer resp.Body.Close()
	// Huma validates input before the handler runs; missing/empty required
	// string fields return 422 Unprocessable Entity.
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestMemoryGet(t *testing.T) {
	srv := newTestServer(t, testToken)

	request(t, srv, http.MethodPost, "/api/v1/memory", map[string]any{
		"key": "get.key", "value": "data",
	}, testToken, testAgent).Body.Close()

	resp := request(t, srv, http.MethodGet, "/api/v1/memory/get.key", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var e store.MemoryEntry
	decodeJSON(t, resp, &e)
	if e.Value != "data" {
		t.Errorf("Value = %q, want data", e.Value)
	}
}

func TestMemoryGet_NotFound(t *testing.T) {
	srv := newTestServer(t, testToken)
	resp := request(t, srv, http.MethodGet, "/api/v1/memory/no.such.key", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestMemoryList(t *testing.T) {
	srv := newTestServer(t, testToken)

	for _, k := range []string{"list.1", "list.2"} {
		request(t, srv, http.MethodPost, "/api/v1/memory", map[string]any{
			"key": k, "value": "v", "tags": []string{"mytag"},
		}, testToken, testAgent).Body.Close()
	}

	// List all.
	resp := request(t, srv, http.MethodGet, "/api/v1/memory", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var entries []store.MemoryEntry
	decodeJSON(t, resp, &entries)
	if len(entries) != 2 {
		t.Errorf("len = %d, want 2", len(entries))
	}
}

func TestMemoryList_ByTag(t *testing.T) {
	srv := newTestServer(t, testToken)

	request(t, srv, http.MethodPost, "/api/v1/memory", map[string]any{
		"key": "tagged", "value": "v", "tags": []string{"findme"},
	}, testToken, testAgent).Body.Close()
	request(t, srv, http.MethodPost, "/api/v1/memory", map[string]any{
		"key": "untagged", "value": "v",
	}, testToken, testAgent).Body.Close()

	resp := request(t, srv, http.MethodGet, "/api/v1/memory?tag=findme", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var entries []store.MemoryEntry
	decodeJSON(t, resp, &entries)
	if len(entries) != 1 {
		t.Errorf("len = %d, want 1", len(entries))
	}
}

func TestMemoryList_InvalidLimit(t *testing.T) {
	srv := newTestServer(t, testToken)
	resp := request(t, srv, http.MethodGet, "/api/v1/memory?limit=bad", nil, testToken, testAgent)
	defer resp.Body.Close()
	// Huma validates query parameter types before the handler runs;
	// a non-integer limit returns 422 Unprocessable Entity.
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestMemoryDelete(t *testing.T) {
	srv := newTestServer(t, testToken)

	request(t, srv, http.MethodPost, "/api/v1/memory", map[string]any{
		"key": "del.key", "value": "v",
	}, testToken, testAgent).Body.Close()

	resp := request(t, srv, http.MethodDelete, "/api/v1/memory/del.key", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}

	// Confirm gone.
	resp2 := request(t, srv, http.MethodGet, "/api/v1/memory/del.key", nil, testToken, testAgent)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("after delete: status = %d, want 404", resp2.StatusCode)
	}
}

func TestMemoryDelete_NotFound(t *testing.T) {
	srv := newTestServer(t, testToken)
	resp := request(t, srv, http.MethodDelete, "/api/v1/memory/ghost.key", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
