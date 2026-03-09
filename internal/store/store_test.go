package store_test

import (
	"os"
	"testing"

	"github.com/christmas-island/hive-server/internal/store"
)

// testDatabaseURL returns the DATABASE_URL for store tests.
// Tests are skipped if the env var is not set (no live DB required in CI without CRDB).
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping store integration test")
	}
	return url
}

// newTestStore creates a store connected to the test database.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	url := testDatabaseURL(t)
	s, err := store.New(url)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		cleanTestDB(t, s)
		_ = s.Close()
	})
	return s
}

// cleanTestDB removes all rows inserted during a test to keep the DB clean between runs.
func cleanTestDB(t *testing.T, s *store.Store) {
	t.Helper()
	db := s.DB()
	for _, tbl := range []string{"task_notes", "tasks", "memory", "agents"} {
		if _, err := db.Exec("DELETE FROM " + tbl); err != nil {
			t.Logf("cleanup %s: %v", tbl, err)
		}
	}
}

func TestNew(t *testing.T) {
	s := newTestStore(t)
	if s == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestNewInvalidURL(t *testing.T) {
	_, err := store.New("postgresql://invalid-host-that-does-not-exist:9999/hive?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("expected error for invalid connection URL")
	}
}

func TestClose(t *testing.T) {
	url := testDatabaseURL(t)
	s, err := store.New(url)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	cleanTestDB(t, s)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSchemaTables(t *testing.T) {
	s := newTestStore(t)
	db := s.DB()

	tables := []string{"memory", "tasks", "task_notes", "agents"}
	for _, table := range tables {
		var name string
		err := db.QueryRow(
			`SELECT table_name FROM information_schema.tables
             WHERE table_schema = current_schema() AND table_name = $1`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

// Remove unused import.
var _ = os.DevNull
