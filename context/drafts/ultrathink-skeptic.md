# Skeptical Senior Engineer Review

**Date:** 2026-03-09
**Reviewer perspective:** Someone who has watched ambitious infrastructure projects die under their own weight, and who has actually read the code.

---

## 0. The Executive Summary Nobody Asked For

You have a ~3000-line Go application that does CRUD over SQLite. It works. It has tests. It ships.

You are now staring at five documents totaling ~40,000 words that describe a system spanning 4 databases, 3 skill integrations, an LLM synthesis layer, a memory injection pipeline, a search engine, an event-sourced append-only log, skill-specific API namespaces, and 33 build steps across 7 phases.

The vision-v2 document is significantly better than the original build plan. It correctly kills CockroachDB, Gel DB, and MasterClaw from the near-term scope. That took intellectual honesty. But it still describes a system that is 10-20x larger than what exists today, with no consumers for any of the new APIs, and a dependency on Meilisearch that adds operational complexity for search queries that nobody is making yet.

The core question is not "is this well-designed?" (it is). The question is "is any of this the right thing to build right now?"

---

## 1. What Is Over-Engineered

### 1.1 The Skill-Specific API Namespaces Are Premature

The vision-v2 defines three skill-specific API namespaces:

- `/api/v1/gsd/` -- 7 endpoints for GSD project management
- `/api/v1/superpowers/` -- 4 endpoints for Superpowers skill tracking
- `/api/v1/specs/` -- 5 endpoints for Allium spec management

None of these skills currently call hive-server. None of them have been modified to call hive-server. There is no `hive-local` binary. There is no `hive-plugin`. The entire consumer side of this API does not exist.

Building 16 new endpoints with custom SQLite tables for three skills that do not yet integrate with hive-server is speculative infrastructure. You are designing a restaurant menu before anyone has walked in the door.

**The real question:** How do GSD, Superpowers, and Allium actually send data to hive-server? Each is a slash command / hooks system running inside a Claude Code session. The integration path is:

1. Modify each skill's source code to make HTTP calls to hive-server
2. Or build a `hive-local` proxy that intercepts skill output
3. Or build a `hive-plugin` that hooks into the Claude Code lifecycle

None of these integration paths are built, designed in detail, or estimated. The build plan and vision-v2 treat them as "and then the skills call the API" without addressing the actual plumbing.

### 1.2 The Events Table Is a Solution Without a Query

The events table design:

```sql
CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    agent_id TEXT NOT NULL DEFAULT '',
    session_id TEXT NOT NULL DEFAULT '',
    repo TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL
);
```

What query will you run against this table? "Show me all events of type `task.completed` in the last week"? You can do that with `ListTasks` filtered by status and a date range. "Show me all events for agent X"? You already have agent heartbeats and task assignee fields.

The events table is designed for cross-skill analytics that require accumulated data from skills that do not yet integrate. It will sit empty, or it will contain synthetic test data that nobody queries. The table is cheap to add, but the mental overhead of designing around it, indexing into Meilisearch, and maintaining event type taxonomies is not.

**What to do instead:** Add the events table when the first actual consumer exists. When GSD's orchestrator actually calls `POST /api/v1/events` with real data, you will know exactly what fields and indexes you need. Right now you are guessing.

### 1.3 The Session Summaries Endpoint Assumes a Problem That Does Not Exist Yet

Session summaries are described as "the most impactful capability." But:

- Who writes the session summary? The agent, at the end of a session? How does the agent know the session is ending? Claude Code sessions do not have a clean "session end" lifecycle event.
- What goes in the summary? The entire conversation? A human-written paragraph? An LLM-generated summary of the session? The schema says `summary TEXT` but the actual content strategy is undefined.
- Who reads it? The next session's agent, via memory injection? But memory injection requires Meilisearch, which is Phase 1. And the injection endpoint requires keyword extraction, which is undefined.

