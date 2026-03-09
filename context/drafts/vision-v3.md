# Hive-Server Vision v3: The Skill Replacement Platform

**Date:** 2026-03-09
**Status:** Revised vision incorporating production realities and corrected infrastructure assumptions
**Supersedes:** vision-v2.md
**Inputs:** All prior context documents, 4 ultra-think perspectives, 3 skill briefs, 3 database briefs, final review

---

## 0. What Changed From v2

Vision v2 got three things wrong. This document corrects them.

**Wrong #1: SQLite-first.** CockroachDB is already running in production. It is not Phase 4. It is not a scaling decision. It is a locked infrastructure decision that is deployed and operational. The synthesis recommendation to "defer CRDB until justified by specific, tangible needs" was written without knowledge that CRDB is already the production database. This document treats CockroachDB as Phase 0 -- it is the primary transactional store from day one.

**Wrong #2: Enhance skills, do not replace them.** The v2 principle was "hive-server provides durable state and search; skills keep their own planning, orchestration, and workflow logic." The revised direction is the opposite: hive-server INCORPORATES the capabilities of GSD, Superpowers, and Allium as API-backed services, so that an MCP plugin talking to hive-server can do everything those skills do. The skills become unnecessary, not enhanced.

**Wrong #3: Databases are sequential.** v2 phased databases: SQLite now, Meilisearch later, Gel much later, CRDB at scale. The revised architecture runs all three databases from the start:

- CockroachDB: already running, primary transactional store
- Meilisearch: search and discovery across all content
- Gel DB: graph-relational queries for relationships, dependencies, traversal

---

## 1. What Hive-Server Becomes

### 1.1 The Platform

Hive-server is not a coordination hub. It is not a state store that skills call. It is the **platform that replaces standalone skills entirely**.

Today, an agent uses GSD by loading prompt templates that instruct the LLM to follow a workflow and write markdown files. The agent uses Superpowers by loading skill definitions that guide behavior. The agent uses Allium by loading spec files and running CLI tools.

Tomorrow, an agent uses hive-server through an MCP plugin that exposes planning, orchestration, memory, search, spec management, and analytics as tool calls. The prompt engineering, workflow state, skill definitions, and spec storage all live in hive-server, queryable and persistent, not in markdown files scattered across the filesystem.

### 1.2 Why Replace Instead of Enhance

The v2 approach of "enhance skills, do not replace them" assumed the skills are stable, maintained dependencies worth preserving. They are not:

- **GSD** stores all state as markdown files in `.planning/`. No querying, no indexing, no concurrent access, no cross-session memory. Every skill independently invented its own fragile persistence layer. The filesystem-as-database pattern is the core problem, not a feature to preserve.

- **Superpowers** has no persistent state at all. Every session starts from zero. Skills are markdown files interpreted by the LLM. The entire "skill discovery" mechanism is a JavaScript file that scans directories for YAML frontmatter.

- **Allium** stores specs as flat `.allium` files with a Rust parser for ASTs. Drift detection is per-session. No structured querying of spec content, no cross-spec impact analysis, no drift trend tracking.

Enhancing these skills means modifying their source code to make HTTP calls to hive-server -- modifying repos we do not control, adding network dependencies to zero-infrastructure tools, and maintaining two state systems (files + database) that must stay in sync.

Replacing them means building their capabilities into hive-server and exposing them through the MCP plugin. The agent gets the same workflows (planning, TDD, spec management) but backed by persistent, queryable, cross-session state. The skills disappear from the dependency chain.

### 1.3 What Capabilities Are Absorbed

| Skill       | Capability                                     | hive-server Provides                                                                      |
| ----------- | ---------------------------------------------- | ----------------------------------------------------------------------------------------- |
| GSD         | Project initialization with research           | Planning API: create project, run structured research phase, store findings               |
| GSD         | Phase/plan/task decomposition                  | Planning API: create phases, generate plans, break into tasks with dependencies           |
| GSD         | Wave-based parallel execution                  | Orchestration API: dependency-ordered wave scheduling, concurrent agent dispatch          |
| GSD         | STATE.md living memory                         | Memory API: persistent, queryable project state with session continuity                   |
| GSD         | Verification (Nyquist)                         | Planning API: verification tasks linked to implementation tasks, automated check tracking |
| GSD         | Model profile system (quality/balanced/budget) | Orchestration API: agent capability profiles, configurable model tiers per task type      |
| Superpowers | Skill discovery and resolution                 | Search API: skill catalog indexed in Meilisearch, on-demand discovery by context          |
| Superpowers | Brainstorm-plan-execute pipeline               | Planning API: structured workflow with gates between stages                               |
| Superpowers | Session memory                                 | Memory API: cross-session memory with search, injected into new sessions                  |
| Superpowers | Skill effectiveness tracking                   | Analytics API: invocation records, success rates, duration metrics per skill              |
| Superpowers | Subagent dispatch and coordination             | Orchestration API: task assignment, agent tracking, result collection                     |
| Allium      | Spec storage and versioning                    | Specs API: structured spec storage with version history                                   |
| Allium      | Drift detection (Weed agent)                   | Specs API: drift report submission, trend tracking, cross-spec analysis                   |
| Allium      | Spec composition and imports                   | Graph queries via Gel: cross-spec references, dependency traversal                        |
| Allium      | Spec search and discovery                      | Search API: spec content indexed in Meilisearch, full-text and structural queries         |

---

## 2. Database Roles (All Active)

### 2.1 CockroachDB: Primary Transactional Store (Already Running)

CockroachDB owns all transactional state. It is the source of truth for every entity in the system.

**What it stores:**

- All core entities: memory entries, tasks, task notes, agents
- Planning entities: projects, phases, plans, requirements
- Orchestration entities: workflows, waves, agent assignments
- Spec entities: spec metadata, construct summaries, drift reports
- Event stream: append-only events for all domains
- Session records: session summaries, session-scoped state

**Why CockroachDB:**

- Already deployed and operational. This is not a decision to make; it is a fact.
- Serializable transactions for concurrent agent writes (multiple agents claiming tasks, updating state simultaneously)
- JSONB support for flexible metadata fields (tags, capabilities, payloads)
- PostgreSQL-compatible wire protocol -- pgx/v5 driver, standard SQL
- Horizontal scaling available if/when needed

**Driver and patterns:**

- `jackc/pgx/v5` in standalone mode with `pgxpool` for connection pooling
- `crdbpgx.ExecuteTx()` for all write transactions (handles serialization retry)
- `$1, $2, ...` placeholders, `TIMESTAMPTZ` for timestamps, `JSONB` for structured fields
- `gen_random_uuid()` for ID generation (avoids hot-spot ranges)
- goose for schema migrations, embedded in binary

**Connection configuration:**

```
DATABASE_URL=postgresql://hive@crdb:26257/hive?sslmode=verify-full
```

### 2.2 Meilisearch: Search and Discovery

Meilisearch is a secondary index. It does not own data. Every document in Meilisearch is derived from CockroachDB. If Meilisearch data is lost, it is rebuilt from CRDB.

**What it indexes:**

| Index      | Source                            | Searchable Fields         | Filterable Fields                         | Purpose                    |
| ---------- | --------------------------------- | ------------------------- | ----------------------------------------- | -------------------------- |
| `memories` | CRDB memory table                 | value, tags               | agent_id, repo, scope, session_id         | Find memories by content   |
| `tasks`    | CRDB tasks table                  | title, description, tags  | status, assignee, creator, priority, repo | Find tasks by content      |
| `sessions` | CRDB sessions table               | summary                   | agent_id, repo                            | Find past session context  |
| `events`   | CRDB events table                 | payload (text)            | event_type, agent_id, repo                | Find events by content     |
| `specs`    | CRDB specs table + construct data | content, constructs, name | project, spec_type                        | Find spec constructs       |
| `skills`   | CRDB skills table                 | name, description, tags   | category, effectiveness_score             | Discover skills by context |
| `plans`    | CRDB plans table                  | title, description, tasks | project, phase, status                    | Find plans across projects |

