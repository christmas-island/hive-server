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

// --- GetMemory ---

func TestGetMemory_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"key", "value", "agent_id", "tags", "version", "created_at", "updated_at"}).
		AddRow("mykey", "myvalue", "agent1", `["tag1"]`, 1, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE key = $1`,
	)).WithArgs("mykey").WillReturnRows(rows)

	got, err := s.GetMemory(context.Background(), "mykey")
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if got.Key != "mykey" {
		t.Errorf("Key = %q, want mykey", got.Key)
	}
	if got.Value != "myvalue" {
		t.Errorf("Value = %q, want myvalue", got.Value)
	}
	if got.Version != 1 {
		t.Errorf("Version = %d, want 1", got.Version)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "tag1" {
		t.Errorf("Tags = %v, want [tag1]", got.Tags)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestGetMemory_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows([]string{"key", "value", "agent_id", "tags", "version", "created_at", "updated_at"})

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE key = $1`,
	)).WithArgs("missing").WillReturnRows(rows)

	_, err := s.GetMemory(context.Background(), "missing")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetMemory_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("db error")
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE key = $1`,
	)).WithArgs("key1").WillReturnError(dbErr)

	_, err := s.GetMemory(context.Background(), "key1")
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

// --- ListMemory ---

func TestListMemory_NoFilters(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"key", "value", "agent_id", "tags", "version", "created_at", "updated_at"}).
		AddRow("k1", "v1", "a1", `[]`, 1, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)).
		AddRow("k2", "v2", "a2", `["t1"]`, 2, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE 1=1 ORDER BY updated_at DESC LIMIT 50`,
	)).WillReturnRows(rows)

	got, err := s.ListMemory(context.Background(), model.MemoryFilter{})
	if err != nil {
		t.Fatalf("ListMemory: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestListMemory_WithTag(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"key", "value", "agent_id", "tags", "version", "created_at", "updated_at"}).
		AddRow("k1", "v1", "a1", `["bug"]`, 1, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE 1=1 AND tags @> jsonb_build_array($1::text) ORDER BY updated_at DESC LIMIT 50`,
	)).WithArgs("bug").WillReturnRows(rows)

	got, err := s.ListMemory(context.Background(), model.MemoryFilter{Tag: "bug"})
	if err != nil {
		t.Fatalf("ListMemory with tag: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want 1", len(got))
	}
}

func TestListMemory_WithAgent(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"key", "value", "agent_id", "tags", "version", "created_at", "updated_at"}).
		AddRow("k1", "v1", "agent1", `[]`, 1, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE 1=1 AND agent_id = $1 ORDER BY updated_at DESC LIMIT 50`,
	)).WithArgs("agent1").WillReturnRows(rows)

	got, err := s.ListMemory(context.Background(), model.MemoryFilter{Agent: "agent1"})
	if err != nil {
		t.Fatalf("ListMemory with agent: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want 1", len(got))
	}
}

func TestListMemory_WithPrefix(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"key", "value", "agent_id", "tags", "version", "created_at", "updated_at"}).
		AddRow("prefix:key1", "v1", "a1", `[]`, 1, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE 1=1 AND key LIKE $1 ORDER BY updated_at DESC LIMIT 50`,
	)).WithArgs("prefix:%").WillReturnRows(rows)

	got, err := s.ListMemory(context.Background(), model.MemoryFilter{Prefix: "prefix:"})
	if err != nil {
		t.Fatalf("ListMemory with prefix: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want 1", len(got))
	}
}

func TestListMemory_WithLimit(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows([]string{"key", "value", "agent_id", "tags", "version", "created_at", "updated_at"})

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE 1=1 ORDER BY updated_at DESC LIMIT 10`,
	)).WillReturnRows(rows)

	_, err := s.ListMemory(context.Background(), model.MemoryFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListMemory with limit: %v", err)
	}
}

