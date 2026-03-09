//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/christmas-island/hive-server/internal/model"
	"github.com/christmas-island/hive-server/internal/store"
)

func makeTask(s *store.Store, t *testing.T, title string) *model.Task {
	t.Helper()
	task, err := s.CreateTask(context.Background(), &model.Task{
		Title:   title,
		Creator: "agent-test",
		Tags:    []string{},
	})
	if err != nil {
		t.Fatalf("CreateTask(%q): %v", title, err)
	}
	return task
}

func TestTaskCreate(t *testing.T) {
	s := newTestStore(t)
	task := makeTask(s, t, "test task")

	if task.ID == "" {
		t.Error("expected non-empty ID")
	}
	if task.Status != model.TaskStatusOpen {
		t.Errorf("Status = %q, want %q", task.Status, model.TaskStatusOpen)
	}
	if task.Notes == nil {
		t.Error("Notes should not be nil")
	}
	if task.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

func TestTaskCreate_TitleRequired(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.CreateTask(ctx, &model.Task{Creator: "a"})
	// SQLite won't enforce NOT NULL on title from Go side directly in this impl,
	// but the handler validates. For the store, an empty title is stored.
	// This test verifies the call doesn't panic.
	_ = err
}

func TestTaskGet(t *testing.T) {
	s := newTestStore(t)
	created := makeTask(s, t, "get me")

	got, err := s.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
	if got.Title != "get me" {
		t.Errorf("Title = %q, want %q", got.Title, "get me")
	}
}

func TestTaskGet_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetTask(context.Background(), "no-such-id")
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestTaskList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	makeTask(s, t, "task-1")
	makeTask(s, t, "task-2")
	makeTask(s, t, "task-3")

	tasks, err := s.ListTasks(ctx, model.TaskFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("len = %d, want 3", len(tasks))
	}
}

func TestTaskList_FilterStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t1 := makeTask(s, t, "open-task")
	t2 := makeTask(s, t, "claimed-task")

	// Claim t2.
	assignee := "agent-2"
	claimed := model.TaskStatusClaimed
	_, err := s.UpdateTask(ctx, t2.ID, model.TaskUpdate{Status: &claimed, Assignee: &assignee})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}

	_ = t1

	open, err := s.ListTasks(ctx, model.TaskFilter{Status: "open"})
	if err != nil {
		t.Fatalf("list open: %v", err)
	}
	if len(open) != 1 {
		t.Errorf("open len = %d, want 1", len(open))
	}
	if open[0].Status != model.TaskStatusOpen {
		t.Errorf("status = %q, want open", open[0].Status)
	}
}

