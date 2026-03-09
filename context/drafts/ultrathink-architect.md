# Systems Architecture Analysis: hive-server

**Date:** 2026-03-09
**Perspective:** Systems Architect
**Scope:** Interfaces, data flow, schema evolution, deployment topology, failure modes, hard problems

---

## 1. Interface Design: The Store Problem

### 1.1 What Exists Today

The current `Store` is a concrete struct with methods, not an interface. Handlers define their own `Store` interface for decoupling:

```go
// internal/handlers/handlers.go (current)
type Store interface {
    UpsertMemory(ctx context.Context, entry *store.MemoryEntry) (*store.MemoryEntry, error)
    GetMemory(ctx context.Context, key string) (*store.MemoryEntry, error)
    ListMemory(ctx context.Context, f store.MemoryFilter) ([]*store.MemoryEntry, error)
    DeleteMemory(ctx context.Context, key string) error
    CreateTask(ctx context.Context, t *store.Task) (*store.Task, error)
    // ... 8 more methods
}
```

This is a single monolithic interface with 13 methods. It couples types to the `store` package (handlers import `store.MemoryEntry`, `store.TaskFilter`, etc.). Every new feature domain (events, sessions, GSD projects, Superpowers invocations, Allium specs) adds methods to this interface.

### 1.2 The Growth Problem

By Phase 2, the Store interface needs methods for:

- Memory CRUD (4 methods)
- Tasks CRUD (5 methods)
- Agents (3 methods)
- Events (2 methods)
- Sessions (3 methods)
- GSD projects/phases/requirements (~6 methods)
- Superpowers invocations/workflows (~4 methods)
- Allium specs/drift (~4 methods)
- Lifecycle (2 methods: Close, Ping)

That is ~33 methods. A single interface with 33 methods is a code smell. It violates interface segregation. Every backend (SQLite, CRDB) must implement all 33, even if a feature domain is irrelevant.

### 1.3 The Right Interface Design

Split into domain-specific interfaces. Compose them when needed.

```go
// internal/model/types.go -- ALL types live here, not in store

package model

type MemoryEntry struct {
    Key       string    `json:"key"`
    Value     string    `json:"value"`
    AgentID   string    `json:"agent_id"`
    Scope     string    `json:"scope"`     // "private", "project", "global"
    Repo      string    `json:"repo"`
    Tags      []string  `json:"tags"`
    Version   int64     `json:"version"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type Event struct {
    ID        string          `json:"id"`
    Type      string          `json:"event_type"`
    AgentID   string          `json:"agent_id"`
    SessionID string          `json:"session_id"`
    Repo      string          `json:"repo"`
    Payload   json.RawMessage `json:"payload"`
    CreatedAt time.Time       `json:"created_at"`
}

type Session struct {
    ID          string     `json:"id"`
    AgentID     string     `json:"agent_id"`
    Repo        string     `json:"repo"`
    Summary     string     `json:"summary"`
    StartedAt   time.Time  `json:"started_at"`
    CompletedAt *time.Time `json:"completed_at,omitempty"`
}
```

```go
// internal/store/interfaces.go

package store

import (
    "context"
    "github.com/christmas-island/hive-server/internal/model"
)

// MemoryStore handles memory entry persistence.
type MemoryStore interface {
    UpsertMemory(ctx context.Context, entry *model.MemoryEntry) (*model.MemoryEntry, error)
    GetMemory(ctx context.Context, key string) (*model.MemoryEntry, error)
    ListMemory(ctx context.Context, f model.MemoryFilter) ([]*model.MemoryEntry, error)
    DeleteMemory(ctx context.Context, key string) error
}

// TaskStore handles task persistence.
type TaskStore interface {
    CreateTask(ctx context.Context, t *model.Task) (*model.Task, error)
    GetTask(ctx context.Context, id string) (*model.Task, error)
    ListTasks(ctx context.Context, f model.TaskFilter) ([]*model.Task, error)
    UpdateTask(ctx context.Context, id string, upd model.TaskUpdate) (*model.Task, error)
    DeleteTask(ctx context.Context, id string) error
}

// AgentStore handles agent registration and heartbeats.
type AgentStore interface {
    Heartbeat(ctx context.Context, id string, caps []string, status model.AgentStatus) (*model.Agent, error)
    GetAgent(ctx context.Context, id string) (*model.Agent, error)
    ListAgents(ctx context.Context) ([]*model.Agent, error)
}

// EventStore handles the append-only event stream.
type EventStore interface {
    CreateEvent(ctx context.Context, event *model.Event) (*model.Event, error)
    ListEvents(ctx context.Context, f model.EventFilter) ([]*model.Event, error)
}

// SessionStore handles session summaries.
type SessionStore interface {
    CreateSession(ctx context.Context, s *model.Session) (*model.Session, error)
    GetSession(ctx context.Context, id string) (*model.Session, error)
    ListSessions(ctx context.Context, f model.SessionFilter) ([]*model.Session, error)
    CompleteSession(ctx context.Context, id string, summary string) (*model.Session, error)
}

// Store composes all domain stores plus lifecycle methods.
// This is what SQLiteStore and CRDBStore implement.
type Store interface {
    MemoryStore
    TaskStore
    AgentStore
    EventStore
    SessionStore
    Ping(ctx context.Context) error
    Close() error
}
```

**Why this matters for backend swaps:** When CRDB replaces SQLite, you implement the same `Store` interface. But if you only need to test memory handlers, you mock only `MemoryStore`. When Gel is added, it does NOT implement `Store`. It implements its own `GraphStore` interface with completely different methods. The handler layer accepts both, checks for nil on the optional one.

### 1.4 The Searcher Interface

```go
// internal/search/interfaces.go

package search

import "context"

// Searcher handles full-text search (secondary index, not source of truth).
type Searcher interface {
    Search(ctx context.Context, index string, req SearchRequest) (*SearchResponse, error)
    Index(ctx context.Context, index string, docs []Document) error
    Delete(ctx context.Context, index string, ids []string) error
    EnsureIndex(ctx context.Context, index string, settings IndexSettings) error
    Healthy(ctx context.Context) error
}

