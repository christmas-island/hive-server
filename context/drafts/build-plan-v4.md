# Build Plan v4: Behavioral Knowledge Engine

**Date:** 2026-03-09
**Status:** Authoritative build plan. Supersedes build-plan-v3.
**Inputs:** vision-v4.md, directive-schema.md, injection-pipeline.md, recomposition-design.md, skill-replacement-analysis.md, hive-server-current.md, cockroachdb.md, meilisearch.md, gel-db.md, final-review-v3.md
**Verified against:** actual codebase on 2026-03-09

---

## What Changed From Build Plan v3

Seven structural changes, driven by the recomposition-design.md and final-review-v3.md:

1. **Template-based recomposition is replaced by LLM synthesis.** The injection pipeline now calls a Haiku-class LLM to transform raw directive principles into contextual micro-prompts. This is a focused synthesis task, not deep reasoning. The `recompose.go` file that did template variable substitution is gone.

2. **LLM client moves to Phase 1.** The old plan had the LLM client in Phase 6 (decomposition only). Now it is needed in Phase 3 (recomposition) AND Phase 6 (decomposition). The `internal/llm/` package is built early.

3. **No fabricated 400ms latency constraint.** The actual requirement is accuracy over speed. 2-3 seconds for a full recomposition LLM call is acceptable. The pipeline's latency budget is revised accordingly.

4. **Directive storage is simpler.** No `static_content` or `variables` fields. Directives store principles and patterns. The LLM contextualizes them at query time.

5. **Three-layer caching is new.** Response cache, session cache, and pre-synthesis cache get their own build step to manage LLM call volume and latency.

6. **The Recomposer interface is new.** `LLMRecomposer` and `FallbackRecomposer` implementations replace the template-based recompose.go.

7. **Final-review-v3 critical issues are resolved.** The three conflicting directive schemas are reconciled (directive-schema.md is authoritative). Seed directive bootstrap is added. A migration tool (goose) is adopted.

---

## Authoritative Schema Declaration

**directive-schema.md is the single source of truth for all database schemas.**

- vision-v4.md Section 2.2 is illustrative/conceptual. Do not implement from it.
- injection-pipeline.md Section 4 is superseded by recomposition-design.md.
- injection-pipeline.md Section 2.1 Meilisearch configuration derives from directive-schema.md Section 3.
- build-plan-v3 is superseded by this document.
- When any document conflicts with directive-schema.md on field names, types, enum values, or table structure, directive-schema.md wins.

**Directive type taxonomy (authoritative):** `behavioral`, `procedural`, `contextual`, `guardrail`

**Directive type field name (authoritative):** `directive_type` (not `kind`, not `category`)

**Priority field (authoritative):** `priority` INT 1-100 (not `weight` float)

**Trigger mechanism (authoritative):** `context_triggers` JSONB with structured sub-fields per directive-schema.md Section 2

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

| Current Feature               | Disposition  | Rationale                                                                                                                             |
| ----------------------------- | ------------ | ------------------------------------------------------------------------------------------------------------------------------------- |
| Memory CRUD (4 endpoints)     | **KEPT**     | Still needed. Agents store and retrieve arbitrary key-value data. Memory entries become additional context for the recomposition LLM. |
| Task CRUD (5 endpoints)       | **KEPT**     | Task coordination between agents is orthogonal to the directive engine.                                                               |
| Agent heartbeat (3 endpoints) | **KEPT**     | Agent presence tracking remains useful for multi-agent coordination.                                                                  |
| Health / Ready probes         | **KEPT**     | Unchanged.                                                                                                                            |
| Bearer token auth             | **KEPT**     | Unchanged. Token auth applies to all endpoints including new ones.                                                                    |
| X-Agent-ID header middleware  | **MODIFIED** | Currently optional. Required for `POST /api/v1/inject` and `POST /api/v1/feedback`.                                                   |
| SQLite schema patterns        | **REPLACED** | TEXT timestamps become TIMESTAMPTZ. TEXT UUIDs become UUID with gen_random_uuid(). Proper CockroachDB idioms.                         |
| Monolithic Store interface    | **MODIFIED** | Split into composed interfaces (MemoryStore, TaskStore, AgentStore, DirectiveStore, etc.) per ultrathink-architect guidance.          |

The existing API does not disappear. It is enhanced with directive-engine endpoints alongside it.

---

## Phase 0: Foundation

**Goal:** Clean up project layout, finish CockroachDB idiom migration, establish the model package, composed store interfaces, LLM client abstraction, and migration tooling that everything downstream depends on.

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

### Step 0.2: CockroachDB Schema Hardening + Migration Tooling

- **Step ID:** 0.2
- **Dependencies:** 0.1
- **Scope:** M (1-3 days)

**What gets built:**

1. Add `github.com/pressly/goose/v3` dependency for schema migrations.

2. Create `migrations/` directory at project root with SQL migration files:

   - `migrations/001_initial_schema.sql` -- the existing table definitions (memory, tasks, task_notes, agents) rewritten with proper CockroachDB types
   - `created_at TEXT` becomes `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
   - `updated_at TEXT` becomes `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`
   - `tasks.id TEXT` becomes `tasks.id UUID NOT NULL PRIMARY KEY DEFAULT gen_random_uuid()`
   - `agents.last_heartbeat TEXT` becomes `TIMESTAMPTZ`
   - `agents.registered_at TEXT` becomes `TIMESTAMPTZ`

3. Update Go code to use `time.Time` natively instead of string formatting:

   - Remove all `time.Now().UTC().Format(time.RFC3339Nano)` calls in store methods
   - Remove all `time.Parse(time.RFC3339Nano, ...)` calls
   - Let pgx handle `time.Time` <-> `TIMESTAMPTZ` natively
   - Update `model` types: all timestamp fields become `time.Time`

4. Add CockroachDB transaction retry wrapper:

   - `internal/store/tx.go` -- `RetryTx(ctx, db, fn)` helper that wraps `crdbpgx.ExecuteTx` logic for `database/sql`
   - Add `github.com/cockroachdb/cockroach-go/v2/crdb` dependency
   - Wrap all multi-statement store operations in RetryTx
   - Addresses GitHub issue #18

5. Replace inline DDL in `store.go` with goose-managed migrations:
   - `internal/store/store.go` initialization calls `goose.Up(db, "migrations")` on startup
   - Remove the raw `CREATE TABLE IF NOT EXISTS` blocks from store.go
   - Add a `--migrate-only` CLI flag to run migrations without starting the server

**Tests:**

- `internal/store/store_test.go` -- verify schema creates cleanly via goose
- `internal/store/memory_test.go` -- verify timestamps are `time.Time` in returned structs
- `internal/store/tasks_test.go` -- verify UUID generation for task IDs
- All handler tests pass (they mock the store, so unaffected by schema changes)

**Acceptance criteria:**

- All timestamp columns are `TIMESTAMPTZ` in the schema DDL
- All Go model types use `time.Time` for timestamps
- No manual `time.Parse` or `time.Format` calls in store code
- Task IDs are UUID, generated by the database
- `crdb` transaction retry wrapper exists and is used for multi-statement transactions
- goose manages all migrations; no inline DDL in store.go
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

### Step 0.4: LLM Client Abstraction

- **Step ID:** 0.4
- **Dependencies:** 0.1
- **Scope:** M (1-3 days)

**What gets built:**

The LLM client is needed early because the injection pipeline (Phase 3) uses it for recomposition AND the decomposition pipeline (Phase 6) uses it for extraction. Building it in Phase 0 means both consumers can depend on it.

1. Create `internal/llm/` package:
   - `internal/llm/llm.go` -- `Client` interface:

```go
package llm

