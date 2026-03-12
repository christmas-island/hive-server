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

var todoColumns = []string{"id", "agent_id", "title", "status", "sort_order", "parent_task", "context", "created_at", "updated_at"}

func newTodoRow(id, agentID, title, status string, order int) *sqlmock.Rows {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return sqlmock.NewRows(todoColumns).AddRow(id, agentID, title, status, order, "", "", now, now)
}

// --- GetTodo ---

func TestGetTodo_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, agent_id, title, status, sort_order, parent_task, context, created_at, updated_at FROM todos WHERE id = $1`,
	)).WithArgs("todo-1").WillReturnRows(newTodoRow("todo-1", "shopclaw", "write tests", "pending", 0))

	got, err := s.GetTodo(context.Background(), "todo-1")
	if err != nil {
		t.Fatalf("GetTodo: %v", err)
	}
	if got.ID != "todo-1" {
		t.Errorf("ID = %q, want todo-1", got.ID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestGetTodo_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, agent_id, title, status, sort_order, parent_task, context, created_at, updated_at FROM todos WHERE id = $1`,
	)).WithArgs("missing").WillReturnRows(sqlmock.NewRows(todoColumns))

	_, err := s.GetTodo(context.Background(), "missing")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetTodo_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("db error")
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT id, agent_id, title, status, sort_order, parent_task, context, created_at, updated_at FROM todos WHERE id = $1`,
	)).WithArgs("todo-1").WillReturnError(dbErr)

	_, err := s.GetTodo(context.Background(), "todo-1")
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

// --- CreateTodo ---

func TestCreateTodo_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO todos`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	todo := &model.Todo{AgentID: "shopclaw", Title: "do a thing"}
	got, err := s.CreateTodo(context.Background(), todo)
	if err != nil {
		t.Fatalf("CreateTodo: %v", err)
	}
	if got.ID == "" {
		t.Error("expected non-empty ID")
	}
	if got.Status != model.TodoStatusPending {
		t.Errorf("status = %q, want pending", got.Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCreateTodo_DBError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("insert failed")
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO todos`).WillReturnError(dbErr)
	mock.ExpectRollback()

	_, err := s.CreateTodo(context.Background(), &model.Todo{AgentID: "shopclaw", Title: "x"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- ListTodos ---

func TestListTodos_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows(todoColumns)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rows.AddRow("t1", "shopclaw", "first", "pending", 0, "", "", now, now)
	rows.AddRow("t2", "shopclaw", "second", "done", 1, "", "", now, now)

	mock.ExpectQuery(`SELECT id, agent_id`).WithArgs("shopclaw").WillReturnRows(rows)

	got, err := s.ListTodos(context.Background(), model.TodoFilter{AgentID: "shopclaw"})
	if err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestListTodos_WithStatus(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows(todoColumns)
	mock.ExpectQuery(`SELECT id, agent_id`).WithArgs("shopclaw", "pending").WillReturnRows(rows)

	got, err := s.ListTodos(context.Background(), model.TodoFilter{AgentID: "shopclaw", Status: "pending"})
	if err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if got == nil {
		got = []*model.Todo{}
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

func TestListTodos_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("query failed")
	mock.ExpectQuery(`SELECT id, agent_id`).WithArgs("shopclaw").WillReturnError(dbErr)

	_, err := s.ListTodos(context.Background(), model.TodoFilter{AgentID: "shopclaw"})
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

// --- DeleteTodo ---

func TestDeleteTodo_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM todos WHERE id = $1`)).
		WithArgs("todo-1").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := s.DeleteTodo(context.Background(), "todo-1"); err != nil {
		t.Fatalf("DeleteTodo: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestDeleteTodo_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM todos WHERE id = $1`)).
		WithArgs("missing").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := s.DeleteTodo(context.Background(), "missing")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteTodo_DBError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("delete failed")
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM todos WHERE id = $1`)).
		WithArgs("todo-1").WillReturnError(dbErr)
	mock.ExpectRollback()

	err := s.DeleteTodo(context.Background(), "todo-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- PruneDoneTodos ---

func TestPruneDoneTodos_AllAgents(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM todos WHERE status`).
		WillReturnResult(sqlmock.NewResult(3, 3))
	mock.ExpectCommit()

	n, err := s.PruneDoneTodos(context.Background(), "", 24*time.Hour)
	if err != nil {
		t.Fatalf("PruneDoneTodos: %v", err)
	}
	if n != 3 {
		t.Errorf("n = %d, want 3", n)
	}
}

func TestPruneDoneTodos_SpecificAgent(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM todos WHERE status`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	n, err := s.PruneDoneTodos(context.Background(), "shopclaw", 24*time.Hour)
	if err != nil {
		t.Fatalf("PruneDoneTodos: %v", err)
	}
	if n != 1 {
		t.Errorf("n = %d, want 1", n)
	}
}

// --- ReorderTodos ---

func TestReorderTodos_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE todos SET sort_order`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`UPDATE todos SET sort_order`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := s.ReorderTodos(context.Background(), "shopclaw", []string{"t1", "t2"})
	if err != nil {
		t.Fatalf("ReorderTodos: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestReorderTodos_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE todos SET sort_order`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := s.ReorderTodos(context.Background(), "shopclaw", []string{"missing"})
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