**Configuration:**

```
MEILI_URL=http://meilisearch:7700
MEILI_API_KEY=<key>
```

If `MEILI_URL` is unset, search endpoints return 503. All other functionality is unaffected. This is the graceful degradation contract.

### 2.3 Gel DB: Graph-Relational Queries

Gel provides relationship traversal and computed properties that neither CRDB nor Meilisearch can serve efficiently.

**What it models:**

The Gel schema mirrors the relational data in CRDB as a graph:

```
Project -[has_phase]-> Phase -[has_plan]-> Plan -[has_task]-> Task
    |                                         |
    +--[has_requirement]-> Requirement <------+-- [implements]

Spec -[has_entity]-> Entity -[affected_by]-> Rule -[surfaces_on]-> Surface
    |                                                    |
    +--[imports]-> Spec (cross-spec references)          +--[demands]-> Contract

Skill -[depends_on]-> Skill
Agent -[assigned_to]-> Task
Agent -[invoked]-> Skill (with outcome)
```

**What Gel answers that SQL cannot (efficiently):**

- "What requirements are unmet across all projects?" (multi-hop: project -> phase -> plan -> task -> requirement, filtered by completion status)
- "What is the impact of changing entity X in spec Y?" (graph traversal: entity -> rules -> surfaces -> contracts, across imported specs)
- "What skills are reachable from skill Z through dependency chains?" (recursive traversal with depth control)
- "What is the full agent tree for this workflow?" (hierarchical: orchestrator -> dispatched agents -> their sub-tasks)
- "Which plans are blocked by incomplete dependencies?" (dependency graph with cycle detection)

**Configuration:**

```
GEL_DSN=gel://gel:5656/hive
```

If `GEL_DSN` is unset, graph-dependent endpoints return 404. Endpoints that can fall back to SQL do so transparently.

### 2.4 Consistency Model and Sync

**Source of truth:** CockroachDB, always.

**Write path:**

```
Agent -> hive-server API -> CockroachDB (sync, transactional)
                               |
                               +---> Meilisearch (async, best-effort)
                               |
                               +---> Gel DB (async, best-effort)
```

All writes go to CockroachDB synchronously. The API call succeeds or fails based on the CRDB write. Meilisearch and Gel indexing happens asynchronously after the CRDB write succeeds.

**Sync mechanism:**

A `SyncStore` wrapper sits in front of the CRDB store. On every successful write, it dispatches indexing jobs to a bounded worker pool (not unbounded goroutines):

```go
type SyncStore struct {
    primary  store.Store           // CockroachDB
    search   search.Searcher       // Meilisearch (may be nil)
    graph    graph.GraphStore      // Gel (may be nil)
    workers  *workerpool.Pool      // bounded goroutine pool
}

func (s *SyncStore) UpsertMemory(ctx context.Context, entry *model.MemoryEntry) (*model.MemoryEntry, error) {
    result, err := s.primary.UpsertMemory(ctx, entry)
    if err != nil {
        return nil, err
    }
    // Async: index in search and graph
    if s.search != nil {
        s.workers.Submit(func() { s.search.Index(context.Background(), "memories", toDoc(result)) })
    }
    if s.graph != nil {
        s.workers.Submit(func() { s.graph.SyncMemory(context.Background(), result) })
    }
    return result, nil
}
```

**Reconciliation:**

A periodic reconciliation job runs every 5 minutes:

1. Queries CRDB for records updated since last reconciliation timestamp
2. Re-indexes those records in Meilisearch
3. Re-syncs those records to Gel

A full re-index can be triggered manually via admin endpoint. Index swapping (Meilisearch feature) enables zero-downtime full rebuilds.

**Consistency guarantees:**

- CRDB: immediately consistent (serializable transactions)
- Meilisearch: eventually consistent, typically <1 second lag, worst case 5 minutes (reconciliation interval)
- Gel: eventually consistent, same guarantees as Meilisearch

**Failure modes:**

- Meilisearch down: search endpoints return 503, writes continue to CRDB, reconciliation catches up when Meilisearch recovers
- Gel down: graph endpoints return 404 or fall back to SQL, writes continue to CRDB, reconciliation catches up
- CRDB down: entire API returns 503 (nothing works without the primary store)

---

## 3. Skill Replacement Mapping

For each skill, this section maps every major capability to a hive-server API feature and shows what the MCP tool call looks like compared to the old slash command.

### 3.1 GSD Replacement

#### Project Initialization

**Old (GSD):** `/gsd:new-project` -- triggers interactive questions, spawns 4 parallel research agents, produces PROJECT.md, REQUIREMENTS.md, ROADMAP.md, STATE.md, config.json.

**New (hive-server):**

```
MCP tool: hive.planning.create_project
Input: {
    "name": "my-app",
    "repo": "christmas-island/my-app",
    "description": "REST API for widget management",
    "constraints": ["Must use Go", "PostgreSQL backend"],
    "research_topics": ["widget APIs", "Go REST frameworks", "PostgreSQL patterns"]
}
Response: {
    "project_id": "proj_abc123",
    "status": "researching",
    "research_tasks": [
        {"id": "task_r1", "topic": "widget APIs", "status": "open"},
        {"id": "task_r2", "topic": "Go REST frameworks", "status": "open"},
        {"id": "task_r3", "topic": "PostgreSQL patterns", "status": "open"}
    ]
}
```

The project creation stores the project metadata in CRDB, creates research tasks, and returns them for the orchestrating agent to dispatch sub-agents against. The sub-agents claim tasks, do research, store findings as memories and task notes, and mark tasks complete.

Research findings are stored in CRDB (structured), indexed in Meilisearch (searchable), and linked in Gel (project -> research task -> findings). Unlike GSD's `.planning/RESEARCH.md` files, findings are queryable across projects: "What did we learn about OAuth in any project?"

#### Phase Planning

**Old (GSD):** `/gsd:plan-phase 3` -- reads .planning/ files, creates PLAN.md files with XML task blocks.

**New (hive-server):**

```
MCP tool: hive.planning.create_phase
Input: {
    "project_id": "proj_abc123",
    "phase_number": 3,
    "name": "API Authentication",
    "goal": "Implement JWT-based authentication",
    "requirements": ["AUTH-01", "AUTH-02", "AUTH-03"]
}
Response: {
    "phase_id": "phase_xyz",
    "status": "planning",
    "linked_requirements": [
        {"id": "AUTH-01", "title": "User signup with email/password", "status": "open"},
        ...
    ]
}
```

```
MCP tool: hive.planning.create_plan
Input: {
    "phase_id": "phase_xyz",
    "title": "01-Implement login endpoint",
    "tasks": [
        {
            "title": "Create login handler",
            "files": ["internal/handlers/auth.go"],
            "action": "Implement POST /api/auth/login with bcrypt verification",
            "verify": "curl -X POST localhost:3000/api/auth/login returns 200",
            "done": "Login endpoint accepts email/password and returns JWT"
        },
        {
            "title": "Add JWT middleware",
            "files": ["internal/middleware/auth.go"],
            "action": "Create middleware that validates JWT Bearer tokens",
            "verify": "curl without token returns 401, with valid token returns 200",
            "done": "All /api/ routes are protected by JWT middleware"
        }
    ],
    "depends_on": []
}
Response: {
    "plan_id": "plan_001",
    "tasks": [
        {"id": "task_t1", "title": "Create login handler", "status": "open"},
        {"id": "task_t2", "title": "Add JWT middleware", "status": "open"}
    ]
}
```

Plans are stored with their full task structure, verification criteria, and dependency links. Unlike GSD's XML-in-markdown format, plans are structured data with real foreign key relationships to requirements, phases, and projects.

#### Wave-Based Execution

