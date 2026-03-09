# Hive-Server Vision v2: Skill-Aware Agent Infrastructure

**Date:** 2026-03-09
**Status:** Revised vision integrating synthesis findings and skill ecosystem analysis
**Supersedes:** vision.md (original unified architecture vision)
**Inputs:** vision.md, synthesis.md, build-plan.md, review.md, gsd.md, superpowers.md, allium.md

---

## 1. What Changed

### 1.1 The Original Vision

The original vision positioned hive-server as three things: a tool abstraction API (fanning out to Gel+Meilisearch+CRDB), a memory injection system, and an LLM-enabled project manager. It assumed CockroachDB as the production store from day one, Gel DB for knowledge graphs, Meilisearch for search, and MasterClaw (in-cluster OpenClaw) for LLM synthesis.

### 1.2 What the Skills Revealed

Studying GSD, Superpowers, and Allium exposed the actual consumers of hive-server's APIs. Three findings reshape the architecture:

**Finding 1: All three skills are stateless systems generating stateful artifacts.** GSD stores everything as markdown in `.planning/`. Superpowers has no persistence at all. Allium uses flat `.allium` files. Every skill independently invented its own fragile persistence layer because no shared infrastructure existed. hive-server does not need to be a sophisticated multi-database system to deliver massive value -- it needs to be a reliable state store that skills can actually use.

**Finding 2: The five universal gaps are more urgent than graph traversal.** Cross-session memory, cross-project visibility, structured querying, agent coordination, and historical analytics are the gaps every skill shares. These are all solvable with SQLite + Meilisearch. Gel DB's graph capabilities address secondary concerns (multi-hop traversal, computed properties) that become relevant only at scale.

**Finding 3: Skills already have planning systems.** GSD has a complete project management workflow (phases, plans, waves, verification). Superpowers has brainstorm-plan-execute pipelines. hive-server's "LLM-enabled project manager" risks competing with these existing systems rather than enhancing them. The right role for hive-server is to provide durable state and cross-session memory that makes these skill-internal planning systems more effective.

### 1.3 What the Synthesis Confirmed

The synthesis of all nine permutation analyses converged on a clear recommendation: **start with SQLite + Meilisearch, defer Gel and CRDB until justified by specific, tangible needs**. The value-per-complexity ratio of Meilisearch is the highest of any single addition. CockroachDB solves scaling problems that do not exist yet. Gel DB provides elegant graph queries whose value depends on accumulated data that does not yet exist.

This directly contradicts the original build plan's Phase 3B (CockroachDB migration as an early priority) and the locked design decision #5 from ops#82 (CockroachDB for production). The revised vision adopts the synthesis recommendation and defers CRDB.

---

## 2. Revised Architecture

### 2.1 Architecture Principles

1. **Skills are first-class consumers.** API design starts from "what do GSD, Superpowers, and Allium need?" not "what can our databases do?"
2. **SQLite until proven insufficient.** The single-writer limitation of SQLite is not a problem for a solo developer with a few agents. When it becomes one, migrate to PostgreSQL or CockroachDB.
3. **Meilisearch is the first new backend.** It addresses the #1 gap (cross-session memory retrieval) across all three skills with the lowest infrastructure cost.
4. **Gel and CRDB are earned, not assumed.** Each requires a demonstrated query that cannot be served by SQLite, measured in developer hours saved vs infrastructure hours spent.
5. **Enhance skills, do not replace them.** hive-server provides durable state and search. Skills keep their own planning, orchestration, and workflow logic.

### 2.2 Revised Component Diagram

```
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
                 |         (Go, chi/Huma v2 API)              |
                 |                                            |
                 |  +----------+  +---------+  +----------+  |
                 |  | Shared   |  | Skill   |  | Memory   |  |
                 |  | Core API |  | APIs    |  | Injector |  |
                 |  +----+-----+  +----+----+  +----+-----+  |
                 +-------+-----------+-----------+------------+
                         |           |           |
                    +----+----+ +----+----+ +----+-----+
                    | SQLite  | | SQLite  | | Meili    |
                    | (state) | | (events)| | (search) |
                    +---------+ +---------+ +----------+
```

Phase 0-2: SQLite is the only required backend. Meilisearch is the first optional addition. No Gel, no CRDB, no MasterClaw.

