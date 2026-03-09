# Build Plan v5: Behavioral Knowledge Engine

**Date:** 2026-03-09
**Status:** Authoritative build plan. Supersedes build-plan-v4.
**Inputs:** vision-v5.md (authoritative vision), directive-schema.md, injection-pipeline.md, recomposition-design.md, skill-replacement-analysis.md, hive-server-current.md, cockroachdb.md, meilisearch.md, gel-db.md, final-review-v3.md
**Verified against:** actual codebase on 2026-03-09

---

## What Changed From Build Plan v4

Twelve structural corrections, driven by realignment with vision-v5.md as the single authoritative source:

1. **Vision-v4.md is the authority.** Build plan v4 declared directive-schema.md as overriding vision-v5 Section 2.2. That was backwards. The vision defines what we are building; implementation documents like directive-schema.md provide supplementary detail that must align with the vision, not override it. Where directive-schema.md conflicts with vision-v5, vision-v5 wins.

2. **Directive taxonomy corrected.** Five kinds per vision Section 2.3: `behavioral`, `pattern`, `contextual`, `corrective`, `factual`. Field name is `kind` (TEXT), not `directive_type`. The fabricated `procedural` and `guardrail` kinds are removed.

3. **Priority replaced by weight.** `weight FLOAT8 DEFAULT 1.0` (range 0.0-2.0) per vision Section 2.2. The `priority INT 1-100` field from directive-schema.md is removed everywhere -- Go structs, SQL, indexes, ranking formulas, Gel schema.

4. **Multi-tenancy restored.** `tenant_id UUID NOT NULL` appears on every table in the vision's schema. Build plan v4 explicitly deferred it. This plan includes it from the start because the vision requires it and adding it retroactively to every table, index, and query is far more expensive than including it upfront.

5. **Ranking formula corrected.** Vision Section 4.4 defines: `score = (meilisearch_relevance * 0.4) + (effectiveness * 0.3) + (weight * 0.2) + (recency_bonus * 0.1)`. The different formula from injection-pipeline.md (with source_boost, type-level priority normalization, and freshness as separate factors) is removed.

6. **Effectiveness formula corrected.** Vision Section 6.2 defines: `effectiveness = (times_followed - times_negative) / GREATEST(times_injected, 1)`. This penalizes negative outcomes. Denormalized counters (`times_injected`, `times_followed`, `times_ignored`, `times_negative`) live on the directives table.

7. **Feedback outcomes reduced to 3.** Vision Section 6.1: `followed`, `ignored`, `negative`. The fabricated `partially_followed`, `inapplicable`, `helpful`, `unhelpful` outcomes are removed.

8. **All fabricated constraints removed.** No per-source timeouts (150ms, 200ms). No pipeline retrieval deadlines (400ms, 200ms). No fabricated token budget bounds (min 100, max 2000 -- just default 500). No fabricated similarity threshold (0.8). No fabricated rate limits (60/min, 120/min). No per-directive token limit (150 tokens). These are implementation details to be determined during development, not spec'd constraints.

9. **Endpoint count matches vision.** Vision Section 8.1 defines 9 endpoints. Admin endpoints (sync, backup, integrity) and metrics are implementation details added during hardening, not core API design. Directive CRUD is limited to what the vision specifies (GET list, GET by id, PATCH).

10. **Session tracking uses vision's tables.** `agent_sessions`, `injections`, `injection_outcomes` per vision Section 5.1, not a flat `injection_log`.

11. **Removed directive_pins and agent_preferences tables.** Not in the vision. If needed, they are future work.

12. **Caching removed as a build phase.** Three-layer caching was a premature optimization. Build the system, measure, then cache. Caching is noted as future optimization work, not Phase 3.3.

---

## Schema Alignment Note

**vision-v5.md is the single source of truth for the schema and system design.** directive-schema.md provides implementation details (index configurations, EdgeQL queries, Meilisearch settings) that supplement the vision. Where directive-schema.md conflicts with vision-v5 on field names, types, enum values, table structure, or behavioral semantics, vision-v5 wins.

Specific alignment decisions:

- Field name: `kind` TEXT (vision) not `directive_type` (directive-schema.md)
- Taxonomy: `behavioral`, `pattern`, `contextual`, `corrective`, `factual` (vision) not `behavioral`, `procedural`, `contextual`, `guardrail` (directive-schema.md)
- Weight: `weight FLOAT8 DEFAULT 1.0` range 0.0-2.0 (vision) not `priority INT 1-100` (directive-schema.md)
- Outcomes: `followed`, `ignored`, `negative` (vision) not expanded set (directive-schema.md)
- Multi-tenancy: included (vision) not deferred

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
| Monolithic Store interface    | **MODIFIED** | Split into composed interfaces (MemoryStore, TaskStore, AgentStore, DirectiveStore, etc.).                                            |

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
    Content      string
    InputTokens  int
    OutputTokens int
    Model        string
    StopReason   string
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
   - Model selection is per-call-site (recomposition uses Sonnet-class, decomposition uses Sonnet-class)
   - Model strings are not hardcoded to specific versions -- use "Sonnet-class model" conceptually; actual model IDs are configurable via environment variables

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
- Model IDs are configurable, not hardcoded to specific version strings

---

### Step 0.5: Seed Directives

- **Step ID:** 0.5
- **Dependencies:** 0.2
- **Scope:** S (< 1 day)

**What gets built:**

A manually curated set of 30-50 high-value directives embedded in the binary, loaded on first migration. This solves the cold start problem identified in final-review-v3 Section 6.

1. Create `seed/directives.json` -- JSON array of directive objects:

   - Each directive has: `content`, `kind`, `source_type`, `source_name`, `trigger_tags`, `trigger_intent`, `trigger_phase`, `trigger_scope`, `weight`
   - Curated from the skill-replacement-analysis's highest-value directives

2. Create `migrations/002_seed_directives.sql` -- goose migration that:

   - Checks if directives table is empty
   - If empty, inserts the seed set
   - Idempotent: re-running does nothing if directives already exist

3. The seed set covers universal behavioral directives:

   **From Superpowers (anti-rationalization, highest weight):**

   - "Before claiming any task is complete, run the verification command and read its full output." (behavioral, weight 1.8)
   - "When debugging, reproduce the problem first. Do not attempt a fix before you can reliably trigger the failure." (behavioral, weight 1.7)
   - "Before any creative work, brainstorm at least 3 meaningfully different approaches." (behavioral, weight 1.2)
   - "When writing tests, follow red-green-refactor: write a failing test first, then make it pass, then refactor." (behavioral, weight 1.1)

   **From GSD (workflow discipline):**

   - "Commit completed work atomically. One commit per completed task with conventional commit messages." (behavioral, weight 1.0)
   - "When a sub-agent finishes, verify its output against the acceptance criteria before marking the task done." (behavioral, weight 1.5)

   **From Allium (specification discipline):**

   - "When specs and code disagree, classify the mismatch: is it a spec bug, code bug, aspirational design, or intentional gap?" (contextual, weight 1.2)

   Plus ~25 additional directives covering testing, error handling, code review, debugging, and implementation patterns.