**Old (GSD):** The orchestrator reads PLAN.md files, analyzes dependencies, groups into waves, spawns sub-agents with `Task` tool.

**New (hive-server):**

```
MCP tool: hive.orchestration.schedule_wave
Input: {
    "phase_id": "phase_xyz"
}
Response: {
    "wave_number": 1,
    "plans": [
        {"plan_id": "plan_001", "title": "Implement login endpoint", "depends_on": [], "status": "ready"},
        {"plan_id": "plan_003", "title": "Create user model", "depends_on": [], "status": "ready"}
    ],
    "blocked_plans": [
        {"plan_id": "plan_002", "title": "Add OAuth2 support", "depends_on": ["plan_001"], "status": "blocked"}
    ]
}
```

The server computes the dependency graph (using Gel for efficiency) and returns the current wave -- plans with no unresolved dependencies. The orchestrating agent dispatches sub-agents for each ready plan. When plans complete, the next wave is computed.

```
MCP tool: hive.orchestration.claim_plan
Input: {
    "plan_id": "plan_001",
    "agent_id": "executor-01"
}
Response: {
    "plan_id": "plan_001",
    "status": "claimed",
    "assignee": "executor-01",
    "tasks": [
        {"id": "task_t1", "title": "Create login handler", "status": "open", ...},
        {"id": "task_t2", "title": "Add JWT middleware", "status": "open", ...}
    ]
}
```

#### STATE.md Replacement

**Old (GSD):** STATE.md is a 100-line markdown file updated by whichever agent last wrote to it. No validation, fragile parsing.

**New (hive-server):**

```
MCP tool: hive.planning.get_project_state
Input: {
    "project_id": "proj_abc123"
}
Response: {
    "project": "my-app",
    "current_phase": {"number": 3, "name": "API Authentication", "status": "in_progress"},
    "current_plan": {"number": 1, "title": "Implement login endpoint", "status": "in_progress"},
    "progress": {
        "phases_total": 6,
        "phases_complete": 2,
        "current_phase_plans_total": 3,
        "current_phase_plans_complete": 0,
        "overall_percentage": 33
    },
    "metrics": {
        "plans_completed": 8,
        "average_plan_duration_minutes": 14,
        "velocity_plans_per_day": 4.2
    },
    "recent_decisions": [
        {"key": "auth/jwt-config", "value": "JWT with bcrypt cost 12, 24h access tokens", "created_at": "2026-03-09T10:00:00Z"}
    ],
    "blockers": [
        {"task_id": "task_t5", "title": "OAuth2 scope requirements unclear", "status": "blocked"}
    ],
    "last_session": {
        "agent_id": "gsd-planner-01",
        "summary": "Planned phase 3, created 3 plans...",
        "completed_at": "2026-03-08T18:30:00Z"
    }
}
```

This is STATE.md as a queryable, validated, always-consistent API response. It is computed from real data (task statuses, event timestamps, session records), not maintained as a manually-updated string in a markdown file.

### 3.2 Superpowers Replacement

#### Skill Discovery

**Old (Superpowers):** `skills-core.js` scans directories for SKILL.md files, extracts YAML frontmatter, returns a catalog. All 14 skills are loaded into context on session start.

**New (hive-server):**

```
MCP tool: hive.skills.discover
Input: {
    "context": "I need to debug a race condition in a Go HTTP handler",
    "limit": 5
}
Response: {
    "skills": [
        {
            "id": "skill_sysdbg",
            "name": "systematic-debugging",
            "description": "Use when encountering bugs or unexpected behavior - systematic root cause investigation before any fix attempt",
            "category": "debugging",
            "effectiveness": {"success_rate": 0.87, "avg_duration_minutes": 18, "sample_size": 23},
            "relevance_score": 0.95
        },
        {
            "id": "skill_tdd",
            "name": "test-driven-development",
            "description": "Use when writing new code or fixing bugs - write tests first, then implementation",
            "category": "quality",
            "effectiveness": {"success_rate": 0.92, "avg_duration_minutes": 25, "sample_size": 41},
            "relevance_score": 0.72
        }
    ]
}
```

Skills are stored in CRDB (metadata, content, version history), indexed in Meilisearch (full-text search on description and content), and linked in Gel (skill dependency graph). Discovery uses Meilisearch for contextual matching -- the agent describes what it is doing, and the search engine returns relevant skills ranked by relevance and effectiveness.

Unlike Superpowers' static catalog (all 14 skills in context every session), hive-server provides dynamic discovery -- only relevant skills are returned, reducing token usage.

#### Brainstorm-Plan-Execute Pipeline

**Old (Superpowers):** `/brainstorm` loads brainstorming skill content into context. `/write-plan` loads planning skill. `/execute-plan` loads execution skill. Each is a separate slash command that loads a markdown prompt.

**New (hive-server):**

```
MCP tool: hive.planning.create_workflow
Input: {
    "type": "brainstorm-plan-execute",
    "title": "Add rate limiting to API",
    "repo": "christmas-island/hive-server"
}
Response: {
    "workflow_id": "wf_abc123",
    "stages": [
        {"id": "stage_1", "type": "brainstorm", "status": "active", "instructions": "..."},
        {"id": "stage_2", "type": "plan", "status": "pending", "depends_on": "stage_1"},
        {"id": "stage_3", "type": "execute", "status": "pending", "depends_on": "stage_2"},
        {"id": "stage_4", "type": "review", "status": "pending", "depends_on": "stage_3"},
        {"id": "stage_5", "type": "verify", "status": "pending", "depends_on": "stage_4"}
    ]
}
```

```
MCP tool: hive.planning.complete_stage
Input: {
    "stage_id": "stage_1",
    "output": {
        "ideas": ["Token bucket algorithm", "Sliding window", "Leaky bucket"],
        "decision": "Token bucket -- simplest, well-understood, good library support in Go",
        "rationale": "golang.org/x/time/rate implements token bucket. No external dependencies needed."
    }
}
Response: {
    "stage_id": "stage_1",
    "status": "complete",
    "next_stage": {
        "id": "stage_2",
        "type": "plan",
        "status": "active",
        "instructions": "Create an implementation plan based on the brainstorm output...",
        "context": { "brainstorm_output": { ... } }
    }
}
```

The workflow state is persistent. If the session ends mid-brainstorm, the next session retrieves the workflow and resumes at the current stage. Unlike Superpowers, where losing the session means starting over, hive-server preserves workflow progress across sessions.

#### Skill Effectiveness Tracking

**Old (Superpowers):** No tracking exists. No skill knows if it is effective.

**New (hive-server):**

```
MCP tool: hive.analytics.record_invocation
Input: {
    "skill_id": "skill_tdd",
    "workflow_id": "wf_abc123",
    "duration_seconds": 1500,
    "success": true,
    "repo": "christmas-island/hive-server",
    "notes": "Applied TDD to rate limiter implementation. 8 tests, all green."
}
```

```
MCP tool: hive.analytics.skill_effectiveness
Input: {
    "skill_id": "skill_tdd",
    "time_range": "30d"
}
Response: {
    "skill": "test-driven-development",
    "period": "last 30 days",
    "invocations": 41,
    "success_rate": 0.92,
    "avg_duration_seconds": 1500,
    "by_repo": {
        "christmas-island/hive-server": {"invocations": 15, "success_rate": 0.93},
        "christmas-island/hive-local": {"invocations": 8, "success_rate": 0.88}
    },
    "by_agent": {
        "executor-01": {"invocations": 20, "success_rate": 0.95},
        "executor-02": {"invocations": 21, "success_rate": 0.90}
    }
}
```

### 3.3 Allium Replacement

#### Spec Storage and Versioning

**Old (Allium):** Specs are `.allium` files on the filesystem. Versioning is git. No structured storage, no querying.

**New (hive-server):**