### 2.3 What Was Removed (For Now)

| Original Component | Status                               | Rationale                                                                                                                                    |
| ------------------ | ------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------- |
| CockroachDB        | Deferred to Phase 4 (at scale)       | Distributed SQL solves scaling problems that do not exist at solo-developer scale. SQLite handles the concurrency.                           |
| Gel DB             | Deferred to Phase 3 (when justified) | Graph traversal is valuable but not urgent. No skill currently needs multi-hop queries that justify 1 GB RAM + PostgreSQL.                   |
| MasterClaw         | Deferred indefinitely                | LLM synthesis can be added when the simpler system proves insufficient. Skills already have their own orchestration. OpenClaw CVEs add risk. |
| Query Router       | Simplified to direct dispatch        | With one primary store (SQLite) and one search index (Meilisearch), routing logic is trivial. No fan-out-synthesize pattern needed yet.      |

### 2.4 What Was Added

| New Component                 | Purpose                                                                                                                                |
| ----------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| Events table                  | Append-only cross-skill event stream. Every skill emits events through a shared mechanism, enabling analytics without schema coupling. |
| Skill-specific API namespaces | `/api/v1/gsd/`, `/api/v1/superpowers/`, `/api/v1/specs/` for skill-specific structured state.                                          |
| Session summaries             | First-class endpoint for agents to submit session summaries, searchable across sessions via Meilisearch.                               |

---

## 3. Skill-Aware API Design

### 3.1 The Design Question

Should hive-server expose separate endpoints per skill, a unified abstraction, or both?

**Answer: Shared core + skill-specific extensions.** The synthesis identified a small set of entities common to all skills (agents, sessions, projects, events, memory) and skill-specific entities that do not generalize (GSD phases, Superpowers skill invocations, Allium spec constructs). Forcing these into a unified schema produces over-abstraction. Keeping them entirely separate prevents cross-skill analytics.

### 3.2 Shared Core API (All Skills Use)

These endpoints exist today or are natural extensions of the current API:

```
# Memory (exists today)
POST   /api/v1/memory              Create/update entry
GET    /api/v1/memory              List entries
GET    /api/v1/memory/{key}        Get entry
DELETE /api/v1/memory/{key}        Delete entry

# Tasks (exists today)
POST   /api/v1/tasks               Create task
GET    /api/v1/tasks               List tasks
GET    /api/v1/tasks/{id}          Get task
PATCH  /api/v1/tasks/{id}          Update task
DELETE /api/v1/tasks/{id}          Delete task

# Agents (exists today)
POST   /api/v1/agents/{id}/heartbeat
GET    /api/v1/agents
GET    /api/v1/agents/{id}

# Search (new, requires Meilisearch)
POST   /api/v1/search              Federated search across indexes
POST   /api/v1/search/memories     Search memory entries
POST   /api/v1/search/tasks        Search tasks
POST   /api/v1/search/sessions     Search session summaries
POST   /api/v1/search/artifacts    Search skill artifacts

# Events (new)
POST   /api/v1/events              Record event
GET    /api/v1/events              List events (filters: type, agent, session, repo)

# Sessions (new)
POST   /api/v1/sessions            Submit session summary
GET    /api/v1/sessions            List sessions
GET    /api/v1/sessions/{id}       Get session
```

### 3.3 GSD-Specific API

GSD's planning workflow generates structured state (projects, phases, plans, tasks, requirements) that benefits from durable storage. hive-server does not replace `.planning/` files -- it provides a structured mirror that enables querying and cross-project visibility.

```
POST   /api/v1/gsd/projects                    Register project
GET    /api/v1/gsd/projects                    List projects
GET    /api/v1/gsd/projects/{name}             Get project with phases
PATCH  /api/v1/gsd/projects/{name}             Update project state
POST   /api/v1/gsd/projects/{name}/phases      Record phase transition
GET    /api/v1/gsd/projects/{name}/velocity     Velocity from events
POST   /api/v1/gsd/requirements                Store requirement
GET    /api/v1/gsd/requirements                List (filters: status, category, project)
```

**Source-of-truth model:** The `.planning/` files remain the source of truth for GSD. hive-server stores a structured representation synced from those files. GSD's orchestrator can push state updates to hive-server after each phase transition, plan completion, or requirement status change. This is Option C from the synthesis (hybrid: structured data in database, prose in files).

