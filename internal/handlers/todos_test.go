package handlers_test

import (
	"net/http"
	"testing"

	"github.com/christmas-island/hive-server/internal/model"
)

const testAgentDragon = "dragon"

func TestTodoCreate(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	resp := request(t, srv, http.MethodPost, "/api/v1/todos", map[string]any{
		"title":       "write tests",
		"parent_task": "task-1",
		"context":     "handler tests",
	}, testToken, testAgentDragon)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var todo model.Todo
	decodeJSON(t, resp, &todo)
	if todo.Title != "write tests" {
		t.Errorf("title = %q, want write tests", todo.Title)
	}
	if todo.AgentID != testAgentDragon {
		t.Errorf("agent_id = %q, want %s", todo.AgentID, testAgentDragon)
	}
	if todo.Status != model.TodoStatusPending {
		t.Errorf("status = %q, want pending", todo.Status)
	}
}

func TestTodoCreate_MissingAgentID(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	resp := request(t, srv, http.MethodPost, "/api/v1/todos", map[string]any{
		"title": "no agent",
	}, testToken, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", resp.StatusCode)
	}
}

func TestTodoList(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	request(t, srv, http.MethodPost, "/api/v1/todos", map[string]any{"title": "todo A"}, testToken, testAgentDragon).Body.Close()
	request(t, srv, http.MethodPost, "/api/v1/todos", map[string]any{"title": "todo B"}, testToken, testAgentDragon).Body.Close()
	request(t, srv, http.MethodPost, "/api/v1/todos", map[string]any{"title": "not mine"}, testToken, "smokey").Body.Close()

	resp := request(t, srv, http.MethodGet, "/api/v1/todos", nil, testToken, testAgentDragon)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var todos []*model.Todo
	decodeJSON(t, resp, &todos)
	if len(todos) != 2 {
		t.Errorf("got %d todos, want 2", len(todos))
	}
}

func TestTodoUpdate(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	r1 := request(t, srv, http.MethodPost, "/api/v1/todos", map[string]any{"title": "original"}, testToken, testAgentDragon)
	var created model.Todo
	decodeJSON(t, r1, &created)

	resp := request(t, srv, http.MethodPatch, "/api/v1/todos/"+created.ID, map[string]any{
		"title":  "updated",
		"status": "done",
	}, testToken, testAgentDragon)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var todo model.Todo
	decodeJSON(t, resp, &todo)
	if todo.Title != "updated" {
		t.Errorf("title = %q, want updated", todo.Title)
	}
	if todo.Status != model.TodoStatusDone {
		t.Errorf("status = %q, want done", todo.Status)
	}
}

func TestTodoUpdate_WrongOwner(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	r1 := request(t, srv, http.MethodPost, "/api/v1/todos", map[string]any{"title": "mine"}, testToken, testAgentDragon)
	var created model.Todo
	decodeJSON(t, r1, &created)

	resp := request(t, srv, http.MethodPatch, "/api/v1/todos/"+created.ID, map[string]any{
		"title": "stolen",
	}, testToken, "smokey")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestTodoUpdate_NotFound(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	resp := request(t, srv, http.MethodPatch, "/api/v1/todos/no-such-id", map[string]any{
		"title": "ghost",
	}, testToken, testAgentDragon)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestTodoDelete(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	r1 := request(t, srv, http.MethodPost, "/api/v1/todos", map[string]any{"title": "temporary"}, testToken, testAgentDragon)
	var created model.Todo
	decodeJSON(t, r1, &created)

	resp := request(t, srv, http.MethodDelete, "/api/v1/todos/"+created.ID, nil, testToken, testAgentDragon)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestTodoDelete_WrongOwner(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	r1 := request(t, srv, http.MethodPost, "/api/v1/todos", map[string]any{"title": "mine"}, testToken, testAgentDragon)
	var created model.Todo
	decodeJSON(t, r1, &created)

	resp := request(t, srv, http.MethodDelete, "/api/v1/todos/"+created.ID, nil, testToken, "smokey")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestTodoDelete_NotFound(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	resp := request(t, srv, http.MethodDelete, "/api/v1/todos/no-such-id", nil, testToken, testAgentDragon)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestTodoPruneDone(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	r1 := request(t, srv, http.MethodPost, "/api/v1/todos", map[string]any{"title": "done todo"}, testToken, testAgentDragon)
	var created model.Todo
	decodeJSON(t, r1, &created)

	// Mark as done.
	request(t, srv, http.MethodPatch, "/api/v1/todos/"+created.ID, map[string]any{
		"status": "done",
	}, testToken, testAgentDragon).Body.Close()

	// Prune.
	resp := request(t, srv, http.MethodDelete, "/api/v1/todos/done", nil, testToken, testAgentDragon)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var result struct {
		Pruned int64 `json:"pruned"`
	}
	decodeJSON(t, resp, &result)
	if result.Pruned != 1 {
		t.Errorf("pruned = %d, want 1", result.Pruned)
	}
}

func TestTodoReorder(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	r1 := request(t, srv, http.MethodPost, "/api/v1/todos", map[string]any{"title": "first"}, testToken, testAgentDragon)
	var todoA model.Todo
	decodeJSON(t, r1, &todoA)

	r2 := request(t, srv, http.MethodPost, "/api/v1/todos", map[string]any{"title": "second"}, testToken, testAgentDragon)
	var todoB model.Todo
	decodeJSON(t, r2, &todoB)

	resp := request(t, srv, http.MethodPost, "/api/v1/todos/reorder", map[string]any{
		"ids": []string{todoB.ID, todoA.ID},
	}, testToken, testAgentDragon)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestTodoReorder_NotFound(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	resp := request(t, srv, http.MethodPost, "/api/v1/todos/reorder", map[string]any{
		"ids": []string{"no-such-id"},
	}, testToken, testAgentDragon)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestTodoCreate_StoreError(t *testing.T) {
	srv, ms := newMockServerWithStore(t, testToken)
	ms.injectErr("CreateTodo", errTest)

	resp := request(t, srv, http.MethodPost, "/api/v1/todos", map[string]any{
		"title": "fail",
	}, testToken, testAgentDragon)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestTodoList_StoreError(t *testing.T) {
	srv, ms := newMockServerWithStore(t, testToken)
	ms.injectErr("ListTodos", errTest)

	resp := request(t, srv, http.MethodGet, "/api/v1/todos", nil, testToken, testAgentDragon)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}