4. Create `internal/store/seed.go` -- `SeedDirectives(ctx, db)` function:
   - Reads `seed/directives.json` (embedded via `//go:embed`)
   - Called by the migration or by explicit bootstrap command
   - Skips if directives already exist in the table
   - Seeds include a default `tenant_id` (configurable via `HIVE_DEFAULT_TENANT_ID` or a generated UUID)

**Tests:**

- `internal/store/seed_test.go`:
  - TestSeedDirectives_EmptyDB -- inserts all seed directives
  - TestSeedDirectives_NonEmpty -- skips insertion
  - TestSeedDirectives_ValidJSON -- all embedded directives parse correctly

**Acceptance criteria:**

- 30-50 curated directives exist as embedded JSON
- Directives are loaded on first startup when the database is empty
- Seed is idempotent (safe to run multiple times)
- All seed directives include `tenant_id`
- The injection pipeline has directives to work with from day one

---

## Phase 1: Directive Storage

**Goal:** Implement the directive data model in CockroachDB, basic operations, and the ingest endpoint that accepts raw text documents for future decomposition.

### Step 1.1: Directive Schema and Store

- **Step ID:** 1.1
- **Dependencies:** 0.2, 0.3
- **Scope:** M (1-3 days)

**What gets built:**

1. Add directive model types to `internal/model/directive.go`:

```go
package model

type DirectiveKind string
const (
    KindBehavioral  DirectiveKind = "behavioral"
    KindPattern     DirectiveKind = "pattern"
    KindContextual  DirectiveKind = "contextual"
    KindCorrective  DirectiveKind = "corrective"
    KindFactual     DirectiveKind = "factual"
)

type Directive struct {
    ID              string        `json:"id"`
    Content         string        `json:"content"`
    Kind            DirectiveKind `json:"kind"`

    // Provenance
    SourceType      string        `json:"source_type"`     // 'skill', 'experience', 'feedback', 'observation', 'user'
    SourceID        string        `json:"source_id"`
    SourceName      string        `json:"source_name"`     // Human-readable: 'superpowers:systematic-debugging'
    SourceSection   string        `json:"source_section"`

    // Context triggers
    TriggerTags     []string       `json:"trigger_tags"`
    TriggerIntent   string         `json:"trigger_intent"`
    TriggerPhase    string         `json:"trigger_phase"`   // 'planning', 'implementation', 'debugging', 'review', 'any'
    TriggerScope    string         `json:"trigger_scope"`   // 'global', 'repo:...', 'project:...'

    // Effectiveness (denormalized counters)
    TimesInjected   int64          `json:"times_injected"`
    TimesFollowed   int64          `json:"times_followed"`
    TimesIgnored    int64          `json:"times_ignored"`
    TimesNegative   int64          `json:"times_negative"`
    Effectiveness   float64        `json:"effectiveness"`   // Computed: (followed - negative) / max(injected, 1)

    // Relationships (denormalized)
    RelatedIDs      []string       `json:"related_ids,omitempty"`
    SupersedesID    *string        `json:"supersedes_id,omitempty"`
    ChainID         *string        `json:"chain_id,omitempty"`

    // Metadata
    Weight          float64        `json:"weight"`          // 0.0-2.0, default 1.0
    TokenCost       int            `json:"token_cost"`
    Active          bool           `json:"active"`
    TenantID        string         `json:"tenant_id"`
    CreatedAt       time.Time      `json:"created_at"`
    UpdatedAt       time.Time      `json:"updated_at"`
}

type DirectiveFilter struct {
    Kind         *DirectiveKind
    SourceType   *string
    TriggerPhase *string
    TriggerScope *string
    Active       *bool
    TenantID     string
    Tags         []string
    Limit        int
    Offset       int
}

type IngestionSource struct {
    ID              string    `json:"id"`
    Name            string    `json:"name"`
    SourceType      string    `json:"source_type"`
    ContentHash     string    `json:"content_hash"`
    Version         int       `json:"version"`
    DirectivesCount int       `json:"directives_count"`
    LastIngested    time.Time `json:"last_ingested"`
    TenantID        string    `json:"tenant_id"`
}
```

2. Add `DirectiveStore` interface to `internal/model/store.go`:

```go
type DirectiveStore interface {
    CreateDirective(ctx context.Context, d *Directive) (*Directive, error)
    GetDirective(ctx context.Context, id string, tenantID string) (*Directive, error)
    ListDirectives(ctx context.Context, f DirectiveFilter) ([]*Directive, error)
    UpdateDirective(ctx context.Context, id string, tenantID string, upd DirectiveUpdate) (*Directive, error)
    DeactivateDirective(ctx context.Context, id string, tenantID string) error
    CreateIngestionSource(ctx context.Context, src *IngestionSource) (*IngestionSource, error)
    GetIngestionSource(ctx context.Context, id string) (*IngestionSource, error)
}
```

3. Add CockroachDB schema via goose migration `migrations/003_directives.sql`:

```sql
CREATE TABLE directives (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The directive itself
    content         TEXT        NOT NULL,
    kind            TEXT        NOT NULL,       -- 'behavioral', 'pattern', 'contextual', 'corrective', 'factual'

    -- Provenance
    source_type     TEXT        NOT NULL,       -- 'skill', 'experience', 'feedback', 'observation', 'user'
    source_id       TEXT        NOT NULL,
    source_name     TEXT        NOT NULL,
    source_section  TEXT        NOT NULL DEFAULT '',

    -- Context triggers
    trigger_tags    JSONB       NOT NULL DEFAULT '[]'::JSONB,
    trigger_intent  TEXT        NOT NULL DEFAULT '',
    trigger_phase   TEXT        NOT NULL DEFAULT '',
    trigger_scope   TEXT        NOT NULL DEFAULT '',

    -- Effectiveness (denormalized counters)
    times_injected  INT8        NOT NULL DEFAULT 0,
    times_followed  INT8        NOT NULL DEFAULT 0,
    times_ignored   INT8        NOT NULL DEFAULT 0,
    times_negative  INT8        NOT NULL DEFAULT 0,
    effectiveness   FLOAT8      NOT NULL DEFAULT 0.0,

    -- Relationships (denormalized; Gel has the full graph)
    related_ids     JSONB       NOT NULL DEFAULT '[]'::JSONB,
    supersedes_id   UUID,
    chain_id        UUID,

    -- Metadata
    weight          FLOAT8      NOT NULL DEFAULT 1.0,
    token_cost      INT4        NOT NULL DEFAULT 0,
    active          BOOLEAN     NOT NULL DEFAULT true,
    tenant_id       UUID        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Indexes per vision Section 2.2
CREATE INDEX idx_directives_active ON directives (active, tenant_id) WHERE active = true;
CREATE INDEX idx_directives_phase ON directives (trigger_phase, tenant_id) WHERE active = true;
CREATE INDEX idx_directives_scope ON directives (trigger_scope, tenant_id) WHERE active = true;
CREATE INDEX idx_directives_effectiveness ON directives (effectiveness DESC, tenant_id) WHERE active = true;
CREATE INVERTED INDEX idx_directives_tags ON directives (trigger_tags);

-- Ingestion tracking per vision Section 5.1
CREATE TABLE ingestion_sources (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT        NOT NULL,
    source_type     TEXT        NOT NULL,
    content_hash    TEXT        NOT NULL,
    version         INT4        NOT NULL DEFAULT 1,
    directives_count INT4       NOT NULL DEFAULT 0,
    last_ingested   TIMESTAMPTZ NOT NULL DEFAULT now(),
    tenant_id       UUID        NOT NULL,

    CONSTRAINT uq_source_name_tenant UNIQUE (name, tenant_id)
);
```

