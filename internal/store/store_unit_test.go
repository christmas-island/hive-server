package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// newMockDBWithPing creates a sqlmock DB with ping monitoring enabled.
func newMockDBWithPing(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New(MonitorPings): %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, mock
}

// TestStore_Close verifies that Close delegates to the underlying db.
func TestStore_Close(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	s := &Store{db: db}

	// sqlmock v1.5.2 requires ExpectClose to be registered.
	mock.ExpectClose()

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestStore_DB verifies that DB() returns the underlying *sql.DB.
func TestStore_DB(t *testing.T) {
	db, _ := newMockDB(t)
	s := &Store{db: db}

	got := s.DB()
	if got != db {
		t.Error("DB() returned wrong *sql.DB")
	}
}

// TestStore_Ping_Success verifies that Ping succeeds when the DB is available.
func TestStore_Ping_Success(t *testing.T) {
	db, mock := newMockDBWithPing(t)
	s := &Store{db: db}

	mock.ExpectPing()

	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestStore_Ping_Error verifies that Ping returns an error when the DB is unavailable.
func TestStore_Ping_Error(t *testing.T) {
	db, mock := newMockDBWithPing(t)
	s := &Store{db: db}

	pingErr := errors.New("connection refused")
	mock.ExpectPing().WillReturnError(pingErr)

	err := s.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error from Ping, got nil")
	}
	if !errors.Is(err, pingErr) {
		t.Errorf("expected pingErr, got %v", err)
	}
}
