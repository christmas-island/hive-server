# Build Plan v2: hive-server

**Date:** 2026-03-09
**Status:** Implementable build plan for vision-v3
**Inputs:** vision-v3.md, skill-replacement-analysis.md, hive-server-current.md, github-issues.md, ultrathink-architect.md, ultrathink-skeptic.md, ultrathink-devex.md, ultrathink-ops.md, actual codebase inspection

---

## Current State (verified by code inspection)

- Single Go binary, ~3000 lines, 28 Go files
- Huma v2 already integrated (handlers use `humachi.New`, register operations)
- SQLite via `modernc.org/sqlite` (pure Go, no CGO)
- chi v5 router with auth middleware, agent ID injection
- Store is a concrete struct; handlers define their own `Store` interface (13 methods)
- Data types live in `internal/store/` (MemoryEntry, Task, Agent, etc.)
- Tests exist for handlers (mock-based) and store (SQLite-based)
- E2E tests exist in `test/e2e/`
- CI: pre-commit + go test; Release: semantic-release + goreleaser
- `cmd/app/` is the entrypoint (not yet refactored to `cmd/hive-server/`)
- No `internal/model/` package yet
- No events, sessions, projects, phases, plans, requirements, workflows, specs, skills tables
- No Meilisearch, no Gel, no CockroachDB driver
- `k8s/` directory still exists (managed externally, should be removed per #10)

---

## Phase 0: Foundation -- CockroachDB Migration and Structural Refactor

**Goal:** Move from SQLite to CockroachDB as primary store, clean up project layout, establish patterns for everything that follows.

**Why first:** CockroachDB is already running in production. Every subsequent step builds on CRDB schema. Doing this first means we never build features against SQLite only to rewrite them.

### Step 0.1: Remove scaffolded k8s/ directory

- **Dependencies:** None
- **Description:** Delete the `k8s/` directory. Deployment is managed externally. Remove any references to it in CLAUDE.md or README.md.
- **Files modified:** Delete `k8s/` directory, update `CLAUDE.md` (remove k8s mention from Structure section), update `README.md` if it references k8s/.
- **Acceptance criteria:** `k8s/` directory does not exist. CI passes. No remaining references to k8s/ in documentation.
- **Estimated scope:** S (< 1 hour)
- **GitHub issue:** #10

### Step 0.2: Extract internal/model/ package

- **Dependencies:** None (can parallelize with 0.1)
- **Description:** Move all data types and sentinel errors from `internal/store/` to a new `internal/model/` package. This breaks the circular dependency where handlers import store types. The store package will import model; handlers will import model; handlers will NOT import store for types.
- **Files created:**
  - `internal/model/memory.go` -- MemoryEntry, MemoryFilter
  - `internal/model/tasks.go` -- Task, TaskNote, TaskFilter, TaskUpdate, valid status transitions
  - `internal/model/agents.go` -- Agent, AgentStatus
  - `internal/model/errors.go` -- ErrNotFound, ErrConflict, ErrInvalidTransition
- **Files modified:**
  - `internal/store/store.go` -- remove type definitions, import model
  - `internal/store/memory.go` -- change references from `store.MemoryEntry` to `model.MemoryEntry`
  - `internal/store/tasks.go` -- same
  - `internal/store/agents.go` -- same
  - `internal/handlers/handlers.go` -- Store interface uses `model.*` types instead of `store.*`
  - `internal/handlers/memory.go` -- update type references
  - `internal/handlers/tasks.go` -- update type references
  - `internal/handlers/agents.go` -- update type references
  - All `_test.go` files in handlers/ and store/
- **Acceptance criteria:** `go build ./...` succeeds. `go test ./...` passes. No handler or test imports `internal/store` for type access (only for store construction). `internal/model/` exists with all types.
- **Estimated scope:** M (1-2 days)
- **GitHub issue:** #20 (partial)

### Step 0.3: Split Store interface into composed domain interfaces

- **Dependencies:** 0.2
- **Description:** Define domain-specific interfaces in `internal/store/interfaces.go`. The composite `Store` interface embeds them all. Handler tests can mock individual domain interfaces.
- **Files created:**
  - `internal/store/interfaces.go` -- MemoryStore, TaskStore, AgentStore interfaces, plus composite Store interface with Ping() and Close()
- **Files modified:**
  - `internal/handlers/handlers.go` -- remove the local Store interface definition, import `store.Store` (or use domain-specific interfaces per handler group)
  - `internal/store/store.go` -- SQLiteStore struct implements Store interface (verify with compile-time assertion: `var _ Store = (*SQLiteStore)(nil)`)
- **Acceptance criteria:** `go build ./...` succeeds. `go test ./...` passes. Compile-time interface assertion succeeds. Handler tests continue to work with mock that satisfies the composed interface.
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** #20 (partial)

### Step 0.4: Rename cmd/app/ to cmd/hive-server/

- **Dependencies:** 0.2 (do after model extraction to avoid merge conflicts)
- **Description:** Rename the CLI entrypoint from `cmd/app/` to `cmd/hive-server/`. Update Dockerfile, .goreleaser.yaml, any scripts, and CI workflows that reference the build path.
- **Files modified:**
  - Move `cmd/app/main.go` to `cmd/hive-server/main.go`
  - Move `cmd/app/serve.go` to `cmd/hive-server/serve.go`
  - Update `Dockerfile` build path
  - Update `.goreleaser.yaml` build path
  - Update any `script/` files that reference `cmd/app`
  - Update `CLAUDE.md` structure section
- **Acceptance criteria:** `go build ./cmd/hive-server` produces binary. `go test ./...` passes. CI passes. Docker build succeeds.
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** #20 (partial)

### Step 0.5: Add schema fields -- repo, scope, session_id on memory and tasks

- **Dependencies:** 0.2 (needs model package)
- **Description:** Add `repo`, `scope`, and `session_id` fields to MemoryEntry and Task models. Add corresponding columns to SQLite schema. Extend MemoryFilter and TaskFilter to support filtering by these fields. Update list endpoints to accept these query params. This is the minimum schema extension needed before CRDB migration so the CRDB schema is complete from the start.
- **Files modified:**
  - `internal/model/memory.go` -- add Repo, Scope, SessionID fields to MemoryEntry; add to MemoryFilter
  - `internal/model/tasks.go` -- add Repo, SessionID fields to Task; add to TaskFilter
  - `internal/store/store.go` -- update SQLite schema CREATE TABLE statements, add migration for new columns
  - `internal/store/memory.go` -- update SQL queries to read/write/filter new fields
  - `internal/store/tasks.go` -- same
  - `internal/handlers/memory.go` -- add query params for new filter fields
  - `internal/handlers/tasks.go` -- same
  - Update tests to exercise new fields and filters
- **Acceptance criteria:** New fields are stored and retrievable. List endpoints filter by repo, scope, session_id. Existing tests still pass. New tests cover the added fields.
- **Estimated scope:** M (1-2 days)
- **GitHub issue:** None (new, derived from devex analysis and vision-v3)

### Step 0.6: Add goose migration framework with embedded migrations

- **Dependencies:** 0.5
- **Description:** Integrate `pressly/goose/v3` for schema migrations. Create the initial migration that establishes the full core CRDB schema (memory, tasks, task_notes, agents) with proper CRDB types (TIMESTAMPTZ, JSONB, UUID with gen_random_uuid(), INVERTED INDEX for tags). Migrations are embedded in the binary via `embed.FS`. The SQLite store continues to manage its own schema inline (goose is for CRDB only).
- **Files created:**
  - `internal/store/crdb/migrations/` directory
  - `internal/store/crdb/migrations/001_core_schema.sql` -- memory, tasks, task_notes, agents tables with all indexes (from vision-v3 Section 5.1)
  - `internal/store/crdb/embed.go` -- `//go:embed migrations/*.sql` for embedding
- **Files modified:**
  - `go.mod` -- add `github.com/pressly/goose/v3`
- **Acceptance criteria:** Migrations directory exists with initial schema. `go build ./...` succeeds. Embedded FS compiles.
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** #12 (partial)

### Step 0.7: Implement CRDBStore with pgx/v5

- **Dependencies:** 0.3, 0.6
- **Description:** Implement the full `Store` interface (MemoryStore + TaskStore + AgentStore + Ping + Close) using `jackc/pgx/v5` with `pgxpool` for connection pooling. Use `cockroachdb/cockroach-go/v2/crdb/crdbpgx` for transaction retry logic on all write operations. Match existing SQLiteStore behavior exactly -- same semantics for upsert, optimistic concurrency, state machine transitions.
- **Files created:**
  - `internal/store/crdb/store.go` -- CRDBStore struct, New() constructor (accepts DATABASE_URL, runs goose migrations on startup, returns Store)
  - `internal/store/crdb/memory.go` -- MemoryStore implementation using CRDB SQL
  - `internal/store/crdb/tasks.go` -- TaskStore implementation
  - `internal/store/crdb/agents.go` -- AgentStore implementation
  - `internal/store/crdb/store_test.go` -- integration tests using `cockroach-go/v2/testserver` for ephemeral CRDB
  - `internal/store/crdb/memory_test.go` -- memory store integration tests
  - `internal/store/crdb/tasks_test.go` -- task store integration tests
  - `internal/store/crdb/agents_test.go` -- agent store integration tests
- **Files modified:**
  - `go.mod` -- add `jackc/pgx/v5`, `cockroachdb/cockroach-go/v2`
- **Key patterns:**
  - Use `$1, $2, ...` placeholders (not `?`)
  - Use `TIMESTAMPTZ` columns, scan into `time.Time`
  - Use `JSONB` for tags/capabilities, marshal/unmarshal with `pgx` custom types or manual JSON handling
  - Use `gen_random_uuid()` for new UUIDs (or generate in Go with `google/uuid`)
  - All write transactions wrapped in `crdbpgx.ExecuteTx()` for serialization retry
  - Use `pgxpool.Pool` for connection management
  - Ping() does `pool.Ping(ctx)`
  - Close() does `pool.Close()`
- **Acceptance criteria:** All CRDBStore integration tests pass against ephemeral CockroachDB. CRDBStore satisfies `store.Store` interface (compile-time check). Behavior matches SQLiteStore: same error types for not found, conflict, invalid transition.
- **Estimated scope:** L (3-5 days)
- **GitHub issue:** #12, #18

### Step 0.8: Wire DATABASE_URL into server startup, backend selection

- **Dependencies:** 0.7
- **Description:** Modify `cmd/hive-server/serve.go` to select store backend based on environment. If `DATABASE_URL` is set, use CRDBStore. Otherwise fall back to SQLite. Add startup log lines showing which backend is active. Update `/ready` endpoint to call `store.Ping()`.
- **Files modified:**
  - `cmd/hive-server/serve.go` -- add DATABASE_URL check, construct CRDBStore or SQLiteStore accordingly, add structured startup logging
  - `internal/handlers/handlers.go` -- update `/ready` handler to call `store.Ping()` (API struct needs to hold a reference to something that implements Ping, or pass it through)
- **Acceptance criteria:** Server starts with `DATABASE_URL` set and connects to CRDB. Server starts without `DATABASE_URL` and uses SQLite. Startup logs show which backend is active. `/ready` returns 503 if database is unreachable, 200 if healthy.
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** #12 (completion), #15 (partial -- healthz with db ping)

### Step 0.9: Update E2E tests for CRDB backend

- **Dependencies:** 0.7, 0.8
- **Description:** Update `test/e2e/` tests to run against both SQLite and CRDB backends. Add a test matrix or build tag. Ensure all existing E2E scenarios pass against CRDB.
- **Files modified:**
  - `test/e2e/suite_test.go` -- add CRDB backend option, use `DATABASE_URL` env var to select backend
  - Potentially add `script/test` or update existing test scripts for integration test mode
- **Acceptance criteria:** `go test ./test/e2e/ -tags e2e` passes against both SQLite and CRDB. CI can run E2E tests against CRDB (may require a CRDB service in CI).
- **Estimated scope:** M (1-2 days)
- **GitHub issue:** #13, #14, #17

### Phase 0 Parallelization

```
0.1 (rm k8s/) ─────────────────────────────────────────────── (independent)
0.2 (model pkg) ──┬── 0.3 (interfaces) ──┬── 0.7 (CRDBStore) ── 0.8 (wiring) ── 0.9 (E2E)
                  ├── 0.4 (rename cmd/)   │
                  └── 0.5 (schema fields) ┘
                                          │
0.6 (goose/migrations) ──────────────────┘
```

Steps 0.1, 0.2, and 0.6 can start in parallel. Steps 0.4 and 0.5 can run in parallel after 0.2. Step 0.7 needs both 0.3 and 0.6. Steps 0.8 and 0.9 are sequential after 0.7.

---

## Phase 1: Events, Sessions, and Error Improvements

**Goal:** Add the two new core tables that everything else depends on. Improve error messages for agent recovery. These are generic, cross-domain capabilities -- not skill-specific.

### Step 1.1: Add EventStore interface and implementation

- **Dependencies:** Phase 0 complete
- **Description:** Add events table migration for CRDB. Define EventStore interface. Implement in both SQLiteStore and CRDBStore. Events are append-only: create and list only, no update or delete. Add goose migration for CRDB. Add SQLite schema migration for local dev.
- **Files created:**
  - `internal/model/events.go` -- Event struct (ID, EventType, AgentID, SessionID, Repo, Payload as json.RawMessage, CreatedAt), EventFilter struct (Type, AgentID, SessionID, Repo, Since, Until, Limit, Offset)
  - `internal/store/crdb/migrations/002_events_sessions.sql` -- events and sessions tables
  - `internal/store/crdb/events.go` -- CRDB EventStore implementation
  - `internal/store/events.go` -- SQLite EventStore implementation (for local dev)
  - `internal/store/crdb/events_test.go` -- integration tests
  - `internal/store/events_test.go` -- SQLite unit tests
- **Files modified:**
  - `internal/store/interfaces.go` -- add EventStore interface (CreateEvent, ListEvents), embed in Store
  - `internal/store/store.go` -- SQLiteStore: add events table to schema, implement EventStore
  - `internal/store/crdb/store.go` -- CRDBStore: implement EventStore
- **Acceptance criteria:** Events can be created and listed with all filters. Both SQLite and CRDB implementations pass tests. Events are append-only (no update/delete methods).
- **Estimated scope:** M (1-2 days)
- **GitHub issue:** None (new, from vision-v3)

### Step 1.2: Add SessionStore interface and implementation

- **Dependencies:** Phase 0 complete (can parallelize with 1.1)
- **Description:** Add sessions table migration. Define SessionStore interface. Sessions have a lifecycle: created (with agent_id, repo), then completed (with summary). Implement in both store backends.
- **Files created:**
  - `internal/model/sessions.go` -- Session struct (ID, AgentID, Repo, Summary, StartedAt, CompletedAt), SessionFilter struct (AgentID, Repo, Limit, Offset)
  - `internal/store/crdb/sessions.go` -- CRDB SessionStore implementation
  - `internal/store/sessions.go` -- SQLite SessionStore implementation
  - `internal/store/crdb/sessions_test.go`
  - `internal/store/sessions_test.go`
- **Files modified:**
  - `internal/store/interfaces.go` -- add SessionStore interface (CreateSession, GetSession, ListSessions, CompleteSession), embed in Store
  - `internal/store/store.go` -- SQLiteStore: add sessions table to schema, implement SessionStore
  - `internal/store/crdb/store.go` -- CRDBStore: implement SessionStore
  - Migration file from 1.1 (002_events_sessions.sql) includes both events and sessions tables
- **Acceptance criteria:** Sessions can be created, retrieved, listed, and completed. Summary can only be set via CompleteSession. Both backends pass tests.
- **Estimated scope:** M (1-2 days)
- **GitHub issue:** None (new, from vision-v3)

### Step 1.3: Add events and sessions HTTP endpoints

- **Dependencies:** 1.1, 1.2
- **Description:** Register Huma operations for events and sessions endpoints. Follow existing patterns from memory/tasks handlers.
- **Files created:**
  - `internal/handlers/events.go` -- POST /api/v1/events (create), GET /api/v1/events (list with filters)
  - `internal/handlers/events_test.go` -- handler tests with mock store
  - `internal/handlers/sessions.go` -- POST /api/v1/sessions (create), GET /api/v1/sessions (list), GET /api/v1/sessions/{id} (get), PATCH /api/v1/sessions/{id} (complete with summary)
  - `internal/handlers/sessions_test.go` -- handler tests with mock store
- **Files modified:**
  - `internal/handlers/handlers.go` -- add `registerEvents(a, api)` and `registerSessions(a, api)` calls in routes()
- **Acceptance criteria:** All events and sessions endpoints return correct responses. Handler tests pass with mock store. E2E tests for new endpoints pass.
- **Estimated scope:** M (1-2 days)
- **GitHub issue:** None (new)

### Step 1.4: Improve error messages with recovery guidance

- **Dependencies:** Phase 0 complete (can parallelize with 1.1-1.3)
- **Description:** Audit all error responses in handlers. Add recovery guidance to every error. Include current state in state-machine errors (task status transitions). Include allowed transitions. Use Huma's error detail field (concatenate recovery text into detail since Huma uses RFC 7807 format). Touch approximately 15-20 error sites across memory.go, tasks.go, agents.go, and new handlers.
- **Files modified:**
  - `internal/handlers/memory.go` -- improve error messages for 404 (key not found, suggest prefix search), 409 (version conflict, tell agent to re-read), 422 (validation, show what is wrong and how to fix)
  - `internal/handlers/tasks.go` -- improve 422 (include current_status, current_assignee, allowed_transitions in error detail), 404 (suggest task list)
  - `internal/handlers/agents.go` -- improve error messages
  - `internal/handlers/events.go` -- add recovery guidance from the start
  - `internal/handlers/sessions.go` -- add recovery guidance from the start
  - `internal/handlers/handlers.go` -- improve 401 error to include recovery guidance (mention HIVE_TOKEN, mention auth-disabled mode)
- **Acceptance criteria:** Every error response includes: what went wrong, the current state (where applicable), and what the agent should do next. Task status errors include current_status and allowed_transitions. Tests verify error message content.
- **Estimated scope:** M (1-2 days)
- **GitHub issue:** None (new, from devex analysis)

### Step 1.5: Add structured startup logging

- **Dependencies:** 0.8
- **Description:** Improve server startup to log configured backends, connection status, and feature flags. Make it clear whether search is enabled, which database backend is active, and what port the server is listening on.
- **Files modified:**
  - `cmd/hive-server/serve.go` -- add structured log lines at startup:
    - Database backend (SQLite path or CRDB connection)
    - Auth status (enabled with token hash prefix, or disabled)
    - Search status (Meilisearch URL or "search=disabled")
    - Listen address
  - `internal/log/log.go` -- consider adding structured key-value log helpers if not already present
- **Acceptance criteria:** Starting the server produces clear log output showing all configured backends and their status. A developer can tell at a glance what is enabled and what is not.
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** None (new, from devex analysis)

### Phase 1 Parallelization

```
1.1 (EventStore) ──────┐
                        ├── 1.3 (HTTP endpoints)
1.2 (SessionStore) ────┘
1.4 (error messages) ────────── (independent)
1.5 (startup logging) ──────── (independent)
```

Steps 1.1, 1.2, 1.4, and 1.5 can all start in parallel. Step 1.3 depends on 1.1 and 1.2.

---

## Phase 2: Meilisearch Integration

**Goal:** Full-text search across all stored content. The search layer is a secondary index -- CRDB remains source of truth. If Meilisearch is down, search endpoints return 503 but all other functionality works.

### Step 2.1: Define Searcher interface and NoopSearcher

- **Dependencies:** Phase 1 complete
- **Description:** Define the search abstraction layer. The Searcher interface is backend-agnostic. NoopSearcher satisfies it by returning "service unavailable" errors. This establishes the contract before implementing Meilisearch.
- **Files created:**
  - `internal/search/interfaces.go` -- Searcher interface (Search, Index, Delete, EnsureIndex, Healthy), SearchRequest, SearchResponse, SearchHit, Document, IndexSettings types
  - `internal/search/noop.go` -- NoopSearcher that returns a typed "search unavailable" error for Search() and no-ops for Index/Delete/EnsureIndex
- **Acceptance criteria:** Searcher interface compiles. NoopSearcher satisfies it. Search() on NoopSearcher returns a recognizable error. Index/Delete are no-ops (return nil).
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** None (new)

### Step 2.2: Implement MeiliSearcher backend

- **Dependencies:** 2.1
- **Description:** Implement the Searcher interface using the Meilisearch Go SDK (`meilisearch-go`). Handle index creation, document indexing, search queries, and health checks. Translate between internal types and Meilisearch SDK types.
- **Files created:**
  - `internal/search/meili/meili.go` -- MeiliSearcher struct, constructor (accepts URL + API key), implements all Searcher methods
  - `internal/search/meili/meili_test.go` -- integration tests against a real Meilisearch instance (use build tag `//go:build meili` to skip in CI without Meilisearch)
- **Files modified:**
  - `go.mod` -- add `github.com/meilisearch/meilisearch-go`
- **Key implementation details:**
  - Search(): translate SearchRequest to Meilisearch search params, respect 10-word query limit (truncate/extract keywords)
  - Index(): use Meilisearch AddDocuments with auto-generated task, optionally wait or fire-and-forget
  - Delete(): use Meilisearch DeleteDocuments
  - EnsureIndex(): create index if not exists, update settings (searchable, filterable, sortable attributes)
  - Healthy(): call Meilisearch health endpoint
- **Acceptance criteria:** MeiliSearcher passes integration tests against a real Meilisearch instance. Documents can be indexed and searched. Health check works. 10-word limit is handled.
- **Estimated scope:** M (1-3 days)
- **GitHub issue:** None (new)

### Step 2.3: Implement SyncStore wrapper

- **Dependencies:** 2.1
- **Description:** Create a SyncStore that wraps a Store and a Searcher. On every successful write (upsert memory, create task, create event, create session, complete session), it dispatches an async indexing job to a bounded worker pool. The SyncStore itself implements the Store interface, so it is a transparent wrapper.
- **Files created:**
  - `internal/store/sync.go` -- SyncStore struct with primary Store, Searcher (may be nil/noop), bounded channel-based worker pool (not unbounded goroutines). Implements Store by delegating to primary, then dispatching index jobs.
  - `internal/store/sync_test.go` -- tests with mock store and mock searcher, verify async indexing happens
- **Key implementation details:**
  - Worker pool: use a buffered channel of fixed size (e.g., 100) with N worker goroutines (e.g., 4). If channel is full, drop the index job and log a warning (do not block the write path).
  - Each worker reads from the channel, converts the model object to a search.Document, and calls searcher.Index().
  - Use `context.Background()` with a timeout for async operations (not the request context).
  - Graceful shutdown: drain the channel on Close().
- **Acceptance criteria:** SyncStore passes write-through to primary store. Indexing jobs are dispatched asynchronously. Worker pool has bounded concurrency. Channel overflow drops gracefully with log warning. Tests verify both the primary write and the async index dispatch.
- **Estimated scope:** M (1-2 days)
- **GitHub issue:** None (new)

### Step 2.4: Define and create Meilisearch indexes

- **Dependencies:** 2.2
- **Description:** Create the index definitions for all content types. On server startup (if Meilisearch is configured), call EnsureIndex for each index with proper settings.
- **Files created:**
  - `internal/search/indexes.go` -- index definitions as constants/vars: index names, searchable/filterable/sortable attributes per index. Function `EnsureAllIndexes(ctx, Searcher) error` that calls EnsureIndex for each.
- **Index definitions (from vision-v3 Section 2.2):**
  - `memories`: searchable=[value, tags], filterable=[agent_id, repo, scope, session_id]
  - `tasks`: searchable=[title, description, tags], filterable=[status, assignee, creator, priority, repo]
  - `sessions`: searchable=[summary], filterable=[agent_id, repo]
  - `events`: searchable=[payload], filterable=[event_type, agent_id, repo]
- **Acceptance criteria:** EnsureAllIndexes creates all indexes with correct settings. Idempotent (safe to call on every startup).
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** None (new)

### Step 2.5: Implement search HTTP endpoints

- **Dependencies:** 2.1, 2.3
- **Description:** Add search endpoints that delegate to the Searcher. If Searcher is NoopSearcher (Meilisearch not configured), return 503. Add federated search across all indexes.
- **Files created:**
  - `internal/handlers/search.go` -- POST /api/v1/search/memories, POST /api/v1/search/tasks, POST /api/v1/search/sessions, POST /api/v1/search/events, POST /api/v1/search (federated: searches all indexes, merges results)
  - `internal/handlers/search_test.go` -- handler tests with mock searcher
- **Files modified:**
  - `internal/handlers/handlers.go` -- add Searcher to API struct, add to New() / Config. Register search routes. Check for nil/noop searcher and return 503 with recovery guidance.
- **Acceptance criteria:** Search endpoints return results when Meilisearch is available. Return 503 with clear message when unavailable. Federated search merges results across indexes. Handler tests pass with mock searcher.
- **Estimated scope:** M (1-2 days)
- **GitHub issue:** None (new)

### Step 2.6: Implement reconciliation job

- **Dependencies:** 2.3
- **Description:** Periodic background job that re-indexes recently changed records from CRDB to Meilisearch. Catches any records that failed async indexing. Runs every 5 minutes. Also supports a full re-index triggered by an admin endpoint.
- **Files created:**
  - `internal/search/reconciler.go` -- Reconciler struct. Runs a ticker goroutine. On each tick: query CRDB for records updated since last reconciliation timestamp, re-index them in Meilisearch. Track last reconciliation time.
  - `internal/search/reconciler_test.go`
- **Files modified:**
  - `cmd/hive-server/serve.go` -- start reconciler goroutine if Meilisearch is configured, stop on shutdown
- **Acceptance criteria:** Reconciler runs on a configurable interval. Re-indexes records that were updated since last run. Handles Meilisearch being temporarily unavailable (retries next cycle). Full re-index can be triggered.
- **Estimated scope:** M (1-2 days)
- **GitHub issue:** None (new)

### Step 2.7: Wire Meilisearch into server startup

- **Dependencies:** 2.2, 2.3, 2.4, 2.5, 2.6
- **Description:** Connect all the pieces. If `MEILI_URL` is set, create MeiliSearcher. Otherwise use NoopSearcher. Wrap store in SyncStore. Start reconciler. Pass searcher to handlers.
- **Files modified:**
  - `cmd/hive-server/serve.go` -- add MEILI_URL / MEILI_API_KEY env var handling. Construct MeiliSearcher or NoopSearcher. Wrap store in SyncStore. Call EnsureAllIndexes. Start reconciler. Log Meilisearch status at startup.
  - `internal/handlers/handlers.go` -- update New() to accept Searcher in Config
- **Acceptance criteria:** Server starts with Meilisearch configured: indexes created, writes sync to Meilisearch, search endpoints work. Server starts without Meilisearch: search returns 503, everything else works. Startup logs show search status.
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** None (new)

### Step 2.8: Implement memory injection endpoint

- **Dependencies:** 2.5, 2.7
- **Description:** The core "memory injection" endpoint that enables pre-prompt context injection. Given a prompt text and constraints, extract keywords, search across memories and sessions, rank by relevance and recency, trim to token budget, return context blocks with provenance.
- **Files created:**
  - `internal/handlers/inject.go` -- POST /api/v1/memory/inject handler
  - `internal/handlers/inject_test.go`
  - `internal/search/keywords.go` -- simple keyword extractor (split on whitespace, remove stop words, deduplicate, limit to 10 terms)
  - `internal/search/keywords_test.go` -- keyword extraction tests
  - `internal/search/ranking.go` -- merge and rank search results from multiple indexes, apply recency weighting, trim to token budget (estimate tokens as len(s)/4)
  - `internal/search/ranking_test.go`
- **Request body:**

  ```json
  {
    "agent_id": "string",
    "prompt_text": "string",
    "session_id": "string (optional)",
    "repo": "string (optional)",
    "max_tokens": 2000
  }
  ```

- **Response body:**

  ```json
  {
    "context_blocks": [
      {
        "type": "memory|session|task",
        "content": "...",
        "source": "memory:key or session:id",
        "score": 0.95,
        "reason": "matched terms: ..."
      }
    ],
    "meta": {
      "candidates_evaluated": 47,
      "candidates_returned": 3,
      "tokens_used": 891,
      "token_budget": 2000,
      "search_terms_extracted": ["auth", "login"]
    }
  }
  ```

- **Acceptance criteria:** Endpoint extracts keywords, searches across memories and sessions, ranks results, trims to token budget, returns context blocks with provenance metadata. Works with Meilisearch. Falls back to CRDB LIKE queries if Meilisearch is unavailable (degraded mode). Tests verify keyword extraction, ranking, token budgeting, and provenance metadata.
- **Estimated scope:** M (2-3 days)
- **GitHub issue:** None (new, from vision-v3 Section 4.2)

### Phase 2 Parallelization

```
2.1 (Searcher interface) ──┬── 2.2 (MeiliSearcher) ── 2.4 (indexes) ──┐
                           │                                            │
                           └── 2.3 (SyncStore) ────────────────────────┤
                                                                       │
                           2.5 (search endpoints) ────────────────────┤
                                                                       ├── 2.7 (wiring) ── 2.8 (injection)
                           2.6 (reconciler) ───────────────────────────┘
```

Steps 2.2, 2.3, 2.5, and 2.6 can start in parallel after 2.1. Step 2.4 needs 2.2. Step 2.7 needs all of 2.2-2.6. Step 2.8 needs 2.7.

---

## Phase 3: Planning and Orchestration Domain

**Goal:** Replace GSD's project management with API-backed planning. Projects, phases, plans, requirements, workflows, wave computation, plan claiming.

### Step 3.1: Add planning domain models

- **Dependencies:** Phase 1 complete (needs EventStore for recording state transitions)
- **Description:** Define all planning domain data types in the model package.
- **Files created:**
  - `internal/model/projects.go` -- Project struct (ID, Name, Repo, Description, Constraints as json.RawMessage, Config as json.RawMessage, Status, timestamps), ProjectFilter
  - `internal/model/phases.go` -- Phase struct (ID, ProjectID, PhaseNumber, Name, Goal, Status, timestamps), PhaseFilter. Status enum: pending, planning, executing, verifying, complete
  - `internal/model/plans.go` -- Plan struct (ID, PhaseID, Title, Description, Status, Assignee, DependsOn as []UUID, Metadata as json.RawMessage, timestamps), PlanFilter. Status enum: open, claimed, in_progress, complete, failed
  - `internal/model/requirements.go` -- Requirement struct (ID, ProjectID, Code, Title, Description, Category, Status, timestamps), RequirementFilter. PlanRequirement link type.
  - `internal/model/workflows.go` -- Workflow struct (ID, Type, Title, Repo, ProjectID, Status, Config, timestamps). WorkflowStage struct (ID, WorkflowID, Type, Status, StageOrder, DependsOn, Input, Output, timestamps, CompletedAt).
- **Acceptance criteria:** All types compile. JSON tags are correct. Filter types have appropriate fields.
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** None (new)

### Step 3.2: Add planning schema migrations and store implementations

- **Dependencies:** 3.1
- **Description:** Create CRDB migration for planning tables. Implement PlanningStore interface in both SQLite and CRDB backends.
- **Files created:**
  - `internal/store/crdb/migrations/003_planning.sql` -- projects, phases, plans, requirements, plan_requirements, workflows, workflow_stages tables with all indexes (from vision-v3 Section 5.1)
  - `internal/store/interfaces.go` -- add PlanningStore interface:
    - CreateProject, GetProject, ListProjects, UpdateProject
    - CreatePhase, GetPhase, ListPhases, UpdatePhase
    - CreatePlan, GetPlan, ListPlans, UpdatePlan
    - CreateRequirement, ListRequirements, UpdateRequirement
    - LinkPlanRequirement
    - CreateWorkflow, GetWorkflow, ListWorkflows
    - CreateWorkflowStage, CompleteWorkflowStage, GetCurrentStage
  - `internal/store/crdb/planning.go` -- CRDB PlanningStore implementation
  - `internal/store/planning.go` -- SQLite PlanningStore implementation
  - Tests for both backends
- **Key patterns:**
  - Plan claiming uses `UPDATE plans SET status = 'claimed', assignee = $1 WHERE id = $2 AND status = 'open'` with row count check (0 rows = already claimed, return ErrConflict)
  - Phase status transitions enforced in application layer (same pattern as task status machine)
  - Workflow stage advancement: set current stage to complete, activate next stage (by depends_on chain)
- **Acceptance criteria:** All planning CRUD operations work in both backends. Plan claiming is atomic (no double-claim). Phase/plan status transitions enforce valid state machine. Tests cover happy paths, error cases, and concurrency.
- **Estimated scope:** L (3-5 days)
- **GitHub issue:** None (new, from vision-v3 Section 4.7)

### Step 3.3: Add planning HTTP endpoints

- **Dependencies:** 3.2
- **Description:** Register Huma operations for all planning endpoints. Follow patterns from existing handlers.
- **Files created:**
  - `internal/handlers/projects.go` -- POST /api/v1/projects, GET /api/v1/projects, GET /api/v1/projects/{id}, PATCH /api/v1/projects/{id}
  - `internal/handlers/phases.go` -- POST /api/v1/projects/{id}/phases, GET /api/v1/projects/{id}/phases, PATCH /api/v1/phases/{id}
  - `internal/handlers/plans.go` -- POST /api/v1/phases/{id}/plans, GET /api/v1/phases/{id}/plans, PATCH /api/v1/plans/{id}
  - `internal/handlers/requirements.go` -- POST /api/v1/projects/{id}/requirements, GET /api/v1/projects/{id}/requirements, PATCH /api/v1/requirements/{id}
  - `internal/handlers/workflows.go` -- POST /api/v1/workflows, GET /api/v1/workflows/{id}, PATCH /api/v1/workflows/{id}/stages/{stage_id}
  - Tests for each handler file
- **Files modified:**
  - `internal/handlers/handlers.go` -- add PlanningStore to API struct and Config. Add nil-check guards (return 404 "Planning module not enabled" if nil). Register routes.
- **Acceptance criteria:** All planning endpoints work end-to-end. Handler tests pass with mock PlanningStore. Optional module: returns 404 if PlanningStore is nil.
- **Estimated scope:** L (3-5 days)
- **GitHub issue:** None (new)

### Step 3.4: Implement wave computation and orchestration endpoints

- **Dependencies:** 3.2, 3.3
- **Description:** Compute dependency waves for a phase. Given all plans in a phase with their depends_on links, compute which plans are ready (all dependencies complete), which are blocked, and what the current wave number is. Add claim/release/complete endpoints for plans.
- **Files created:**
  - `internal/planning/waves.go` -- ComputeWave function: takes list of plans, returns WaveResult (ready plans, blocked plans with their blockers, wave number)
  - `internal/planning/waves_test.go` -- test with various dependency graphs: linear chains, diamonds, independent groups, cycles (should error)
  - `internal/handlers/orchestration.go` -- POST /api/v1/phases/{id}/waves (compute next wave), GET /api/v1/phases/{id}/waves/current, POST /api/v1/plans/{id}/claim, POST /api/v1/plans/{id}/release, POST /api/v1/plans/{id}/complete
  - `internal/handlers/orchestration_test.go`
- **Key implementation:**
  - Wave computation is a topological sort: plans with no incomplete dependencies are "ready"
  - Cycle detection: if no plans are ready and some are not complete, there is a cycle -- return error
  - Plan claim: atomic status transition open -> claimed with assignee
  - Plan release: claimed -> open, clear assignee
  - Plan complete: in_progress -> complete, record completion event
- **Acceptance criteria:** Wave computation correctly identifies ready and blocked plans. Cycle detection works. Plan claim/release/complete enforce valid transitions atomically. Events recorded for state transitions.
- **Estimated scope:** M (2-3 days)
- **GitHub issue:** None (new, from vision-v3 Section 4.8)

### Step 3.5: Add project state endpoint (STATE.md replacement)

- **Dependencies:** 3.2, 3.3, 3.4
- **Description:** The GET /api/v1/projects/{id} endpoint should return comprehensive project state: current phase, current plan, progress metrics, recent decisions, blockers, last session. This replaces GSD's STATE.md with a computed, always-consistent response.
- **Files modified:**
  - `internal/handlers/projects.go` -- enhance GET /api/v1/projects/{id} response to include:
    - Current phase (latest non-complete phase)
    - Current plan (latest in_progress plan in current phase)
    - Progress (phases complete/total, plans complete/total in current phase, overall percentage)
    - Velocity metrics (plans completed, average duration -- computed from events)
    - Recent decisions (last N memories with tag "decision" for this project's repo)
    - Blockers (tasks/plans in blocked status)
    - Last session summary for this project's repo
  - `internal/handlers/projects_test.go` -- test the computed state response
- **Acceptance criteria:** GET /api/v1/projects/{id} returns a rich state object that provides everything STATE.md provided, but computed from real data. Progress percentages are accurate. Velocity is computed from event timestamps.
- **Estimated scope:** M (1-2 days)
- **GitHub issue:** None (new, from vision-v3 Section 3.1)

### Step 3.6: Index plans in Meilisearch

- **Dependencies:** 3.2, Phase 2 complete
- **Description:** Add plans to the Meilisearch index. Update SyncStore to index plans on create/update.
- **Files modified:**
  - `internal/search/indexes.go` -- add plans index definition: searchable=[title, description, tasks], filterable=[project_id, phase_id, status, assignee]
  - `internal/store/sync.go` -- add plan indexing to SyncStore wrapper
  - `internal/search/reconciler.go` -- add plans to reconciliation
- **Acceptance criteria:** Plans are indexed on create/update. Search across plans works. Reconciler re-indexes plans.
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** None (new)

### Phase 3 Parallelization

```
3.1 (models) ── 3.2 (store) ──┬── 3.3 (HTTP endpoints) ──┬── 3.5 (project state)
                               │                           │
                               └── 3.4 (waves/orch) ──────┘
                               │
                               └── 3.6 (search index) ─── (needs Phase 2)
```

Step 3.1 is a prerequisite. After 3.2, steps 3.3, 3.4, and 3.6 can proceed in parallel. Step 3.5 needs 3.3 and 3.4.

---

## Phase 4: Skills and Specs Domains

**Goal:** Replace Superpowers' skill catalog and tracking, and Allium's spec management and drift detection.

### Step 4.1: Add skills domain models and store

- **Dependencies:** Phase 1 complete (can start in parallel with Phase 3)
- **Description:** Define skill-related types. Implement SkillStore interface in both backends. Add migration.
- **Files created:**
  - `internal/model/skills.go` -- Skill struct (ID, Name, Description, Category, Content, Tags, Config, timestamps). SkillInvocation struct (ID, SkillID, AgentID, WorkflowID, Repo, DurationSeconds, Success, Notes, CreatedAt). SkillEffectiveness struct (computed: SuccessRate, AvgDuration, SampleSize, ByRepo, ByAgent). SkillFilter, InvocationFilter.
  - `internal/store/crdb/migrations/004_skills_specs.sql` -- skills, skill_invocations, specs, spec_constructs, drift_reports tables
  - `internal/store/interfaces.go` -- add SkillStore interface (RegisterSkill, GetSkill, ListSkills, DeleteSkill, RecordInvocation, ListInvocations, GetEffectiveness)
  - `internal/store/crdb/skills.go` -- CRDB implementation
  - `internal/store/skills.go` -- SQLite implementation
  - Tests for both backends
- **Key implementation:**
  - GetEffectiveness computes aggregates from skill_invocations: COUNT, AVG(duration_seconds), SUM(CASE WHEN success THEN 1 END) / COUNT(\*) as success_rate, grouped by repo and agent
  - RegisterSkill is upsert by name
- **Acceptance criteria:** Skills can be registered, retrieved, listed, deleted. Invocations can be recorded and queried. Effectiveness is computed correctly. Both backends pass tests.
- **Estimated scope:** M (2-3 days)
- **GitHub issue:** None (new, from vision-v3 Section 4.11)

### Step 4.2: Add skills HTTP endpoints and discovery

- **Dependencies:** 4.1, Phase 2 complete (needs Searcher for discovery)
- **Description:** HTTP endpoints for skill CRUD plus context-aware discovery. Discovery searches Meilisearch for skills matching a context string, then enriches results with effectiveness data.
- **Files created:**
  - `internal/handlers/skills.go` -- POST /api/v1/skills (register), GET /api/v1/skills (list), GET /api/v1/skills/{id} (get with effectiveness), DELETE /api/v1/skills/{id}, POST /api/v1/skills/discover (context-aware search)
  - `internal/handlers/skills_test.go`
- **Files modified:**
  - `internal/handlers/handlers.go` -- add SkillStore to API struct/Config, register routes
  - `internal/search/indexes.go` -- add skills index: searchable=[name, description, tags, content], filterable=[category]
  - `internal/store/sync.go` -- add skill indexing
- **Acceptance criteria:** Skill CRUD works. Discovery endpoint searches by context and returns skills ranked by relevance and effectiveness. Returns 503 if search unavailable with fallback to list-all. Tests pass.
- **Estimated scope:** M (1-2 days)
- **GitHub issue:** None (new)

### Step 4.3: Add analytics endpoints for skill invocations

- **Dependencies:** 4.1, 4.2
- **Description:** Analytics endpoints for recording and querying skill effectiveness.
- **Files created:**
  - `internal/handlers/analytics.go` -- POST /api/v1/analytics/invocations (record invocation), GET /api/v1/analytics/invocations (list with filters), GET /api/v1/analytics/skills (aggregated effectiveness across all skills)
  - `internal/handlers/analytics_test.go`
- **Files modified:**
  - `internal/handlers/handlers.go` -- register analytics routes
- **Acceptance criteria:** Invocations can be recorded and queried. Aggregated skill effectiveness returns correct stats. Filters by skill, agent, repo, time range work.
- **Estimated scope:** M (1-2 days)
- **GitHub issue:** None (new, from vision-v3 Section 4.12)

### Step 4.4: Add specs domain models and store

- **Dependencies:** Phase 1 complete (can parallelize with 4.1)
- **Description:** Define spec-related types. Implement SpecStore interface. Add migration (same migration file as skills -- 004).
- **Files created:**
  - `internal/model/specs.go` -- Spec struct (ID, Project, Name, Version, RawContent, AstJSON, timestamps). SpecConstruct struct (ID, SpecID, Type, Name, Fields, Metadata). DriftReport struct (ID, SpecID, AgentID, Mismatches as json.RawMessage, Summary as json.RawMessage, CreatedAt). DriftTrend struct (computed). SpecFilter.
  - `internal/store/interfaces.go` -- add SpecStore interface (SyncSpec, GetSpec, ListSpecs, RecordDrift, GetDriftHistory, GetDriftTrend)
  - `internal/store/crdb/specs.go` -- CRDB implementation
  - `internal/store/specs.go` -- SQLite implementation
  - Tests for both backends
- **Key implementation:**
  - SyncSpec: upsert by (project, name). On update, increment version, replace constructs (delete old, insert new).
  - GetDriftTrend: query drift_reports grouped by time bucket, compute trend (improving/worsening/stable based on mismatch count slope).
- **Acceptance criteria:** Specs can be synced, retrieved, listed. Constructs are stored and linked. Drift reports can be submitted and queried. Trend computation works. Both backends pass tests.
- **Estimated scope:** M (2-3 days)
- **GitHub issue:** None (new, from vision-v3 Section 4.10)

### Step 4.5: Add specs HTTP endpoints

- **Dependencies:** 4.4, Phase 2 complete (needs Searcher)
- **Description:** HTTP endpoints for spec management and drift tracking.
- **Files created:**
  - `internal/handlers/specs.go` -- POST /api/v1/specs (sync), GET /api/v1/specs (list), GET /api/v1/specs/{id} (get with constructs), POST /api/v1/specs/{id}/drift (submit report), GET /api/v1/specs/{id}/drift (history and trend)
  - `internal/handlers/specs_test.go`
- **Files modified:**
  - `internal/handlers/handlers.go` -- add SpecStore to API struct/Config, register routes
  - `internal/search/indexes.go` -- add specs index: searchable=[name, raw_content, constructs.name], filterable=[project, type]
  - `internal/store/sync.go` -- add spec indexing
- **Acceptance criteria:** Spec CRUD works. Drift reports can be submitted and queried with trend. Specs are indexed in Meilisearch for search. Tests pass.
- **Estimated scope:** M (1-2 days)
- **GitHub issue:** None (new)

### Step 4.6: Add cross-project drift analytics

- **Dependencies:** 4.3, 4.5
- **Description:** Add a drift summary endpoint that shows drift trends across all specs in all projects.
- **Files modified:**
  - `internal/handlers/analytics.go` -- add GET /api/v1/analytics/drift (cross-project summary)
  - `internal/handlers/analytics_test.go`
- **Acceptance criteria:** Drift analytics endpoint returns aggregated drift stats across all specs. Filterable by project, time range.
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** None (new)

### Phase 4 Parallelization

```
4.1 (skills store) ── 4.2 (skills HTTP) ── 4.3 (analytics)
                                                    │
4.4 (specs store) ── 4.5 (specs HTTP) ─────────── 4.6 (drift analytics)
```

Steps 4.1 and 4.4 can start in parallel. Steps 4.2 and 4.5 can proceed in parallel after their respective stores. Step 4.3 depends on 4.2. Step 4.6 depends on 4.3 and 4.5.

---

## Phase 5: Gel DB Integration

**Goal:** Graph queries for relationships, dependencies, and impact analysis. Gel provides capabilities that SQL cannot serve efficiently: multi-hop traversals, back-links, computed properties.

### Step 5.1: Define GraphStore interface and NoopGraphStore

- **Dependencies:** Phase 3 complete (needs planning entities to model in graph)
- **Description:** Define the graph abstraction layer. GraphStore is for relationship queries that SQL handles poorly. NoopGraphStore returns empty results or falls back to SQL-based approximations.
- **Files created:**
  - `internal/graph/interfaces.go` -- GraphStore interface:
    - SyncProject, SyncPhase, SyncPlan, SyncTask, SyncRequirement (write/update graph nodes)
    - SyncSpec, SyncConstruct (spec graph nodes)
    - GetRequirementTraceability(projectID) -- project -> phase -> plan -> task -> requirement chain
    - GetSpecImpact(entityName, project) -- entity -> rules -> surfaces -> contracts
    - GetBlockedPlans(phaseID) -- plans blocked by incomplete dependencies with cycle detection
    - GetUnmetRequirements(projectID) -- requirements without implementing plans in "complete" status
  - `internal/graph/noop.go` -- NoopGraphStore: returns empty results, allows callers to fall back to SQL queries
- **Acceptance criteria:** GraphStore interface compiles. NoopGraphStore satisfies it. All methods return empty/nil without error.
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** k8s#58 (partial)

### Step 5.2: Create Gel schema and implement GelStore

- **Dependencies:** 5.1
- **Description:** Create the Gel SDL schema mirroring the relational model. Implement GelStore using the gel-go driver.
- **Files created:**
  - `internal/graph/gel/schema.esdl` -- Gel schema (from vision-v3 Section 5.3): Project, Phase, Plan, Task, Requirement, Spec, Construct, Skill, Agent types with all links
  - `internal/graph/gel/store.go` -- GelStore struct, constructor (accepts GEL_DSN), implements GraphStore
  - `internal/graph/gel/store_test.go` -- integration tests (use build tag `//go:build gel`)
- **Files modified:**
  - `go.mod` -- add gel-go driver
- **Key implementation:**
  - Sync methods: upsert nodes by external ID (CRDB UUID). Use EdgeQL INSERT ... UNLESS CONFLICT ON .external_id.
  - Traceability: `SELECT Project { phases: { plans: { tasks, implements: { title, status } } } } FILTER .external_id = <uuid>$id`
  - Impact: `SELECT Construct { spec: { name }, .<affects_entities[IS Construct] { name, type } } FILTER .name = <str>$name`
  - Blocked plans: traverse depends_on links, find plans where any dependency is not complete
- **Acceptance criteria:** GelStore passes integration tests against a running Gel instance. All graph queries return correct results. Sync operations are idempotent.
- **Estimated scope:** L (3-5 days)
- **GitHub issue:** k8s#58

### Step 5.3: Add graph sync to SyncStore and reconciler

- **Dependencies:** 5.2, Phase 2 complete
- **Description:** Extend SyncStore to dispatch graph sync jobs alongside search index jobs. Extend reconciler to re-sync graph nodes.
- **Files modified:**
  - `internal/store/sync.go` -- add GraphStore field (may be nil). On writes, dispatch graph sync jobs to worker pool.
  - `internal/search/reconciler.go` -- add graph reconciliation alongside search reconciliation
- **Acceptance criteria:** Graph nodes are synced on every write. Reconciler catches missed syncs. GraphStore being nil is handled gracefully.
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** None (new)

### Step 5.4: Add graph-dependent HTTP endpoints

- **Dependencies:** 5.2, 5.3
- **Description:** Endpoints that use graph queries. These either use Gel if available or fall back to SQL-based approximations (recursive CTEs or multiple queries).
- **Files modified:**
  - `internal/handlers/projects.go` -- enhance GET /api/v1/projects/{id} to include requirement traceability from graph (if GraphStore available)
  - `internal/handlers/requirements.go` -- add GET /api/v1/projects/{id}/requirements/unmet (graph query for unmet requirements)
- **Files created:**
  - `internal/handlers/specs.go` -- add GET /api/v1/specs/{id}/impact (impact analysis via graph traversal)
  - `internal/handlers/orchestration.go` -- enhance wave computation to use graph for blocked plan detection with cycle analysis
- **Acceptance criteria:** Graph endpoints return correct results when Gel is available. Return 404 or degraded results when Gel is unavailable. Impact analysis traverses cross-spec relationships. Tests cover both graph-available and graph-unavailable scenarios.
- **Estimated scope:** M (2-3 days)
- **GitHub issue:** k8s#58 (partial)

### Step 5.5: Wire Gel into server startup

- **Dependencies:** 5.2, 5.3, 5.4
- **Description:** Connect Gel to server startup. If `GEL_DSN` is set, create GelStore. Otherwise use NoopGraphStore.
- **Files modified:**
  - `cmd/hive-server/serve.go` -- add GEL_DSN env var handling. Construct GelStore or NoopGraphStore. Pass to SyncStore and handlers. Log status.
  - `internal/handlers/handlers.go` -- add GraphStore to API struct/Config
- **Acceptance criteria:** Server starts with Gel configured: graph nodes sync, graph endpoints work. Server starts without Gel: graph endpoints return appropriate fallback. Startup logs show graph status.
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** k8s#58 (completion)

### Phase 5 Parallelization

```
5.1 (interface) ── 5.2 (GelStore) ──┬── 5.3 (sync) ── 5.5 (wiring)
                                     │
                                     └── 5.4 (HTTP endpoints)
```

Mostly sequential. Step 5.4 can start in parallel with 5.3 after 5.2 is done.

---

## Phase 6: Production Hardening and Velocity Analytics

**Goal:** Operational readiness. Rate limiting, request size limits, metrics, backup procedures, velocity analytics.

### Step 6.1: Add velocity analytics endpoint

- **Dependencies:** Phase 3 complete (needs planning data), Phase 1 complete (needs events)
- **Description:** Compute velocity metrics from events and task/plan completion data. Plans completed per day, average plan duration, trend over time.
- **Files modified:**
  - `internal/handlers/analytics.go` -- add GET /api/v1/analytics/velocity (query: project, time_range). Compute from events table: count task.completed events, average duration between task.claimed and task.completed, group by day/week.
  - `internal/handlers/analytics_test.go`
- **Acceptance criteria:** Velocity endpoint returns plans_completed, avg_duration, velocity_per_day. Filterable by project and time range. Handles no data gracefully (returns zeros, not errors).
- **Estimated scope:** M (1-2 days)
- **GitHub issue:** None (new)

### Step 6.2: Add request body size limits and field validation

- **Dependencies:** Phase 1 complete
- **Description:** Add middleware for request body size limiting. Add field-level validation: memory value max 64KB, task title max 500 chars, etc. Return clear validation errors with recovery guidance.
- **Files modified:**
  - `internal/handlers/handlers.go` -- add body size limit middleware (e.g., 1MB max)
  - `internal/handlers/memory.go` -- add Huma validation for value field size
  - `internal/handlers/tasks.go` -- add validation for title/description size
  - `internal/handlers/sessions.go` -- add validation for summary size
- **Acceptance criteria:** Requests exceeding body size limit get 413. Fields exceeding max size get 422 with clear error. Tests verify limits.
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** None (new)

### Step 6.3: Add Prometheus metrics endpoint

- **Dependencies:** Phase 1 complete
- **Description:** Add /metrics endpoint with Prometheus-compatible metrics: request count, latency histogram, error rate, database connection pool stats, Meilisearch sync lag.
- **Files created:**
  - `internal/metrics/metrics.go` -- define Prometheus metrics (using prometheus/client_golang)
  - Middleware for recording request metrics
- **Files modified:**
  - `internal/handlers/handlers.go` -- add metrics middleware, register /metrics endpoint (no auth)
  - `go.mod` -- add prometheus/client_golang
  - `cmd/hive-server/serve.go` -- register metrics collectors for connection pool, etc.
- **Acceptance criteria:** /metrics returns Prometheus-format metrics. Request count, latency, and error rate are tracked per endpoint. Database stats are exposed.
- **Estimated scope:** M (1-2 days)
- **GitHub issue:** None (new)

### Step 6.4: Add request audit logging middleware

- **Dependencies:** Phase 1 complete
- **Description:** Log every request with method, path, status code, latency, agent_id, and request_id. Existing chi middleware.RequestID is already in use; add a response-logging middleware that captures the response status.
- **Files modified:**
  - `internal/handlers/handlers.go` -- add response logging middleware after RequestID in the chain
- **Acceptance criteria:** Every request produces a log line with method, path, status, latency_ms, agent_id, request_id. Log format is structured (JSON or key=value).
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** None (new)

### Step 6.5: Add bulk memory operations

- **Dependencies:** Phase 2 complete
- **Description:** Add POST /api/v1/memory/bulk endpoint for batch upsert. Accepts up to 100 entries. Each entry is processed independently; partial failures return which entries succeeded and which failed.
- **Files created:**
  - `internal/handlers/memory.go` -- add POST /api/v1/memory/bulk handler
  - Add tests
- **Acceptance criteria:** Bulk upsert accepts up to 100 entries. Returns success/failure per entry. Exceeding 100 returns 422. Each entry is indexed in Meilisearch.
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** None (new, from devex analysis)

### Step 6.6: Add scripts-to-rule-them-all

- **Dependencies:** None (independent, can do at any point)
- **Description:** Add standardized development scripts.
- **Files created:**
  - `script/bootstrap` -- install dependencies (go mod download, check for required tools)
  - `script/setup` -- one-time setup (create local config, initialize database)
  - `script/test` -- run unit tests (`go test ./...`)
  - `script/integration/test` -- run integration tests (start CRDB testserver, run with build tags)
  - `script/server` -- build and run the server locally
- **Acceptance criteria:** A new developer can clone the repo and run `script/bootstrap && script/setup && script/test` to get a working development environment.
- **Estimated scope:** S (< 1 day)
- **GitHub issue:** #11

### Phase 6 Parallelization

All steps in Phase 6 are independent and can be done in any order or in parallel.

```
6.1 (velocity) ── (independent)
6.2 (limits) ──── (independent)
6.3 (metrics) ─── (independent)
6.4 (audit log) ─ (independent)
6.5 (bulk ops) ── (independent)
6.6 (scripts) ─── (independent)
```

---

## Dependency Summary

```
Phase 0: Foundation (CockroachDB + refactor)
    |
    v
Phase 1: Events + Sessions + Error Messages
    |
    +----> Phase 2: Meilisearch Integration
    |         |
    |         v
    +----> Phase 3: Planning + Orchestration ──> Phase 5: Gel DB
    |         |
    +----> Phase 4: Skills + Specs ───────────> Phase 5: Gel DB
    |
    +----> Phase 6: Hardening (mostly independent, can interleave)
```

Phase 0 is the foundation. Phase 1 adds core tables. Phases 2, 3, and 4 can partially overlap. Phase 5 needs Phases 3 and 4 for entities to model in the graph. Phase 6 is independent and can be interleaved anywhere after Phase 1.

---

## Issue Mapping

| GitHub Issue                | Build Plan Steps                    | Status After Completion                                              |
| --------------------------- | ----------------------------------- | -------------------------------------------------------------------- |
| #10 (rm k8s/)               | 0.1                                 | Closed                                                               |
| #11 (scripts)               | 6.6                                 | Closed                                                               |
| #12 (CRDB migration)        | 0.6, 0.7, 0.8                       | Closed                                                               |
| #13 (update tests for CRDB) | 0.9                                 | Closed                                                               |
| #14 (ephemeral CRDB tests)  | 0.7, 0.9                            | Closed                                                               |
| #15 (k8s deploy for CRDB)   | 0.8 (healthz)                       | Partially addressed (healthz done, k8s manifests managed externally) |
| #16 (Huma v2)               | Already done                        | Already closed (verify)                                              |
| #17 (E2E tests)             | 0.9                                 | Closed (existing E2E tests updated for CRDB)                         |
| #18 (tx retries)            | 0.7                                 | Closed (crdbpgx.ExecuteTx in CRDBStore)                              |
| #20 (project layout)        | 0.2, 0.3, 0.4                       | Closed                                                               |
| #9 (Discovery API)          | 4.2 (skill discovery), 2.5 (search) | Partially addressed                                                  |
| k8s#58 (Gel deploy)         | 5.1-5.5                             | Addressed                                                            |

---

## Timeline Estimate

| Phase   | Steps   | Est. Duration        | Cumulative |
| ------- | ------- | -------------------- | ---------- |
| Phase 0 | 0.1-0.9 | 2-3 weeks            | Week 3     |
| Phase 1 | 1.1-1.5 | 1-2 weeks            | Week 5     |
| Phase 2 | 2.1-2.8 | 2-3 weeks            | Week 8     |
| Phase 3 | 3.1-3.6 | 2-3 weeks            | Week 11    |
| Phase 4 | 4.1-4.6 | 2-3 weeks            | Week 14    |
| Phase 5 | 5.1-5.5 | 2-3 weeks            | Week 17    |
| Phase 6 | 6.1-6.6 | 1 week (interleaved) | Ongoing    |

**Total estimated duration:** 14-17 weeks for a single developer with LLM assistance, accounting for the skeptic's observation that integration work takes 2-3x longer than expected.

---

## Step Count Summary

| Phase     | Steps        | Endpoints Added                                                                        |
| --------- | ------------ | -------------------------------------------------------------------------------------- |
| Phase 0   | 9 steps      | 0 (infrastructure)                                                                     |
| Phase 1   | 5 steps      | 6 (events: 2, sessions: 4)                                                             |
| Phase 2   | 8 steps      | 6 (search: 5, inject: 1)                                                               |
| Phase 3   | 6 steps      | 21 (projects: 4, phases: 3, plans: 3, requirements: 3, workflows: 3, orchestration: 5) |
| Phase 4   | 6 steps      | 13 (skills: 5, specs: 5, analytics: 3)                                                 |
| Phase 5   | 5 steps      | 3 (graph-enhanced existing endpoints, impact analysis)                                 |
| Phase 6   | 6 steps      | 4 (velocity: 1, bulk: 1, metrics: 1, existing endpoint enhancements)                   |
| **Total** | **45 steps** | **53 new endpoints** (+ 14 existing = 67 total)                                        |

---

## What Is Explicitly Out of Scope

1. **MCP plugin (hive-plugin, hive-local)** -- the API is designed for it, but the plugin is a separate project
2. **Modifying GSD, Superpowers, or Allium source code** -- hive-server provides the backend; skill modifications are separate projects
3. **MasterClaw (in-cluster LLM for synthesis)** -- deferred until search proves insufficient
4. **Vector/embedding search** -- explicit decision: no GPU, no embeddings
5. **Rate limiting at the API level** -- Phase 6 adds request size limits; rate limiting can be done at the ingress layer
6. **Multi-tenancy** -- single-tenant (one developer, multiple agents) for now. Multi-tenant is a future concern.
7. **SQLite removal** -- SQLite stays for local dev and fast tests even after CRDB is primary