func TestListMemory_WithOffset(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows([]string{"key", "value", "agent_id", "tags", "version", "created_at", "updated_at"})

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE 1=1 ORDER BY updated_at DESC LIMIT 50 OFFSET 20`,
	)).WillReturnRows(rows)

	_, err := s.ListMemory(context.Background(), model.MemoryFilter{Offset: 20})
	if err != nil {
		t.Fatalf("ListMemory with offset: %v", err)
	}
}

func TestListMemory_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("query failed")
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE 1=1 ORDER BY updated_at DESC LIMIT 50`,
	)).WillReturnError(dbErr)

	_, err := s.ListMemory(context.Background(), model.MemoryFilter{})
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

func TestListMemory_ScanError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	// Return wrong column count to trigger scan error.
	rows := sqlmock.NewRows([]string{"key"}).AddRow("k1")

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE 1=1 ORDER BY updated_at DESC LIMIT 50`,
	)).WillReturnRows(rows)

	_, err := s.ListMemory(context.Background(), model.MemoryFilter{})
	if err == nil {
		t.Error("expected scan error, got nil")
	}
}

func TestListMemory_Empty(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	rows := sqlmock.NewRows([]string{"key", "value", "agent_id", "tags", "version", "created_at", "updated_at"})

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE 1=1 ORDER BY updated_at DESC LIMIT 50`,
	)).WillReturnRows(rows)

	got, err := s.ListMemory(context.Background(), model.MemoryFilter{})
	if err != nil {
		t.Fatalf("ListMemory empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

// --- DeleteMemory ---

func TestDeleteMemory_Success(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM memory WHERE key = $1`)).
		WithArgs("mykey").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := s.DeleteMemory(context.Background(), "mykey"); err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestDeleteMemory_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM memory WHERE key = $1`)).
		WithArgs("missing").
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected
	mock.ExpectRollback()

	err := s.DeleteMemory(context.Background(), "missing")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteMemory_ExecError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("exec failed")
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM memory WHERE key = $1`)).
		WithArgs("mykey").
		WillReturnError(dbErr)
	mock.ExpectRollback()

	err := s.DeleteMemory(context.Background(), "mykey")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

// --- UpsertMemory ---

func TestUpsertMemory_Insert(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	// Check for existing: returns no rows
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE key = $1`,
	)).WithArgs("newkey").WillReturnRows(
		sqlmock.NewRows([]string{"key", "value", "agent_id", "tags", "version", "created_at", "updated_at"}),
	)
	// Insert
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO memory (key, value, agent_id, tags, version, created_at, updated_at)`,
	)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	entry := &model.MemoryEntry{
		Key:     "newkey",
		Value:   "newvalue",
		AgentID: "agent1",
		Tags:    []string{"tag1"},
	}
	got, err := s.UpsertMemory(context.Background(), entry)
	if err != nil {
		t.Fatalf("UpsertMemory insert: %v", err)
	}
	if got.Key != "newkey" {
		t.Errorf("Key = %q, want newkey", got.Key)
	}
	if got.Version != 1 {
		t.Errorf("Version = %d, want 1", got.Version)
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestUpsertMemory_Update(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	mock.ExpectBegin()
	// Check for existing: returns a row
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE key = $1`,
	)).WithArgs("existingkey").WillReturnRows(
		sqlmock.NewRows([]string{"key", "value", "agent_id", "tags", "version", "created_at", "updated_at"}).
			AddRow("existingkey", "oldval", "agent1", `[]`, 3, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)),
	)
	// Update
	mock.ExpectExec(regexp.QuoteMeta(
		`UPDATE memory SET value = $1, agent_id = $2, tags = $3, version = version + 1, updated_at = $4`,
	)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	entry := &model.MemoryEntry{
		Key:     "existingkey",
		Value:   "newval",
		AgentID: "agent1",
		Tags:    []string{},
		Version: 3, // matches existing
	}
	got, err := s.UpsertMemory(context.Background(), entry)
	if err != nil {
		t.Fatalf("UpsertMemory update: %v", err)
	}
	if got.Version != 4 {
		t.Errorf("Version = %d, want 4", got.Version)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestUpsertMemory_OptimisticConflict(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	now := time.Now().UTC()

	mock.ExpectBegin()
	// Existing entry has version 5
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE key = $1`,
	)).WithArgs("key1").WillReturnRows(
		sqlmock.NewRows([]string{"key", "value", "agent_id", "tags", "version", "created_at", "updated_at"}).
			AddRow("key1", "v", "a", `[]`, 5, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)),
	)
	mock.ExpectRollback()

	entry := &model.MemoryEntry{
		Key:     "key1",
		Value:   "new",
		AgentID: "agent1",
		Tags:    []string{},
		Version: 3, // does not match existing version 5
	}
	_, err := s.UpsertMemory(context.Background(), entry)
	if !errors.Is(err, model.ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

func TestUpsertMemory_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	dbErr := errors.New("query error")
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE key = $1`,
	)).WithArgs("key1").WillReturnError(dbErr)
	mock.ExpectRollback()

	entry := &model.MemoryEntry{Key: "key1", Value: "v", Tags: []string{}}
	_, err := s.UpsertMemory(context.Background(), entry)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr, got %v", err)
	}
}