type Message struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

type CompletionRequest struct {
    Model        string
    SystemPrompt string
    Messages     []Message
    MaxTokens    int
    Temperature  float64
}

type CompletionResponse struct {
    Content    string
    InputTokens  int
    OutputTokens int
    Model      string
    StopReason string
}

type Client interface {
    Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
}
```

- `internal/llm/anthropic.go` -- Anthropic API client implementation:

  - Uses `net/http` for the Anthropic Messages API (`https://api.anthropic.com/v1/messages`)
  - Supports any Anthropic model (configured per call site)
  - Handles API key auth, rate limiting headers, error responses
  - Supports `anthropic-beta` header for prompt caching
  - No external SDK dependency (raw HTTP for minimal deps)

- `internal/llm/noop.go` -- `NoopClient` that returns an error (explicit failure mode when no API key is configured)

2. Configuration via environment:
   - `ANTHROPIC_API_KEY` -- required for recomposition and decomposition
   - When API key is empty, `NoopClient` is used
   - Model selection is per-call-site (recomposition uses Haiku, decomposition uses Sonnet)

**Tests:**

- `internal/llm/anthropic_test.go`:
  - TestComplete_Success -- mock HTTP server returns valid response
  - TestComplete_APIError -- handles 400/429/500 responses
  - TestComplete_Timeout -- respects context deadline
  - TestComplete_ParsesTokenCounts -- correctly extracts usage from response
- `internal/llm/noop_test.go` -- returns error

**Acceptance criteria:**

- `Client` interface exists with Anthropic and Noop implementations
- Anthropic client handles auth, errors, rate limits, and timeouts
- Token usage (input + output) is captured in the response
- No external LLM SDK dependency (raw HTTP)
- Configuration via environment variables
- Both recomposition (Haiku) and decomposition (Sonnet) can use the same client with different model parameters

---

### Step 0.5: Seed Directives

- **Step ID:** 0.5
- **Dependencies:** 0.2
- **Scope:** S (< 1 day)

**What gets built:**

A manually curated set of 30-50 high-value directives embedded in the binary, loaded on first migration. This solves the cold start problem identified in final-review-v3 Section 6.

1. Create `seed/directives.json` -- JSON array of directive objects:

   - Each directive has: `content`, `rationale`, `directive_type`, `context_triggers`, `verification_criteria`, `priority`, `tags`
   - No `source_skill` (these are curated, not decomposed)
   - Curated from the skill-replacement-analysis's highest-value directives

2. Create `migrations/002_seed_directives.sql` -- goose migration that:

   - Checks if directives table is empty
   - If empty, inserts the seed set
   - Idempotent: re-running does nothing if directives already exist

3. The seed set covers universal behavioral guardrails:

   **From Superpowers (anti-rationalization, highest priority):**

   - "Before claiming any task is complete, run the verification command and read its full output." (guardrail, P95)
   - "When debugging, reproduce the problem first. Do not attempt a fix before you can reliably trigger the failure." (guardrail, P90)
   - "Before any creative work, brainstorm at least 3 meaningfully different approaches." (behavioral, P70)
   - "When writing tests, follow red-green-refactor: write a failing test first, then make it pass, then refactor." (behavioral, P65)

   **From GSD (workflow discipline):**

   - "Commit completed work atomically. One commit per completed task with conventional commit messages." (behavioral, P60)
   - "When a sub-agent finishes, verify its output against the acceptance criteria before marking the task done." (guardrail, P85)

   **From Allium (specification discipline):**

   - "When specs and code disagree, classify the mismatch: is it a spec bug, code bug, aspirational design, or intentional gap?" (contextual, P70)

   Plus ~25 additional directives covering testing, error handling, code review, debugging, and implementation patterns.

4. Create `internal/store/seed.go` -- `SeedDirectives(ctx, db)` function:
   - Reads `seed/directives.json` (embedded via `//go:embed`)
   - Called by the migration or by explicit bootstrap command
   - Skips if directives already exist in the table

**Tests:**

- `internal/store/seed_test.go`:
  - TestSeedDirectives_EmptyDB -- inserts all seed directives
  - TestSeedDirectives_NonEmpty -- skips insertion
  - TestSeedDirectives_ValidJSON -- all embedded directives parse correctly

**Acceptance criteria:**

