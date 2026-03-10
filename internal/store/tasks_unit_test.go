package store

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/christmas-island/hive-server/internal/model"
)

// taskColumns lists columns for task queries.
var taskColumns = []string{"id", "title", "description", "status", "creator", "assignee", "priority", "tags", "created_at", "updated_at"}

// sampleTaskRow builds a sample task row.
func sampleTaskRow(now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows(taskColumns).AddRow(
		"task-1", "Test Task", "Description", "open", "creator1", "", 0,
		`["tag1"]`,
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
}

// expectLoadTaskNotes sets up the mock expectation for loadTaskNotes.
func expectLoadTaskNotes(mock sqlmock.Sqlmock, taskID string, notes ...string) {
	rows := sqlmock.NewRows([]string{"note"})
	for _, note := range notes {
		rows = rows.AddRow(note)
	}
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT note FROM task_notes WHERE task_id = $1 ORDER BY created_at ASC, id ASC`,
	)).WithArgs(taskID).WillReturnRows(rows)
}

// --- GetTask ---

func TestGetTask_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
         FROM tasks WHERE id = $1`,
	)).WithArgs("task-1").WillReturnRows(sampleTaskRow(now))

	expectLoadTaskNotes(mock, "task-1", "note1", "note2")

	got, err := s.GetTask(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ID != "task-1" {
		t.Errorf("ID = %q, want task-1", got.ID)
	}
	if got.Title != "Test Task" {
		t.Errorf("Title = %q, want Test Task", got.Title)
	}
	if got.Status != model.TaskStatusOpen {
		t.Errorf("Status = %q, want open", got.Status)
	}
	if len(got.Notes) != 2 {
		t.Errorf("Notes len = %d, want 2", len(got.Notes))
	}
	if got.Notes[0] != "note1" {
		t.Errorf("Notes[0] = %q, want note1", got.Notes[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
         FROM tasks WHERE id = $1`,
	)).WithArgs("missing").WillReturnRows(sqlmock.NewRows(taskColumns))

	_, err := s.GetTask(context.Background(), "missing")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetTask_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("db error")
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
         FROM tasks WHERE id = $1`,
	)).WithArgs("task-1").WillReturnError(dbErr)

	_, err := s.GetTask(context.Background(), "task-1")
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

func TestGetTask_LoadNotesError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	notesErr := errors.New("notes error")

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
         FROM tasks WHERE id = $1`,
	)).WithArgs("task-1").WillReturnRows(sampleTaskRow(now))

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT note FROM task_notes WHERE task_id = $1 ORDER BY created_at ASC, id ASC`,
	)).WithArgs("task-1").WillReturnError(notesErr)

	_, err := s.GetTask(context.Background(), "task-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, notesErr) {
		t.Errorf("expected notesErr, got %v", err)
	}
}

func TestGetTask_NoNotes(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
         FROM tasks WHERE id = $1`,
	)).WithArgs("task-1").WillReturnRows(sampleTaskRow(now))

	expectLoadTaskNotes(mock, "task-1") // no notes

	got, err := s.GetTask(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("GetTask no notes: %v", err)
	}
	if len(got.Notes) != 0 {
		t.Errorf("Notes len = %d, want 0", len(got.Notes))
	}
}

// --- ListTasks ---

func TestListTasks_NoFilter(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows(taskColumns).
		AddRow("t1", "Task 1", "Desc", "open", "c1", "", 0, `[]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)).
		AddRow("t2", "Task 2", "Desc", "done", "c2", "a2", 1, `["bug"]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
          FROM tasks WHERE 1=1 ORDER BY created_at DESC LIMIT 50`,
	)).WillReturnRows(rows)

	expectLoadTaskNotes(mock, "t1")
	expectLoadTaskNotes(mock, "t2")

	got, err := s.ListTasks(context.Background(), model.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestListTasks_WithStatus(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows(taskColumns).
		AddRow("t1", "Task 1", "", "open", "c1", "", 0, `[]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
          FROM tasks WHERE 1=1 AND status = $1 ORDER BY created_at DESC LIMIT 50`,
	)).WithArgs("open").WillReturnRows(rows)

	expectLoadTaskNotes(mock, "t1")

	got, err := s.ListTasks(context.Background(), model.TaskFilter{Status: "open"})
	if err != nil {
		t.Fatalf("ListTasks with status: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want 1", len(got))
	}
}

func TestListTasks_WithAssignee(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows(taskColumns)

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
          FROM tasks WHERE 1=1 AND assignee = $1 ORDER BY created_at DESC LIMIT 50`,
	)).WithArgs("agent1").WillReturnRows(rows)

	_, err := s.ListTasks(context.Background(), model.TaskFilter{Assignee: "agent1"})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
}

func TestListTasks_WithCreator(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows(taskColumns)

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
          FROM tasks WHERE 1=1 AND creator = $1 ORDER BY created_at DESC LIMIT 50`,
	)).WithArgs("creator1").WillReturnRows(rows)

	_, err := s.ListTasks(context.Background(), model.TaskFilter{Creator: "creator1"})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
}