The value chain for session summaries is: agent generates summary -> POST to hive-server -> indexed in Meilisearch -> retrieved by injection endpoint -> injected into next session. Every link in this chain is unbuilt. Building the middle link (the endpoint) without the first or last link is not useful.

### 1.4 Meilisearch Is Not Free

The synthesis and vision-v2 repeatedly describe Meilisearch as "a single binary, ~50 MB RAM, lowest infrastructure cost." This is misleading.

The actual cost of Meilisearch:

- **Operational complexity:** A second process to manage, monitor, restart, and debug. When hive-server starts but Meilisearch is down, every search endpoint returns 503. You need health checks, alerting, and restart logic.
- **Sync complexity:** The SyncStore wrapper pattern fires async goroutines on every write. This means: goroutine lifecycle management, error handling for sync failures, reconciliation for missed writes (the build plan specifies a 5-minute reconciliation job), and potential data inconsistency between SQLite and Meilisearch during the sync window.
- **Query complexity:** The 10-word query limit is a real constraint. LLM agents generate verbose queries. The keyword extraction logic ("strip stop words, take top 10") is hand-waved. Bad keyword extraction makes search useless. Good keyword extraction is a non-trivial NLP problem.
- **Index management:** Five indexes (memories, tasks, sessions, artifacts, specs) each with their own searchable/filterable/sortable attribute configuration. Changing these settings requires re-indexing.

For a solo developer with 3 agents, SQLite `LIKE '%keyword%'` queries on a few hundred records will return in under 1ms. Meilisearch adds value when you have thousands of records and need typo-tolerant fuzzy matching. That is not the current scale.

---

## 2. What Is Under-Specified

### 2.1 Memory Injection Has No Concrete Implementation

The memory injection pipeline is described in vision-v2 Section 4.2:

```
Agent receives message
    -> hive-local intercepts (pre-prompt hook)
    -> POST /api/v1/memory/inject
    -> hive-server extracts key terms, queries Meilisearch, queries SQLite
    -> Returns context blocks trimmed to token budget
```

Every step of this pipeline raises unanswered questions:

1. **"hive-local intercepts"** -- How? hive-local does not exist. Is it a Claude Code hook? A proxy process? A VS Code extension? The interception mechanism determines everything about latency, reliability, and user experience. This is not a detail; it is the entire architecture.

2. **"Extract key terms from prompt_text"** -- How? Split on whitespace and remove stop words? That loses "race condition in store.go" because "in" is a stop word. Use TF-IDF? Against what corpus? Use an LLM to extract keywords? That adds latency and cost to every single prompt.

3. **"Query Meilisearch: search memories + sessions + artifacts"** -- One query or three? How do you merge results across indexes? How do you handle the case where memories return 50 hits and sessions return 0? The synthesis says "Meilisearch's built-in relevance scoring combined with recency weighting provides adequate ranking" but Meilisearch does not have recency weighting. You have to implement it.

4. **"Trim to max_tokens budget"** -- How do you estimate tokens? The build plan says `EstimateTokens` using "len(s) / 4 as approximation." That is a 20-30% error rate on real text. Over-estimating means you inject less context than you could. Under-estimating means you blow the token budget and waste money.

5. **"200ms latency budget"** -- For the full pipeline including network round-trips, Meilisearch queries, SQLite queries, result merging, and token estimation? On what hardware? This number appears to be invented rather than measured.

### 2.2 The GSD Integration Path Is Magical Thinking

From vision-v2 Section 3.3:

> GSD's orchestrator can push state updates to hive-server after each phase transition, plan completion, or requirement status change.

GSD is a slash command (`/gsd`) that runs inside a Claude Code conversation. Its orchestrator is a prompt template that tells Claude what to do. It does not "push state updates" anywhere. It writes markdown files to `.planning/`.

To make GSD push state to hive-server, you would need to:

1. Modify GSD's source code to add HTTP calls after each state change
2. Or add a file watcher that detects `.planning/` file changes and syncs them
3. Or build a post-commit hook that parses `.planning/` files and uploads structured data

Each of these is a non-trivial project. The first requires modifying GSD (a separate repo with its own maintainer/lifecycle). The second is fragile (file watching is notoriously unreliable, and you need to parse markdown to extract structured data). The third runs after every commit, including commits that do not touch `.planning/`.

The vision treats "GSD calls hive-server" as a given. It is actually the hardest integration problem in the entire plan, and it is not estimated, designed, or even acknowledged as difficult.

### 2.3 The Allium Integration Assumes allium-cli Produces JSON ASTs

From vision-v2 Section 3.5:

> `allium-cli` produces a typed AST as JSON. hive-server stores this JSON and indexes its contents in Meilisearch.

Does allium-cli currently produce JSON ASTs? The allium context document describes a spec language, not a CLI tool with JSON output. If the CLI does not produce this output today, someone needs to build it. That is a separate project.

### 2.4 "Unified API" Across Skills Is Not Actually Unified

The synthesis says "Can a single schema work across all three skills? No. The data models are too different." Then the vision-v2 proposes three separate API namespaces plus a shared events endpoint as "the correct unification point."

This is not unification. This is three separate APIs that happen to share a web server and an events table. Which is fine. But it means hive-server is not a "unified agent infrastructure." It is three independent micro-APIs that could just as easily be three separate services. The shared value is... a shared database? A shared auth token?

The honest framing is: hive-server is a generic CRUD API with per-skill schemas. The "unification" is that skills can query each other's events table. That is a legitimate value proposition, but it is much smaller than "unified agent infrastructure" implies.

---

## 3. What Will Actually Break

### 3.1 The Huma v2 Migration Is the Biggest Risk and Nobody Is Treating It That Way

The current codebase uses Huma v2 already (I can see it in the handlers.go imports). So the build plan's Step 3A.5 "Add Huma v2 API Framework" appears to be already done. This raises the question: **is the build plan aware of the current state of the code?**

Looking at handlers.go, Huma is already integrated with chi. The `routes()` method already creates a `humachi.New(r, config)` and registers operations. The build plan's Step 3A.5 describes doing exactly what the code already does. This suggests the build plan was written against a prior version of the code and has not been updated.

If the build plan's steps are not aligned with the actual codebase, every step that says "after 3A.5" has invalid prerequisites. This could waste significant time.

### 3.2 Bolting Meilisearch Onto SQLite Creates a Consistency Problem

The SyncStore pattern fires async goroutines on every write:

```go
go ss.sync.IndexMemory(context.Background(), result)
```

This means:

- A successful `POST /api/v1/memory` returns 200 to the client
- The Meilisearch index update happens asynchronously, maybe
- If Meilisearch is down, the index update silently fails
- The reconciliation job runs every 5 minutes and re-indexes everything

During those 5 minutes, search results are stale. An agent writes a memory entry, then immediately searches for it, and does not find it. This is a consistency bug that will be confusing to debug because it is intermittent and depends on timing.

The "graceful degradation" story is: "search endpoints return 503, all other functionality unaffected." But what about the memory injection endpoint that depends on search results? If injection returns empty context because Meilisearch is down, agents operate without memory, which is the exact problem hive-server is supposed to solve.

### 3.3 SQLite Single-Writer Under Agent Load

The current SQLite configuration:

```go
db.SetMaxOpenConns(1)
db.SetMaxIdleConns(1)
```

This is a single connection. Any concurrent requests serialize through this one connection. With 3 agents sending heartbeats every 30 seconds, plus memory writes, plus task updates, the request queue grows. Each SQLite write takes 1-5ms, so at low load this is fine. But if an agent sends a burst of writes (e.g., GSD recording 20 task completions after a plan wave), requests queue behind each other.

