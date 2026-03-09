# Hive-Server Detailed Build Plan

**Date:** 2026-03-09
**Status:** Master Build Plan
**Scope:** hive-server evolution from SQLite REST API to unified agent infrastructure

---

## Table of Contents

- [Phase 3A: Foundation (Existing Issues)](#phase-3a-foundation-existing-issues)
- [Phase 3B: CockroachDB Migration](#phase-3b-cockroachdb-migration)
- [Phase 4A: Search Integration (Meilisearch)](#phase-4a-search-integration-meilisearch)
- [Phase 4B: Knowledge Graph (Gel DB)](#phase-4b-knowledge-graph-gel-db)
- [Phase 4C: Agent Intelligence (MasterClaw)](#phase-4c-agent-intelligence-masterclaw)
- [Phase 5: Memory Injection System](#phase-5-memory-injection-system)
- [Phase 6: LLM-Enabled Project Manager](#phase-6-llm-enabled-project-manager)
- [Phase 7: Discovery API and Knowledge Graph Evolution](#phase-7-discovery-api-and-knowledge-graph-evolution)

---

## Design Decisions (Resolved Open Questions)

Before proceeding, these open questions from the vision document are resolved:

1. **Gel DB MUST NOT use CockroachDB as its PostgreSQL backend.** CockroachDB has ~40% PostgreSQL compatibility, and Gel's internal schema management, migration system, and EdgeQL compiler rely on PostgreSQL features (extensions, system catalogs, CREATE DOMAIN, etc.) that CockroachDB does not support. Gel must run against its own bundled PostgreSQL instance or a separate managed PostgreSQL. They are separate services.

2. **MasterClaw cost model:** Use a tiered approach. Simple decisions (round-robin assignment, rule-based priority) use Go logic. Complex decisions (task decomposition, capability-based assignment, progress evaluation) use LLM calls through MasterClaw. Add a `MASTERCLAW_ENABLED` toggle so the system works without MasterClaw (manual-only task management).

3. **CockroachDB-to-Meilisearch sync:** Asynchronous indexing is acceptable. Add a periodic reconciliation job (configurable interval, default 5 minutes) that re-indexes any CockroachDB records modified since last sync. This handles crash recovery and missed async events.

4. **Memory injection latency budget:** Target 200ms for the full injection pipeline. hive-local should cache recent injections with a 30-second TTL. MasterClaw synthesis is optional and adds latency; without it, raw ranked results are returned directly.

5. **CockroachDB licensing:** Enterprise Free is acceptable for christmas-island org (sub-$10M revenue). Telemetry is fine. Annual renewal is manageable.

---

## Dependency Graph

```
Phase 3A (Foundation):
  Step 3A.1 (rm k8s/)              -- independent
  Step 3A.2 (scripts)              -- independent
  Step 3A.3 (project layout, #20)  -- first structural change
  Step 3A.4 (store interface)      -- after 3A.3
  Step 3A.5 (Huma v2, #16)        -- after 3A.4
  Step 3A.6 (E2E test scaffold)   -- after 3A.5

Phase 3B (CockroachDB):
  Step 3B.1 (CRDB store + tx retries, #12 + #18) -- after 3A.5
  Step 3B.2 (goose migrations)                     -- after 3B.1
  Step 3B.3 (unit test updates, #13)               -- after 3B.1
  Step 3B.4 (ephemeral CRDB, #14)                  -- after 3B.1
  Step 3B.5 (healthz endpoint, #15)                -- after 3B.1

Phase 4A (Meilisearch):
  Step 4A.1 (search interface)     -- after 3A.4
  Step 4A.2 (meili implementation) -- after 4A.1
  Step 4A.3 (sync pipeline)        -- after 4A.2, 3B.1
  Step 4A.4 (search endpoints)     -- after 4A.2, 3A.5
  Step 4A.5 (reconciliation job)   -- after 4A.3

Phase 4B (Gel DB):
  Step 4B.1 (gel schema design)    -- after 3A.3
  Step 4B.2 (gel-go client)        -- after 4B.1
  Step 4B.3 (knowledge store)      -- after 4B.2
  Step 4B.4 (knowledge endpoints)  -- after 4B.3, 3A.5

Phase 4C (MasterClaw):
  Step 4C.1 (masterclaw client)    -- after 3A.3
  Step 4C.2 (synthesis service)    -- after 4C.1
  Step 4C.3 (k8s deployment)       -- after 4C.1

Phase 5 (Memory Injection):
  Step 5.1 (query router)          -- after 4A.2, 4B.3, 3B.1
  Step 5.2 (injection endpoint)    -- after 5.1, 4C.2
  Step 5.3 (injection logging)     -- after 5.2
  Step 5.4 (token budget mgmt)     -- after 5.2

Phase 6 (LLM Project Manager):
  Step 6.1 (subtask hierarchy)     -- after 3B.1
  Step 6.2 (task decomposition)    -- after 6.1, 4C.2
  Step 6.3 (intelligent assign)    -- after 6.2, 4B.3
  Step 6.4 (progress monitoring)   -- after 6.3

Phase 7 (Discovery API):
  Step 7.1 (discovery endpoints)   -- after 4B.3
  Step 7.2 (tool registry)         -- after 7.1
  Step 7.3 (agent/channel disc.)   -- after 7.1
  Step 7.4 (deep health)           -- after 4A.2, 4B.2, 3B.1
```

---

## Phase 3A: Foundation (Existing Issues)

This phase prepares the codebase for all subsequent work. It follows the locked dependency chain from the GitHub issues.

---

### Step 3A.1: Remove Scaffolded k8s/ Directory

**Size**: XS
**Prerequisites**: None
**Issue**: #10
**Files**:

- `k8s/` (delete entire directory)
- `.goreleaser.yaml` (remove any k8s references if present)
- `CLAUDE.md` (update structure section)

**Dependencies**: None

**Prompt**:

> Remove the `k8s/` directory from hive-server. This directory contains dead Kubernetes manifests inherited from the go-scaffold template. Deployment is managed externally in the ops/k8s repository.
>
> 1. Delete the entire `k8s/` directory and everything in it (deployment.yaml, service.yaml, ingress.yaml, hpa.yaml, pdb.yaml, serviceaccount.yaml, kustomization.yaml, README.md).
> 2. Check `.goreleaser.yaml` for any references to the k8s/ directory and remove them.
> 3. Update the `CLAUDE.md` file to remove the `k8s/` entry from the Structure section.
> 4. Do NOT create any new k8s manifests. The deployment manifests live in a separate repository.

**Verification**:

> - `ls k8s/` should fail (directory gone)
> - `go build ./...` still succeeds
> - `go test ./...` still passes

---

### Step 3A.2: Adopt scripts-to-rule-them-all Pattern

**Size**: S
**Prerequisites**: None
**Issue**: #11
**Files**:

- `script/bootstrap` (new)
- `script/setup` (new)
- `script/test` (new)
- `script/lint` (new)
- `script/build` (new)
- `script/server` (new)

**Dependencies**: None

**Prompt**:

> Implement the scripts-to-rule-them-all pattern for hive-server. Create a `script/` directory with the following executable shell scripts:
>
> **script/bootstrap**: Install Go dependencies and tools. Run `go mod download`. Install golangci-lint if not present. Install pre-commit if not present. Print success message.
>
> **script/setup**: Run bootstrap, then any one-time setup. For now this just calls `script/bootstrap`. Later it will set up local databases.
>
> **script/test**: Run `go test ./... -race -count=1`. Accept optional arguments to pass through to `go test` (e.g., `script/test ./internal/store/...`). Set a 2-minute timeout.
>
> **script/lint**: Run `golangci-lint run ./...`. Accept optional arguments.
>
> **script/build**: Run `go build -o bin/hive-server ./cmd/app/`. Create the `bin/` directory if it doesn't exist.
>
> **script/server**: Run `script/build` then exec `bin/hive-server serve`. Pass through any additional arguments.
>
> All scripts should:
>
> - Start with `#!/usr/bin/env bash`
> - Use `set -euo pipefail`
> - Use `cd "$(dirname "$0")/.."` to ensure they run from the project root
> - Be executable (chmod +x)
> - Print what they are doing with brief log messages
>
> Do NOT create `script/integration/test` yet -- that depends on the CockroachDB migration.

**Verification**:

> - `script/bootstrap` runs without error
> - `script/test` runs all tests and passes
> - `script/build` produces `bin/hive-server`
> - `script/lint` runs linting
> - `script/server` starts the server

---

### Step 3A.3: Refactor Project Layout to Go Standard Conventions

**Size**: M
**Prerequisites**: None (but best done after 3A.1)
**Issue**: #20
**Files**:

- `cmd/app/` -> `cmd/hive-server/` (rename)
- `internal/model/` (new package, extract data types)
- `internal/server/` (new package, extract HTTP server setup)
- `internal/search/` (new empty package with placeholder interface)
- `internal/knowledge/` (new empty package with placeholder interface)
- `internal/inject/` (new empty package with placeholder interface)
- `internal/masterclaw/` (new empty package with placeholder interface)
- All import paths updated
- `CLAUDE.md` updated
- `Dockerfile` updated
- `.goreleaser.yaml` updated

**Dependencies**: None

**Prompt**:

> Refactor hive-server's project layout to follow Go standard conventions and prepare the package structure for future phases. This is the first structural change in the dependency chain.
>
> **1. Rename cmd/app/ to cmd/hive-server/:**
>
> - Move `cmd/app/main.go` and `cmd/app/serve.go` to `cmd/hive-server/`
> - Update the Dockerfile `COPY` and build paths
> - Update `.goreleaser.yaml` to reference `cmd/hive-server/`
>
> **2. Extract internal/model/ package:**
>
> - Create `internal/model/model.go`
> - Move all data types from `internal/store/` into `internal/model/`:
>   - `MemoryEntry`, `MemoryFilter`
>   - `Task`, `TaskNote`, `TaskFilter`, `TaskUpdate`, `TaskStatus` and all status constants
>   - `Agent`, `AgentStatus` and all status constants
> - Move sentinel errors to `internal/model/errors.go`: `ErrNotFound`, `ErrConflict`, `ErrInvalidTransition`
> - Update `internal/store/` to import from `internal/model/` and use those types
> - Update `internal/handlers/` to import from `internal/model/` instead of `internal/store/` for type references
>
> **3. Extract internal/server/ package:**
>
> - Create `internal/server/server.go`
> - Move the HTTP server construction and lifecycle logic from `cmd/hive-server/serve.go` into this package
> - The `server.New()` function should accept a config struct and return `*http.Server`
> - The config struct holds: bind address, store, token, any future dependencies
> - Keep the cobra command in `cmd/hive-server/serve.go` but have it call `server.New()`
>
> **4. Create placeholder packages for future phases:**
>
> - `internal/search/search.go`: Define a `Searcher` interface with methods `Search`, `Index`, `Delete`, `Configure`. Add a no-op implementation `NoopSearcher` that returns empty results. Add a brief package doc comment explaining this will be implemented in Phase 4A.
> - `internal/knowledge/knowledge.go`: Define a `KnowledgeStore` interface with placeholder methods `Query`, `Relate`, `GetGraph`. Add `NoopKnowledgeStore`. Doc comment: Phase 4B.
> - `internal/inject/inject.go`: Define an `Injector` interface with a single `Inject(ctx, req) (resp, error)` method. Add `NoopInjector`. Doc comment: Phase 5.
> - `internal/masterclaw/client.go`: Define a `Client` interface with methods `Synthesize`, `DecomposeTask`, `DecideAssignment`. Add `NoopClient`. Doc comment: Phase 4C.
>
> **5. Update all import paths** across the codebase to reflect the new package locations.
>
> **6. Update CLAUDE.md** to reflect the new project structure.
>
> **7. Ensure the handlers.Store interface in `internal/handlers/handlers.go` now references `model.MemoryEntry`, `model.Task`, etc. from the new model package.**
>
> All existing tests must continue to pass with no behavior changes.

**Verification**:

> - `go build ./cmd/hive-server/` succeeds
> - `go test ./...` passes all existing tests
> - `go vet ./...` reports no issues
> - `docker build .` succeeds
> - The server starts and responds to health checks

---

### Step 3A.4: Formalize Store Interface

**Size**: S
**Prerequisites**: Step 3A.3
**Files**:

- `internal/store/store.go` (add formal interface definition)
- `internal/store/sqlite.go` (rename/refactor current implementation)
- `internal/store/memory.go`, `internal/store/tasks.go`, `internal/store/agents.go` (update receiver types)

**Dependencies**: None

**Prompt**:

> Formalize the Store interface in the store package, preparing for multiple backend implementations (SQLite now, CockroachDB in Phase 3B).
>
> **1. Define the Store interface in `internal/store/store.go`:**
>
> ```go
> // Store defines the contract for all persistence backends.
> type Store interface {
>     // Memory
>     UpsertMemory(ctx context.Context, entry *model.MemoryEntry) (*model.MemoryEntry, error)
>     GetMemory(ctx context.Context, key string) (*model.MemoryEntry, error)
>     ListMemory(ctx context.Context, f model.MemoryFilter) ([]*model.MemoryEntry, error)
>     DeleteMemory(ctx context.Context, key string) error
>     // Tasks
>     CreateTask(ctx context.Context, t *model.Task) (*model.Task, error)
>     GetTask(ctx context.Context, id string) (*model.Task, error)
>     ListTasks(ctx context.Context, f model.TaskFilter) ([]*model.Task, error)
>     UpdateTask(ctx context.Context, id string, upd model.TaskUpdate) (*model.Task, error)
>     DeleteTask(ctx context.Context, id string) error
>     // Agents
>     Heartbeat(ctx context.Context, id string, capabilities []string, status model.AgentStatus) (*model.Agent, error)
>     GetAgent(ctx context.Context, id string) (*model.Agent, error)
>     ListAgents(ctx context.Context) ([]*model.Agent, error)
>     // Lifecycle
>     Close() error
>     Ping(ctx context.Context) error
> }
> ```
>
> Note the addition of `Ping(ctx context.Context) error` for health checks.
>
> **2. Rename the current SQLite implementation:**
>
> - Rename the `Store` struct to `SQLiteStore` (or keep it as `Store` but ensure it explicitly implements the `Store` interface via a compile-time check: `var _ Store = (*SQLiteStore)(nil)`)
> - Keep the constructor as `NewSQLite(path string) (Store, error)` returning the interface
> - Add a `Ping` method that calls `s.db.PingContext(ctx)`
>
> **3. Update `internal/handlers/handlers.go`:**
>
> - The handlers `Store` interface should now match exactly the `store.Store` interface (or embed it). Since both are identical, you can import and use `store.Store` directly, or keep the local interface for test decoupling. The local interface in handlers is fine for test mocking -- keep it but add `Ping`.
>
> **4. Ensure compile-time interface checks:**
>
> - `var _ store.Store = (*store.SQLiteStore)(nil)` in store package
> - `var _ handlers.Store = (*store.SQLiteStore)(nil)` verified by the existing test imports
>
> All existing tests must pass with no behavior changes.

**Verification**:

> - `go build ./...` succeeds
> - `go test ./...` passes
> - `go vet ./...` clean
> - Compile-time interface assertion does not panic

---

### Step 3A.5: Add Huma v2 API Framework

**Size**: L
**Prerequisites**: Step 3A.4
**Issue**: #16
**Files**:

- `go.mod` (add huma dependency)
- `internal/handlers/handlers.go` (rewrite route setup with Huma)
- `internal/handlers/memory.go` (convert to Huma operations)
- `internal/handlers/tasks.go` (convert to Huma operations)
- `internal/handlers/agents.go` (convert to Huma operations)
- `internal/handlers/health.go` (new, extract health endpoints)
- `internal/handlers/handlers_test.go` (update test helpers)
- `internal/handlers/memory_test.go` (update for Huma)
- `internal/handlers/tasks_test.go` (update for Huma)
- `internal/handlers/agents_test.go` (update for Huma)
- `internal/server/server.go` (update to use Huma)

**Dependencies**:

- `github.com/danielgtaylor/huma/v2`

**Prompt**:

> Migrate hive-server from raw chi handlers to Huma v2 for OpenAPI generation. Huma sits on top of chi, so the existing chi router remains as the underlying mux. All 12 existing endpoints must be converted.
>
> **1. Add Huma v2 dependency:**
>
> ```
> go get github.com/danielgtaylor/huma/v2
> ```
>
> **2. Create Huma API setup in `internal/handlers/handlers.go`:**
>
> Replace the current `routes()` method with Huma operation registration. Use `humachi.New()` to create a Huma API on top of the chi router:
>
> ```go
> import (
>     "github.com/danielgtaylor/huma/v2"
>     "github.com/danielgtaylor/huma/v2/adapters/humachi"
> )
>
> func New(s Store, token string) http.Handler {
>     a := &API{store: s, token: token}
>     r := chi.NewRouter()
>     r.Use(middleware.RequestID)
>     r.Use(middleware.Recoverer)
>     r.Use(a.authMiddleware)
>
>     api := humachi.New(r, huma.DefaultConfig("Hive Server API", "1.0.0"))
>     a.registerRoutes(api)
>
>     // Health endpoints outside Huma (no auth, no OpenAPI)
>     r.Get("/health", a.handleHealth)
>     r.Get("/ready", a.handleReady)
>
>     return r
> }
> ```
>
> **3. Convert each endpoint to a Huma operation.** For each endpoint, define input/output structs with Huma tags and register with `huma.Register()`. Example for memory upsert:
>
> ```go
> type UpsertMemoryInput struct {
>     Body struct {
>         Key     string   `json:"key" doc:"Memory entry key" minLength:"1"`
>         Value   string   `json:"value" doc:"Memory entry value"`
>         Tags    []string `json:"tags,omitempty" doc:"Tags for categorization"`
>         Version int      `json:"version,omitempty" doc:"Version for optimistic concurrency"`
>     }
> }
>
> type MemoryOutput struct {
>     Body *model.MemoryEntry
> }
>
> huma.Register(api, huma.Operation{
>     OperationID: "upsert-memory",
>     Method:      http.MethodPost,
>     Path:        "/api/v1/memory",
>     Summary:     "Create or update a memory entry",
>     Tags:        []string{"Memory"},
> }, a.handleMemoryUpsert)
> ```
>
> **4. Convert all 12 endpoints:**
>
> - POST /api/v1/memory (upsert)
> - GET /api/v1/memory (list, with query params: tag, agent, prefix, limit, offset)
> - GET /api/v1/memory/{key} (get)
> - DELETE /api/v1/memory/{key} (delete)
> - POST /api/v1/tasks (create)
> - GET /api/v1/tasks (list, with query params: status, assignee, creator, limit, offset)
> - GET /api/v1/tasks/{id} (get)
> - PATCH /api/v1/tasks/{id} (update)
> - DELETE /api/v1/tasks/{id} (delete)
> - POST /api/v1/agents/{id}/heartbeat (heartbeat)
> - GET /api/v1/agents (list)
> - GET /api/v1/agents/{id} (get)
>
> **5. Preserve the auth middleware.** The `authMiddleware` stays as chi middleware wrapping all routes. The `X-Agent-ID` header injection into context also remains unchanged. Huma operations receive the context with agent_id already set.
>
> **6. Add OpenAPI spec endpoint.** Huma automatically serves the OpenAPI spec. Ensure it is accessible at `/openapi.json` or the Huma default path. Optionally add `/docs` for Swagger UI using Huma's built-in support.
>
> **7. Update all handler tests.** Tests currently use `httptest.NewRecorder()` and direct handler calls. They need to be updated to work with Huma's operation-based routing. The test pattern should use `humatest` or continue using `httptest` against the full router. Keep the mock-based Store pattern.
>
> **8. Ensure the OpenAPI spec is valid.** After conversion, fetch `/openapi.json` and validate it is well-formed. All operations should have proper summaries, tags, and input/output schemas.
>
> The API behavior (request/response formats, status codes, error messages) must remain backward-compatible. No changes to the external contract.

**Verification**:

> - `go test ./...` passes all tests
> - `curl http://localhost:8080/openapi.json` returns a valid OpenAPI 3.1 spec
> - All 12 endpoints return the same responses as before
> - `curl http://localhost:8080/health` returns `{"status":"ok"}`
> - OpenAPI spec lists all 12 operations with correct paths, methods, and schemas

---

### Step 3A.6: E2E Smoke Test Scaffold

**Size**: S
**Prerequisites**: Step 3A.5
**Issue**: #17
**Files**:

- `test/e2e/e2e_test.go` (new)
- `test/e2e/helpers_test.go` (new)

**Dependencies**: None new

**Prompt**:

> Create the E2E smoke test scaffold for hive-server. These tests run against a live server instance and verify the full request/response cycle.
>
> **1. Create `test/e2e/e2e_test.go`:**
>
> - Use build tag `//go:build e2e` so they do not run with normal `go test`
> - Define a `TestMain` that:
>   - Reads `HIVE_SERVER_URL` from environment (default: `http://localhost:8080`)
>   - Reads `HIVE_TOKEN` from environment (default: empty for no-auth dev mode)
>   - Generates a unique `runID` using a UUID prefix `__e2e__{uuid}__`
>   - All test data keys/names are prefixed with `runID` for namespace isolation
>   - Runs tests
>   - Cleanup: delete all test data using the `runID` prefix
>
> **2. Create `test/e2e/helpers_test.go`:**
>
> - Helper functions: `doRequest(method, path, body) (*http.Response, error)`
> - JSON encode/decode helpers
> - Assertion helpers for status codes and response bodies
>
> **3. Write basic smoke tests:**
>
> - `TestHealthEndpoints`: GET /health, GET /ready both return 200
> - `TestMemoryCRUD`: Create, read, list, update (version bump), delete a memory entry
> - `TestTaskCRUD`: Create, read, list, update status through state machine, delete
> - `TestAgentHeartbeat`: Send heartbeat, verify agent appears in list, verify status
> - `TestMemoryOptimisticConcurrency`: Create entry, attempt update with wrong version, verify 409 conflict
> - `TestTaskStateTransitions`: Verify valid transitions succeed and invalid transitions fail
>
> **4. Add `script/e2e` script:**
>
> - Starts the server in the background (or uses existing server if `HIVE_SERVER_URL` is set)
> - Runs `go test -tags e2e -count=1 ./test/e2e/...`
> - Stops the background server on exit (if started)

**Verification**:

> - `go test -tags e2e ./test/e2e/...` passes against a running local server
> - Tests are properly namespaced (no data pollution)
> - Tests clean up after themselves

---

## Phase 3B: CockroachDB Migration

This phase implements the CockroachDB store backend, replacing SQLite for production use.

---

### Step 3B.1: Implement CockroachDB Store with Transaction Retries

**Size**: L
**Prerequisites**: Step 3A.5
**Issues**: #12, #18
**Files**:

- `go.mod` (add pgx, cockroach-go, goose dependencies)
- `internal/store/crdb.go` (new -- CockroachDB store implementation)
- `internal/store/crdb_memory.go` (new -- memory CRUD)
- `internal/store/crdb_tasks.go` (new -- task CRUD with state machine)
- `internal/store/crdb_agents.go` (new -- agent heartbeat/listing)
- `internal/store/retry.go` (new -- RetryTx helper)
- `internal/store/migrations/` (new directory)
- `internal/store/migrations/001_initial_schema.sql` (new)
- `internal/store/embed.go` (new -- embed migrations)
- `cmd/hive-server/serve.go` (add --database-url flag and CRDB backend selection)

**Dependencies**:

- `github.com/jackc/pgx/v5`
- `github.com/jackc/pgx/v5/pgxpool`
- `github.com/cockroachdb/cockroach-go/v2/crdb/crdbpgx`
- `github.com/pressly/goose/v3`

**Prompt**:

> Implement the CockroachDB store backend for hive-server. This is the largest single step and ships together with transaction retry logic (#18).
>
> **1. Add dependencies:**
>
> ```
> go get github.com/jackc/pgx/v5
> go get github.com/cockroachdb/cockroach-go/v2
> go get github.com/pressly/goose/v3
> ```
>
> **2. Create `internal/store/migrations/001_initial_schema.sql`:**
> Use the schema from the CockroachDB brief, with goose annotations:
>
> ```sql
> -- +goose Up
> CREATE TABLE IF NOT EXISTS memory (
>     key         TEXT        NOT NULL PRIMARY KEY,
>     value       TEXT        NOT NULL DEFAULT '',
>     agent_id    TEXT        NOT NULL DEFAULT '',
>     tags        JSONB       NOT NULL DEFAULT '[]'::JSONB,
>     version     INT8        NOT NULL DEFAULT 1,
>     created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
>     updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
> );
> -- (full schema for tasks, task_notes, agents with indexes)
>
> -- +goose Down
> DROP TABLE IF EXISTS task_notes;
> DROP TABLE IF EXISTS tasks;
> DROP TABLE IF EXISTS memory;
> DROP TABLE IF EXISTS agents;
> ```
>
> **3. Create `internal/store/embed.go`:**
>
> ```go
> package store
>
> import "embed"
>
> //go:embed migrations/*.sql
> var Migrations embed.FS
> ```
>
> **4. Create `internal/store/retry.go`:**
> Implement a `RetryTx` helper wrapping `crdbpgx.ExecuteTx`:
>
> ```go
> func RetryTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
>     return crdbpgx.ExecuteTx(ctx, pool, pgx.TxOptions{}, fn)
> }
> ```
>
> Also add a standalone retry helper with configurable max retries (default 5), exponential backoff with jitter (base 50ms, max 5s). Log retry attempts.
>
> **5. Create `internal/store/crdb.go`:**
>
> - `CRDBStore` struct holding `*pgxpool.Pool`
> - `NewCRDB(ctx context.Context, connString string) (Store, error)`:
>   - Parse config with `pgxpool.ParseConfig`
>   - Set pool parameters: MaxConns=25, MinConns=5, MaxConnLifetime=1h, HealthCheckPeriod=1m
>   - Create pool, ping to verify connection
>   - Run goose migrations (using embedded SQL files)
>   - Return `Store` interface
> - `Close()` method
> - `Ping(ctx)` method using `pool.Ping(ctx)`
> - Compile-time check: `var _ Store = (*CRDBStore)(nil)`
>
> **6. Create `internal/store/crdb_memory.go`:**
> Implement all memory operations using pgx:
>
> - `UpsertMemory`: Use `RetryTx` wrapping INSERT/UPDATE with optimistic concurrency. Use `$1, $2, ...` placeholders. Tags stored as JSONB. Timestamps are native `TIMESTAMPTZ` (scan directly to `time.Time`).
> - `GetMemory`: Simple SELECT by key. Return `model.ErrNotFound` if no rows.
> - `ListMemory`: Build query dynamically based on filter (tag filtering uses `tags @> $N::JSONB`, prefix uses `key LIKE $N`, agent uses `agent_id = $N`). Support limit/offset.
> - `DeleteMemory`: DELETE by key. Return `model.ErrNotFound` if no rows affected.
>
> **7. Create `internal/store/crdb_tasks.go`:**
> Implement all task operations:
>
> - `CreateTask`: INSERT with `gen_random_uuid()` for ID. Validate required fields. Use `RetryTx`.
> - `GetTask`: SELECT task + LEFT JOIN task_notes. Scan notes into task struct.
> - `ListTasks`: Dynamic query with status/assignee/creator filters. Support limit/offset.
> - `UpdateTask`: Use `RetryTx`. Enforce state machine transitions (same validation as SQLite store). Optionally append note.
> - `DeleteTask`: DELETE with cascade (task_notes deleted via FK).
>
> **8. Create `internal/store/crdb_agents.go`:**
> Implement agent operations:
>
> - `Heartbeat`: UPSERT (INSERT ON CONFLICT UPDATE). Capabilities stored as JSONB.
> - `GetAgent`: SELECT by ID. Mark offline if heartbeat > 5 minutes ago.
> - `ListAgents`: SELECT all. Mark offline for stale heartbeats.
>
> **9. Update `cmd/hive-server/serve.go`:**
> Add `--database-url` flag and `DATABASE_URL` env var. Selection logic:
>
> ```go
> dbURL := os.Getenv("DATABASE_URL")
> if dbURL == "" {
>     // Default to SQLite
>     s, err = store.NewSQLite(dbPath)
> } else {
>     s, err = store.NewCRDB(ctx, dbURL)
> }
> ```
>
> **Key differences from SQLite implementation:**
>
> - Placeholders: `$1, $2, ...` instead of `?`
> - Timestamps: native `time.Time` scan instead of string parsing
> - Tags: JSONB with `@>` operator instead of `json_each()`
> - IDs: `gen_random_uuid()` in DB instead of `uuid.New()` in Go (though Go-side UUID generation is also acceptable)
> - Transactions: `RetryTx` wrapper for all writes
> - Connection pool instead of single connection
>
> **Important:** Every transaction function passed to `RetryTx` must be idempotent. Do not perform side effects (HTTP calls, logging state changes) inside the transaction function.

**Verification**:

> - `go build ./...` succeeds
> - Unit tests pass (SQLite store tests unchanged)
> - Manual test against local CockroachDB:
>
>   ```
>   cockroach start-single-node --insecure --store=type=mem
>   cockroach sql --insecure -e "CREATE DATABASE hive"
>   DATABASE_URL="postgresql://root@localhost:26257/hive?sslmode=disable" go run ./cmd/hive-server/ serve
>   ```
>
> - All CRUD operations work via curl against CockroachDB backend
> - Migrations run on startup without error

---

### Step 3B.2: CockroachDB Store Unit Tests

**Size**: M
**Prerequisites**: Step 3B.1
**Issue**: #13
**Files**:

- `internal/store/crdb_test.go` (new)
- `internal/store/crdb_memory_test.go` (new)
- `internal/store/crdb_tasks_test.go` (new)
- `internal/store/crdb_agents_test.go` (new)
- `internal/store/testutil_test.go` (new -- shared test setup)

**Dependencies**: None new

**Prompt**:

> Write unit tests for the CockroachDB store implementation. These tests use a build tag so they only run when a CockroachDB instance is available.
>
> **1. Create `internal/store/testutil_test.go`:**
>
> - Use build tag `//go:build crdb`
> - Define a `testCRDBStore(t *testing.T) Store` helper that:
>   - Reads `TEST_CRDB_URL` from environment (default: `postgresql://root@localhost:26257/hive_test?sslmode=disable`)
>   - Creates a fresh database or schema per test (use `t.Name()` as suffix)
>   - Returns a `Store` connected to that database
>   - Registers `t.Cleanup()` to drop the test database and close the store
>
> **2. Port all existing SQLite store tests to CRDB:**
> Mirror the test structure from the existing `*_test.go` files, but use the CRDB store. Tests should cover:
>
> - Memory: upsert, get, list (all filter combinations), delete, optimistic concurrency conflict
> - Tasks: create, get, list (all filter combinations), update with valid transitions, update with invalid transitions, delete, notes
> - Agents: heartbeat (create and update), get, list, offline detection
>
> **3. Add CRDB-specific tests:**
>
> - Transaction retry: simulate a serialization conflict (two concurrent updates to the same key) and verify the retry logic resolves it
> - JSONB tag queries: verify `tags @> '["tag"]'` filtering works correctly
> - TIMESTAMPTZ handling: verify timestamps round-trip correctly (no timezone drift)
>
> **4. Add `script/test-crdb` script:**
>
> ```bash
> #!/usr/bin/env bash
> set -euo pipefail
> cd "$(dirname "$0")/.."
> go test -tags crdb -race -count=1 ./internal/store/...
> ```

**Verification**:

> - `go test -tags crdb ./internal/store/...` passes against local CockroachDB
> - `go test ./internal/store/...` (without tag) still runs only SQLite tests
> - Handler tests (mock-based) are unaffected

---

### Step 3B.3: Ephemeral CockroachDB for Integration Tests

**Size**: S
**Prerequisites**: Step 3B.1
**Issue**: #14
**Files**:

- `go.mod` (add cockroach-go testserver)
- `internal/store/crdb_integration_test.go` (new)
- `script/integration/test` (new)

**Dependencies**:

- `github.com/cockroachdb/cockroach-go/v2/testserver`

**Prompt**:

> Set up ephemeral CockroachDB instances for integration testing using `cockroach-go/v2/testserver`. This allows CI to run CRDB tests without a persistent database.
>
> **1. Add testserver dependency:**
>
> ```
> go get github.com/cockroachdb/cockroach-go/v2/testserver
> ```
>
> **2. Create `internal/store/crdb_integration_test.go`:**
>
> - Use build tag `//go:build integration`
> - In `TestMain`, start an ephemeral CockroachDB:
>
>   ```go
>   ts, err := testserver.NewTestServer()
>   if err != nil {
>       // Skip if cockroach binary not available
>       fmt.Println("cockroach binary not found, skipping integration tests")
>       os.Exit(0)
>   }
>   defer ts.Stop()
>   testDBURL = ts.PGURL().String()
>   ```
>
> - Run the same test suite as the CRDB unit tests but against the ephemeral instance
> - Use a helper that creates the `hive_test` database on the ephemeral server
>
> **3. Create `script/integration/test`:**
>
> ```bash
> #!/usr/bin/env bash
> set -euo pipefail
> cd "$(dirname "$0")/../.."
>
> echo "==> Running integration tests with ephemeral CockroachDB..."
> go test -tags integration -race -count=1 -timeout 5m ./internal/store/...
> ```
>
> **4. Update CI workflow (`.github/workflows/ci.yaml`):**
> Add an integration test job that runs `script/integration/test`. This job can use the cockroach binary from `cockroachdb/cockroach` Docker image or install it directly.

**Verification**:

> - `script/integration/test` spins up ephemeral CRDB, runs tests, and tears down
> - Tests pass in CI without a persistent CockroachDB
> - No leftover processes or temp files after tests complete

---

### Step 3B.4: Deep Health Check Endpoint

**Size**: XS
**Prerequisites**: Step 3B.1
**Issue**: #15 (partial)
**Files**:

- `internal/handlers/health.go` (update)
- `internal/handlers/health_test.go` (new or update)

**Dependencies**: None new

**Prompt**:

> Add a `/healthz` deep health check endpoint that pings the database backend.
>
> **1. Add `/healthz` endpoint in the health handler:**
> This endpoint is registered outside the auth middleware (like /health and /ready). It calls `store.Ping(ctx)` to verify the database connection.
>
> ```go
> func (a *API) handleHealthz(w http.ResponseWriter, r *http.Request) {
>     ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
>     defer cancel()
>
>     if err := a.store.Ping(ctx); err != nil {
>         w.WriteHeader(http.StatusServiceUnavailable)
>         json.NewEncoder(w).Encode(map[string]string{
>             "status": "unhealthy",
>             "error":  err.Error(),
>         })
>         return
>     }
>     json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
> }
> ```
>
> **2. Register the endpoint outside the auth middleware** in the router setup (same as /health and /ready).
>
> **3. Write tests:**
>
> - Test with healthy store mock -> 200
> - Test with failing Ping -> 503
>
> **4. Update the Huma OpenAPI spec** to include the /healthz endpoint (or keep it outside Huma since it has no auth).

**Verification**:

> - `curl http://localhost:8080/healthz` returns `{"status":"healthy"}` when DB is up
> - Returns 503 when DB is down
> - `go test ./internal/handlers/...` passes

---

## Phase 4A: Search Integration (Meilisearch)

This phase adds full-text search capabilities via Meilisearch as a secondary index.

---

### Step 4A.1: Define Search Interface and Types

**Size**: S
**Prerequisites**: Step 3A.4
**Files**:

- `internal/search/search.go` (update placeholder from 3A.3)
- `internal/search/types.go` (new)
- `internal/search/noop.go` (new or update)

**Dependencies**: None

**Prompt**:

> Flesh out the search interface and types that were stubbed in Step 3A.3. These types define the contract for all search backend implementations.
>
> **1. Update `internal/search/search.go`:**
>
> ```go
> package search
>
> import "context"
>
> // Searcher defines the contract for search backends.
> type Searcher interface {
>     // Search performs a full-text search against an index.
>     Search(ctx context.Context, index string, req SearchRequest) (*SearchResponse, error)
>
>     // Index adds or updates documents in a search index.
>     // This is asynchronous -- the documents may not be immediately searchable.
>     Index(ctx context.Context, index string, documents []Document) error
>
>     // Delete removes documents from a search index by their IDs.
>     Delete(ctx context.Context, index string, ids []string) error
>
>     // EnsureIndex creates an index if it does not exist and applies settings.
>     EnsureIndex(ctx context.Context, index string, settings IndexSettings) error
>
>     // Healthy returns nil if the search backend is reachable.
>     Healthy(ctx context.Context) error
> }
> ```
>
> **2. Create `internal/search/types.go`:**
> Define the request/response types:
>
> - `SearchRequest`: Query string, Filters (map[string]string), Sort ([]string), Limit, Offset, AgentID (for scoping), Facets ([]string)
> - `SearchResponse`: Hits ([]SearchHit), TotalHits int, ProcessingTimeMs int
> - `SearchHit`: ID string, Score float64, Fields map[string]interface{}, Highlights map[string]string
> - `Document`: map[string]interface{} (flexible document format)
> - `IndexSettings`: SearchableAttributes []string, FilterableAttributes []string, SortableAttributes []string, Synonyms map[string][]string
>
> **3. Update `internal/search/noop.go`:**
> Update the NoopSearcher to match the new interface. All methods return empty results and nil errors. `Search` returns an empty `SearchResponse`.

**Verification**:

> - `go build ./...` succeeds
> - `go vet ./...` clean
> - Noop implementation satisfies the interface (compile-time check)

---

### Step 4A.2: Implement Meilisearch Backend

**Size**: M
**Prerequisites**: Step 4A.1
**Files**:

- `go.mod` (add meilisearch-go)
- `internal/search/meilisearch.go` (new)
- `internal/search/meilisearch_test.go` (new)

**Dependencies**:

- `github.com/meilisearch/meilisearch-go`

**Prompt**:

> Implement the Meilisearch backend for the Searcher interface.
>
> **1. Add dependency:**
>
> ```
> go get github.com/meilisearch/meilisearch-go
> ```
>
> **2. Create `internal/search/meilisearch.go`:**
>
> ```go
> type MeiliSearcher struct {
>     client meilisearch.ServiceManager
> }
>
> func NewMeili(host, apiKey string) *MeiliSearcher {
>     client := meilisearch.New(host, meilisearch.WithAPIKey(apiKey))
>     return &MeiliSearcher{client: client}
> }
> ```
>
> Implement all Searcher methods:
>
> - **Search**: Translate `SearchRequest` to `meilisearch.SearchRequest`. Build filter string from Filters map and AgentID (e.g., `"agent_id = 'agent-1' AND status = 'open'"`). Set ShowRankingScore=true. Map response hits to `SearchHit` structs.
> - **Index**: Call `client.Index(index).AddDocuments(documents)`. Use `WaitForTask` with a 30-second timeout. Log but do not fail on indexing errors (async best-effort pattern).
> - **Delete**: Call `client.Index(index).DeleteDocuments(ids)`.
> - **EnsureIndex**: Call `client.CreateIndex()` (idempotent if exists). Then call `UpdateSettings()` with the provided `IndexSettings` mapped to `meilisearch.Settings`.
> - **Healthy**: Call `client.Health()`.
>
> **3. Add compile-time check:** `var _ Searcher = (*MeiliSearcher)(nil)`
>
> **4. Create `internal/search/meilisearch_test.go`:**
> Use the meilisearch-go mock package (`github.com/meilisearch/meilisearch-go/mocks`) for unit tests:
>
> - Test Search with mock response -> correct mapping to SearchHit
> - Test Index -> correct documents passed to mock
> - Test Delete -> correct IDs passed
> - Test EnsureIndex -> CreateIndex + UpdateSettings called
> - Test Healthy -> delegates to mock Health
> - Test Search with AgentID filter -> verify filter string includes agent_id
>
> **5. Handle the 10-word query limit:**
> Before sending the query to Meilisearch, truncate to the 10 most significant words. Strip common stop words first, then take the first 10 remaining words. Log a warning if truncation occurs.

**Verification**:

> - `go test ./internal/search/...` passes (mock-based)
> - `go build ./...` succeeds
> - Manual test against local Meilisearch:
>
>   ```
>   docker run -d -p 7700:7700 -e MEILI_ENV=development getmeili/meilisearch:v1.12
>   ```

---

### Step 4A.3: Search Sync Pipeline (Store -> Meilisearch)

**Size**: M
**Prerequisites**: Steps 4A.2, 3B.1
**Files**:

- `internal/search/sync.go` (new)
- `internal/search/sync_test.go` (new)
- `internal/store/crdb_memory.go` (update -- add sync hooks)
- `internal/store/crdb_tasks.go` (update -- add sync hooks)
- `cmd/hive-server/serve.go` (update -- wire up search sync)

**Dependencies**: None new

**Prompt**:

> Implement the sync pipeline that keeps Meilisearch in sync with the primary store (CockroachDB or SQLite).
>
> **1. Create `internal/search/sync.go`:**
>
> Define a `SyncService` that bridges the store and search:
>
> ```go
> type SyncService struct {
>     searcher Searcher
>     logger   *slog.Logger
> }
>
> func NewSyncService(searcher Searcher, logger *slog.Logger) *SyncService { ... }
>
> // IndexMemory converts a MemoryEntry to a Document and indexes it.
> func (s *SyncService) IndexMemory(ctx context.Context, entry *model.MemoryEntry) {
>     doc := Document{
>         "id":         entry.Key,
>         "content":    entry.Value,
>         "agent_id":   entry.AgentID,
>         "tags":       entry.Tags,
>         "created_at": entry.CreatedAt.Unix(),
>         "updated_at": entry.UpdatedAt.Unix(),
>     }
>     if err := s.searcher.Index(ctx, "memories", []Document{doc}); err != nil {
>         s.logger.Error("failed to index memory in search", "key", entry.Key, "error", err)
>     }
> }
>
> // IndexTask converts a Task to a Document and indexes it.
> func (s *SyncService) IndexTask(ctx context.Context, task *model.Task) { ... }
>
> // DeleteMemory removes a memory entry from the search index.
> func (s *SyncService) DeleteMemory(ctx context.Context, key string) { ... }
>
> // DeleteTask removes a task from the search index.
> func (s *SyncService) DeleteTask(ctx context.Context, id string) { ... }
> ```
>
> **2. Create a `SyncStore` wrapper** that wraps any `Store` and adds sync hooks:
>
> ```go
> type SyncStore struct {
>     store.Store
>     sync *SyncService
> }
>
> func NewSyncStore(s store.Store, sync *SyncService) *SyncStore { ... }
>
> func (ss *SyncStore) UpsertMemory(ctx context.Context, entry *model.MemoryEntry) (*model.MemoryEntry, error) {
>     result, err := ss.Store.UpsertMemory(ctx, entry)
>     if err != nil {
>         return nil, err
>     }
>     // Async index in Meilisearch (fire-and-forget)
>     go ss.sync.IndexMemory(context.Background(), result)
>     return result, nil
> }
> // Similar wrappers for CreateTask, UpdateTask, DeleteMemory, DeleteTask
> ```
>
> This wrapper pattern means the sync logic does not pollute the store implementations.
>
> **3. Wire up in `cmd/hive-server/serve.go`:**
> If `MEILI_URL` is set:
>
> - Create `MeiliSearcher`
> - Ensure indexes exist with appropriate settings on startup
> - Wrap the store with `SyncStore`
> - Pass the searcher to handlers (for search endpoints in Step 4A.4)
>
> If `MEILI_URL` is not set:
>
> - Use `NoopSearcher`
> - Do not wrap the store (no sync overhead)
>
> **4. Index settings on startup:**
> Define default settings for the `memories` and `tasks` indexes:
>
> - memories: searchable=[content, tags], filterable=[agent_id, scope, created_at], sortable=[created_at, updated_at]
> - tasks: searchable=[title, description, tags], filterable=[status, assignee, creator, priority, created_at], sortable=[created_at, priority, updated_at]

**Verification**:

> - `go test ./internal/search/...` passes
> - Manual test: start server with Meilisearch, create a memory, verify it appears in Meilisearch via `curl http://localhost:7700/indexes/memories/documents`
> - Create/update/delete operations are reflected in Meilisearch within a few seconds

---

### Step 4A.4: Search Endpoints

**Size**: M
**Prerequisites**: Steps 4A.2, 3A.5
**Files**:

- `internal/handlers/search.go` (new)
- `internal/handlers/search_test.go` (new)
- `internal/handlers/handlers.go` (update -- add Searcher dependency to API struct)

**Dependencies**: None new

**Prompt**:

> Add search endpoints to the Huma v2 API. These endpoints delegate to the Searcher interface.
>
> **1. Update `internal/handlers/handlers.go`:**
> Add a `Searcher` field to the `API` struct:
>
> ```go
> type API struct {
>     store    Store
>     searcher search.Searcher
>     token    string
> }
> ```
>
> Update `New()` to accept a `search.Searcher` parameter. If nil, use `search.NoopSearcher`.
>
> **2. Create `internal/handlers/search.go`:**
> Register two new Huma operations:
>
> **POST /api/v1/memory/search**:
>
> ```go
> type MemorySearchInput struct {
>     Body struct {
>         Query   string   `json:"q" doc:"Search query" minLength:"1"`
>         Tags    []string `json:"tags,omitempty" doc:"Filter by tags"`
>         Scope   string   `json:"scope,omitempty" doc:"Filter by scope (private, project, global)"`
>         Limit   int      `json:"limit,omitempty" doc:"Maximum results" minimum:"1" maximum:"100" default:"20"`
>         Offset  int      `json:"offset,omitempty" doc:"Result offset" minimum:"0" default:"0"`
>     }
> }
>
> type SearchOutput struct {
>     Body *search.SearchResponse
> }
> ```
>
> Handler: Extract agent_id from context. Build `SearchRequest` with agent_id filter. Call `searcher.Search(ctx, "memories", req)`. Return results.
>
> **POST /api/v1/tasks/search**:
> Same pattern but searching the `tasks` index. Filters: status, assignee, creator, priority.
>
> **3. Graceful degradation:**
> If the searcher is NoopSearcher or returns an error, return a 503 with a clear error message: `{"error": "search service unavailable"}`. Do NOT fall back to database LIKE queries in the handler -- that is a future enhancement for the query router (Phase 5).
>
> **4. Create `internal/handlers/search_test.go`:**
> Define a `MockSearcher` that implements the `search.Searcher` interface for testing. Write tests:
>
> - Search with results -> 200 with hits
> - Search with no results -> 200 with empty hits
> - Search with unavailable backend -> 503
> - Agent ID scoping: verify the agent_id from context is passed as a filter
> - Invalid request (empty query) -> 422

**Verification**:

> - `go test ./internal/handlers/...` passes
> - Manual test with Meilisearch running:
>
>   ```
>   curl -X POST http://localhost:8080/api/v1/memory/search \
>     -H "Content-Type: application/json" \
>     -d '{"q": "deployment"}'
>   ```
>
> - OpenAPI spec at /openapi.json includes the new search operations

---

### Step 4A.5: Search Reconciliation Job

**Size**: S
**Prerequisites**: Step 4A.3
**Files**:

- `internal/search/reconcile.go` (new)
- `internal/search/reconcile_test.go` (new)
- `cmd/hive-server/serve.go` (update -- start reconciliation goroutine)

**Dependencies**: None new

**Prompt**:

> Implement a periodic reconciliation job that ensures Meilisearch stays in sync with the primary store.
>
> **1. Create `internal/search/reconcile.go`:**
>
> ```go
> type Reconciler struct {
>     store    store.Store
>     searcher Searcher
>     sync     *SyncService
>     interval time.Duration
>     logger   *slog.Logger
> }
>
> func NewReconciler(s store.Store, searcher Searcher, sync *SyncService, interval time.Duration, logger *slog.Logger) *Reconciler { ... }
>
> // Run starts the periodic reconciliation loop. Blocks until ctx is cancelled.
> func (r *Reconciler) Run(ctx context.Context) {
>     ticker := time.NewTicker(r.interval)
>     defer ticker.Stop()
>     for {
>         select {
>         case <-ctx.Done():
>             return
>         case <-ticker.C:
>             r.reconcile(ctx)
>         }
>     }
> }
>
> func (r *Reconciler) reconcile(ctx context.Context) {
>     // 1. List all memories from store
>     // 2. Re-index each into Meilisearch (idempotent upsert)
>     // 3. List all tasks from store
>     // 4. Re-index each into Meilisearch
>     // Log errors but do not fail
> }
> ```
>
> Note: For the initial implementation, this does a full re-index. This is simple and correct. For large datasets, a future optimization would track `updated_at` timestamps and only re-index changed records since last reconciliation.
>
> **2. Configuration:**
>
> - `MEILI_RECONCILE_INTERVAL` env var (default: "5m")
> - Set to "0" or empty to disable reconciliation
>
> **3. Wire up in `cmd/hive-server/serve.go`:**
> Start the reconciler in a goroutine when Meilisearch is configured. Cancel via the server's shutdown context.
>
> **4. Write tests:**
>
> - Test that reconcile calls Index for all store entries
> - Test that reconcile handles store errors gracefully (logs, continues)
> - Test that reconcile handles searcher errors gracefully

**Verification**:

> - Server starts without error with reconciliation enabled
> - Logs show periodic reconciliation running
> - After deleting Meilisearch data and waiting for reconciliation interval, data reappears

---

## Phase 4B: Knowledge Graph (Gel DB)

This phase adds the graph-relational knowledge store using Gel DB (formerly EdgeDB).

---

### Step 4B.1: Gel DB Schema Design

**Size**: S
**Prerequisites**: Step 3A.3
**Files**:

- `dbschema/default.esdl` (new)
- `dbschema/migrations/` (new, empty initially -- generated by Gel CLI)
- `docker-compose.yml` (new or update -- add Gel service)

**Dependencies**: None (Go deps added in 4B.2)

**Prompt**:

> Design and create the Gel DB schema for the hive-server knowledge graph. Gel DB runs as a separate service with its own bundled PostgreSQL -- it does NOT use CockroachDB as its backend.
>
> **1. Create `dbschema/default.esdl`:**
> This is the EdgeQL schema definition. Define the following types:
>
> ```sdl
> module default {
>
>     abstract type HasTimestamps {
>         required created_at: datetime {
>             default := datetime_current();
>             readonly := true;
>         };
>         required updated_at: datetime {
>             default := datetime_current();
>         };
>     }
>
>     # Agents in the knowledge graph (mirrors CockroachDB agents but with relationships)
>     type Agent extending HasTimestamps {
>         required agent_id: str {
>             constraint exclusive;
>         };
>         required name: str;
>         multi capabilities: str;
>         multi projects: Project;
>         multi tools: Tool;
>     }
>
>     # Projects as organizational units
>     type Project extending HasTimestamps {
>         required name: str {
>             constraint exclusive;
>         };
>         description: str;
>         multi repositories: Repository;
>         multi agents := .<projects[is Agent];
>     }
>
>     # Code repositories
>     type Repository extending HasTimestamps {
>         required url: str {
>             constraint exclusive;
>         };
>         required name: str;
>         language: str;
>         multi dependencies: Repository;
>         required project: Project;
>     }
>
>     # Tool registry (replaces static TOOLS.md)
>     type Tool extending HasTimestamps {
>         required name: str {
>             constraint exclusive;
>         };
>         required description: str;
>         parameters_schema: json;
>         multi available_to: Agent;
>         multi required_capabilities: str;
>     }
>
>     # Communication channels
>     scalar type ChannelType extending enum<
>         'slack', 'discord', 'webhook', 'internal'
>     >;
>
>     type Channel extending HasTimestamps {
>         required name: str;
>         required channel_type: ChannelType;
>         multi connected_agents: Agent;
>         project: Project;
>     }
> }
> ```
>
> **2. Create or update `docker-compose.yml`:**
> Add a Gel service for local development:
>
> ```yaml
> services:
>   gel:
>     image: geldata/gel:6
>     environment:
>       GEL_SERVER_SECURITY: insecure_dev_mode
>     volumes:
>       - "./dbschema:/dbschema"
>       - "gel-data:/var/lib/gel/data"
>     ports:
>       - "5656:5656"
>
> volumes:
>   gel-data:
> ```
>
> **3. Generate initial migration:**
> After starting Gel, run:
>
> ```bash
> gel migration create --non-interactive
> gel migrate
> ```
>
> Commit the generated migration files in `dbschema/migrations/`.
>
> **4. Document the schema decisions:**
>
> - Agent `agent_id` maps to the CockroachDB `agents.id` field -- this is the join key between operational state and knowledge graph
> - Projects, repositories, and tools exist ONLY in Gel (not duplicated in CockroachDB)
> - The `agents` backlink on Project uses a computed link (`.<projects[is Agent]`)

**Verification**:

> - `docker compose up gel` starts Gel without error
> - `gel migration create` generates a valid migration
> - `gel migrate` applies the migration
> - `gel query "SELECT Agent { agent_id, name }"` returns empty set (no errors)

---

### Step 4B.2: Gel-Go Client Integration

**Size**: M
**Prerequisites**: Step 4B.1
**Files**:

- `go.mod` (add gel-go)
- `internal/knowledge/knowledge.go` (update from 3A.3 placeholder)
- `internal/knowledge/gel.go` (new)
- `internal/knowledge/gel_test.go` (new)
- `internal/knowledge/types.go` (new)
- `internal/knowledge/noop.go` (update)

**Dependencies**:

- `github.com/geldata/gel-go`

**Prompt**:

> Implement the Gel DB client for the knowledge graph store.
>
> **1. Add dependency:**
>
> ```
> go get github.com/geldata/gel-go
> ```
>
> **2. Create `internal/knowledge/types.go`:**
> Define Go types that correspond to the Gel schema:
>
> ```go
> type KnownAgent struct {
>     ID           string   `json:"id"`
>     AgentID      string   `json:"agent_id"`
>     Name         string   `json:"name"`
>     Capabilities []string `json:"capabilities"`
> }
>
> type Project struct {
>     ID           string       `json:"id"`
>     Name         string       `json:"name"`
>     Description  string       `json:"description"`
>     Repositories []Repository `json:"repositories,omitempty"`
>     Agents       []KnownAgent `json:"agents,omitempty"`
> }
>
> type Repository struct { ... }
> type Tool struct { ... }
> type Channel struct { ... }
>
> type QueryRequest struct {
>     Type       string // "tools", "agents", "channels", "projects"
>     Filters    map[string]string
>     AgentID    string
>     Capability string
>     ProjectName string
> }
>
> type QueryResponse struct {
>     Results []interface{} `json:"results"`
> }
> ```
>
> **3. Update `internal/knowledge/knowledge.go`:**
> Flesh out the `KnowledgeStore` interface:
>
> ```go
> type KnowledgeStore interface {
>     // Query methods
>     ListTools(ctx context.Context, filters map[string]string) ([]Tool, error)
>     ListAgents(ctx context.Context, filters map[string]string) ([]KnownAgent, error)
>     ListChannels(ctx context.Context, filters map[string]string) ([]Channel, error)
>     ListProjects(ctx context.Context) ([]Project, error)
>     GetAgentByID(ctx context.Context, agentID string) (*KnownAgent, error)
>
>     // Mutation methods
>     UpsertAgent(ctx context.Context, agent KnownAgent) error
>     UpsertTool(ctx context.Context, tool Tool) error
>     UpsertProject(ctx context.Context, project Project) error
>     UpsertChannel(ctx context.Context, channel Channel) error
>     RelateAgentToProject(ctx context.Context, agentID, projectName string) error
>     RelateAgentToTool(ctx context.Context, agentID, toolName string) error
>
>     // Health
>     Healthy(ctx context.Context) error
> }
> ```
>
> **4. Create `internal/knowledge/gel.go`:**
>
> ```go
> type GelStore struct {
>     client *gel.Client
> }
>
> func NewGel(ctx context.Context, opts gel.Options) (*GelStore, error) {
>     client, err := gel.CreateClient(ctx, opts)
>     if err != nil {
>         return nil, fmt.Errorf("create gel client: %w", err)
>     }
>     return &GelStore{client: client}, nil
> }
> ```
>
> Implement all KnowledgeStore methods using EdgeQL queries. Example:
>
> ```go
> func (g *GelStore) ListTools(ctx context.Context, filters map[string]string) ([]Tool, error) {
>     var tools []Tool
>     query := `SELECT Tool {
>         name, description, parameters_schema,
>         available_to: { agent_id, name },
>         required_capabilities
>     }`
>     if cap, ok := filters["capability"]; ok {
>         query += ` FILTER $0 IN .required_capabilities`
>         err := g.client.Query(ctx, query, &tools, cap)
>         return tools, err
>     }
>     err := g.client.Query(ctx, query, &tools)
>     return tools, err
> }
> ```
>
> For mutations, use `Execute`:
>
> ```go
> func (g *GelStore) UpsertAgent(ctx context.Context, agent KnownAgent) error {
>     return g.client.Execute(ctx, `
>         INSERT Agent {
>             agent_id := <str>$0,
>             name := <str>$1,
>             capabilities := <array<str>>$2
>         } UNLESS CONFLICT ON .agent_id
>         ELSE (UPDATE Agent SET {
>             name := <str>$1,
>             capabilities := <array<str>>$2,
>             updated_at := datetime_current()
>         })
>     `, agent.AgentID, agent.Name, agent.Capabilities)
> }
> ```
>
> **5. Update noop implementation** to match the new interface.
>
> **6. Write tests in `internal/knowledge/gel_test.go`:**
> Use build tag `//go:build gel` for tests that require a running Gel instance. Test:
>
> - UpsertAgent and GetAgentByID round-trip
> - ListTools with and without capability filter
> - RelateAgentToProject creates the link
> - Healthy returns nil when connected

**Verification**:

> - `go build ./...` succeeds
> - `go test -tags gel ./internal/knowledge/...` passes against local Gel
> - `go test ./internal/knowledge/...` (without tag) tests noop only

---

### Step 4B.3: Knowledge Graph Sync from CockroachDB

**Size**: S
**Prerequisites**: Steps 4B.2, 3B.1
**Files**:

- `internal/knowledge/sync.go` (new)
- `internal/knowledge/sync_test.go` (new)

**Dependencies**: None new

**Prompt**:

> Implement a sync mechanism that populates the Gel knowledge graph from CockroachDB agent data.
>
> **1. Create `internal/knowledge/sync.go`:**
> When agents register via heartbeat, their capabilities should also be reflected in the knowledge graph. This is a one-way sync: CockroachDB (operational state) -> Gel (knowledge graph).
>
> ```go
> type KnowledgeSyncService struct {
>     knowledge KnowledgeStore
>     logger    *slog.Logger
> }
>
> func NewKnowledgeSyncService(ks KnowledgeStore, logger *slog.Logger) *KnowledgeSyncService { ... }
>
> // SyncAgent upserts an agent into the knowledge graph after a heartbeat.
> func (s *KnowledgeSyncService) SyncAgent(ctx context.Context, agent *model.Agent) {
>     if err := s.knowledge.UpsertAgent(ctx, KnownAgent{
>         AgentID:      agent.ID,
>         Name:         agent.Name,
>         Capabilities: agent.Capabilities,
>     }); err != nil {
>         s.logger.Error("failed to sync agent to knowledge graph", "agent_id", agent.ID, "error", err)
>     }
> }
> ```
>
> **2. Integrate with the SyncStore pattern from 4A.3:**
> Add knowledge sync hooks to the SyncStore wrapper (or create a separate `KnowledgeSyncStore` wrapper that chains with the search `SyncStore`). When `Heartbeat` is called, also sync the agent to Gel.
>
> **3. Write tests:**
>
> - Test SyncAgent calls UpsertAgent on the knowledge store
> - Test error handling (logs error, does not fail the heartbeat)

**Verification**:

> - `go test ./internal/knowledge/...` passes
> - Manual test: send heartbeat, verify agent appears in Gel via `gel query "SELECT Agent { agent_id, name }"`

---

### Step 4B.4: Knowledge Graph API Endpoints

**Size**: M
**Prerequisites**: Steps 4B.3, 3A.5
**Files**:

- `internal/handlers/knowledge.go` (new)
- `internal/handlers/knowledge_test.go` (new)
- `internal/handlers/handlers.go` (update -- add KnowledgeStore dependency)

**Dependencies**: None new

**Prompt**:

> Add knowledge graph query endpoints to the Huma v2 API.
>
> **1. Update API struct:**
> Add `knowledge knowledge.KnowledgeStore` to the API struct.
>
> **2. Create `internal/handlers/knowledge.go`:**
> Register Huma operations for:
>
> **POST /api/v1/knowledge/query** (restricted, admin-like):
>
> - Input: `{ "type": "tools|agents|channels|projects", "filters": { ... } }`
> - Delegates to the appropriate `KnowledgeStore.List*` method based on type
> - Returns the results array
>
> **POST /api/v1/knowledge/relate**:
>
> - Input: `{ "from_type": "agent", "from_id": "agent-42", "relation": "works_on", "to_type": "project", "to_id": "hive-server" }`
> - Maps to the appropriate `RelateAgentTo*` method
> - Returns 204 on success
>
> **GET /api/v1/knowledge/graph** (future visualization):
>
> - For now, return a simple JSON representation of all entities and their relationships
> - This is a read-only endpoint for debugging/visualization
>
> **3. Graceful degradation:**
> If knowledge store is NoopKnowledgeStore, return 503: `{"error": "knowledge graph service unavailable"}`.
>
> **4. Write tests using a mock KnowledgeStore.**

**Verification**:

> - `go test ./internal/handlers/...` passes
> - OpenAPI spec includes knowledge endpoints
> - Manual test against running Gel:
>
>   ```
>   curl -X POST http://localhost:8080/api/v1/knowledge/query \
>     -H "Content-Type: application/json" \
>     -d '{"type": "agents"}'
>   ```

---

## Phase 4C: Agent Intelligence (MasterClaw/OpenClaw Integration)

This phase adds the in-cluster OpenClaw instance (MasterClaw) for LLM-powered operations.

---

### Step 4C.1: MasterClaw HTTP Client

**Size**: S
**Prerequisites**: Step 3A.3
**Files**:

- `internal/masterclaw/client.go` (update from 3A.3 placeholder)
- `internal/masterclaw/types.go` (new)
- `internal/masterclaw/http.go` (new)
- `internal/masterclaw/http_test.go` (new)
- `internal/masterclaw/noop.go` (update)

**Dependencies**: None new (uses standard net/http)

**Prompt**:

> Implement the HTTP client for communicating with MasterClaw (an in-cluster OpenClaw instance).
>
> **1. Create `internal/masterclaw/types.go`:**
>
> ```go
> package masterclaw
>
> // SynthesisRequest asks MasterClaw to synthesize results from multiple backends.
> type SynthesisRequest struct {
>     AgentID    string                 `json:"agent_id"`
>     Query      string                 `json:"query"`
>     Results    map[string]interface{} `json:"results"` // keyed by backend name
>     MaxTokens  int                    `json:"max_tokens"`
> }
>
> type SynthesisResponse struct {
>     Summary    string                 `json:"summary"`
>     Ranked     []interface{}          `json:"ranked"`
>     TokensUsed int                    `json:"tokens_used"`
> }
>
> // DecomposeRequest asks MasterClaw to break a task into subtasks.
> type DecomposeRequest struct {
>     Task       TaskSummary   `json:"task"`
>     Agents     []AgentSummary `json:"agents"`
> }
>
> type TaskSummary struct {
>     ID          string `json:"id"`
>     Title       string `json:"title"`
>     Description string `json:"description"`
> }
>
> type AgentSummary struct {
>     ID           string   `json:"id"`
>     Name         string   `json:"name"`
>     Capabilities []string `json:"capabilities"`
>     Status       string   `json:"status"`
> }
>
> type DecomposeResponse struct {
>     Subtasks []SubtaskSuggestion `json:"subtasks"`
> }
>
> type SubtaskSuggestion struct {
>     Title       string `json:"title"`
>     Description string `json:"description"`
>     Priority    int    `json:"priority"`
>     Tags        []string `json:"tags"`
>     SuggestedAgent string `json:"suggested_agent,omitempty"`
> }
>
> // AssignRequest asks MasterClaw to choose the best agent for a task.
> type AssignRequest struct {
>     Task       TaskSummary    `json:"task"`
>     Candidates []AgentSummary `json:"candidates"`
> }
>
> type AssignResponse struct {
>     AgentID   string `json:"agent_id"`
>     Reasoning string `json:"reasoning"`
> }
> ```
>
> **2. Update `internal/masterclaw/client.go`:**
>
> ```go
> type Client interface {
>     Synthesize(ctx context.Context, req SynthesisRequest) (*SynthesisResponse, error)
>     DecomposeTask(ctx context.Context, req DecomposeRequest) (*DecomposeResponse, error)
>     DecideAssignment(ctx context.Context, req AssignRequest) (*AssignResponse, error)
>     Healthy(ctx context.Context) error
> }
> ```
>
> **3. Create `internal/masterclaw/http.go`:**
> Implement the Client interface using HTTP calls to OpenClaw's webhook API:
>
> ```go
> type HTTPClient struct {
>     baseURL   string
>     authToken string
>     http      *http.Client
> }
>
> func NewHTTPClient(baseURL, authToken string) *HTTPClient {
>     return &HTTPClient{
>         baseURL:   baseURL,
>         authToken: authToken,
>         http:      &http.Client{Timeout: 60 * time.Second},
>     }
> }
> ```
>
> Each method POSTs to `/hooks/agent` with a structured JSON body:
>
> ```json
> {
>   "agentId": "masterclaw",
>   "message": "<structured prompt with context>",
>   "sessionKey": "<unique key per request>",
>   "timeout": 30000
> }
> ```
>
> The prompt for each operation should be carefully structured. For Synthesize:
>
> ```
> You are a search result synthesizer. Given the following search results from multiple backends, produce a ranked summary.
>
> Query: {query}
> Agent: {agent_id}
> Token budget: {max_tokens}
>
> Search Results:
> {JSON-formatted results}
>
> Respond with JSON: {"summary": "...", "ranked": [...], "tokens_used": N}
> ```
>
> Parse the LLM's JSON response. If parsing fails, return a fallback (raw results without synthesis).
>
> **4. Update noop implementation** to match the new interface. NoopClient returns empty responses.
>
> **5. Write tests using httptest.NewServer to mock the OpenClaw webhook endpoint.**

**Verification**:

> - `go test ./internal/masterclaw/...` passes
> - `go build ./...` succeeds

---

### Step 4C.2: MasterClaw Synthesis Service

**Size**: S
**Prerequisites**: Step 4C.1
**Files**:

- `internal/masterclaw/synthesis.go` (new)
- `internal/masterclaw/synthesis_test.go` (new)

**Dependencies**: None new

**Prompt**:

> Create a higher-level synthesis service that wraps the MasterClaw client and handles the common patterns of synthesizing results from multiple backends.
>
> **1. Create `internal/masterclaw/synthesis.go`:**
>
> ```go
> type SynthesisService struct {
>     client  Client
>     enabled bool
>     logger  *slog.Logger
> }
>
> func NewSynthesisService(client Client, enabled bool, logger *slog.Logger) *SynthesisService { ... }
>
> // SynthesizeSearchResults takes results from Meilisearch and/or Gel and synthesizes them.
> // If MasterClaw is disabled or unavailable, returns the raw results without synthesis.
> func (s *SynthesisService) SynthesizeSearchResults(ctx context.Context, agentID, query string, searchHits []search.SearchHit, knowledgeResults []interface{}, maxTokens int) (*SynthesisResponse, error) {
>     if !s.enabled {
>         // Return raw results formatted as a simple response
>         return s.rawFallback(searchHits, knowledgeResults), nil
>     }
>
>     resp, err := s.client.Synthesize(ctx, SynthesisRequest{
>         AgentID:   agentID,
>         Query:     query,
>         Results: map[string]interface{}{
>             "search":    searchHits,
>             "knowledge": knowledgeResults,
>         },
>         MaxTokens: maxTokens,
>     })
>     if err != nil {
>         s.logger.Warn("MasterClaw synthesis failed, falling back to raw results", "error", err)
>         return s.rawFallback(searchHits, knowledgeResults), nil
>     }
>     return resp, nil
> }
>
> func (s *SynthesisService) rawFallback(searchHits []search.SearchHit, knowledgeResults []interface{}) *SynthesisResponse {
>     // Convert raw results to a simple synthesis response without LLM processing
>     ...
> }
> ```
>
> **2. Write tests:**
>
> - Test with enabled=true, successful synthesis
> - Test with enabled=true, synthesis failure -> falls back to raw
> - Test with enabled=false -> raw results returned directly
> - Test token budget is passed through correctly

**Verification**:

> - `go test ./internal/masterclaw/...` passes
> - Graceful degradation works correctly

---

### Step 4C.3: MasterClaw Kubernetes Deployment Config

**Size**: S
**Prerequisites**: Step 4C.1
**Files**:

- `docker-compose.yml` (update -- add MasterClaw service for local dev)
- `deploy/masterclaw/` (new directory with reference configs)
- `deploy/masterclaw/SOUL.md` (new -- MasterClaw agent personality)
- `deploy/masterclaw/openclaw.json` (new -- OpenClaw config)

**Dependencies**: None (infrastructure only)

**Prompt**:

> Create the deployment configuration for MasterClaw (in-cluster OpenClaw instance).
>
> **1. Update `docker-compose.yml`:**
> Add a MasterClaw service for local development:
>
> ```yaml
> masterclaw:
>   image: openclaw/openclaw:latest
>   environment:
>     OPENCLAW_PORT: "3000"
>     OPENCLAW_AUTH_TOKEN: "dev-masterclaw-token"
>     ANTHROPIC_API_KEY: "${ANTHROPIC_API_KEY}"
>   volumes:
>     - "./deploy/masterclaw:/workspace"
>   ports:
>     - "3000:3000"
> ```
>
> **2. Create `deploy/masterclaw/SOUL.md`:**
> This is the agent personality file for MasterClaw:
>
> ```markdown
> # MasterClaw - Hive Project Manager
>
> You are MasterClaw, an AI project manager for the Hive agent infrastructure.
>
> ## Your Responsibilities
>
> 1. Synthesize search results from multiple backends into coherent, ranked summaries
> 2. Decompose high-level tasks into actionable subtasks
> 3. Evaluate agent capabilities and workload to make optimal task assignments
> 4. Monitor task progress and recommend interventions
>
> ## Response Format
>
> Always respond with valid JSON. Your responses are parsed programmatically.
> Never include markdown formatting, code blocks, or explanatory text outside the JSON.
>
> ## Constraints
>
> - You are internal-only. You do not interact with end users.
> - You make recommendations, not final decisions. The hive-server applies them.
> - Keep responses concise. Token budget is provided in each request.
> ```
>
> **3. Create `deploy/masterclaw/openclaw.json`:**
> Minimal OpenClaw configuration for the MasterClaw agent:
>
> ```json
> {
>   "agents": {
>     "masterclaw": {
>       "name": "MasterClaw",
>       "model": "claude-sonnet-4-20250514",
>       "workspace": "/workspace"
>     }
>   },
>   "hooks": {
>     "auth": {
>       "token": "${OPENCLAW_AUTH_TOKEN}"
>     }
>   }
> }
> ```
>
> **4. SECURITY NOTE:**
> MasterClaw must NEVER be exposed to the public internet. In production k8s, it runs as a ClusterIP service with no Ingress. The docker-compose setup binds to localhost only. Add a comment in the docker-compose.yml noting the CVE-2026-25253 and CVE-2026-25157 vulnerabilities.

**Verification**:

> - `docker compose up masterclaw` starts without error
> - `curl http://localhost:3000/health` returns healthy
> - `curl -X POST http://localhost:3000/hooks/agent -H "Authorization: Bearer dev-masterclaw-token" -d '{"agentId":"masterclaw","message":"respond with {\"test\": true}"}'` returns a response

---

## Phase 5: Memory Injection System

This phase builds the per-prompt context injection system that enriches every agent prompt with relevant memories, tasks, and knowledge.

---

### Step 5.1: Query Router

**Size**: L
**Prerequisites**: Steps 4A.2, 4B.3, 3B.1
**Files**:

- `internal/router/router.go` (new)
- `internal/router/types.go` (new)
- `internal/router/router_test.go` (new)

**Dependencies**: None new

**Prompt**:

> Implement the Query Router -- the central component that fans out requests to multiple backends and merges results.
>
> **1. Create `internal/router/types.go`:**
>
> ```go
> package router
>
> type Intent int
>
> const (
>     IntentMemorySearch Intent = iota
>     IntentMemoryStore
>     IntentMemoryRecall
>     IntentTaskSearch
>     IntentTaskStore
>     IntentKnowledgeQuery
>     IntentDiscovery
>     IntentInject
> )
>
> type Request struct {
>     Intent    Intent
>     AgentID   string
>     Query     string
>     Filters   map[string]string
>     Limit     int
>     MaxTokens int
>     Body      interface{}
> }
>
> type Response struct {
>     Results    []interface{}          `json:"results"`
>     Sources    map[string]interface{} `json:"sources"` // which backends contributed
>     Metadata   map[string]interface{} `json:"metadata"`
> }
> ```
>
> **2. Create `internal/router/router.go`:**
>
> ```go
> type Router struct {
>     store     store.Store
>     searcher  search.Searcher
>     knowledge knowledge.KnowledgeStore
>     synthesis *masterclaw.SynthesisService
>     logger    *slog.Logger
> }
>
> func New(s store.Store, srch search.Searcher, kg knowledge.KnowledgeStore, synth *masterclaw.SynthesisService, logger *slog.Logger) *Router { ... }
> ```
>
> Implement the `Route` method that handles fan-out logic:
>
> ```go
> func (r *Router) Route(ctx context.Context, req Request) (*Response, error) {
>     switch req.Intent {
>     case IntentMemorySearch:
>         return r.memorySearch(ctx, req)
>     case IntentKnowledgeQuery:
>         return r.knowledgeQuery(ctx, req)
>     case IntentInject:
>         return r.inject(ctx, req)
>     // ... etc
>     }
> }
> ```
>
> For `IntentMemorySearch`:
>
> 1. Fan out to Meilisearch (full-text search) and Gel (related entities) in parallel using `errgroup`
> 2. Merge results
> 3. Optionally pass through MasterClaw synthesis
> 4. Return unified response
>
> For `IntentInject`:
>
> 1. Extract key terms from the prompt text (simple keyword extraction -- split on whitespace, remove stop words, take top N terms)
> 2. Fan out in parallel:
>    a. Meilisearch: search memories matching key terms
>    b. Gel DB: find entities related to this agent
>    c. CockroachDB: get active tasks assigned to this agent
> 3. Merge all results
> 4. Optionally synthesize via MasterClaw
> 5. Return context blocks within token budget
>
> **3. Use `golang.org/x/sync/errgroup` for parallel fan-out:**
>
> ```go
> g, ctx := errgroup.WithContext(ctx)
>
> var searchResults *search.SearchResponse
> g.Go(func() error {
>     var err error
>     searchResults, err = r.searcher.Search(ctx, "memories", search.SearchRequest{...})
>     return err
> })
>
> var knowledgeResults []knowledge.KnownAgent
> g.Go(func() error {
>     var err error
>     knowledgeResults, err = r.knowledge.ListAgents(ctx, nil)
>     return err
> })
>
> if err := g.Wait(); err != nil {
>     // Handle partial failures: if one backend fails, still return results from the other
> }
> ```
>
> **4. Handle partial failures gracefully.** If Meilisearch is down, return results from Gel and CockroachDB only. If Gel is down, return results from Meilisearch and CockroachDB only. Log the failures.
>
> **5. Write thorough tests:**
>
> - Test fan-out with all backends returning results
> - Test fan-out with one backend failing -> partial results returned
> - Test fan-out with all backends failing -> empty results, no error
> - Test inject intent with keyword extraction
> - Test token budget is respected

**Verification**:

> - `go test ./internal/router/...` passes
> - `go build ./...` succeeds

---

### Step 5.2: Memory Injection Endpoint

**Size**: M
**Prerequisites**: Steps 5.1, 4C.2
**Files**:

- `internal/handlers/inject.go` (new)
- `internal/handlers/inject_test.go` (new)
- `internal/inject/inject.go` (update)
- `internal/handlers/handlers.go` (update -- add Router dependency)

**Dependencies**: None new

**Prompt**:

> Implement the memory injection endpoint that agents call before each LLM prompt.
>
> **1. Create the injection endpoint as a Huma operation:**
>
> **POST /api/v1/memory/inject**:
>
> ```go
> type InjectInput struct {
>     Body struct {
>         AgentID    string `json:"agent_id" doc:"Agent requesting injection"`
>         PromptText string `json:"prompt_text" doc:"The current prompt text" minLength:"1"`
>         SessionKey string `json:"session_key,omitempty" doc:"Session identifier for caching"`
>         MaxTokens  int    `json:"max_tokens,omitempty" doc:"Maximum tokens for injected context" default:"2000" minimum:"100" maximum:"10000"`
>     }
> }
>
> type InjectOutput struct {
>     Body struct {
>         ContextBlocks []ContextBlock `json:"context_blocks"`
>         TokensUsed    int            `json:"tokens_used"`
>     }
> }
>
> type ContextBlock struct {
>     Type    string `json:"type"`    // "memory", "task", "knowledge", "tool"
>     Content string `json:"content"` // Human-readable context string
>     Source  string `json:"source"`  // Which backend provided this
>     Score   float64 `json:"score,omitempty"` // Relevance score
> }
> ```
>
> **2. Handler implementation:**
>
> - Extract agent_id from the request body (override context agent_id if provided)
> - Create a `router.Request` with `IntentInject`
> - Call `router.Route(ctx, req)`
> - Convert router response to `ContextBlock` array
> - Estimate token count (simple: len(content)/4 as rough approximation)
> - Trim blocks to fit within `max_tokens` budget (remove lowest-scored blocks first)
> - Return the response
>
> **3. Token estimation:**
> Use a simple character-based approximation: 1 token ~= 4 characters for English text. This is good enough for budget management. A more accurate tokenizer can be added later.
>
> **4. Write tests:**
>
> - Test injection with results from all backends -> context blocks returned
> - Test injection with no relevant results -> empty context blocks
> - Test token budget trimming -> lowest-scored blocks dropped
> - Test agent_id scoping -> only relevant results returned
> - Test max_tokens validation (min 100, max 10000)

**Verification**:

> - `go test ./internal/handlers/...` passes
> - Manual test:
>
>   ```
>   curl -X POST http://localhost:8080/api/v1/memory/inject \
>     -H "Content-Type: application/json" \
>     -d '{"agent_id":"agent-1","prompt_text":"How do I fix the CI pipeline?","max_tokens":2000}'
>   ```
>
> - OpenAPI spec includes the inject operation

---

### Step 5.3: Injection Logging

**Size**: S
**Prerequisites**: Step 5.2
**Files**:

- `internal/store/migrations/002_injection_log.sql` (new)
- `internal/store/crdb_inject_log.go` (new)
- `internal/store/sqlite_inject_log.go` (new -- SQLite version)
- `internal/store/store.go` (update interface)
- `internal/handlers/inject.go` (update -- add logging)

**Dependencies**: None new

**Prompt**:

> Add injection logging to track what context is being injected and optimize over time.
>
> **1. Create migration `002_injection_log.sql`:**
>
> ```sql
> -- +goose Up
> CREATE TABLE IF NOT EXISTS injection_log (
>     id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
>     agent_id    TEXT        NOT NULL,
>     session_key TEXT        NOT NULL DEFAULT '',
>     prompt_hash TEXT        NOT NULL,
>     context_blocks JSONB    NOT NULL DEFAULT '[]'::JSONB,
>     tokens_used INT4        NOT NULL DEFAULT 0,
>     latency_ms  INT4        NOT NULL DEFAULT 0,
>     created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
> );
>
> CREATE INDEX idx_injection_log_agent ON injection_log(agent_id);
> CREATE INDEX idx_injection_log_created ON injection_log(created_at);
>
> -- +goose Down
> DROP TABLE IF EXISTS injection_log;
> ```
>
> **2. Add injection log methods to Store interface:**
>
> ```go
> LogInjection(ctx context.Context, log *model.InjectionLog) error
> ```
>
> **3. Implement in both SQLite and CRDB stores.**
> For SQLite, use a TEXT JSON column instead of JSONB. For the id, generate UUID in Go.
>
> **4. Update the inject handler:**
> After computing the injection response, log it asynchronously:
>
> ```go
> go func() {
>     _ = a.store.LogInjection(context.Background(), &model.InjectionLog{
>         AgentID:       agentID,
>         SessionKey:    input.Body.SessionKey,
>         PromptHash:    sha256(input.Body.PromptText)[:16],
>         ContextBlocks: responseBlocks,
>         TokensUsed:    tokensUsed,
>         LatencyMs:     elapsed.Milliseconds(),
>     })
> }()
> ```
>
> **5. Add model type:**
> Define `model.InjectionLog` in `internal/model/`.

**Verification**:

> - `go test ./...` passes
> - After injection requests, check the injection_log table has entries
> - Logging is async and does not affect injection latency

---

### Step 5.4: Token Budget Management

**Size**: S
**Prerequisites**: Step 5.2
**Files**:

- `internal/inject/budget.go` (new)
- `internal/inject/budget_test.go` (new)

**Dependencies**: None new

**Prompt**:

> Implement the token budget management system for memory injection.
>
> **1. Create `internal/inject/budget.go`:**
>
> ```go
> package inject
>
> // EstimateTokens estimates the token count for a string.
> // Uses a simple character-based approximation (1 token ~= 4 chars for English).
> func EstimateTokens(text string) int {
>     return (len(text) + 3) / 4 // round up
> }
>
> // TrimToTokenBudget trims a slice of ContextBlocks to fit within maxTokens.
> // Blocks are sorted by score (highest first). Lowest-scored blocks are removed
> // until the total fits within the budget.
> func TrimToTokenBudget(blocks []ContextBlock, maxTokens int) ([]ContextBlock, int) {
>     // Sort by score descending
>     sort.Slice(blocks, func(i, j int) bool {
>         return blocks[i].Score > blocks[j].Score
>     })
>
>     var result []ContextBlock
>     total := 0
>     for _, b := range blocks {
>         tokens := EstimateTokens(b.Content)
>         if total + tokens > maxTokens {
>             break // Would exceed budget
>         }
>         result = append(result, b)
>         total += tokens
>     }
>     return result, total
> }
> ```
>
> **2. Write comprehensive tests:**
>
> - Test EstimateTokens with known strings
> - Test TrimToTokenBudget with blocks that fit entirely
> - Test TrimToTokenBudget with blocks that must be trimmed
> - Test TrimToTokenBudget with empty input
> - Test that highest-scored blocks are kept when trimming

**Verification**:

> - `go test ./internal/inject/...` passes
> - Budget trimming produces reasonable results

---

## Phase 6: LLM-Enabled Project Manager

This phase adds intelligent task management capabilities powered by MasterClaw.

---

### Step 6.1: Subtask Hierarchy

**Size**: M
**Prerequisites**: Step 3B.1
**Files**:

- `internal/store/migrations/003_subtask_hierarchy.sql` (new, if not already in 001)
- `internal/model/model.go` (update Task model)
- `internal/store/crdb_tasks.go` (update for parent_id)
- `internal/store/sqlite_tasks.go` (update for parent_id)
- `internal/handlers/tasks.go` (update for parent_id in create/list)

**Dependencies**: None new

**Prompt**:

> Add subtask hierarchy to the task system. Tasks can now have a parent_id pointing to another task, creating a tree structure.
>
> **1. Add migration (or update 001 if not yet deployed):**
>
> ```sql
> -- +goose Up
> ALTER TABLE tasks ADD COLUMN IF NOT EXISTS parent_id UUID REFERENCES tasks(id);
> CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks(parent_id);
>
> -- +goose Down
> DROP INDEX IF EXISTS idx_tasks_parent;
> ALTER TABLE tasks DROP COLUMN IF EXISTS parent_id;
> ```
>
> **2. Update Task model:**
> Add `ParentID *string` field (nullable). Add `Subtasks []*Task` field (populated on GetTask).
>
> **3. Update store implementations:**
>
> - CreateTask: accept optional parent_id. Validate parent exists if provided.
> - GetTask: also fetch subtasks (SELECT tasks WHERE parent_id = $1).
> - ListTasks: add `parent_id` filter. Add `root_only` filter (WHERE parent_id IS NULL) for listing top-level tasks only.
>
> **4. Update task handlers:**
>
> - POST /api/v1/tasks: accept optional `parent_id` in request body
> - GET /api/v1/tasks: add `parent` query param for filtering by parent_id, add `root_only` boolean query param
> - GET /api/v1/tasks/{id}: response now includes `subtasks` array
>
> **5. Update tests to cover:**
>
> - Creating a subtask linked to a parent
> - Fetching a task returns its subtasks
> - Listing with root_only=true excludes subtasks
> - Deleting a parent cascades to subtasks (if configured) or prevents deletion (if subtasks exist)

**Verification**:

> - `go test ./...` passes
> - Manual test: create parent task, create subtask, GET parent shows subtasks
> - OpenAPI spec updated with parent_id field and new query params

---

### Step 6.2: Task Decomposition Endpoint

**Size**: M
**Prerequisites**: Steps 6.1, 4C.2
**Files**:

- `internal/handlers/tasks.go` (update -- add decompose endpoint)
- `internal/handlers/tasks_test.go` (update)

**Dependencies**: None new

**Prompt**:

> Add an LLM-powered task decomposition endpoint that uses MasterClaw to break a high-level task into subtasks.
>
> **1. Register Huma operation:**
>
> **POST /api/v1/tasks/{id}/decompose**:
>
> ```go
> type DecomposeInput struct {
>     ID string `path:"id" doc:"Task ID to decompose"`
> }
>
> type DecomposeOutput struct {
>     Body struct {
>         Subtasks []model.Task `json:"subtasks"`
>     }
> }
> ```
>
> **2. Handler implementation:**
>
> 1. Fetch the task from the store
> 2. Fetch available agents from the store
> 3. Call `masterclaw.Client.DecomposeTask()` with the task and agent list
> 4. For each subtask suggestion returned:
>    - Create the subtask in the store with `parent_id` set to the original task
>    - If a `suggested_agent` is provided and valid, set it as the assignee
> 5. Return the created subtasks
>
> **3. Graceful degradation:**
> If MasterClaw is unavailable (NoopClient), return 503: `{"error": "task decomposition requires MasterClaw (LLM service unavailable)"}`
>
> **4. Rate limiting:**
> Add a simple in-memory rate limiter: max 10 decomposition requests per minute per agent. This prevents runaway LLM costs.
>
> **5. Write tests:**
>
> - Test decomposition creates subtasks linked to parent
> - Test with MasterClaw unavailable -> 503
> - Test rate limiting -> 429 after limit exceeded

**Verification**:

> - `go test ./internal/handlers/...` passes
> - Manual test against running MasterClaw:
>
>   ```
>   curl -X POST http://localhost:8080/api/v1/tasks/{id}/decompose
>   ```
>
> - Subtasks appear linked to the parent task

---

### Step 6.3: Intelligent Task Assignment

**Size**: M
**Prerequisites**: Steps 6.2, 4B.3
**Files**:

- `internal/handlers/tasks.go` (update -- add assign endpoint)
- `internal/handlers/tasks_test.go` (update)

**Dependencies**: None new

**Prompt**:

> Add an LLM-powered task assignment endpoint that uses MasterClaw and the knowledge graph to choose the best agent for a task.
>
> **1. Register Huma operation:**
>
> **POST /api/v1/tasks/{id}/assign**:
>
> ```go
> type AssignInput struct {
>     ID string `path:"id" doc:"Task ID to assign"`
> }
>
> type AssignOutput struct {
>     Body struct {
>         Task      *model.Task `json:"task"`
>         Reasoning string      `json:"reasoning"`
>     }
> }
> ```
>
> **2. Handler implementation:**
>
> 1. Fetch the task from the store (must be status=open or status=failed for reassignment)
> 2. Fetch online agents from the store (heartbeat within 5 minutes)
> 3. Enrich agent data from knowledge graph: get capabilities, project associations
> 4. If only one capable agent is available, assign directly (no LLM call)
> 5. If multiple candidates, call `masterclaw.Client.DecideAssignment()`
> 6. Update task: set assignee, change status to `claimed`
> 7. Return the updated task and MasterClaw's reasoning
>
> **3. Simple rule-based fallback:**
> If MasterClaw is unavailable, use a simple rule-based assignment:
>
> - Filter agents by matching capabilities (from knowledge graph)
> - Among matches, pick the one with the fewest active tasks (from CockroachDB)
> - If no capability data available, pick the least-loaded agent
>
> **4. Write tests:**
>
> - Test assignment with MasterClaw -> correct agent assigned
> - Test assignment without MasterClaw -> rule-based fallback works
> - Test assignment with no available agents -> appropriate error
> - Test assignment of non-open task -> error

**Verification**:

> - `go test ./internal/handlers/...` passes
> - Manual test: create task, trigger assignment, verify assignee set

---

### Step 6.4: Progress Monitoring Service

**Size**: M
**Prerequisites**: Step 6.3
**Files**:

- `internal/monitor/monitor.go` (new)
- `internal/monitor/monitor_test.go` (new)
- `cmd/hive-server/serve.go` (update -- start monitor goroutine)

**Dependencies**: None new

**Prompt**:

> Implement a background service that monitors task progress and takes corrective action.
>
> **1. Create `internal/monitor/monitor.go`:**
>
> ```go
> type Monitor struct {
>     store     store.Store
>     claw      masterclaw.Client
>     knowledge knowledge.KnowledgeStore
>     config    MonitorConfig
>     logger    *slog.Logger
> }
>
> type MonitorConfig struct {
>     CheckInterval     time.Duration // default: 5 minutes
>     StaleTaskTimeout  time.Duration // default: 6 hours
>     OfflineGracePeriod time.Duration // default: 10 minutes
>     Enabled           bool
> }
> ```
>
> **2. Implement the monitoring loop:**
>
> ```go
> func (m *Monitor) Run(ctx context.Context) {
>     ticker := time.NewTicker(m.config.CheckInterval)
>     defer ticker.Stop()
>     for {
>         select {
>         case <-ctx.Done():
>             return
>         case <-ticker.C:
>             m.check(ctx)
>         }
>     }
> }
>
> func (m *Monitor) check(ctx context.Context) {
>     // 1. Find tasks in_progress for longer than StaleTaskTimeout
>     //    without any recent notes/updates
>     //    -> Log warning, optionally request MasterClaw evaluation
>
>     // 2. Find agents that went offline (heartbeat > OfflineGracePeriod)
>     //    with claimed/in_progress tasks
>     //    -> Unclaim those tasks (status -> open, assignee -> "")
>     //    -> Log the reassignment
>
>     // 3. Find tasks with all subtasks completed
>     //    -> If parent task is in_progress, log that it may be ready for completion
> }
> ```
>
> **3. Configuration via env vars:**
>
> - `MONITOR_ENABLED` (default: "true")
> - `MONITOR_INTERVAL` (default: "5m")
> - `MONITOR_STALE_TIMEOUT` (default: "6h")
>
> **4. Wire up in `cmd/hive-server/serve.go`:**
> Start the monitor in a goroutine when enabled. Cancel via shutdown context.
>
> **5. Write tests:**
>
> - Test stale task detection
> - Test offline agent task reclamation
> - Test subtask completion detection
> - Test disabled monitor does not run

**Verification**:

> - `go test ./internal/monitor/...` passes
> - Server starts with monitor enabled (visible in logs)
> - Create a task, let agent go offline, verify task is unclaimed after grace period

---

## Phase 7: Discovery API and Knowledge Graph Evolution

This phase completes the vision by implementing the Discovery API backed by the knowledge graph and adding deep health checks.

---

### Step 7.1: Discovery API Endpoints

**Size**: M
**Prerequisites**: Step 4B.3
**Issue**: #9
**Files**:

- `internal/handlers/discovery.go` (new)
- `internal/handlers/discovery_test.go` (new)
- `internal/handlers/handlers.go` (update)

**Dependencies**: None new

**Prompt**:

> Implement the Discovery API -- a dynamic metadata registry that replaces static TOOLS.md blobs.
>
> **1. Register Huma operations:**
>
> **GET /api/v1/discover**:
> Unified discovery endpoint with query parameters:
>
> - `type` (required): "tools", "agents", "channels"
> - `capability`: filter by capability string
> - `project`: filter by project name
> - `agent_id`: filter by specific agent
>
> Delegates to the appropriate knowledge store method based on type.
>
> **GET /api/v1/discover/tools**:
> List available tools. If knowledge store is available, return from Gel. Otherwise, return a static fallback list.
>
> **GET /api/v1/discover/agents**:
> Combine live agent data (from CockroachDB heartbeats) with knowledge graph data (capabilities, projects):
>
> 1. Fetch agents from store (CockroachDB)
> 2. Enrich with knowledge graph data (Gel)
> 3. Return merged results
>
> **GET /api/v1/discover/channels**:
> List communication channels from the knowledge graph.
>
> **PUT /api/v1/discover/tools/{name}**:
> Register or update a tool in the knowledge graph. Input: name, description, parameters_schema (JSON Schema), required_capabilities.
>
> **PUT /api/v1/discover/agents/{id}**:
> Register or update agent metadata in the knowledge graph. Input: agent_id, name, capabilities, projects.
>
> **2. Response format:**
>
> ```json
> {
>     "results": [...],
>     "source": "knowledge_graph" | "store" | "static_fallback",
>     "count": 42
> }
> ```
>
> **3. Graceful degradation:**
> If Gel is unavailable, fall back to CockroachDB-only data for agents. For tools and channels, return 503 or an empty list with a warning header.
>
> **4. Write tests using mock knowledge store and mock regular store.**

**Verification**:

> - `go test ./internal/handlers/...` passes
> - OpenAPI spec includes all discovery endpoints
> - Manual test: register a tool via PUT, discover it via GET

---

### Step 7.2: Tool Registry Population

**Size**: S
**Prerequisites**: Step 7.1
**Files**:

- `internal/knowledge/tools.go` (new)
- `deploy/masterclaw/tools.json` (new -- seed data)
- `cmd/hive-server/serve.go` (update -- seed tools on startup)

**Dependencies**: None new

**Prompt**:

> Populate the tool registry in Gel DB with the known hive-server tools on startup.
>
> **1. Create `internal/knowledge/tools.go`:**
> Define the built-in tools that hive-server exposes:
>
> ```go
> var BuiltinTools = []Tool{
>     {
>         Name:        "hive.memory.store",
>         Description: "Store a key-value memory entry for later retrieval",
>         ParametersSchema: json.RawMessage(`{
>             "type": "object",
>             "properties": {
>                 "key": {"type": "string"},
>                 "value": {"type": "string"},
>                 "tags": {"type": "array", "items": {"type": "string"}}
>             },
>             "required": ["key", "value"]
>         }`),
>     },
>     // ... one entry per hive subcommand
> }
> ```
>
> **2. Seed on startup:**
> When the knowledge store is available, upsert all built-in tools during server initialization. This is idempotent.
>
> **3. Create `deploy/masterclaw/tools.json`:**
> A JSON file listing the tools available to MasterClaw for reference in its SOUL.md.

**Verification**:

> - Server starts and seeds tools without error
> - `GET /api/v1/discover/tools` returns the built-in tools

---

### Step 7.3: Agent and Channel Discovery

**Size**: S
**Prerequisites**: Step 7.1
**Files**:

- `internal/handlers/discovery.go` (update)
- `internal/handlers/discovery_test.go` (update)

**Dependencies**: None new

**Prompt**:

> Enhance the discovery endpoints for agents and channels with richer data from the knowledge graph.
>
> **1. Agent discovery enrichment:**
> When listing agents via discovery:
>
> - Fetch live status from CockroachDB (heartbeat, online/offline)
> - Fetch capabilities and project associations from Gel
> - Merge into a single response per agent:
>
> ```json
> {
>   "agent_id": "agent-42",
>   "name": "CI-bot",
>   "status": "online",
>   "last_heartbeat": "2026-03-09T12:00:00Z",
>   "capabilities": ["go", "docker", "ci-cd"],
>   "projects": ["hive-server", "hive-local"],
>   "active_tasks": 2
> }
> ```
>
> - Include `active_tasks` count (from CockroachDB: count of tasks with status in [claimed, in_progress] for this agent)
>
> **2. Channel discovery:**
>
> - Channels exist only in Gel. No CockroachDB merge needed.
> - Include connected agents (live status from CockroachDB)
>
> **3. Write tests for the merge logic.**

**Verification**:

> - `go test ./internal/handlers/...` passes
> - Agent discovery shows merged data from both backends
> - Channel discovery shows connected agents with live status

---

### Step 7.4: Deep Health Check (All Backends)

**Size**: S
**Prerequisites**: Steps 4A.2, 4B.2, 3B.1
**Files**:

- `internal/handlers/health.go` (update)
- `internal/handlers/health_test.go` (update)

**Dependencies**: None new

**Prompt**:

> Upgrade the /healthz endpoint to check ALL backends, and add a /ready endpoint that gates on required backends.
>
> **1. Update `/healthz` to check all backends:**
>
> ```go
> func (a *API) handleHealthz(w http.ResponseWriter, r *http.Request) {
>     ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
>     defer cancel()
>
>     status := map[string]string{}
>     healthy := true
>
>     // Store (required)
>     if err := a.store.Ping(ctx); err != nil {
>         status["store"] = "unhealthy: " + err.Error()
>         healthy = false
>     } else {
>         status["store"] = "healthy"
>     }
>
>     // Search (optional)
>     if err := a.searcher.Healthy(ctx); err != nil {
>         status["search"] = "unhealthy: " + err.Error()
>     } else {
>         status["search"] = "healthy"
>     }
>
>     // Knowledge (optional)
>     if err := a.knowledge.Healthy(ctx); err != nil {
>         status["knowledge"] = "unhealthy: " + err.Error()
>     } else {
>         status["knowledge"] = "healthy"
>     }
>
>     // MasterClaw (optional)
>     if err := a.claw.Healthy(ctx); err != nil {
>         status["masterclaw"] = "unhealthy: " + err.Error()
>     } else {
>         status["masterclaw"] = "healthy"
>     }
>
>     if !healthy {
>         w.WriteHeader(http.StatusServiceUnavailable)
>     }
>     json.NewEncoder(w).Encode(status)
> }
> ```
>
> **2. Update `/ready`:**
> The readiness probe should only check required backends (store). Optional backends (search, knowledge, masterclaw) being down should NOT make the pod unready.
>
> **3. Write tests for each combination of healthy/unhealthy backends.**

**Verification**:

> - `go test ./internal/handlers/...` passes
> - `/healthz` returns status for all configured backends
> - `/ready` returns 200 even when optional backends are down
> - `/ready` returns 503 when the store is down

---

## Appendix A: Full Docker Compose for Local Development

After all phases, the `docker-compose.yml` should support the full local stack:

```yaml
services:
  cockroachdb:
    image: cockroachdb/cockroach:latest
    command: start-single-node --insecure
    ports:
      - "26257:26257"
      - "8080:8080"
    volumes:
      - crdb-data:/cockroach/cockroach-data

  gel:
    image: geldata/gel:6
    environment:
      GEL_SERVER_SECURITY: insecure_dev_mode
    volumes:
      - "./dbschema:/dbschema"
      - gel-data:/var/lib/gel/data
    ports:
      - "5656:5656"

  meilisearch:
    image: getmeili/meilisearch:v1.12
    environment:
      MEILI_ENV: development
    volumes:
      - meili-data:/meili_data
    ports:
      - "7700:7700"

  masterclaw:
    image: openclaw/openclaw:latest
    environment:
      OPENCLAW_PORT: "3000"
      OPENCLAW_AUTH_TOKEN: "dev-masterclaw-token"
      ANTHROPIC_API_KEY: "${ANTHROPIC_API_KEY}"
    volumes:
      - "./deploy/masterclaw:/workspace"
    ports:
      - "127.0.0.1:3000:3000" # localhost only -- never expose publicly

volumes:
  crdb-data:
  gel-data:
  meili-data:
```

---

## Appendix B: Environment Variables Reference

| Variable                   | Default         | Phase | Description                                           |
| -------------------------- | --------------- | ----- | ----------------------------------------------------- |
| `PORT`                     | `8080`          | 3A    | HTTP listen port                                      |
| `HIVE_TOKEN`               | (empty)         | 3A    | Bearer auth token (empty = disabled)                  |
| `DATABASE_URL`             | (none)          | 3B    | CockroachDB connection string. If unset, uses SQLite. |
| `HIVE_DB_PATH`             | `/data/hive.db` | 3A    | SQLite database path (when no DATABASE_URL)           |
| `MEILI_URL`                | (none)          | 4A    | Meilisearch URL (e.g., <http://localhost:7700>)       |
| `MEILI_API_KEY`            | (none)          | 4A    | Meilisearch API key                                   |
| `MEILI_RECONCILE_INTERVAL` | `5m`            | 4A    | Search reconciliation interval (0 to disable)         |
| `GEL_DSN`                  | (none)          | 4B    | Gel DB connection string                              |
| `MASTERCLAW_URL`           | (none)          | 4C    | MasterClaw base URL                                   |
| `MASTERCLAW_TOKEN`         | (none)          | 4C    | MasterClaw auth token                                 |
| `MASTERCLAW_ENABLED`       | `false`         | 4C    | Enable LLM-powered features                           |
| `MONITOR_ENABLED`          | `true`          | 6     | Enable progress monitor                               |
| `MONITOR_INTERVAL`         | `5m`            | 6     | Monitor check interval                                |
| `MONITOR_STALE_TIMEOUT`    | `6h`            | 6     | Stale task threshold                                  |

---

## Appendix C: Step Summary Table

| Step | Title                   | Size | Phase | Prerequisites    |
| ---- | ----------------------- | ---- | ----- | ---------------- |
| 3A.1 | Remove k8s/             | XS   | 3A    | None             |
| 3A.2 | Scripts pattern         | S    | 3A    | None             |
| 3A.3 | Project layout refactor | M    | 3A    | None             |
| 3A.4 | Store interface         | S    | 3A    | 3A.3             |
| 3A.5 | Huma v2                 | L    | 3A    | 3A.4             |
| 3A.6 | E2E test scaffold       | S    | 3A    | 3A.5             |
| 3B.1 | CRDB store + tx retries | L    | 3B    | 3A.5             |
| 3B.2 | CRDB unit tests         | M    | 3B    | 3B.1             |
| 3B.3 | Ephemeral CRDB          | S    | 3B    | 3B.1             |
| 3B.4 | Deep health check       | XS   | 3B    | 3B.1             |
| 4A.1 | Search interface        | S    | 4A    | 3A.4             |
| 4A.2 | Meilisearch backend     | M    | 4A    | 4A.1             |
| 4A.3 | Search sync pipeline    | M    | 4A    | 4A.2, 3B.1       |
| 4A.4 | Search endpoints        | M    | 4A    | 4A.2, 3A.5       |
| 4A.5 | Reconciliation job      | S    | 4A    | 4A.3             |
| 4B.1 | Gel schema design       | S    | 4B    | 3A.3             |
| 4B.2 | Gel-Go client           | M    | 4B    | 4B.1             |
| 4B.3 | Knowledge graph sync    | S    | 4B    | 4B.2, 3B.1       |
| 4B.4 | Knowledge endpoints     | M    | 4B    | 4B.3, 3A.5       |
| 4C.1 | MasterClaw client       | S    | 4C    | 3A.3             |
| 4C.2 | Synthesis service       | S    | 4C    | 4C.1             |
| 4C.3 | MasterClaw deployment   | S    | 4C    | 4C.1             |
| 5.1  | Query router            | L    | 5     | 4A.2, 4B.3, 3B.1 |
| 5.2  | Injection endpoint      | M    | 5     | 5.1, 4C.2        |
| 5.3  | Injection logging       | S    | 5     | 5.2              |
| 5.4  | Token budget mgmt       | S    | 5     | 5.2              |
| 6.1  | Subtask hierarchy       | M    | 6     | 3B.1             |
| 6.2  | Task decomposition      | M    | 6     | 6.1, 4C.2        |
| 6.3  | Intelligent assignment  | M    | 6     | 6.2, 4B.3        |
| 6.4  | Progress monitoring     | M    | 6     | 6.3              |
| 7.1  | Discovery endpoints     | M    | 7     | 4B.3             |
| 7.2  | Tool registry           | S    | 7     | 7.1              |
| 7.3  | Agent/channel discovery | S    | 7     | 7.1              |
| 7.4  | Deep health (all)       | S    | 7     | 4A.2, 4B.2, 3B.1 |

**Total: 33 steps across 7 phases**

- XS: 2, S: 14, M: 13, L: 4