func TestListTasks_WithLimitAndOffset(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows(taskColumns)

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
          FROM tasks WHERE 1=1 ORDER BY created_at DESC LIMIT 10 OFFSET 5`,
	)).WillReturnRows(rows)

	_, err := s.ListTasks(context.Background(), model.TaskFilter{Limit: 10, Offset: 5})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
}

func TestListTasks_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("query error")
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
          FROM tasks WHERE 1=1 ORDER BY created_at DESC LIMIT 50`,
	)).WillReturnError(dbErr)

	_, err := s.ListTasks(context.Background(), model.TaskFilter{})
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

func TestListTasks_ScanError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows([]string{"id"}).AddRow("t1")

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
          FROM tasks WHERE 1=1 ORDER BY created_at DESC LIMIT 50`,
	)).WillReturnRows(rows)

	_, err := s.ListTasks(context.Background(), model.TaskFilter{})
	if err == nil {
		t.Error("expected scan error, got nil")
	}
}

func TestListTasks_LoadNotesError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows(taskColumns).
		AddRow("t1", "Task 1", "", "open", "c1", "", 0, `[]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
          FROM tasks WHERE 1=1 ORDER BY created_at DESC LIMIT 50`,
	)).WillReturnRows(rows)

	notesErr := errors.New("notes error")
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT note FROM task_notes WHERE task_id = $1 ORDER BY created_at ASC, id ASC`,
	)).WithArgs("t1").WillReturnError(notesErr)

	_, err := s.ListTasks(context.Background(), model.TaskFilter{})
	if err == nil {
		t.Fatal("expected notes error, got nil")
	}
}

func TestListTasks_Empty(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows(taskColumns)

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
          FROM tasks WHERE 1=1 ORDER BY created_at DESC LIMIT 50`,
	)).WillReturnRows(rows)

	got, err := s.ListTasks(context.Background(), model.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

// --- CreateTask ---

func TestCreateTask_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO tasks (id, title, description, status, creator, assignee, priority, tags, created_at, updated_at)`,
	)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	task := &model.Task{
		Title:       "New Task",
		Description: "Description",
		Creator:     "creator1",
		Tags:        []string{"tag1"},
	}
	got, err := s.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if got.ID == "" {
		t.Error("ID should be set")
	}
	if got.Status != model.TaskStatusOpen {
		t.Errorf("Status = %q, want open", got.Status)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if got.Notes == nil {
		t.Error("Notes should not be nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCreateTask_NilTags(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO tasks (id, title, description, status, creator, assignee, priority, tags, created_at, updated_at)`,
	)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	task := &model.Task{Title: "Task", Creator: "c1", Tags: nil}
	got, err := s.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("CreateTask nil tags: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil task")
	}
}

