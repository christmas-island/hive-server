//go:build e2e

package e2e

import (
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/christmas-island/hive-server/internal/store"
)

var (
	// cli is the shared E2E HTTP client, initialised in TestMain.
	cli *hiveClient

	// authToken is the value of E2E_TOKEN (empty = auth disabled on server).
	authToken string

	// taskCleanup tracks task IDs created during tests so TestMain can
	// perform a post-run safety-net sweep even if individual test defers fail.
	taskMu      sync.Mutex
	taskCleanup []string
)

// TestMain bootstraps the E2E suite: reads environment, runs all tests, then
// performs a best-effort cleanup of any __e2e__ resources left behind.
func TestMain(m *testing.M) {
	targetURL := os.Getenv("E2E_TARGET_URL")
	if targetURL == "" {
		fmt.Fprintln(os.Stderr, "E2E_TARGET_URL is required")
		os.Exit(1)
	}
	authToken = os.Getenv("E2E_TOKEN")
	cli = newHiveClient(targetURL, authToken)

	code := m.Run()

	sweepE2EResources()

	os.Exit(code)
}

// trackTask registers a task ID for post-run cleanup.
func trackTask(id string) {
	taskMu.Lock()
	defer taskMu.Unlock()
	taskCleanup = append(taskCleanup, id)
}

// sweepE2EResources deletes all __e2e__ prefixed resources as a safety net.
// Individual tests are responsible for their own cleanup via defer; this sweep
// handles any leftovers (e.g. from panics or skipped cleanup).
func sweepE2EResources() {
	fmt.Println("[e2e sweep] cleaning up __e2e__ resources")

	// Memory: list all entries with __e2e__ prefix and delete each.
	if status, body, err := cli.do("GET", "/api/v1/memory?prefix=__e2e__&limit=100", nil); err == nil && status == 200 {
		if entries, err := decodeJSON[[]*store.MemoryEntry](body); err == nil {
			for _, e := range entries {
				cli.do("DELETE", "/api/v1/memory/"+url.PathEscape(e.Key), nil) //nolint:errcheck
			}
			fmt.Printf("[e2e sweep] deleted %d memory entries\n", len(entries))
		}
	}

	// Tasks: delete all tracked IDs (deferred cleanup may have already handled most).
	taskMu.Lock()
	ids := append([]string(nil), taskCleanup...)
	taskMu.Unlock()

	deleted := 0
	for _, id := range ids {
		if status, _, err := cli.do("DELETE", "/api/v1/tasks/"+id, nil); err == nil && status == 204 {
			deleted++
		}
	}
	fmt.Printf("[e2e sweep] deleted %d tasks\n", deleted)

	// Agents: no delete endpoint; __e2e__agent-* agents persist but are
	// harmless — they will be overwritten on the next test run with the same ID.
}