### 3.4 Superpowers-Specific API

Superpowers' most urgent gap is cross-session memory. The second is skill effectiveness tracking.

```
POST   /api/v1/superpowers/invocations         Record skill invocation
GET    /api/v1/superpowers/invocations         List invocations
GET    /api/v1/superpowers/skills/effectiveness Aggregate effectiveness metrics
POST   /api/v1/superpowers/workflows           Record workflow execution
GET    /api/v1/superpowers/workflows           List workflows (filters: status, repo)
```

**Data model:** A skill invocation record captures: skill name, duration, success/failure, agent_id, session_id, repo, timestamp. Effectiveness is computed by aggregation (success rate, average duration per skill). This is the "SQLite table with 4 columns" approach the synthesis recommended as the 80/20 solution.

### 3.5 Allium-Specific API

Allium's primary value from hive-server is spec search and drift tracking.

```
POST   /api/v1/specs/sync                      Ingest parsed AST JSON
GET    /api/v1/specs                            List specs
GET    /api/v1/specs/{name}                     Get spec metadata + constructs
POST   /api/v1/specs/{name}/drift               Submit drift report
GET    /api/v1/specs/{name}/drift               Get drift history
```

**Integration pattern:** `allium-cli` produces a typed AST as JSON. hive-server stores this JSON and indexes its contents in Meilisearch. "Find all constructs related to password reset" becomes a search query against the `specs` index. Full graph traversal of entity-rule-surface relationships is deferred until Gel DB is added (Phase 3).

### 3.6 Why Not One Unified Abstraction

The synthesis explicitly answered this: "Can a single schema work across all three skills? No. The data models are too different." GSD's model is project management. Superpowers' model is workflow execution. Allium's model is behavioral specification. A generic `{type, name, metadata}` abstraction would require every consumer to parse unstructured metadata blobs, eliminating the benefit of structured storage.

The shared event stream (`POST /api/v1/events`) is the correct unification point. Every skill emits events through the same mechanism. Cross-skill analytics operate on events, not on skill-specific tables.

---

## 4. Memory Injection Evolution

### 4.1 Original Design

The original vision described a memory injection pipeline that fans out to Meilisearch + Gel + CRDB, synthesizes via MasterClaw, and returns ranked context blocks within a token budget.

### 4.2 Revised Design

Without Gel and MasterClaw, the injection pipeline simplifies:

```
Agent receives message
    |
    v
hive-local intercepts (pre-prompt hook)
    |
    v
POST /api/v1/memory/inject
    Body: { agent_id, prompt_text, session_id, repo, max_tokens }
    |
    v
hive-server:
    1. Extract key terms from prompt_text (keyword extraction)
    2. Query Meilisearch: search memories + sessions + artifacts
    3. Query SQLite: active tasks for this agent, recent events
    4. Rank by relevance (Meilisearch score + recency)
    5. Trim to max_tokens budget
    |
    v
Response: {
    context_blocks: [
        { type: "memory", content: "..." },
        { type: "task", content: "Active: Fix arm64 CI (#47)" },
        { type: "session", content: "Yesterday: debugged race condition in store.go" }
    ],
    tokens_used: 847
}
```

### 4.3 Skill-Specific Injection Context

Different skills benefit from different injected context:

| Skill           | Most Valuable Injected Context                                                                                                                            |
| --------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **GSD**         | Active project state (current phase, blockers), prior research findings from similar projects, requirement status, velocity metrics                       |
| **Superpowers** | Prior solutions to similar problems (cross-session memory), skill effectiveness data ("TDD skill had 92% success rate on Go projects"), active plan tasks |
| **Allium**      | Related spec constructs, prior drift reports, cross-spec references ("OAuth contract used in 3 other specs")                                              |

The `repo` field in the injection request provides scoping context. hive-server can use it to prioritize memories and sessions from the same project, while still surfacing relevant cross-project context.

### 4.4 When MasterClaw Adds Value

The synthesis step (LLM ranking/summarization of raw results) becomes valuable when:

- The number of candidate context blocks exceeds what simple relevance scoring can handle (50+)
- Cross-skill context needs to be merged intelligently (GSD project state + Superpowers skill data for the same repo)
- The agent's current task requires context from multiple unrelated prior sessions