This is not a crisis, but it is a real performance ceiling that contradicts the claim "SQLite handles the concurrency." SQLite handles it, slowly, with queueing. The vision-v2 says "The single-writer limitation of SQLite is not a problem for a solo developer with a few agents." True, until it is. And the diagnostic path (figuring out why requests are slow) leads to a "you need CockroachDB" conversation that the vision-v2 is trying to defer.

The honest advice: keep SQLite but set `MaxOpenConns` higher (it can handle concurrent readers with WAL mode) and benchmark write throughput before promising it is fine.

### 3.4 The Store Interface Will Become Unwieldy

The current Store interface has 12 methods. The vision-v2 proposes adding:

- Events: `RecordEvent`, `ListEvents`
- Sessions: `CreateSession`, `GetSession`, `ListSessions`
- GSD: `CreateProject`, `GetProject`, `ListProjects`, `UpdateProject`, `RecordPhaseTransition`, `GetVelocity`, `StoreRequirement`, `ListRequirements`
- Superpowers: `RecordInvocation`, `ListInvocations`, `GetSkillEffectiveness`, `RecordWorkflow`, `ListWorkflows`
- Allium: `SyncSpec`, `GetSpec`, `ListSpecs`, `SubmitDriftReport`, `GetDriftHistory`

That is 12 existing + ~20 new = ~32 methods on a single interface. Any new Store backend implementation (CockroachDB, PostgreSQL, test mock) must implement all 32 methods. Test mocks will be 200+ lines of boilerplate.

The correct pattern is to split the Store interface into composed interfaces:

```go
type MemoryStore interface { ... }
type TaskStore interface { ... }
type EventStore interface { ... }
type GSDStore interface { ... }
```

But the build plan and vision-v2 do not propose this. They keep adding methods to the monolithic Store interface.

---

## 4. The Impedance Mismatches

### 4.1 GSD: Markdown Files vs. Structured API

GSD stores everything as markdown in `.planning/`. Its data model is:

- `STATE.md` -- current phase, status
- `PLAN.md` -- task lists with checkboxes
- `ROADMAP.md` -- milestone tracking
- `RESEARCH-*.md` -- research findings

Converting this to structured API calls requires parsing markdown to extract structure (task names, statuses, phase transitions), then POSTing that structure to hive-server. This is a lossy conversion. The markdown contains rich context (why a decision was made, what alternatives were considered) that a structured API cannot capture. You end up storing a degraded version of what the files already have.

The synthesis acknowledges this with "Option C: Hybrid -- structured data in database, prose in files." But this means maintaining two copies of the data (files and database), keeping them in sync, and resolving conflicts when they diverge.

### 4.2 Superpowers: Hooks Framework vs. REST API

Superpowers is a hooks-based system. Its workflow is:

1. User says `/superpowers brainstorm` in Claude Code
2. Superpowers prompt template tells Claude to follow a specific process
3. Claude executes the process within the conversation context
4. Nothing is persisted anywhere

To integrate with hive-server, Superpowers would need to make HTTP calls from within a Claude Code conversation. Claude Code does have tool use (bash, file operations), so `curl` calls are technically possible. But every `curl` call to hive-server is tokens spent on HTTP plumbing instead of the actual task.

The single-tool pattern (one `hive` CLI tool) is supposed to solve this. But `hive` does not exist. The `hive-local` proxy does not exist. The `hive-plugin` does not exist. The entire token-efficient invocation path is unbuilt.

### 4.3 Allium: Spec Language vs. Structured Storage

Allium is a specification language. Its data model is a custom syntax:

```allium
entity User {
  rule "must have email" { ... }
  surface "login form" { ... }
}
```

Storing this in hive-server requires either:

- Storing the raw text (in which case, why not just search the files?)
- Parsing the AST and storing structured constructs (which requires a parser that may not exist as a standalone tool)

