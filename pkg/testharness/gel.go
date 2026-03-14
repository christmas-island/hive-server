//go:build integration

package testharness

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// startGel starts a Gel (EdgeDB) container and returns the connection URL.
// The container is stopped automatically via t.Cleanup.
// This is scaffolding for future use — the returned URL is not wired into the store yet.
func startGel(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "geldata/gel:latest",
		ExposedPorts: []string{"5656/tcp"},
		Env: map[string]string{
			"GEL_SERVER_SECURITY": "insecure_dev_mode",
		},
		WaitingFor: wait.ForListeningPort("5656/tcp"),
	}

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start gel container: %v", err)
	}
	t.Cleanup(func() {
		if err := ctr.Terminate(ctx); err != nil {
			t.Logf("terminate gel container: %v", err)
		}
	})

	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatalf("gel host: %v", err)
	}
	port, err := ctr.MappedPort(ctx, "5656/tcp")
	if err != nil {
		t.Fatalf("gel port: %v", err)
	}
	return "gel://" + host + ":" + port.Port()
}
