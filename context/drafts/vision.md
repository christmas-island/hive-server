# Hive-Server Vision: Unified Agent Infrastructure

**Date:** 2026-03-09
**Status:** Vision Document (informs detailed build plan)

---

## 1. Executive Summary

hive-server evolves from a simple REST API with SQLite persistence into the **central nervous system for autonomous agent infrastructure**. It becomes three things simultaneously:

1. **Tool Abstraction API** -- A unified API that agents call for all storage, search, and knowledge operations. hive-server fans out to the appropriate backend (Gel DB for graph-relational queries, Meilisearch for full-text/fuzzy search, CockroachDB for transactional state) and returns unified results. Agents never interact with backends directly.

2. **Memory System for Per-Prompt Hook Injections** -- A memory layer that is queried on every agent prompt to inject relevant context. This replaces static TOOLS.md blobs with dynamic, per-prompt context injection, saving tokens and improving relevance.

3. **LLM-Enabled Project Manager** -- A task coordination server that uses LLM intelligence (via MasterClaw or direct API calls) to make decisions about task assignment, priority, routing, and delegation across a fleet of agents.

---

## 2. Architecture Overview

### 2.1 Full Request Path

```
                                 +-----------+
                                 | OpenClaw  |
                                 | Gateway   |
                                 +-----+-----+
                                       |
                                       v
                              +--------+--------+
                              |  hive-plugin    |
                              |  (TS shim)      |
                              +--------+--------+
                                       |
                                       v
                              +--------+--------+
                              |  hive-local     |
                              |  (Go, :18820)   |
                              +--------+--------+
                                       |
                                       v
                 +---------------------+---------------------+
                 |              hive-server                   |
                 |         (Go, k8s, Huma v2 API)             |
                 |                                            |
                 |  +-----------+  +----------+  +----------+|
                 |  | Query     |  | Memory   |  | Task     ||
                 |  | Router    |  | Injector |  | Manager  ||
                 |  +-----+-----+  +----+-----+  +----+-----+|
                 +--------+-------------+-------------+-------+
                          |             |             |
              +-----------+---+---------+---+---------+---+
              |               |             |             |
              v               v             v             v
        +-----+-----+  +-----+-----+  +----+------+  +---+--------+
        |   Gel DB   |  |Meilisearch|  |CockroachDB|  | MasterClaw |
        | (graph-    |  |(full-text/ |  |(txn state,|  | (OpenClaw  |
        | relational)|  | fuzzy)    |  | ACID)     |  |  in-cluster)|
        +-----------+  +-----------+  +-----------+  +------------+
```

### 2.2 Component Responsibilities

| Component       | Role                                                                                                                                                                                         | Technology                                 |
| --------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------ |
| **hive-plugin** | Thin TS registration shim. Registers a single `hive` tool with OpenClaw. Proxies all calls to hive-local.                                                                                    | TypeScript, OpenClaw plugin API            |
| **hive-local**  | Persistent local Go process. Exposes MCP tools. Handles token brokering, credential management, local caching. Translates single-tool subcommands into hive-server API calls.                | Go, port 18820                             |
| **hive-server** | Central API. Receives requests, routes to backends, synthesizes results, manages task lifecycle.                                                                                             | Go, chi/Huma v2, k8s                       |
| **Gel DB**      | Graph-relational knowledge store. Stores entities, relationships, ontologies. Handles queries that require traversing links (e.g., "what agents worked on tasks related to this codebase?"). | Gel (EdgeDB), EdgeQL, PostgreSQL backend   |
| **Meilisearch** | Full-text/fuzzy search engine. Handles natural-language queries, typo-tolerant lookups, memory search by content.                                                                            | Meilisearch (Rust), REST API               |
| **CockroachDB** | Transactional state store. Handles ACID operations: task state machines, agent heartbeats, memory CRUD with optimistic concurrency, audit logs. Source of truth for all mutable state.       | CockroachDB, pgx, PostgreSQL wire protocol |
| **MasterClaw**  | In-cluster OpenClaw instance. Synthesizes results from multiple backends, makes intelligent routing decisions, handles LLM-powered task management.                                          | OpenClaw Gateway, Claude/OpenAI            |

### 2.3 The Single-Tool Pattern (hive-local#13)

From the agent's perspective, there is exactly **one tool**: `hive`. All interactions are subcommands of this single tool, reducing the tool registration surface from 9+ tools to 1. This is critical for token efficiency -- agents do not burn context tokens on 9 separate tool schemas.

```
hive memory search "deployment issues last week"
hive memory store key=deploy-notes value="..."
hive task create title="Fix CI pipeline" priority=3
hive task list status=open assignee=me
hive discover tools
hive discover agents
```

hive-local translates these subcommands into the appropriate hive-server API calls. The single-tool pattern also means the tool description itself can be dynamic -- hive-local can tailor the tool schema based on the agent's role and available capabilities.

---

## 3. Tool Abstraction API

### 3.1 The Problem

LLM agents currently must understand multiple backend APIs, connection strings, and query languages. Each new backend adds cognitive load (more tool schemas to parse) and token cost (more tool descriptions injected into prompts). Backends have different consistency models, error semantics, and authentication mechanisms.

### 3.2 The Solution

hive-server presents a **single, unified API** that abstracts all backend operations. The API is organized by intent, not by backend:

| Intent                  | Endpoint                             | Backend(s)                                          |
| ----------------------- | ------------------------------------ | --------------------------------------------------- |
| Store a memory          | `POST /api/v1/memory`                | CockroachDB (write) + Meilisearch (index)           |
| Recall a memory by key  | `GET /api/v1/memory/{key}`           | CockroachDB (read)                                  |
| Search memories         | `POST /api/v1/memory/search`         | Meilisearch (search) -> CockroachDB (hydrate)       |
| Explore knowledge graph | `POST /api/v1/knowledge/query`       | Gel DB (EdgeQL)                                     |
| Create a task           | `POST /api/v1/tasks`                 | CockroachDB (write)                                 |
| Search tasks            | `POST /api/v1/tasks/search`          | Meilisearch (search) -> CockroachDB (hydrate)       |
| Discover capabilities   | `GET /api/v1/discover`               | Gel DB (graph traversal) + CockroachDB (live state) |
| Agent heartbeat         | `POST /api/v1/agents/{id}/heartbeat` | CockroachDB (write)                                 |

### 3.3 Query Router

The Query Router is a new internal component that examines each inbound request and decides which backend(s) to fan out to:

```go
type QueryRouter struct {
    gel    *gel.Client
    meili  *meilisearch.ServiceManager
    crdb   *pgxpool.Pool
    claw   *MasterClawClient
}

func (qr *QueryRouter) Route(ctx context.Context, req Request) (Response, error) {
    switch req.Intent {
    case IntentMemorySearch:
        // Fan out: Meilisearch for full-text, Gel for graph context
        searchResults := qr.meili.Search(...)
        graphContext := qr.gel.Query(...)
        // Merge and rank
        return qr.merge(searchResults, graphContext), nil

    case IntentMemoryStore:
        // Write to CockroachDB (source of truth), then async-index in Meilisearch
        result := qr.crdb.UpsertMemory(...)
        go qr.meili.Index(...)
        return result, nil

    case IntentKnowledgeQuery:
        // Pure Gel DB query for graph-relational traversal
        return qr.gel.Query(...), nil

    case IntentTaskAssign:
        // CockroachDB for state + MasterClaw for intelligent assignment
        candidates := qr.crdb.ListAgents(...)
        assignment := qr.claw.DecideAssignment(task, candidates)
        return qr.crdb.AssignTask(task, assignment), nil
    }
}
```

### 3.4 Backend Mapping: Which Backend Serves Which Purpose

**CockroachDB -- Transactional State (Source of Truth)**

- All CRUD operations for memory entries, tasks, agents, task notes
- Optimistic concurrency control (version columns)
- Task state machine enforcement
- Agent heartbeats and registration
- Audit trails and event logs
- Multi-tenancy via shared tables with tenant_id columns
- ACID guarantees for all state mutations