The vision-v2 assumes `allium-cli` produces JSON ASTs. If it does not, someone needs to build that first. And even then, the JSON AST is a derivative artifact -- the `.allium` file is still the source of truth. You are building a read-only secondary index of data that is already on disk.

### 4.4 The Fundamental Mismatch

All three skills are **prompt engineering systems**. They work by telling an LLM what to do using carefully crafted prompts. They do not make HTTP calls, query databases, or manage state programmatically. They output text and files.

hive-server is an **HTTP API with structured data models**. It expects programmatic clients that serialize JSON, manage auth tokens, and handle error responses.

Bridging these two worlds requires an intermediary (hive-local, hive-plugin, or modified skills) that translates between "LLM writes markdown" and "API expects JSON POST." That intermediary is the actual product. hive-server is just the backend storage for it. And the intermediary does not exist.

---

## 5. Dependency Hell

### 5.1 The Critical Path

The build plan's dependency chain: #20 (project layout) -> #16 (Huma v2) -> #12 (CockroachDB).

Since Huma v2 appears to already be integrated, #16 may be done or partially done. But #20 (project layout refactor) is not done. The cmd/app/ directory still exists. The internal/model/ package does not exist. The internal/server/ package does not exist.

If #20 takes longer than expected (and refactoring always takes longer than expected because you discover unexpected coupling), everything downstream shifts.