```
MCP tool: hive.specs.sync
Input: {
    "project": "christmas-island/hive-server",
    "spec_name": "auth",
    "version": "2",
    "raw_content": "-- allium: 2\n\nentity User {\n    email: String\n    ...\n}",
    "ast_json": { ... },  // Output from allium-cli, if available
    "constructs": [
        {"type": "entity", "name": "User", "fields": ["email", "password_hash", "status"]},
        {"type": "rule", "name": "RequestPasswordReset", "triggers": ["UserRequestsPasswordReset"], "entities": ["User", "ResetToken"]},
        {"type": "surface", "name": "LoginForm", "facing": "User", "exposes": ["email", "password"]}
    ]
}
Response: {
    "spec_id": "spec_auth",
    "version": 3,
    "constructs_indexed": 3,
    "searchable": true
}
```

Spec content is stored in CRDB. Constructs are indexed in Meilisearch for full-text discovery. Entity-rule-surface relationships are modeled in Gel for graph traversal.

#### Drift Detection

**Old (Allium):** Weed agent runs in a session, compares spec to code, reports mismatches. No persistence across sessions, no trend tracking.

**New (hive-server):**

```
MCP tool: hive.specs.submit_drift_report
Input: {
    "spec_id": "spec_auth",
    "agent_id": "allium-weed-01",
    "mismatches": [
        {"type": "code_bug", "construct": "PasswordReset", "detail": "Email template uses wrong subject line"},
        {"type": "aspirational", "construct": "LoginForm", "detail": "Rate limiting not yet implemented"},
        {"type": "spec_bug", "construct": "SessionTimeout", "detail": "Spec says 60min, config says 30min"}
    ],
    "summary": {
        "total_mismatches": 3,
        "by_type": {"code_bug": 1, "aspirational": 1, "spec_bug": 1}
    }
}
```

```
MCP tool: hive.specs.drift_trend
Input: {
    "spec_id": "spec_auth",
    "time_range": "30d"
}
Response: {
    "spec": "auth",
    "reports": 12,
    "trend": "improving",
    "mismatch_counts": [
        {"date": "2026-02-09", "total": 7, "code_bug": 3, "aspirational": 2, "spec_bug": 2},
        {"date": "2026-02-16", "total": 5, "code_bug": 2, "aspirational": 2, "spec_bug": 1},
        {"date": "2026-03-09", "total": 3, "code_bug": 1, "aspirational": 1, "spec_bug": 1}
    ]
}
```

#### Cross-Spec Impact Analysis (Gel)

**Old (Allium):** Not possible. No tool answers "which rules affect entity X across all specs."

**New (hive-server):**

```
MCP tool: hive.specs.impact_analysis
Input: {
    "entity": "User",
    "project": "christmas-island/hive-server"
}
Response: {
    "entity": "User",
    "affected_rules": [
        {"spec": "auth", "rule": "RequestPasswordReset", "relationship": "direct"},
        {"spec": "auth", "rule": "LoginAttempt", "relationship": "direct"},
        {"spec": "billing", "rule": "SubscriptionRenewal", "relationship": "via_import"}
    ],
    "surfaces": [
        {"spec": "auth", "surface": "LoginForm", "facing": "User"},
        {"spec": "admin", "surface": "UserManagement", "facing": "Admin"}
    ],
    "contracts_demanded": [
        {"spec": "auth", "contract": "PasswordHasher", "direction": "demands"}
    ]
}
```

This query traverses Gel's graph: User entity -> rules referencing User -> surfaces exposing User -> contracts demanded by those surfaces. It crosses spec boundaries through `use` imports. This is the type of multi-hop query that recursive CTEs handle awkwardly but Gel's path expressions handle naturally.

---

## 4. API Surface Design

### 4.1 API Prefix and Auth

All endpoints under `/api/v1/`. Auth via Bearer token (`HIVE_TOKEN` env var). If empty, auth disabled (local dev). Agent ID via `X-Agent-ID` header, injected into context by middleware.

Health probes (`/health`, `/ready`) are outside the API prefix, no auth.

### 4.2 Core Domain: Memory

Carries forward from existing API, extended with `repo`, `scope`, and `session_id`.

| Method | Path                    | Description                                                             |
| ------ | ----------------------- | ----------------------------------------------------------------------- |
| POST   | `/api/v1/memory`        | Create/update entry (upsert by key, optimistic concurrency via version) |
| GET    | `/api/v1/memory`        | List entries (query: tag, agent, prefix, repo, scope, limit, offset)    |
| GET    | `/api/v1/memory/{key}`  | Get single entry                                                        |
| DELETE | `/api/v1/memory/{key}`  | Delete entry                                                            |
| POST   | `/api/v1/memory/bulk`   | Bulk upsert (up to 100 entries)                                         |
| POST   | `/api/v1/memory/inject` | Context injection: extract terms, search, rank, trim to token budget    |

### 4.3 Core Domain: Tasks

Carries forward from existing API. Task status state machine: `open -> claimed -> in_progress -> done|failed|cancelled`.

| Method | Path                 | Description                                                                 |
| ------ | -------------------- | --------------------------------------------------------------------------- |
| POST   | `/api/v1/tasks`      | Create task                                                                 |
| GET    | `/api/v1/tasks`      | List tasks (query: status, assignee, creator, repo, project, limit, offset) |
| GET    | `/api/v1/tasks/{id}` | Get task with notes                                                         |
| PATCH  | `/api/v1/tasks/{id}` | Update status/assignee/append note                                          |
| DELETE | `/api/v1/tasks/{id}` | Delete task                                                                 |

### 4.4 Core Domain: Agents

Carries forward from existing API.

| Method | Path                            | Description           |
| ------ | ------------------------------- | --------------------- |
| POST   | `/api/v1/agents/{id}/heartbeat` | Register/update agent |
| GET    | `/api/v1/agents`                | List all agents       |
| GET    | `/api/v1/agents/{id}`           | Get agent by ID       |

### 4.5 Core Domain: Events

Append-only event stream. Cross-domain glue for analytics.

| Method | Path             | Description                                                          |
| ------ | ---------------- | -------------------------------------------------------------------- |
| POST   | `/api/v1/events` | Record event                                                         |
| GET    | `/api/v1/events` | List events (query: type, agent, session, repo, since, until, limit) |

### 4.6 Core Domain: Sessions

Session lifecycle and summaries.

| Method | Path                    | Description                               |
| ------ | ----------------------- | ----------------------------------------- |
| POST   | `/api/v1/sessions`      | Create session (returns session_id)       |
| GET    | `/api/v1/sessions`      | List sessions (query: agent, repo, limit) |
| GET    | `/api/v1/sessions/{id}` | Get session                               |
| PATCH  | `/api/v1/sessions/{id}` | Complete session (submit summary)         |

### 4.7 Planning Domain

Replaces GSD's project management workflow.

| Method | Path                                       | Description                                         |
| ------ | ------------------------------------------ | --------------------------------------------------- |
| POST   | `/api/v1/projects`                         | Create project with metadata, constraints           |
| GET    | `/api/v1/projects`                         | List projects                                       |
| GET    | `/api/v1/projects/{id}`                    | Get project with current state (replaces STATE.md)  |
| PATCH  | `/api/v1/projects/{id}`                    | Update project metadata                             |
| POST   | `/api/v1/projects/{id}/phases`             | Create phase linked to requirements                 |
| GET    | `/api/v1/projects/{id}/phases`             | List phases with status                             |
| PATCH  | `/api/v1/phases/{id}`                      | Update phase status                                 |
| POST   | `/api/v1/phases/{id}/plans`                | Create plan with tasks and dependencies             |
| GET    | `/api/v1/phases/{id}/plans`                | List plans in phase                                 |
| PATCH  | `/api/v1/plans/{id}`                       | Update plan status                                  |
| POST   | `/api/v1/projects/{id}/requirements`       | Create requirement                                  |
| GET    | `/api/v1/projects/{id}/requirements`       | List requirements (query: status, category)         |
| PATCH  | `/api/v1/requirements/{id}`                | Update requirement status                           |
| POST   | `/api/v1/workflows`                        | Create workflow (brainstorm-plan-execute or custom) |
| GET    | `/api/v1/workflows/{id}`                   | Get workflow with current stage                     |
| PATCH  | `/api/v1/workflows/{id}/stages/{stage_id}` | Complete stage, advance workflow                    |

