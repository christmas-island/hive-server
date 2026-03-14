//go:build integration

package testharness

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/meilisearch"
)

// startMeilisearch starts a Meilisearch container and returns the host URL.
// The container is stopped automatically via t.Cleanup.
// This is scaffolding for future use — the returned URL is not wired into the store yet.
func startMeilisearch(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	ctr, err := meilisearch.Run(ctx, "getmeili/meilisearch:latest")
	if err != nil {
		t.Fatalf("start meilisearch container: %v", err)
	}
	t.Cleanup(func() {
		if err := ctr.Terminate(ctx); err != nil {
			t.Logf("terminate meilisearch container: %v", err)
		}
	})

	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatalf("meilisearch host: %v", err)
	}
	port, err := ctr.MappedPort(ctx, "7700/tcp")
	if err != nil {
		t.Fatalf("meilisearch port: %v", err)
	}
	return "http://" + host + ":" + port.Port()
}