4. Implement `internal/store/directives.go`:

   - `CreateDirective` -- INSERT with RETURNING
   - `GetDirective` -- SELECT with tenant_id filter
   - `ListDirectives` -- parameterized query with filters, tenant_id always required
   - `UpdateDirective` -- UPDATE selected fields
   - `DeactivateDirective` -- soft delete (SET active = false)
   - `CreateIngestionSource` / `GetIngestionSource`

5. Update `model.Store` interface to compose `DirectiveStore`.

**Tests:**

- `internal/store/directives_test.go`:
  - TestCreateDirective -- creates and retrieves
  - TestGetDirective_NotFound -- returns ErrNotFound
  - TestListDirectives_FilterByKind -- filters by kind
  - TestListDirectives_FilterByPhase -- filters by trigger_phase
  - TestListDirectives_TenantIsolation -- tenant_id filter prevents cross-tenant access
  - TestUpdateDirective_WeightChange -- weight updates correctly
  - TestDeactivateDirective_SoftDelete -- sets active=false
  - TestCreateIngestionSource -- creates and retrieves

**Acceptance criteria:**

- All directive tables exist in the schema (created via goose migration)
- Schema matches vision-v5 Section 2.2 (kind TEXT, weight FLOAT8, tenant_id UUID on all tables)
- CRUD operations work for directives and ingestion sources
- Soft delete works (active flag, not physical delete)
- Filtering by kind, phase, scope, active status, and tags works
- All queries include tenant_id
- All tests pass against CockroachDB

---

### Step 1.2: Directive Handlers (Vision Endpoints Only)

- **Step ID:** 1.2
- **Dependencies:** 1.1
- **Scope:** M (1-3 days)

**What gets built:**

Per vision Section 8.1, the directive-related endpoints are:

1. `internal/handlers/directives.go` -- Huma-registered handlers:

   - `GET /api/v1/directives` -- List/search the directive catalog (admin/debug). Filters: kind, phase, scope, active, tags, limit, offset.
   - `GET /api/v1/directives/{id}` -- Get single directive with full metadata (admin/debug)
   - `PATCH /api/v1/directives/{id}` -- Manually adjust a directive's weight or active status

2. `POST /api/v1/ingest` -- Ingest endpoint (vision Section 8.4):

   - Accepts `{ "name": "superpowers:brainstorming", "source_type": "skill", "content": "<full markdown text>", "metadata": {...} }`
   - Creates an `ingestion_source` record with the content hash
   - Stores the raw content for later decomposition (Phase 6)
   - Returns 202 Accepted with the ingestion ID and status `"processing"`
   - For now, this is a storage-only endpoint. The actual LLM decomposition is Phase 6.

3. Wire new handlers into `internal/handlers/handlers.go`:
   - `registerDirectives(a, api)` called in the authenticated router group

**Tests:**

- `internal/handlers/directives_test.go`:
  - TestListDirectives_Empty -- returns empty array
  - TestListDirectives_WithFilters
  - TestGetDirective_NotFound -- 404
  - TestPatchDirective_UpdateWeight
  - TestPatchDirective_Deactivate
  - TestIngest_CreatesSource
  - TestIngest_DuplicateHash -- detects re-ingestion of same content

**Acceptance criteria:**

- Directive endpoints match vision Section 8.1 (GET list, GET by id, PATCH -- no POST create, no DELETE)
- Ingest endpoint accepts raw text and creates an ingestion source
- Duplicate content detection works via content_hash
- All existing endpoints continue working (no regressions)

---

## Phase 2: Meilisearch Integration

**Goal:** Add Meilisearch as a search layer for directives. Sync directives from CockroachDB to Meilisearch. Enable semantic discovery for the injection pipeline.

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
    DirectiveID   string
    Content       string
    Score         float64  // 0.0-1.0 relevance score
    Kind          string
    Weight        float64
    Effectiveness float64
}

