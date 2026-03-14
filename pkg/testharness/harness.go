//go:build integration

// Package testharness provides an in-process hive-server backed by real
// services (via testcontainers-go) for integration testing by external
// consumers such as hive-local.
package testharness

import (
	"net/http/httptest"
	"testing"

	"github.com/christmas-island/hive-server/internal/relay"
	"github.com/christmas-island/hive-server/internal/server"
	"github.com/christmas-island/hive-server/internal/store"
)

// Option configures a test server.
type Option func(*options)

type options struct {
	token       string
	meilisearch bool
	gel         bool
}

// WithToken sets the bearer auth token for the test server.
// Default is "test-token".
func WithToken(token string) Option {
	return func(o *options) { o.token = token }
}

// WithMeilisearch starts a Meilisearch container alongside CockroachDB.
// The Meilisearch URL is logged but not wired into the store yet.
func WithMeilisearch() Option {
	return func(o *options) { o.meilisearch = true }
}

// WithGel starts a Gel (EdgeDB) container alongside CockroachDB.
// The Gel URL is logged but not wired into the store yet.
func WithGel() Option {
	return func(o *options) { o.gel = true }
}

// WithCRDBOnly is a no-op that documents the default: only CockroachDB is started.
func WithCRDBOnly() Option {
	return func(*options) {}
}

// NewTestServer starts backing service containers and a hive-server
// httptest.Server, returning the base URL. All resources are cleaned up
// automatically via t.Cleanup.
func NewTestServer(t *testing.T, opts ...Option) string {
	t.Helper()

	o := &options{token: "test-token"}
	for _, fn := range opts {
		fn(o)
	}

	// Always start CockroachDB.
	crdbURL := startCRDB(t)

	// Optional services (scaffolding — URLs logged but not connected to store).
	if o.meilisearch {
		msURL := startMeilisearch(t)
		t.Logf("meilisearch URL: %s", msURL)
	}
	if o.gel {
		gelURL := startGel(t)
		t.Logf("gel URL: %s", gelURL)
	}

	// Connect store.
	st, err := store.New(crdbURL)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// No-op relay client.
	rc := relay.New("", "")

	// Build the full HTTP handler and wrap in httptest.Server.
	handler := server.BuildMux(st, o.token, rc, "")
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	return ts.URL
}