func TestUpsertMemory_InsertError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	insertErr := errors.New("insert failed")
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE key = $1`,
	)).WithArgs("newkey").WillReturnRows(
		sqlmock.NewRows([]string{"key", "value", "agent_id", "tags", "version", "created_at", "updated_at"}),
	)
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO memory (key, value, agent_id, tags, version, created_at, updated_at)`,
	)).WillReturnError(insertErr)
	mock.ExpectRollback()

	entry := &model.MemoryEntry{Key: "newkey", Value: "v", Tags: []string{}}
	_, err := s.UpsertMemory(context.Background(), entry)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestUpsertMemory_UpdateError(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	updateErr := errors.New("update failed")
	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE key = $1`,
	)).WithArgs("key1").WillReturnRows(
		sqlmock.NewRows([]string{"key", "value", "agent_id", "tags", "version", "created_at", "updated_at"}).
			AddRow("key1", "oldval", "a1", `[]`, 1, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)),
	)
	mock.ExpectExec(regexp.QuoteMeta(
		`UPDATE memory SET value = $1, agent_id = $2, tags = $3, version = version + 1, updated_at = $4`,
	)).WillReturnError(updateErr)
	mock.ExpectRollback()

	entry := &model.MemoryEntry{Key: "key1", Value: "newval", Tags: []string{}}
	_, err := s.UpsertMemory(context.Background(), entry)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestUpsertMemory_NilTags(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE key = $1`,
	)).WithArgs("newkey").WillReturnRows(
		sqlmock.NewRows([]string{"key", "value", "agent_id", "tags", "version", "created_at", "updated_at"}),
	)
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO memory (key, value, agent_id, tags, version, created_at, updated_at)`,
	)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	entry := &model.MemoryEntry{Key: "newkey", Value: "v", Tags: nil}
	got, err := s.UpsertMemory(context.Background(), entry)
	if err != nil {
		t.Fatalf("UpsertMemory nil tags: %v", err)
	}
	if got.Tags == nil {
		t.Error("expected non-nil tags after upsert")
	}
}

func TestUpsertMemory_WithCreatedAt(t *testing.T) {
	db, mock := newMockDB(t)
	s := &Store{db: db}

	customCreated := time.Now().UTC().Add(-time.Hour)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT key, value, agent_id, tags, version, created_at, updated_at FROM memory WHERE key = $1`,
	)).WithArgs("newkey").WillReturnRows(
		sqlmock.NewRows([]string{"key", "value", "agent_id", "tags", "version", "created_at", "updated_at"}),
	)
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO memory (key, value, agent_id, tags, version, created_at, updated_at)`,
	)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	entry := &model.MemoryEntry{
		Key:       "newkey",
		Value:     "v",
		Tags:      []string{},
		CreatedAt: customCreated,
	}
	got, err := s.UpsertMemory(context.Background(), entry)
	if err != nil {
		t.Fatalf("UpsertMemory with createdAt: %v", err)
	}
	// CreatedAt should match the custom value.
	if !got.CreatedAt.Equal(customCreated) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, customCreated)
	}
}