type SearchRequest struct {
    Query       string
    TenantID    string
    Phase       string
    Scope       string
    Limit       int
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
  - `ConfigureIndex` sets up the `directives` index per vision Section 5.2:
    - searchableAttributes: `content`, `trigger_intent`, `trigger_tags`, `source_name`
    - filterableAttributes: `kind`, `trigger_phase`, `trigger_scope`, `active`, `tenant_id`, `chain_id`
    - sortableAttributes: `effectiveness`, `weight`, `created_at`
    - rankingRules: `words`, `typo`, `proximity`, `attribute`, `sort`, `exactness`
    - Synonyms per vision Section 5.2 (debug/fix/investigate/troubleshoot, test/spec/assertion/verification, etc.)
  - `IndexDirective` / `IndexDirectives` transforms `model.Directive` to Meilisearch document format
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
  - TestSearchFilters -- phase and scope filters work
  - TestSearchTenantIsolation -- tenant_id filter prevents cross-tenant results
  - TestQueryPreprocessing -- long summaries are truncated to 10 terms
  - TestRemoveDirective -- removes from index
  - TestHealthy -- returns true when Meilisearch is up

**Acceptance criteria:**

- `Searcher` interface exists with Meilisearch and Noop implementations
- Meilisearch index configuration matches vision Section 5.2
- Query preprocessing truncates to 10 meaningful terms
- Directives can be indexed and searched
- tenant_id filtering works in Meilisearch
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
   - After `DeactivateDirective` succeeds, fire async remove from Meilisearch
   - Use `context.Background()` for async fire-and-forget (sync failure does not fail the HTTP request)

3. Add sync status to `/ready` health check:
   - `/ready` returns `{"status": "ready", "meilisearch": "connected"}` or `{"status": "ready", "meilisearch": "unavailable"}`

**Tests:**

- `internal/search/sync_test.go`:
  - TestSyncDirective -- creates in CRDB, syncs to Meilisearch, searchable
  - TestSyncAll -- full rebuild indexes all directives
  - TestReconcileLoop -- updated directive gets re-indexed within interval
  - TestSyncFailure_DoesNotBlockWrite -- CRDB write succeeds even if Meilisearch is down

**Acceptance criteria:**

- New/updated directives appear in Meilisearch search results (eventual consistency, typically < 1 second)
- Full re-sync can be triggered programmatically
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

// InjectRequest matches vision Section 8.2
type InjectRequest struct {
    SessionID           string         `json:"session_id"`
    Context             InjectContext  `json:"context"`
    TokenBudget         int            `json:"token_budget,omitempty"`   // default 500
    PreviousInjectionID string         `json:"previous_injection_id,omitempty"`
}

type InjectContext struct {
    Intent              string   `json:"intent"`
    Files               []string `json:"files,omitempty"`
    Repo                string   `json:"repo,omitempty"`
    Phase               string   `json:"phase,omitempty"`        // planning, implementation, debugging, review, brainstorming
    RecentActions       []string `json:"recent_actions,omitempty"`
    ConversationSummary string   `json:"conversation_summary,omitempty"`
    OpenRequirements    []string `json:"open_requirements,omitempty"`
    CurrentProject      string   `json:"current_project,omitempty"`
}

// InjectionResponse matches vision Section 8.2
type InjectionResponse struct {
    InjectionID          string              `json:"injection_id"`
    Directives           []InjectedDirective `json:"directives"`
    TokensUsed           int                 `json:"tokens_used"`
    TokenBudget          int                 `json:"token_budget"`
    CandidatesConsidered int                 `json:"candidates_considered"`
    CandidatesSelected   int                 `json:"candidates_selected"`
}

// InjectedDirective matches vision Section 8.2 response format
type InjectedDirective struct {
    ID         string  `json:"id"`
    Content    string  `json:"content"`
    Kind       string  `json:"kind"`
    Source     string  `json:"source"`
    Confidence float64 `json:"confidence"`
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

func (p *Pipeline) Inject(ctx context.Context, agentID string, tenantID string, req *model.InjectRequest) (*model.InjectionResponse, error)
```

- `internal/inject/retrieve.go` -- Fan-out retrieval:

  - `retrieveFromMeilisearch(ctx, tenantID, req) ([]candidate, error)` -- builds search query from context.ConversationSummary + context.Intent, searches directive index
  - `retrieveFromCRDB(ctx, tenantID, req) ([]candidate, error)` -- runs structured queries per vision Section 4.3:
    - Active directives matching trigger_phase and trigger_scope for tenant
    - Recently injected directive IDs for deduplication (via previous_injection_id)
  - Fan-out uses `errgroup.Group` -- both queries run in parallel

- `internal/inject/rank.go` -- Ranking and selection per vision Section 4.4:
  - Merge candidates from all sources, deduplicate by directive ID
  - Score each candidate: `score = (meilisearch_relevance * 0.4) + (effectiveness * 0.3) + (weight * 0.2) + (recency_bonus * 0.1)`
  - Where:
    - `meilisearch_relevance`: 0.0-1.0, how semantically relevant to the current context
    - `effectiveness`: 0.0-1.0, `(times_followed - times_negative) / max(times_injected, 1)`
    - `weight`: 0.0-2.0, normalized to 0.0-1.0 for scoring (divide by 2.0)
    - `recency_bonus`: 0.0-0.5, bonus for directives from the agent's recent experience with this repo
  - Select top candidates within token budget
  - `EstimateTokens(content string) int` -- `len(content) / 4` heuristic

3. Add session tracking tables via goose migration `migrations/004_sessions_and_injections.sql` per vision Section 5.1:

```sql
CREATE TABLE agent_sessions (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id        TEXT        NOT NULL,
    tenant_id       UUID        NOT NULL,
    repo            TEXT        NOT NULL DEFAULT '',
    project_id      UUID,
    phase           TEXT        NOT NULL DEFAULT '',
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_active_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    summary         TEXT        NOT NULL DEFAULT '',
    active          BOOLEAN     NOT NULL DEFAULT true
);
CREATE INDEX idx_agent_sessions_agent ON agent_sessions (agent_id, tenant_id) WHERE active = true;

CREATE TABLE injections (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID        NOT NULL REFERENCES agent_sessions(id),
    tenant_id       UUID        NOT NULL,
    context_hash    TEXT        NOT NULL,
    directives      JSONB       NOT NULL,  -- Array of {directive_id, confidence} pairs
    tokens_used     INT4        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_injections_session ON injections (session_id, created_at DESC);
CREATE INDEX idx_injections_tenant ON injections (tenant_id, created_at DESC);

CREATE TABLE injection_outcomes (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    injection_id    UUID        NOT NULL REFERENCES injections(id),
    tenant_id       UUID        NOT NULL,
    directive_id    UUID        NOT NULL REFERENCES directives(id),
    outcome         TEXT        NOT NULL,  -- 'followed', 'ignored', 'negative'
    evidence        TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_injection_outcomes_injection ON injection_outcomes (injection_id);
CREATE INDEX idx_injection_outcomes_directive ON injection_outcomes (directive_id, tenant_id);
```

4. `internal/handlers/inject.go` -- Huma handler:

   - `POST /api/v1/inject` -- validates request, calls Pipeline.Inject, returns response
   - X-Agent-ID header is required (not optional)
   - Default token_budget: 500
   - Logs the injection in the `injections` table (creating/updating an `agent_session` as needed)

5. Wire into router: `registerInject(a, api)` in handlers.go

**Tests:**

- `internal/inject/pipeline_test.go`:
  - TestInject_EmptyDirectives -- returns empty when no directives exist
  - TestInject_ReturnsRelevantDirectives -- seeds directives, injects, gets results
  - TestInject_RespectsTokenBudget -- does not exceed budget
  - TestInject_DeduplicatesPreviousInjection -- excludes recently injected
  - TestInject_MeilisearchDown_FallsBackToCRDB -- degraded but functional
  - TestInject_TenantIsolation -- tenant A cannot see tenant B directives
- `internal/inject/rank_test.go`:
  - TestRankCandidates -- verifies scoring formula matches vision Section 4.4
  - TestTokenBudgetPacking -- verifies greedy packing
  - TestDeduplication -- same directive from 2 sources merged
  - TestEffectivenessImpactsRanking -- higher effectiveness = higher score
  - TestWeightImpactsRanking -- higher weight = higher score
- `internal/handlers/inject_test.go`:
  - TestInjectEndpoint_Success
  - TestInjectEndpoint_MissingAgentID -- 400 error

**Acceptance criteria:**

- `POST /api/v1/inject` retrieves and ranks directives per vision Section 4
- Fan-out to Meilisearch + CockroachDB with graceful degradation
- Ranking formula: `(relevance * 0.4) + (effectiveness * 0.3) + (weight * 0.2) + (recency * 0.1)`
- Token budget defaults to 500
- Session tracking uses `agent_sessions` and `injections` tables per vision Section 5.1
- All queries include tenant_id
- Injection is logged for session deduplication

---

### Step 3.2: Recomposer Interface and LLM Implementation

- **Step ID:** 3.2
- **Dependencies:** 0.4, 3.1
- **Scope:** L (3-5 days)

**What gets built:**

This is the core change from v3. Template-based recomposition is replaced by LLM synthesis per vision Section 4.6.

1. Create `internal/inject/recompose.go` -- Recomposer interface and types:

```go
// Recomposer transforms raw directives into contextual micro-prompts.
type Recomposer interface {
    Recompose(ctx context.Context, input *RecompositionInput) (*RecompositionOutput, error)
}

type RecompositionInput struct {
    // Selected directives
    Directives []DirectiveForSynthesis

    // Request context (from InjectRequest)
    Context    model.InjectContext

    // Additional context from knowledge base
    ProjectMemories []MemorySnippet
    AgentMemories   []MemorySnippet

    // Constraints
    OutputTokenBudget int
}

type DirectiveForSynthesis struct {
    ID            string
    Content       string
    Kind          string
    Weight        float64
    Effectiveness float64
    SourceName    string
}

type MemorySnippet struct {
    Key     string
    Value   string
    Source  string
}

type RecompositionOutput struct {
    Directives []model.InjectedDirective
    Fallback   bool
}
```

2. `internal/inject/recompose_llm.go` -- `LLMRecomposer` implementation:

   - Constructor takes `llm.Client`, model string (configurable, Sonnet-class by default), system prompt
   - System prompt instructs the LLM to transform raw directive principles into contextual micro-prompts adapted to the agent's current situation
   - `Recompose` method:
     1. Assembles the LLM prompt from RecompositionInput (directives + context + memories)
     2. Calls the LLM with structured output request
     3. Parses JSON response into contextualized directives
     4. Token budget enforcement: if LLM output exceeds budget, trim lowest-weight directives
     5. Returns `RecompositionOutput` with `Fallback: false`
   - Timeout: 10-15 seconds for the LLM call. This is an intentional correction of the vision's aspirational 50ms. An LLM call cannot complete in 50ms. A generous timeout is appropriate because accuracy matters more than speed.
   - Temperature: configurable, default not set to 0.0. The recomposer needs some creativity for contextual adaptation. A reasonable range is 0.3-0.7; the exact default should be tuned empirically. Configurable via `HIVE_RECOMPOSER_TEMPERATURE`.

3. `internal/inject/recompose_fallback.go` -- `FallbackRecomposer` implementation:

   - Returns raw directive `content` as-is, formatted as InjectedDirective entries
   - Sets `Fallback: true` in output
   - Used when: LLM client is NoopClient, LLM call fails, LLM call times out

4. Integrate the recomposer into the Pipeline:

   - Pipeline receives a `Recomposer` in its constructor
   - After ranking selects candidates, Pipeline calls `Recomposer.Recompose`
   - If `LLMRecomposer` fails, fall back to `FallbackRecomposer`
   - Pipeline wraps the output into `InjectionResponse`

5. Fetch relevant memories for recomposition context:

   - Query the existing memory store for entries matching the project and agent
   - Pass up to 3 most recent/relevant memories as `ProjectMemories` / `AgentMemories`
   - This enriches the LLM's context for better synthesis

6. Configuration via environment:
   - `HIVE_RECOMPOSER` -- `llm` (default when API key exists) or `fallback` (explicit override)
   - `HIVE_RECOMPOSER_MODEL` -- LLM model for recomposition (Sonnet-class by default; let configuration determine the specific model)
   - `HIVE_RECOMPOSER_TIMEOUT` -- timeout for LLM call (default: 15s)
   - `HIVE_RECOMPOSER_TEMPERATURE` -- temperature for LLM call (default: 0.4)

**Tests:**

- `internal/inject/recompose_llm_test.go`:
  - TestLLMRecompose_Success -- mock LLM returns valid JSON, parses correctly
  - TestLLMRecompose_InvalidJSON -- falls back to FallbackRecomposer
  - TestLLMRecompose_Timeout -- respects deadline, falls back
  - TestLLMRecompose_TokenBudgetEnforcement -- trims over-budget output
- `internal/inject/recompose_fallback_test.go`:
  - TestFallbackRecompose_ReturnsRawContent -- directive content verbatim
  - TestFallbackRecompose_SetsFallbackTrue
- `internal/inject/pipeline_test.go` (extend):
  - TestInject_WithLLMRecomposition -- end-to-end with mock LLM
  - TestInject_LLMFails_FallsBack -- LLM error triggers fallback
  - TestInject_FallbackMode -- explicit fallback configuration

**Acceptance criteria:**

- `LLMRecomposer` calls Sonnet-class LLM and produces contextual micro-prompts
- Structured JSON output is parsed and validated
- Token budget is enforced post-LLM (trim if over)
- Fallback to raw directive content when LLM is unavailable
- Feature flag allows switching between LLM and fallback modes
- Relevant memories are included in recomposition context
- End-to-end pipeline works: retrieve -> rank -> recompose -> respond
- Contextualization timeout is 10-15 seconds (intentional correction of vision's aspirational 50ms)

**Latency budget (revised):**

Accuracy over speed. No hard latency constraints. The LLM recomposition call takes 2.5-3.5 seconds and that is fine. The full pipeline including retrieval, ranking, and recomposition will typically complete in 3-4 seconds. This is acceptable because the MCP plugin calls inject on context transitions, not on every message.

| Phase                        | Typical Duration | Notes                            |
| ---------------------------- | ---------------- | -------------------------------- |
| Request parsing + validation | ~1ms             |                                  |
| Fan-out retrieval (parallel) | 50-200ms         | Meilisearch + CRDB               |
| Ranking + selection          | ~5ms             |                                  |
| LLM recomposition            | 2,500-3,500ms    | Sonnet-class, ~250 tokens output |
| Parse + validate output      | ~1ms             |                                  |
| **Total**                    | **~3-4 seconds** | Acceptable                       |

---

## Phase 4: Feedback Loop

**Goal:** Track directive outcomes, update effectiveness scores, enable experience-derived directives from session completion. Per vision Sections 6.1-6.4.

### Step 4.1: Feedback Endpoint and Effectiveness Scoring

- **Step ID:** 4.1
- **Dependencies:** 3.1
- **Scope:** M (1-3 days)

**What gets built:**

1. Add feedback model types to `internal/model/feedback.go`:

```go
type FeedbackOutcome string
const (
    OutcomeFollowed  FeedbackOutcome = "followed"
    OutcomeIgnored   FeedbackOutcome = "ignored"
    OutcomeNegative  FeedbackOutcome = "negative"
)

// FeedbackRequest matches vision Section 8.3
type FeedbackRequest struct {
    InjectionID    string             `json:"injection_id"`
    Outcomes       []DirectiveOutcome `json:"outcomes"`
    SessionOutcome string             `json:"session_outcome,omitempty"`  // success, failure, partial, ongoing
    SessionSummary string             `json:"session_summary,omitempty"`
}

type DirectiveOutcome struct {
    DirectiveID string          `json:"directive_id"`
    Outcome     FeedbackOutcome `json:"outcome"`       // 'followed', 'ignored', 'negative'
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
    RecordOutcome(ctx context.Context, tenantID string, injectionID string, directiveID string, outcome FeedbackOutcome, evidence string) error
    UpdateEffectiveness(ctx context.Context, tenantID string, directiveID string) error
    RecordSessionComplete(ctx context.Context, tenantID string, req *SessionCompleteRequest, agentID string) error
}
```

3. Implement `internal/store/feedback.go`:
   - `RecordOutcome` -- INSERT into injection_outcomes, then update denormalized counters on directives table per vision Section 6.2:

```sql
-- For 'followed' outcome
UPDATE directives
SET times_injected = times_injected + 1,
    times_followed = times_followed + 1,
    effectiveness = (times_followed::FLOAT8 - times_negative::FLOAT8) / GREATEST(times_injected::FLOAT8, 1),
    updated_at = now()
WHERE id = $1 AND tenant_id = $2;

-- For 'ignored' outcome
UPDATE directives
SET times_injected = times_injected + 1,
    times_ignored = times_ignored + 1,
    effectiveness = (times_followed::FLOAT8 - times_negative::FLOAT8) / GREATEST(times_injected::FLOAT8, 1),
    updated_at = now()
WHERE id = $1 AND tenant_id = $2;

-- For 'negative' outcome (directive was followed but made things worse)
UPDATE directives
SET times_injected = times_injected + 1,
    times_negative = times_negative + 1,
    effectiveness = (times_followed::FLOAT8 - times_negative::FLOAT8) / GREATEST(times_injected::FLOAT8, 1),
    weight = GREATEST(weight * 0.8, 0.1),  -- Reduce weight by 20%, floor at 0.1
    updated_at = now()
WHERE id = $1 AND tenant_id = $2;
```

- `UpdateEffectiveness` -- recalculate effectiveness from denormalized counters (fallback/correction path)
- `RecordSessionComplete` -- INSERT into a session_completions table and optionally create experience-derived directives per vision Section 6.3

4. Add `session_completions` table via goose migration `migrations/005_session_completions.sql`:

```sql
CREATE TABLE session_completions (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id  UUID        NOT NULL REFERENCES agent_sessions(id),
    agent_id    TEXT        NOT NULL,
    tenant_id   UUID        NOT NULL,
    summary     TEXT        NOT NULL DEFAULT '',
    repo        TEXT        NOT NULL DEFAULT '',
    outcome     TEXT        NOT NULL DEFAULT '',
    key_insight TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_session_completions_session ON session_completions(session_id);
CREATE INDEX idx_session_completions_repo ON session_completions(repo, tenant_id, created_at DESC);
```

5. `internal/handlers/feedback.go` -- Huma handlers per vision Section 8.3:

   - `POST /api/v1/feedback` -- record directive outcomes from an injection, update effectiveness scores and denormalized counters
   - `POST /api/v1/feedback/session-complete` -- record session completion with summary and key insight, optionally create experience-derived directives

6. Wire into router: `registerFeedback(a, api)` in handlers.go

**Tests:**

- `internal/store/feedback_test.go`:
  - TestRecordOutcome_Followed -- increments times_followed, recalculates effectiveness
  - TestRecordOutcome_Ignored -- increments times_ignored, recalculates effectiveness
  - TestRecordOutcome_Negative -- increments times_negative, reduces weight, recalculates effectiveness
  - TestEffectivenessFormula -- verifies (followed - negative) / max(injected, 1)
  - TestNegativeOutcome_PenalizesEffectiveness -- effectiveness decreases with negative outcomes
  - TestRecordSessionComplete_CreatesRecord
- `internal/handlers/feedback_test.go`:
  - TestFeedbackEndpoint_Success
  - TestFeedbackEndpoint_InvalidOutcome -- 422 for outcomes other than followed/ignored/negative
  - TestSessionCompleteEndpoint_Success

**Acceptance criteria:**

- `POST /api/v1/feedback` records outcomes and updates effectiveness scores
- `POST /api/v1/feedback/session-complete` records session summaries
- Only 3 valid outcomes: `followed`, `ignored`, `negative`
- Effectiveness formula: `(followed - negative) / max(injected, 1)` per vision Section 6.2
- Negative outcomes reduce both effectiveness and weight per vision Section 6.2
- Denormalized counters on directives table are updated atomically
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
   - `dbschema/default.esdl` -- Gel schema per vision Section 5.3:

```sdl
module default {
    type Directive {
        required content: str;
        required kind: str;  -- behavioral, pattern, contextual, corrective, factual
        required source_name: str;
        required weight: float64 { default := 1.0; };
        required effectiveness: float64 { default := 0.0; };
        required token_cost: int32 { default := 0; };
        required active: bool { default := true; };
        crdb_id: uuid;  -- Reference back to CRDB

        # Relationships
        multi related_to: Directive;
        multi superseded_by: Directive;
        link chain: DirectiveChain;
        property sequence_in_chain: int32;

        # Computed
        property influence_score := .weight * .effectiveness;
    }

    type DirectiveChain {
        required name: str;          -- "systematic-debugging-methodology"
        required description: str;   -- "Full debugging workflow from reproduction to verification"
        multi member directives: Directive {
            property sequence_order: int32;
        };

        # Computed
        property total_tokens := sum(.directives.token_cost);
        property avg_effectiveness := math::mean(.directives.effectiveness);
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
    GetChainForDirective(ctx context.Context, directiveID string) ([]*model.Directive, error)
    GetRelatedDirectives(ctx context.Context, directiveID string, depth int) ([]*model.Directive, error)
    SyncDirective(ctx context.Context, d *model.Directive) error
    SyncRelationship(ctx context.Context, fromID, toID string, relType string) error
    Healthy(ctx context.Context) bool
}
```

- `internal/graph/noop.go` -- `NoopGraphStore` that returns nil/empty (graceful degradation when Gel is unavailable)
- `internal/graph/gel.go` -- Gel DB implementation:
  - Constructor takes Gel connection options
  - `GetChainForDirective` -- EdgeQL query to traverse chain links, returning the ordered sequence per vision Section 5.3
  - `GetRelatedDirectives` -- EdgeQL query traversing `related_to` links to specified depth
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

**Acceptance criteria:**

- Gel schema exists in `dbschema/default.esdl` matching vision Section 5.3
- GraphStore interface with Gel and Noop implementations
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

2. Add to `internal/inject/retrieve.go`:

   - New retrieval source: `retrieveFromGel(ctx, foundIDs) ([]candidate, error)`
   - Given directive IDs found by Meilisearch and CRDB, query Gel for:
     - Behavioral chains containing those directives (get the next step)
     - Related directives (1-hop traversal)

3. Update fan-out in `Pipeline.Inject`:

   - Phase 1: Parallel fan-out to Meilisearch + CRDB (existing)
   - Phase 2: After Phase 1 completes, fan-out to Gel with the found directive IDs
   - Gel results are added as candidates and participate in the standard ranking formula

4. Degradation: Gel is the lowest priority. If it is unavailable, pipeline returns results from Meilisearch + CRDB only.

**Tests:**

- `internal/inject/pipeline_test.go` (extend existing):
  - TestInject_WithGelChain -- directive in a chain returns the next step
  - TestInject_GelUnavailable_StillWorks -- pipeline degrades gracefully
  - TestInject_GelAddsRelatedDirectives -- related directives appear in results

**Acceptance criteria:**

- Injection pipeline queries Gel as a third source when available
- Chain traversal adds "next step" directives to candidates
- Related directives boost diversity of results
- Gel unavailability does not block the pipeline
- All previous injection tests still pass

---

## Phase 6: Decomposition Pipeline

**Goal:** Implement LLM-powered ingestion -- when a skill document is ingested, decompose it into atomic directives using an LLM. Per vision Section 3.

### Step 6.1: Decomposition Engine

- **Step ID:** 6.1
- **Dependencies:** 0.4, 1.1, 2.2
- **Scope:** L (3-5 days)

**What gets built:**

Note: The LLM client (Step 0.4) already exists. This step uses it for decomposition with a Sonnet-class model (same Sonnet-class model used for recomposition).

1. Create `internal/decompose/` package:
   - `internal/decompose/decompose.go` -- `Engine` struct:

```go
type Engine struct {
    llm   llm.Client
    model string // Sonnet-class model, configurable
    store model.DirectiveStore
    sync  *search.SyncManager
}

func NewEngine(llm llm.Client, model string, store model.DirectiveStore, sync *search.SyncManager) *Engine

func (e *Engine) Decompose(ctx context.Context, source *model.IngestionSource, content string) ([]*model.Directive, error)
```

- `internal/decompose/section.go` -- Deterministic sectioning per vision Section 3.2:

  - `SplitIntoSections(content string) []Section`
  - Splits on markdown headings (##, ###)
  - Splits on numbered lists
  - Each section retains its heading context and position in the document
  - No LLM needed for sectioning

- `internal/decompose/extract.go` -- LLM-based directive extraction per vision Section 3.3:

  - `ExtractDirectives(ctx, section Section) ([]RawDirective, error)`
  - Sends section content to LLM with the decomposition prompt
  - The prompt requests: content, kind (behavioral/pattern/contextual/corrective/factual), trigger_tags, trigger_intent, trigger_phase, token_cost
  - Parses JSON response into `[]RawDirective`

- `internal/decompose/enrich.go` -- Post-extraction enrichment per vision Section 3.5:

  - Token cost estimation via `len(content) / 4`
  - Cross-reference detection via Meilisearch similarity search
  - Scope assignment (global vs. repo-specific, inferred from source and content)
  - Weight assignment: anti-rationalization directives get higher initial weight per vision Section 3.5

- `internal/decompose/dedup.go` -- Deduplication per vision Section 3.6:
  - For each extracted directive, search Meilisearch for semantically similar existing directives
  - When duplicates are found: keep the most specific version, preserve provenance from both sources
  - A directive with multiple independent sources gets a weight bonus (multiple skills independently concluded this behavior matters)

2. Update `POST /api/v1/ingest` handler:

   - After creating the ingestion_source, check if LLM is available
   - If available, run decomposition asynchronously (fire-and-forget goroutine)
   - Decomposition status tracked on ingestion_source
   - The decomposition prompt is versioned (stored as a constant, referenced in the ingestion record)

3. The decomposition prompt (stored as a constant in `internal/decompose/prompt.go`):

   - Per vision Section 3.3: "You are a behavioral knowledge extractor..."
   - Requests JSON array of directives with fields matching the vision's schema
   - Prompt version tracked for reproducibility

4. Configuration via environment:
   - `HIVE_DECOMPOSE_MODEL` -- model for decomposition (Sonnet-class by default, configurable)
   - Uses the same `ANTHROPIC_API_KEY` as the recomposer

**Tests:**

- `internal/decompose/section_test.go`:
  - TestSplitIntoSections_MarkdownHeadings
  - TestSplitIntoSections_NumberedList
  - TestSplitIntoSections_EmptyDocument
- `internal/decompose/extract_test.go`:
  - TestExtractDirectives_ParsesJSON -- mock LLM returns valid JSON
  - TestExtractDirectives_InvalidJSON -- handles malformed response
  - TestExtractDirectives_CorrectKinds -- extracted directives use valid kind values
  - TestExtractDirectives_EmptySection -- returns empty
- `internal/decompose/enrich_test.go`:
  - TestTokenCostEstimation
  - TestWeightAssignment_AntiRationalizationHigher
  - TestScopeInference
- `internal/decompose/dedup_test.go`:
  - TestDedup_SimilarContent -- merges semantically similar, preserves provenance
  - TestDedup_MultiSourceBoost -- multiple independent sources increase weight
  - TestDedup_NoMatch -- creates new
- `internal/decompose/decompose_test.go` (integration):
  - TestDecompose_EndToEnd -- full pipeline from markdown to stored directives

**Acceptance criteria:**

- Skill documents can be ingested and decomposed into atomic directives
- Sectioning is deterministic (no LLM)
- LLM extraction produces valid directive structures with correct `kind` values (behavioral, pattern, contextual, corrective, factual)
- Deduplication prevents redundant directives from multiple skill sources
- Multi-source directives get weight bonus per vision Section 3.6
- Results are synced to Meilisearch after creation
- A typical skill document (2000 tokens) produces 15-30 directives
- All directives include tenant_id

---

## Phase 7: Hardening

**Goal:** Production readiness -- metrics, structured logging, and operational tooling.

### Step 7.1: Observability

- **Step ID:** 7.1
- **Dependencies:** 3.2, 4.1
- **Scope:** M (1-3 days)

**What gets built:**

1. Add structured logging throughout:

   - Replace `internal/log/log.go` with `log/slog` (standard library, Go 1.21+)
   - Add request logging middleware: method, path, status, duration, agent_id
   - Add injection pipeline timing logs: meilisearch_ms, crdb_ms, gel_ms, recompose_ms, total_ms, candidates, selected
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
     - `hive_recompose_fallback_total` -- counter for fallback events
     - `hive_recompose_tokens_input_total` -- counter
     - `hive_recompose_tokens_output_total` -- counter
     - `hive_directives_total{kind, active}` -- gauge
     - `hive_feedback_total{outcome}` -- counter
     - `hive_meilisearch_healthy` -- gauge (1 or 0)
     - `hive_gel_healthy` -- gauge (1 or 0)

3. Add audit logging for admin operations:
   - `POST /api/v1/ingest` -- log source_name, content_hash, agent_id
   - `PATCH /api/v1/directives/{id}` -- log directive_id, changes, agent_id
   - Audit log goes to structured logging output (not a separate table)

**Tests:**

- Metrics endpoint returns valid Prometheus format
- Structured log output contains expected fields

**Acceptance criteria:**

- All API requests are logged with structured fields
- Injection pipeline timing is logged and metriced
- Recomposition metrics (tokens, cost, latency, fallback rate) are tracked
- Prometheus metrics endpoint works
- Audit trail for admin operations exists in logs

---

### Step 7.2: Operational Tooling

- **Step ID:** 7.2
- **Dependencies:** 2.2, 5.1
- **Scope:** S (< 1 day)

**What gets built:**

1. Full re-sync command (CLI flag or admin trigger):

   - Triggers full CockroachDB -> Meilisearch re-sync
   - Can be run as `hive-server --sync-only`

2. `script/backup` shell script:

   - Triggers CockroachDB backup (documents the `BACKUP` SQL command)
   - Triggers Meilisearch dump
   - Intended for cron/scheduled execution

3. Data integrity check (CLI flag or admin trigger):
   - Compares directive counts between CockroachDB and Meilisearch, reports discrepancies
   - Can be run as `hive-server --check-integrity`

**Tests:**

- Integrity check detects when Meilisearch is missing directives

**Acceptance criteria:**

- Full re-sync can be triggered from CLI
- Backup script exists and documents the process
- Integrity check detects CockroachDB/Meilisearch drift

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

Phase 4: Feedback Loop
  4.1 (feedback + scoring)       -- depends on 3.1

Phase 5: Gel DB
  5.1 (gel schema + client)      -- depends on 1.1 (parallelizable with Phase 2-3)
    -> 5.2 (graph-enhanced inject) -- depends on 3.1, 5.1

Phase 6: Decomposition
  6.1 (decomposition engine)     -- depends on 0.4, 1.1, 2.2

Phase 7: Hardening
  7.1 (observability)            -- depends on 3.2, 4.1
  7.2 (operational tooling)      -- depends on 2.2, 5.1
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

## API Endpoint Summary

Per vision Section 8.1, the API has **9 endpoints**:

| #   | Endpoint                            | Method | Purpose                                                 | Phase                |
| --- | ----------------------------------- | ------ | ------------------------------------------------------- | -------------------- |
| 1   | `/api/v1/inject`                    | POST   | Get contextualized directives for the current situation | 3                    |
| 2   | `/api/v1/feedback`                  | POST   | Report outcomes for a previous injection                | 4                    |
| 3   | `/api/v1/feedback/session-complete` | POST   | Report session summary and key insights                 | 4                    |
| 4   | `/api/v1/ingest`                    | POST   | Submit a skill document or experience for decomposition | 1 (storage), 6 (LLM) |
| 5   | `/api/v1/directives`                | GET    | Browse/search the directive catalog (admin/debug)       | 1                    |
| 6   | `/api/v1/directives/{id}`           | GET    | Get a single directive with full metadata               | 1                    |
| 7   | `/api/v1/directives/{id}`           | PATCH  | Manually adjust a directive's weight or active status   | 1                    |
| 8   | `/health`                           | GET    | Health probe                                            | existing             |
| 9   | `/ready`                            | GET    | Readiness probe                                         | existing             |

Additional operational endpoints (metrics, sync, integrity) are implementation details added during Phase 7, not part of the core API design.

The existing memory CRUD (4 endpoints), task CRUD (5 endpoints), and agent heartbeat (3 endpoints) continue to function alongside the new endpoints.

---

## What This Plan Does NOT Include

Explicitly out of scope (deferred to future work):

1. **Three-layer caching.** Build plan v4 included response cache, session cache, and pre-synthesis cache as Phase 3.3. Caching is an optimization that should be added after the system works and real latency/cost data is available. When caching is needed, add it as a future phase.

2. **Meilisearch hybrid search / embeddings.** directive-schema.md defines an OpenAI embedder configuration. This plan defers it because it adds a second external API dependency (OpenAI) and hybrid search is not needed until the directive count is large enough for keyword search to be insufficient.

3. **The `hive` CLI tool.** The MCP plugin is the primary consumer. A CLI tool for manual directive management is useful but not blocking.

4. **LLM-friendly error messages.** Valuable but can be incrementally improved during any phase.

5. **`repo` column on existing memory/tasks tables.** Can be added as a standalone migration at any time.

6. **Rate limiting.** Rate limiting is an implementation detail. When abuse becomes a concern, add appropriate limits. No specific numbers are spec'd upfront.

---

## Vision Alignment Checklist

| Vision Requirement                                                                      | Build Plan v5 Status                                                    |
| --------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| 5 directive kinds (behavioral, pattern, contextual, corrective, factual)                | Aligned. `kind` TEXT field throughout.                                  |
| `weight` FLOAT8 0.0-2.0                                                                 | Aligned. Replaced `priority INT 1-100` everywhere.                      |
| `tenant_id` on all tables                                                               | Aligned. Included from Phase 1.                                         |
| Ranking: `(relevance * 0.4) + (effectiveness * 0.3) + (weight * 0.2) + (recency * 0.1)` | Aligned. Single formula in Step 3.1.                                    |
| Effectiveness: `(followed - negative) / max(injected, 1)`                               | Aligned. Denormalized counters + formula in Step 4.1.                   |
| 3 feedback outcomes: followed, ignored, negative                                        | Aligned. No extras.                                                     |
| 9 API endpoints                                                                         | Aligned. Listed in API Endpoint Summary.                                |
| Session tracking: agent_sessions, injections, injection_outcomes                        | Aligned. Created in Step 3.1.                                           |
| No fabricated latency constraints                                                       | Aligned. "Accuracy over speed" throughout.                              |
| Contextualization via LLM                                                               | Aligned. Step 3.2 with 10-15s timeout (intentional correction of 50ms). |
| Sonnet-class model for recomposition                                                    | Aligned. Configurable, not version-pinned.                              |
| Sonnet-class model for decomposition                                                    | Aligned. Configurable, not version-pinned.                              |