- 30-50 curated directives exist as embedded JSON
- Directives are loaded on first startup when the database is empty
- Seed is idempotent (safe to run multiple times)
- The injection pipeline has directives to work with from day one

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
    DecompositionRunID   *string        `json:"decomposition_run_id,omitempty"`
    CreatedAt            time.Time      `json:"created_at"`
    UpdatedAt            time.Time      `json:"updated_at"`
    Tags                 []string       `json:"tags,omitempty"`
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
    Status           string    `json:"status"` // pending, processing, completed, failed
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
    UpdateDecompositionRunStatus(ctx context.Context, id string, status string, directivesCreated int) error
}
```

3. Add CockroachDB schema via goose migration `migrations/003_directives.sql`:

   - `CREATE TYPE directive_type AS ENUM (...)` per directive-schema.md
   - `decomposition_runs` table per directive-schema.md
   - `directives` table per directive-schema.md (using directive_type enum, context_triggers JSONB, priority INT)
   - `directive_tags` table (normalized)
   - `directive_relationships` table (normalized, with relationship_type enum: chains_to, conflicts_with, alternative_to, refines, requires, equivalent_to)
   - `directive_feedback` table
   - `directive_sets` and `directive_set_members` tables
   - All indexes from directive-schema.md
   - SQL functions: `update_effectiveness_score`, `match_directives`
   - SQL view: `active_directives`

4. Implement `internal/store/directives.go`:

   - `CreateDirective` -- INSERT with RETURNING
   - `GetDirective` -- SELECT with LEFT JOIN on directive_tags
   - `ListDirectives` -- parameterized query with filters, JOIN tags
   - `UpdateDirective` -- UPDATE with version increment
   - `DeleteDirective` -- soft delete (SET is_active = false)
   - `SetDirectiveTags` -- DELETE + batch INSERT for tags
   - `CreateDecompositionRun` / `GetDecompositionRun` / `UpdateDecompositionRunStatus`

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

- All directive tables exist in the schema (created via goose migration)
- Schema matches directive-schema.md Section 2 exactly
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
    DirectiveType string
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

- `internal/search/noop.go` -- `NoopSearcher` that returns empty results (for when Meilisearch is unavailable or disabled)

- `internal/search/meili.go` -- `MeiliSearcher` implementing `Searcher`:
  - Constructor takes Meilisearch host + API key
  - `ConfigureIndex` sets up the `directives` index per directive-schema.md Section 3:
    - searchableAttributes: `content`, `rationale`, `verification_criteria`, `tags`, `source_section`, `trigger_keywords`
    - filterableAttributes: `directive_type`, `source_skill`, `activity_types`, `workflow_stages`, `priority`, `effectiveness_score`, `is_active`, `tags`
    - sortableAttributes: `priority`, `effectiveness_score`, `created_at`
    - Custom ranking rules: `effectiveness_score:desc`, `priority:desc`
    - Synonyms per directive-schema.md Section 3
    - typoTolerance.disableOnAttributes for enum-like fields
  - `IndexDirective` / `IndexDirectives` transforms `model.Directive` to Meilisearch document format (flattening context_triggers into top-level fields: `activity_types`, `workflow_stages`, `trigger_keywords`)
  - `SearchDirectives` builds query from SearchRequest, applies filters, returns scored results
  - **Query preprocessing:** Truncate query to 10 meaningful terms (Meilisearch word limit). Extract key terms by removing stop words, then taking the 10 highest-signal words.
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
  - TestQueryPreprocessing -- long summaries are truncated to 10 terms
  - TestRemoveDirective -- removes from index
  - TestHealthy -- returns true when Meilisearch is up

**Acceptance criteria:**

- `Searcher` interface exists with Meilisearch and Noop implementations
- Meilisearch index configuration matches directive-schema.md Section 3
- Query preprocessing truncates to 10 meaningful terms
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

**Goal:** Build the core `POST /api/v1/inject` endpoint -- the primary value delivery mechanism. Fan-out queries to CockroachDB and Meilisearch, rank, select, recompose via LLM, return contextual micro-prompts.

### Step 3.1: Injection Pipeline Core (Retrieval + Ranking)

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
    Summary      string   `json:"summary"`
    RecentFiles  []string `json:"recent_files,omitempty"`
    RecentTools  []string `json:"recent_tools,omitempty"`
    ErrorContext string   `json:"error_context,omitempty"`
}

type InjectionResponse struct {
    InjectionID          string              `json:"injection_id"`
    Directives           []InjectedDirective `json:"directives"`
    TokensUsed           int                 `json:"tokens_used"`
    TokenBudget          int                 `json:"token_budget"`
    CandidatesConsidered int                 `json:"candidates_considered"`
    CandidatesSelected   int                 `json:"candidates_selected"`
    Recomposed           bool                `json:"recomposed"`
    Fallback             bool                `json:"fallback"`
}

type InjectedDirective struct {
    DirectiveIDs []string `json:"directive_ids"`
    Content      string   `json:"content"`
    Category     string   `json:"category"`
    Action       string   `json:"action"` // "rule", "suggest", "context"
    Source       string   `json:"source"`
    Confidence   float64  `json:"confidence"`
}
```

2. Create `internal/inject/` package:
   - `internal/inject/pipeline.go` -- `Pipeline` struct:

```go
type Pipeline struct {
    store      model.DirectiveStore
    searcher   search.Searcher
    recomposer Recomposer
    // gel will be added in Phase 5
}

func NewPipeline(store model.DirectiveStore, searcher search.Searcher, recomposer Recomposer) *Pipeline

func (p *Pipeline) Inject(ctx context.Context, agentID string, req *model.InjectRequest) (*model.InjectionResponse, error)
```

- `internal/inject/retrieve.go` -- Fan-out retrieval:

  - `retrieveFromMeilisearch(ctx, req) ([]candidate, error)` -- builds search query from context.summary + intent, searches directive index
  - `retrieveFromCRDB(ctx, req) ([]candidate, error)` -- runs structured queries:
    - Query 1: Active directives matching activity (via context_triggers JSONB)
    - Query 2: Active directives matching project scope
    - Query 3: Pinned directives for this project (from `directive_pins` table)
    - Query 4: Recently injected directive IDs for deduplication
  - Fan-out uses `errgroup.Group` with per-source timeouts (150ms Meilisearch, 200ms CRDB)

- `internal/inject/rank.go` -- Ranking and selection:
  - Merge candidates from all sources, deduplicate by directive ID
  - Score each candidate: `score = (relevance * 0.35) + (priority * 0.25) + (freshness * 0.15) + (source_boost * 0.15) + (feedback * 0.10)` per injection-pipeline.md Section 3.1
  - Priority normalization by directive_type: guardrail=1.0, contextual=0.75, behavioral=0.5, procedural=0.25
  - Select top 5-15 directives as input for recomposition (directive input budget, not final output budget)
  - Guardrail directives always included (never dropped by budget)
  - `EstimateTokens(content string) int` -- `len(content) / 4` heuristic

3. Add supporting tables via goose migration `migrations/004_injection_support.sql`:

```sql
CREATE TABLE IF NOT EXISTS directive_pins (
    id           UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    directive_id UUID        NOT NULL REFERENCES directives(id),
    project      TEXT        NOT NULL,
    activity     TEXT        NOT NULL DEFAULT 'all',
    enabled      BOOLEAN     NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (directive_id, project, activity)
);
CREATE INDEX IF NOT EXISTS idx_directive_pins_project ON directive_pins(project, enabled);

CREATE TABLE IF NOT EXISTS agent_preferences (
    id           UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id     TEXT        NOT NULL,
    directive_id UUID        NOT NULL REFERENCES directives(id),
    weight_adj   FLOAT8      NOT NULL DEFAULT 0.0,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (agent_id, directive_id)
);
CREATE INDEX IF NOT EXISTS idx_agent_prefs_agent ON agent_preferences(agent_id);

CREATE TABLE IF NOT EXISTS injection_log (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      TEXT        NOT NULL,
    agent_id        TEXT        NOT NULL,
    context_hash    TEXT        NOT NULL,
    directive_ids   JSONB       NOT NULL DEFAULT '[]',
    tokens_used     INT4        NOT NULL DEFAULT 0,
    recomposed      BOOLEAN     NOT NULL DEFAULT false,
    fallback        BOOLEAN     NOT NULL DEFAULT false,
    latency_ms      INT4        NOT NULL DEFAULT 0,
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
  - TestPriorityNormalization -- guardrail=1.0, procedural=0.25
- `internal/handlers/inject_test.go`:
  - TestInjectEndpoint_Success
  - TestInjectEndpoint_MissingAgentID -- 400 error
  - TestInjectEndpoint_InvalidActivity -- 422 validation error

**Acceptance criteria:**

- `POST /api/v1/inject` retrieves and ranks directives
- Fan-out to Meilisearch + CockroachDB with timeouts and graceful degradation
- Ranking produces sensible ordering (guardrails first, then by score)
- Directive input budget selects 5-15 candidates for recomposition
- `directive_pins` and `agent_preferences` tables exist
- Injection is logged for session deduplication

---

### Step 3.2: Recomposer Interface and LLM Implementation

- **Step ID:** 3.2
- **Dependencies:** 0.4, 3.1
- **Scope:** L (3-5 days)

**What gets built:**

This is the core change from v3. Template-based recomposition is replaced by LLM synthesis per recomposition-design.md.

1. Create `internal/inject/recompose.go` -- Recomposer interface and types:

```go
// Recomposer transforms raw directives into contextual micro-prompts.
type Recomposer interface {
    Recompose(ctx context.Context, input *RecompositionInput) (*RecompositionOutput, error)
}

type RecompositionInput struct {
    // Selected directives (principles/patterns)
    Directives []DirectiveForSynthesis

    // Request context
    Activity        string
    ProjectName     string
    ProjectLanguage string
    ContextSummary  string
    RecentFiles     []string
    ErrorContext     string
    Intent          string

    // Additional context from knowledge base
    ProjectMemories []MemorySnippet
    AgentMemories   []MemorySnippet

    // Constraints
    OutputTokenBudget int
}

type DirectiveForSynthesis struct {
    ID                   string
    Content              string
    Rationale            string
    DirectiveType        string
    Priority             int
    VerificationCriteria string
    SourceSkill          string
}

type MemorySnippet struct {
    Key     string
    Value   string
    Source  string
}

type RecompositionOutput struct {
    Snippets []MicroPrompt
    Skipped  []SkippedDirective
    Fallback bool
}

type MicroPrompt struct {
    DirectiveIDs []string
    Content      string
    Category     string
    Action       string // "rule", "suggest", "context"
    Reasoning    string
}

type SkippedDirective struct {
    DirectiveID string
    Reason      string
}
```

2. `internal/inject/recompose_llm.go` -- `LLMRecomposer` implementation:

   - Constructor takes `llm.Client`, model string (default: `claude-haiku-4-5-20251015`), system prompt
   - System prompt from recomposition-design.md Section 4 (stored as Go constant)
   - `Recompose` method:
     1. Assembles the LLM prompt from RecompositionInput (directives + context + memories)
     2. Calls the LLM with structured output request
     3. Parses JSON response into `[]MicroPrompt` and `[]SkippedDirective`
     4. Validates: each micro-prompt references valid directive IDs, per-directive token limit (150 tokens), total within budget
     5. Token budget enforcement: if LLM output exceeds budget, trim lowest-priority non-guardrail directives
     6. Returns `RecompositionOutput` with `Fallback: false`
   - Timeout: 10 seconds hard deadline for the LLM call
   - Temperature: 0.0 for deterministic synthesis

3. `internal/inject/recompose_fallback.go` -- `FallbackRecomposer` implementation:

   - Returns raw directive `content` as-is, formatted as micro-prompts
   - Maps directive_type to action: guardrail -> "rule", others -> "suggest"
   - Sets `Fallback: true` in output
   - Used when: LLM client is NoopClient, LLM call fails, LLM call times out

4. Integrate the recomposer into the Pipeline:

   - Pipeline receives a `Recomposer` in its constructor
   - After ranking selects 5-15 directives, Pipeline calls `Recomposer.Recompose`
   - If `LLMRecomposer` fails, fall back to `FallbackRecomposer`
   - Pipeline wraps the output into `InjectionResponse`

5. Fetch relevant memories for recomposition context:

   - Query the existing memory store for entries matching `project.name` and `agent_id`
   - Pass up to 3 most recent/relevant memories as `ProjectMemories` / `AgentMemories`
   - This enriches the LLM's context for better synthesis

6. Configuration via environment:
   - `HIVE_RECOMPOSER` -- `llm` (default when API key exists) or `fallback` (explicit override)
   - `HIVE_RECOMPOSER_MODEL` -- LLM model for recomposition (default: `claude-haiku-4-5-20251015`)
   - `HIVE_RECOMPOSER_TIMEOUT` -- hard timeout for LLM call (default: 10s)

**Tests:**

- `internal/inject/recompose_llm_test.go`:
  - TestLLMRecompose_Success -- mock LLM returns valid JSON, parses correctly
  - TestLLMRecompose_InvalidJSON -- falls back to FallbackRecomposer
  - TestLLMRecompose_Timeout -- respects deadline, falls back
  - TestLLMRecompose_TokenBudgetEnforcement -- trims over-budget output
  - TestLLMRecompose_SkippedDirectives -- handles skipped array correctly
  - TestLLMRecompose_GuardrailsNeverTrimmed -- guardrails survive budget trimming
- `internal/inject/recompose_fallback_test.go`:
  - TestFallbackRecompose_ReturnsRawContent -- directive content verbatim
  - TestFallbackRecompose_MapsActions -- guardrail -> rule, behavioral -> suggest
  - TestFallbackRecompose_SetsFallbackTrue
- `internal/inject/pipeline_test.go` (extend):
  - TestInject_WithLLMRecomposition -- end-to-end with mock LLM
  - TestInject_LLMFails_FallsBack -- LLM error triggers fallback
  - TestInject_FallbackMode -- explicit fallback configuration

**Acceptance criteria:**

- `LLMRecomposer` calls Haiku-class LLM and produces contextual micro-prompts
- System prompt matches recomposition-design.md Section 4
- Structured JSON output is parsed and validated
- Token budget is enforced post-LLM (trim if over)
- Guardrail directives are never trimmed
- Fallback to raw directive content when LLM is unavailable
- Feature flag allows switching between LLM and fallback modes
- Relevant memories are included in recomposition context
- End-to-end pipeline works: retrieve -> rank -> recompose -> respond

**Latency budget (revised from v3):**

| Phase                        | Duration             | Notes               |
| ---------------------------- | -------------------- | ------------------- |
| Request parsing + validation | 1ms                  |                     |
| Fan-out retrieval (parallel) | 50-200ms             | Meilisearch + CRDB  |
| Ranking + selection          | 5ms                  |                     |
| LLM prompt assembly          | 2ms                  |                     |
| LLM TTFT (cached prompt)     | 300-400ms            | With prompt caching |
| LLM generation (~250 tokens) | 2,500-3,000ms        | ~94 tokens/sec      |
| Parse + validate output      | 1ms                  |                     |
| **Total**                    | **~3.0-3.5 seconds** | Accuracy over speed |

---

### Step 3.3: Recomposition Caching Layer

- **Step ID:** 3.3
- **Dependencies:** 3.2
- **Scope:** M (1-3 days)

**What gets built:**

Three-layer caching per recomposition-design.md Section 6 to reduce LLM call volume and latency.

1. `internal/inject/cache.go` -- Cache types and management:

```go
// Layer 1: Full response cache (hash-based, exact match)
type ResponseCache struct {
    mu    sync.RWMutex
    items map[string]*CachedResponse
    ttl   time.Duration // default 5 minutes
}