The vision-v2 eliminates the CockroachDB dependency (#12) from the critical path. Good. But it replaces it with: events table + sessions table + Searcher interface + Meilisearch integration. That is still 4+ steps before any skill-specific API can be built.

### 5.2 What Happens When Meilisearch Integration Takes 3x Longer

The vision-v2 estimates Meilisearch integration at "Weeks 3-4." In my experience, integrating a search engine into an existing application takes 3-6 weeks for a solo developer, not 1-2. The reasons:

- **Index schema design** requires understanding what queries you will run, which requires understanding your consumers, which do not exist yet.
- **Sync reliability** requires handling every edge case: create, update, delete, bulk operations, partial failures, re-indexing.
- **Query preprocessing** (the 10-word limit, stop word removal, keyword extraction) is iterative. You try one approach, test it with real queries, discover it does not work, and redesign.
- **Testing** against a real Meilisearch instance introduces CI complexity (running Meilisearch in GitHub Actions, handling test isolation, cleaning up indexes between tests).

If Meilisearch takes 6 weeks instead of 2, the skill-specific APIs (Phase 2, weeks 5-8) shift to weeks 9-12. That is a 3-month project, not a 2-month project.

### 5.3 The Fallback Is "Just Use SQLite"

Every document says "graceful degradation: if Meilisearch is unavailable, fall back to SQLite LIKE queries." This is the correct architectural decision, but it raises a question: if the fallback is SQLite LIKE queries, and the current scale is a few hundred records, why not just ship the SQLite LIKE queries and skip Meilisearch entirely?

The answer in the documents is "Meilisearch provides typo-tolerant fuzzy search and relevance ranking." But for the current use case (one developer searching their own memories), exact match or substring match is usually sufficient. You know what you are looking for. You typed it in. Typo tolerance helps when you have thousands of records from many sources and you are doing exploratory discovery. That is not the current scenario.

---

## 6. The Honest Timeline

### 6.1 What a Single Developer With LLM Assistance Can Actually Ship

Assumptions:

- One developer, using Claude Code or similar for implementation
- Available ~4-6 hours/day for this project (not full-time, there are other responsibilities)
- LLM assistance speeds up boilerplate but does not speed up debugging, design decisions, or integration testing

**Week 1-2 (realistic):**

- Remove k8s/ directory (30 minutes)
- Add scripts-to-rule-them-all (2 hours)
- Start project layout refactor (this is the big one -- extracting model package, renaming cmd/app, updating all imports). Realistically 2-3 days with testing.
- Add events and sessions tables to SQLite schema (1-2 hours for schema, half a day for CRUD endpoints and tests)

**Week 3-4 (realistic):**

- Finish anything that slipped from week 1-2
- Define and implement Searcher interface with NoopSearcher (half a day)
- Start Meilisearch integration if feeling ambitious

**Week 5-8 (realistic):**

- Finish Meilisearch integration (sync, search endpoints, reconciliation)
- Maybe start one skill-specific API (probably events, since it is the simplest)

**Month 3 (realistic):**

- One or two skill-specific API namespaces with basic CRUD
- Memory injection endpoint (basic version without sophisticated ranking)
- No GSD integration (requires modifying GSD)
- No Superpowers integration (requires building hive-local or modifying Superpowers)
- No Allium integration (requires allium-cli JSON AST output)

**What you will NOT have in 3 months:**

- Any skill actually calling hive-server
- Memory injection working end-to-end
- Cross-session memory (requires the full injection pipeline)
- Cross-project visibility (requires at least 2 projects actively using hive-server)
- Historical analytics (requires accumulated data that does not exist)

### 6.2 The Real Bottleneck

The real bottleneck is not building hive-server. The real bottleneck is building the integration layer that connects skills to hive-server. That means:

1. Building `hive-local` (a Go proxy that runs on the developer's machine)
2. Building `hive-plugin` (a TypeScript shim for Claude Code MCP)
3. Modifying GSD, Superpowers, and Allium to call the `hive` tool

Each of these is a separate project with its own design, testing, and deployment considerations. The vision-v2 treats them as assumed infrastructure. They are not. They are the product.

---

## 7. What to Cut

### 7.1 If You Had 1 Week

Build exactly this and nothing else:

1. **Add events and sessions tables to the existing SQLite schema.** Two CREATE TABLE statements. Four new CRUD endpoints (POST/GET for each). Basic tests. No Meilisearch, no Searcher interface, no NoopSearcher. Just raw SQLite.

2. **Add a `POST /api/v1/memory/search` endpoint** that does `SELECT * FROM memory WHERE value LIKE '%' || ? || '%' OR tags LIKE '%' || ? || '%' ORDER BY updated_at DESC LIMIT 20`. It is ugly. It works. It answers the question "what do I know about X?" for a few hundred records.

3. **Write a shell script** called `hive` that wraps `curl` calls to hive-server. Give it to Claude Code as a tool. Something like:

   ```bash
   hive memory set <key> <value> [--tags tag1,tag2]
   hive memory get <key>
   hive memory search <query>
   hive session save <summary>
   hive events record <type> <payload>
   ```

This gives you cross-session memory (via the shell script tool), basic search (via LIKE queries), and event recording. Total: ~500 lines of new Go code, ~100 lines of shell script. Shippable in a week.

### 7.2 What to Throw Away

- **All skill-specific API namespaces.** Build them when a skill actually needs them.
- **Meilisearch.** Add it when LIKE queries become too slow (hundreds/thousands of records).
- **The Searcher interface and NoopSearcher pattern.** This is abstraction for future flexibility. When you need Meilisearch, add it. The interface can be designed then, informed by actual usage patterns.
- **The SyncStore wrapper.** Premature optimization for a consistency problem that does not exist yet.
- **Events table indexes.** Add them when queries are slow, not before.
- **The memory injection pipeline.** Build a dumb version first: "fetch the 10 most recent memories and 5 most recent session summaries." No keyword extraction, no relevance ranking, no token budgets. See if that is useful. If it is, then add sophistication.
- **The project layout refactor (#20).** Controversial take: the current layout works. `cmd/app/` vs `cmd/hive-server/` does not matter for a single-binary application. `internal/model/` extraction is nice but not blocking. Do this when you are adding a second store backend, not before.

### 7.3 What to Actually Build Next (Priority Order)

1. **A working `hive` CLI tool that an agent can call.** This is the product. Without it, hive-server is a database with a REST API that nobody uses. It can be a bash script wrapping curl. It does not need to be a Go binary.

2. **Session summaries.** The simplest useful cross-session capability. Agent writes a summary at the end of a session. Next session's agent retrieves the last N summaries. No search engine, no injection pipeline, just CRUD.

3. **A Claude Code hook** (MCP tool or pre-prompt hook) that injects the last 3 session summaries into every conversation. This is the "memory injection" system, v0. No keyword extraction, no Meilisearch, no token budget management. Just "here is what happened recently."

4. **Measure whether that is useful.** If agents with session summaries perform measurably better than agents without, you have validated the core value proposition and earned the right to build the sophisticated version. If they do not, you have saved months of work.

---

## 8. Specific Risks Nobody Is Talking About

### 8.1 The Cold Start Problem

Every analytics feature (velocity, skill effectiveness, cross-project visibility) requires accumulated data. On day 1, hive-server has zero events, zero session summaries, zero skill invocations. The analytics endpoints return empty results. They will return empty results for weeks or months until enough data accumulates.

This means the value proposition is deferred. You invest engineering time now for value that materializes later. How much later? If a developer runs 5 agent sessions per day and each generates one session summary, after a month you have 100 summaries. Is that enough for meaningful cross-session memory? Maybe. Is it enough for "velocity analytics"? No.

The risk is that the system works technically but delivers no perceptible value for months, leading to abandonment before the compounding data value kicks in.

### 8.2 Data Quality

The session summaries, skill invocations, and event records are only as good as the data that goes in. If the agent writes a poor summary ("I worked on some stuff"), that is what gets stored and retrieved. Garbage in, garbage out.

No document discusses data quality. What makes a good session summary? What fields are required vs optional? Is there validation? Should hive-server reject summaries that are too short? Too long? Missing required fields?

### 8.3 The Maintenance Burden of Unused Code

If you build 30+ endpoints and only 5 get used, you still have to maintain all 30. Every schema migration touches all tables. Every dependency upgrade requires testing all endpoints. Every refactor must consider all code paths.

Unused code is not free. It is negative value. It adds cognitive load, maintenance cost, and false confidence ("we have an API for that!") while delivering nothing.

### 8.4 The Build Plan Itself Is a Risk

The build plan is 1000+ lines across 33 steps. Reading, understanding, and following it is a significant time investment. If reality diverges from the plan (and it will -- the Huma v2 step is already done), you face a choice: update the plan (more planning work) or ignore it (defeating its purpose).

The most productive thing might be to throw away the build plan and work from a one-page prioritized backlog. The 33-step dependency graph optimizes for a world where 5 developers are working in parallel. You have 1.

---

## 9. The Bottom Line

### What the documents get right

- Killing CockroachDB, Gel DB, and MasterClaw from the near-term scope (vision-v2)
- The observation that all three skills are stateless systems generating stateful artifacts
- The principle "enhance skills, do not replace them"
- Graceful degradation as a design principle
- SQLite-first approach

### What the documents get wrong

- Assuming the API design is the hard part (it is not; the skill integration is)
- Treating Meilisearch as low-cost (it adds real operational and code complexity)
- Estimating timelines based on code generation speed rather than design/debug/integrate speed
- Planning 16 new endpoints for consumers that do not exist
- Spending 40,000 words on architecture when the whole codebase is 3,000 lines of code

### The uncomfortable truth

The most valuable thing you can build is not in any of these documents. It is a `hive` CLI tool (even a 50-line bash script) that an agent can call to save and retrieve session state. If that tool exists and agents use it, you have validated the entire value proposition. If it does not exist, the most sophisticated backend architecture is worthless.

Build the CLI tool. Add session CRUD to hive-server. Hook it into one Claude Code session. See if it helps. Then decide what to build next based on what you learned, not based on a 33-step plan written before you had a single user.