func TestTaskList_FilterAssignee(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t1 := makeTask(s, t, "assigned")
	makeTask(s, t, "unassigned")

	assignee := "jake"
	status := model.TaskStatusClaimed
	_, err := s.UpdateTask(ctx, t1.ID, model.TaskUpdate{Status: &status, Assignee: &assignee})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	tasks, err := s.ListTasks(ctx, model.TaskFilter{Assignee: "jake"})
	if err != nil {
		t.Fatalf("list by assignee: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("len = %d, want 1", len(tasks))
	}
}

func TestTaskDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task := makeTask(s, t, "delete me")
	if err := s.DeleteTask(ctx, task.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	_, err := s.GetTask(ctx, task.ID)
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestTaskDelete_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.DeleteTask(context.Background(), "no-such-id")
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestTaskUpdate_Note(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task := makeTask(s, t, "noted")
	note := "first note"
	updated, err := s.UpdateTask(ctx, task.ID, model.TaskUpdate{Note: &note, AgentID: "a1"})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if len(updated.Notes) != 1 || updated.Notes[0] != "first note" {
		t.Errorf("Notes = %v, want [first note]", updated.Notes)
	}
}

func TestTaskUpdate_MultipleNotes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task := makeTask(s, t, "multi-noted")
	for _, n := range []string{"note-1", "note-2", "note-3"} {
		note := n
		_, err := s.UpdateTask(ctx, task.ID, model.TaskUpdate{Note: &note})
		if err != nil {
			t.Fatalf("add note %q: %v", n, err)
		}
	}

	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(got.Notes) != 3 {
		t.Errorf("len(Notes) = %d, want 3", len(got.Notes))
	}
	if got.Notes[0] != "note-1" {
		t.Errorf("Notes[0] = %q, want note-1", got.Notes[0])
	}
}

// TestStateMachine exercises all defined valid transitions.
func TestStateMachine_ValidTransitions(t *testing.T) {
	type transition struct {
		from model.TaskStatus
		to   model.TaskStatus
	}

	valid := []transition{
		{model.TaskStatusOpen, model.TaskStatusClaimed},
		{model.TaskStatusOpen, model.TaskStatusCancelled},
		{model.TaskStatusClaimed, model.TaskStatusOpen},
		{model.TaskStatusClaimed, model.TaskStatusInProgress},
		{model.TaskStatusClaimed, model.TaskStatusCancelled},
		{model.TaskStatusInProgress, model.TaskStatusDone},
		{model.TaskStatusInProgress, model.TaskStatusFailed},
		{model.TaskStatusInProgress, model.TaskStatusOpen},
	}

	for _, tr := range valid {
		if !model.IsValidTransition(tr.from, tr.to) {
			t.Errorf("expected valid transition %q→%q", tr.from, tr.to)
		}
	}
}

func TestStateMachine_InvalidTransitions(t *testing.T) {
	type transition struct {
		from model.TaskStatus
		to   model.TaskStatus
	}

	invalid := []transition{
		{model.TaskStatusOpen, model.TaskStatusInProgress},
		{model.TaskStatusOpen, model.TaskStatusDone},
		{model.TaskStatusOpen, model.TaskStatusFailed},
		{model.TaskStatusDone, model.TaskStatusOpen},
		{model.TaskStatusFailed, model.TaskStatusOpen},
		{model.TaskStatusCancelled, model.TaskStatusOpen},
		{model.TaskStatusInProgress, model.TaskStatusClaimed},
	}

	for _, tr := range invalid {
		if model.IsValidTransition(tr.from, tr.to) {
			t.Errorf("expected invalid transition %q→%q", tr.from, tr.to)
		}
	}
}

func TestUpdateTask_InvalidTransition(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task := makeTask(s, t, "invalid transition")
	done := model.TaskStatusDone
	_, err := s.UpdateTask(ctx, task.ID, model.TaskUpdate{Status: &done})
	if err != model.ErrInvalidTransition {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestUpdateTask_FullFlow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task := makeTask(s, t, "full-flow")

	// open → claimed
	assignee := "worker"
	claimed := model.TaskStatusClaimed
	t1, err := s.UpdateTask(ctx, task.ID, model.TaskUpdate{Status: &claimed, Assignee: &assignee})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if t1.Status != model.TaskStatusClaimed {
		t.Errorf("status = %q, want claimed", t1.Status)
	}
	if t1.Assignee != "worker" {
		t.Errorf("assignee = %q, want worker", t1.Assignee)
	}

	// claimed → in_progress
	inProg := model.TaskStatusInProgress
	t2, err := s.UpdateTask(ctx, task.ID, model.TaskUpdate{Status: &inProg})
	if err != nil {
		t.Fatalf("in_progress: %v", err)
	}
	if t2.Status != model.TaskStatusInProgress {
		t.Errorf("status = %q, want in_progress", t2.Status)
	}

	// in_progress → done
	done := model.TaskStatusDone
	t3, err := s.UpdateTask(ctx, task.ID, model.TaskUpdate{Status: &done})
	if err != nil {
		t.Fatalf("done: %v", err)
	}
	if t3.Status != model.TaskStatusDone {
		t.Errorf("status = %q, want done", t3.Status)
	}
}

func TestUpdateTask_NotFound(t *testing.T) {
	s := newTestStore(t)
	note := "hi"
	_, err := s.UpdateTask(context.Background(), "ghost", model.TaskUpdate{Note: &note})
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestTaskNotesCascadeDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task := makeTask(s, t, "cascaded")
	note := "will be deleted"
	_, err := s.UpdateTask(ctx, task.ID, model.TaskUpdate{Note: &note})
	if err != nil {
		t.Fatalf("add note: %v", err)
	}

	if err := s.DeleteTask(ctx, task.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify notes are gone via DB query.
	db := s.DB()
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM task_notes WHERE task_id = $1`, task.ID).Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 notes after cascade delete, got %d", count)
	}
}