// Key: SHA-256 of sorted(directive_ids) + activity + project_name + context_summary_first_100_chars
func (c *ResponseCache) Key(directiveIDs []string, activity, project, summary string) string

// Layer 2: Session-level directive cache
type SessionCache struct {
    mu    sync.RWMutex
    items map[string]*CachedResponse // key: session_id + sorted(directive_ids) + activity
    ttl   time.Duration // default 10 minutes
}

// Layer 3: Directive pre-synthesis cache (per directive + language + activity)
type PreSynthCache struct {
    mu    sync.RWMutex
    items map[string]*MicroPrompt // key: directive_id + project_language + activity
    ttl   time.Duration // default 1 hour
}
```

2. Cache integration into `LLMRecomposer`:

   - Before calling LLM, check Layer 1 (exact response match)
   - Before calling LLM, check Layer 2 (session match)
   - After retrieval, check Layer 3 for each directive individually
   - If all directives have pre-synthesis entries, assemble from cache (skip LLM)
   - If some miss, call LLM for missing ones only (partial LLM call)
   - If all miss, full LLM call

3. Cache cleanup: background goroutine evicts expired entries every 60 seconds.

4. Pre-synthesis background worker:

   - Runs on a configurable interval (default: 1 hour)
   - Queries top 20 most-frequently-surfaced directives
   - For each, generates pre-synthesized micro-prompts for common activity + language combinations
   - Stores in Layer 3 cache
   - Only runs when LLM client is available

5. Configuration via environment:
   - `HIVE_CACHE_RESPONSE_TTL` -- Layer 1 TTL (default: 5m)
   - `HIVE_CACHE_SESSION_TTL` -- Layer 2 TTL (default: 10m)
   - `HIVE_CACHE_PRESYNTH_ENABLED` -- Enable/disable pre-synthesis (default: true)
   - `HIVE_CACHE_PRESYNTH_INTERVAL` -- Pre-synthesis regeneration interval (default: 1h)

**Tests:**

- `internal/inject/cache_test.go`:
  - TestResponseCache_Hit -- exact match returns cached response
  - TestResponseCache_Miss -- different context misses
  - TestResponseCache_Expiry -- expired entries not returned
  - TestSessionCache_SameSession -- same session + directives + activity hits
  - TestSessionCache_ActivityChange -- activity change misses
  - TestPreSynthCache_Hit -- pre-synthesized directive returned
  - TestPreSynthCache_PartialHit -- some directives cached, others need LLM
  - TestCacheCleanup -- expired entries are evicted

**Acceptance criteria:**

- Three-layer cache reduces LLM call volume
- Expected effective latency: p50 ~200ms (cache hit), p90 ~3.0s (full LLM)
- Cache keys are deterministic and collision-resistant
- TTLs are configurable
- Pre-synthesis background worker runs and populates Layer 3
- Cache metrics are trackable (hit rate, miss rate per layer)

---

## Phase 4: Feedback Loop

**Goal:** Track directive outcomes, update effectiveness scores, capture recomposition quality data, enable experience-derived directives from session completion.

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
    InjectionID    string             `json:"injection_id"`
    Outcomes       []DirectiveOutcome `json:"outcomes"`
    SessionOutcome string             `json:"session_outcome,omitempty"`
    SessionSummary string             `json:"session_summary,omitempty"`
}

type DirectiveOutcome struct {
    DirectiveID         string          `json:"directive_id"`
    Outcome             FeedbackOutcome `json:"outcome"`
    Evidence            string          `json:"evidence,omitempty"`
    SnippetContent      string          `json:"snippet_content,omitempty"`
    SpecificityHelpful  *bool           `json:"specificity_helpful,omitempty"`
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
    RecordFeedback(ctx context.Context, directiveID string, agentID string, sessionID string, outcome FeedbackOutcome, evidence string, snippetContent string, specificityHelpful *bool) error
    UpdateEffectiveness(ctx context.Context, directiveID string) error
    RecordSessionComplete(ctx context.Context, req *SessionCompleteRequest, agentID string) error
    GetDirectiveEffectiveness(ctx context.Context, directiveID string) (float64, error)
}
```

3. Implement `internal/store/feedback.go`:

   - `RecordFeedback` -- INSERT into directive_feedback, including snippet_content and specificity_helpful fields for recomposition quality tracking
   - `UpdateEffectiveness` -- recalculate effectiveness_score from feedback history (per directive-schema.md function `update_effectiveness_score`)
   - `RecordSessionComplete` -- INSERT into a new `session_completions` table
   - `GetDirectiveEffectiveness` -- read the computed score

4. Add `session_completions` table via goose migration `migrations/005_feedback.sql`:

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