Until then, Meilisearch's built-in relevance scoring combined with recency weighting provides adequate ranking.

---

## 5. Project Manager Evolution

### 5.1 The Tension

The original vision described an "LLM-Enabled Project Manager" that decomposes tasks, assigns them intelligently, and monitors progress. GSD already has exactly this: a complete project management system with phases, plans, wave-based execution, and a full agent hierarchy (orchestrator, researcher, planner, executor, verifier, debugger).

Building an LLM project manager in hive-server would create two competing coordination systems. GSD's orchestrator would manage phases and plans, while hive-server's MasterClaw would manage tasks and assignments, and they would need to agree on state.

### 5.2 The Resolution

hive-server should **not** be a project manager. It should be the durable state layer that makes GSD's (and other skills') project management more effective.

| Capability               | GSD Owns                               | hive-server Provides                                            |
| ------------------------ | -------------------------------------- | --------------------------------------------------------------- |
| Task decomposition       | Planner agent creates PLAN.md files    | Stores plan metadata for cross-session retrieval                |
| Task assignment          | Orchestrator spawns sub-agents by type | Tracks agent availability and capabilities for smarter dispatch |
| Progress monitoring      | STATE.md + ROADMAP.md checkboxes       | Structured event stream for velocity analytics                  |
| Failure handling         | Debugger agent with systematic process | Cross-session memory of prior fixes for similar failures        |
| Cross-project visibility | None (single-project scoped)           | Query projects, phases, requirements across all GSD projects    |

The single most valuable thing hive-server provides to GSD is **cross-project visibility**. Today, each GSD project is an island. "What requirements are incomplete across all my projects?" is unanswerable without reading every project's `.planning/` directory. hive-server makes this a query.

### 5.3 What Replaces MasterClaw

For task intelligence that goes beyond mechanical state management:

- **Short-term:** Skills continue using their own orchestration logic. hive-server provides data (active tasks, agent capabilities, historical performance).
- **Medium-term:** hive-local can optionally call an LLM API directly for synthesis/ranking, without the overhead of an in-cluster OpenClaw deployment. This is a function call, not an infrastructure component.
- **Long-term:** If multi-agent coordination at scale justifies it, revisit MasterClaw. But the trigger is "multiple teams of agents need centralized coordination," not "we want LLM-powered task assignment."

---

## 6. Updated Phasing

### 6.1 Phase 0: Foundation (Now, 1-2 weeks)

**Goal:** Prepare the codebase for skill integrations. Zero new dependencies.

Steps (from existing GitHub issues):

