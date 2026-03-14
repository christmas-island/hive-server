//go:build integration

// Package testutil provides test helpers for integration tests.
package testutil

import (
	"testing"

	"github.com/cockroachdb/cockroach-go/v2/testserver"

	"github.com/christmas-island/hive-server/internal/store"
)

// Deprecated: NewCockroachStore is superseded by pkg/testharness.NewTestServer,
// which uses testcontainers-go for container lifecycle management.
// Existing tests still use this helper; new tests should prefer the harness.
//
// NewCockroachStore starts an ephemeral CockroachDB single-node cluster
// and returns a connected *store.Store. The cluster is stopped when the
// test completes via t.Cleanup.
//
// This downloads the cockroach binary on first run (~200MB) and caches it
// for subsequent runs.
func NewCockroachStore(t *testing.T) *store.Store {
	t.Helper()

	ts, err := testserver.NewTestServer()
	if err != nil {
		t.Fatalf("testserver.NewTestServer: %v", err)
	}
	t.Cleanup(ts.Stop)

	pgURL := ts.PGURL()
	if pgURL == nil {
		t.Fatal("testserver returned nil PGURL")
	}

	// Create the hive database.
	initStore, err := store.New(pgURL.String())
	if err != nil {
		// The default database is 'defaultdb'. Try creating 'hive' if needed,
		// but for ephemeral tests the default database works fine.
		// store.New runs migrations on connect, so this should succeed
		// as long as the connection is valid.
		t.Fatalf("store.New with testserver URL: %v", err)
	}

	t.Cleanup(func() { _ = initStore.Close() })
	return initStore
}