5. Extend `directive_feedback` table (already created in Step 1.1) to capture recomposition data:

   - Add `snippet_content TEXT` -- the actual micro-prompt delivered (for recomposition quality measurement)
   - Add `specificity_helpful BOOLEAN` -- whether contextual specificity contributed to the agent following the directive
   - These fields enable the recomposition effectiveness query from recomposition-design.md Section 8

6. `internal/handlers/feedback.go` -- Huma handlers:

   - `POST /api/v1/feedback` -- record directive outcomes from an injection, update effectiveness scores
   - `POST /api/v1/feedback/session-complete` -- record session completion with summary and key insight

7. Wire into router: `registerFeedback(a, api)` in handlers.go

**Tests:**

- `internal/store/feedback_test.go`:
  - TestRecordFeedback_CreatesRecord
  - TestRecordFeedback_WithSnippetContent -- captures recomposition output
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
- Recomposition quality is captured: snippet_content and specificity_helpful fields
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
   - `dbschema/default.esdl` -- Gel schema per **directive-schema.md Section 4** (authoritative, not the simplified vision-v4 version):

```sdl
module default {
    scalar type DirectiveType extending enum<behavioral, procedural, contextual, guardrail>;
    scalar type RelationshipType extending enum<chains_to, conflicts_with, alternative_to, refines, requires, equivalent_to>;
    scalar type WorkflowStage extending enum<starting, brainstorming, planning, implementing, debugging, reviewing, testing, refactoring, deploying>;

    type Directive {
        required content: str;
        required rationale: str;
        required directive_type: DirectiveType;
        required source_skill: str;
        required source_section: str;
        required priority: int32 { constraint min_value(1); constraint max_value(100); };
        required effectiveness_score: float64 { default := 0.0; };
        required is_active: bool { default := true; };
        required version: int32 { default := 1; };
        crdb_id: uuid;

        multi chains_to: Directive {
            property strength: float64;
            property description: str;
        };
        multi conflicts_with: Directive {
            property strength: float64;
            property description: str;
        };
        multi alternative_to: Directive {
            property strength: float64;
            property description: str;
        };
        multi refines: Directive {
            property strength: float64;
            property description: str;
        };
        multi requires: Directive;
        multi equivalent_to: Directive {
            property description: str;
        };
        link supersedes: Directive;
        multi tags: str;
        multi activity_types: str;
        multi workflow_stages: WorkflowStage;

        property incoming_chain_count := count(.<chains_to[IS Directive]);
        property conflict_count := count(.<conflicts_with[IS Directive]);

        index on (.directive_type);
        index on (.source_skill);
        index on (.is_active);
    }

    type DirectiveSet {
        required name: str { constraint exclusive; };
        required description: str;
        required is_active: bool { default := true; };
        multi members: Directive;
        property member_count := count(.members);
    }

    type DecompositionRun {
        required source_skill: str;
        required source_section: str;
        source_document: str;
        source_text_hash: str;
        model_used: str;
        multi produced: Directive;
        property directive_count := count(.produced);
    }
}
```

3. Create `internal/graph/` package:
   - `internal/graph/graph.go` -- `GraphStore` interface:

```go
type GraphStore interface {
    GetChainForDirective(ctx context.Context, directiveID string) ([]*model.Directive, error)
    GetRelatedDirectives(ctx context.Context, directiveID string, depth int) ([]*model.Directive, error)
    GetConflictingDirectives(ctx context.Context, directiveID string) ([]*model.Directive, error)
    SyncDirective(ctx context.Context, d *model.Directive) error
    SyncRelationship(ctx context.Context, fromID, toID string, relType string, strength float64) error
    Healthy(ctx context.Context) bool
}
```

- `internal/graph/noop.go` -- `NoopGraphStore` that returns nil/empty (graceful degradation when Gel is unavailable)
- `internal/graph/gel.go` -- Gel DB implementation:
  - Constructor takes Gel connection options
  - `GetChainForDirective` -- EdgeQL query to traverse `chains_to` links
  - `GetRelatedDirectives` -- EdgeQL query traversing all relationship links to specified depth
  - `GetConflictingDirectives` -- EdgeQL query on `conflicts_with` links (used to exclude conflicting directives from injection)
  - `SyncDirective` -- UPSERT directive into Gel (keyed by crdb_id)
  - `SyncRelationship` -- CREATE/UPDATE relationship link between two directives

4. Configuration via environment:
   - `GEL_DSN` -- Gel connection string (default: empty, meaning disabled)
   - When empty, `NoopGraphStore` is used

**Tests:**

- `internal/graph/noop_test.go` -- verify NoopGraphStore returns nil without error
- `internal/graph/gel_test.go` -- integration test (`//go:build integration`):
  - TestSyncDirective -- syncs from CRDB model to Gel
  - TestGetChainForDirective -- creates chain, retrieves it via directive
  - TestGetRelatedDirectives -- creates related directives, traverses links
  - TestGetConflictingDirectives -- finds conflicts

**Acceptance criteria:**

- Gel schema exists in `dbschema/default.esdl` matching directive-schema.md Section 4
- GraphStore interface with Gel and Noop implementations
- Six distinct relationship types modeled (chains_to, conflicts_with, alternative_to, refines, requires, equivalent_to)
- Directives can be synced from CockroachDB to Gel
- Chain and relationship traversal works via EdgeQL
- Graceful degradation when Gel is unavailable

---

### Step 5.2: Graph-Enhanced Injection Pipeline

- **Step ID:** 5.2
- **Dependencies:** 3.1, 5.1
- **Scope:** M (1-3 days)

**What gets built:**

1. Update `internal/inject/pipeline.go`:

   - `Pipeline` struct gains a `graph graph.GraphStore` field
   - Constructor: `NewPipeline(store, searcher, recomposer, graph)` -- graph can be NoopGraphStore

2. Add `internal/inject/retrieve.go`:

   - New retrieval source: `retrieveFromGel(ctx, foundIDs) ([]candidate, error)`
   - Given directive IDs found by Meilisearch and CRDB, query Gel for:
     - Behavioral chains containing those directives (get the next step)
     - Related directives (1-hop traversal)
     - Conflicting directives (to exclude from results)
   - Timeout: 200ms (per injection-pipeline.md Section 2.3)

3. Update fan-out in `Pipeline.Inject`:

   - Phase 1: Parallel fan-out to Meilisearch + CRDB (existing)
   - Phase 2: After Phase 1 completes, fan-out to Gel with the found directive IDs
   - Gel results are added as candidates with `source_boost = 0.7` (per injection-pipeline.md)
   - Conflicting directives from Gel are removed from the candidate list
   - Total pipeline retrieval deadline: 400ms for Phase 1, then 200ms for Phase 2

