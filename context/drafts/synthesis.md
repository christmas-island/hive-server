# Synthesis: 9 Permutations, 3 Skills, 3 Databases

**Date:** 2026-03-09
**Status:** Decision-support document
**Sources:** All 9 permutation analyses, 3 skill briefs, 3 database technology briefs

---

## Table of Contents

1. [Cross-Cutting Patterns](#1-cross-cutting-patterns)
2. [Database Role Clarity](#2-database-role-clarity)
3. [Complementary vs Redundant](#3-complementary-vs-redundant)
4. [Skill-Specific Recommendations](#4-skill-specific-recommendations)
5. [Unified Data Model](#5-unified-data-model)
6. [Integration Architecture](#6-integration-architecture)
7. [Scale-Dependent Decisions](#7-scale-dependent-decisions)
8. [Tradeoff Matrix](#8-tradeoff-matrix)
9. [What NOT to Build](#9-what-not-to-build)
10. [Recommended Architecture](#10-recommended-architecture)

---

## 1. Cross-Cutting Patterns

### 1.1 Every Skill Has the Same Core Problem

All three skills -- GSD, Superpowers, and Allium -- share a single architectural deficiency: **they are stateless systems generating stateful artifacts**. The pattern repeats identically across all nine analyses:

| Skill       | State Today                    | What Is Lost                                                                    |
| ----------- | ------------------------------ | ------------------------------------------------------------------------------- |
| GSD         | Markdown files in `.planning/` | No querying, no cross-project visibility, no concurrent access, fragile parsing |
| Superpowers | Context window + filesystem    | No cross-session memory, no skill effectiveness tracking, no agent coordination |
| Allium      | Flat `.allium` files           | No structural querying, no cross-spec impact analysis, no drift trend tracking  |

The critical observation is that all three skills already have implicit data models. GSD has projects, phases, plans, tasks, and requirements. Superpowers has skills, workflows, stages, tasks, and agents. Allium has specs, entities, rules, surfaces, contracts, and actors. These models are encoded as formatting conventions in markdown or custom syntax, enforced by nothing, and queryable only by an LLM reading prose.

### 1.2 The Five Universal Gaps

Every permutation analysis identifies the same five capability gaps, regardless of which skill or database is being discussed:

1. **Cross-session memory**: No skill remembers anything between sessions. Every agent starts from zero.
2. **Cross-project visibility**: Each project is an island. "What is happening across all my projects?" is unanswerable.
3. **Structured querying**: "Which requirements are unmet?" or "Which rules affect entity X?" requires grep or LLM interpretation instead of a database query.
4. **Agent coordination**: Parallel agents cannot communicate during execution. It is fire-and-forget everywhere.
5. **Historical analytics**: No skill tracks what worked, what failed, how long things took, or which approaches were effective.

### 1.3 The Source-of-Truth Tension

Every permutation analysis confronts the same architectural question: **who owns the data?**

Three options appear in every document:

| Option                   | Description                                            | Appears In                           |
| ------------------------ | ------------------------------------------------------ | ------------------------------------ |
| **A: Database is truth** | All writes go to the database. Files are generated.    | GSD-Gel, Superpowers-Gel, Allium-Gel |
| **B: Files are truth**   | Skills keep writing files. Database is a read replica. | GSD-CRDB, GSD-Meili, Allium-Meili    |
| **C: Hybrid**            | Structured data in database, prose in files.           | Recommended everywhere               |

Every analysis recommends Option C as the starting point. The reasoning is consistent: structured data (statuses, IDs, dependency graphs, metrics) benefits enormously from database storage, while prose content (research findings, action instructions, spec bodies) is better left in files where LLMs can read it directly.

### 1.4 hive-server as Universal Mediator

All nine analyses position hive-server identically: it sits between agents and databases, translating skill-specific operations into database operations. No skill talks to any database directly. This is not negotiable -- it is the only pattern that works across all three skills and all three databases without coupling skills to specific database technologies.

### 1.5 The Complexity Tax Is Real

Every analysis includes a "What Is Lost" section, and they all say the same thing: **simplicity**. GSD's selling point is `npx install` and you have markdown files. Superpowers' selling point is zero infrastructure. Allium's selling point is "just flat files." Adding any database to any skill destroys the zero-infrastructure advantage. This is the central tension, and no clever architecture eliminates it.

---

## 2. Database Role Clarity

### 2.1 Gel DB: The Graph Navigator

**Natural strength:** Modeling and traversing relationships between entities.

Gel shines brightest when the data has rich, multi-hop relationships that need to be queried. Across all three skills:

| Skill           | Gel's Natural Fit                                                         | Why It Works                                                                                                                                                                   |
| --------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **GSD**         | Project -> Milestone -> Phase -> Plan -> Task -> Requirement traceability | GSD's implicit data model is a graph. Requirement traceability, dependency chains, and agent hierarchies are multi-hop traversals. EdgeQL path expressions make these trivial. |
| **Superpowers** | Skill dependency graph, agent dispatch trees, task dependency DAGs        | Skills reference skills, agents spawn agents, tasks block tasks. These are recursive structures that SQL handles awkwardly.                                                    |
| **Allium**      | Entity -> Rule -> Surface -> Contract -> Invariant behavioral graph       | Allium's constructs form a directed graph of behavioral dependencies. "What rules affect entity X?" is a back-link traversal.                                                  |

**Where Gel is forced:** Gel adds a 1 GB RAM server process and a PostgreSQL backend. For simple CRUD (list tasks, update status), this is heavy artillery for a small target. Its Go client is less mature than the TypeScript client. Schema migrations add a CI pipeline step.

**Honest assessment:** Gel is the most natural fit for all three skills' data models, but it is also the heaviest single dependency. If you are only doing one database, Gel gives you the most capability per integration, but at the highest infrastructure cost.

### 2.2 CockroachDB: The Distributed Workhorse

**Natural strength:** ACID transactions at scale, distributed coordination, PostgreSQL-compatible SQL.

CockroachDB addresses a different problem than Gel: not "how do I model relationships?" but "how do I coordinate multiple agents/users/instances writing concurrently?"

| Skill           | CRDB's Natural Fit                                                                               | Why It Works                                                                                                                                                |
| --------------- | ------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **GSD**         | Concurrent multi-agent writes, distributed phase execution, event sourcing for state transitions | GSD's wave-based parallel execution needs transactional guarantees. Multiple executors updating task status simultaneously requires serializable isolation. |
| **Superpowers** | Multi-agent task claiming, distributed locking for parallel investigations, cross-session state  | The "no interference" rule for parallel agents can be enforced with SELECT FOR UPDATE. Session state survives node failures.                                |
| **Allium**      | Transactional spec updates, event-sourced spec evolution, runtime trace storage                  | Spec modifications that touch multiple constructs need atomic transactions. Trace validation generates time-series data.                                    |

**Where CRDB is forced:** Modeling the graph relationships that all three skills naturally have. Recursive CTEs work but are verbose and slow compared to EdgeQL path expressions. The transaction retry requirement adds complexity to every write path. And at the scale hive-server actually operates at (one developer, a few agents), distributed SQL is solving a problem that does not exist yet.

**Honest assessment:** CRDB is the right choice if you need to scale hive-server to multiple instances serving many users. For a solo developer or small team, it is over-engineered. Its real value emerges at 10+ concurrent agents or multi-region deployment -- scenarios that are aspirational, not current.

### 2.3 Meilisearch: The Search Layer

**Natural strength:** Fast, typo-tolerant full-text search with faceting.

Meilisearch addresses a capability that neither Gel nor CRDB provides: finding things by content similarity rather than by structured query.

| Skill           | Meilisearch's Natural Fit                                                                                              | Why It Works                                                                                                            |
| --------------- | ---------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| **GSD**         | Searching across accumulated research, plans, summaries, and decisions. "What did we learn about auth in any project?" | GSD generates dozens of markdown files per project. They become write-only knowledge without search.                    |
| **Superpowers** | Skill discovery by natural language, cross-session memory retrieval, finding prior solutions to similar problems       | Superpowers loads all 14 skills into context today. At 100+ skills, search-based discovery is the only viable approach. |
| **Allium**      | Finding specs by content, cross-project spec discovery, impact analysis by searching references                        | "Find all constructs related to password reset" is a search problem, not a graph traversal.                             |

**Where Meilisearch is forced:** Anything that requires relational integrity, joins, transactions, or structured graph traversal. Meilisearch is a search engine, not a database. It cannot answer "which rules affect entity X" with precision -- it can only return documents that contain the word "X" somewhere in their text. The 10-word query limit conflicts with how LLMs naturally construct queries.

**Honest assessment:** Meilisearch is the lightest dependency (single binary, ~50 MB RAM), provides the most immediately visible value (instant search over accumulated knowledge), and has the lowest integration risk (it is purely additive -- nothing breaks if it goes down). It is the best first addition.

---

## 3. Complementary vs Redundant

### 3.1 The Natural Division of Labor

The three databases have remarkably little overlap when used for their strengths:

```
                Structured          Relationship          Content
                State CRUD          Traversal             Search
                     |                   |                   |
                     v                   v                   v
               +-----------+      +-----------+      +-----------+
               |           |      |           |      |           |
               |  CRDB     |      |   Gel     |      |  Meili    |
               |           |      |           |      |           |
               | Tasks     |      | Graphs    |      | Documents |
               | Sessions  |      | Links     |      | Artifacts |
               | Events    |      | Paths     |      | History   |
               | Metrics   |      | Computed  |      | Skills    |
               |           |      | props     |      |           |
               +-----------+      +-----------+      +-----------+
```

**Meilisearch** handles text search, typo tolerance, faceted discovery. No other database does this.

**Gel** handles graph traversal, computed properties, bidirectional links. CRDB can approximate this with recursive CTEs, but at 5-10x the query complexity.

**CRDB** handles distributed transactions, horizontal scaling, event sourcing. Gel can do transactions (via PostgreSQL), but not distributed ones. Meilisearch cannot do transactions at all.

### 3.2 Where They Overlap (Redundancy)

The overlap is in basic CRUD operations:

| Operation            | Gel | CRDB | Meilisearch            |
| -------------------- | --- | ---- | ---------------------- |
| Store a task record  | Yes | Yes  | Yes (as document)      |
| Update task status   | Yes | Yes  | Yes (replace document) |
| List tasks by status | Yes | Yes  | Yes (filter)           |
| Store agent metadata | Yes | Yes  | Yes (as document)      |

All three can serve as a basic task/memory store. This is where the "build everything" trap lives. If you use all three, you must decide which one is the source of truth for each piece of data, and maintain sync between them.

### 3.3 The Two-Database Sweet Spot

The analyses consistently suggest that **two databases** is the optimal configuration:

| Combination            | What You Get                                                                           | What You Lose                                                   |
| ---------------------- | -------------------------------------------------------------------------------------- | --------------------------------------------------------------- |
| **Gel + Meilisearch**  | Graph queries + text search. Gel is source of truth. Meilisearch indexes from Gel.     | No distributed scaling. Single-node only.                       |
| **CRDB + Meilisearch** | Distributed SQL + text search. CRDB is source of truth. Meilisearch indexes from CRDB. | No graph traversal (recursive CTEs only). More verbose queries. |
| **Gel + CRDB**         | Graph queries + distributed scaling.                                                   | No text search. Must add Meilisearch later anyway for search.   |

The third combination (Gel + CRDB) is the least useful because it doubles the relational database infrastructure without adding search, which is the most immediately valuable new capability.

### 3.4 Verdict: Complement, Not Compete

The databases complement each other cleanly **if** you use each for its strength and do not try to make any single one do everything. The anti-pattern is using Meilisearch as a database, using CRDB for graph queries, or using Gel for text search. Each of those works technically but poorly.

---

## 4. Skill-Specific Recommendations

### 4.1 GSD: Structured State + Search

**Best combination:** Gel + Meilisearch (or SQLite + Meilisearch at small scale)

**Why:** GSD's core problem is that its data model is a graph encoded in markdown. The requirement-to-task traceability chain (Project -> Milestone -> Phase -> Plan -> Task -> Requirement) is a natural fit for Gel's link system. Meanwhile, the accumulated research, plans, and summaries across phases are write-only knowledge that needs search.

**Minimum viable database set:** SQLite (already exists in hive-server) + Meilisearch.

SQLite handles the structured CRUD that GSD needs today (task status, phase transitions, requirement tracking). Meilisearch handles search across accumulated documents. This avoids adding Gel until the graph queries are actually needed.

**When to add Gel:** When cross-project visibility becomes a real need (multiple projects managed simultaneously), or when dependency cycle detection and requirement gap analysis justify the infrastructure.

**When to add CRDB:** When multiple users/teams need concurrent write access to the same GSD project state. This is a scaling concern, not a feature concern.

### 4.2 Superpowers: Memory + Analytics

**Best combination:** SQLite + Meilisearch (expanding to Gel for analytics)

**Why:** Superpowers' most urgent gap is cross-session memory. An agent that debugged a race condition yesterday should know that today. Meilisearch provides this: index session summaries, search for prior solutions. The second gap is skill effectiveness tracking, which is a structured data problem suitable for SQLite or Gel.

**Minimum viable database set:** SQLite + Meilisearch.

SQLite stores skill invocation records, workflow stages, and task outcomes. Meilisearch indexes session logs and workflow artifacts for cross-session retrieval.

**When to add Gel:** When the skill dependency graph and agent dispatch tree need to be queried (e.g., "find skills reachable from brainstorming" or "what is the full agent tree for this workflow"). This becomes relevant at 50+ skills or when multi-agent coordination moves beyond fire-and-forget.

**When to add CRDB:** When Superpowers is used by a team with multiple agents running concurrently against shared state. This is currently not how Superpowers works.

### 4.3 Allium: Behavioral Graph + Spec Search

**Best combination:** Gel + Meilisearch

**Why:** Allium's constructs form a behavioral graph (entities, rules, surfaces, contracts, invariants). Gel's schema maps to this graph almost 1:1. The cross-spec impact analysis ("which rules affect entity X?") is a graph traversal. Meanwhile, finding specs by content ("password reset") is a text search problem.

**Minimum viable database set:** Meilisearch alone.

At small scale (1-5 spec files), just loading all specs into context works. The first valuable addition is search: index spec constructs in Meilisearch, let agents find relevant specs without loading everything.

**When to add Gel:** When the spec corpus grows large enough that cross-spec structural queries are needed (10+ specs, 2+ projects). The Gel schema for Allium's AST is the most technically elegant of all nine permutations, but it is also the most infrastructure for the least current need.

**When to add CRDB:** Almost never. Allium specs are read-mostly, single-writer artifacts. The distributed transaction capabilities of CRDB are not relevant here.

---

## 5. Unified Data Model

### 5.1 Can a Single Schema Work Across All Three Skills?

No. The data models are too different. GSD's model is project management (phases, plans, tasks, requirements). Superpowers' model is workflow execution (skills, stages, invocations, agents). Allium's model is behavioral specification (entities, rules, surfaces, contracts). Forcing these into a shared schema would produce either an over-abstracted generic model that serves no skill well, or a bloated schema with three separate sub-models that happen to share a database.

### 5.2 Common Entities Worth Sharing

While the schemas differ, a small set of entities appear across all three skills:

| Entity            | GSD                                | Superpowers                               | Allium                       | Shared Definition                                |
| ----------------- | ---------------------------------- | ----------------------------------------- | ---------------------------- | ------------------------------------------------ |
| **Agent**         | Executor, verifier, debugger, etc. | Orchestrator, implementer, reviewer, etc. | Tend, Weed agents            | `{id, name, role, model_tier, session_id}`       |
| **Session**       | Implicit (STATE.md)                | AgentSession                              | Implicit                     | `{id, agent_id, repo, started_at, completed_at}` |
| **Task**          | Plan tasks with outcomes           | PlanTasks with status                     | N/A (specs are not tasks)    | `{id, title, status, outcome, duration}`         |
| **Project/Repo**  | Project with milestones            | Workflow with repo                        | Spec with project            | `{id, name, repo_url}`                           |
| **Outcome/Event** | Task completion, phase transitions | Stage outcomes, skill invocations         | Drift reports, spec versions | `{id, type, timestamp, payload}`                 |

### 5.3 Recommended Approach: Shared Core + Skill-Specific Extensions

hive-server should maintain a small shared core schema for entities that all skills reference (agents, sessions, projects), plus skill-specific schema extensions:

```
Shared Core (hive-server owns)
  - agents: {id, name, status, capabilities, last_heartbeat}
  - sessions: {id, agent_id, repo, started_at, completed_at, summary}
  - memory: {key, value, agent_id, tags}  [already exists]
  - tasks: {id, title, status, creator, assignee}  [already exists]
  - events: {id, type, agent_id, session_id, timestamp, payload}

GSD Extension (optional module)
  - gsd_projects, gsd_milestones, gsd_phases, gsd_plans
  - gsd_requirements, gsd_agent_executions

Superpowers Extension (optional module)
  - sp_skills, sp_workflows, sp_stages, sp_invocations
  - sp_plans, sp_plan_tasks

Allium Extension (optional module)
  - al_specs, al_entities, al_rules, al_surfaces
  - al_contracts, al_drift_reports
```

The shared event stream (`events` table) is the glue. Every skill emits events through the same mechanism, enabling cross-skill analytics without cross-skill schema coupling.

---

## 6. Integration Architecture

### 6.1 The Store Interface Pattern

hive-server already uses a `Store` interface. The architecture extends this pattern:

```
                            +-----------------+
                            |   hive-server   |
                            |   (Go + chi)    |
                            +--------+--------+
                                     |
                    +----------------+----------------+
                    |                |                |
              +-----+----+    +-----+----+    +------+-----+
              |  Store   |    |  Search  |    |   Graph    |
              | interface|    | interface|    |  interface  |
              +-----+----+    +-----+----+    +------+-----+
                    |                |                |
               +----+----+    +-----+----+    +------+-----+
               | SQLite  |    | Meili    |    |   Gel      |
               | or CRDB |    | Searcher |    |   Store    |
               +---------+    +----------+    +------------+
```

Three interfaces, not one:

```go
// Store handles CRUD operations (source of truth)
type Store interface {
    // Existing memory/task/agent methods
    UpsertMemory(ctx context.Context, entry *MemoryEntry) (*MemoryEntry, error)
    CreateTask(ctx context.Context, task *Task) (*Task, error)
    // ...
}

// Searcher handles full-text search (read-only index)
type Searcher interface {
    Search(ctx context.Context, index string, req SearchRequest) ([]SearchResult, error)
    Index(ctx context.Context, index string, docs []interface{}) error
    Delete(ctx context.Context, index string, ids []string) error
}

// GraphStore handles relationship traversal (optional, for Gel)
type GraphStore interface {
    // GSD
    GetProjectGraph(ctx context.Context, name string) (*ProjectGraph, error)
    GetBlockedPlans(ctx context.Context, project string) ([]BlockedPlan, error)
    // Superpowers
    GetSkillDependencies(ctx context.Context, name string, depth int) ([]*Skill, error)
    GetAgentTree(ctx context.Context, workflowID string) (*AgentTree, error)
    // Allium
    GetEntityImpact(ctx context.Context, specName, entityName string) (*ImpactReport, error)
    GetDriftHistory(ctx context.Context, specName string) ([]DriftReport, error)
}
```

### 6.2 Data Flow Pattern

All writes go to the Store (SQLite/CRDB). The Store emits events. hive-server fans out to Searcher and GraphStore:

```
Agent writes task outcome
    |
    v
Store.UpdateTask()          <-- Source of truth write
    |
    +---> Searcher.Index()  <-- Async, best-effort
    |
    +---> GraphStore.Sync() <-- Async or skip if not configured
    |
    v
Return success to agent
```

### 6.3 Graceful Degradation

Every additional database beyond SQLite is optional and degrades gracefully:

| Component   | If Available                        | If Unavailable                         |
| ----------- | ----------------------------------- | -------------------------------------- |
| SQLite      | Source of truth for all data        | Cannot start (mandatory)               |
| Meilisearch | Full-text search, faceted discovery | Fall back to SQLite LIKE queries       |
| Gel         | Graph traversal, computed analytics | Return empty results for graph queries |
| CRDB        | Replace SQLite as Store backend     | Use SQLite (single-node only)          |

This is not theoretical. The hive-server handler layer checks for nil interfaces and skips optional features. An agent calling `/api/v1/search/skills` gets either Meilisearch results or a `503 Service Unavailable` with a clear message. The agent adjusts its behavior (load all skills from filesystem instead of searching).

### 6.4 Skill-Specific API Namespaces

Each skill gets its own API namespace under `/api/v1/`:

```
/api/v1/                         Shared (memory, tasks, agents, search)
/api/v1/gsd/                     GSD-specific endpoints
/api/v1/superpowers/             Superpowers-specific endpoints
/api/v1/specs/                   Allium-specific endpoints
```

Skills only call their own namespace. The shared namespace provides cross-cutting capabilities (search, events, memory) that all skills use.

---

## 7. Scale-Dependent Decisions

### 7.1 Solo Developer (1 machine, 1-3 agents)

**What to build:** SQLite + Meilisearch. Nothing else.

| Component   | Justification                                                                                                       |
| ----------- | ------------------------------------------------------------------------------------------------------------------- |
| SQLite      | Already works. Zero new infrastructure. Handles all CRUD.                                                           |
| Meilisearch | Single binary, ~50 MB RAM. Provides search across accumulated documents. The highest value-per-complexity addition. |
| Gel         | Skip. Graph queries are not needed when one person can hold the full project state in their head.                   |
| CRDB        | Skip. Distributed database for a single machine is absurd.                                                          |

**Total infrastructure:** hive-server (Go binary) + Meilisearch (Rust binary). Two processes, ~200 MB RAM combined.

### 7.2 Small Team (5 developers, 10-20 agents)

**What to build:** SQLite or PostgreSQL + Meilisearch + consider Gel.

| Component            | Justification                                                                                                                                  |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| PostgreSQL or SQLite | PostgreSQL if multi-instance hive-server is needed. SQLite if single-instance suffices.                                                        |
| Meilisearch          | Same as above, plus shared search index across team members.                                                                                   |
| Gel                  | Consider adding for cross-project analytics and requirement traceability. The 1 GB RAM cost is justified if multiple projects need visibility. |
| CRDB                 | Skip unless multi-region deployment is required. PostgreSQL handles the concurrency at this scale.                                             |

**Total infrastructure:** hive-server + PostgreSQL + Meilisearch + maybe Gel. 2-4 processes, 1.5-3 GB RAM.

### 7.3 Scaled Organization (50+ agents, multiple teams)

**What to build:** CRDB + Meilisearch + Gel.

| Component   | Justification                                                                                                        |
| ----------- | -------------------------------------------------------------------------------------------------------------------- |
| CRDB        | Multi-region, multi-instance hive-server. Serializable transactions for concurrent agent writes. Horizontal scaling. |
| Meilisearch | Enterprise Edition for sharding if index size exceeds single-node capacity.                                          |
| Gel         | Full graph analytics, cross-project dashboards, skill effectiveness scoring at scale.                                |
| SQLite      | Keep as a local-dev fallback. Developers should not need a cluster to run hive-server locally.                       |

**Total infrastructure:** hive-server cluster + CRDB cluster (3+ nodes) + Meilisearch + Gel + PostgreSQL (Gel backend). This is serious infrastructure for a serious organization. Do not build this speculatively.

---

## 8. Tradeoff Matrix

### 8.1 Database Capabilities by Skill Need

| Need                       | SQLite               | Gel                | CRDB                | Meilisearch     |
| -------------------------- | -------------------- | ------------------ | ------------------- | --------------- |
| **Basic CRUD**             | Excellent            | Good               | Good                | Poor (not a DB) |
| **Graph traversal**        | N/A                  | Excellent          | Passable (CTEs)     | N/A             |
| **Full-text search**       | Poor (LIKE)          | Poor               | Poor (no tsvector)  | Excellent       |
| **Typo tolerance**         | N/A                  | N/A                | N/A                 | Excellent       |
| **Faceted filtering**      | N/A                  | N/A                | Possible (GROUP BY) | Excellent       |
| **Distributed writes**     | N/A                  | N/A                | Excellent           | N/A             |
| **ACID transactions**      | Good (single-writer) | Good (via PG)      | Excellent           | N/A             |
| **Schema validation**      | None                 | Excellent          | Good (CHECK/FK)     | None            |
| **Computed properties**    | N/A                  | Excellent          | Views               | N/A             |
| **Event sourcing**         | Manual               | Manual             | Good (CDC)          | N/A             |
| **Semantic/hybrid search** | N/A                  | ext::ai (pgvector) | N/A                 | Excellent       |

### 8.2 Infrastructure Cost by Component

| Component             | RAM                                 | Disk                 | Operational Complexity            | Setup Time         |
| --------------------- | ----------------------------------- | -------------------- | --------------------------------- | ------------------ |
| SQLite                | ~0 (embedded)                       | Minimal              | None                              | 0 (already exists) |
| Meilisearch           | ~50 MB idle, spikes during indexing | 10-100 MB per index  | Low (single binary)               | 1 day              |
| Gel                   | 1 GB minimum                        | PostgreSQL storage   | Medium (server + PG + migrations) | 3-5 days           |
| CRDB (single-node)    | ~500 MB                             | Standard SQL storage | Low-Medium                        | 2-3 days           |
| CRDB (3-node cluster) | 1.5 GB+                             | 3x storage           | High (cluster management)         | 5-7 days           |

### 8.3 Value Delivered Per Unit of Complexity

This is the most important table. Ranked by value/complexity ratio:

| Integration                                  | Value                               | Complexity                              | Ratio                  | Verdict                           |
| -------------------------------------------- | ----------------------------------- | --------------------------------------- | ---------------------- | --------------------------------- |
| Meilisearch for search across all skills     | High (search is universally needed) | Low (single binary, simple API)         | **Best**               | Build first                       |
| SQLite schema upgrade (typed tables, events) | Medium (better structure)           | Low (already exists)                    | **Good**               | Build second                      |
| Gel for GSD graph queries                    | High (cross-project, traceability)  | High (new server, PG, migrations)       | Moderate               | Build when needed                 |
| Gel for Allium spec graph                    | High (cross-spec impact analysis)   | High (same Gel instance)                | Moderate               | Build when spec corpus grows      |
| Gel for Superpowers analytics                | Medium (skill effectiveness)        | High (same Gel instance)                | Lower                  | Build after data accumulates      |
| CRDB for distributed hive-server             | High (if scaling needed)            | Very High (cluster, retries, licensing) | **Low unless scaling** | Build only at scale               |
| CRDB for event sourcing                      | Medium (audit trail)                | High (new backend, retries)             | Low                    | SQLite events table is sufficient |

---

## 9. What NOT to Build

### 9.1 Over-Engineered Permutations

The following specific combinations from the nine analyses are not worth the complexity:

**CRDB for Allium specs.** Allium specs are read-mostly, single-writer files. The distributed transaction capabilities, the serialization retry logic, the 3-node minimum cluster -- none of this addresses an actual Allium problem. The entire Allium-CRDB analysis is a solution looking for a problem. The one exception is if runtime trace validation generates high-volume time-series data, but that feature is still on Allium's roadmap, not shipped.

**CRDB for Superpowers at solo-developer scale.** Superpowers runs one orchestrator with a few subagents, all on one machine. Distributed SQL adds retry complexity, network latency, and operational burden for zero benefit. SQLite handles this concurrency trivially.

**Gel for basic Superpowers skill tracking.** If all you need is "record skill invocations and query effectiveness," a SQLite table with 4 columns (skill_name, timestamp, success, duration) gets you 80% of the value at 5% of the complexity. Gel's graph model is warranted only when the skill dependency graph and agent dispatch trees need to be queried.

**Full event sourcing for GSD state transitions via CRDB.** GSD's state machine has approximately 6 states. Recording every transition in an append-only event log with materialized views is elegant but absurd for a system where state transitions happen a few times per hour. A simple `updated_at` column on the task record is sufficient.

**Three-database architecture for a solo developer.** Running SQLite + Meilisearch + Gel + PostgreSQL (Gel backend) = 4 processes for one person writing code. The infrastructure management time exceeds the time saved by the capabilities. Start with SQLite + Meilisearch. Add Gel when the need is tangible.

### 9.2 Capabilities That Sound Nice but Are Not Worth It

**Skill recommendation engine (Superpowers-Gel).** Requires accumulated invocation data, effectiveness scoring (whose scoring?), and relevance computation. By the time you have enough data to make this work, you already know which skills are useful because you have been using them. The cold start problem kills the value proposition.

**Workflow templates from successful past workflows (Superpowers-Gel/CRDB).** Cloning a prior workflow's task structure assumes that similar-sounding tasks have similar solutions. In practice, every feature is different enough that cloned task lists are misleading. Write a new plan.

**Real-time agent coordination via database notifications (any skill + Gel/CRDB).** The analyses propose LISTEN/NOTIFY (unavailable in CRDB), Gel's PostgreSQL NOTIFY, or polling endpoints for reactive agent coordination. In practice, the orchestrator already manages agent lifecycle. Adding a database-mediated notification layer creates a second coordination mechanism that must be kept consistent with the first. Use the orchestrator.

**Semantic search over Allium specs via embeddings.** The Allium-Meili analysis proposes hybrid search with embedders for behavioral similarity. Allium specs use a formal language with precise keywords. Keyword search with typo tolerance handles 95% of spec discovery. Semantic search adds embedding generation latency and cost for marginal improvement on a small corpus.

**Cross-project spec composition queries at the database level.** The Allium-Gel analysis proposes queries like "which projects depend on the OAuth library spec?" This is a valid question at large scale, but for the foreseeable future, `grep -r "use Auth" specs/` answers it in milliseconds.

### 9.3 The General Rule

If the capability requires more than 2 weeks of implementation and the current workaround takes less than 30 seconds of manual effort, do not build it. Databases should replace impossible operations (cross-project queries across 50 repos) or error-prone operations (concurrent state updates), not merely convenient operations (finding which file mentions a keyword).

---

## 10. Recommended Architecture

### 10.1 Phase 0: Foundation (Now)

**Do this immediately. Zero new databases.**

Upgrade hive-server's existing SQLite schema to support the shared core entities:

```sql
-- Events table (append-only, cross-skill)
CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    event_type TEXT NOT NULL,         -- 'task.completed', 'skill.invoked', 'phase.transitioned'
    agent_id TEXT NOT NULL DEFAULT '',
    session_id TEXT NOT NULL DEFAULT '',
    repo TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}', -- JSON
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_events_type ON events(event_type);
CREATE INDEX IF NOT EXISTS idx_events_agent ON events(agent_id);
CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_created ON events(created_at);
```

Add the `Searcher` interface to hive-server with a no-op implementation:

```go
type NoopSearcher struct{}
func (n *NoopSearcher) Search(ctx context.Context, index string, req SearchRequest) ([]SearchResult, error) {
    return nil, nil // graceful degradation
}
```

Define the API namespace structure. Reserve `/api/v1/search/`, `/api/v1/gsd/`, `/api/v1/superpowers/`, `/api/v1/specs/`.

**Cost:** 1-2 days. **Value:** Establishes the extension points for everything that follows.

### 10.2 Phase 1: Add Meilisearch (Week 1-2)

**The single highest-value addition across all three skills.**

Deploy Meilisearch alongside hive-server. Implement the `MeiliSearcher` backend. Create indexes:

| Index       | Purpose                           | Primary Source            |
| ----------- | --------------------------------- | ------------------------- |
| `memories`  | Search hive-server memory entries | SQLite memory table       |
| `tasks`     | Search hive-server tasks          | SQLite tasks table        |
| `artifacts` | Search GSD/Superpowers documents  | Agent-submitted content   |
| `specs`     | Search Allium spec constructs     | allium-cli AST output     |
| `sessions`  | Search session summaries          | Agent-submitted summaries |

Expose search endpoints:

```
POST /api/v1/search              Federated search across all indexes
POST /api/v1/search/memories     Search memory entries
POST /api/v1/search/artifacts    Search workflow artifacts
POST /api/v1/search/specs        Search Allium constructs
POST /api/v1/search/sessions     Search session logs
```

Implement query preprocessing (stopword removal, 10-word limit handling). Implement sync from SQLite to Meilisearch on writes (async, best-effort).

**Cost:** 5-7 days. **Value:** Cross-session memory, skill discovery, spec search, document retrieval. Addresses the #1 gap across all three skills.

### 10.3 Phase 2: Structured Skill Integration (Week 3-6)

Add skill-specific endpoints backed by SQLite:

**GSD endpoints:**

```
POST   /api/v1/gsd/projects                    Create project
GET    /api/v1/gsd/projects/{name}/status       Current state
PATCH  /api/v1/gsd/projects/{name}/phases/{n}   Update phase status
POST   /api/v1/gsd/events                       Record GSD events
GET    /api/v1/gsd/analytics/velocity            Velocity from events
```

**Superpowers endpoints:**

```
POST   /api/v1/superpowers/skills               Register a skill
POST   /api/v1/superpowers/invocations           Record skill invocation
POST   /api/v1/superpowers/sessions              Submit session summary
GET    /api/v1/superpowers/skills/effectiveness   Aggregate from invocations
```

**Allium endpoints:**

```
POST   /api/v1/specs/sync                       Ingest parsed AST JSON
GET    /api/v1/specs/{name}                     Get spec metadata
POST   /api/v1/specs/{name}/drift               Submit drift report
GET    /api/v1/specs/{name}/drift               Get drift history
```

All backed by SQLite. No new databases. The events table captures everything for analytics.

**Cost:** 2-4 weeks. **Value:** Persistent state for all three skills. Structured task tracking for GSD. Skill effectiveness data for Superpowers. Drift history for Allium.

### 10.4 Phase 3: Add Gel (When Justified)

Add Gel only when specific graph queries are needed that SQLite cannot serve:

**Trigger conditions:**

- Managing 3+ GSD projects simultaneously and needing cross-project requirement traceability
- Allium spec corpus exceeds 10+ files across 2+ projects and cross-spec impact analysis is needed
- Superpowers skill catalog exceeds 50+ skills and dependency graph queries are valuable

**What Gel adds over SQLite:**

- Bidirectional links (back-references without application logic)
- Path traversal (multi-hop queries without recursive CTEs)
- Computed properties (always-correct derived values)
- Schema-enforced enums and constraints

**Implementation:** Add `GelStore` as an optional `GraphStore` implementation. Run Gel alongside (not replacing) SQLite. SQLite remains source of truth for CRUD. Gel is a materialized graph view synced from SQLite events.

**Cost:** 1-2 weeks for initial integration + ongoing schema maintenance. **Value:** Depends entirely on whether the graph queries are actually needed.

### 10.5 Phase 4: Replace SQLite with CRDB (Only at Scale)

If hive-server needs to run as multiple instances behind a load balancer:

**Trigger conditions:**

- Multiple hive-server instances needed for availability or throughput
- Agents are distributed across regions
- Single-writer SQLite is a proven bottleneck

**What CRDB adds over SQLite:**

- Multi-instance concurrent writes with serializable isolation
- Horizontal scaling
- Automatic failover

**Implementation:** Implement `CRDBStore` as an alternative `Store` backend. Both SQLite and CRDB implement the same interface. Configuration selects the backend. All transaction paths use `crdbpgx.ExecuteTx()` for retry safety.

**Cost:** 2-3 weeks + ongoing operational burden. **Value:** Only realized at scale that exceeds SQLite's single-writer capacity.

### 10.6 Architecture Summary

```
Phase 0 (now):      [SQLite] ---- hive-server ---- [NoopSearcher]
Phase 1 (week 2):   [SQLite] ---- hive-server ---- [Meilisearch]
Phase 2 (week 6):   [SQLite] ---- hive-server ---- [Meilisearch]
                                     |
                              skill-specific APIs
Phase 3 (when needed): [SQLite] ---- hive-server ---- [Meilisearch]
                                        |
                                     [Gel DB]
Phase 4 (at scale):    [CRDB] ---- hive-server ---- [Meilisearch]
                                        |
                                     [Gel DB]
```

### 10.7 The Decision Framework

When someone asks "should we add X?", apply this filter:

1. **Is there a specific query we cannot answer today?** If yes, which database answers it? Add that one.
2. **Is the workaround for not having this database more than 30 seconds of effort?** If no, do not add it.
3. **How many times per day would this capability be used?** If less than once, it is not worth a new runtime dependency.
4. **Does the capability require accumulated data to be useful?** If yes, can we start collecting the data in SQLite/events now and add the fancy database later? Usually yes.

The goal is to build the **minimum infrastructure that makes autonomous agents measurably more effective**. Every database added is a process to monitor, a sync mechanism to maintain, and a failure mode to handle. The value must exceed the cost, measured in actual developer hours saved, not in theoretical capability unlocked.

---

## Sources

This synthesis draws from the following analyses:

**Permutation analyses:**

- perm-gsd-gel.md, perm-gsd-crdb.md, perm-gsd-meili.md
- perm-superpowers-gel.md, perm-superpowers-crdb.md, perm-superpowers-meili.md
- perm-allium-gel.md, perm-allium-crdb.md, perm-allium-meili.md

**Skill briefs:**

- gsd.md, superpowers.md, allium.md

**Database technology briefs:**

- gel-db.md, cockroachdb.md, meilisearch.md
