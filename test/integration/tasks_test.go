//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/christmas-island/hive-server/pkg/testharness"
)

// TestTaskLifecycle exercises create → claim → update (add note) → complete → delete
// through the full stack.
func TestTaskLifecycle(t *testing.T) {
	base := testharness.NewTestServer(t)
	cli := newClient(base, "test-token")
	aid := "integ-agent-tasks"

	// Create task.
	createBody := map[string]any{
		"title":       "integration test task",
		"description": "verifies task lifecycle through real CRDB",
		"priority":    2,
		"tags":        []string{"integration", "test"},
	}
	status, body, err := cli.do("POST", "/api/v1/tasks", createBody, agentID(aid))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", status, body)
	}

	type taskResp struct {
		ID          string   `json:"id"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Status      string   `json:"status"`
		Priority    int      `json:"priority"`
		Creator     string   `json:"creator"`
		Assignee    string   `json:"assignee"`
		Notes       []string `json:"notes"`
		Tags        []string `json:"tags"`
	}

	task, err := decode[taskResp](body)
	if err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if task.ID == "" {
		t.Fatal("task ID is empty")
	}
	if task.Status != "open" {
		t.Errorf("initial status: want open, got %s", task.Status)
	}
	if task.Creator != aid {
		t.Errorf("creator: want %q, got %q", aid, task.Creator)
	}
	taskID := task.ID
	t.Logf("created task %s", taskID)

	// Claim it (open → claimed).
	updateBody := map[string]any{
		"status":   "claimed",
		"assignee": aid,
		"note":     "claiming for work",
	}
	status, body, err = cli.do("PATCH", "/api/v1/tasks/"+taskID, updateBody, agentID(aid))
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("claim: want 200, got %d (%s)", status, body)
	}
	claimed, err := decode[taskResp](body)
	if err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if claimed.Status != "claimed" {
		t.Errorf("claimed status: want claimed, got %s", claimed.Status)
	}
	if claimed.Assignee != aid {
		t.Errorf("assignee: want %q, got %q", aid, claimed.Assignee)
	}
	if len(claimed.Notes) != 1 {
		t.Errorf("notes count: want 1, got %d", len(claimed.Notes))
	}

	// Add another note.
	noteBody := map[string]any{
		"note": "making progress",
	}
	status, body, err = cli.do("PATCH", "/api/v1/tasks/"+taskID, noteBody, agentID(aid))
	if err != nil {
		t.Fatalf("add note: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("add note: want 200, got %d (%s)", status, body)
	}
	noted, err := decode[taskResp](body)
	if err != nil {
		t.Fatalf("decode note: %v", err)
	}
	if len(noted.Notes) != 2 {
		t.Errorf("notes after second: want 2, got %d", len(noted.Notes))
	}

	// Complete it (claimed → done).
	doneBody := map[string]any{
		"status": "done",
		"note":   "finished",
	}
	status, body, err = cli.do("PATCH", "/api/v1/tasks/"+taskID, doneBody, agentID(aid))
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("complete: want 200, got %d (%s)", status, body)
	}
	done, err := decode[taskResp](body)
	if err != nil {
		t.Fatalf("decode done: %v", err)
	}
	if done.Status != "done" {
		t.Errorf("done status: want done, got %s", done.Status)
	}
	if len(done.Notes) != 3 {
		t.Errorf("final notes: want 3, got %d", len(done.Notes))
	}

	// Delete.
	status, _, err = cli.do("DELETE", "/api/v1/tasks/"+taskID, nil)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if status != http.StatusNoContent {
		t.Fatalf("delete: want 204, got %d", status)
	}

	// Verify gone.
	status, _, err = cli.do("GET", "/api/v1/tasks/"+taskID, nil)
	if err != nil {
		t.Fatalf("verify gone: %v", err)
	}
	if status != http.StatusNotFound {
		t.Errorf("verify gone: want 404, got %d", status)
	}
}
