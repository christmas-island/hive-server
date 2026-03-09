package handlers_test

import (
	"net/http"
	"testing"

	"github.com/christmas-island/hive-server/internal/model"
)

// TestTaskCreate tests POST /api/v1/tasks
func TestTaskCreate(t *testing.T) {
	srv := newTestServer(t, testToken)

	resp := request(t, srv, http.MethodPost, "/api/v1/tasks", map[string]any{
		"title":       "build feature X",
		"description": "detailed description",
		"priority":    2,
		"tags":        []string{"feat", "backend"},
	}, testToken, testAgent)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var task model.Task
	decodeJSON(t, resp, &task)

	if task.ID == "" {
		t.Error("ID is empty")
	}
	if task.Title != "build feature X" {
		t.Errorf("Title = %q", task.Title)
	}
	if task.Status != model.TaskStatusOpen {
		t.Errorf("Status = %q, want open", task.Status)
	}
	if task.Creator != testAgent {
		t.Errorf("Creator = %q, want %q", task.Creator, testAgent)
	}
	if task.Priority != 2 {
		t.Errorf("Priority = %d, want 2", task.Priority)
	}
	if len(task.Tags) != 2 {
		t.Errorf("Tags len = %d, want 2", len(task.Tags))
	}
}

func TestTaskCreate_MissingTitle(t *testing.T) {
	srv := newTestServer(t, testToken)
	resp := request(t, srv, http.MethodPost, "/api/v1/tasks", map[string]any{
		"description": "no title",
	}, testToken, testAgent)
	defer resp.Body.Close()
	// Huma validates input before the handler runs; missing/empty required
	// string fields return 422 Unprocessable Entity.
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestTaskGet(t *testing.T) {
	srv := newTestServer(t, testToken)

	// Create.
	r1 := request(t, srv, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "get me"}, testToken, testAgent)
	var created model.Task
	decodeJSON(t, r1, &created)

	resp := request(t, srv, http.MethodGet, "/api/v1/tasks/"+created.ID, nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got model.Task
	decodeJSON(t, resp, &got)
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
}

func TestTaskGet_NotFound(t *testing.T) {
	srv := newTestServer(t, testToken)
	resp := request(t, srv, http.MethodGet, "/api/v1/tasks/no-such-id", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestTaskList(t *testing.T) {
	srv := newTestServer(t, testToken)

	for _, title := range []string{"t1", "t2", "t3"} {
		request(t, srv, http.MethodPost, "/api/v1/tasks", map[string]any{"title": title}, testToken, testAgent).Body.Close()
	}

	resp := request(t, srv, http.MethodGet, "/api/v1/tasks", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var tasks []model.Task
	decodeJSON(t, resp, &tasks)
	if len(tasks) != 3 {
		t.Errorf("len = %d, want 3", len(tasks))
	}
}

func TestTaskList_FilterStatus(t *testing.T) {
	srv := newTestServer(t, testToken)

	// Create two tasks.
	r1 := request(t, srv, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "t1"}, testToken, testAgent)
	var t1 model.Task
	decodeJSON(t, r1, &t1)
	request(t, srv, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "t2"}, testToken, testAgent).Body.Close()

	// Claim t1.
	request(t, srv, http.MethodPatch, "/api/v1/tasks/"+t1.ID, map[string]any{
		"status": "claimed", "assignee": "jake",
	}, testToken, testAgent).Body.Close()

	resp := request(t, srv, http.MethodGet, "/api/v1/tasks?status=open", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var tasks []model.Task
	decodeJSON(t, resp, &tasks)
	if len(tasks) != 1 {
		t.Errorf("open len = %d, want 1", len(tasks))
	}
}

func TestTaskUpdate_StatusTransition(t *testing.T) {
	srv := newTestServer(t, testToken)

	r1 := request(t, srv, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "transit"}, testToken, testAgent)
	var task model.Task
	decodeJSON(t, r1, &task)

	// Claim.
	r2 := request(t, srv, http.MethodPatch, "/api/v1/tasks/"+task.ID, map[string]any{
		"status": "claimed", "assignee": "worker",
	}, testToken, testAgent)
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("claim status = %d, want 200", r2.StatusCode)
	}
	var claimed model.Task
	decodeJSON(t, r2, &claimed)
	if claimed.Status != model.TaskStatusClaimed {
		t.Errorf("Status = %q, want claimed", claimed.Status)
	}

	// In progress.
	r3 := request(t, srv, http.MethodPatch, "/api/v1/tasks/"+task.ID, map[string]any{
		"status": "in_progress",
	}, testToken, testAgent)
	if r3.StatusCode != http.StatusOK {
		t.Fatalf("in_progress status = %d, want 200", r3.StatusCode)
	}
	r3.Body.Close()

	// Done.
	r4 := request(t, srv, http.MethodPatch, "/api/v1/tasks/"+task.ID, map[string]any{
		"status": "done",
	}, testToken, testAgent)
	if r4.StatusCode != http.StatusOK {
		t.Fatalf("done status = %d, want 200", r4.StatusCode)
	}
	r4.Body.Close()
}

func TestTaskUpdate_InvalidTransition(t *testing.T) {
	srv := newTestServer(t, testToken)

	r1 := request(t, srv, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "bad-transit"}, testToken, testAgent)
	var task model.Task
	decodeJSON(t, r1, &task)

	// open → done (invalid)
	resp := request(t, srv, http.MethodPatch, "/api/v1/tasks/"+task.ID, map[string]any{
		"status": "done",
	}, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestTaskUpdate_AddNote(t *testing.T) {
	srv := newTestServer(t, testToken)

	r1 := request(t, srv, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "noted"}, testToken, testAgent)
	var task model.Task
	decodeJSON(t, r1, &task)

	// Add note.
	r2 := request(t, srv, http.MethodPatch, "/api/v1/tasks/"+task.ID, map[string]any{
		"note": "progress update",
	}, testToken, testAgent)
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", r2.StatusCode)
	}
	var updated model.Task
	decodeJSON(t, r2, &updated)
	if len(updated.Notes) != 1 || updated.Notes[0] != "progress update" {
		t.Errorf("Notes = %v", updated.Notes)
	}
}

func TestTaskUpdate_NotFound(t *testing.T) {
	srv := newTestServer(t, testToken)
	resp := request(t, srv, http.MethodPatch, "/api/v1/tasks/no-such", map[string]any{"note": "hi"}, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestTaskDelete(t *testing.T) {
	srv := newTestServer(t, testToken)

	r1 := request(t, srv, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "del me"}, testToken, testAgent)
	var task model.Task
	decodeJSON(t, r1, &task)

	resp := request(t, srv, http.MethodDelete, "/api/v1/tasks/"+task.ID, nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}

	resp2 := request(t, srv, http.MethodGet, "/api/v1/tasks/"+task.ID, nil, testToken, testAgent)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("after delete: status = %d, want 404", resp2.StatusCode)
	}
}

func TestTaskDelete_NotFound(t *testing.T) {
	srv := newTestServer(t, testToken)
	resp := request(t, srv, http.MethodDelete, "/api/v1/tasks/ghost", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
