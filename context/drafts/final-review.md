# Final Review: Cross-Document Analysis

**Date:** 2026-03-09
**Status:** Quality gate before implementation
**Documents reviewed:** 17 documents (core research, synthesis, vision, build plan, original review, 4 ultra-think perspectives)
**Method:** Full read of all documents, cross-referenced against the actual codebase

---

## 1. Contradictions

### 1.1 Build Plan vs. Actual Codebase (CRITICAL)

The build plan describes Steps 3A.1, 3A.2, and 3A.5 as work to be done. The actual codebase shows:

- **Step 3A.1 (Remove k8s/):** Already done. The `k8s/` directory does not exist.
- **Step 3A.2 (scripts-to-rule-them-all):** Already done. `script/bootstrap`, `script/setup`, `script/test`, `script/server`, `script/docs` all exist, plus `script/e2e/` and `script/integration/` directories.
- **Step 3A.5 (Huma v2 migration):** Already done. `internal/handlers/handlers.go` imports `huma/v2` and `humachi`, creates a Huma API with `humachi.New(r, config)`, and registers all operations via `registerMemory`, `registerTasks`, `registerAgents`.

The skeptic (ultrathink-skeptic.md, Section 3.1) caught this: "Huma v2 appears to already be integrated... This suggests the build plan was written against a prior version of the code and has not been updated." The skeptic was correct on all three points.

**Impact:** The build plan's dependency chain assumes 3A.1, 3A.2, and 3A.5 are prerequisites for later work. Since they are already done, Steps 3A.3 (project layout), 3A.4 (formalize Store interface), and 3A.6 (E2E tests) can start immediately. The entire Phase 3A timeline compresses significantly.

The `cmd/app/` directory still exists (not yet renamed to `cmd/hive-server/`), confirming Step 3A.3 is still needed.

### 1.2 Build Plan Phase 3B (CockroachDB) vs. Vision-v2 Phasing

**Build plan:** Places CockroachDB migration as Phase 3B, immediately following Phase 3A Foundation. The CRDB store is the second major work item.

**Vision-v2:** Defers CockroachDB to Phase 4, after Meilisearch (Phase 1) and skill-specific APIs (Phase 2). States explicitly (Section 1.3): "This directly contradicts the original build plan's Phase 3B."

**Synthesis:** Recommends SQLite + Meilisearch first, CRDB "only at scale" (Section 10.5).

**github-issues.md:** Reports locked design decision #5: "CockroachDB for production."

**ultrathink-ops.md (Section 4):** Says "Do not migrate to CRDB until ALL of the following are true: measured write latency p99 exceeds 500ms, multiple instances required, team has operational capacity." For a solo developer: "CRDB is never needed."

**Resolution:** Vision-v2 and synthesis agree on deferring CRDB. The build plan's Phase 3B is obsolete. The locked decision #5 from ops#82 is explicitly overridden by vision-v2 Section 7.3, which recommends updating ops#82 to acknowledge the decision is scale-dependent. The original review (review.md, Section 9) says decision #5 is "HONORED" by the build plan, which was true when the review was written but is no longer the intended direction.

### 1.3 Architect's Interface Design vs. Vision-v2 API Surface

**ultrathink-architect.md (Section 1.3):** Proposes splitting the Store into domain-specific interfaces: `MemoryStore`, `TaskStore`, `AgentStore`, `EventStore`, `SessionStore`, plus skill-specific interfaces (`GSDStore`, `SuperpowersStore`, `AlliumStore`). The composed `Store` interface embeds the core domain interfaces.

**Vision-v2 (Section 3):** Defines API endpoints for GSD, Superpowers, and Allium but does not specify the interface architecture. It implicitly assumes methods are added to the existing Store.

**ultrathink-skeptic.md (Section 3.4):** Warns that the Store interface will grow to ~32 methods and become unwieldy. Recommends the same split pattern as the architect.

**Build plan (Step 3A.4):** Defines a single monolithic `Store` interface with all methods.

**Resolution:** The architect and skeptic agree on interface segregation. The build plan and vision-v2 do not address this. The architect's design is the correct approach and should be adopted.

### 1.4 Meilisearch RAM Numbers