4. Degradation: Gel is the lowest priority. If it times out or is unavailable, pipeline returns results from Meilisearch + CRDB only.

**Tests:**

- `internal/inject/pipeline_test.go` (extend existing):
  - TestInject_WithGelChain -- directive in a chain returns the next step
  - TestInject_GelUnavailable_StillWorks -- pipeline degrades gracefully
  - TestInject_GelAddsRelatedDirectives -- related directives appear in results
  - TestInject_GelExcludesConflicts -- conflicting directives removed from candidates

**Acceptance criteria:**

- Injection pipeline queries Gel as a third source when available
- Chain traversal adds "next step" directives to candidates
- Conflicting directives are excluded from injection results
- Related directives boost diversity of results
- Gel timeout does not block the pipeline
- All previous injection tests still pass

---

## Phase 6: Decomposition Pipeline

**Goal:** Implement LLM-powered ingestion -- when a skill document is ingested, decompose it into atomic directives using an LLM.

### Step 6.1: Decomposition Engine

- **Step ID:** 6.1
- **Dependencies:** 0.4, 1.1, 2.2
- **Scope:** L (3-5 days)

**What gets built:**

Note: The LLM client (Step 0.4) already exists. This step uses it for decomposition with a Sonnet-class model (different from the Haiku model used for recomposition).

1. Create `internal/decompose/` package:
   - `internal/decompose/decompose.go` -- `Engine` struct:

```go
type Engine struct {
    llm   llm.Client
    model string // default: claude-sonnet-4-20250514
    store model.DirectiveStore
    sync  *search.SyncManager
}

func NewEngine(llm llm.Client, model string, store model.DirectiveStore, sync *search.SyncManager) *Engine

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
  - Each RawDirective has: content, rationale, directive_type, context_triggers, verification_criteria, priority

- `internal/decompose/enrich.go` -- Post-extraction enrichment:

  - Token cost estimation via `len(content) / 4`
  - Tag derivation from context_triggers fields
  - Priority assignment based on directive_type (guardrail defaults higher)
  - Source provenance attachment

- `internal/decompose/dedup.go` -- Deduplication:
  - For each extracted directive, search Meilisearch for similar existing directives
  - If similarity score > 0.8, merge: keep the most specific version, preserve both sources
  - If a directive from multiple independent sources is found, boost priority

2. Update `POST /api/v1/ingest` handler:

   - After creating the decomposition_run, check if LLM is available
   - If available, run decomposition asynchronously (fire-and-forget goroutine)
   - Add `GET /api/v1/ingest/{run_id}` endpoint to check decomposition status and see results
   - Decomposition run status: `pending` -> `processing` -> `completed` / `failed`

3. The decomposition prompt (stored as a constant in `internal/decompose/prompt.go`):

   - Per vision-v4.md Section 3.3: "You are a behavioral knowledge extractor..."
   - Requests JSON array of directives with specific fields matching directive-schema.md
   - Version the prompt (e.g., `decompose-v1`) and store version in decomposition_run

4. Configuration via environment:
   - `HIVE_DECOMPOSE_MODEL` -- model for decomposition (default: `claude-sonnet-4-20250514`)
   - Uses the same `ANTHROPIC_API_KEY` as the recomposer

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
  - TestPriorityAssignment_GuardrailHigher
  - TestTagDerivation_FromTriggers
- `internal/decompose/dedup_test.go`:
  - TestDedup_ExactMatch -- merges identical directives
  - TestDedup_SimilarContent -- merges semantically similar
  - TestDedup_NoMatch -- creates new
- `internal/decompose/decompose_test.go` (integration):
  - TestDecompose_EndToEnd -- full pipeline from markdown to stored directives

**Acceptance criteria:**

- Skill documents can be ingested and decomposed into atomic directives
- Sectioning is deterministic (no LLM)
- LLM extraction produces valid directive structures matching directive-schema.md
- Deduplication prevents redundant directives from multiple skill sources
- Decomposition runs are tracked with status
- Results are synced to Meilisearch after creation
- A typical skill document (2000 tokens) produces 15-30 directives
- Budget 2-3 iterations of the decomposition prompt for quality tuning

---

## Phase 7: Hardening

**Goal:** Production readiness -- rate limiting, metrics, structured logging, audit logging, and backup infrastructure.

### Step 7.1: Observability and Rate Limiting

- **Step ID:** 7.1
- **Dependencies:** 3.2, 4.1
- **Scope:** M (1-3 days)

**What gets built:**

1. Add structured logging throughout:

   - Replace `internal/log/log.go` with `log/slog` (standard library, Go 1.21+)
   - Add request logging middleware: method, path, status, duration, agent_id
   - Add injection pipeline timing logs: meilisearch_ms, crdb_ms, gel_ms, recompose_ms, total_ms, candidates, selected, cache_hit
   - Add recomposition metrics logs: input_tokens, output_tokens, model, fallback, latency_ms
   - Log feedback recording: directive_id, outcome, agent_id

2. Add Prometheus metrics endpoint:

   - `GET /metrics` -- standard Prometheus exposition format
   - Metrics:
     - `hive_http_requests_total{method, path, status}` -- counter
     - `hive_http_request_duration_seconds{method, path}` -- histogram
     - `hive_inject_duration_seconds` -- histogram for injection pipeline
     - `hive_inject_candidates_total` -- histogram for candidates considered
     - `hive_inject_selected_total` -- histogram for candidates selected
     - `hive_recompose_latency_seconds` -- histogram for LLM recomposition time
     - `hive_recompose_cache_hit_rate` -- gauge (fraction of requests served from cache)
     - `hive_recompose_fallback_rate` -- gauge (fraction using fallback)
     - `hive_recompose_tokens_input_total` -- counter
     - `hive_recompose_tokens_output_total` -- counter
     - `hive_recompose_cost_usd_total` -- counter (estimated from token counts)
     - `hive_directives_total{directive_type, source_skill, active}` -- gauge
     - `hive_feedback_total{outcome}` -- counter
     - `hive_meilisearch_healthy` -- gauge (1 or 0)
     - `hive_gel_healthy` -- gauge (1 or 0)

3. Add rate limiting middleware:

   - Per-agent rate limit on `POST /api/v1/inject` (default: 60 requests/minute)
   - Per-agent rate limit on `POST /api/v1/feedback` (default: 120 requests/minute)
   - Use `golang.org/x/time/rate` (token bucket)
   - Rate limit state in-memory (acceptable for single-instance)
   - Return `429 Too Many Requests` with `Retry-After` header

4. Add audit logging for admin operations:
   - `POST /api/v1/ingest` -- log source_name, content_hash, agent_id
   - `DELETE /api/v1/directives/{id}` -- log directive_id, agent_id
   - `POST /api/v1/admin/sync` -- log trigger source
   - Audit log goes to structured logging output (not a separate table)

**Tests:**

- Metrics endpoint returns valid Prometheus format
- Rate limiting returns 429 after threshold
- Structured log output contains expected fields

**Acceptance criteria:**

- All API requests are logged with structured fields
- Injection pipeline timing is logged and metriced
- Recomposition metrics (tokens, cost, latency, cache hit rate, fallback rate) are tracked
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
    -> 0.2 (CRDB + goose)       -- depends on 0.1
    -> 0.3 (composed interfaces) -- depends on 0.1
    -> 0.4 (LLM client)         -- depends on 0.1
       (0.2, 0.3, and 0.4 are parallelizable after 0.1)
  0.5 (seed directives)          -- depends on 0.2

Phase 1: Directive Storage
  1.1 (directive schema/store)   -- depends on 0.2, 0.3
    -> 1.2 (directive handlers)  -- depends on 1.1

Phase 2: Meilisearch
  2.1 (search interface)         -- depends on 1.1
    -> 2.2 (CRDB-Meili sync)    -- depends on 2.1

Phase 3: Injection Pipeline
  3.1 (retrieval + ranking)      -- depends on 1.1, 2.1
    -> 3.2 (LLM recomposer)     -- depends on 0.4, 3.1
      -> 3.3 (caching layer)    -- depends on 3.2

Phase 4: Feedback Loop
  4.1 (feedback + scoring)       -- depends on 3.1

Phase 5: Gel DB
  5.1 (gel schema + client)      -- depends on 1.1 (parallelizable with Phase 2-3)
    -> 5.2 (graph-enhanced inject) -- depends on 3.1, 5.1

Phase 6: Decomposition
  6.1 (decomposition engine)     -- depends on 0.4, 1.1, 2.2

Phase 7: Hardening
  7.1 (observability + rate limit) -- depends on 3.2, 4.1
  7.2 (backup + integrity)        -- depends on 2.2, 5.1
  (7.1 and 7.2 are parallelizable)
```

