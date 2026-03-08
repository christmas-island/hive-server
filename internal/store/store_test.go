package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/christmas-island/hive-server/internal/store"
)

// newTestStore creates a temporary SQLite store for testing.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// tempDBPath returns a path for a temporary database.
func tempDBPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "test.db")
}

func TestNew(t *testing.T) {
	s := newTestStore(t)
	if s == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestNewInvalidPath(t *testing.T) {
	_, err := store.New("/nonexistent/directory/test.db")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestClose(t *testing.T) {
	path := tempDBPath(t)
	s, err := store.New(path)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestWALMode(t *testing.T) {
	path := tempDBPath(t)
	s, err := store.New(path)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer func() { _ = s.Close() }()

	// Check WAL mode is enabled.
	var mode string
	row := s.DB().QueryRow(`PRAGMA journal_mode`)
	if err := row.Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("expected WAL mode, got %q", mode)
	}
}

func TestSchemaTables(t *testing.T) {
	s := newTestStore(t)
	db := s.DB()

	tables := []string{"memory", "tasks", "task_notes", "agents"}
	for _, table := range tables {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

// Remove unused import.
var _ = os.DevNull