type SearchRequest struct {
    Query     string            `json:"q"`
    Filters   map[string]string `json:"filters,omitempty"`
    Sort      []string          `json:"sort,omitempty"`
    Limit     int               `json:"limit"`
    Offset    int               `json:"offset"`
    Facets    []string          `json:"facets,omitempty"`
}

type SearchResponse struct {
    Hits             []SearchHit `json:"hits"`
    TotalHits        int         `json:"total_hits"`
    ProcessingTimeMs int         `json:"processing_time_ms"`
}

type SearchHit struct {
    ID         string                 `json:"id"`
    Score      float64                `json:"score"`
    Fields     map[string]interface{} `json:"fields"`
    Highlights map[string]string      `json:"highlights,omitempty"`
}

type Document map[string]interface{}

type IndexSettings struct {
    Searchable  []string            `json:"searchable_attributes"`
    Filterable  []string            `json:"filterable_attributes"`
    Sortable    []string            `json:"sortable_attributes"`
    Synonyms    map[string][]string `json:"synonyms,omitempty"`
}
```

**Critical design decision:** `Document` is `map[string]interface{}`, not a typed struct. This is intentional. Meilisearch indexes heterogeneous data (memory entries, tasks, session summaries, GSD project metadata, Allium spec constructs). A typed struct would require a different Index method per type. The conversion from typed model to Document happens at the sync layer, not the search layer.

### 1.5 The Skill-Specific Store Interfaces

Skill-specific endpoints do NOT extend the core Store interface. They get their own interfaces with their own SQLite tables.

```go
// internal/store/gsd.go

// GSDStore handles GSD-specific persistence.
type GSDStore interface {
    CreateProject(ctx context.Context, p *model.GSDProject) (*model.GSDProject, error)
    GetProject(ctx context.Context, name string) (*model.GSDProject, error)
    ListProjects(ctx context.Context) ([]*model.GSDProject, error)
    UpdateProject(ctx context.Context, name string, upd model.GSDProjectUpdate) (*model.GSDProject, error)
    RecordPhaseTransition(ctx context.Context, project string, phase *model.GSDPhase) (*model.GSDPhase, error)
    CreateRequirement(ctx context.Context, req *model.GSDRequirement) (*model.GSDRequirement, error)
    ListRequirements(ctx context.Context, f model.GSDRequirementFilter) ([]*model.GSDRequirement, error)
}

// SuperpowersStore handles Superpowers-specific persistence.
type SuperpowersStore interface {
    RecordInvocation(ctx context.Context, inv *model.SPInvocation) (*model.SPInvocation, error)
    ListInvocations(ctx context.Context, f model.SPInvocationFilter) ([]*model.SPInvocation, error)
    GetSkillEffectiveness(ctx context.Context, skill string) (*model.SPEffectiveness, error)
}

// AlliumStore handles Allium-specific persistence.
type AlliumStore interface {
    SyncSpec(ctx context.Context, spec *model.ALSpec) error
    GetSpec(ctx context.Context, name string) (*model.ALSpec, error)
    ListSpecs(ctx context.Context) ([]*model.ALSpec, error)
    RecordDrift(ctx context.Context, specName string, drift *model.ALDriftReport) (*model.ALDriftReport, error)
    GetDriftHistory(ctx context.Context, specName string) ([]*model.ALDriftReport, error)
}
```

The SQLiteStore can implement all of these on the same struct (same `*sql.DB`), but they are separate interfaces. This means:

1. A handler that only needs GSD accepts `GSDStore`, not the full `Store`.
2. Tests mock only the skill-specific interface.
3. If Allium is never deployed, its tables are never created and its interface is never wired.

### 1.6 Dependency Wiring

The API struct grows, but in a structured way:

```go
// internal/handlers/handlers.go

type API struct {
    store       store.Store           // required
    searcher    search.Searcher       // optional (NoopSearcher if nil/absent)
    gsd         store.GSDStore        // optional (nil if GSD not enabled)
    superpowers store.SuperpowersStore // optional (nil if SP not enabled)
    allium      store.AlliumStore      // optional (nil if Allium not enabled)
    token       string
}

func New(cfg Config) http.Handler {
    a := &API{
        store:       cfg.Store,
        searcher:    cfg.Searcher,
        gsd:         cfg.GSDStore,
        superpowers: cfg.SuperpowersStore,
        allium:      cfg.AlliumStore,
        token:       cfg.Token,
    }
    // ...
}

type Config struct {
    Store           store.Store
    Searcher        search.Searcher
    GSDStore        store.GSDStore        // nil = GSD endpoints return 404
    SuperpowersStore store.SuperpowersStore // nil = SP endpoints return 404
    AlliumStore     store.AlliumStore       // nil = Allium endpoints return 404
    Token           string
}
```

Handlers for optional features check for nil and return 404 or 501:

```go
func (a *API) handleGSDListProjects(ctx context.Context, input *struct{}) (*GSDProjectsOutput, error) {
    if a.gsd == nil {
        return nil, huma.Error404NotFound("GSD module not enabled")
    }
    projects, err := a.gsd.ListProjects(ctx)
    // ...
}
```

---

## 2. Data Flow Analysis

### 2.1 Write Path: Agent Creates a Memory Entry

```
Agent (Claude Code / hive-local)
  |
  | POST /api/v1/memory
  | Headers: Authorization: Bearer <token>, X-Agent-ID: agent-42
  | Body: { "key": "debug/race-cond-fix", "value": "...", "tags": ["go","concurrency"] }
  |
  v
chi Router
  |
  v
authMiddleware
  | - Validates Bearer token
  | - Extracts X-Agent-ID -> context
  |
  v
Huma Operation: handleMemoryUpsert
  | - Validates input (Huma schema validation)
  | - Extracts agent_id from context
  | - Sets entry.AgentID = agent_id
  |
  v
store.UpsertMemory(ctx, entry)                    [SQLite write, ~1-5ms]
  | - BEGIN TRANSACTION
  | - SELECT existing by key (optimistic concurrency check)
  | - INSERT or UPDATE
  | - COMMIT
  |
  v (async, fire-and-forget)