**Synthesis (Section 8.2):** States Meilisearch idle RAM is "~50 MB."

**Vision-v2 (Section 8.2):** States "Meilisearch (~50 MB RAM)." Total system "~200 MB RAM combined."

**ultrathink-ops.md (Section 1):** States Meilisearch idle RAM is "50-80 MB" but peak is "200-500 MB (indexing)." Total Phase 1 peak is "300-620 MB."

**Meilisearch research (meilisearch.md, Section 9.3):** States "Memory usage can spike during indexing due to the memory-mapped architecture."

**Resolution:** The 50 MB figure is the idle/minimum. The ops analysis correctly identifies that peak RAM during indexing can reach 200-500 MB. The synthesis and vision-v2 understate the real resource requirement by citing only the idle figure. For resource planning, use the ops numbers: 100 MB idle, 500 MB peak.

### 1.5 Skeptic's Timeline vs. Build Plan Phasing

**Build plan:** Phase 3A Foundation (existing issues), then Phase 3B CockroachDB, then Phase 4A Meilisearch. Implies weeks 1-4 for foundation, weeks 5-8 for CRDB, weeks 9-12 for Meilisearch.

**Vision-v2:** Phase 0 Foundation (1-2 weeks), Phase 1 Meilisearch (weeks 3-4), Phase 2 Skill APIs (weeks 5-8).

**ultrathink-skeptic.md (Section 6):** Provides an "honest timeline" suggesting Meilisearch integration alone takes 3-6 weeks, not 1-2. Estimates 3 months before any skill-specific APIs are partially built. States "What you will NOT have in 3 months: Any skill actually calling hive-server."

**Resolution:** The skeptic's timeline is more realistic. Vision-v2's "weeks 3-4" for Meilisearch integration is aggressive. The skeptic correctly identifies that index schema design, sync reliability, query preprocessing, and CI integration each have hidden complexity. A more honest estimate is 3-4 weeks for Meilisearch integration (not including skill-specific work).

### 1.6 Step 4A.3 Dependency on 3B.1

**Build plan:** Step 4A.3 (Search Sync Pipeline) depends on Step 3B.1 (CockroachDB Store). The prompt text says "keeps Meilisearch in sync with the primary store (CockroachDB or SQLite)."

**Original review (review.md, Section 3.2):** Correctly identifies this as artificial: "The sync pipeline should work against any Store implementation, including SQLite. This unlocks Meilisearch integration earlier."

**Vision-v2:** Assumes Meilisearch integration happens before CRDB.

**Resolution:** The dependency is artificial and should be removed. The SyncStore wrapper embeds `store.Store`, which works with any backend.

### 1.7 Events Table: Useful vs. Premature

**Vision-v2 (Section 6.1):** Includes events and sessions tables as Phase 0 work. Describes the events table as "the cross-skill glue."

**ultrathink-skeptic.md (Section 1.2):** States the events table is "a solution without a query" and recommends adding it "when the first actual consumer exists."

**ultrathink-architect.md (Section 3.2):** Includes the events table in Phase 0 additions without questioning its utility.

**ultrathink-ops.md:** Treats events as part of the planned schema without comment.

**Resolution:** The skeptic makes a valid point: no skill currently emits events. However, the table itself is trivially cheap (one CREATE TABLE statement). The risk is not the table; it is over-designing around it (indexes, Meilisearch sync, event type taxonomies). **Add the table; do not build infrastructure around it until events are actually being recorded.**

---

## 2. Unsupported Claims

### 2.1 "200ms Injection Latency Budget"

