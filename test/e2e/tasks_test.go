//go:build e2e

package e2e

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"github.com/christmas-island/hive-server/internal/store"
)

// TestTaskCRUD exercises the full task lifecycle:
// Create → Read → Update → List → Delete.
func TestTaskCRUD(t *testing.T) {
	runID := uuid.New().String()
	creator := "__e2e__agent-tasks-" + runID
	title := "__e2e__ task " + runID

	// --- Create ---
	createBody := map[string]any{
		"title":       title,
		"description": "E2E smoke test task",
		"priority":    1,
		"tags":        []string{"e2e"},
	}
	status, resp, err := cli.do("POST", "/api/v1/tasks", createBody, withAgentID(creator))
	if err != nil {
		t.Fatalf("POST /api/v1/tasks: %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (body: %s)", status, resp)
	}
	task, err := decodeJSON[*store.Task](resp)
	if err != nil {
		t.Fatalf("create: decode response: %v", err)
	}
	if task.ID == "" {
		t.Fatal("create: task ID is empty")
	}
	if task.Title != title {
		t.Errorf("create: title: want %q, got %q", title, task.Title)
	}
	if string(task.Status) != "open" {
		t.Errorf("create: status: want %q, got %q", "open", task.Status)
	}
	if task.Creator != creator {
		t.Errorf("create: creator: want %q, got %q", creator, task.Creator)
	}

	taskID := task.ID
	// Register for TestMain sweep and defer immediate cleanup.
	trackTask(taskID)
	defer func() {
		cli.do("DELETE", "/api/v1/tasks/"+taskID, nil) //nolint:errcheck
	}()
	t.Logf("created task %s (%q)", taskID, title)

	// --- Read ---
	status, resp, err = cli.do("GET", "/api/v1/tasks/"+taskID, nil)
	if err != nil {
		t.Fatalf("GET /api/v1/tasks/{id}: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("read: want 200, got %d (body: %s)", status, resp)
	}
	got, err := decodeJSON[*store.Task](resp)
	if err != nil {
		t.Fatalf("read: decode response: %v", err)
	}
	if got.Title != title {
		t.Errorf("read: title: want %q, got %q", title, got.Title)
	}

	// --- Update: advance status and append a note ---
	// Transition open → claimed is a valid state machine move.
	statusClaimed := "claimed"
	note := "e2e test note added by " + creator
	updateBody := map[string]any{
		"status": statusClaimed,
		"note":   note,
	}
	status, resp, err = cli.do("PATCH", "/api/v1/tasks/"+taskID, updateBody, withAgentID(creator))
	if err != nil {
		t.Fatalf("PATCH /api/v1/tasks/{id}: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("update: want 200, got %d (body: %s)", status, resp)
	}
	updated, err := decodeJSON[*store.Task](resp)
	if err != nil {
		t.Fatalf("update: decode response: %v", err)
	}
	if string(updated.Status) != "claimed" {
		t.Errorf("update: status: want %q, got %q", "claimed", updated.Status)
	}
	if len(updated.Notes) != 1 || updated.Notes[0] != note {
		t.Errorf("update: notes: want [%q], got %v", note, updated.Notes)
	}
	t.Logf("updated task %s: status=%s, notes=%d", taskID, updated.Status, len(updated.Notes))

	// --- List: verify task appears when filtering by creator ---
	listURL := "/api/v1/tasks?creator=" + url.QueryEscape(creator) + "&limit=50"
	status, resp, err = cli.do("GET", listURL, nil)
	if err != nil {
		t.Fatalf("GET /api/v1/tasks (list): %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("list: want 200, got %d (body: %s)", status, resp)
	}
	tasks, err := decodeJSON[[]*store.Task](resp)
	if err != nil {
		t.Fatalf("list: decode response: %v", err)
	}
	found := false
	for _, tk := range tasks {
		if tk.ID == taskID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("list: task %s not found in results (creator=%s, got %d tasks)", taskID, creator, len(tasks))
	}

	// --- Delete ---
	status, resp, err = cli.do("DELETE", "/api/v1/tasks/"+taskID, nil)
	if err != nil {
		t.Fatalf("DELETE /api/v1/tasks/{id}: %v", err)
	}
	if status != http.StatusNoContent {
		t.Fatalf("delete: want 204, got %d (body: %s)", status, resp)
	}

	// --- Verify gone ---
	status, _, err = cli.do("GET", "/api/v1/tasks/"+taskID, nil)
	if err != nil {
		t.Fatalf("GET /api/v1/tasks/{id} (verify gone): %v", err)
	}
	if status != http.StatusNotFound {
		t.Errorf("verify-gone: want 404, got %d", status)
	}
	t.Logf("task CRUD complete for %s", taskID)
}
