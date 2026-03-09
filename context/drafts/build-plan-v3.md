# Build Plan v3: Behavioral Knowledge Engine

**Date:** 2026-03-09
**Status:** Implementable build plan for vision-v4
**Inputs:** vision-v4.md, directive-schema.md, injection-pipeline.md, skill-replacement-analysis.md, hive-server-current.md, github-issues.md, cockroachdb.md, meilisearch.md, gel-db.md, build-plan-v2.md, final-review-v2.md, ultrathink-skeptic.md, ultrathink-architect.md, ultrathink-ops.md
**Verified against:** actual codebase on 2026-03-09

---

## Current State (verified by code inspection)

- Single Go binary, ~28 Go files across `cmd/app/`, `internal/handlers/`, `internal/store/`, `internal/log/`, `test/e2e/`
- **Huma v2 already integrated** (handlers use `humachi.New`, register operations via `huma.Register`)
- **pgx already integrated** (`github.com/jackc/pgx/v5/stdlib` via `database/sql`). Store connects to PostgreSQL/CockroachDB.
- Schema uses `JSONB`, `BIGSERIAL`, `TEXT` timestamps -- partially migrated to CockroachDB idioms but still using TEXT for timestamps and TEXT for UUIDs.
- chi v5 router with auth middleware (`HIVE_TOKEN` bearer), agent ID injection (`X-Agent-ID` header)
- Store is a **concrete struct**; handlers define their own `Store` interface (13 methods)
- Data models live in `internal/store/` (MemoryEntry, Task, Agent, etc.) -- no separate `internal/model/` package
- Tests exist for handlers (mock-based) and store (SQL-based)
- E2E tests exist in `test/e2e/`
- CI: pre-commit + go test on PRs; Release: semantic-release + goreleaser
- `cmd/app/` is the entrypoint (not yet refactored to `cmd/hive-server/`)
- No `internal/model/` package, no `internal/server/` package
- No Meilisearch, no Gel DB, no directives table, no injection pipeline, no feedback loop
- No `k8s/` directory (already removed per issue #10)

**Key observation:** CockroachDB migration is partially done. The store uses pgx and JSONB but retains SQLite-era patterns (TEXT timestamps, TEXT UUIDs, no `TIMESTAMPTZ`, no `gen_random_uuid()`). The foundation is closer than build-plan-v2 assumed.

---

## Existing Functionality: KEPT vs MODIFIED vs REPLACED

| Current Feature               | Disposition  | Rationale                                                                                                                    |
| ----------------------------- | ------------ | ---------------------------------------------------------------------------------------------------------------------------- |
| Memory CRUD (4 endpoints)     | **KEPT**     | Still needed. Agents store and retrieve arbitrary key-value data. Becomes source of `factual` directives over time.          |
| Task CRUD (5 endpoints)       | **KEPT**     | Task coordination between agents is orthogonal to the directive engine.                                                      |
| Agent heartbeat (3 endpoints) | **KEPT**     | Agent presence tracking remains useful for multi-agent coordination.                                                         |
| Health / Ready probes         | **KEPT**     | Unchanged.                                                                                                                   |
| Bearer token auth             | **KEPT**     | Unchanged. Token auth applies to all endpoints including new ones.                                                           |
| X-Agent-ID header middleware  | **MODIFIED** | Currently optional. Required for `POST /api/v1/inject` and `POST /api/v1/feedback`.                                          |
| SQLite schema patterns        | **REPLACED** | TEXT timestamps become TIMESTAMPTZ. TEXT UUIDs become UUID with gen_random_uuid(). Proper CockroachDB idioms.                |
| Monolithic Store interface    | **MODIFIED** | Split into composed interfaces (MemoryStore, TaskStore, AgentStore, DirectiveStore, etc.) per ultrathink-architect guidance. |

The existing API does not disappear. It is enhanced with directive-engine endpoints alongside it.

---

## Phase 0: Foundation

**Goal:** Clean up project layout, finish CockroachDB idiom migration, establish the model package and composed store interfaces that everything downstream depends on.

### Step 0.1: Project Layout Refactor

- **Step ID:** 0.1
- **Dependencies:** None
- **Scope:** M (1-3 days)

**What gets built:**

1. Rename `cmd/app/` to `cmd/hive-server/`:

   - Move `cmd/app/main.go` to `cmd/hive-server/main.go`
   - Move `cmd/app/serve.go` to `cmd/hive-server/serve.go`
   - Update `Dockerfile`, `.goreleaser.yaml` build paths
   - Update CI workflows if they reference `cmd/app/`

2. Create `internal/model/` package with domain types extracted from `internal/store/`:

   - `internal/model/memory.go` -- `MemoryEntry`, `MemoryFilter`
   - `internal/model/task.go` -- `Task`, `TaskNote`, `TaskFilter`, `TaskUpdate`, `TaskStatus` enum
   - `internal/model/agent.go` -- `Agent`, `AgentStatus` enum
   - `internal/model/errors.go` -- `ErrNotFound`, `ErrConflict`, `ErrInvalidTransition`
   - Each type keeps its current fields and JSON tags

3. Update `internal/store/` to import from `internal/model/` instead of defining types locally:

   - `store.go`, `memory.go`, `tasks.go`, `agents.go` all import `model` package
   - Method signatures reference `*model.MemoryEntry`, `model.MemoryFilter`, etc.

4. Update `internal/handlers/handlers.go` Store interface to reference `model` types:

   - Replace `store.MemoryEntry` with `model.MemoryEntry` etc.
   - Update all handler files and test files

5. Create `internal/server/` package:
   - `internal/server/server.go` -- HTTP server setup (extracted from `cmd/hive-server/serve.go`). Contains `Server` struct with `Start(ctx)`, `Shutdown(ctx)` methods, signal handling.

**Tests:**

- All existing tests pass after refactor (no behavioral changes)
- `go build ./cmd/hive-server/` succeeds
- `go test ./...` passes

**Acceptance criteria:**

- `cmd/hive-server/main.go` is the entrypoint
- `internal/model/` package exists with all domain types
- `internal/store/` imports from `model`, not the reverse
- `internal/handlers/` imports from `model` for types
- `internal/server/` package exists with extracted server logic
- All existing tests pass
- CI builds and tests pass
- Addresses GitHub issue #20

---

### Step 0.2: CockroachDB Schema Hardening

- **Step ID:** 0.2
- **Dependencies:** 0.1
- **Scope:** S (< 1 day)

**What gets built:**

1. Update `internal/store/store.go` schema to use proper CockroachDB types:

   - `created_at TEXT` becomes `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
   - `updated_at TEXT` becomes `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`
   - `tasks.id TEXT` becomes `tasks.id UUID NOT NULL PRIMARY KEY DEFAULT gen_random_uuid()`
   - `task_notes.id BIGSERIAL` stays (acceptable for CockroachDB)
   - `agents.last_heartbeat TEXT` becomes `TIMESTAMPTZ`
   - `agents.registered_at TEXT` becomes `TIMESTAMPTZ`

2. Update Go code to use `time.Time` natively instead of string formatting:

   - Remove all `time.Now().UTC().Format(time.RFC3339Nano)` calls in store methods
   - Remove all `time.Parse(time.RFC3339Nano, ...)` calls
   - Let pgx handle `time.Time` <-> `TIMESTAMPTZ` natively
   - Update `model` types: all timestamp fields become `time.Time`

3. Add CockroachDB transaction retry wrapper:

   - `internal/store/tx.go` -- `RetryTx(ctx, db, fn)` helper that wraps `crdbpgx.ExecuteTx` logic for `database/sql`
   - Add `github.com/cockroachdb/cockroach-go/v2/crdb` dependency
   - Wrap all multi-statement store operations in RetryTx
   - Addresses GitHub issue #18

4. Migration strategy: Since CockroachDB supports `CREATE TABLE IF NOT EXISTS` and `ALTER TABLE`, add an `ALTER`-based migration block after the `CREATE` block that upgrades existing TEXT columns to TIMESTAMPTZ.

**Tests:**

- `internal/store/store_test.go` -- verify schema creates cleanly
- `internal/store/memory_test.go` -- verify timestamps are `time.Time` in returned structs
- `internal/store/tasks_test.go` -- verify UUID generation for task IDs
- All handler tests pass (they mock the store, so unaffected by schema changes)

**Acceptance criteria:**

- All timestamp columns are `TIMESTAMPTZ` in the schema DDL
- All Go model types use `time.Time` for timestamps
- No manual `time.Parse` or `time.Format` calls in store code
- Task IDs are UUID, generated by the database
- `crdb` transaction retry wrapper exists and is used for multi-statement transactions
- Addresses GitHub issues #12 (partial) and #18

---

### Step 0.3: Composed Store Interfaces

- **Step ID:** 0.3
- **Dependencies:** 0.1
- **Scope:** S (< 1 day)

**What gets built:**

1. Define composed store interfaces in `internal/model/store.go`:

```go
package model

type MemoryStore interface {
    UpsertMemory(ctx context.Context, entry *MemoryEntry) (*MemoryEntry, error)
    GetMemory(ctx context.Context, key string) (*MemoryEntry, error)
    ListMemory(ctx context.Context, f MemoryFilter) ([]*MemoryEntry, error)
    DeleteMemory(ctx context.Context, key string) error
}

type TaskStore interface {
    CreateTask(ctx context.Context, t *Task) (*Task, error)
    GetTask(ctx context.Context, id string) (*Task, error)
    ListTasks(ctx context.Context, f TaskFilter) ([]*Task, error)
    UpdateTask(ctx context.Context, id string, upd TaskUpdate) (*Task, error)
    DeleteTask(ctx context.Context, id string) error
}

type AgentStore interface {
    Heartbeat(ctx context.Context, id string, capabilities []string, status AgentStatus) (*Agent, error)
    GetAgent(ctx context.Context, id string) (*Agent, error)
    ListAgents(ctx context.Context) ([]*Agent, error)
}

// Store composes all sub-stores for backwards compatibility.
// Handlers that need all stores can depend on this.
// Handlers that need only one store depend on the sub-interface.
type Store interface {
    MemoryStore
    TaskStore
    AgentStore
    Ping(ctx context.Context) error
    Close() error
}
```

2. Update `internal/handlers/handlers.go`:

   - Remove the local `Store` interface definition
   - Import `model.Store` (or the specific sub-interface per handler group)
   - `API.store` field type becomes `model.Store`

3. Verify the concrete `store.Store` struct satisfies `model.Store` via compile-time check:
   - Add `var _ model.Store = (*Store)(nil)` in `internal/store/store.go`

**Tests:**

- Compile-time interface satisfaction check
- All existing handler tests pass with the new interface

**Acceptance criteria:**

- Store interfaces live in `internal/model/store.go`
- Handler package imports interfaces from `model`, not defining its own
- Concrete store satisfies the composed interface (compile-time verified)
- All tests pass

---

## Phase 1: Directive Storage

**Goal:** Implement the directive data model in CockroachDB, basic CRUD operations, and the ingest endpoint that accepts raw text documents for future decomposition.

### Step 1.1: Directive Schema and Store

- **Step ID:** 1.1
- **Dependencies:** 0.2, 0.3
- **Scope:** M (1-3 days)

**What gets built:**

1. Add directive model types to `internal/model/directive.go`:

```go
package model

type DirectiveType string
const (
    DirectiveBehavioral DirectiveType = "behavioral"
    DirectiveProcedural DirectiveType = "procedural"
    DirectiveContextual DirectiveType = "contextual"
    DirectiveGuardrail  DirectiveType = "guardrail"
)

type Directive struct {
    ID                   string         `json:"id"`
    Content              string         `json:"content"`
    Rationale            string         `json:"rationale"`
    DirectiveType        DirectiveType  `json:"directive_type"`
    SourceSkill          string         `json:"source_skill"`
    SourceSection        string         `json:"source_section"`
    SourceTextHash       string         `json:"source_text_hash"`
    ContextTriggers      map[string]any `json:"context_triggers"`
    VerificationCriteria string         `json:"verification_criteria"`
    EffectivenessScore   float64        `json:"effectiveness_score"`
    Priority             int            `json:"priority"`
    Version              int            `json:"version"`
    SupersedesID         *string        `json:"supersedes_id,omitempty"`
    IsActive             bool           `json:"is_active"`
    DecompositionRunID   string         `json:"decomposition_run_id"`
    CreatedAt            time.Time      `json:"created_at"`
    UpdatedAt            time.Time      `json:"updated_at"`
    Tags                 []string       `json:"tags,omitempty"` // populated by JOINs
}

type DirectiveFilter struct {
    DirectiveType *DirectiveType
    SourceSkill   *string
    IsActive      *bool
    Tags          []string
    Limit         int
    Offset        int
}

type DecompositionRun struct {
    ID               string    `json:"id"`
    SourceSkill      string    `json:"source_skill"`
    SourceSection    string    `json:"source_section"`
    SourceDocument   string    `json:"source_document"`
    SourceTextHash   string    `json:"source_text_hash"`
    ModelUsed        string    `json:"model_used"`
    PromptVersion    string    `json:"prompt_version"`
    DirectivesCreated int      `json:"directives_created"`
    CreatedAt        time.Time `json:"created_at"`
}
```

2. Add `DirectiveStore` interface to `internal/model/store.go`:

```go
type DirectiveStore interface {
    CreateDirective(ctx context.Context, d *Directive) (*Directive, error)
    GetDirective(ctx context.Context, id string) (*Directive, error)
    ListDirectives(ctx context.Context, f DirectiveFilter) ([]*Directive, error)
    UpdateDirective(ctx context.Context, id string, upd DirectiveUpdate) (*Directive, error)
    DeleteDirective(ctx context.Context, id string) error
    SetDirectiveTags(ctx context.Context, directiveID string, tags []string) error
    CreateDecompositionRun(ctx context.Context, run *DecompositionRun) (*DecompositionRun, error)
    GetDecompositionRun(ctx context.Context, id string) (*DecompositionRun, error)
}
```

3. Add CockroachDB schema for directives in `internal/store/store.go` migration:

   - `decomposition_runs` table (per directive-schema.md)
   - `directives` table (per directive-schema.md, using CREATE TYPE for enums)
   - `directive_tags` table
   - `directive_relationships` table
   - `directive_feedback` table
   - `directive_sets` and `directive_set_members` tables
   - All indexes from directive-schema.md

4. Implement `internal/store/directives.go`:

   - `CreateDirective` -- INSERT with RETURNING
   - `GetDirective` -- SELECT with LEFT JOIN on directive_tags
   - `ListDirectives` -- parameterized query with filters, JOIN tags
   - `UpdateDirective` -- UPDATE with version increment
   - `DeleteDirective` -- soft delete (SET is_active = false)
   - `SetDirectiveTags` -- DELETE + batch INSERT for tags
   - `CreateDecompositionRun` / `GetDecompositionRun`

5. Update `model.Store` interface to compose `DirectiveStore`.

**Tests:**

- `internal/store/directives_test.go`:
  - TestCreateDirective -- creates and retrieves
  - TestGetDirective_NotFound -- returns ErrNotFound
  - TestListDirectives_FilterByType -- filters by directive_type
  - TestListDirectives_FilterByTags -- filters by tags via JOIN
  - TestUpdateDirective_VersionIncrement -- version goes up
  - TestDeleteDirective_SoftDelete -- sets is_active=false
  - TestSetDirectiveTags -- replaces tags correctly
  - TestCreateDecompositionRun -- creates and retrieves run

**Acceptance criteria:**

- All directive tables exist in the schema
- CRUD operations work for directives, tags, decomposition runs
- Soft delete works (is_active flag, not physical delete)
- Filtering by type, skill, active status, and tags works
- All tests pass against CockroachDB

---

### Step 1.2: Directive CRUD Handlers

- **Step ID:** 1.2
- **Dependencies:** 1.1
- **Scope:** M (1-3 days)

**What gets built:**

1. `internal/handlers/directives.go` -- Huma-registered handlers:

   - `POST /api/v1/directives` -- Create a directive (admin/ingestion use)
   - `GET /api/v1/directives` -- List directives with filters (type, skill, active, tags, limit, offset)
   - `GET /api/v1/directives/{id}` -- Get single directive with tags
   - `PATCH /api/v1/directives/{id}` -- Update directive fields (content, priority, active status, triggers)
   - `DELETE /api/v1/directives/{id}` -- Soft-delete a directive

2. `POST /api/v1/ingest` -- Ingest endpoint:

   - Accepts `{ "source_name": "superpowers:brainstorming", "source_type": "skill", "content": "<full markdown text>" }`
   - Creates a `decomposition_run` record with the source hash
   - Stores the raw content for later decomposition (Phase 6)
   - Returns the run ID and status `"pending_decomposition"`
   - For now, this is a storage-only endpoint. The actual LLM decomposition is Phase 6.

3. Wire new handlers into `internal/handlers/handlers.go`:
   - `registerDirectives(a, api)` called in the authenticated router group

**Tests:**

- `internal/handlers/directives_test.go`:
  - TestCreateDirective_Success
  - TestCreateDirective_MissingContent -- 422 validation error
  - TestListDirectives_Empty -- returns empty array
  - TestListDirectives_WithFilters
  - TestGetDirective_NotFound -- 404
  - TestUpdateDirective_Success
  - TestDeleteDirective_Success
  - TestIngest_CreatesRun
  - TestIngest_DuplicateHash -- detects re-ingestion of same content

**Acceptance criteria:**

- All directive CRUD endpoints work via Huma with OpenAPI docs generated
- Ingest endpoint accepts raw text and creates a decomposition run
- Duplicate content detection works via source_text_hash
- All existing endpoints continue working (no regressions)

---

## Phase 2: Meilisearch Integration

**Goal:** Add Meilisearch as a search layer for directives. Sync directives from CockroachDB to Meilisearch. Expose a search endpoint.

### Step 2.1: Meilisearch Client and Search Interface

- **Step ID:** 2.1
- **Dependencies:** 1.1
- **Scope:** M (1-3 days)

**What gets built:**

1. Add `github.com/meilisearch/meilisearch-go` dependency.

2. Create `internal/search/` package:
   - `internal/search/search.go` -- `Searcher` interface:

```go
package search

type SearchResult struct {
    DirectiveID string
    Content     string
    Score       float64
    Source      string
    Category    string
    Priority    int
}

type SearchRequest struct {
    Query          string
    Activity       string
    ProjectName    string
    Language       string
    Limit          int
    MinScore       float64
}

type Searcher interface {
    SearchDirectives(ctx context.Context, req SearchRequest) ([]SearchResult, error)
    IndexDirective(ctx context.Context, d *model.Directive) error
    IndexDirectives(ctx context.Context, ds []*model.Directive) error
    RemoveDirective(ctx context.Context, id string) error
    ConfigureIndex(ctx context.Context) error
    Healthy(ctx context.Context) bool
}
```

- `internal/search/noop.go` -- `NoopSearcher` that returns empty results (for when Meilisearch is unavailable or disabled):

```go
type NoopSearcher struct{}
func (n *NoopSearcher) SearchDirectives(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
    return nil, nil
}
// ... all methods return nil/empty
```

- `internal/search/meili.go` -- `MeiliSearcher` implementing `Searcher`:
  - Constructor takes Meilisearch host + API key
  - `ConfigureIndex` sets up the `directives` index with searchable/filterable/sortable attributes per directive-schema.md Section 3
  - `IndexDirective` / `IndexDirectives` transforms `model.Directive` to Meilisearch document format (flattening context_triggers into top-level fields: `activity_tags`, `workflow_stages`, `trigger_keywords`)
  - `SearchDirectives` builds query from SearchRequest, applies activity/project/language filters, returns scored results
  - `RemoveDirective` removes document from index
  - `Healthy` calls Meilisearch `/health` endpoint

3. Configuration via environment:
   - `MEILI_URL` -- Meilisearch host (default: empty, meaning disabled)
   - `MEILI_API_KEY` -- Meilisearch master key
   - When `MEILI_URL` is empty, `NoopSearcher` is used (graceful degradation)

**Tests:**

- `internal/search/noop_test.go` -- verify NoopSearcher returns empty results without error
- `internal/search/meili_test.go` -- integration test (build-tagged `//go:build integration`):
  - TestConfigureIndex -- creates index with correct settings
  - TestIndexAndSearch -- indexes a directive, searches for it, finds it
  - TestSearchFilters -- activity and project filters work
  - TestRemoveDirective -- removes from index
  - TestHealthy -- returns true when Meilisearch is up

**Acceptance criteria:**

- `Searcher` interface exists with Meilisearch and Noop implementations
- Meilisearch index configuration matches directive-schema.md
- Directives can be indexed and searched
- Graceful degradation when Meilisearch is unavailable
- Integration tests pass against a real Meilisearch instance

---

### Step 2.2: CockroachDB-to-Meilisearch Sync

- **Step ID:** 2.2
- **Dependencies:** 2.1
- **Scope:** M (1-3 days)

**What gets built:**

1. `internal/search/sync.go` -- Sync logic:

   - `SyncManager` struct holding both `DirectiveStore` and `Searcher`
   - `SyncDirective(ctx, directiveID)` -- fetch from CRDB, index in Meilisearch
   - `SyncAll(ctx)` -- full rebuild: list all active directives from CRDB, batch-index into Meilisearch
   - `ReconcileLoop(ctx, interval)` -- background goroutine that runs every N minutes:
     - Query CRDB for directives updated since last sync timestamp
     - Re-index updated directives in Meilisearch
     - Remove deactivated directives from Meilisearch
     - Default interval: 5 minutes

2. Hook sync into directive write paths:

   - After `CreateDirective` succeeds in CRDB, fire `go syncManager.SyncDirective(ctx, id)` asynchronously
   - After `UpdateDirective` succeeds, fire async sync
   - After `DeleteDirective` (soft delete) succeeds, fire async remove from Meilisearch
   - Use `context.Background()` for async fire-and-forget (sync failure does not fail the HTTP request)

3. Add `POST /api/v1/admin/sync` endpoint (admin-only) to trigger a full re-sync manually.

4. Add sync status to `/ready` health check:
   - `/ready` returns `{"status": "ready", "meilisearch": "connected"}` or `{"status": "ready", "meilisearch": "unavailable"}`

**Tests:**

- `internal/search/sync_test.go`:
  - TestSyncDirective -- creates in CRDB, syncs to Meilisearch, searchable
  - TestSyncAll -- full rebuild indexes all directives
  - TestReconcileLoop -- updated directive gets re-indexed within interval
  - TestSyncFailure_DoesNotBlockWrite -- CRDB write succeeds even if Meilisearch is down

**Acceptance criteria:**

- New/updated directives appear in Meilisearch search results (eventual consistency, typically < 1 second)
- Full re-sync can be triggered manually
- Reconciliation loop catches any missed updates
- Meilisearch failures do not block CockroachDB writes
- Health endpoint reports Meilisearch connectivity

---

## Phase 3: Injection Pipeline

**Goal:** Build the core `POST /api/v1/inject` endpoint -- the primary value delivery mechanism. Fan-out queries to CockroachDB and Meilisearch, rank, select within token budget, return directives.

### Step 3.1: Injection Pipeline Core

- **Step ID:** 3.1
- **Dependencies:** 1.1, 2.1
- **Scope:** L (3-5 days)

**What gets built:**

1. Add injection model types to `internal/model/inject.go`:

```go
package model

type InjectRequest struct {
    SessionID           string         `json:"session_id"`
    Activity            string         `json:"activity"`
    Project             ProjectContext `json:"project"`
    Context             AgentContext   `json:"context"`
    Intent              string         `json:"intent,omitempty"`
    TokenBudget         int            `json:"token_budget,omitempty"`
    PreviousInjectionID string         `json:"previous_injection_id,omitempty"`
}

type ProjectContext struct {
    Name     string `json:"name"`
    Language string `json:"language,omitempty"`
    Path     string `json:"path,omitempty"`
}

type AgentContext struct {
    Summary     string   `json:"summary"`
    RecentFiles []string `json:"recent_files,omitempty"`
    RecentTools []string `json:"recent_tools,omitempty"`
    ErrorContext string  `json:"error_context,omitempty"`
}

type InjectionResponse struct {
    InjectionID         string               `json:"injection_id"`
    Directives          []InjectedDirective  `json:"directives"`
    TokensUsed          int                  `json:"tokens_used"`
    TokenBudget         int                  `json:"token_budget"`
    CandidatesConsidered int                 `json:"candidates_considered"`
    CandidatesSelected  int                  `json:"candidates_selected"`
}

type InjectedDirective struct {
    ID         string  `json:"id"`
    Content    string  `json:"content"`
    Category   string  `json:"category"`
    Source     string  `json:"source"`
    Confidence float64 `json:"confidence"`
}
```

2. Create `internal/inject/` package:
   - `internal/inject/pipeline.go` -- `Pipeline` struct:

```go
type Pipeline struct {
    store    model.DirectiveStore
    searcher search.Searcher
    // gel will be added in Phase 5
}

func NewPipeline(store model.DirectiveStore, searcher search.Searcher) *Pipeline

func (p *Pipeline) Inject(ctx context.Context, agentID string, req *model.InjectRequest) (*model.InjectionResponse, error)
```

- `internal/inject/retrieve.go` -- Fan-out retrieval:

  - `retrieveFromMeilisearch(ctx, req) ([]candidate, error)` -- builds search query from context.summary + intent, searches directive index
  - `retrieveFromCRDB(ctx, req) ([]candidate, error)` -- runs structured queries:
    - Query 1: Active directives matching activity (trigger phase)
    - Query 2: Active directives matching project scope
    - Query 3: Recently injected directive IDs for deduplication
  - Fan-out uses `errgroup.Group` with per-source timeouts (150ms Meilisearch, 200ms CRDB)

- `internal/inject/rank.go` -- Ranking and selection:

  - Merge candidates from all sources, deduplicate by directive ID
  - Score each candidate: `score = (relevance * 0.35) + (priority * 0.25) + (freshness * 0.15) + (source_boost * 0.15) + (feedback * 0.10)` per injection-pipeline.md Section 3.1
  - Token budget packing: greedy algorithm with category minimum slots (1 guardrail minimum)
  - `EstimateTokens(content string) int` -- `len(content) / 4` heuristic

- `internal/inject/recompose.go` -- Template-based recomposition:
  - Variable substitution: `{{package}}`, `{{test_command}}` per injection-pipeline.md Section 4
  - Language-aware command substitution
  - Fallback to static_content when variables unresolvable
  - No LLM call at runtime (per injection-pipeline.md Section 4.1 decision)

3. Add injection log tables to schema:

```sql
CREATE TABLE IF NOT EXISTS injection_log (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      TEXT        NOT NULL,
    agent_id        TEXT        NOT NULL,
    context_hash    TEXT        NOT NULL,
    directive_ids   JSONB       NOT NULL DEFAULT '[]',
    tokens_used     INT4        NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_injection_log_session ON injection_log(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_injection_log_agent ON injection_log(agent_id, created_at DESC);
```

4. `internal/handlers/inject.go` -- Huma handler:

   - `POST /api/v1/inject` -- validates request, calls Pipeline.Inject, returns response
   - X-Agent-ID header is required (not optional)
   - Default token_budget: 500, min: 100, max: 2000
   - Logs the injection in injection_log table

5. Wire into router: `registerInject(a, api)` in handlers.go

**Tests:**

- `internal/inject/pipeline_test.go`:
  - TestInject_EmptyDirectives -- returns empty when no directives exist
  - TestInject_ReturnsRelevantDirectives -- seeds directives, injects, gets results
  - TestInject_RespectsTokenBudget -- does not exceed budget
  - TestInject_DeduplicatesPreviousInjection -- excludes recently injected
  - TestInject_GuardrailMinSlot -- at least 1 guardrail when available
  - TestInject_MeilisearchDown_FallsBackToCRDB -- degraded but functional
- `internal/inject/rank_test.go`:
  - TestRankCandidates -- verifies scoring formula
  - TestTokenBudgetPacking -- verifies greedy packing
  - TestDeduplication -- same directive from 2 sources merged
- `internal/inject/recompose_test.go`:
  - TestRecompose_GoProject -- substitutes go test command
  - TestRecompose_UnresolvedVariable_FallsBack -- uses static_content
- `internal/handlers/inject_test.go`:
  - TestInjectEndpoint_Success
  - TestInjectEndpoint_MissingAgentID -- 400 error
  - TestInjectEndpoint_InvalidActivity -- 422 validation error

**Acceptance criteria:**

- `POST /api/v1/inject` returns contextually relevant directives
- Fan-out to Meilisearch + CockroachDB with timeouts and graceful degradation
- Ranking produces sensible ordering (guardrails first, then by score)
- Token budget is respected
- Template variables are resolved per language/project context
- Injection is logged for session deduplication
- Pipeline completes within 400ms for typical requests

---

## Phase 4: Feedback Loop

**Goal:** Track directive outcomes, update effectiveness scores, enable experience-derived directives from session completion.

### Step 4.1: Feedback Endpoint and Effectiveness Scoring

- **Step ID:** 4.1
- **Dependencies:** 3.1
- **Scope:** M (1-3 days)

**What gets built:**

1. Add feedback model types to `internal/model/feedback.go`:

```go
type FeedbackOutcome string
const (
    OutcomeFollowed          FeedbackOutcome = "followed"
    OutcomeIgnored           FeedbackOutcome = "ignored"
    OutcomePartiallyFollowed FeedbackOutcome = "partially_followed"
    OutcomeInapplicable      FeedbackOutcome = "inapplicable"
    OutcomeHelpful           FeedbackOutcome = "helpful"
    OutcomeUnhelpful         FeedbackOutcome = "unhelpful"
)

type FeedbackRequest struct {
    InjectionID    string            `json:"injection_id"`
    Outcomes       []DirectiveOutcome `json:"outcomes"`
    SessionOutcome string            `json:"session_outcome,omitempty"`
    SessionSummary string            `json:"session_summary,omitempty"`
}

type DirectiveOutcome struct {
    DirectiveID string          `json:"directive_id"`
    Outcome     FeedbackOutcome `json:"outcome"`
    Evidence    string          `json:"evidence,omitempty"`
}

type SessionCompleteRequest struct {
    SessionID   string `json:"session_id"`
    Summary     string `json:"summary"`
    Repo        string `json:"repo,omitempty"`
    Outcome     string `json:"outcome"`
    KeyInsight  string `json:"key_insight,omitempty"`
}
```

2. Add `FeedbackStore` interface to `internal/model/store.go`:

```go
type FeedbackStore interface {
    RecordFeedback(ctx context.Context, directiveID string, agentID string, sessionID string, outcome FeedbackOutcome, evidence string) error
    UpdateEffectiveness(ctx context.Context, directiveID string) error
    RecordSessionComplete(ctx context.Context, req *SessionCompleteRequest, agentID string) error
    GetDirectiveEffectiveness(ctx context.Context, directiveID string) (float64, error)
}
```

3. Implement `internal/store/feedback.go`:

   - `RecordFeedback` -- INSERT into directive_feedback
   - `UpdateEffectiveness` -- recalculate effectiveness_score from feedback history (per directive-schema.md function `update_effectiveness_score`)
   - `RecordSessionComplete` -- INSERT into a new `session_completions` table
   - `GetDirectiveEffectiveness` -- read the computed score

4. Add `session_completions` table to schema:

```sql
CREATE TABLE IF NOT EXISTS session_completions (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id  TEXT        NOT NULL,
    agent_id    TEXT        NOT NULL,
    summary     TEXT        NOT NULL DEFAULT '',
    repo        TEXT        NOT NULL DEFAULT '',
    outcome     TEXT        NOT NULL DEFAULT '',
    key_insight TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_session_completions_session ON session_completions(session_id);
CREATE INDEX IF NOT EXISTS idx_session_completions_repo ON session_completions(repo, created_at DESC);
```

5. `internal/handlers/feedback.go` -- Huma handlers:

   - `POST /api/v1/feedback` -- record directive outcomes from an injection, update effectiveness scores
   - `POST /api/v1/feedback/session-complete` -- record session completion with summary and key insight

6. Wire into router: `registerFeedback(a, api)` in handlers.go

**Tests:**

- `internal/store/feedback_test.go`:
  - TestRecordFeedback_CreatesRecord
  - TestUpdateEffectiveness_CalculatesCorrectly -- 3 followed + 1 ignored = 0.75
  - TestRecordSessionComplete_CreatesRecord
- `internal/handlers/feedback_test.go`:
  - TestFeedbackEndpoint_Success
  - TestFeedbackEndpoint_InvalidOutcome -- 422
  - TestSessionCompleteEndpoint_Success

**Acceptance criteria:**

- `POST /api/v1/feedback` records outcomes and updates effectiveness scores
- `POST /api/v1/feedback/session-complete` records session summaries
- Effectiveness scoring formula: `(followed + helpful) / total_feedback`
- Effectiveness score is reflected in subsequent inject calls (higher-scoring directives rank higher)
- All tests pass

---

## Phase 5: Gel DB Integration

**Goal:** Add Gel DB as the relationship graph layer for behavioral chains and directive traversal. Sync from CockroachDB. Enable chain-based retrieval in the injection pipeline.

### Step 5.1: Gel DB Schema and Client

- **Step ID:** 5.1
- **Dependencies:** 1.1
- **Scope:** M (1-3 days)

**What gets built:**

1. Add `github.com/geldata/gel-go` dependency.

2. Create `dbschema/` directory at project root with Gel SDL files:
   - `dbschema/default.esdl` -- Gel schema per vision-v4.md Section 5.3:

```sdl
module default {
    type Directive {
        required content: str;
        required kind: str;
        required source_name: str;
        required weight: float64 { default := 1.0; };
        required effectiveness: float64 { default := 0.0; };
        required token_cost: int32 { default := 0; };
        required active: bool { default := true; };
        crdb_id: uuid;

        multi related_to: Directive;
        multi superseded_by: Directive;
        link chain: DirectiveChain;

        property influence_score := .weight * .effectiveness;
    }

    type DirectiveChain {
        required name: str { constraint exclusive; };
        required description: str;
        multi members: Directive {
            property sequence_order: int32;
        };
        property total_tokens := sum(.members.token_cost);
        property avg_effectiveness := math::mean(.members.effectiveness);
    }

    type Source {
        required name: str { constraint exclusive; };
        required source_type: str;
        multi produced: Directive;
        property directive_count := count(.produced);
        property avg_effectiveness := math::mean(.produced.effectiveness);
    }
}
```

3. Create `internal/graph/` package:
   - `internal/graph/graph.go` -- `GraphStore` interface:

```go
type GraphStore interface {
    GetChainForDirective(ctx context.Context, directiveID string) (*model.DirectiveChain, error)
    GetRelatedDirectives(ctx context.Context, directiveID string, depth int) ([]*model.Directive, error)
    SyncDirective(ctx context.Context, d *model.Directive) error
    SyncChain(ctx context.Context, chain *model.DirectiveChain) error
    Healthy(ctx context.Context) bool
}
```

- `internal/graph/noop.go` -- `NoopGraphStore` that returns nil/empty (graceful degradation when Gel is unavailable)
- `internal/graph/gel.go` -- Gel DB implementation:
  - Constructor takes Gel connection options
  - `GetChainForDirective` -- EdgeQL query to traverse from a directive to its chain and chain members (per vision-v4.md Section 5.3)
  - `GetRelatedDirectives` -- EdgeQL query traversing `related_to` links
  - `SyncDirective` -- UPSERT directive into Gel (keyed by crdb_id)
  - `SyncChain` -- UPSERT chain with members

4. Configuration via environment:
   - `GEL_DSN` -- Gel connection string (default: empty, meaning disabled)
   - When empty, `NoopGraphStore` is used

**Tests:**

- `internal/graph/noop_test.go` -- verify NoopGraphStore returns nil without error
- `internal/graph/gel_test.go` -- integration test (`//go:build integration`):
  - TestSyncDirective -- syncs from CRDB model to Gel
  - TestGetChainForDirective -- creates chain, retrieves it via directive
  - TestGetRelatedDirectives -- creates related directives, traverses links

**Acceptance criteria:**

- Gel schema exists in `dbschema/default.esdl`
- GraphStore interface with Gel and Noop implementations
- Directives can be synced from CockroachDB to Gel
- Chain traversal works via EdgeQL
- Graceful degradation when Gel is unavailable

---

### Step 5.2: Graph-Enhanced Injection Pipeline

- **Step ID:** 5.2
- **Dependencies:** 3.1, 5.1
- **Scope:** M (1-3 days)

**What gets built:**

1. Update `internal/inject/pipeline.go`:

   - `Pipeline` struct gains a `graph graph.GraphStore` field
   - Constructor: `NewPipeline(store, searcher, graph)` -- graph can be NoopGraphStore

2. Add `internal/inject/retrieve.go`:

   - New retrieval source: `retrieveFromGel(ctx, foundIDs) ([]candidate, error)`
   - Given directive IDs found by Meilisearch and CRDB, query Gel for:
     - Behavioral chains containing those directives (get the next step)
     - Related directives (1-hop traversal on `related_to` links)
   - Timeout: 200ms (per injection-pipeline.md Section 2.3)

3. Update fan-out in `Pipeline.Inject`:

   - Phase 1: Parallel fan-out to Meilisearch + CRDB (existing)
   - Phase 2: After Phase 1 completes, fan-out to Gel with the found directive IDs
   - Gel results are added as candidates with `source_boost = 0.7` (per injection-pipeline.md)
   - Total pipeline deadline: 400ms + 150ms buffer

4. Degradation: Gel is the lowest priority. If it times out or is unavailable, pipeline returns results from Meilisearch + CRDB only.

**Tests:**

- `internal/inject/pipeline_test.go` (extend existing):
  - TestInject_WithGelChain -- directive in a chain returns the next step
  - TestInject_GelUnavailable_StillWorks -- pipeline degrades gracefully
  - TestInject_GelAddsRelatedDirectives -- related directives appear in results

**Acceptance criteria:**

- Injection pipeline queries Gel as a third source when available
- Chain traversal adds "next step" directives to candidates
- Related directives boost diversity of results
- Gel timeout does not block the pipeline
- All previous injection tests still pass

---

## Phase 6: Decomposition Pipeline

**Goal:** Implement LLM-powered ingestion -- when a skill document is ingested, decompose it into atomic directives using an LLM.

### Step 6.1: LLM Client Abstraction

- **Step ID:** 6.1
- **Dependencies:** 1.2
- **Scope:** M (1-3 days)

**What gets built:**

1. Create `internal/llm/` package:
   - `internal/llm/llm.go` -- `LLMClient` interface:

```go
type Message struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

type CompletionRequest struct {
    Messages    []Message
    Model       string
    MaxTokens   int
    Temperature float64
}

type CompletionResponse struct {
    Content    string
    TokensUsed int
    Model      string
}

type LLMClient interface {
    Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
}
```

- `internal/llm/anthropic.go` -- Anthropic API client implementation:

  - Uses `net/http` for the Anthropic Messages API (`https://api.anthropic.com/v1/messages`)
  - Supports `claude-sonnet-4-20250514` (or configurable model)
  - Handles API key auth, rate limiting headers, error responses
  - No external SDK dependency (raw HTTP for minimal deps)

- `internal/llm/noop.go` -- `NoopLLMClient` that returns an error (decomposition cannot work without an LLM, so this is an explicit failure mode, not silent degradation)

2. Configuration via environment:
   - `ANTHROPIC_API_KEY` -- required for decomposition
   - `LLM_MODEL` -- model to use (default: `claude-sonnet-4-20250514`)
   - When API key is empty, `NoopLLMClient` is used and ingest returns `"decomposition_unavailable"`

**Tests:**

- `internal/llm/anthropic_test.go`:
  - TestComplete_Success -- mock HTTP server returns valid response
  - TestComplete_APIError -- handles 400/500 responses
  - TestComplete_Timeout -- respects context deadline
- `internal/llm/noop_test.go` -- returns error

**Acceptance criteria:**

- LLMClient interface exists with Anthropic and Noop implementations
- Anthropic client handles auth, errors, and timeouts
- No external LLM SDK dependency (raw HTTP)
- Configuration via environment variables

---

### Step 6.2: Decomposition Engine

- **Step ID:** 6.2
- **Dependencies:** 6.1, 1.1, 2.2
- **Scope:** L (3-5 days)

**What gets built:**

1. Create `internal/decompose/` package:
   - `internal/decompose/decompose.go` -- `Engine` struct:

```go
type Engine struct {
    llm   llm.LLMClient
    store model.DirectiveStore
    sync  *search.SyncManager
}

func NewEngine(llm llm.LLMClient, store model.DirectiveStore, sync *search.SyncManager) *Engine

func (e *Engine) Decompose(ctx context.Context, run *model.DecompositionRun, content string) ([]*model.Directive, error)
```

- `internal/decompose/section.go` -- Deterministic sectioning:

  - `SplitIntoSections(content string) []Section`
  - Splits on markdown headings (##, ###)
  - Splits on numbered lists
  - Each section retains its heading context and position in the document
  - No LLM needed for sectioning

- `internal/decompose/extract.go` -- LLM-based directive extraction:

  - `ExtractDirectives(ctx, section Section) ([]RawDirective, error)`
  - Sends section content to LLM with the decomposition prompt (per vision-v4.md Section 3.3)
  - Parses JSON response into `[]RawDirective`
  - Each RawDirective has: content, kind, trigger_tags, trigger_intent, trigger_phase, related_indices

- `internal/decompose/enrich.go` -- Post-extraction enrichment:

  - Token cost estimation via `len(content) / 4`
  - Tag derivation from trigger fields
  - Weight assignment (anti-rationalization directives get 1.5x weight)
  - Source provenance attachment

- `internal/decompose/dedup.go` -- Deduplication:
  - For each extracted directive, search Meilisearch for similar existing directives
  - If similarity score > 0.8, merge: keep the most specific version, preserve both sources
  - If a directive from multiple independent sources is found, boost weight

2. Update `POST /api/v1/ingest` handler:

   - After creating the decomposition_run, check if LLM is available
   - If available, run decomposition asynchronously (fire-and-forget goroutine)
   - Add `GET /api/v1/ingest/{run_id}` endpoint to check decomposition status and see results
   - Decomposition run status: `pending` -> `processing` -> `completed` / `failed`

3. The decomposition prompt (stored as a constant in `internal/decompose/prompt.go`):
   - Per vision-v4.md Section 3.3: "You are a behavioral knowledge extractor..."
   - Requests JSON array of directives with specific fields
   - Version the prompt (e.g., `decompose-v1`) and store version in decomposition_run

**Tests:**

- `internal/decompose/section_test.go`:
  - TestSplitIntoSections_MarkdownHeadings
  - TestSplitIntoSections_NumberedList
  - TestSplitIntoSections_EmptyDocument
- `internal/decompose/extract_test.go`:
  - TestExtractDirectives_ParsesJSON -- mock LLM returns valid JSON
  - TestExtractDirectives_InvalidJSON -- handles malformed response
  - TestExtractDirectives_EmptySection -- returns empty
- `internal/decompose/enrich_test.go`:
  - TestTokenCostEstimation
  - TestWeightAssignment_AntiRationalization
- `internal/decompose/dedup_test.go`:
  - TestDedup_ExactMatch -- merges identical directives
  - TestDedup_SimilarContent -- merges semantically similar
  - TestDedup_NoMatch -- creates new
- `internal/decompose/decompose_test.go` (integration):
  - TestDecompose_EndToEnd -- full pipeline from markdown to stored directives

**Acceptance criteria:**

- Skill documents can be ingested and decomposed into atomic directives
- Sectioning is deterministic (no LLM)
- LLM extraction produces valid directive structures
- Deduplication prevents redundant directives from multiple skill sources
- Decomposition runs are tracked with status
- Results are synced to Meilisearch after creation
- A typical skill document (2000 tokens) produces 15-30 directives

---

## Phase 7: Hardening

**Goal:** Production readiness -- rate limiting, metrics, structured logging, audit logging, and backup infrastructure.

### Step 7.1: Observability and Rate Limiting

- **Step ID:** 7.1
- **Dependencies:** 3.1, 4.1
- **Scope:** M (1-3 days)

**What gets built:**

1. Add structured logging throughout:

   - Replace `internal/log/log.go` with `log/slog` (standard library, Go 1.21+)
   - Add request logging middleware: method, path, status, duration, agent_id
   - Add injection pipeline timing logs: meilisearch_ms, crdb_ms, gel_ms, total_ms, candidates, selected
   - Log feedback recording: directive_id, outcome, agent_id

2. Add Prometheus metrics endpoint:

   - `GET /metrics` -- standard Prometheus exposition format
   - Metrics:
     - `hive_http_requests_total{method, path, status}` -- counter
     - `hive_http_request_duration_seconds{method, path}` -- histogram
     - `hive_inject_duration_seconds` -- histogram for injection pipeline
     - `hive_inject_candidates_total` -- histogram for candidates considered
     - `hive_inject_selected_total` -- histogram for candidates selected
     - `hive_directives_total{kind, source_skill, active}` -- gauge
     - `hive_feedback_total{outcome}` -- counter
     - `hive_meilisearch_healthy` -- gauge (1 or 0)
     - `hive_gel_healthy` -- gauge (1 or 0)

3. Add rate limiting middleware:

   - Per-agent rate limit on `POST /api/v1/inject` (default: 60 requests/minute)
   - Per-agent rate limit on `POST /api/v1/feedback` (default: 120 requests/minute)
   - Use `golang.org/x/time/rate` (token bucket)
   - Rate limit state in-memory (acceptable for single-instance; CockroachDB-backed for multi-instance future)
   - Return `429 Too Many Requests` with `Retry-After` header

4. Add audit logging for admin operations:
   - `POST /api/v1/ingest` -- log source_name, content_hash, agent_id
   - `DELETE /api/v1/directives/{id}` -- log directive_id, agent_id
   - `POST /api/v1/admin/sync` -- log trigger source
   - Audit log goes to structured logging output (not a separate table -- keep it simple)

**Tests:**

- Metrics endpoint returns valid Prometheus format
- Rate limiting returns 429 after threshold
- Structured log output contains expected fields

**Acceptance criteria:**

- All API requests are logged with structured fields
- Injection pipeline timing is logged and metriced
- Prometheus metrics endpoint works
- Rate limiting prevents abuse of inject/feedback endpoints
- Audit trail for admin operations exists in logs

---

### Step 7.2: Backup and Data Integrity

- **Step ID:** 7.2
- **Dependencies:** 2.2, 5.1
- **Scope:** S (< 1 day)

**What gets built:**

1. `POST /api/v1/admin/backup` endpoint:

   - Triggers Meilisearch dump via API
   - Returns dump task ID
   - CockroachDB backups are handled at the infrastructure level (not in application code) but document the `BACKUP` SQL command in `script/backup`

2. `script/backup` shell script:

   - Triggers CockroachDB backup (documents the command)
   - Triggers Meilisearch dump
   - Intended for cron/scheduled execution

3. Data integrity checks:
   - `POST /api/v1/admin/integrity` -- compares directive counts between CockroachDB and Meilisearch, reports discrepancies
   - If discrepancies found, returns `{"status": "drift", "crdb_count": N, "meili_count": M, "action": "run /admin/sync"}`

**Tests:**

- Integrity check detects when Meilisearch is missing directives
- Backup endpoint triggers Meilisearch dump

**Acceptance criteria:**

- Backup script exists and documents the process
- Integrity check detects CockroachDB/Meilisearch drift
- Admin endpoints are wired and functional

---

## Dependency Graph Summary

```
Phase 0: Foundation
  0.1 (project layout)          -- independent, start immediately
    -> 0.2 (CRDB hardening)     -- depends on 0.1
    -> 0.3 (composed interfaces) -- depends on 0.1
       (0.2 and 0.3 are parallelizable after 0.1)

Phase 1: Directive Storage
  1.1 (directive schema/store)  -- depends on 0.2, 0.3
    -> 1.2 (directive handlers) -- depends on 1.1

Phase 2: Meilisearch
  2.1 (search interface)        -- depends on 1.1
    -> 2.2 (CRDB-Meili sync)   -- depends on 2.1

Phase 3: Injection Pipeline
  3.1 (inject endpoint)         -- depends on 1.1, 2.1

Phase 4: Feedback Loop
  4.1 (feedback + scoring)      -- depends on 3.1

Phase 5: Gel DB
  5.1 (gel schema + client)     -- depends on 1.1 (parallelizable with Phase 2-3)
    -> 5.2 (graph-enhanced inject) -- depends on 3.1, 5.1

Phase 6: Decomposition
  6.1 (LLM client)              -- depends on 1.2 (parallelizable with Phase 2-5)
    -> 6.2 (decomposition engine) -- depends on 6.1, 1.1, 2.2

Phase 7: Hardening
  7.1 (observability + rate limit) -- depends on 3.1, 4.1
  7.2 (backup + integrity)        -- depends on 2.2, 5.1
  (7.1 and 7.2 are parallelizable)
```

### Parallelization Opportunities

Within the dependency constraints:

1. **After 0.1 completes:** 0.2 and 0.3 can run in parallel.
2. **After 1.1 completes:** 2.1 (Meilisearch) and 5.1 (Gel) can run in parallel.
3. **After 1.2 completes:** 6.1 (LLM client) can start in parallel with Phase 2-3.
4. **After 3.1 completes:** 4.1 (feedback) and 5.2 (graph-inject) can run in parallel.
5. **After 4.1 completes:** 7.1 and 7.2 can run in parallel.

---

## API Surface Summary

The complete API after all phases:

| Method | Path                                | Phase    | Description                                    |
| ------ | ----------------------------------- | -------- | ---------------------------------------------- |
| GET    | `/health`                           | existing | Health check                                   |
| GET    | `/ready`                            | 2.2      | Ready check (enhanced with Meilisearch status) |
| GET    | `/metrics`                          | 7.1      | Prometheus metrics                             |
| POST   | `/api/v1/memory`                    | existing | Create/update memory entry                     |
| GET    | `/api/v1/memory`                    | existing | List memory entries                            |
| GET    | `/api/v1/memory/{key}`              | existing | Get memory entry                               |
| DELETE | `/api/v1/memory/{key}`              | existing | Delete memory entry                            |
| POST   | `/api/v1/tasks`                     | existing | Create task                                    |
| GET    | `/api/v1/tasks`                     | existing | List tasks                                     |
| GET    | `/api/v1/tasks/{id}`                | existing | Get task                                       |
| PATCH  | `/api/v1/tasks/{id}`                | existing | Update task                                    |
| DELETE | `/api/v1/tasks/{id}`                | existing | Delete task                                    |
| POST   | `/api/v1/agents/{id}/heartbeat`     | existing | Agent heartbeat                                |
| GET    | `/api/v1/agents`                    | existing | List agents                                    |
| GET    | `/api/v1/agents/{id}`               | existing | Get agent                                      |
| POST   | `/api/v1/directives`                | 1.2      | Create directive                               |
| GET    | `/api/v1/directives`                | 1.2      | List directives                                |
| GET    | `/api/v1/directives/{id}`           | 1.2      | Get directive                                  |
| PATCH  | `/api/v1/directives/{id}`           | 1.2      | Update directive                               |
| DELETE | `/api/v1/directives/{id}`           | 1.2      | Soft-delete directive                          |
| POST   | `/api/v1/ingest`                    | 1.2/6.2  | Ingest skill document                          |
| GET    | `/api/v1/ingest/{run_id}`           | 6.2      | Check decomposition status                     |
| POST   | `/api/v1/inject`                    | 3.1      | Get contextual directives (THE core endpoint)  |
| POST   | `/api/v1/feedback`                  | 4.1      | Report directive outcomes                      |
| POST   | `/api/v1/feedback/session-complete` | 4.1      | Report session completion                      |
| POST   | `/api/v1/admin/sync`                | 2.2      | Trigger full Meilisearch re-sync               |
| POST   | `/api/v1/admin/backup`              | 7.2      | Trigger backup                                 |
| POST   | `/api/v1/admin/integrity`           | 7.2      | Check cross-database consistency               |

**Total: ~28 endpoints (15 existing + 13 new)**

This is not a 66-endpoint CRUD API. It is a focused behavioral knowledge engine with ~9 new functional endpoints (directive CRUD, ingest, inject, feedback x2, ingest status) and ~4 admin/ops endpoints.

---

## Lessons Learned from Previous Plans (and how this plan addresses them)

### From final-review-v2.md

1. **"Vision says replace skills, analysis says they are 60-85% prompt engineering"** -- v4 resolves this. Skills are not replaced. They are _decomposed into directives_. The prompt engineering wisdom is extracted, atomized, and contextually reinjected. The skills become training data, not deprecated software.

2. **"Build plan steps not aligned with actual codebase"** -- This plan was verified against the actual codebase on 2026-03-09. Huma v2 is already integrated. pgx is already the driver. The plan does not re-do completed work.

3. **"Monolithic Store interface grows to 32+ methods"** -- Step 0.3 splits the Store interface into composed sub-interfaces from the start.

### From ultrathink-skeptic.md

1. **"Build the CLI tool first"** -- The MCP plugin (hive-plugin, hive-local) already exists and is out of scope. The consumer side exists. This plan builds what the consumer calls.

2. **"Meilisearch is not free"** -- Acknowledged. This plan uses NoopSearcher as default when Meilisearch is unavailable. The injection pipeline works (degraded) with CRDB-only results. Meilisearch is a value-add, not a hard dependency.

3. **"33-step plan is too many steps"** -- This plan has 13 steps across 8 phases. Steps are meatier and more self-contained.

4. **"Cold start problem"** -- The ingest endpoint + decomposition pipeline (Phase 6) solves cold start by letting users seed the directive database from existing skill documents on day one.

### From ultrathink-architect.md

1. **"Store interface should be composed"** -- Done in Step 0.3.
2. **"Model types should be in their own package"** -- Done in Step 0.1.
3. **"Fan-out queries need timeouts and graceful degradation"** -- Specified in Step 3.1 with per-source timeouts.

### From ultrathink-ops.md

1. **"Health checks should report dependency status"** -- Step 2.2 enhances `/ready` with Meilisearch status.
2. **"Meilisearch sync failures should not block writes"** -- Step 2.2 uses async fire-and-forget sync with reconciliation loop.
3. **"Monitor injection pipeline latency"** -- Step 7.1 adds Prometheus histograms for pipeline timing.

---

## Estimated Timeline

Assuming one developer with LLM assistance, 4-6 hours/day:

| Phase                       | Steps         | Estimated Duration | Cumulative |
| --------------------------- | ------------- | ------------------ | ---------- |
| Phase 0: Foundation         | 0.1, 0.2, 0.3 | 3-5 days           | Week 1     |
| Phase 1: Directive Storage  | 1.1, 1.2      | 3-5 days           | Week 2     |
| Phase 2: Meilisearch        | 2.1, 2.2      | 4-6 days           | Weeks 3-4  |
| Phase 3: Injection Pipeline | 3.1           | 3-5 days           | Week 4-5   |
| Phase 4: Feedback Loop      | 4.1           | 2-3 days           | Week 5     |
| Phase 5: Gel DB             | 5.1, 5.2      | 4-6 days           | Weeks 6-7  |
| Phase 6: Decomposition      | 6.1, 6.2      | 5-8 days           | Weeks 7-9  |
| Phase 7: Hardening          | 7.1, 7.2      | 3-4 days           | Week 9-10  |

**Total: 8-10 weeks** for the full behavioral knowledge engine.

**MVP (Phases 0-3, inject endpoint working):** 4-5 weeks. This is the point where the MCP plugin can call `POST /api/v1/inject` and get back contextual directives. Everything after Phase 3 is enhancement (feedback improves quality, Gel adds chain traversal, decomposition automates ingestion, hardening adds ops tooling).

Phases 2 and 5 can be parallelized (Meilisearch and Gel DB are independent integrations). If done in parallel, the MVP timeline drops to 3-4 weeks.