### Parallelization Opportunities

Within the dependency constraints:

1. **After 0.1 completes:** 0.2, 0.3, and 0.4 can all run in parallel.
2. **After 1.1 completes:** 2.1 (Meilisearch), 5.1 (Gel), and 1.2 (handlers) can all run in parallel.
3. **After 3.1 completes:** 4.1 (feedback) and 5.2 (graph-inject) can run in parallel with 3.2 (recomposer).
4. **After 0.4 and 1.1 and 2.2 complete:** 6.1 (decomposition) can start independently of the injection pipeline.
5. **After 3.2 and 4.1 complete:** 7.1 and 7.2 can run in parallel.

### Critical Path

The critical path to first value delivery is:

```
0.1 -> 0.2 -> 1.1 -> 2.1 -> 3.1 -> 3.2
  \-> 0.4 --------/              /
                                /
  (0.4 joins at 3.2, not 3.1)
```

Steps 0.1 through 3.2 represent the minimum path to a working injection pipeline with LLM recomposition. Estimated: 4-6 weeks of focused work.

---

## What This Plan Does NOT Include

Explicitly out of scope (deferred to future work):

1. **Multi-tenancy (`tenant_id` column).** directive-schema.md does not include it. vision-v4 does. This plan defers it because adding `tenant_id` to every table and every query is invasive and not needed for the initial single-tenant deployment. When multi-tenant is needed, add it as a Phase 8.

2. **Meilisearch hybrid search / embeddings.** directive-schema.md defines an OpenAI embedder configuration. This plan defers it because it adds a second external API dependency (OpenAI) and hybrid search is not needed until the directive count is large enough for keyword search to be insufficient. Add it after the directive population exceeds ~500.

3. **The `hive` CLI tool.** The MCP plugin is the primary consumer. A CLI tool for manual directive management is useful but not blocking.

4. **LLM-friendly error messages.** Recommended by ultrathink-devex Section 4. Valuable but can be incrementally improved during any phase.

5. **`repo` column on existing memory/tasks tables.** Identified in ultrathink-devex Section 9. Can be added as a standalone migration at any time.

---

## Final-Review-v3 Issue Resolution

| #   | Issue                                                              | Resolution in This Plan                                                                                                                                        |
| --- | ------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| C1  | Three conflicting directive schemas                                | Resolved: directive-schema.md declared authoritative. Authoritative Schema Declaration section at top of this document.                                        |
| C2  | LLM contextualization: vision says yes, injection-pipeline says no | Resolved: LLM wins. recomposition-design.md replaces injection-pipeline.md Section 4. Template-based recomposition is gone. Step 3.2 implements LLMRecomposer. |
| S1  | No seed directive bootstrap                                        | Resolved: Step 0.5 embeds 30-50 curated directives, loaded on first migration.                                                                                 |
| S2  | Gel schema mismatch                                                | Resolved: Step 5.1 uses directive-schema.md Section 4 (the detailed version with 6 relationship types), not the simplified vision-v4 version.                  |
| S3  | directive_pins and agent_preferences missing                       | Resolved: Step 3.1 creates both tables via goose migration 004.                                                                                                |
| S4  | No multi-tenancy                                                   | Deferred: explicitly documented as out-of-scope. Single-tenant is sufficient for initial deployment.                                                           |
| S5  | No migration tool                                                  | Resolved: Step 0.2 adds goose. All schema changes are goose migrations.                                                                                        |
| A1  | Meilisearch 10-word query limit                                    | Resolved: Step 2.1 includes query preprocessing that truncates to 10 meaningful terms.                                                                         |
| A2  | Error messages for LLM agents                                      | Deferred: documented as out-of-scope for this plan.                                                                                                            |
| A3  | repo column missing                                                | Deferred: documented as out-of-scope for this plan.                                                                                                            |
| A4  | OpenAI embedding dependency                                        | Deferred: hybrid search is out-of-scope until directive count warrants it.                                                                                     |
| A5  | Phase 6 ordering (empty pipeline)                                  | Resolved: Step 0.5 provides seed directives so the pipeline has content from day one. Phase 6 order is acceptable because seeds provide cold-start coverage.   |
| A6  | No total duration estimate                                         | Resolved: Critical path (Phase 0-3) estimated at 4-6 weeks. Full plan (Phase 0-7) estimated at 10-14 weeks.                                                    |