### 4.8 Orchestration Domain

Replaces GSD's wave-based execution and Superpowers' agent dispatch.

| Method | Path                                | Description                                              |
| ------ | ----------------------------------- | -------------------------------------------------------- |
| POST   | `/api/v1/phases/{id}/waves`         | Compute next wave from dependency graph                  |
| GET    | `/api/v1/phases/{id}/waves/current` | Get current wave (ready plans)                           |
| POST   | `/api/v1/plans/{id}/claim`          | Claim plan for execution (atomic, prevents double-claim) |
| POST   | `/api/v1/plans/{id}/release`        | Release claimed plan                                     |
| POST   | `/api/v1/plans/{id}/complete`       | Mark plan complete with outcome                          |

### 4.9 Search Domain

Powered by Meilisearch. Returns 503 if Meilisearch unavailable.

| Method | Path                      | Description                         |
| ------ | ------------------------- | ----------------------------------- |
| POST   | `/api/v1/search`          | Federated search across all indexes |
| POST   | `/api/v1/search/memories` | Search memory entries               |
| POST   | `/api/v1/search/tasks`    | Search tasks                        |
| POST   | `/api/v1/search/sessions` | Search session summaries            |
| POST   | `/api/v1/search/specs`    | Search spec constructs              |
| POST   | `/api/v1/search/skills`   | Search skills by context            |
| POST   | `/api/v1/search/plans`    | Search plans across projects        |

### 4.10 Specs Domain

Replaces Allium's spec management.

| Method | Path                        | Description                                    |
| ------ | --------------------------- | ---------------------------------------------- |
| POST   | `/api/v1/specs`             | Sync spec (upsert with content and constructs) |
| GET    | `/api/v1/specs`             | List specs (query: project, name)              |
| GET    | `/api/v1/specs/{id}`        | Get spec metadata + construct summary          |
| POST   | `/api/v1/specs/{id}/drift`  | Submit drift report                            |
| GET    | `/api/v1/specs/{id}/drift`  | Get drift history and trend                    |
| GET    | `/api/v1/specs/{id}/impact` | Impact analysis via Gel graph traversal        |

### 4.11 Skills Domain

Replaces Superpowers' skill catalog.

| Method | Path                      | Description                                                    |
| ------ | ------------------------- | -------------------------------------------------------------- |
| POST   | `/api/v1/skills`          | Register/update skill (name, description, content, category)   |
| GET    | `/api/v1/skills`          | List skills (query: category, name)                            |
| GET    | `/api/v1/skills/{id}`     | Get skill with content and effectiveness metrics               |
| DELETE | `/api/v1/skills/{id}`     | Remove skill                                                   |
| POST   | `/api/v1/skills/discover` | Context-aware skill discovery (search + effectiveness ranking) |

### 4.12 Analytics Domain

Replaces GSD's velocity tracking, Superpowers' (nonexistent) effectiveness metrics.

| Method | Path                            | Description                                              |
| ------ | ------------------------------- | -------------------------------------------------------- |
| GET    | `/api/v1/analytics/velocity`    | Velocity metrics (query: project, time_range)            |
| GET    | `/api/v1/analytics/skills`      | Skill effectiveness aggregates                           |
| POST   | `/api/v1/analytics/invocations` | Record skill invocation outcome                          |
| GET    | `/api/v1/analytics/invocations` | List invocations (query: skill, agent, repo, time_range) |
| GET    | `/api/v1/analytics/drift`       | Cross-project drift summary                              |

### 4.13 Endpoint Count

| Domain        | Endpoints |
| ------------- | --------- |
| Memory        | 6         |
| Tasks         | 5         |
| Agents        | 3         |
| Events        | 2         |
| Sessions      | 4         |
| Planning      | 16        |
| Orchestration | 5         |
| Search        | 7         |
| Specs         | 6         |
| Skills        | 5         |
| Analytics     | 5         |
| Health        | 2         |
| **Total**     | **66**    |

This is a large API surface. It is designed to be built incrementally -- see Section 7 for phasing. The MCP plugin exposes a subset at any given phase.

---

## 5. Data Model

### 5.1 Core Entities (CockroachDB)