CockroachDB replaces SQLite as the production store (per locked decision #5 and issue #12). SQLite remains available for single-instance local development. The Store interface pattern already in hive-server makes this swap clean.

**Meilisearch -- Full-Text/Fuzzy Search**

- Memory content search with typo tolerance
- Task search by title/description
- Tool/capability discovery by natural language query
- Multi-index federated search across memories, tasks, tools
- Agent-scoped filtering via `agent_id` field (shared index, not per-tenant indexes)
- Synonym support for domain-specific vocabulary

Meilisearch is NOT a source of truth. It is a secondary index populated from CockroachDB. Writes go to CockroachDB first, then asynchronously indexed into Meilisearch. If Meilisearch is unavailable, search degrades gracefully (fall back to CockroachDB LIKE queries or JSONB containment).

**Gel DB -- Graph-Relational Knowledge**

- Entity relationships: agents, projects, codebases, tools, skills, dependencies
- Knowledge ontology: "Agent X is skilled at Go", "Project Y depends on Service Z"
- Graph traversal queries: "What agents have experience with this codebase?"
- Discovery API backing store: tool capabilities, agent capabilities, channel metadata
- Schema-first design with EdgeQL for expressive relational queries

Gel DB stores the **structural knowledge** that describes the world agents operate in, while CockroachDB stores the **operational state** that changes on every request.

**MasterClaw -- LLM Intelligence Layer**

- Synthesizes results from multiple backends into coherent responses
- Makes intelligent decisions about task routing and assignment
- Generates summaries of search results for memory injection
- Acts as the "brain" that transforms raw data into actionable context

### 3.5 k8s#58 Architecture: The Fan-Out-Synthesize Pattern

The architecture from k8s#58 is the core data flow pattern:

```
Agent Request
    |
    v
hive-server receives request
    |
    +---> Gel DB (graph-relational query)     ----+
    |                                              |
    +---> Meilisearch (full-text/fuzzy search) ---+
    |                                              |
    v                                              v
Results from both backends arrive at hive-server
    |
    v
MasterClaw (in-cluster OpenClaw) synthesizes results
    |
    v
Synthesized response returned through hive-server to agent
```

Key design decisions from k8s#58:

- **No Postgres directly**: Gel DB wraps PostgreSQL; CockroachDB uses the pgwire protocol. No standalone PostgreSQL instance needed.
- **No vector/embedding search**: Explicit decision -- no GPU in the cluster. Meilisearch's keyword + typo-tolerant search is sufficient. If semantic search is needed later, Meilisearch's hybrid search with external embedder endpoints can be added without architectural changes.
- **MasterClaw synthesizes**: Raw results from Gel and Meilisearch are not returned directly to agents. MasterClaw (an in-cluster OpenClaw instance) processes, ranks, summarizes, and formats the results into agent-consumable context.

---

## 4. Memory System for Per-Prompt Hook Injections

### 4.1 The Problem

Today, agent context is injected via static files (TOOLS.md, AGENTS.md, SOUL.md) that are loaded at agent initialization. These blobs:

- Burn context tokens on every prompt, regardless of relevance
- Cannot adapt to what the agent is currently working on
- Contain stale information (tool lists, agent rosters, project status)
- Are the same for every prompt in a session

This is the problem that Discovery API (#9) was originally designed to solve, but the solution needs to go further.

### 4.2 The Solution: Dynamic Context Injection

Every agent prompt passes through a memory injection pipeline before reaching the LLM:

```
Agent receives message/task
    |
    v
hive-local intercepts (pre-prompt hook)
    |
    v
hive-local calls hive-server: POST /api/v1/memory/inject
    Body: {
        "agent_id": "agent-42",
        "prompt_text": "<the current prompt>",
        "session_key": "sess-abc",
        "max_tokens": 2000
    }
    |
    v
hive-server Memory Injector:
    1. Extract key terms from prompt_text
    2. Fan out:
       a. Meilisearch: search memories matching key terms
       b. Gel DB: find related entities/context for this agent
       c. CockroachDB: get active tasks assigned to this agent
    3. MasterClaw: synthesize and rank results by relevance to prompt
    4. Trim to max_tokens budget
    |
    v
Response: {
    "context_blocks": [
        {"type": "memory", "content": "Previous finding: CI pipeline fails on arm64..."},
        {"type": "task", "content": "Active task: Fix arm64 CI (#47, priority 3, in_progress)"},
        {"type": "knowledge", "content": "This repo uses GoReleaser with multi-arch Docker builds"}
    ],
    "tokens_used": 847
}
    |
    v
hive-local injects context_blocks into agent prompt
    |
    v
Enriched prompt sent to LLM
```

### 4.3 What Discovery API (#9) Becomes

In the original vision, Discovery API was a metadata registry replacing static TOOLS.md. In the new architecture, it becomes a subset of the Memory Injection system:

- **Tool discovery** is now a Gel DB query: "What tools are available to agent X in context Y?"
- **Agent discovery** is now a CockroachDB + Gel query: "What agents are online and capable of task Z?"
- **Channel discovery** is now a Gel query: "What communication channels exist for project P?"

The Discovery API endpoints still exist, but they are powered by the unified backend architecture rather than a standalone metadata registry. The `/api/v1/discover` endpoint becomes a convenience wrapper around the knowledge graph:

```
GET /api/v1/discover?type=tools&agent_id=agent-42
    -> Gel DB: SELECT Tool { name, description, parameters }
              FILTER .available_to.id = <agent_id>

GET /api/v1/discover?type=agents&capability=go
    -> Gel DB: SELECT Agent { id, name, status, capabilities }
              FILTER 'go' IN .capabilities

GET /api/v1/discover?type=channels&project=hive-server
    -> Gel DB: SELECT Channel { name, type, connected_agents }
              FILTER .project.name = 'hive-server'
```

### 4.4 Memory Lifecycle

```
1. Agent produces output (code change, finding, decision)
       |
       v
2. hive-local stores memory: POST /api/v1/memory
       Body: { key, value, tags, agent_id }
       |
       v
3. hive-server writes to CockroachDB (source of truth)
       |
       +---> Async: index in Meilisearch (full-text searchable)
       +---> Async: update Gel knowledge graph if entity-relevant
       |
       v
4. Later, another agent's prompt triggers memory injection
       |
       v
5. Relevant memories retrieved and injected as context
```

### 4.5 Token Budget Management

The Memory Injector respects a `max_tokens` budget per injection request. This prevents memory injection from consuming the agent's entire context window. The budget is configurable per agent and per request:

- Default: 2000 tokens per injection
- Configurable via agent settings in Gel DB
- Adjustable per-request by hive-local based on prompt size and remaining context budget
- MasterClaw handles the ranking/pruning: it sees all candidate memories and selects the most relevant subset that fits within the budget

### 4.6 Multi-Tenancy in the Memory System

Memory isolation operates on three levels:

1. **Agent-private memories**: Only visible to the creating agent. Stored with `scope=private` and filtered by `agent_id`.
2. **Project-shared memories**: Visible to all agents working on the same project. Stored with `scope=project` and filtered by `project_id`.
3. **Global memories**: Visible to all agents in the tenant. Stored with `scope=global`.

In CockroachDB, this is enforced via the application layer (middleware sets scope filters based on agent identity and project membership). In Meilisearch, this is enforced via `filterableAttributes` on `agent_id`, `project_id`, and `scope` fields.

In Gel DB, access policies can enforce this at the database level:

```sdl
type Memory extending HasTimestamps {
    required key: str { constraint exclusive; };
    required value: str;
    required owner: Agent;
    required scope: MemoryScope;
    project: Project;

    access policy agent_isolation
        allow select, update, delete
        using (
            .scope = MemoryScope.global
            OR .owner = global current_agent
            OR (.scope = MemoryScope.project
                AND .project IN global current_agent.projects)
        );
}
```

---

## 5. LLM-Enabled Project Manager

### 5.1 The Problem

Current task coordination is purely mechanical: tasks have a state machine (`open` -> `claimed` -> `in_progress` -> `done`/`failed`/`cancelled`), but there is no intelligence in how tasks are assigned, prioritized, or decomposed. An agent claims a task and works on it; there is no system that can:

- Look at a high-level goal and decompose it into subtasks
- Examine agent capabilities and workload, then assign optimally
- Detect when a task is blocked or failing and reassign
- Prioritize across a backlog based on dependencies and urgency
- Summarize progress for human operators

### 5.2 The Solution: MasterClaw as Project Manager

MasterClaw is an in-cluster OpenClaw instance that acts as the intelligent layer for task management. It is NOT a general-purpose agent -- it is specifically configured for project management operations.

#### MasterClaw Responsibilities

**Task Decomposition**

```
Human or agent creates high-level task:
    "Migrate hive-server from SQLite to CockroachDB"
        |
        v
hive-server forwards to MasterClaw for decomposition:
    POST /hooks/agent {
        agentId: "masterclaw",
        message: "Decompose task: <task details>. Available agents: <agent list with capabilities>."
    }
        |
        v
MasterClaw returns subtasks:
    1. "Define Store interface" (Go, backend, ~2 hours)
    2. "Implement CockroachDB store" (Go, pgx, ~4 hours)
    3. "Write goose migrations" (SQL, ~1 hour)
    4. "Update handler tests" (Go, testing, ~2 hours)
    5. "Add integration test infrastructure" (Docker, CI, ~2 hours)
        |
        v
hive-server creates subtasks in CockroachDB, linked to parent task
```

**Intelligent Assignment**

```
New task arrives or agent becomes available
    |
    v
hive-server queries:
    - CockroachDB: agent heartbeats, current workload, task queue
    - Gel DB: agent capabilities, past performance on similar tasks
    |
    v
MasterClaw evaluates:
    - Agent X has Go + pgx experience, currently idle
    - Agent Y has Go experience but is working on 2 tasks
    - Agent Z is offline
    -> Assign to Agent X
    |
    v
hive-server updates task assignment in CockroachDB
hive-server notifies Agent X via webhook/channel
```

**Progress Monitoring**

```
Periodic check (cron or event-driven):
    |
    v
hive-server queries CockroachDB:
    - Tasks in_progress for > N hours without updates
    - Tasks with failed subtasks
    - Agents that went offline with claimed tasks
    |
    v
MasterClaw evaluates:
    - Task #47 has been in_progress for 6 hours, agent went offline
    -> Recommend: unclaim task, reassign to available agent
    - Task #48 has 3/5 subtasks done, 2 blocked by #47
    -> Recommend: prioritize #47 resolution before continuing #48
    |
    v
hive-server takes recommended actions (or surfaces for human review)
```

### 5.3 MasterClaw Integration Architecture

MasterClaw runs as a separate Deployment in the same k8s cluster:

```yaml
# Simplified -- actual manifest in k8s repo
apiVersion: apps/v1
kind: Deployment
metadata:
  name: masterclaw
spec:
  replicas: 1
  template:
    spec:
      containers:
        - name: openclaw
          image: openclaw/openclaw:latest
          env:
            - name: OPENCLAW_PORT
              value: "3000"
            - name: OPENCLAW_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: masterclaw-secrets
                  key: auth-token
          volumeMounts:
            - name: workspace
              mountPath: /workspace
      volumes:
        - name: workspace
          configMap:
            name: masterclaw-workspace
```

hive-server communicates with MasterClaw via OpenClaw's webhook API:

```go
type MasterClawClient struct {
    baseURL   string
    authToken string
    http      *http.Client
}

func (mc *MasterClawClient) Synthesize(ctx context.Context, req SynthesisRequest) (*SynthesisResponse, error) {
    // POST /hooks/agent with structured prompt
    // Returns synthesized/ranked results
}

func (mc *MasterClawClient) DecomposeTask(ctx context.Context, task Task, agents []Agent) ([]Task, error) {
    // POST /hooks/agent with task decomposition prompt
    // Returns list of subtasks
}

func (mc *MasterClawClient) DecideAssignment(ctx context.Context, task Task, candidates []Agent) (*Agent, error) {
    // POST /hooks/agent with assignment decision prompt
    // Returns selected agent
}
```

### 5.4 Task Coordination Flow

```
1. Task Created (by human, agent, or MasterClaw decomposition)
       |
       v
2. hive-server stores task in CockroachDB
       Status: open
       |
       v
3. MasterClaw evaluates assignment (if auto-assign enabled)
       Queries: CockroachDB (agent state), Gel (agent capabilities)
       |
       v
4. Task assigned to agent
       Status: open -> claimed
       Agent notified via hive-local -> hive-plugin -> OpenClaw channel
       |
       v
5. Agent begins work
       Status: claimed -> in_progress
       Agent stores progress notes via hive-server
       Agent stores memories (findings, decisions) via hive-server
       |
       v
6. Agent completes or fails
       Status: in_progress -> done | failed
       If failed: MasterClaw evaluates retry/reassignment
       If done: MasterClaw checks if parent task can progress
       |
       v
7. Progress reported
       Auto-report to only-claws API (#19)
       Human-readable summary via MasterClaw
```

### 5.5 Task Locking and Concurrency

CockroachDB's serializable transactions provide the foundation for task locking:

```go
// Claim a task atomically -- no two agents can claim the same task
func (s *CRDBStore) ClaimTask(ctx context.Context, taskID, agentID string) error {
    return crdbpgx.ExecuteTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
        var currentStatus, currentAssignee string
        err := tx.QueryRow(ctx,
            `SELECT status, assignee FROM tasks WHERE id = $1 FOR UPDATE`,
            taskID,
        ).Scan(&currentStatus, &currentAssignee)
        if err != nil {
            return err
        }
        if currentStatus != "open" {
            return ErrInvalidTransition
        }
        _, err = tx.Exec(ctx,
            `UPDATE tasks SET status = 'claimed', assignee = $1, updated_at = now()
             WHERE id = $2`,
            agentID, taskID,
        )
        return err
    })
}
```

The `FOR UPDATE` clause in CockroachDB acquires an exclusive lock on the row, preventing concurrent claims. Combined with `crdbpgx.ExecuteTx` retry logic, this handles serialization conflicts cleanly.

---

## 6. Data Model Evolution

### 6.1 CockroachDB Schema (Operational State)

Extends the existing schema from the CockroachDB brief with new tables for the enhanced architecture:

```sql
-- Core tables (from existing schema, translated to CockroachDB)
CREATE TABLE memory (
    key         TEXT        NOT NULL PRIMARY KEY,
    value       TEXT        NOT NULL DEFAULT '',
    agent_id    TEXT        NOT NULL DEFAULT '',
    project_id  TEXT        NOT NULL DEFAULT '',
    scope       TEXT        NOT NULL DEFAULT 'private',  -- private, project, global
    tags        JSONB       NOT NULL DEFAULT '[]'::JSONB,
    version     INT8        NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE tasks (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_id   UUID        REFERENCES tasks(id),  -- subtask hierarchy
    title       TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    status      TEXT        NOT NULL DEFAULT 'open',
    creator     TEXT        NOT NULL,
    assignee    TEXT        NOT NULL DEFAULT '',
    priority    INT4        NOT NULL DEFAULT 0,
    tags        JSONB       NOT NULL DEFAULT '[]'::JSONB,
    metadata    JSONB       NOT NULL DEFAULT '{}'::JSONB,  -- extensible
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE task_notes (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     UUID        NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    note        TEXT        NOT NULL,
    agent_id    TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE agents (
    id              TEXT        NOT NULL PRIMARY KEY,
    name            TEXT        NOT NULL DEFAULT '',
    status          TEXT        NOT NULL DEFAULT 'offline',
    capabilities    JSONB       NOT NULL DEFAULT '[]'::JSONB,
    metadata        JSONB       NOT NULL DEFAULT '{}'::JSONB,
    last_heartbeat  TIMESTAMPTZ NOT NULL DEFAULT now(),
    registered_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- New: injection log for debugging/optimization
CREATE TABLE injection_log (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    TEXT        NOT NULL,
    session_key TEXT        NOT NULL DEFAULT '',
    prompt_hash TEXT        NOT NULL,  -- hash of prompt for dedup
    context_blocks JSONB    NOT NULL DEFAULT '[]'::JSONB,
    tokens_used INT4        NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Indexes
CREATE INDEX idx_memory_agent ON memory(agent_id);
CREATE INDEX idx_memory_scope ON memory(scope);
CREATE INDEX idx_memory_project ON memory(project_id);
CREATE INVERTED INDEX idx_memory_tags ON memory(tags);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_assignee ON tasks(assignee);
CREATE INDEX idx_tasks_creator ON tasks(creator);
CREATE INDEX idx_tasks_parent ON tasks(parent_id);
CREATE INVERTED INDEX idx_tasks_tags ON tasks(tags);
CREATE INDEX idx_task_notes_task ON task_notes(task_id);
CREATE INDEX idx_injection_log_agent ON injection_log(agent_id);
```

### 6.2 Gel DB Schema (Knowledge Graph)

```sdl
# Agents and their relationships
type Agent extending HasTimestamps {
    required agent_id: str { constraint exclusive; };
    required name: str;
    multi capabilities: str;
    multi projects: Project;
    multi tools: Tool;
}

# Projects as organizational units
type Project extending HasTimestamps {
    required name: str { constraint exclusive; };
    description: str;
    multi repositories: Repository;
    multi agents: Agent;
}

type Repository extending HasTimestamps {
    required url: str { constraint exclusive; };
    required name: str;
    language: str;
    multi dependencies: Repository;
    required project: Project;
}

# Tool registry (replaces static TOOLS.md)
type Tool extending HasTimestamps {
    required name: str { constraint exclusive; };
    required description: str;
    parameters_schema: json;  -- JSON Schema for tool parameters
    multi available_to: Agent;
    multi required_capabilities: str;
}

# Channels for agent communication
type Channel extending HasTimestamps {
    required name: str;
    required channel_type: ChannelType;
    multi connected_agents: Agent;
    project: Project;
}

scalar type ChannelType extending enum<
    'slack', 'discord', 'webhook', 'internal'
>;

# Abstract base
abstract type HasTimestamps {
    required created_at: datetime { default := datetime_current(); };
    required updated_at: datetime { default := datetime_current(); };
}
```

### 6.3 Meilisearch Indexes

```
Index: memories
  Primary key: key
  Searchable: [content, title, tags]
  Filterable: [agent_id, project_id, scope, created_at, updated_at]
  Sortable: [created_at, updated_at]

Index: tasks
  Primary key: id
  Searchable: [title, description, tags]
  Filterable: [status, assignee, creator, priority, created_at]
  Sortable: [created_at, priority, updated_at]

Index: tools
  Primary key: name
  Searchable: [name, description, capabilities]
  Filterable: [capabilities]
  Sortable: [name]
```

---

## 7. Issue Resolution and Migration Path

### 7.1 Existing Issues in the New Architecture

| Issue                             | Status in New Architecture                                                                                                                                                                            |
| --------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **#9 Discovery API**              | Evolves into the Knowledge Graph query layer (Gel DB). The `/api/v1/discover` endpoints become wrappers around Gel queries. No longer a standalone metadata registry.                                 |
| **#10 Remove k8s/**               | Still valid. k8s manifests move to the ops/k8s repo.                                                                                                                                                  |
| **#11 Scripts pattern**           | Still valid. Integration tests now need CockroachDB, Meilisearch, and Gel containers. `script/integration/test` spins up all three via Docker Compose.                                                |
| **#12 SQLite -> CockroachDB**     | Core to the new architecture. CockroachDB becomes the transactional state store. Implementation unchanged from the issue, but scope expands to include new tables (injection_log, subtask hierarchy). |
| **#13 Update tests for CRDB**     | Still valid. Store tests need CRDB. Handler tests remain mock-based.                                                                                                                                  |
| **#14 Ephemeral CRDB**            | Still valid. `cockroach-go/v2/testserver` for integration tests.                                                                                                                                      |
| **#15 Update k8s for CRDB**       | Expands: k8s deployment now includes CRDB, Gel, Meilisearch, and MasterClaw pods.                                                                                                                     |
| **#16 Huma v2**                   | Still valid and still the prerequisite for CRDB. OpenAPI generation enables client codegen for hive-local.                                                                                            |
| **#17 E2E tests**                 | Still valid. E2E tests now cover the full fan-out path.                                                                                                                                               |
| **#18 CRDB tx retries**           | Still valid. Ships with #12.                                                                                                                                                                          |
| **#19 Auto-report to only-claws** | MasterClaw handles this. Agent activity is tracked in CockroachDB; MasterClaw formats and reports.                                                                                                    |
| **#20 Project layout**            | Still first in chain. Layout now needs `internal/search/`, `internal/knowledge/`, `internal/inject/` packages.                                                                                        |
| **#21 LSP plugin lifecycle**      | Unchanged. hive-local concern.                                                                                                                                                                        |
| **#22 Stateful store research**   | Scope narrows. hive-local only needs local caching/state, not a full database. Pebble or Badger for local KV cache.                                                                                   |

### 7.2 Dependency Graph Update

```
Phase 3: Foundation (preserving existing dependency chain)
  #10 (rm k8s/)                  -- independent, ready
  #11 (scripts)                  -- independent, ready
  #20 (project layout)           -- first structural change
      adds: internal/search/, internal/knowledge/, internal/inject/
    -> #16 (Huma v2)             -- blocked by #20
         -> #12 (CRDB)           -- blocked by #16
              +-> #18 (tx retries, ships WITH #12)
              +-> #13 (test updates)
              +-> #14 (ephemeral CRDB)
              +-> #15 (k8s deploy, now includes Gel + Meili + MasterClaw)
  #17 (E2E tests)                -- independent, benefits from #16

Phase 4: Backend Integration (NEW -- after Phase 3)
  #23 (Meilisearch integration)  -- search interface + Meili implementation
  #24 (Gel DB integration)       -- knowledge graph schema + gel-go client
  #25 (MasterClaw deployment)    -- in-cluster OpenClaw for synthesis
  #26 (Query Router)             -- fan-out logic, depends on #23, #24
  #27 (Memory Injector)          -- per-prompt injection, depends on #26
  #28 (Task Intelligence)        -- LLM-powered task management, depends on #25, #26

Phase 5: Advanced Features
  #9  (Discovery API v2)         -- now powered by Gel, depends on #24
  #19 (auto-report via MasterClaw) -- depends on #25
  #21 (LSP plugin lifecycle)
  #22 (stateful store research)
```

### 7.3 Conflict Resolution

**Locked Decision #5 (CockroachDB for production) vs Gel DB**

No conflict. CockroachDB handles transactional state. Gel DB handles the knowledge graph. They serve different purposes. Gel DB is not a replacement for CockroachDB -- it is an additional backend for graph-relational queries.

**Locked Decision #6 (Huma v2 before CRDB migration) still holds**

Huma v2 provides OpenAPI spec generation, which is even more valuable now: the expanded API surface (memory injection, knowledge queries, discovery) benefits from auto-generated docs and client SDKs.

**OpenClaw's built-in memory system vs hive-server's memory system**

Clear ownership boundary: OpenClaw's built-in memory (FTS5 + vector search in local SQLite) is for **session-level, agent-local memory**. hive-server's memory system is for **cross-agent, persistent, shared memory**. hive-local bridges the two: it can query both OpenClaw's local memory and hive-server's shared memory, merging results for injection.

**No vector/embedding search decision (k8s#58) vs Meilisearch hybrid search**

Compatible. The decision was "no GPU" -- we do not run our own embedding model. However, Meilisearch's hybrid search can use external embedding endpoints (OpenAI, etc.) if semantic search is later desired. The architecture does not preclude it; it just is not required at launch.

---

## 8. API Surface (Huma v2)

The full API after all phases, organized by domain:

### Memory

```
POST   /api/v1/memory              -- Create/update memory entry
GET    /api/v1/memory               -- List memory entries (filters: tag, agent, prefix, scope)
GET    /api/v1/memory/{key}         -- Get single entry
DELETE /api/v1/memory/{key}         -- Delete entry
POST   /api/v1/memory/search       -- Full-text search (Meilisearch)
POST   /api/v1/memory/inject       -- Get context injection for a prompt
```

### Tasks

```
POST   /api/v1/tasks               -- Create task
GET    /api/v1/tasks                -- List tasks (filters: status, assignee, creator, parent)
GET    /api/v1/tasks/{id}           -- Get task with notes and subtasks
PATCH  /api/v1/tasks/{id}           -- Update status/assignee/note
DELETE /api/v1/tasks/{id}           -- Delete task
POST   /api/v1/tasks/search        -- Full-text search (Meilisearch)
POST   /api/v1/tasks/{id}/decompose -- LLM-powered subtask generation (MasterClaw)
POST   /api/v1/tasks/{id}/assign    -- LLM-powered assignment (MasterClaw)
```

### Agents

```
POST   /api/v1/agents/{id}/heartbeat -- Register/update agent
GET    /api/v1/agents                -- List all agents
GET    /api/v1/agents/{id}           -- Get agent by ID
```

### Discovery (Knowledge Graph)

```
GET    /api/v1/discover              -- Unified discovery (query: type, capability, project)
GET    /api/v1/discover/tools        -- Available tools
GET    /api/v1/discover/agents       -- Available agents
GET    /api/v1/discover/channels     -- Available channels
PUT    /api/v1/discover/tools/{name} -- Register/update tool metadata
PUT    /api/v1/discover/agents/{id}  -- Register/update agent metadata
```

### Knowledge Graph (Advanced)

```
POST   /api/v1/knowledge/query      -- Raw EdgeQL query (restricted, admin-only)
POST   /api/v1/knowledge/relate     -- Create relationship between entities
GET    /api/v1/knowledge/graph       -- Subgraph visualization data
```

### Health

```
GET    /health                       -- Health check (no auth)
GET    /ready                        -- Readiness (checks all backends)
GET    /healthz                      -- Deep health (pings CRDB, Gel, Meili)
```

---

## 9. Deployment Architecture

### 9.1 Kubernetes Topology

```
Namespace: hive
  |
  +-- Deployment: hive-server (2+ replicas)
  |     Go binary, stateless, connects to all backends
  |     Resources: 256Mi-512Mi RAM, 100m-500m CPU
  |
  +-- StatefulSet: cockroachdb (3 nodes minimum)
  |     Or: CockroachDB Operator CrdbCluster
  |     Resources: 2Gi RAM, 1 CPU per node
  |     Storage: 10Gi+ PVC per node
  |
  +-- Deployment: gel (1+ replicas)
  |     Gel server with external PostgreSQL backend (CockroachDB or managed PG)
  |     Resources: 1Gi+ RAM (Gel minimum), 500m CPU
  |     Or: Gel against CockroachDB's pgwire protocol (if compatible)
  |
  +-- Deployment: meilisearch (1 replica, CE)
  |     Single-node community edition
  |     Resources: 512Mi-2Gi RAM, 500m CPU
  |     Storage: 10Gi PVC
  |
  +-- Deployment: masterclaw (1 replica)
  |     OpenClaw Gateway, configured as project manager agent
  |     Resources: 512Mi RAM, 250m CPU
  |     NOT exposed to public internet (CVE-2026-25253, CVE-2026-25157)
  |
  +-- Service: hive-server (ClusterIP + Ingress)
  +-- Service: cockroachdb (ClusterIP, ports 26257/8080)
  +-- Service: gel (ClusterIP, port 5656)
  +-- Service: meilisearch (ClusterIP, port 7700)
  +-- Service: masterclaw (ClusterIP, port 3000)
```

### 9.2 Local Development

For local development, all backends are optional and hive-server degrades gracefully:

```
# Minimal (SQLite only, same as today)
HIVE_DB_DRIVER=sqlite HIVE_DB_DSN=data/hive.db ./hive-server serve

# With Meilisearch
docker run -d -p 7700:7700 getmeili/meilisearch:v1.12
HIVE_DB_DRIVER=sqlite MEILI_URL=http://localhost:7700 ./hive-server serve

# Full stack
docker compose up -d  # cockroachdb, gel, meilisearch, masterclaw
HIVE_DB_DRIVER=cockroach \
HIVE_DB_DSN=postgresql://root@localhost:26257/hive?sslmode=disable \
GEL_DSN=gel://localhost:5656 \
MEILI_URL=http://localhost:7700 \
MASTERCLAW_URL=http://localhost:3000 \
./hive-server serve
```

### 9.3 Graceful Degradation

Each backend is independently optional. When a backend is unavailable:

| Backend Unavailable | Degraded Behavior                                                                                                                                    |
| ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Meilisearch**     | Search falls back to CockroachDB `LIKE` / JSONB queries. Slower, no typo tolerance, but functional.                                                  |
| **Gel DB**          | Discovery returns data from CockroachDB only (no graph traversal). Knowledge graph queries return 503.                                               |
| **MasterClaw**      | Task decomposition/assignment unavailable. Tasks must be manually created and assigned. Memory injection skips synthesis step (returns raw results). |
| **CockroachDB**     | If SQLite fallback configured: use SQLite. Otherwise: 503 for all state operations.                                                                  |

---

## 10. Security Considerations

### 10.1 Network Isolation

- MasterClaw (OpenClaw) is NEVER exposed to the public internet. Internal ClusterIP service only. This mitigates CVE-2026-25253 and CVE-2026-25157.
- Meilisearch is internal-only. hive-server is the only ingress point for search queries.
- Gel DB is internal-only. No external access to EdgeQL/PostgreSQL ports.
- CockroachDB is internal-only. Console (port 8080) restricted to operators.

### 10.2 Authentication Chain

```
External Agent -> Bearer token (HIVE_TOKEN) -> hive-server
hive-server -> MEILI_API_KEY -> Meilisearch
hive-server -> GEL_DSN (includes credentials) -> Gel DB
hive-server -> DATABASE_URL (includes credentials) -> CockroachDB
hive-server -> MASTERCLAW_TOKEN -> MasterClaw
```

All inter-service credentials stored as k8s Secrets, injected as environment variables.

### 10.3 Multi-Tenancy Security

- Application-layer enforcement in hive-server middleware (agent_id, project_id scoping)
- CockroachDB RLS policies as defense-in-depth (Phase 5)
- Meilisearch agent-scoped filters injected server-side (agents cannot bypass)
- Gel DB access policies on types (Phase 5)

---

## 11. Key Design Decisions

1. **CockroachDB is the source of truth for all mutable state.** Meilisearch and Gel DB are secondary indexes/stores populated from CockroachDB. If there is a conflict, CockroachDB wins.

2. **Gel DB stores structural knowledge, not operational state.** Agent capabilities, tool registries, project relationships, and entity ontologies live in Gel. Task status, memory entries, and heartbeats live in CockroachDB.

3. **Meilisearch is a search index, not a database.** It is populated asynchronously from CockroachDB writes. It can be rebuilt from scratch by re-indexing CockroachDB data.

4. **MasterClaw is stateless (from hive-server's perspective).** It receives a prompt with all necessary context and returns a response. It does not maintain its own state about tasks or agents. All state is in CockroachDB.

5. **The single-tool pattern is mandatory.** Agents interact with one tool (`hive`) that has subcommands. hive-local handles the translation. This is non-negotiable for token efficiency.

6. **Graceful degradation over hard dependencies.** Every backend except the primary store (CockroachDB or SQLite) is optional. Features degrade, but the system remains operational.

7. **No GPU, no self-hosted embeddings.** Semantic/vector search, if needed, uses external embedding APIs through Meilisearch's hybrid search. The cluster runs on CPU-only nodes.

8. **MasterClaw is internal-only.** It is never exposed outside the cluster. All LLM operations route through hive-server's API.

---

## 12. Open Questions

1. **Can Gel DB use CockroachDB as its PostgreSQL backend?** Gel connects to PostgreSQL via `GEL_SERVER_BACKEND_DSN`. CockroachDB implements pgwire but has ~40% PostgreSQL compatibility. Testing required to determine if Gel's internal queries work against CockroachDB, or if a separate managed PostgreSQL instance is needed for Gel's backend.

2. **MasterClaw cost model.** Every synthesis, decomposition, and assignment decision is an LLM API call. What is the per-request cost, and should there be rate limiting or caching? Should some decisions (e.g., simple task assignments) use rules instead of LLM calls?

3. **Sync consistency between CockroachDB and Meilisearch.** Asynchronous indexing means there is a window where CockroachDB has data that Meilisearch does not. Is this acceptable? Should there be a periodic full-sync reconciliation job?

4. **Memory injection latency budget.** If every prompt triggers a fan-out to 3 backends + MasterClaw synthesis, what is the acceptable latency? Should hive-local cache recent injections?

5. **Gel DB Go client maturity.** The gel-go client is noted as less mature than the TypeScript client. Are there blocking gaps for hive-server's use cases?

6. **CockroachDB licensing.** Enterprise Free requires annual renewal and mandatory telemetry. Is this acceptable for the christmas-island org? Should PostgreSQL or YugabyteDB be evaluated as alternatives?

---

## 13. Success Metrics

The vision is realized when:

1. An agent can call `hive memory search "deployment issue"` and get relevant results from across all agents' memories, ranked by relevance, in under 500ms.

2. Every agent prompt is automatically enriched with relevant context (memories, active tasks, tool information) without burning tokens on static blobs.

3. A human can create a high-level task ("migrate to CockroachDB") and MasterClaw decomposes it into subtasks, assigns them to capable agents, and tracks progress to completion.

4. Adding a new backend or tool type requires implementing one interface and registering it with the Query Router -- no changes to the API surface or agent-facing tools.

5. The system operates with all backends, with any subset of optional backends, and in local-development mode with just SQLite -- without code changes, only configuration.
