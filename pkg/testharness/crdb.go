//go:build integration

package testharness

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/cockroachdb"
)

// startCRDB starts a CockroachDB container and returns the connection URL.
// The container is stopped automatically via t.Cleanup.
func startCRDB(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	ctr, err := cockroachdb.Run(ctx, "cockroachdb/cockroach:latest-v24.3")
	if err != nil {
		t.Fatalf("start cockroachdb container: %v", err)
	}
	t.Cleanup(func() {
		if err := ctr.Terminate(ctx); err != nil {
			t.Logf("terminate cockroachdb container: %v", err)
		}
	})

	connStr, err := ctr.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("cockroachdb connection string: %v", err)
	}
	return connStr
}