```sql
-- Core: Memory
CREATE TABLE memory (
    key         TEXT        NOT NULL PRIMARY KEY,
    value       TEXT        NOT NULL DEFAULT '',
    agent_id    TEXT        NOT NULL DEFAULT '',
    repo        TEXT        NOT NULL DEFAULT '',
    scope       TEXT        NOT NULL DEFAULT 'global',  -- 'private', 'project', 'global'
    session_id  TEXT        NOT NULL DEFAULT '',
    tags        JSONB       NOT NULL DEFAULT '[]'::JSONB,
    version     INT8        NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Core: Tasks
CREATE TABLE tasks (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    title       TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    status      TEXT        NOT NULL DEFAULT 'open',
    creator     TEXT        NOT NULL,
    assignee    TEXT        NOT NULL DEFAULT '',
    priority    INT4        NOT NULL DEFAULT 0,
    repo        TEXT        NOT NULL DEFAULT '',
    project_id  UUID        REFERENCES projects(id),
    plan_id     UUID        REFERENCES plans(id),
    tags        JSONB       NOT NULL DEFAULT '[]'::JSONB,
    metadata    JSONB       NOT NULL DEFAULT '{}'::JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Core: Task Notes
CREATE TABLE task_notes (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     UUID        NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    note        TEXT        NOT NULL,
    agent_id    TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Core: Agents
CREATE TABLE agents (
    id              TEXT        NOT NULL PRIMARY KEY,
    name            TEXT        NOT NULL DEFAULT '',
    status          TEXT        NOT NULL DEFAULT 'offline',
    capabilities    JSONB       NOT NULL DEFAULT '[]'::JSONB,
    model_tier      TEXT        NOT NULL DEFAULT '',  -- 'opus', 'sonnet', 'haiku'
    last_heartbeat  TIMESTAMPTZ NOT NULL DEFAULT now(),
    registered_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Core: Events
CREATE TABLE events (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type  TEXT        NOT NULL,
    agent_id    TEXT        NOT NULL DEFAULT '',
    session_id  TEXT        NOT NULL DEFAULT '',
    repo        TEXT        NOT NULL DEFAULT '',
    payload     JSONB       NOT NULL DEFAULT '{}'::JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Core: Sessions
CREATE TABLE sessions (
    id           UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id     TEXT        NOT NULL DEFAULT '',
    repo         TEXT        NOT NULL DEFAULT '',
    summary      TEXT        NOT NULL DEFAULT '',
    started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

-- Planning: Projects
CREATE TABLE projects (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT        NOT NULL,
    repo        TEXT        NOT NULL DEFAULT '',
    description TEXT        NOT NULL DEFAULT '',
    constraints JSONB       NOT NULL DEFAULT '[]'::JSONB,
    config      JSONB       NOT NULL DEFAULT '{}'::JSONB,
    status      TEXT        NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Planning: Phases
CREATE TABLE phases (
    id             UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    phase_number   INT4        NOT NULL,
    name           TEXT        NOT NULL,
    goal           TEXT        NOT NULL DEFAULT '',
    status         TEXT        NOT NULL DEFAULT 'pending',  -- pending, planning, executing, verifying, complete
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, phase_number)
);

-- Planning: Plans
CREATE TABLE plans (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    phase_id    UUID        NOT NULL REFERENCES phases(id) ON DELETE CASCADE,
    title       TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    status      TEXT        NOT NULL DEFAULT 'open',  -- open, claimed, in_progress, complete, failed
    assignee    TEXT        NOT NULL DEFAULT '',
    depends_on  JSONB       NOT NULL DEFAULT '[]'::JSONB,  -- array of plan UUIDs
    metadata    JSONB       NOT NULL DEFAULT '{}'::JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Planning: Requirements
CREATE TABLE requirements (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    code        TEXT        NOT NULL,  -- 'AUTH-01', 'DATA-03'
    title       TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    category    TEXT        NOT NULL DEFAULT '',
    status      TEXT        NOT NULL DEFAULT 'open',  -- open, in_progress, done, deferred
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, code)
);

-- Planning: Requirement-Plan linkage
CREATE TABLE plan_requirements (
    plan_id        UUID NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    requirement_id UUID NOT NULL REFERENCES requirements(id) ON DELETE CASCADE,
    PRIMARY KEY (plan_id, requirement_id)
);

-- Planning: Workflows
CREATE TABLE workflows (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    type        TEXT        NOT NULL,  -- 'brainstorm-plan-execute', 'custom'
    title       TEXT        NOT NULL,
    repo        TEXT        NOT NULL DEFAULT '',
    project_id  UUID        REFERENCES projects(id),
    status      TEXT        NOT NULL DEFAULT 'active',  -- active, complete, abandoned
    config      JSONB       NOT NULL DEFAULT '{}'::JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Planning: Workflow Stages
CREATE TABLE workflow_stages (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID        NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    type        TEXT        NOT NULL,  -- 'brainstorm', 'plan', 'execute', 'review', 'verify'
    status      TEXT        NOT NULL DEFAULT 'pending',  -- pending, active, complete, skipped
    stage_order INT4        NOT NULL,
    depends_on  UUID        REFERENCES workflow_stages(id),
    input       JSONB       NOT NULL DEFAULT '{}'::JSONB,
    output      JSONB       NOT NULL DEFAULT '{}'::JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

-- Specs: Spec metadata
CREATE TABLE specs (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    project     TEXT        NOT NULL,
    name        TEXT        NOT NULL,
    version     INT4        NOT NULL DEFAULT 1,
    raw_content TEXT        NOT NULL DEFAULT '',
    ast_json    JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project, name)
);

-- Specs: Constructs (parsed from spec)
CREATE TABLE spec_constructs (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    spec_id     UUID        NOT NULL REFERENCES specs(id) ON DELETE CASCADE,
    type        TEXT        NOT NULL,  -- 'entity', 'rule', 'surface', 'contract', 'invariant'
    name        TEXT        NOT NULL,
    fields      JSONB       NOT NULL DEFAULT '[]'::JSONB,
    metadata    JSONB       NOT NULL DEFAULT '{}'::JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Specs: Drift reports
CREATE TABLE drift_reports (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    spec_id     UUID        NOT NULL REFERENCES specs(id) ON DELETE CASCADE,
    agent_id    TEXT        NOT NULL DEFAULT '',
    mismatches  JSONB       NOT NULL DEFAULT '[]'::JSONB,
    summary     JSONB       NOT NULL DEFAULT '{}'::JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Skills: Skill catalog
CREATE TABLE skills (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT        NOT NULL UNIQUE,
    description TEXT        NOT NULL DEFAULT '',
    category    TEXT        NOT NULL DEFAULT '',  -- 'process', 'quality', 'debugging', 'collaboration', 'review', 'meta'
    content     TEXT        NOT NULL DEFAULT '',  -- full skill instructions
    tags        JSONB       NOT NULL DEFAULT '[]'::JSONB,
    config      JSONB       NOT NULL DEFAULT '{}'::JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Skills: Invocation records
CREATE TABLE skill_invocations (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    skill_id        UUID        NOT NULL REFERENCES skills(id),
    agent_id        TEXT        NOT NULL DEFAULT '',
    workflow_id     UUID        REFERENCES workflows(id),
    repo            TEXT        NOT NULL DEFAULT '',
    duration_seconds INT4       NOT NULL DEFAULT 0,
    success         BOOLEAN     NOT NULL DEFAULT true,
    notes           TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 5.2 Indexes

```sql
-- Core indexes
CREATE INDEX idx_memory_agent ON memory(agent_id);
CREATE INDEX idx_memory_repo ON memory(repo);
CREATE INDEX idx_memory_session ON memory(session_id);
CREATE INVERTED INDEX idx_memory_tags ON memory(tags);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_assignee ON tasks(assignee);
CREATE INDEX idx_tasks_creator ON tasks(creator);
CREATE INDEX idx_tasks_project ON tasks(project_id);
CREATE INDEX idx_task_notes_task ON task_notes(task_id);
CREATE INDEX idx_events_type ON events(event_type);
CREATE INDEX idx_events_agent ON events(agent_id);
CREATE INDEX idx_events_created ON events(created_at);
CREATE INDEX idx_sessions_agent ON sessions(agent_id);
CREATE INDEX idx_sessions_repo ON sessions(repo);

-- Planning indexes
CREATE INDEX idx_phases_project ON phases(project_id);
CREATE INDEX idx_plans_phase ON plans(phase_id);
CREATE INDEX idx_plans_status ON plans(status);
CREATE INDEX idx_requirements_project ON requirements(project_id);
CREATE INDEX idx_workflow_stages_workflow ON workflow_stages(workflow_id);

-- Specs indexes
CREATE INDEX idx_spec_constructs_spec ON spec_constructs(spec_id);
CREATE INDEX idx_spec_constructs_type ON spec_constructs(type);
CREATE INDEX idx_drift_reports_spec ON drift_reports(spec_id);
CREATE INDEX idx_drift_reports_created ON drift_reports(created_at);

-- Skills indexes
CREATE INDEX idx_skill_invocations_skill ON skill_invocations(skill_id);
CREATE INDEX idx_skill_invocations_agent ON skill_invocations(agent_id);
CREATE INDEX idx_skill_invocations_created ON skill_invocations(created_at);
```

### 5.3 Gel Schema (Graph Overlay)

```sdl
# Project management graph
type Project extending HasTimestamps {
    required name: str;
    required repo: str;
    multi phases: Phase;
    multi requirements: Requirement;
}

type Phase extending HasTimestamps {
    required project: Project;
    required phase_number: int32;
    required name: str;
    required status: str;
    multi plans: Plan;
}

type Plan extending HasTimestamps {
    required phase: Phase;
    required title: str;
    required status: str;
    multi tasks: Task;
    multi depends_on: Plan;       # dependency graph
    multi implements: Requirement; # traceability
}

type Task extending HasTimestamps {
    required title: str;
    required status: str;
    assignee: Agent;
    plan: Plan;
}

type Requirement extending HasTimestamps {
    required project: Project;
    required code: str;
    required title: str;
    required status: str;
    multi implemented_by := .<implements[is Plan];  # back-link
}

type Agent {
    required name: str;
    required status: str;
    multi assigned_tasks := .<assignee[is Task];  # back-link
}

# Spec graph
type Spec extending HasTimestamps {
    required project: str;
    required name: str;
    multi constructs: Construct;
    multi imports: Spec;           # cross-spec references
}

type Construct {
    required spec: Spec;
    required type: str;            # entity, rule, surface, contract, invariant
    required name: str;
    multi affects_entities: Construct;  # rule -> entity links
    multi surfaced_on: Construct;      # entity -> surface links
}

# Skill graph
type Skill extending HasTimestamps {
    required name: str;
    required category: str;
    multi depends_on: Skill;       # skill dependency chain
}