syncService.IndexMemory(ctx, result)               [Meilisearch index, ~10-50ms]
  | - Converts MemoryEntry -> search.Document
  | - searcher.Index(ctx, "memories", []Document{doc})
  | - Meilisearch enqueues task (async internally)
  | - Log error on failure, do not propagate
  |
  v
HTTP 200 + serialized MemoryEntry                  [Total: ~5-15ms]
```

**Key observations:**

- The write path is synchronous to SQLite only. Meilisearch indexing is fire-and-forget.
- If Meilisearch is down, the write succeeds. The periodic reconciliation job will catch up.
- The async goroutine needs a timeout-bounded context (not the request context, which is cancelled on response).

### 2.2 Read Path: Skill Queries for Context

```
GSD Orchestrator
  |
  | POST /api/v1/search/memories
  | Body: { "q": "race condition debugging", "limit": 10 }
  |
  v
authMiddleware -> Huma Operation: handleSearchMemories
  |
  v
searcher.Search(ctx, "memories", req)              [Meilisearch query, ~5-20ms]
  | - Translates SearchRequest to Meilisearch query
  | - Applies agent_id filter if scoped
  | - Returns ranked hits with relevance scores
  |
  v
HTTP 200 + SearchResponse                          [Total: ~10-30ms]
```

**Fallback path when Meilisearch is down:**

```
searcher.Search(ctx, "memories", req)
  |
  v (Meilisearch unreachable)
  |
  v
HTTP 503 { "error": "search service unavailable" }
```

The 503 is correct for Phase 1. In Phase 5, the memory injection endpoint would fall back to SQLite LIKE queries with degraded relevance.

### 2.3 Memory Injection Path (Phase 1, Critical Latency Path)

This is the most latency-sensitive flow. It runs on EVERY agent prompt via a pre-prompt hook.

```
Agent receives user message
  |
  v
hive-local (pre-prompt hook, :18820)
  |
  | POST /api/v1/memory/inject
  | Body: { "agent_id": "agent-42", "prompt_text": "fix the race condition in store.go",
  |          "session_id": "sess-abc", "repo": "hive-server", "max_tokens": 1500 }
  |
  v
hive-server: handleMemoryInject
  |
  +---> Extract keywords from prompt_text               [~1ms, in-process]
  |       "fix", "race", "condition", "store.go"
  |       (stopword removal + deduplication)
  |
  +---> Parallel queries:
  |     |
  |     +---> searcher.Search("memories", keywords)     [~10ms]
  |     |     Returns: prior debugging notes, solutions
  |     |
  |     +---> searcher.Search("sessions", keywords)     [~10ms]
  |     |     Returns: relevant session summaries
  |     |
  |     +---> store.ListTasks(agent_id, status=active)  [~2ms]
  |     |     Returns: current task assignments
  |     |
  |     +---> store.ListEvents(session_id, limit=10)    [~2ms]
  |           Returns: recent events in this session
  |
  v
Merge + Rank results                                    [~1ms]
  | - Sort by Meilisearch score * recency_weight
  | - Deduplicate
  | - Trim to max_tokens budget
  |
  v
Response: {
    context_blocks: [...],
    tokens_used: 847
}                                                       [Total target: <200ms]
```

**Latency budget breakdown:**

| Component                           | Target    | Notes                         |
| ----------------------------------- | --------- | ----------------------------- |
| Network (hive-local -> hive-server) | <5ms      | localhost, same machine       |
| Keyword extraction                  | <2ms      | Simple string ops, no LLM     |
| Meilisearch queries (parallel)      | <20ms     | Two queries, run concurrently |
| SQLite queries (parallel)           | <5ms      | Two queries, run concurrently |
| Merge + rank + serialize            | <3ms      | In-memory operations          |
| **Total**                           | **<35ms** | Well under 200ms budget       |

**The latency danger:** If hive-local introduces caching with a 30s TTL (as suggested), and the cache is cold, the first prompt of a session pays the full latency cost. With warm cache, it is zero. The question is whether 35ms (likely actual) justifies caching infrastructure. Probably not until prompt volume increases.

### 2.4 Event Recording Path (Cross-Skill)

```
Any skill (GSD, Superpowers, Allium)
  |
  | POST /api/v1/events
  | Body: {
  |   "event_type": "task.completed",
  |   "agent_id": "executor-1",
  |   "session_id": "sess-abc",
  |   "repo": "hive-server",
  |   "payload": { "task_id": "...", "duration_ms": 4500, "outcome": "success" }
  | }
  |
  v
store.CreateEvent(ctx, event)                      [SQLite append, ~1ms]
  | - INSERT INTO events (auto-generated ID, server-set timestamp)
  |
  v (async)
searcher.Index("events", doc)                      [Optional indexing]
  |
  v