**Source:** Build plan (resolved open question #4), vision-v2 (Section 4.2).

**Problem:** This number appears in multiple documents but is never derived from measurement. The ultrathink-architect.md (Section 2.3) provides a latency budget breakdown totaling <35ms, which is well under 200ms. The ultrathink-skeptic.md (Section 2.1) calls it "invented rather than measured."

**Assessment:** The 35ms estimate from the architect is more credible (based on component-level analysis), but it also has no measurement behind it. The 200ms budget is likely comfortable for the actual implementation. The real risk is not the budget itself but the absence of any mechanism to measure or enforce it, as the original review (Section 11) notes.

### 2.2 "Skills Will Call hive-server"

**Source:** Vision-v2 (Sections 3.3-3.5), synthesis (Section 6).

**Problem:** Every document assumes GSD, Superpowers, and Allium will integrate with hive-server's APIs. None of them specify how this integration happens. The ultrathink-skeptic.md (Section 2.2) identifies this as "magical thinking":

- GSD is a prompt template system that writes markdown files. It does not make HTTP calls.
- Superpowers is a hooks-based system with no persistent state layer. Its skills are markdown files.
- Allium is a specification language with a parser. It has no API client.

The integration path requires either: (a) modifying each skill's source code, (b) building hive-local as a proxy, or (c) building hive-plugin as a Claude Code lifecycle hook. None of these exist, and none are designed in detail.

**Assessment:** This is the single most important gap in the entire research effort. The server-side API is well-designed, but the client-side integration path is completely unspecified. The devex analysis (ultrathink-devex.md, Section 3) provides the most concrete proposal: a `hive` CLI tool with subcommands. The skeptic's recommendation (Section 7.1) of a bash script wrapping curl is the fastest path to validation.

### 2.3 "allium-cli Produces JSON ASTs"

**Source:** Vision-v2 (Section 3.5), synthesis (perm-allium references).

**Problem:** The allium.md research document describes the tooling as including "Rust parser producing a typed AST and JSON output." However, the skeptic (Section 2.3) questions whether this is currently shipping: "Does allium-cli currently produce JSON ASTs?"

**Assessment:** The allium.md document does describe this capability exists in allium-tools. The claim appears to be supported by the research. However, the end-to-end integration (allium-cli -> JSON -> hive-server API -> Meilisearch index) is a multi-step pipeline where any link could be missing or broken.

### 2.4 "SQLite Handles the Concurrency"

**Source:** Vision-v2 (Section 2.1), synthesis (Section 7.1).

**Problem:** The ultrathink-skeptic.md (Section 3.3) provides the counterpoint: SQLite with `MaxOpenConns=1` serializes ALL operations (reads AND writes) through a single connection. The ultrathink-ops.md (Section 4) provides benchmarks: 500-2000 writes/second, which is adequate, but notes that a burst of 20 task completions from a GSD wave will queue.

**Assessment:** SQLite is genuinely fine for 1-10 concurrent agents. The claim is supported but with the important caveat from ops that `MaxOpenConns` should be increased (WAL mode supports concurrent readers) and a `busy_timeout` PRAGMA should be set. The current `MaxOpenConns=1` is unnecessarily conservative.

### 2.5 Meilisearch "~50 MB RAM, Lowest Infrastructure Cost"

See Contradiction 1.4 above. The idle figure is defensible. The total system estimate of "~200 MB RAM combined" ignores indexing peaks. This is a soft misrepresentation, not a lie, but it could lead to under-provisioned containers.

---

## 3. Inaccuracies

### 3.1 Meilisearch Capabilities: Correctly Described

The meilisearch.md research document is thorough and accurate. Key claims verified:

- 10-word query limit (Section 9.2): Correctly described as a hard limitation.
- Asynchronous writes (Section 2.1): Correctly described.
- MIT Community Edition license (Section 1): Correctly described.
- Single-node only for CE (Section 9.2): Correctly described.
- Go SDK mocks available (Section 4.6): Correctly described.

One minor inaccuracy: the synthesis states "Meilisearch does not have recency weighting" (Section 2.1 referencing the skeptic). This is correct -- Meilisearch's ranking rules do not include time-based relevance. Custom ranking rules can include a numeric timestamp field to approximate recency, but true time-decay scoring is not built in.

### 3.2 CockroachDB Limitations: Accurately Represented

The cockroachdb.md research document is thorough and accurate. Key claims verified:

- ~40% PostgreSQL compatibility (Section 3): Correctly described.
- No LISTEN/NOTIFY (Section 9): Correctly described.
- Serializable isolation default requiring client-side retries (Section 9): Correctly described.
- License change to proprietary (Section 13): Correctly described with accurate tier details.

### 3.3 Gel DB Features: Correctly Understood

The gel-db.md research document is thorough and accurate. Key claims verified:

- 1 GB minimum RAM (Section on architecture): Correctly described.
- PostgreSQL backend requirement (Section on architecture): Correctly described.
- Go client less mature than TypeScript (Section on trade-offs): Correctly described.
- SQL support in Gel 6.0+ (Section on EdgeQL): Correctly described.

The build plan's resolved open question #1 ("Gel DB MUST NOT use CockroachDB as its PostgreSQL backend") is accurate -- Gel relies on PostgreSQL features CRDB does not support.

### 3.4 hive-server Current Codebase: Partially Outdated

The hive-server-current.md research document is accurate for the data models, API routes, and overall architecture. However:

- **Structure section:** Shows `k8s/` directory, which no longer exists.
- **Project structure:** Does not show the `script/` directory, which exists with bootstrap, setup, test, server, and docs scripts.
- **Huma v2:** The document does not mention Huma v2, but the codebase already uses it. The build plan step 3A.5 (Huma migration) is already done.
- **`test/` directory:** Not mentioned in the current state doc, but `test/e2e/` exists (suggesting Step 3A.6 may also be done or partially done).

The overall effect is that the research was conducted against an earlier snapshot of the code. This causes the build plan to include steps that are already completed, wasting implementation time if followed literally.

---

## 4. Missing Pieces

### 4.1 The Integration Layer (CRITICAL)

No document adequately addresses how agents actually invoke hive-server. The devex analysis (ultrathink-devex.md) comes closest with a concrete `hive` CLI tool definition (Section 3) and MCP vs CLI discussion. But even it does not provide:

- A concrete implementation plan for hive-local
- How hive-local maps to Claude Code's MCP tool system
- How the pre-prompt hook for memory injection is registered and invoked
- How hive-plugin bridges the TypeScript/MCP world to the Go/HTTP world
- Token cost estimates for the tool invocation overhead

This is what the skeptic correctly identifies as "the product" -- not hive-server itself.

### 4.2 Data Quality and Session Summary Strategy

No document defines:

- What constitutes a good session summary
- How session end is detected (Claude Code has no clean lifecycle event for this)
- Whether summaries are agent-generated, human-written, or LLM-generated
- Minimum quality requirements for stored data
- Whether hive-server should validate summary content

The devex analysis (Section 5) asks the right questions but does not answer them.

### 4.3 Migration Path for Existing Data

If developers adopt hive-server, they may want to import existing `.planning/` files, Superpowers plans, or Allium specs. No document discusses bulk import, data migration, or backfill strategies.

### 4.4 Multi-Tenancy Details

The vision-v2 references `repo` as a scoping field but does not define the multi-tenancy model:

- Is data isolated per repo? Per agent? Per user?
- Can agents from different repos access each other's memories?
- What are the default visibility rules?

The architect (Section 1.3) adds `Scope` and `Repo` fields to the MemoryEntry model but does not define the access control semantics.

### 4.5 Backup and Recovery (Operational)

The ultrathink-ops.md (Section 2.9) identifies this as a critical gap: "There are no backup procedures. There is no backup code, no backup documentation, no backup automation." The SQLite file is the single point of data loss. No other document addresses this.

### 4.6 Monitoring and Alerting

The ultrathink-ops.md (Section 6) identifies the complete absence of observability. No Prometheus metrics, no structured logging with request tracing, no alerting. The original review (Section 11) notes the lack of injection latency metrics.

### 4.7 SQLite Migration Framework

Both the ultrathink-ops.md (Section 2.8) and the original review (Section 2.3) identify that the current schema is applied as a single `const schema` string. Adding any ALTER TABLE statements requires a proper migration framework (goose is recommended). This must happen before any schema changes beyond CREATE TABLE.

---

## 5. Consensus Points

Every perspective agrees on the following. These are the highest-confidence recommendations.

### 5.1 SQLite First, Defer CRDB

All four ultra-think perspectives, the synthesis, and vision-v2 agree: SQLite is adequate for the current scale. CockroachDB is unnecessary until multi-instance or multi-region deployment is needed.

### 5.2 Meilisearch Is the Highest-Value Addition

All documents agree Meilisearch provides the best value-per-complexity ratio. It is the first new dependency worth adding.

### 5.3 Graceful Degradation Is Non-Negotiable

Every backend beyond SQLite must be optional. If Meilisearch is down, search returns 503 but CRUD continues. If Gel is not configured, graph endpoints return 404. This principle appears in every document without exception.

### 5.4 Interface-First Design

The build plan, vision-v2, synthesis, and architect all agree: define `Store`, `Searcher`, and (future) `GraphStore` interfaces before implementations. Use NoopSearcher for testing and gradual rollout.

### 5.5 Enhance Skills, Do Not Replace Them

Vision-v2 (Section 2.1, principle 5), synthesis (Section 1.5), and the skeptic all agree: hive-server provides durable state and search. Skills keep their own planning, orchestration, and workflow logic. Building a competing project manager is wrong.

### 5.6 The Single-Tool Pattern

All documents that discuss agent interaction agree: one `hive` tool with subcommands. This is non-negotiable for token efficiency.

### 5.7 No Vector/Embedding Search (No GPU)

The github-issues.md (k8s#58), synthesis, and vision-v2 all agree: no vector search, no GPU. Keyword + typo-tolerant search via Meilisearch is sufficient. Hybrid search via external embedder is the escape hatch if ever needed.

---

## 6. Key Disagreements

### 6.1 When to Add Meilisearch

**Pro (vision-v2, synthesis, architect, devex):** Meilisearch is Phase 1, weeks 3-4. It addresses the #1 gap (cross-session memory retrieval) and is low-cost.

**Against (skeptic):** "For a solo developer with 3 agents, SQLite LIKE queries on a few hundred records will return in under 1ms." Recommends skipping Meilisearch entirely until LIKE queries become too slow.

**Who has the stronger argument:** The skeptic is right about current scale but wrong about trajectory. The value of Meilisearch is not speed on small datasets -- it is typo-tolerant fuzzy search across heterogeneous content types (memories, sessions, tasks, specs). `LIKE '%race condition%'` will not find "race condition" stored as "race-cond" or "concurrency bug." However, the skeptic is correct that Meilisearch should not be added until there is data to search and a tool to invoke searches. **Add Meilisearch after the `hive` CLI tool exists and agents are actually storing data,** not before.

### 6.2 Skill-Specific API Namespaces: Now or Later

**Pro (vision-v2, synthesis, architect):** Design and reserve `/api/v1/gsd/`, `/api/v1/superpowers/`, `/api/v1/specs/` namespaces. Build structured tables and endpoints for each skill.

**Against (skeptic):** "Building 16 new endpoints with custom SQLite tables for three skills that do not yet integrate with hive-server is speculative infrastructure. You are designing a restaurant menu before anyone has walked in the door."

**Who has the stronger argument:** The skeptic, decisively. No skill currently calls hive-server. Building 16 endpoints for consumers that do not exist is waste. However, the devex analysis (Section 5, Story 5) shows that the existing memory API can already serve many use cases (drift tracking via memory keys like `drift/auth-spec/2026-03-09`). **Build skill-specific APIs only when the generic memory/tasks/events APIs prove insufficient for specific skill needs.**

### 6.3 Events Table: Proactive vs. Reactive

**Pro (vision-v2, architect):** Add events and sessions tables now as Phase 0 work. They are cheap and provide extension points.

**Against (skeptic):** Add them when the first consumer exists.

**Who has the stronger argument:** The architect, narrowly. A CREATE TABLE statement costs nothing. The events table provides a standardized extension point even if it starts empty. The risk the skeptic identifies (over-designing around it) is valid, but that risk is managed by discipline, not by omitting the table.

### 6.4 Project Layout Refactor (#20): Now or Never

**Pro (build plan, vision-v2):** Rename `cmd/app/` to `cmd/hive-server/`, extract `internal/model/`, create `internal/server/`.

**Against (skeptic, Section 7.2):** "The current layout works. `cmd/app/` vs `cmd/hive-server/` does not matter for a single-binary application."

**Who has the stronger argument:** Mixed. The model extraction is genuinely useful -- handlers currently import types from `internal/store/`, creating tight coupling. Extracting `internal/model/` breaks this coupling and enables multiple store backends. The `cmd/app/` rename is cosmetic. **Do the model extraction; defer the cosmetic renames.**

---

## 7. The Final Recommendation

### 7.1 What Is Already Done (Do Not Repeat)

Based on codebase verification:

- Step 3A.1: Remove k8s/ -- DONE
- Step 3A.2: Scripts -- DONE
- Step 3A.5: Huma v2 migration -- DONE
- Step 3A.6: E2E test scaffold -- PARTIALLY DONE (test/e2e/ directory exists)

### 7.2 Prioritized Action List

**Priority 1: The Integration Layer (Week 1)**

Nothing in hive-server delivers value until agents can call it. Build a minimal `hive` CLI tool first.

1. **Create a `hive` shell script** (or minimal Go binary) that wraps curl calls to hive-server. Subcommands: `memory set`, `memory get`, `memory list`, `session submit`, `session list`, `search` (falls back to `memory list` without Meilisearch). This is the devex recommendation (ultrathink-devex.md, Section 3) and the skeptic's minimum viable product (Section 7.1).

2. **Register it as a Claude Code tool** (via CLAUDE.md tool definition or MCP). This is the absolute minimum for agents to use hive-server.

3. **Add a pre-prompt hook** (or session-start hook) that retrieves the last 3-5 session summaries and active tasks, then injects them as context. This is the memory injection system, v0. No keyword extraction, no Meilisearch, no token budget management.

**Priority 2: Schema Extensions (Week 1, parallel with Priority 1)**

4. **Add sessions table** to SQLite schema. Schema as defined in vision-v2 Section 6.1.

5. **Add events table** to SQLite schema. Same source.

6. **Add `repo` and `session_id` columns** to the memory table. The devex analysis (Section 9.1) identifies this as a critical gap for cross-project queries. This requires a proper migration mechanism -- adopt goose (or at minimum, add `ALTER TABLE IF NOT EXISTS`-style guards).

7. **Add session and event CRUD endpoints** (4 new endpoints: POST/GET for each).

**Priority 3: Store Interface Cleanup (Week 2)**

8. **Extract `internal/model/` package** from `internal/store/`. Move all data types (MemoryEntry, Task, Agent, etc.) and sentinel errors. Update all imports. This is Step 3A.3's most valuable sub-step.

9. **Split Store interface** into domain-specific interfaces per the architect's design (Section 1.3): `MemoryStore`, `TaskStore`, `AgentStore`, `EventStore`, `SessionStore`. Compose into a single `Store` interface. This enables smaller test mocks and prepares for future backends.

10. **Add `Ping(ctx)` method** to Store interface and implement for SQLite. Wire into `/ready` endpoint (currently returns 200 unconditionally -- the ops analysis, Section 2.2, identifies this as a critical gap).

**Priority 4: Error Messages (Week 2, parallel)**

11. **Improve error messages with recovery guidance.** The devex analysis (Section 4) provides concrete examples. Touch ~15 error sites. Include current state, allowed transitions, and suggested next actions in error responses. This directly improves agent recovery behavior.

**Priority 5: Meilisearch Integration (Weeks 3-5)**

12. **Define Searcher interface** with NoopSearcher. The architect's interface design (Section 1.4) is the right one: `Search`, `Index`, `Delete`, `EnsureIndex`, `Healthy`.

13. **Implement MeiliSearcher** backend. Handle the 10-word query limit with keyword extraction. Handle filter construction safely (the original review, Section 12.4, identifies an injection risk in `fmt.Sprintf("agent_id = '%s'", req.AgentID)`).

14. **Implement SyncStore wrapper** for async indexing. Use a bounded worker pool, not unbounded goroutines (original review, Section 6.5).

15. **Add search endpoints** and wire Meilisearch into memory injection.

16. **Add reconciliation job** (periodic full re-index from SQLite).

**Priority 6: Validation and Hardening (Weeks 4-6, parallel)**

17. **Add request body size limits** (ops Section 2.3, 2.6).

18. **Add rate limiting middleware** (ops Section 2.6).

19. **Add field size validation** (ops Section 5).

20. **Implement SQLite backups** (ops Section 2.9). At minimum, a periodic file copy with WAL checkpoint.

21. **Add request logging/audit middleware** (ops Section 5).

**Deferred (build when triggered):**

- Skill-specific API namespaces (GSD, Superpowers, Allium): Build when the first skill actually integrates with hive-server and the generic memory/tasks/events APIs prove insufficient.
- Gel DB: Build when graph queries are demonstrably needed (3+ projects, 10+ specs, 50+ skills).
- CockroachDB: Build when multi-instance deployment is needed or SQLite p99 write latency exceeds 500ms under measured production load.
- MasterClaw / LLM synthesis: Build when simple relevance ranking proves insufficient for memory injection.
- Project layout cosmetic renames (`cmd/app/` to `cmd/hive-server/`): Low priority, do whenever convenient.

### 7.3 What to Throw Away

- **The 33-step build plan** in its current form. It was written against an earlier codebase version and includes completed steps. Replace with the prioritized action list above.
- **Phase 3B (CockroachDB migration)** as a near-term priority. It is well-designed but premature.
- **Phase 4B (Gel DB)** as a near-term priority. Same assessment.
- **Phase 4C (MasterClaw)** as a near-term priority. The OpenClaw CVEs, infrastructure complexity, and the fact that skills already have orchestration make this the least valuable addition.
- **Phase 6 (LLM-Enabled Project Manager).** GSD already has this capability. Building a competing one is wrong.

### 7.4 The Decision Framework

When deciding what to build next, apply this filter (synthesized from vision-v2 Section 9.5 and synthesis Section 10.7):

1. **Does an agent need this today?** If no agent is currently blocked by the absence of this feature, do not build it.
2. **Can the existing memory/tasks/events APIs serve this need?** If yes, use them with conventions (key prefixes, tags) rather than building custom endpoints.
3. **Is the workaround more than 30 seconds of manual effort?** If no, the feature is not worth a new dependency.
4. **Can we collect the data now in SQLite and add the fancy database later?** Usually yes. Do that.
5. **Does this enhance an existing skill or compete with it?** If it competes, do not build it.

### 7.5 The One-Sentence Summary

Build the `hive` CLI tool first, add session/event CRUD to the server, hook it into one Claude Code session, measure whether cross-session memory helps, and let that measurement drive every subsequent decision.

---

## Appendix: Document Cross-Reference Matrix

| Topic                  | build-plan       | vision-v2       | synthesis       | review         | architect          | skeptic            | devex             | ops           |
| ---------------------- | ---------------- | --------------- | --------------- | -------------- | ------------------ | ------------------ | ----------------- | ------------- |
| CRDB timing            | Phase 3B (early) | Phase 4 (defer) | Phase 4 (defer) | Honors #5      | Not discussed      | Agrees: defer      | Not discussed     | Agrees: defer |
| Meilisearch timing     | Phase 4A         | Phase 1         | Phase 1         | N/A            | Phase 1            | Skip until needed  | Phase 1           | Phase 1       |
| Store interface        | Monolithic       | Not specified   | Not specified   | N/A            | Split into domains | Split into domains | Not specified     | Not discussed |
| Skill-specific APIs    | Phase 7          | Phase 2         | Phase 2         | N/A            | Phase 2            | Do not build yet   | Build when needed | Not discussed |
| Events table           | Not explicit     | Phase 0         | Phase 0         | N/A            | Phase 0            | Wait for consumer  | Phase 0           | Phase 0       |
| MasterClaw             | Phase 4C         | Deferred        | Not recommended | Risk noted     | Not discussed      | Do not build       | Not discussed     | Not discussed |
| CLI tool / integration | Not addressed    | Assumed exists  | Assumed exists  | Not addressed  | Not addressed      | #1 priority        | Detailed design   | Not addressed |
| Backups                | Not addressed    | Not addressed   | Not addressed   | Not addressed  | Not addressed      | Not addressed      | Not addressed     | Critical gap  |
| Error messages         | Not addressed    | Not addressed   | Not addressed   | Injection risk | Not addressed      | Not addressed      | Detailed design   | Not addressed |
| Huma v2 status         | Step 3A.5 (todo) | Assumes done    | N/A             | N/A            | N/A                | Already done       | N/A               | N/A           |