abstract type HasTimestamps {
    required created_at: datetime { default := datetime_current(); };
    required updated_at: datetime { default := datetime_current(); };
}
```

### 5.4 Entity Relationships

```
CRDB (source of truth)          Meilisearch (search index)       Gel (graph overlay)
========================        ==========================       ====================
memory -----------------------> memories index                   (not in graph)
tasks ------------------------> tasks index ------------------> Task type
task_notes                      (embedded in tasks index)        (not separate)
agents                          (not indexed)                    Agent type
events -----------------------> events index                    (not in graph)
sessions ---------------------> sessions index                  (not in graph)
projects                        (not indexed directly)           Project type
phases                          (not indexed directly)           Phase type
plans ------------------------> plans index ------------------> Plan type
requirements                    (not indexed directly)           Requirement type
plan_requirements               (not indexed)                    Plan.implements link
workflows                       (not indexed)                    (not in graph yet)
workflow_stages                  (not indexed)                    (not in graph yet)
specs ------------------------> specs index ------------------> Spec type
spec_constructs --------------->(embedded in specs index) -----> Construct type
drift_reports                   (not indexed)                    (not in graph)
skills -----------------------> skills index -----------------> Skill type
skill_invocations               (not indexed)                    (not in graph)
```

---

## 6. The MCP Contract

### 6.1 What the MCP Plugin Needs

The MCP plugin (hive-plugin / hive-local) is out of scope for this document. But hive-server's API must be designed so that a thin MCP plugin can expose capabilities as tools. Here is what the plugin needs from the server.

**Requirement 1: JSON-in, JSON-out.** Every endpoint accepts and returns JSON. No HTML, no XML, no multipart. The MCP plugin deserializes responses and passes them to the agent.

**Requirement 2: Self-describing errors.** Every error response includes the current state, why the operation failed, and what the agent should do next. See the devex analysis for the error format. The `recovery` field is mandatory for every error.

**Requirement 3: Stable tool interface.** The MCP plugin maps tool names to API endpoints. The mapping is:

```
hive.memory.set        -> POST   /api/v1/memory
hive.memory.get        -> GET    /api/v1/memory/{key}
hive.memory.list       -> GET    /api/v1/memory
hive.memory.delete     -> DELETE /api/v1/memory/{key}
hive.memory.inject     -> POST   /api/v1/memory/inject
hive.task.create       -> POST   /api/v1/tasks
hive.task.list         -> GET    /api/v1/tasks
hive.task.get          -> GET    /api/v1/tasks/{id}
hive.task.update       -> PATCH  /api/v1/tasks/{id}
hive.task.delete       -> DELETE /api/v1/tasks/{id}
hive.agent.heartbeat   -> POST   /api/v1/agents/{id}/heartbeat
hive.agent.list        -> GET    /api/v1/agents
hive.planning.*        -> /api/v1/projects/*, /api/v1/phases/*, /api/v1/plans/*
hive.orchestration.*   -> /api/v1/phases/*/waves, /api/v1/plans/*/claim|release|complete
hive.search.*          -> POST   /api/v1/search/*
hive.specs.*           -> /api/v1/specs/*
hive.skills.*          -> /api/v1/skills/*
hive.analytics.*       -> /api/v1/analytics/*
hive.session.*         -> /api/v1/sessions/*
hive.event.record      -> POST   /api/v1/events
```

**Requirement 4: OpenAPI spec.** Huma v2 generates the OpenAPI spec automatically. The MCP plugin can use this spec for input validation and documentation. The spec is available at `/api/v1/openapi.json`.

**Requirement 5: Pagination and limits.** All list endpoints support `limit` and `offset`. Default limit is 20. Maximum limit is 100. The MCP plugin enforces this to prevent token explosion.

**Requirement 6: Token-efficient responses.** Responses should be compact. No verbose wrapper objects. No redundant fields. The MCP plugin passes responses directly to the agent, and every byte is tokens.

### 6.2 What the Server Side Must Guarantee

1. **Idempotent writes where possible.** Memory upsert is idempotent by key. Task status transitions are guarded by state machine. Plan claiming is atomic (no double-claim).

2. **Consistent state after every operation.** The agent should never see an inconsistent state from CRDB. If a plan is marked complete, all its tasks are complete. If a phase has no incomplete plans, it can transition to complete.

3. **Graceful degradation.** Search endpoints return 503 when Meilisearch is down. Graph endpoints return 404 when Gel is down. Core CRUD (memory, tasks, agents) always works if CRDB is up.

4. **Low latency.** All CRDB-only operations complete in <50ms. Search operations complete in <200ms. Graph operations complete in <500ms. These are p99 targets, not averages.

---

## 7. Phasing

### 7.1 Phase 0: Foundation (Week 1-2)

**Goal:** CockroachDB store implementation, core schema, existing API endpoints working against CRDB.

This phase resolves open GitHub issues: #12 (migrate to CRDB), #16 (Huma v2 -- already done), #18 (tx retries), #20 (project layout).

**Steps:**

1. Extract `internal/model/` package from `internal/store/` -- move all data types and sentinel errors
2. Split Store interface into domain-specific interfaces (MemoryStore, TaskStore, AgentStore) per the architect's design
3. Implement CRDBStore using pgx/v5 with crdbpgx transaction retry wrapper
4. Add goose migration framework, create initial migration from the core schema (memory, tasks, task_notes, agents)
5. Add `Ping()` method to Store interface, wire into `/ready` endpoint
6. Add `repo` and `session_id` columns to memory and tasks tables
7. Configuration: `DATABASE_URL` env var selects CRDB backend
8. Update all tests to run against both SQLite (for fast unit tests) and CRDB (for integration tests via `cockroach-go/v2/testserver`)

**Output:** hive-server running against CockroachDB in production, SQLite retained for local dev and fast tests.

### 7.2 Phase 1: Events, Sessions, and Schema Extensions (Week 2-3)

**Goal:** The new core tables that everything else depends on.

**Steps:**

1. Add events table migration and EventStore interface + implementation
2. Add sessions table migration and SessionStore interface + implementation
3. Add POST/GET endpoints for events and sessions
4. Add error messages with recovery guidance (touch ~15 error sites)
5. Improve startup logging (show configured backends, connection status)

**Output:** Agents can record events, submit session summaries, and associate work with sessions. The cross-session memory foundation exists.

### 7.3 Phase 2: Meilisearch Integration (Weeks 3-5)

**Goal:** Full-text search across all stored content.

**Steps:**

1. Define Searcher interface with NoopSearcher fallback
2. Implement MeiliSearcher backend
3. Implement SyncStore wrapper with bounded worker pool for async indexing
4. Create Meilisearch indexes: memories, tasks, sessions, events
5. Implement search endpoints (POST /api/v1/search/\*)
6. Implement query preprocessing (keyword extraction, 10-word limit handling)
7. Implement reconciliation job (periodic re-index from CRDB)
8. Implement POST /api/v1/memory/inject (context injection endpoint)

**Output:** Agents can search across accumulated memories, sessions, and tasks. Memory injection returns relevant context per prompt.

### 7.4 Phase 3: Planning and Orchestration (Weeks 5-8)

**Goal:** Replace GSD's core workflow with API-backed planning.

**Steps:**

1. Add projects, phases, plans, requirements, plan_requirements tables
2. Add workflows, workflow_stages tables
3. Implement planning endpoints (projects, phases, plans, requirements)
4. Implement workflow endpoints (create, advance stages)
5. Implement wave computation (analyze plan dependencies, return execution order)
6. Implement plan claim/release/complete with atomic state transitions
7. Add plans index to Meilisearch
8. Record events for all planning state transitions

**Output:** Full project planning workflow available via API. Agents can create projects, plan phases, schedule waves, claim and execute plans, and track requirements.

### 7.5 Phase 4: Skills and Specs (Weeks 8-11)

**Goal:** Replace Superpowers' skill system and Allium's spec management.

**Steps:**

1. Add skills and skill_invocations tables
2. Implement skills CRUD and discovery endpoints
3. Add skills index to Meilisearch with effectiveness-weighted ranking
4. Implement invocation recording and effectiveness analytics
5. Add specs, spec_constructs, drift_reports tables
6. Implement spec sync and drift report endpoints
7. Add specs index to Meilisearch
8. Implement drift trend computation

**Output:** Skills are stored, discoverable, and tracked for effectiveness. Specs are stored, searchable, and drift is tracked over time.

### 7.6 Phase 5: Gel DB Integration (Weeks 11-14)

**Goal:** Graph queries for relationships, dependencies, and impact analysis.

**Steps:**

1. Define GraphStore interface with NoopGraphStore fallback
2. Deploy Gel alongside CRDB and Meilisearch
3. Create Gel schema (SDL) mirroring the CRDB relational model
4. Implement GelStore backend using gel-go
5. Add graph sync to SyncStore wrapper
6. Implement graph-dependent endpoints:
   - Project state with full dependency graph
   - Cross-project requirement traceability
   - Spec impact analysis (entity -> rules -> surfaces)
   - Skill dependency traversal
   - Blocked plan detection with cycle analysis
7. Add reconciliation for Gel (same pattern as Meilisearch)

**Output:** Complex relationship queries work. Cross-project visibility, impact analysis, and dependency graph navigation are available.

### 7.7 Phase 6: Analytics and Hardening (Weeks 14-16)

**Goal:** Analytics endpoints, operational hardening, production readiness.

**Steps:**

1. Implement velocity analytics (computed from events and task completion data)
2. Implement skill effectiveness dashboards
3. Implement cross-project drift summary
4. Add request body size limits, rate limiting, field size validation
5. Add request logging/audit middleware
6. Add Prometheus metrics endpoint
7. Implement CRDB backup procedures
8. Load testing and performance profiling
9. Update OpenAPI spec documentation

**Output:** Full analytics suite. Production-hardened API with monitoring, rate limiting, and backup procedures.

### 7.8 Phase Summary

```
Phase 0 (wk 1-2):  [CRDB] ---- hive-server ---- [NoopSearcher] [NoopGraph]
                     core schema, existing API

Phase 1 (wk 2-3):  [CRDB] ---- hive-server ---- [NoopSearcher] [NoopGraph]
                     + events, sessions

Phase 2 (wk 3-5):  [CRDB] ---- hive-server ---- [Meilisearch]  [NoopGraph]
                     + search, memory injection

Phase 3 (wk 5-8):  [CRDB] ---- hive-server ---- [Meilisearch]  [NoopGraph]
                     + planning, orchestration, workflows

Phase 4 (wk 8-11): [CRDB] ---- hive-server ---- [Meilisearch]  [NoopGraph]
                     + skills, specs, drift tracking

Phase 5 (wk 11-14):[CRDB] ---- hive-server ---- [Meilisearch]  [Gel DB]
                     + graph queries, impact analysis

Phase 6 (wk 14-16):[CRDB] ---- hive-server ---- [Meilisearch]  [Gel DB]
                     + analytics, hardening, monitoring
```

---

## 8. What This Does NOT Do

### 8.1 Explicitly Out of Scope

**The MCP plugin.** This document designs the server-side API. The MCP plugin (hive-plugin), the local proxy (hive-local), and the tool registration in Claude Code are separate projects. The API is designed to be consumed by a thin MCP plugin, but the plugin itself is not specified here.

**Prompt engineering.** GSD's value is not just the workflow -- it is the carefully crafted prompts that guide the LLM through each phase. Superpowers' value is the skill instructions that prevent common failure modes. Hive-server stores skill content and workflow definitions, but the quality of those prompts is a content problem, not an API problem. The initial skill and workflow content must be migrated from the existing skills.

**Real-time agent coordination.** Agents communicate through hive-server by reading and writing state. There is no WebSocket, no pub/sub, no LISTEN/NOTIFY. Agents poll for state changes. If an orchestrator needs to know when a sub-agent finishes, it polls the task status. This is sufficient for the current fire-and-forget dispatch model. Real-time coordination (event streaming, reactive updates) is deferred until polling proves inadequate.

**LLM synthesis (MasterClaw).** The original vision included an in-cluster OpenClaw instance for LLM-powered synthesis of search results and intelligent task assignment. This is deferred indefinitely. The search ranking from Meilisearch, combined with simple recency weighting and effectiveness scoring, is sufficient. If synthesis is needed later, it can be added as a function call to an external LLM API, not as an infrastructure component.

**Multi-tenancy beyond repo scoping.** Data is scoped by `repo` and `agent_id`. There is no user authentication, no team isolation, no organization hierarchy. The `HIVE_TOKEN` bearer auth is a shared secret. Multi-tenancy at the user/team level is a future concern.

**File storage.** Hive-server stores structured data, not files. It does not replace git. Plans, research findings, and spec content are stored as text fields, not as file uploads. If an agent needs to store a file, it stores a reference (path, URL, commit SHA) in a memory entry.

**CI/CD integration.** Hive-server does not trigger builds, run tests, or deploy code. It provides data (project state, requirement status, drift reports) that CI/CD systems could consume, but it does not integrate with them directly.

**Self-hosted embeddings / vector search.** No GPU. No embedding generation. Meilisearch provides keyword + typo-tolerant search. If hybrid/semantic search is needed later, Meilisearch supports external embedders without requiring self-hosted GPU infrastructure.

### 8.2 Boundary with the Skills

The skills (GSD, Superpowers, Allium) become **content**, not **dependencies**:

- GSD's agent definitions (researcher, planner, executor, verifier, debugger) become skill records in hive-server, with their prompt content stored in the `skills.content` field
- Superpowers' skill catalog (14 skills) becomes skill records in hive-server
- Allium's spec language remains external -- hive-server stores and queries specs but does not parse the Allium language. Parsing is done by allium-cli (external tool) or by the LLM reading the raw content.

The skills' npm packages, shell scripts, and plugin manifests are no longer needed. An agent using hive-server gets planning, skill discovery, and spec management through the MCP plugin, backed by persistent queryable state.

### 8.3 Boundary with Infrastructure

Hive-server assumes its databases exist and are reachable. It does not:

- Provision CockroachDB clusters
- Deploy Meilisearch instances
- Manage Gel server lifecycle
- Handle TLS certificate rotation
- Perform database backups (though it should -- see Phase 6)

Infrastructure provisioning is managed by the k8s repo and ops tooling, not by hive-server itself.

---

## 9. Open Questions

1. **Workflow stage instructions.** When the MCP plugin asks hive-server for the next workflow stage, should the response include full prompt instructions for the agent? If so, where do those instructions come from? They need to be stored as templates -- is this the `skills.content` field or a separate template system?

2. **Skill content migration.** The existing skills have carefully written content (GSD's 12 agent definitions, Superpowers' 14 SKILL.md files, Allium's Tend/Weed agent prompts). How is this content migrated into hive-server's skills table? Manual entry? Automated import? Who maintains it?

3. **Session end detection.** Claude Code does not have a clean "session end" lifecycle event. How does the MCP plugin know when to submit a session summary? Options: (a) the agent explicitly calls `hive.session.complete`, (b) a hook fires on session end/clear/compact, (c) the session is implicitly completed when a new session starts for the same agent.

4. **Data quality validation.** Should hive-server validate the quality of stored content? Reject empty session summaries? Require minimum lengths for memory values? Or accept anything and let data quality be the agent's responsibility?

5. **Gel schema evolution.** When the CRDB schema changes, the Gel schema must change too. How are these kept in sync? Manual parallel migrations? Automated generation of Gel SDL from CRDB DDL?

---

## Sources

This vision synthesizes findings from:

- hive-server-current.md (current codebase state)
- github-issues.md (open issues and locked decisions)
- vision-v2.md (prior vision, superseded)
- synthesis.md (9-permutation database analysis)
- final-review.md (cross-document quality review)
- gsd.md, superpowers.md, allium.md (skill analysis)
- gel-db.md, cockroachdb.md, meilisearch.md (database analysis)
- ultrathink-architect.md, ultrathink-skeptic.md, ultrathink-devex.md, ultrathink-ops.md (perspective analysis)