func TestCreateTask_ExecError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("insert failed")
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO tasks (id, title, description, status, creator, assignee, priority, tags, created_at, updated_at)`,
	)).WillReturnError(dbErr)
	mock.ExpectRollback()

	task := &model.Task{Title: "Task", Creator: "c1", Tags: []string{}}
	_, err := s.CreateTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- UpdateTask ---

func TestUpdateTask_Success_StatusTransition(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	newStatus := model.TaskStatusClaimed
	assignee := "agent1"

	// Fetch current task within RetryTx
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
             FROM tasks WHERE id = $1`,
	)).WithArgs("task-1").WillReturnRows(
		sqlmock.NewRows(taskColumns).AddRow(
			"task-1", "Task", "", "open", "c1", "", 0, `[]`,
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		),
	)
	mock.ExpectExec(regexp.QuoteMeta(
		`UPDATE tasks SET status = $1, assignee = $2, updated_at = $3 WHERE id = $4`,
	)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// GetTask after update
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
         FROM tasks WHERE id = $1`,
	)).WithArgs("task-1").WillReturnRows(
		sqlmock.NewRows(taskColumns).AddRow(
			"task-1", "Task", "", "claimed", "c1", "agent1", 0, `[]`,
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		),
	)
	expectLoadTaskNotes(mock, "task-1")

	upd := model.TaskUpdate{Status: &newStatus, Assignee: &assignee}
	got, err := s.UpdateTask(context.Background(), "task-1", upd)
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if got.Status != model.TaskStatusClaimed {
		t.Errorf("Status = %q, want claimed", got.Status)
	}
	if got.Assignee != "agent1" {
		t.Errorf("Assignee = %q, want agent1", got.Assignee)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestUpdateTask_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
             FROM tasks WHERE id = $1`,
	)).WithArgs("missing").WillReturnRows(sqlmock.NewRows(taskColumns))
	mock.ExpectRollback()

	_, err := s.UpdateTask(context.Background(), "missing", model.TaskUpdate{})
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateTask_InvalidTransition(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	// Try to transition from "done" to "open" — invalid (done is terminal)
	invalidStatus := model.TaskStatusOpen

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
             FROM tasks WHERE id = $1`,
	)).WithArgs("task-1").WillReturnRows(
		sqlmock.NewRows(taskColumns).AddRow(
			"task-1", "Task", "", "done", "c1", "", 0, `[]`,
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		),
	)
	mock.ExpectRollback()

	_, err := s.UpdateTask(context.Background(), "task-1", model.TaskUpdate{Status: &invalidStatus})
	if !errors.Is(err, model.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestUpdateTask_WithNote(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	note := "added a note"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
             FROM tasks WHERE id = $1`,
	)).WithArgs("task-1").WillReturnRows(
		sqlmock.NewRows(taskColumns).AddRow(
			"task-1", "Task", "", "open", "c1", "", 0, `[]`,
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		),
	)
	mock.ExpectExec(regexp.QuoteMeta(
		`UPDATE tasks SET status = $1, assignee = $2, updated_at = $3 WHERE id = $4`,
	)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO task_notes (task_id, note, agent_id, created_at) VALUES ($1, $2, $3, $4)`,
	)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	// GetTask
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
         FROM tasks WHERE id = $1`,
	)).WithArgs("task-1").WillReturnRows(
		sqlmock.NewRows(taskColumns).AddRow(
			"task-1", "Task", "", "open", "c1", "", 0, `[]`,
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		),
	)
	expectLoadTaskNotes(mock, "task-1", "added a note")

	upd := model.TaskUpdate{Note: &note, AgentID: "agent1"}
	got, err := s.UpdateTask(context.Background(), "task-1", upd)
	if err != nil {
		t.Fatalf("UpdateTask with note: %v", err)
	}
	if len(got.Notes) != 1 || got.Notes[0] != "added a note" {
		t.Errorf("Notes = %v, want [added a note]", got.Notes)
	}
}

func TestUpdateTask_FetchError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("db error")
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
             FROM tasks WHERE id = $1`,
	)).WithArgs("task-1").WillReturnError(dbErr)
	mock.ExpectRollback()

	_, err := s.UpdateTask(context.Background(), "task-1", model.TaskUpdate{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- DeleteTask ---

func TestDeleteTask_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM tasks WHERE id = $1`)).
		WithArgs("task-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := s.DeleteTask(context.Background(), "task-1"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestDeleteTask_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM tasks WHERE id = $1`)).
		WithArgs("missing").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := s.DeleteTask(context.Background(), "missing")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteTask_ExecError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("exec failed")
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM tasks WHERE id = $1`)).
		WithArgs("task-1").
		WillReturnError(dbErr)
	mock.ExpectRollback()

	err := s.DeleteTask(context.Background(), "task-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

// --- loadTaskNotes ---

func TestLoadTaskNotes_ScanError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	// Return a row that can't be scanned as a string.
	rows := sqlmock.NewRows([]string{"note", "extra"}).AddRow("note1", "extra-col")

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT note FROM task_notes WHERE task_id = $1 ORDER BY created_at ASC, id ASC`,
	)).WithArgs("task-1").WillReturnRows(rows)

	task := &model.Task{ID: "task-1"}
	err := s.loadTaskNotes(context.Background(), task)
	if err == nil {
		t.Error("expected scan error, got nil")
	}
}

// TestUpdateTask_SameStatus verifies that no status transition is applied when status equals current.
func TestUpdateTask_SameStatus(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	sameStatus := model.TaskStatusOpen // Same as current

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
             FROM tasks WHERE id = $1`,
	)).WithArgs("task-1").WillReturnRows(
		sqlmock.NewRows(taskColumns).AddRow(
			"task-1", "Task", "", "open", "c1", "", 0, `[]`,
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		),
	)
	mock.ExpectExec(regexp.QuoteMeta(
		`UPDATE tasks SET status = $1, assignee = $2, updated_at = $3 WHERE id = $4`,
	)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// GetTask
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
         FROM tasks WHERE id = $1`,
	)).WithArgs("task-1").WillReturnRows(
		sqlmock.NewRows(taskColumns).AddRow(
			"task-1", "Task", "", "open", "c1", "", 0, `[]`,
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		),
	)
	expectLoadTaskNotes(mock, "task-1")

	upd := model.TaskUpdate{Status: &sameStatus}
	got, err := s.UpdateTask(context.Background(), "task-1", upd)
	if err != nil {
		t.Fatalf("UpdateTask same status: %v", err)
	}
	if got.Status != model.TaskStatusOpen {
		t.Errorf("Status = %q, want open", got.Status)
	}
}