HTTP 201 + Event
```

Events are append-only. No updates, no deletes (in the hot path). This is the simplest store operation and the one that scales most predictably.

---

## 3. Schema Evolution Strategy

### 3.1 Current Schema (Phase 0)

Four tables: `memory`, `tasks`, `task_notes`, `agents`.

### 3.2 Phase 0 Additions

```sql
-- Events: append-only, cross-skill event stream
CREATE TABLE IF NOT EXISTS events (
    id         TEXT    NOT NULL PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    event_type TEXT    NOT NULL,
    agent_id   TEXT    NOT NULL DEFAULT '',
    session_id TEXT    NOT NULL DEFAULT '',
    repo       TEXT    NOT NULL DEFAULT '',
    payload    TEXT    NOT NULL DEFAULT '{}',
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_events_type    ON events(event_type);
CREATE INDEX IF NOT EXISTS idx_events_agent   ON events(agent_id);
CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_created ON events(created_at);

-- Sessions: agent session summaries
CREATE TABLE IF NOT EXISTS sessions (
    id           TEXT NOT NULL PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    agent_id     TEXT NOT NULL DEFAULT '',
    repo         TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    started_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    completed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_sessions_agent ON sessions(agent_id);
CREATE INDEX IF NOT EXISTS idx_sessions_repo  ON sessions(repo);
```

### 3.3 Phase 2: Skill-Specific Tables

**Principle: Each skill gets prefixed tables. No shared tables beyond the core.**

```sql
-- GSD tables
CREATE TABLE IF NOT EXISTS gsd_projects (
    name        TEXT NOT NULL PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    repo        TEXT NOT NULL DEFAULT '',
    phase       TEXT NOT NULL DEFAULT 'research',  -- current phase
    status      TEXT NOT NULL DEFAULT 'active',
    metadata    TEXT NOT NULL DEFAULT '{}',         -- JSON: milestones, config
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS gsd_phases (
    id         TEXT NOT NULL PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    project    TEXT NOT NULL REFERENCES gsd_projects(name) ON DELETE CASCADE,
    phase      TEXT NOT NULL,     -- 'research', 'planning', 'execution', 'verification'
    status     TEXT NOT NULL,     -- 'started', 'completed', 'failed'
    started_at TEXT NOT NULL,
    ended_at   TEXT,
    metadata   TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS gsd_requirements (
    id         TEXT NOT NULL PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    project    TEXT NOT NULL REFERENCES gsd_projects(name) ON DELETE CASCADE,
    category   TEXT NOT NULL DEFAULT 'functional',
    title      TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'pending',
    priority   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Superpowers tables
CREATE TABLE IF NOT EXISTS sp_invocations (
    id         TEXT    NOT NULL PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    skill      TEXT    NOT NULL,
    agent_id   TEXT    NOT NULL DEFAULT '',
    session_id TEXT    NOT NULL DEFAULT '',
    repo       TEXT    NOT NULL DEFAULT '',
    success    INTEGER NOT NULL DEFAULT 0,    -- boolean: 0 or 1
    duration_ms INTEGER NOT NULL DEFAULT 0,
    error_msg  TEXT    NOT NULL DEFAULT '',
    metadata   TEXT    NOT NULL DEFAULT '{}',
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_sp_inv_skill ON sp_invocations(skill);
CREATE INDEX IF NOT EXISTS idx_sp_inv_agent ON sp_invocations(agent_id);

-- Allium tables
CREATE TABLE IF NOT EXISTS al_specs (
    name       TEXT NOT NULL PRIMARY KEY,
    project    TEXT NOT NULL DEFAULT '',
    version    TEXT NOT NULL DEFAULT '',
    ast_json   TEXT NOT NULL DEFAULT '{}',    -- full Allium AST
    metadata   TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS al_drift_reports (
    id         TEXT NOT NULL PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    spec_name  TEXT NOT NULL REFERENCES al_specs(name) ON DELETE CASCADE,
    drift_type TEXT NOT NULL,
    severity   TEXT NOT NULL DEFAULT 'low',
    details    TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
```

### 3.4 When to Use JSONB vs Separate Tables

| Use JSON column                                         | Use separate table                                                   |
| ------------------------------------------------------- | -------------------------------------------------------------------- |
| Schema varies per record (Allium AST, event payloads)   | Schema is fixed and queryable (task status, requirement category)    |
| Data is opaque to the database (skill metadata, config) | Data needs indexes (phase transitions by date, invocations by skill) |
| Read as a blob, never filtered on individual fields     | Filtered, sorted, aggregated by the database                         |
| Low cardinality, no joins needed                        | Referenced by foreign keys                                           |

**Concrete example:** GSD project metadata (milestones, config) is JSON because the schema is GSD-internal and varies. But `gsd_phases` is a separate table because "list all phases for a project ordered by start date" is a real query that benefits from indexing.

### 3.5 Migration Strategy

The current codebase uses inline schema (`CREATE TABLE IF NOT EXISTS`). This is fine for the current scope. For Phase 2+, use numbered migration files.

```go
// internal/store/migrations.go

//go:embed migrations/*.sql
var migrationFS embed.FS

func (s *SQLiteStore) migrate(ctx context.Context) error {
    // Read all *.sql files in order
    // Execute each within a transaction
    // Track applied migrations in a schema_version table
}
```

Migration files:

```
internal/store/migrations/
    001_initial_schema.sql          # current schema
    002_add_events_sessions.sql     # Phase 0
    003_add_memory_scope_repo.sql   # Phase 0 (add columns to memory)
    004_add_gsd_tables.sql          # Phase 2
    005_add_sp_tables.sql           # Phase 2
    006_add_al_tables.sql           # Phase 2
```

**Do NOT use goose or another migration framework yet.** The inline approach works. A migration framework adds a dependency for a problem (complex migration graphs, rollbacks) that does not exist at this scale. When CRDB is added, goose becomes justified because CRDB has different DDL syntax.

---

## 4. Deployment Topology

### 4.1 Local Development (Phase 0-2)

```
+-------------------------------------------+
|  Developer Machine                        |
|                                           |
|  +-----------+      +-----------+         |
|  | hive-     |      | hive-     |         |
|  | server    |<---->| local     |         |
|  | :8080     |      | :18820    |         |
|  +-----+-----+      +-----+-----+        |
|        |                   |              |
|        v                   v              |
|  +-----+-----+      +-----+-----+        |
|  | SQLite    |      | Claude    |         |
|  | hive.db   |      | Code      |         |
|  +-----------+      +-----------+         |
|                                           |
|  +-----------+                            |
|  | Meili-    |  (optional, Phase 1+)      |
|  | search    |                            |
|  | :7700     |                            |
|  +-----------+                            |
+-------------------------------------------+
```

**Startup command (Phase 0):**

```bash
HIVE_DB_PATH=./hive.db HIVE_TOKEN="" ./hive-server serve
```

**Startup command (Phase 1+):**

```bash
# Terminal 1: Meilisearch
meilisearch --env development --db-path ./meili-data

# Terminal 2: hive-server
HIVE_DB_PATH=./hive.db MEILI_URL=http://localhost:7700 ./hive-server serve
```

**Docker Compose (for convenience):**

```yaml
# docker-compose.yml
services:
  hive-server:
    build: .
    ports:
      - "8080:8080"
    environment:
      HIVE_DB_PATH: /data/hive.db
      HIVE_TOKEN: ""
      MEILI_URL: http://meilisearch:7700
    volumes:
      - hive-data:/data
    depends_on:
      meilisearch:
        condition: service_healthy

  meilisearch:
    image: getmeili/meilisearch:v1.12
    ports:
      - "7700:7700"
    environment:
      MEILI_ENV: development
      MEILI_NO_ANALYTICS: "true"
    volumes:
      - meili-data:/meili_data
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:7700/health"]
      interval: 5s
      timeout: 3s
      retries: 3

volumes:
  hive-data:
  meili-data:
```

### 4.2 Production (Single Node, Phases 0-2)

```
+-------------------------------------------+
|  VPS / Droplet                            |
|                                           |
|  +-----------+      +-----------+         |
|  | hive-     |      | Meili-    |         |
|  | server    |----->| search    |         |
|  | :8080     |      | :7700     |         |
|  +-----+-----+      +-----------+         |
|        |                                  |
|  +-----+-----+                            |
|  | SQLite    |                            |
|  | /data/    |                            |
|  | hive.db   |                            |
|  +-----------+                            |
|                                           |
|  Reverse proxy: Caddy or nginx            |
|  TLS termination + basic rate limiting    |
+-------------------------------------------+
```

**Resource requirements:**

| Component   | RAM                             | CPU                | Disk            |
| ----------- | ------------------------------- | ------------------ | --------------- |
| hive-server | ~30 MB idle, ~100 MB under load | Minimal            | <1 MB binary    |
| Meilisearch | ~50 MB idle, ~200 MB indexing   | Burst during index | ~100 MB data    |
| SQLite      | Embedded (part of hive-server)  | N/A                | <100 MB typical |
| **Total**   | **~200 MB**                     | **0.5 vCPU**       | **<500 MB**     |

A $5/month VPS handles this with room to spare.

### 4.3 Production (K8s, Phase 4+)

```
+-------------------------------------------------------+
|  Kubernetes Cluster                                    |
|                                                        |
|  +-----------+     +-----------+     +-----------+     |
|  | hive-     |     | hive-     |     | hive-     |     |
|  | server    |     | server    |     | server    |     |
|  | replica 1 |     | replica 2 |     | replica 3 |     |
|  +-----+-----+     +-----+-----+     +-----+-----+    |
|        |                 |                 |           |
|        +--------+--------+--------+--------+          |
|                 |                 |                    |
|           +-----+-----+    +-----+-----+              |
|           | CRDB       |    | Meili-    |              |
|           | cluster    |    | search    |              |
|           | (3 nodes)  |    +-----------+              |
|           +-----------+                                |
|                                                        |
|  Optional:                                             |
|           +-----------+                                |
|           | Gel DB    |                                |
|           | + PG      |                                |
|           +-----------+                                |
+-------------------------------------------------------+
```

This is Phase 4 territory. Do not build this until SQLite's single-writer is a measured bottleneck.

### 4.4 Port Allocation

| Service     | Port  | Configurable Via            |
| ----------- | ----- | --------------------------- |
| hive-server | 8080  | `PORT` env or `--bind` flag |
| hive-local  | 18820 | (defined by hive-local)     |
| Meilisearch | 7700  | `MEILI_URL` env             |
| Gel DB      | 5656  | `GEL_DSN` env (future)      |
| CockroachDB | 26257 | `DATABASE_URL` env (future) |

---

## 5. Failure Mode Analysis

### 5.1 Meilisearch Down

**Scenario:** Meilisearch process crashes or is unreachable.

| Affected Operation | Behavior                                                                                                   | User-Visible Impact                                                     |
| ------------------ | ---------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| Memory/task writes | Succeed (SQLite write completes). Async index call fails silently (logged).                                | None. Data is persisted.                                                |
| Search endpoints   | Return 503 `{"error": "search service unavailable"}`                                                       | Agents cannot search. Must fall back to listing endpoints with filters. |
| Memory injection   | Partial results (SQLite queries succeed, Meilisearch queries fail). Return available context with warning. | Injected context is less relevant (no fuzzy search, no ranked results). |
| Reconciliation job | Logs error, retries on next tick.                                                                          | No impact until Meilisearch recovers. Then re-indexes everything.       |

**Recovery:** When Meilisearch comes back, the reconciliation job (every 5 minutes) re-indexes all data. No manual intervention needed. Data consistency is guaranteed because SQLite is the source of truth.

**Code pattern:**

```go
func (s *SyncStore) UpsertMemory(ctx context.Context, entry *model.MemoryEntry) (*model.MemoryEntry, error) {
    result, err := s.Store.UpsertMemory(ctx, entry)
    if err != nil {
        return nil, err // SQLite error = fail the request
    }
    // Fire-and-forget indexing with independent context + timeout
    go func() {
        indexCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        if err := s.sync.IndexMemory(indexCtx, result); err != nil {
            s.logger.Warn("search index failed", "key", result.Key, "error", err)
        }
    }()
    return result, nil
}
```

### 5.2 SQLite Write Contention

**Scenario:** Multiple goroutines attempting concurrent writes.

The current configuration uses `MaxOpenConns(1)`, which means all writes are serialized. Under high concurrency:

| Concurrency Level | Observed Behavior                                                   |
| ----------------- | ------------------------------------------------------------------- |
| 1-3 agents        | No contention. Writes serialize in <1ms.                            |
| 5-10 agents       | Writes queue. P99 latency rises to 10-50ms.                         |
| 20+ agents        | Write queue grows. Risk of context deadline exceeded (30s default). |

**Mitigation (current architecture):**

- WAL mode allows concurrent reads while one write is in progress.
- The 30-second request timeout is generous for SQLite operations.
- Event inserts are ~0.1ms each. Even 100/second serializes fine.

**When this actually breaks:** When you have 20+ agents all writing memory entries simultaneously with optimistic concurrency retries. Each retry re-reads and re-writes, doubling the lock pressure. This is the trigger for CRDB migration.

**Detection:** Add a histogram metric for SQLite write latency. Alert when P99 exceeds 100ms.

### 5.3 Malformed Data from Skills

**Scenario:** A skill sends invalid JSON, wrong types, or missing required fields.

**Defense layers:**

1. **Huma schema validation (first line):** Huma validates request bodies against the OpenAPI schema before the handler runs. Missing required fields, wrong types, out-of-range values are all caught here with a 422 response.

2. **Handler validation (second line):** Business logic validation (e.g., valid event_type values, valid phase names) happens in the handler. Returns 400 with descriptive error.

3. **Store validation (third line):** SQLite constraints (NOT NULL, FOREIGN KEY, CHECK) catch anything that slips through. Returns 500 (should not happen if layers 1-2 are correct).

```go
// Example: Huma catches type errors before handler runs
type CreateEventInput struct {
    Body struct {
        EventType string          `json:"event_type" required:"true" minLength:"1" doc:"Event type identifier"`
        AgentID   string          `json:"agent_id" doc:"Agent that generated the event"`
        SessionID string          `json:"session_id" doc:"Session context"`
        Repo      string          `json:"repo" doc:"Repository context"`
        Payload   json.RawMessage `json:"payload" doc:"Event-specific data"`
    }
}
```

**The real danger:** `payload` is `json.RawMessage` (arbitrary JSON). A skill could send a 10 MB payload. Defense: add a max body size middleware.

```go
r.Use(func(next http.Handler) http.Handler {
    return http.MaxBytesHandler(next, 1<<20) // 1 MB max request body
})
```

### 5.4 Stale Search Index

**Scenario:** Meilisearch index diverges from SQLite due to missed async index calls.

This is the most likely failure mode in steady state. The async fire-and-forget pattern means any of these can cause divergence:

- hive-server crashes between SQLite commit and Meilisearch index call
- Meilisearch rejects the document (malformed)
- Network timeout on the index call

**Mitigation:** The reconciliation job. Every 5 minutes, it re-indexes everything from SQLite. Maximum divergence window: 5 minutes.

**Optimization for later:** Track a `search_indexed_at` column on each record. The reconciliation job only re-indexes records where `updated_at > search_indexed_at`. This reduces the reconciliation window without re-indexing the entire dataset.

### 5.5 Concurrent Session Summary Writes

**Scenario:** An agent completes a session and calls `CompleteSession`. Meanwhile, the same agent starts a new session and calls `CreateSession`. If these race, the agent could have two "active" sessions.

**Mitigation:** Sessions are identified by ID (generated server-side). The `CompleteSession` call targets a specific session ID. There is no "current session" concept in the database. The client (hive-local) is responsible for tracking which session ID is active.

---

## 6. The Actually Hard Problems

### 6.1 Keyword Extraction Without an LLM

The memory injection endpoint needs to extract meaningful search terms from the agent's prompt. The vision says "keyword extraction" but does not specify how.

**Option A: Stopword removal + splitting.** Remove common English stopwords, split on whitespace and punctuation, take the top N terms by length (longer words are more specific).

```go
func extractKeywords(text string, maxTerms int) []string {
    words := tokenize(text)                    // split on whitespace/punctuation
    words = removeStopwords(words)             // remove "the", "is", "a", etc.
    words = dedup(words)                       // remove duplicates
    sort.Slice(words, func(i, j int) bool {
        return len(words[i]) > len(words[j])   // longer words first
    })
    if len(words) > maxTerms {
        words = words[:maxTerms]
    }
    return words
}
```

**Problem:** This is dumb. "fix the race condition in store.go" produces ["condition", "store.go", "race", "fix"]. Meilisearch gets "condition store.go race fix" as a query. Meilisearch's 10-word limit means we want to send fewer, better words.

**Option B: TF-IDF against the existing corpus.** Maintain a term frequency table in SQLite. Weight extraction by inverse document frequency. This produces genuinely better keywords but requires maintaining the frequency table.

**Option C: Just send the raw prompt (truncated to 10 words).** Let Meilisearch handle relevance ranking. This is the simplest approach and might be good enough.

**Recommendation:** Start with Option C. Meilisearch is designed to handle natural language queries. Its built-in relevance ranking (combining word proximity, typo tolerance, and position weighting) will outperform naive keyword extraction. Truncate to 10 words after stopword removal. Measure quality. Add TF-IDF only if search results are demonstrably poor.

### 6.2 Token Budget Management in Memory Injection

The injection endpoint accepts `max_tokens` and must return context blocks that fit within that budget. This requires counting tokens.

**Problem:** Token counting depends on the model's tokenizer. GPT-4 uses cl100k_base. Claude uses its own tokenizer. A rough estimate (4 characters per token) is wrong enough to either waste budget or overflow it.

**Options:**

1. **Use tiktoken-go** for approximate counting. It implements cl100k_base, which is close enough for Claude.
2. **Use character count / 4** as a rough estimate. Simple, fast, sometimes wrong.
3. **Accept a `max_characters` budget instead of `max_tokens`.** Let the client (hive-local) do the token conversion since it knows the model.

**Recommendation:** Option 3. hive-server should not know or care about tokenizers. The API accepts `max_chars` (or `max_bytes`). hive-local converts from its model's token budget to characters before calling.

```go
type InjectRequest struct {
    AgentID    string `json:"agent_id"`
    PromptText string `json:"prompt_text"`
    SessionID  string `json:"session_id"`
    Repo       string `json:"repo"`
    MaxChars   int    `json:"max_chars"` // NOT max_tokens
}
```

### 6.3 The SyncStore Goroutine Leak

The `SyncStore` pattern fires goroutines on every write:

```go
go func() {
    indexCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    s.sync.IndexMemory(indexCtx, result)
}()
```

Under high write throughput, this creates unbounded goroutines. If Meilisearch is slow or down, goroutines pile up (each waiting for 5-second timeout).

**Solution:** Use a buffered channel as a work queue with a fixed worker pool.

```go
type SyncStore struct {
    store.Store
    sync    *SyncService
    workCh  chan syncWork
    wg      sync.WaitGroup
}

type syncWork struct {
    fn func(ctx context.Context)
}

func NewSyncStore(s store.Store, sync *SyncService, workers int) *SyncStore {
    ss := &SyncStore{
        Store:  s,
        sync:   sync,
        workCh: make(chan syncWork, 1000), // buffer 1000 pending index ops
    }
    for i := 0; i < workers; i++ {
        ss.wg.Add(1)
        go ss.worker()
    }
    return ss
}

func (ss *SyncStore) worker() {
    defer ss.wg.Done()
    for work := range ss.workCh {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        work.fn(ctx)
        cancel()
    }
}

func (ss *SyncStore) Close() error {
    close(ss.workCh)
    ss.wg.Wait()
    return ss.Store.Close()
}
```

Start with 4 workers. If the channel is full (1000 pending), drop the index operation and log a warning. The reconciliation job catches up.

### 6.4 Schema Migration Without Downtime

Today, `migrate()` runs inline at startup:

```go
func (s *Store) migrate(ctx context.Context) error {
    _, err := s.db.ExecContext(ctx, schema)
    return err
}
```

`CREATE TABLE IF NOT EXISTS` is idempotent, so this works for adding tables. But what about adding a column?

```sql
ALTER TABLE memory ADD COLUMN scope TEXT NOT NULL DEFAULT 'global';
ALTER TABLE memory ADD COLUMN repo TEXT NOT NULL DEFAULT '';
```

SQLite `ALTER TABLE ADD COLUMN` is safe and fast (it does not rewrite the table). But:

1. The code must handle rows that have the column vs rows that do not (if reads happen between startup instances with different schemas).
2. `ALTER TABLE ... ADD COLUMN` fails if the column already exists.

**Solution:** Wrap column additions in a check:

```go
func (s *SQLiteStore) addColumnIfNotExists(ctx context.Context, table, column, colDef string) error {
    // Query pragma table_info to check if column exists
    rows, err := s.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
    if err != nil {
        return err
    }
    defer rows.Close()
    for rows.Next() {
        var cid int
        var name, typ string
        var notnull int
        var dflt *string
        var pk int
        if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
            return err
        }
        if name == column {
            return nil // column already exists
        }
    }
    _, err = s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, colDef))
    return err
}
```

Or simpler: catch the "duplicate column" error and ignore it.

### 6.5 Memory Entry Scope and Multi-Tenancy

The current memory model has no concept of scope. Every memory entry is globally visible to every agent. The vision documents mention scope ("private", "project", "global") but the current schema does not support it.

Adding `scope` and `repo` columns to the memory table changes the semantics of every existing query. Every `ListMemory` call must now filter by scope rules:

- `private`: only the creating agent can see it
- `project`: any agent working on the same repo can see it
- `global`: everyone can see it

```go
func (s *SQLiteStore) ListMemory(ctx context.Context, f model.MemoryFilter) ([]*model.MemoryEntry, error) {
    q := `SELECT ... FROM memory WHERE 1=1`
    args := []any{}

    // Scope filtering
    if f.AgentID != "" {
        // Show: global entries + project entries for this repo + private entries for this agent
        q += ` AND (scope = 'global' OR (scope = 'project' AND repo = ?) OR (scope = 'private' AND agent_id = ?))`
        args = append(args, f.Repo, f.AgentID)
    }
    // ... rest of filters
}
```

This is a breaking change to the API contract. Existing clients that do not send `scope` get `global` (backward compatible). But the query logic is now more complex and every handler must ensure `AgentID` and `Repo` are passed through.

**Recommendation:** Add the columns with defaults now (Phase 0, migration 003). Do not enforce scope filtering yet. Enforce it in Phase 1 when the injection endpoint needs it. This separates the schema change from the behavior change.

### 6.6 The Reconciliation Full-Scan Problem

The reconciliation job does a full scan of SQLite and re-indexes everything into Meilisearch:

```go
func (r *Reconciler) reconcile(ctx context.Context) {
    memories, _ := r.store.ListMemory(ctx, model.MemoryFilter{Limit: 10000})
    for _, m := range memories {
        r.sync.IndexMemory(ctx, m)
    }
    tasks, _ := r.store.ListTasks(ctx, model.TaskFilter{Limit: 10000})
    for _, t := range tasks {
        r.sync.IndexTask(ctx, t)
    }
}
```

At 10,000 memories, this fetches and re-indexes all 10,000 every 5 minutes. That is:

- SQLite: one query returning 10,000 rows (~50ms)
- Meilisearch: 10,000 document upserts (~2-5 seconds)

This is fine at the current scale. At 100,000 records, it takes ~30 seconds and creates unnecessary Meilisearch write pressure.

**Phase 1 solution (good enough):** Full scan with Limit=10000. If you have more than 10,000 records, paginate.

**Phase 2 optimization:** Add a `search_synced_at` column. The reconciler queries `WHERE updated_at > search_synced_at` and only re-indexes changed records. After successful indexing, update `search_synced_at`.

---

## 7. Concrete Implementation Sequence

Based on the above analysis, here is the actual implementation order with specific interface changes:

### Step 1: Extract model package (prerequisite for everything)

Move all types out of `internal/store/` into `internal/model/`. This is the foundation. Every subsequent change depends on handlers not importing store types directly.

Files:

- `internal/model/memory.go` - MemoryEntry, MemoryFilter
- `internal/model/task.go` - Task, TaskNote, TaskFilter, TaskUpdate, TaskStatus
- `internal/model/agent.go` - Agent, AgentStatus
- `internal/model/event.go` - Event, EventFilter (new)
- `internal/model/session.go` - Session, SessionFilter (new)
- `internal/model/errors.go` - ErrNotFound, ErrConflict, ErrInvalidTransition

### Step 2: Define Store interface (current code has none)

Create `internal/store/interfaces.go` with the split interfaces shown in section 1.3. Add compile-time check:

```go
var _ Store = (*SQLiteStore)(nil)
```

Rename `Store` struct to `SQLiteStore`. Keep constructor as `New()` returning `Store` interface.

### Step 3: Add events and sessions

Add tables (migration 002), add types to model package, add `EventStore` and `SessionStore` to the Store interface, implement on `SQLiteStore`.

### Step 4: Add Searcher interface with NoopSearcher

Create `internal/search/` package. Define interface. Implement NoopSearcher. Wire into API struct.

### Step 5: Huma v2 migration (already partly done based on go.mod)

Looking at the current `go.mod`, Huma v2 is already a dependency and the handlers already use it. This step is complete.

### Step 6: Meilisearch implementation

Implement `MeiliSearcher`. Create `SyncStore` wrapper with worker pool. Add search endpoints. Add reconciliation job.

### Step 7: Memory injection endpoint

Implement keyword extraction (Option C: truncated prompt). Parallel queries to Meilisearch + SQLite. Token budget management (character-based). Return ranked context blocks.

### Step 8: Skill-specific stores and endpoints

Add GSD, Superpowers, Allium stores and handlers. Each is independent -- can be built in parallel.

---

## 8. Interface Stability Guarantees

The following interfaces, once implemented, should NOT change signatures when backends are swapped:

| Interface                | Stable After | Backend Swap                                             |
| ------------------------ | ------------ | -------------------------------------------------------- |
| `store.Store`            | Phase 0      | SQLite -> CRDB: same interface, different implementation |
| `search.Searcher`        | Phase 1      | NoopSearcher -> MeiliSearcher: same interface            |
| `store.GSDStore`         | Phase 2      | SQLite -> CRDB: same interface                           |
| `store.SuperpowersStore` | Phase 2      | SQLite -> CRDB: same interface                           |
| `store.AlliumStore`      | Phase 2      | SQLite -> CRDB: same interface                           |

The `GraphStore` interface (for Gel, Phase 3) is intentionally NOT defined yet. Defining it now would be speculative -- the actual graph queries needed depend on accumulated data patterns that do not exist yet. Define it when Phase 3 begins.

---

## 9. What the Planning Documents Get Wrong

### 9.1 The Build Plan's CRDB Phase is Premature

The build plan (Phase 3B) specifies implementing CockroachDB immediately after the foundation. The synthesis correctly defers it, but the build plan's step-by-step instructions are already written. Anyone following the build plan linearly will build CRDB support before Meilisearch, which is backwards.

**Correction:** Skip Phase 3B entirely. Go from 3A (Foundation) directly to 4A (Meilisearch). Renumber accordingly.

### 9.2 The Vision's `hive-local` Dependency is Undefined

The architecture diagrams show `hive-local` as a Go binary on `:18820` that mediates between `hive-plugin` (TypeScript) and `hive-server`. But `hive-local` does not exist yet. The memory injection flow depends on it for the pre-prompt hook.

**Question that must be answered:** Who calls `POST /api/v1/memory/inject`? If it is hive-local, hive-local must exist first. If it is the Claude Code hook directly (via curl or a shell command), hive-local is not needed for Phase 1.

**Recommendation:** For Phase 1, have the Claude Code hook call hive-server directly. hive-local is a Phase 2 concern.

### 9.3 The Searcher Interface Does Not Handle Batch Operations Well

The current `Index` method accepts `[]Document`. But the reconciliation job indexes one document at a time in a loop. Meilisearch is much more efficient with batch upserts (100-1000 documents per call).

**Correction:** The reconciler should batch documents before calling `Index`:

```go
func (r *Reconciler) reconcile(ctx context.Context) {
    memories, err := r.store.ListMemory(ctx, model.MemoryFilter{Limit: 10000})
    if err != nil {
        r.logger.Error("reconcile: list memories", "error", err)
        return
    }
    batch := make([]search.Document, 0, 100)
    for _, m := range memories {
        batch = append(batch, r.sync.MemoryToDocument(m))
        if len(batch) >= 100 {
            if err := r.searcher.Index(ctx, "memories", batch); err != nil {
                r.logger.Error("reconcile: index batch", "error", err)
            }
            batch = batch[:0]
        }
    }
    if len(batch) > 0 {
        r.searcher.Index(ctx, "memories", batch)
    }
}
```

### 9.4 Event Payload Validation is Missing

The vision defines events with arbitrary JSON payloads. But there is no schema for event payloads. "task.completed" might have `{"task_id": "...", "duration_ms": 4500}` or it might have `{"foo": "bar"}`. Without payload schemas, analytics queries against the events table require defensive JSON parsing everywhere.

**Recommendation:** Define a small set of known event types with documented (but not enforced) payload schemas. Validate in the handler with a warning log for unknown types. Do not reject unknown types (extensibility), but do validate known types.

```go
var knownEventSchemas = map[string]func(json.RawMessage) error{
    "task.completed":       validateTaskCompletedPayload,
    "phase.transitioned":   validatePhaseTransitionPayload,
    "skill.invoked":        validateSkillInvokedPayload,
    "session.completed":    validateSessionCompletedPayload,
}

func (a *API) handleCreateEvent(ctx context.Context, input *CreateEventInput) (*EventOutput, error) {
    if validator, ok := knownEventSchemas[input.Body.EventType]; ok {
        if err := validator(input.Body.Payload); err != nil {
            a.logger.Warn("event payload validation failed",
                "type", input.Body.EventType, "error", err)
            // proceed anyway -- warn, don't reject
        }
    }
    // ...
}
```

---

## 10. Summary of Critical Decisions

| Decision                 | Recommendation                                                      | Rationale                                                   |
| ------------------------ | ------------------------------------------------------------------- | ----------------------------------------------------------- |
| Model types location     | `internal/model/` package                                           | Breaks circular dependency between handlers and store       |
| Store interface design   | Split into domain-specific interfaces, composed by `Store`          | Interface segregation; testability; optional skill stores   |
| Skill store registration | Nil-check in handlers, 404 if not configured                        | Features are additive; no runtime penalty for unused skills |
| Meilisearch sync         | Worker pool (4 workers, 1000-item buffer), not unbounded goroutines | Prevents goroutine leaks under load                         |
| Token budget             | Character-based (`max_chars`), not token-based                      | Server should not know about model tokenizers               |
| Keyword extraction       | Pass truncated prompt directly to Meilisearch                       | Meilisearch relevance ranking > naive extraction            |
| Migration strategy       | Numbered SQL files with embed.FS, not goose                         | Minimal dependencies until CRDB needs it                    |
| CRDB timing              | Defer until SQLite P99 write latency > 100ms                        | Measured bottleneck, not theoretical                        |
| Reconciliation batching  | 100 documents per Meilisearch Index call                            | 10-100x faster than document-at-a-time                      |
| Event payload validation | Warn on known-type schema violations, accept unknown types          | Extensibility without chaos                                 |