- Remove `k8s/` directory (#10)
- Adopt scripts-to-rule-them-all (#11)
- Refactor project layout (#20) -- add `internal/model/`, `internal/server/`, `internal/search/`, `internal/events/`
- Formalize Store interface with `Ping()` method
- Add Huma v2 API framework (#16)
- E2E smoke test scaffold (#17)

**New additions to Phase 0:**

- Add `events` table to SQLite schema:

  ```sql
  CREATE TABLE IF NOT EXISTS events (
      id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
      event_type TEXT NOT NULL,
      agent_id TEXT NOT NULL DEFAULT '',
      session_id TEXT NOT NULL DEFAULT '',
      repo TEXT NOT NULL DEFAULT '',
      payload TEXT NOT NULL DEFAULT '{}',
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
  );
  ```

- Add `sessions` table to SQLite schema:

  ```sql
  CREATE TABLE IF NOT EXISTS sessions (
      id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
      agent_id TEXT NOT NULL DEFAULT '',
      repo TEXT NOT NULL DEFAULT '',
      summary TEXT NOT NULL DEFAULT '',
      started_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
      completed_at TEXT
  );
  ```

- Define `Searcher` interface with `NoopSearcher` implementation
- Reserve API namespace structure: `/api/v1/search/`, `/api/v1/gsd/`, `/api/v1/superpowers/`, `/api/v1/specs/`

**Output:** hive-server with Huma v2 OpenAPI, events and sessions tables, interface extension points for search. Still SQLite-only.

### 6.2 Phase 1: Meilisearch Integration (Weeks 3-4)

**Goal:** Cross-session search across all skill artifacts. The single highest-value addition.

Steps:

- Implement `MeiliSearcher` backend for the `Searcher` interface
- Create indexes: `memories`, `tasks`, `sessions`, `artifacts`, `specs`
- Implement `SyncStore` wrapper (writes to SQLite, async-indexes in Meilisearch)
- Add search endpoints: `POST /api/v1/search/*`
- Implement reconciliation job (periodic full re-index)
- Add `POST /api/v1/memory/inject` endpoint (simplified injection without MasterClaw)

**Index settings:**

| Index       | Searchable               | Filterable                          | Sortable                 |
| ----------- | ------------------------ | ----------------------------------- | ------------------------ |
| `memories`  | content, tags            | agent_id, repo, scope               | created_at, updated_at   |
| `tasks`     | title, description, tags | status, assignee, creator, priority | created_at, priority     |
| `sessions`  | summary                  | agent_id, repo                      | started_at, completed_at |
| `artifacts` | content, title, tags     | agent_id, repo, skill, type         | created_at               |
| `specs`     | constructs, content      | project, spec_name, construct_type  | updated_at               |

**Configuration:** `MEILI_URL` and `MEILI_API_KEY` env vars. If unset, `NoopSearcher` is used. Graceful degradation: search endpoints return 503, all other functionality unaffected.

**Output:** Agents can search across all accumulated memories, session summaries, and skill artifacts. Memory injection returns relevant context per prompt. The cross-session memory gap is closed.

### 6.3 Phase 2: Skill-Specific APIs (Weeks 5-8)

**Goal:** Structured state storage for each skill's domain model, backed by SQLite.

**GSD endpoints:**

- `POST /api/v1/gsd/projects` -- register project, store initial state
- `GET /api/v1/gsd/projects` -- list all projects with current phase/status
- `GET /api/v1/gsd/projects/{name}` -- project detail with phases, requirements
- `PATCH /api/v1/gsd/projects/{name}` -- update state (phase transitions, etc.)
- `GET /api/v1/gsd/projects/{name}/velocity` -- computed from events

**Superpowers endpoints:**

- `POST /api/v1/superpowers/invocations` -- record skill invocation outcome
- `GET /api/v1/superpowers/invocations` -- list (filters: skill, repo, agent)
- `GET /api/v1/superpowers/skills/effectiveness` -- aggregated metrics

**Allium endpoints:**

- `POST /api/v1/specs/sync` -- ingest allium-cli JSON AST
- `GET /api/v1/specs/{name}` -- spec metadata and construct summary
- `POST /api/v1/specs/{name}/drift` -- submit drift report
- `GET /api/v1/specs/{name}/drift` -- drift history

**Shared endpoints:**

- `POST /api/v1/events` -- record event (used by all skills)
- `GET /api/v1/events` -- list events (filters: type, agent, session, repo)
- `POST /api/v1/sessions` -- submit session summary
- `GET /api/v1/sessions` -- list sessions

All backed by SQLite. Skill-specific tables use the `gsd_`, `sp_`, `al_` prefix convention from the synthesis. Events are the cross-skill glue.

**Output:** Each skill has durable structured state. Cross-project queries are possible. Historical analytics become feasible.

### 6.4 Phase 3: Gel DB (When Justified)

**Trigger conditions -- do not start this phase until at least one is true:**

- Managing 3+ GSD projects simultaneously and needing cross-project requirement traceability that SQLite joins cannot serve
- Allium spec corpus exceeds 10+ files across 2+ projects and cross-spec impact analysis (entity -> rule -> surface traversal) is needed
- Superpowers skill catalog exceeds 50+ skills and the dependency graph needs to be queryable

**What Gel adds over SQLite:**

- Bidirectional links (back-references without application logic)
- Multi-hop path traversal without recursive CTEs
- Computed properties (always-correct derived values)
- Schema-enforced structural constraints

**Implementation:** Add `GelStore` as an optional `GraphStore` interface implementation. SQLite remains source of truth. Gel is a materialized graph view synced from SQLite events. Handler layer checks for nil `GraphStore` and skips graph features when unavailable.

### 6.5 Phase 4: CockroachDB (At Scale Only)

**Trigger conditions -- do not start this phase until at least one is true:**

- Multiple hive-server instances needed for availability or throughput
- Agents are distributed across machines or regions
- SQLite's single-writer is a measured bottleneck (not a theoretical one)

**Implementation:** `CRDBStore` implements the same `Store` interface as `SQLiteStore`. Configuration selects the backend. All transaction paths use `crdbpgx.ExecuteTx()`. The existing test suite runs against both backends.

### 6.6 Phase Summary

```
Phase 0 (now):      [SQLite] ---- hive-server ---- [NoopSearcher]
                                     |
                              events + sessions tables

Phase 1 (week 3):   [SQLite] ---- hive-server ---- [Meilisearch]
                                     |
                              memory injection + search

Phase 2 (week 5):   [SQLite] ---- hive-server ---- [Meilisearch]
                                     |
                              GSD + Superpowers + Allium APIs

Phase 3 (when needed): [SQLite] -- hive-server ---- [Meilisearch]
                                       |
                                    [Gel DB]

Phase 4 (at scale):    [CRDB] ---- hive-server ---- [Meilisearch]
                                       |
                                    [Gel DB]
```

---

## 7. Tension Points

### 7.1 Synthesis vs. Original Vision: Where They Disagree

| Topic                       | Original Vision                                                    | Synthesis Recommendation                                                      | Resolution                                                                                                                                                                                                           |
| --------------------------- | ------------------------------------------------------------------ | ----------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **CockroachDB timing**      | Phase 3B, early priority (locked decision #5)                      | Defer until scale demands it                                                  | **Adopt synthesis.** The locked decision was made before skill analysis revealed that SQLite handles current scale. Revisit when scaling is needed.                                                                  |
| **MasterClaw**              | Core component for synthesis, task intelligence, injection ranking | Not recommended until simpler approaches prove insufficient                   | **Adopt synthesis.** MasterClaw adds OpenClaw dependency (with CVEs), infrastructure complexity, and cost. Skills already have orchestration.                                                                        |
| **Gel DB timing**           | Phase 4B, parallel with Meilisearch and MasterClaw                 | Defer until graph queries are demonstrably needed                             | **Adopt synthesis.** Gel is the most natural fit for all three skills' data models, but the infrastructure cost (1 GB RAM, PostgreSQL backend) is not justified until accumulated data makes graph queries valuable. |
| **Project manager**         | LLM-powered task decomposition, assignment, monitoring             | Skills already have planning systems; hive-server should enhance, not replace | **Adopt synthesis.** Building a competing project manager creates confusion about who owns task lifecycle.                                                                                                           |
| **Query Router complexity** | Fan-out to 3+ backends, merge, synthesize via LLM                  | Start with direct dispatch to SQLite + Meilisearch                            | **Adopt synthesis.** The fan-out-synthesize pattern is elegant but premature. Two backends do not need a router.                                                                                                     |

### 7.2 Where They Agree

| Topic                          | Both Documents Say                                                                                                             |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------ |
| **Meilisearch first**          | Highest value-per-complexity addition. Addresses the #1 gap (cross-session memory) across all skills.                          |
| **Graceful degradation**       | Every backend beyond SQLite is optional. Features degrade, system stays operational.                                           |
| **Interface-first design**     | `Store`, `Searcher`, (future) `GraphStore` interfaces enable backend swaps and test mocking.                                   |
| **Single-tool pattern**        | Agents interact with one `hive` tool. hive-local translates subcommands. Non-negotiable for token efficiency.                  |
| **No self-hosted embeddings**  | No GPU. Keyword + typo-tolerant search via Meilisearch is sufficient. Hybrid search via external embedder is the escape hatch. |
| **Source-of-truth discipline** | One primary store (SQLite now, CRDB later). Everything else is a secondary index.                                              |

### 7.3 Tension with Locked Decisions

**Locked Decision #5 (CockroachDB for production)** is the biggest tension. The original ops#82 decision assumed CockroachDB was necessary for production. The synthesis demonstrates it is not necessary at the current scale and adds significant complexity (transaction retry logic, connection pools, cluster management, licensing). The revised vision treats this as a scaling decision, not a foundational one.

**Recommendation:** Update ops#82 to acknowledge that the CRDB decision is scale-dependent, not absolute. SQLite is the production store until demonstrated otherwise. The `Store` interface makes the swap mechanical when needed.

---

## 8. The Actual Value Proposition

### 8.1 What No Individual Skill or Database Provides

**GSD** gives you structured project management within a single project. It cannot search across projects, remember across sessions, or track what worked historically.

**Superpowers** gives you disciplined development workflows. It cannot remember that you debugged a similar race condition last week, or that the TDD skill works better on Go projects than Python ones.

**Allium** gives you behavioral specifications. It cannot tell you which specs are drifting most, which rules affect a given entity across all specs, or what changed since last month.

**SQLite** gives you durable structured storage. It cannot do full-text search, typo-tolerant queries, or relevance ranking.

**Meilisearch** gives you fast, fuzzy search. It cannot enforce referential integrity, run transactions, or serve as a source of truth.

**hive-server's unique value is the integration layer.** It is the single service that:

1. **Provides cross-session memory for stateless skills.** GSD's orchestrator starts a new session and immediately knows what happened in the last session -- what worked, what failed, what decisions were made -- because hive-server stored and can retrieve it. This is the most impactful capability.

2. **Enables cross-project visibility.** "What requirements are incomplete across all my GSD projects?" or "Which Allium specs are drifting?" or "What is my overall velocity this week?" -- questions that are currently unanswerable without reading dozens of files by hand.

3. **Provides structured queryability over unstructured skill artifacts.** Instead of grepping `.planning/` files, skills can query hive-server for tasks by status, requirements by category, specs by construct type. The data is structured and indexed.

4. **Builds an institutional memory that compounds over time.** Every skill invocation, every session summary, every project outcome is recorded. After 100 sessions, hive-server can tell you which approaches work for which types of problems. After 1000, it becomes a genuine knowledge base. No individual skill accumulates this.

5. **Decouples persistence from skill implementation.** Skills remain simple, installable, zero-infrastructure systems. The infrastructure is optional and additive. `npx get-shit-done-cc@latest` still works without hive-server. But with hive-server, GSD becomes aware of its own history.

### 8.2 The Minimum Valuable System

The minimum system that delivers this value is:

- **hive-server** with SQLite (exists today, needs schema extensions)
- **Meilisearch** as a single binary beside hive-server (~50 MB RAM)
- **Skill-specific API endpoints** for GSD, Superpowers, and Allium
- **Memory injection endpoint** that returns relevant context per prompt

Total infrastructure: two processes, ~200 MB RAM combined. This is the target for Phase 0 + Phase 1.

### 8.3 What Success Looks Like

hive-server is successful when:

- A GSD orchestrator starting a new phase can see what research was done in prior phases across all projects
- A Superpowers agent debugging an issue can find that a similar issue was solved three sessions ago and how
- An Allium Weed agent running drift detection can see whether drift is increasing or decreasing over time
- A developer can ask "what is happening across all my projects?" and get an answer in under a second
- All of this works with `hive-server serve` and optionally `meilisearch` -- no cluster, no PostgreSQL, no OpenClaw

---

## 9. Implementation Priorities

### 9.1 What to Build First (Phase 0)

In priority order:

1. Events and sessions tables in SQLite
2. Huma v2 migration (enables OpenAPI, benefits all subsequent work)
3. `Searcher` interface with `NoopSearcher`
4. Events and sessions endpoints

### 9.2 What to Build Second (Phase 1)

1. Meilisearch integration (`MeiliSearcher` implementation)
2. `SyncStore` wrapper for async indexing
3. Search endpoints
4. Memory injection endpoint

### 9.3 What to Build Third (Phase 2)

1. GSD project/phase/requirement endpoints
2. Superpowers invocation recording endpoints
3. Allium spec sync and drift endpoints
4. Cross-skill analytics via events

### 9.4 What to Build Only When Needed

- Gel DB integration (Phase 3): when graph queries are demonstrably needed
- CockroachDB migration (Phase 4): when scaling demands it
- MasterClaw / LLM synthesis: when simple ranking proves insufficient
- Real-time agent coordination: when fire-and-forget is proven inadequate

### 9.5 The Decision Framework

When someone asks "should we add X?":

1. **Is there a specific query we cannot answer today?** If yes, which backend answers it?
2. **Is the workaround more than 30 seconds of effort?** If no, do not add it.
3. **How many times per day would this be used?** If less than once, it is not worth a new dependency.
4. **Can we start collecting data in SQLite/events now and add the fancy database later?** Usually yes. Do that.
5. **Does this enhance an existing skill or compete with it?** If it competes, reconsider.
