//go:build e2e

package e2e

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"github.com/christmas-island/hive-server/internal/store"
)

// TestListIsolation verifies that list endpoints can be scoped to __e2e__
// resources and do not depend on pre-existing production data.
func TestListIsolation(t *testing.T) {
	runID := uuid.New().String()

	// --- Memory isolation ---
	// Create a memory entry with a unique key prefix, then list using that
	// prefix to verify only our entry is returned.
	memKey := "__e2e__-isolation-" + runID
	defer func() {
		cli.do("DELETE", "/api/v1/memory/"+url.PathEscape(memKey), nil) //nolint:errcheck
	}()

	_, _, err := cli.do("POST", "/api/v1/memory", map[string]any{
		"key":   memKey,
		"value": "isolation test value",
		"tags":  []string{"e2e-isolation"},
	})
	if err != nil {
		t.Fatalf("create memory entry: %v", err)
	}

	listMemURL := "/api/v1/memory?prefix=" + url.QueryEscape("__e2e__-isolation-"+runID) + "&limit=50"
	status, resp, err := cli.do("GET", listMemURL, nil)
	if err != nil {
		t.Fatalf("GET /api/v1/memory (list with prefix): %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("memory list: want 200, got %d (body: %s)", status, resp)
	}
	memEntries, err := decodeJSON[[]*store.MemoryEntry](resp)
	if err != nil {
		t.Fatalf("memory list: decode response: %v", err)
	}
	if len(memEntries) != 1 {
		t.Errorf("memory isolation: want exactly 1 entry, got %d", len(memEntries))
	}
	if len(memEntries) > 0 && memEntries[0].Key != memKey {
		t.Errorf("memory isolation: key mismatch: want %q, got %q", memKey, memEntries[0].Key)
	}
	t.Logf("memory isolation OK: %d entry with prefix %q", len(memEntries), "__e2e__-isolation-"+runID)

	// --- Task isolation ---
	// Use a unique creator ID derived from the run ID so we can filter by it.
	// The creator field is populated from the X-Agent-ID header.
	taskCreator := "__e2e__agent-isolation-" + runID
	taskTitle := "__e2e__ isolation task " + runID

	status, resp, err = cli.do("POST", "/api/v1/tasks", map[string]any{
		"title":       taskTitle,
		"description": "list isolation test",
		"tags":        []string{"e2e-isolation"},
	}, withAgentID(taskCreator))
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("create task: want 201, got %d (body: %s)", status, resp)
	}
	task, err := decodeJSON[*store.Task](resp)
	if err != nil {
		t.Fatalf("create task: decode response: %v", err)
	}
	taskID := task.ID
	trackTask(taskID)
	defer func() {
		cli.do("DELETE", "/api/v1/tasks/"+taskID, nil) //nolint:errcheck
	}()

	listTaskURL := "/api/v1/tasks?creator=" + url.QueryEscape(taskCreator) + "&limit=50"
	status, resp, err = cli.do("GET", listTaskURL, nil)
	if err != nil {
		t.Fatalf("GET /api/v1/tasks (list with creator): %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("task list: want 200, got %d (body: %s)", status, resp)
	}
	tasks, err := decodeJSON[[]*store.Task](resp)
	if err != nil {
		t.Fatalf("task list: decode response: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("task isolation: want exactly 1 task, got %d", len(tasks))
	}
	if len(tasks) > 0 && tasks[0].ID != taskID {
		t.Errorf("task isolation: id mismatch: want %q, got %q", taskID, tasks[0].ID)
	}
	t.Logf("task isolation OK: %d task with creator %q", len(tasks), taskCreator)

	// --- Agent isolation ---
	// Register an e2e-scoped agent then verify it appears in the full list.
	// (No filter on agents endpoint; we scan in-code for the __e2e__ agent.)
	agentID := "__e2e__agent-isolation-" + runID
	status, resp, err = cli.do("POST", "/api/v1/agents/"+agentID+"/heartbeat", map[string]any{
		"capabilities": []string{"e2e-isolation"},
		"status":       "online",
	})
	if err != nil {
		t.Fatalf("POST /api/v1/agents/{id}/heartbeat: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("agent heartbeat: want 200, got %d (body: %s)", status, resp)
	}

	status, resp, err = cli.do("GET", "/api/v1/agents", nil)
	if err != nil {
		t.Fatalf("GET /api/v1/agents: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("agent list: want 200, got %d (body: %s)", status, resp)
	}
	agents, err := decodeJSON[[]*store.Agent](resp)
	if err != nil {
		t.Fatalf("agent list: decode response: %v", err)
	}
	found := false
	for _, a := range agents {
		if a.ID == agentID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("agent isolation: agent %q not found in list (%d total agents)", agentID, len(agents))
	}
	t.Logf("agent isolation OK: agent %q present in list of %d", agentID, len(agents))
}
