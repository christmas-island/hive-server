# Final Review v2: Quality Gate Before Implementation

**Date:** 2026-03-09
**Status:** Quality gate review of v3 documents before build execution
**Documents reviewed:** vision-v3.md, build-plan-v2.md, skill-replacement-analysis.md, plus all prior context (17 documents total), plus actual codebase verification
**Method:** Full cross-document analysis with codebase verification and technology reference validation

---

## 0. Executive Summary

The v3 documents represent a substantial improvement over the v2 cycle. The three critical corrections (CockroachDB as Phase 0, skill replacement framing, all databases active from the start) are well-argued and correctly motivated. The build plan is the most detailed and executable plan yet produced. The skill replacement analysis is the most honest document in the entire corpus.

However, a fundamental tension exists that the documents do not fully resolve: **the vision-v3 says "replace skills entirely" while the skill-replacement-analysis says "skills are 40-85% prompt engineering that no server can replace."** These two documents were produced on the same day and directly contradict each other. The build plan sides with the vision (it builds all the replacement APIs) but the analysis says those APIs can only replace 15-35% of what the skills do.

The plan is buildable. It is not yet ready to execute without addressing six specific issues documented below.

---

## 1. Contradictions Between v3 Documents

### 1.1 Vision vs. Skill Replacement Analysis: The Central Contradiction (CRITICAL)

**Vision-v3 Section 0, Wrong #2:** "hive-server INCORPORATES the capabilities of GSD, Superpowers, and Allium as API-backed services, so that an MCP plugin talking to hive-server can do everything those skills do. The skills become unnecessary, not enhanced."

**Vision-v3 Section 1.1:** "Hive-server is not a coordination hub. It is not a state store that skills call. It is the platform that replaces standalone skills entirely."

**Skill-replacement-analysis Section 0 (Executive Summary):** "None of the three skills can be fully replaced by server-side infrastructure. Each skill is 40-60% prompt engineering and LLM reasoning that no API can replicate."

**Skill-replacement-analysis Section 1.5:** "No. GSD is approximately 40% prompt engineering, 25% workflow orchestration, 20% state management, 15% data persistence. hive-server can replace the 35% that is state management + data persistence."

**Skill-replacement-analysis Section 2.5:** "No. Superpowers is approximately 55% prompt engineering [...] hive-server can replace the 15% that is state management + search/discovery."

**Skill-replacement-analysis Section 3.5:** "No. Allium is approximately 50% specification language, 25% prompt engineering [...] hive-server can replace the 10% that is storage/querying."

**Skill-replacement-analysis Section 4.5 (final paragraph):** "The honest answer for all three skills: No, they cannot be made unnecessary. They can be made better by extracting their fragile state management into a proper backend. The 60-85% that is prompt engineering, workflow orchestration, and platform integration is irreplaceable by any server architecture."

These documents cannot both be right. The vision says the skills "become unnecessary." The analysis -- produced as input to the vision -- says they cannot become unnecessary. The vision acknowledges this in Section 8.2 ("The skills become content, not dependencies") but this framing papers over the contradiction rather than resolving it.

**What this means for the build plan:** The build plan builds 66 endpoints across 11 API domains to "replace" skills that the analysis says are 60-85% irreplaceable. The planning domain (16 endpoints), orchestration domain (5 endpoints), and specs domain (6 endpoints) assume the server can provide capabilities that the analysis explicitly classifies as "client-side" (plan creation, wave dependency analysis, spec authoring, drift classification). The server can store and query the outputs of these activities, but it cannot perform them.

**Resolution needed:** The vision must pick one framing and commit to it. Either:

- (a) "Skills become thinner clients that use hive-server for state" (the analysis position) -- which means the planning and orchestration APIs are simpler (CRUD for plans and tasks, not workflow-aware endpoints that "replace" GSD's orchestration)
- (b) "Skills become unnecessary because the MCP plugin + hive-server does everything" (the vision position) -- which requires explaining how the 40-65% that is prompt engineering gets into hive-server (the `skills.content` field is mentioned but no strategy for maintaining that content exists)

My assessment: Position (a) is correct. The skill-replacement-analysis is the more rigorous document. The vision over-promises. The build plan should be adjusted to build the APIs that support thin-client skills, not the APIs that attempt to replace skills outright. Practically, this means the planning and orchestration endpoints are still valuable (they replace STATE.md, ROADMAP.md, and .planning/ files as persistent queryable state), but the framing should be "backend for skills" not "replacement for skills."

### 1.2 Build Plan vs. Vision: Step Count and Scope Mismatch

**Vision-v3 Section 4.13:** 66 total endpoints across 11 domains.

**Build-plan-v2 Step Count Summary:** 45 steps producing 53 new endpoints (+ 14 existing = 67 total).

Minor counting discrepancy (66 vs 67) -- likely a rounding issue in the vision's table. Not material.

However, the vision's API surface in Section 4 includes endpoints not detailed in the build plan:

- `POST /api/v1/memory/bulk` -- appears in vision Section 4.2 and build plan Step 6.5
- `POST /api/v1/memory/inject` -- appears in vision Section 4.2 and build plan Step 2.8
- `GET /api/v1/analytics/velocity` -- appears in vision Section 4.12 and build plan Step 6.1
- `GET /api/v1/analytics/drift` -- appears in vision Section 4.12 and build plan Step 4.6

These all map correctly. The build plan covers the vision's API surface.

### 1.3 Build Plan vs. Vision: Database Primacy

**Vision-v3 Section 2.4:** "Source of truth: CockroachDB, always." All writes go to CRDB synchronously.

**Build-plan-v2:** Implements both SQLiteStore and CRDBStore, selects based on `DATABASE_URL`. SQLite retained for local dev and fast tests.

This is consistent and correct. The build plan operationalizes the vision's CRDB-first stance while preserving SQLite for development ergonomics.

### 1.4 Build Plan Step Dependencies: Two Issues

**Issue 1: Step 2.5 (search HTTP endpoints) lists dependency on 2.1 and 2.3, but not 2.2.** The search endpoints need a working Searcher implementation (2.2) to test meaningfully. The NoopSearcher (2.1) returns errors. The SyncStore (2.3) wraps the Store. Neither provides actual search functionality. Step 2.5 should depend on 2.1 for the interface definition but can only be fully tested after 2.2. The dependency is soft (you can write the handlers against the interface), but the acceptance criteria ("search endpoints return results when Meilisearch is available") requires 2.2.

**Issue 2: Step 3.6 (index plans in Meilisearch) correctly depends on Phase 2 complete and 3.2. But it also modifies `internal/store/sync.go` and `internal/search/reconciler.go`, which were created in Steps 2.3 and 2.6 respectively. This is noted but should be explicit: 3.6 depends on 2.3 and 2.6 specifically, not just "Phase 2."**

Both issues are minor -- the parallelization diagrams are correct in practice even if the dependency lists are slightly imprecise.

### 1.5 Skill Replacement Analysis vs. Vision API Surface

The skill-replacement-analysis classifies capabilities as "server-side," "client-side," or "hybrid." The vision's API surface includes endpoints for capabilities the analysis classifies as server-side or hybrid. Cross-checking:

| Vision Endpoint                    | Skill Capability             | Analysis Classification | Alignment                                                                                                |
| ---------------------------------- | ---------------------------- | ----------------------- | -------------------------------------------------------------------------------------------------------- |
| `hive.planning.create_project`     | G1: Project init             | Client-side             | **Misaligned** -- the server stores the project, but the "initialization questionnaire" is LLM reasoning |
| `hive.planning.create_plan`        | G7: Plan creation            | Client-side             | **Misaligned** -- the server stores the plan, but creating it is LLM reasoning                           |
| `hive.orchestration.schedule_wave` | G9: Wave dependency analysis | Hybrid                  | Aligned -- dependency graph is server-side                                                               |
| `hive.orchestration.claim_plan`    | G10: Execution state         | Hybrid                  | Aligned -- state tracking is server-side                                                                 |
| `hive.skills.discover`             | S20: Description matching    | Hybrid                  | Aligned -- search is server-side                                                                         |
| `hive.specs.submit_drift_report`   | A21: Weed agent              | Hybrid                  | Aligned -- report storage is server-side                                                                 |
| `hive.specs.impact_analysis`       | Gel graph query              | Server-side             | Aligned                                                                                                  |

The "misaligned" cases are not wrong per se -- the server stores and retrieves plans/projects -- but the vision's framing suggests the server is performing the creation ("replace GSD's project management workflow"), when in reality the LLM client creates the content and the server stores it. This is the Section 1.1 contradiction manifesting at the API level.

---

## 2. Previous Review Issues: Were They Addressed?

Going through final-review.md findings one by one:

### 2.1 Contradiction 1.1 (Build Plan vs. Actual Codebase)

**Previous finding:** Steps 3A.1 (rm k8s/), 3A.2 (scripts), 3A.5 (Huma v2) were already done.

**Addressed in v3?** YES. Build-plan-v2 Section "Current State (verified by code inspection)" correctly states:

- "Huma v2 already integrated (handlers use `humachi.New`)"
- "`k8s/` directory still exists (managed externally, should be removed per #10)"

Wait -- the build plan says k8s/ "still exists," but codebase verification shows `k8s/` DOES NOT EXIST. The build plan's current state inventory is wrong on this point. Step 0.1 (remove k8s/) may be unnecessary.

**Codebase verification result:** `k8s/` does not exist. `script/` exists with bootstrap, setup, test, server, docs, and e2e/integration subdirectories. Huma v2 is integrated (confirmed by grep of humachi imports). `cmd/app/` still exists (not yet renamed). `internal/model/` does not exist. `test/e2e/` exists with 9 test files.

**Status: PARTIALLY ADDRESSED.** The plan correctly identifies Huma v2 as done and starts from the right place for model extraction and CRDB migration. But the k8s/ claim is wrong -- it is already deleted. Step 0.1 should be removed or converted to "verify k8s/ is gone and clean up any remaining references."

### 2.2 Contradiction 1.2 (CockroachDB Timing)

**Previous finding:** Build plan placed CRDB as Phase 3B, vision-v2 deferred to Phase 4, synthesis said defer.

**Addressed in v3?** YES, decisively. Vision-v3 Section 0, Wrong #1: "CockroachDB is already running in production. It is not Phase 4." Build-plan-v2 places CRDB as Phase 0. This is the correct resolution given the production infrastructure reality.

**Status: RESOLVED.**

### 2.3 Contradiction 1.3 (Store Interface Design)

**Previous finding:** Architect and skeptic recommended interface segregation. Build plan and vision-v2 used monolithic Store.

**Addressed in v3?** YES. Build-plan-v2 Step 0.3: "Split Store interface into composed domain interfaces." Step 0.3 creates `internal/store/interfaces.go` with MemoryStore, TaskStore, AgentStore, plus composite Store interface. Phase 1 adds EventStore and SessionStore. Phase 3 adds PlanningStore. Phase 4 adds SkillStore and SpecStore.

**Status: RESOLVED.** The architect's design was adopted.

### 2.4 Contradiction 1.4 (Meilisearch RAM Numbers)

**Previous finding:** Synthesis and vision-v2 understated Meilisearch RAM by citing only idle figure (~50 MB).

**Addressed in v3?** NOT EXPLICITLY. Vision-v3 does not cite specific RAM numbers for Meilisearch. It focuses on the role of Meilisearch (secondary index, async, graceful degradation) without making resource claims. The issue is avoided rather than corrected.

**Status: AVOIDED.** The misleading number is no longer present, but no accurate number replaces it. For resource planning, the ops analysis numbers (100 MB idle, 500 MB peak during indexing) should be referenced.

### 2.5 Contradiction 1.5 (Timeline Realism)

**Previous finding:** Skeptic said timelines were 2-3x too aggressive.

**Addressed in v3?** YES, partially. Build-plan-v2 timeline: "14-17 weeks for a single developer with LLM assistance, accounting for the skeptic's observation that integration work takes 2-3x longer than expected." This is significantly more realistic than the v2 estimates. The Phase 2 (Meilisearch) estimate of "2-3 weeks" is better than v2's "weeks 3-4" but still potentially aggressive given the skeptic's 3-6 week estimate.

**Status: IMPROVED.** The timeline is more honest. The 14-17 week total is plausible for the described scope.

### 2.6 Contradiction 1.6 (Step 4A.3 Dependency on 3B.1)

**Previous finding:** Meilisearch sync pipeline had an artificial dependency on CockroachDB.

**Addressed in v3?** YES. The build-plan-v2 orders phases as: 0 (CRDB migration) -> 1 (events/sessions) -> 2 (Meilisearch). Meilisearch integration happens after CRDB is the primary store, so the dependency is natural, not artificial. The SyncStore wraps whatever Store is active (CRDB in production, SQLite in dev).

**Status: RESOLVED.** The phase ordering eliminates the artificial dependency.

### 2.7 Contradiction 1.7 (Events Table: Useful vs. Premature)

**Previous finding:** Skeptic said events table was "a solution without a query." Previous review recommended: "Add the table; do not build infrastructure around it until events are actually being recorded."

**Addressed in v3?** YES. Build-plan-v2 Step 1.1 adds the events table in Phase 1 (after CRDB migration). Step 3.4 records events for planning state transitions. The table is no longer empty from birth -- it gets populated by the planning domain in Phase 3.

**Status: RESOLVED.** The events table now has consumers (planning state transitions, skill invocation records).

### 2.8 Unsupported Claim 2.1 ("200ms Injection Latency Budget")

**Previous finding:** The 200ms budget was "invented rather than measured."

**Addressed in v3?** PARTIALLY. Vision-v3 Section 6.2 states: "All CRDB-only operations complete in <50ms. Search operations complete in <200ms. Graph operations complete in <500ms. These are p99 targets, not averages." These are labeled as targets, not measurements. The build plan does not include a step for measuring latency, but the numbers are at least framed as targets to validate rather than facts.

**Status: IMPROVED but still unvalidated.** Add a build plan step for latency benchmarking (could be part of Phase 6 hardening).

### 2.9 Unsupported Claim 2.2 ("Skills Will Call hive-server")

**Previous finding:** "This is the single most important gap in the entire research effort."

**Addressed in v3?** YES, differently than expected. Vision-v3 Section 8.1 explicitly scopes out the MCP plugin: "This document designs the server-side API. The MCP plugin (hive-plugin), the local proxy (hive-local), and the tool registration in Claude Code are separate projects." The vision no longer assumes skills call hive-server -- it assumes the MCP plugin replaces the skills entirely. The integration path is MCP plugin -> hive-server API, not skill -> hive-server API.

This addresses the gap by reframing it: the integration layer is not "modify skills to call hive-server" but "build an MCP plugin that makes skills unnecessary." Whether this is easier or harder depends on the scope of the MCP plugin, which is explicitly out of scope.

**Status: REFRAMED, NOT RESOLVED.** The integration layer is still unbuilt and undesigned. But the vision correctly identifies it as a separate project rather than pretending it does not exist.

### 2.10 Missing Piece 4.1 (The Integration Layer)

**Previous finding:** No document adequately addresses how agents actually invoke hive-server.

**Addressed in v3?** PARTIALLY. Vision-v3 Section 6 (The MCP Contract) defines what the MCP plugin needs from the server: JSON-in/JSON-out, self-describing errors, stable tool interface, OpenAPI spec, pagination, token-efficient responses. This is the server's half of the contract. The plugin's half remains undesigned.

**Status: HALF-ADDRESSED.** The server-side contract is well-defined. The client-side contract is out of scope.

### 2.11 Missing Piece 4.2 (Data Quality and Session Summary Strategy)

**Previous finding:** No document defines what constitutes a good session summary, how session end is detected, etc.

**Addressed in v3?** PARTIALLY. Vision-v3 Section 9 (Open Questions) lists "Session end detection" as question #3 and "Data quality validation" as question #4. The questions are identified but not answered.

**Status: ACKNOWLEDGED but UNRESOLVED.** The open questions section is an honest admission. These can be answered during implementation.

### 2.12 Missing Piece 4.3 (Migration Path for Existing Data)

**Previous finding:** No document discusses bulk import or data migration.

**Addressed in v3?** PARTIALLY. Vision-v3 Section 8.2 says: "GSD's agent definitions become skill records in hive-server [...] Superpowers' skill catalog becomes skill records in hive-server." Vision Section 9 question #2: "How is this content migrated into hive-server's skills table? Manual entry? Automated import? Who maintains it?" Identified but unanswered.

**Status: ACKNOWLEDGED but UNRESOLVED.**

### 2.13 Missing Piece 4.4 (Multi-Tenancy Details)

**Previous finding:** Multi-tenancy model is undefined.

**Addressed in v3?** YES. Vision-v3 Section 8.1: "Data is scoped by `repo` and `agent_id`. There is no user authentication, no team isolation, no organization hierarchy. The `HIVE_TOKEN` bearer auth is a shared secret. Multi-tenancy at the user/team level is a future concern." Build-plan-v2 out-of-scope section: "Multi-tenancy -- single-tenant (one developer, multiple agents) for now."

**Status: RESOLVED** by explicit scoping as single-tenant.

### 2.14 Missing Piece 4.5 (Backup and Recovery)

**Previous finding:** No backup procedures.

**Addressed in v3?** PARTIALLY. Vision-v3 Section 8.3: "Does not perform database backups (though it should -- see Phase 6)." Build-plan-v2 Phase 6 mentions "CRDB backup procedures" in vision Section 7.7 but the build plan's Phase 6 steps do not include a specific backup step. This is a gap.

**Status: PARTIALLY ADDRESSED.** CRDB has built-in backup capabilities, but no build plan step implements or configures them. Add a Step 6.x for configuring CRDB backup schedules.

### 2.15 Missing Piece 4.6 (Monitoring and Alerting)

**Previous finding:** Complete absence of observability.

**Addressed in v3?** YES. Build-plan-v2 Step 6.3: "Add Prometheus metrics endpoint" with request count, latency histogram, error rate, database connection pool stats. Step 6.4: "Add request audit logging middleware."

**Status: RESOLVED.**

### 2.16 Missing Piece 4.7 (Migration Framework)

**Previous finding:** No schema migration framework; single `const schema` string.

**Addressed in v3?** YES. Build-plan-v2 Step 0.6: "Add goose migration framework with embedded migrations." Uses `pressly/goose/v3` with `embed.FS` for migrations embedded in the binary.

**Status: RESOLVED.**

### Summary: Previous Review Findings

| Finding                                      | Status                                                        |
| -------------------------------------------- | ------------------------------------------------------------- |
| Build plan vs codebase (k8s/, Huma, scripts) | Partially addressed (k8s/ claim is wrong)                     |
| CockroachDB timing                           | Resolved                                                      |
| Store interface segregation                  | Resolved                                                      |
| Meilisearch RAM numbers                      | Avoided (no misleading number, no accurate one either)        |
| Timeline realism                             | Improved                                                      |
| Artificial dependency on CRDB                | Resolved                                                      |
| Events table premature                       | Resolved                                                      |
| 200ms latency budget                         | Improved, still unvalidated                                   |
| Skills will call hive-server                 | Reframed (MCP plugin replaces skills instead)                 |
| Integration layer missing                    | Half-addressed (server contract defined, client out of scope) |
| Session summary strategy                     | Acknowledged, unresolved                                      |
| Data migration                               | Acknowledged, unresolved                                      |
| Multi-tenancy                                | Resolved (single-tenant for now)                              |
| Backup and recovery                          | Partially addressed (acknowledged, no build step)             |
| Monitoring                                   | Resolved                                                      |
| Migration framework                          | Resolved                                                      |

---

## 3. Ultra-Think Concerns: Were They Addressed?

### 3.1 Architect's Hard Problems

**Problem 1 (Interface Design):** Recommended splitting Store into domain-specific interfaces.
**Addressed:** YES, in build-plan-v2 Step 0.3.

**Problem 2 (Schema Evolution):** Recommended goose migrations.
**Addressed:** YES, in build-plan-v2 Step 0.6.

**Problem 3 (Failure Mode Analysis):** Recommended health checks, graceful degradation, circuit breakers.
**Addressed:** YES. Vision-v3 Section 2.4 defines failure modes explicitly. Build-plan-v2 Step 0.8 wires Ping() into /ready. Graceful degradation (search returns 503, graph returns 404, core always works) is a first-class design principle.

**Problem 4 (Data Flow Architecture):** Recommended SyncStore wrapper with bounded worker pool.
**Addressed:** YES. Vision-v3 Section 2.4 includes the SyncStore code sample with bounded worker pool (not unbounded goroutines). Build-plan-v2 Step 2.3 details the implementation.

**Problem 5 (Deployment Topology):** Recommended considering multi-instance deployment.
**Addressed:** YES. CockroachDB as primary store enables multi-instance hive-server deployment. The vision does not require it for Phase 0 but enables it architecturally.

**Architect Status: All major concerns addressed.**

### 3.2 Skeptic's Warnings

**Warning 1 (Skill-specific APIs are premature):** "Building 16 new endpoints for consumers that do not exist."
**Addressed:** PARTIALLY. Build-plan-v2 builds skill-specific APIs in Phases 3-4, not Phase 0. The deferral is an improvement. But the APIs are still built without any consumer existing. The vision's "MCP plugin is out of scope" means these APIs will be built and sit unused until the plugin exists.

**Warning 2 (Events table is a solution without a query):**
**Addressed:** YES. Phase 3 records events for state transitions, giving the events table real consumers.

**Warning 3 (Meilisearch is not free):** Operational and sync complexity concerns.
**Addressed:** PARTIALLY. The SyncStore wrapper addresses sync complexity. The bounded worker pool addresses goroutine lifecycle. But the keyword extraction problem (the skeptic's "Bad keyword extraction makes search useless") is addressed only with "split on whitespace, remove stop words, deduplicate, limit to 10 terms" (Step 2.8). This is the minimum viable approach. The skeptic's concern about query preprocessing remains valid -- this will need iteration.

**Warning 4 (Timeline too aggressive):**
**Addressed:** YES. 14-17 weeks vs the original 8-12 weeks.

**Warning 5 (Build the CLI tool first):**
**Addressed:** NO. The build plan does not include a CLI tool or MCP plugin step. It builds the server and defers the client entirely. The skeptic's strongest recommendation -- "build the CLI tool first, validate the value proposition" -- is ignored. The out-of-scope declaration for the MCP plugin means the skeptic's concern about building a backend nobody uses is still active.

**Warning 6 (The 33-step build plan itself is a risk):**
**Addressed:** The plan grew to 45 steps. This is the opposite of the skeptic's advice to work from a "one-page prioritized backlog." However, the steps are well-structured with clear acceptance criteria and parallelization guidance, which partially mitigates the overhead.

**Skeptic Status: Partially addressed. The most important warning (build the client first) is explicitly ignored. The plan builds a 67-endpoint server with no consumer.**

### 3.3 DevEx Advocate's Priorities

**Priority 1 (Error messages with recovery guidance):**
**Addressed:** YES. Build-plan-v2 Step 1.4 is dedicated to this.

**Priority 2 (Structured startup logging):**
**Addressed:** YES. Build-plan-v2 Step 1.5.

**Priority 3 (hive CLI tool design):**
**Addressed:** NO. The CLI tool is out of scope. The MCP plugin is out of scope. The devex advocate's detailed tool design (Section 3) is not referenced in the build plan.

**Priority 4 (repo and session_id fields):**
**Addressed:** YES. Build-plan-v2 Step 0.5.

**Priority 5 (Migration path for GSD users):**
**Addressed:** NO. The devex advocate's migration checklist (Section 6) is not referenced. The vision says "skills become unnecessary" which implies no migration path -- you just switch to the MCP plugin.

**DevEx Status: Partially addressed. Server-side DevEx improvements (errors, logging, field additions) are covered. Client-side DevEx (the actual tool agents use) is out of scope.**

### 3.4 Ops Concerns

**Concern 1 (Backup and recovery):**
**Addressed:** PARTIALLY. Acknowledged in vision-v3 but no build plan step. CockroachDB has built-in `BACKUP` commands but these need configuration.

**Concern 2 (Request body size limits):**
**Addressed:** YES. Build-plan-v2 Step 6.2.

**Concern 3 (Monitoring and metrics):**
**Addressed:** YES. Build-plan-v2 Step 6.3 (Prometheus), Step 6.4 (audit logging).

**Concern 4 (SQLite single-writer):**
**Addressed:** YES, by migration to CockroachDB in Phase 0. SQLite single-writer is only relevant for local dev, where it is not a problem.

**Concern 5 (Rate limiting):**
**Addressed:** PARTIALLY. Build-plan-v2 out-of-scope section: "Rate limiting at the API level -- Phase 6 adds request size limits; rate limiting can be done at the ingress layer." The vision defers rate limiting to infrastructure (ingress/load balancer) rather than application layer. This is architecturally defensible for a Kubernetes deployment.

**Concern 6 (/ready endpoint health check):**
**Addressed:** YES. Build-plan-v2 Step 0.8 wires store.Ping() into /ready.

**Ops Status: Mostly addressed. Backup configuration is the remaining gap.**

---

## 4. Technical Accuracy

### 4.1 CockroachDB Features

**`crdbpgx.ExecuteTx()` for transaction retries:** CORRECT. The `cockroachdb/cockroach-go/v2/crdb/crdbpgx` package provides this function, which handles serializable isolation retry logic (error code 40001) with exponential backoff. Build-plan-v2 Step 0.7 correctly specifies this pattern.

**JSONB support:** CORRECT. CockroachDB supports JSONB with operators (`->`, `->>`, `@>`, `?`) and inverted indexes (`CREATE INVERTED INDEX`). Vision-v3 schema uses `JSONB` for tags, capabilities, payloads, and metadata. The inverted index on `memory(tags)` is correct CockroachDB syntax.

**`pgx/v5` in standalone mode with `pgxpool`:** CORRECT. This is the recommended driver for CockroachDB in Go. Using pgxpool (not database/sql) gives better connection management and native type support.

**`gen_random_uuid()`:** CORRECT. CockroachDB supports this function natively. It generates random UUIDs which avoid the hot-spot range problem that sequential UUIDs cause in CockroachDB's key-value architecture. Build-plan-v2 correctly notes this.

**`TIMESTAMPTZ` for timestamps:** CORRECT. CockroachDB prefers TIMESTAMPTZ over TIMESTAMP. The vision-v3 schema consistently uses TIMESTAMPTZ. This is a change from the current SQLite schema which uses TEXT for timestamps.

**`$1, $2` placeholders:** CORRECT. CockroachDB (and pgx) use PostgreSQL-style placeholders, not SQLite's `?`. Build-plan-v2 correctly notes this.

**One inaccuracy noted:** Build-plan-v2 Step 0.7 says "Use `gen_random_uuid()` for new UUIDs (or generate in Go with `google/uuid`)." Generating UUIDs in Go and inserting them as text is fine but loses the benefit of CockroachDB's native UUID type handling. The schema uses `UUID` column type with `DEFAULT gen_random_uuid()`, which means the database generates the UUID if the application does not provide one. Both approaches work, but the parenthetical "(or generate in Go)" could lead to UUID format mismatches if the Go-generated UUID is not properly formatted. Minor concern.

### 4.2 Meilisearch Capabilities

**10-word query limit:** CORRECT. Meilisearch limits queries to 10 words by default (configurable). Build-plan-v2 Step 2.2 correctly handles this with "truncate/extract keywords."

**Asynchronous indexing:** CORRECT. Writes return task IDs. The SyncStore pattern of fire-and-forget indexing jobs aligns with Meilisearch's async model.

**Index settings per index:** CORRECT. Each index can have different searchable/filterable/sortable attributes. Vision-v3 Section 2.2 defines per-index settings.

**Graceful degradation on unavailability:** Vision-v3 correctly states search returns 503 when Meilisearch is unavailable. The Meilisearch Go SDK returns errors when the server is unreachable, which the MeiliSearcher can translate to the appropriate response.

**One concern:** Vision-v3 Section 2.2 includes a `specs` index with `constructs` as a searchable field. Meilisearch indexes flat JSON documents -- nested objects are searchable but not in a structured way. If `constructs` is an array of objects, Meilisearch will tokenize the entire JSON string of each construct. This works for discovery ("find specs mentioning User entity") but not for structured queries ("find specs where entity User has field email"). This limitation is not called out. It should be, with a note that structured spec queries go through Gel or CRDB SQL, not Meilisearch.

### 4.3 Gel DB Features

**Go client (`gel-go`):** CORRECT. The package exists at `github.com/geldata/gel-go` with the API described in the technology brief.

**1 GB minimum RAM:** CORRECT. Gel server requires approximately 1 GB RAM minimum.

**PostgreSQL backend:** CORRECT. Gel requires PostgreSQL as its storage backend.

**Gel MUST NOT use CockroachDB as its PostgreSQL backend:** CORRECT. This was identified in the previous review and remains accurate. Gel relies on PostgreSQL features (extensions, specific SQL syntax) that CockroachDB does not support. Build-plan-v2 implicitly handles this by treating Gel as a separate database deployment.

**Back-links with `.<` syntax:** Vision-v3 Section 5.3 schema uses `multi implemented_by := .<implements[is Plan]` which is correct Gel/EdgeQL backlink syntax.

**`UNLESS CONFLICT ON` for upserts:** Build-plan-v2 Step 5.2 mentions `INSERT ... UNLESS CONFLICT ON .external_id`. This is correct Gel syntax for conflict handling.

**One concern:** Build-plan-v2 Step 5.2 says "use build tag `//go:build gel`" for Gel integration tests. This is fine for CI, but unlike Meilisearch (which is a single binary you can start), Gel requires a PostgreSQL instance as well. The infrastructure for running Gel integration tests in CI is not specified. The Gel testserver approach (if one exists) should be documented, or the tests should be marked as requiring external infrastructure.

---

## 5. Build Plan Quality: Is Each Step Executable?

### 5.1 Step-by-Step Assessment

**Phase 0 (Steps 0.1-0.9): GOOD.** These steps are well-defined with clear acceptance criteria. Each step is achievable by an LLM agent working on the codebase.

- Step 0.1 (rm k8s/): Already done. Remove this step or convert to verification.
- Step 0.2 (model extraction): Clear file list, clear acceptance criteria. Executable.
- Step 0.3 (interface split): Clear design. Executable.
- Step 0.4 (rename cmd/): Clear, mechanical. Executable.
- Step 0.5 (schema fields): Clear. Needs a migration mechanism for the SQLite schema -- the step says "add migration for new columns" but goose is not added until Step 0.6. For SQLite, an `ALTER TABLE ADD COLUMN` suffices. Executable with this caveat.
- Step 0.6 (goose): Clear. Note: goose is specified for CRDB migrations only -- SQLite continues with inline schema. This is reasonable.
- Step 0.7 (CRDBStore): The largest step. Scope estimate of "L (3-5 days)" is reasonable. The acceptance criteria are testable. The dependency on `cockroach-go/v2/testserver` for ephemeral CRDB is the right approach. Executable.
- Step 0.8 (wiring): Clear, mechanical. Executable.
- Step 0.9 (E2E for CRDB): Clear. The "test matrix or build tag" approach is sensible. Executable.

**Phase 1 (Steps 1.1-1.5): GOOD.** Straightforward CRUD additions.

- Step 1.1 (EventStore): Clear, follows established patterns. Executable.
- Step 1.2 (SessionStore): Same assessment.
- Step 1.3 (HTTP endpoints): Same.
- Step 1.4 (error messages): Clear guidance with specific examples from the devex analysis. The Huma v2 error format constraint (concatenating recovery into `detail`) is noted. Executable.
- Step 1.5 (startup logging): Clear. Executable.

**Phase 2 (Steps 2.1-2.8): MOSTLY GOOD with one concern.**

- Steps 2.1-2.4: Clear interface-first design. Executable.
- Step 2.5 (search endpoints): Depends on 2.1 and 2.3 but needs 2.2 for meaningful testing. Executable but acceptance criteria should note "integration test with MeiliSearcher requires Step 2.2."
- Step 2.6 (reconciler): Clear. The "records updated since last reconciliation timestamp" query pattern needs a `last_reconciled_at` tracking mechanism. The step does not specify where this timestamp is stored (in-memory? a config table?). Minor gap.
- Step 2.7 (wiring): Clear. Executable.
- Step 2.8 (memory injection): The most complex single step. The keyword extraction approach ("split on whitespace, remove stop words") is clearly described. The token estimation ("len(s)/4") is noted as an approximation. The fallback to CRDB LIKE queries is specified. This step is feasible but will need iteration -- the first implementation of keyword extraction will likely need refinement. Scope estimate of "M (2-3 days)" may be aggressive for getting ranking right.

**Phase 3 (Steps 3.1-3.6): GOOD but large.**

- Step 3.2 (planning store): Scope estimate "L (3-5 days)" is reasonable. This implements ~18 methods across two backends. The plan claiming pattern (`UPDATE ... WHERE status = 'open'` with row count check) is well-specified.
- Step 3.3 (planning endpoints): Another large step. 16 endpoints across 5 handler files. Scope estimate "L (3-5 days)" is reasonable because the endpoints follow established patterns.
- Step 3.4 (wave computation): The topological sort for dependency waves is well-specified. Cycle detection is mentioned. This is a genuinely useful algorithm that has clear test cases. Executable.
- Step 3.5 (project state): The computed state response is the "killer feature" for GSD replacement. The step specifies exactly what fields to compute and from what data. Executable.

**Phase 4 (Steps 4.1-4.6): GOOD.**

- Steps follow the same interface-first pattern. Executable.
- Step 4.2 (skill discovery): The integration with Meilisearch for context-aware search is well-specified. The "enrichment with effectiveness data" is a nice cross-domain query.

**Phase 5 (Steps 5.1-5.5): ADEQUATE but risks are higher.**

- Step 5.2 (GelStore): This is the riskiest step. The Gel Go client is less mature than pgx. The EdgeQL queries are well-specified but translating them to working Go code requires familiarity with the gel-go API. Integration testing requires a running Gel + PostgreSQL stack. Scope estimate "L (3-5 days)" may be optimistic given the immaturity of the Go client.
- The NoopGraphStore fallback (Step 5.1) is the correct mitigation: if Gel integration proves harder than expected, the system works without it.

**Phase 6 (Steps 6.1-6.6): GOOD.**

- All steps are independent and well-specified.
- Step 6.6 (scripts-to-rule-them-all): The `script/` directory already exists. This step should be updated to say "verify and extend existing scripts" rather than "create."

### 5.2 Overall Assessment

The build plan is the most detailed and executable plan in the entire document corpus. Each step has:

- Clear dependencies
- Specific files to create/modify
- Testable acceptance criteria
- Realistic scope estimates
- Parallelization guidance

The plan is executable by an LLM agent with codebase access. The main risk is scope creep in the larger steps (0.7, 3.2, 3.3, 5.2) and the absence of a consumer to validate the APIs against.

---

## 6. Missing Pieces

### 6.1 No Client Exists (CRITICAL)

The build plan produces a 67-endpoint server. No step in the plan, or in any document, produces a client that calls these endpoints. The MCP plugin is "out of scope." The hive CLI tool is "out of scope." The hive-local proxy is "out of scope."

After 14-17 weeks of implementation, the result is a server that nobody and nothing calls.

This is the skeptic's primary concern, and it is valid. The build plan should include at minimum one step that produces a minimal client -- even a shell script wrapper around curl -- that validates that the API is usable by an agent. Without this, there is no feedback loop: you cannot test whether your error messages help agent recovery, whether your memory injection returns useful context, or whether your skill discovery returns relevant skills.

**Recommendation:** Add a Step 1.6 (or equivalent): "Create a minimal `hive` CLI tool (Go binary or shell script) that wraps the core API (memory set/get/list, task CRUD, session submit). Register it as a Claude Code tool. Use it in one real session to validate the API." This should be in Phase 1, not deferred.

### 6.2 No Prompt Content Migration Strategy

Vision-v3 Section 8.2 says skills become "content, not dependencies." The `skills.content` field stores skill prompt text. But:

- Who writes the initial skill content for hive-server's skills table?
- Is it a copy-paste from GSD's agent persona markdown files?
- Is it an automated import?
- Who updates it when the prompt engineering evolves?

Vision-v3 Section 9 question #2 asks this but does not answer it. The build plan has no step for populating skill content. After Phase 4, the skills table exists but is empty.

**Recommendation:** Add a step in Phase 4 (after Step 4.2) for importing initial skill content from the existing skills. This can be manual (curl commands to POST skill content) or scripted (a migration script that reads SKILL.md files and POST them). It should also address maintenance: who updates skill content in hive-server when the prompt engineering improves?

### 6.3 No Latency Validation Step

The vision specifies latency targets (<50ms CRDB, <200ms search, <500ms graph). The build plan has no step for measuring or validating these targets.

**Recommendation:** Add a step in Phase 6 for load testing and latency measurement. Build-plan-v2 Step 6.3 (Prometheus metrics) provides the instrumentation. A separate step should define the benchmark scenarios (e.g., "100 concurrent agents, each sending 1 memory write + 1 task update + 1 search per minute") and validate against the latency targets.

### 6.4 No Gel Schema Evolution Strategy

Vision-v3 Section 9 question #5: "When the CRDB schema changes, the Gel schema must change too. How are these kept in sync?" No answer exists. The build plan creates Gel schema in Phase 5 but does not address how it evolves when CRDB migrations change the data model in subsequent development.

**Recommendation:** This is a design decision, not a build step. Document the strategy: either (a) Gel schema migrations are created alongside CRDB goose migrations, (b) Gel schema is regenerated from CRDB schema, or (c) Gel sync is treated as a secondary index that can be rebuilt from scratch. Option (c) is the simplest and aligns with the "CRDB is source of truth" principle.

### 6.5 No CI Configuration for Integration Tests

Build-plan-v2 references `cockroach-go/v2/testserver` for ephemeral CRDB in tests, and build tags (`//go:build meili`, `//go:build gel`) for optional integration tests. But no step updates the CI workflow (`.github/workflows/ci.yaml`) to run these tests. The existing CI runs `go test ./...` which will skip tagged tests.

**Recommendation:** Add a step (in Phase 0, after 0.9) to update CI to run CRDB integration tests. Add subsequent CI updates in Phase 2 (Meilisearch tests) and Phase 5 (Gel tests) if those are to be tested in CI. If they are not tested in CI, document this explicitly.

### 6.6 Backup Configuration

As noted in Section 2.14, there is no build plan step for configuring CockroachDB backups. CockroachDB supports `BACKUP` to cloud storage or local filesystems. For a production system where CRDB is the source of truth for all data, backup configuration is not optional.

**Recommendation:** Add a Phase 6 step for configuring CRDB backup procedures. At minimum: scheduled full backup, backup verification, and documented restore procedure.

---

## 7. The Skill Replacement Honesty Check

### 7.1 Does the Vision Acknowledge the 40-85% Irreplaceability?

**No.** Vision-v3 states "the skills become unnecessary" (Section 0), "the platform that replaces standalone skills entirely" (Section 1.1), and "Skills disappear from the dependency chain" (Section 1.2). The skill-replacement-analysis's findings are not incorporated into the vision's framing.

Vision-v3 Section 8.2 partially acknowledges this: "The skills' npm packages, shell scripts, and plugin manifests are no longer needed. An agent using hive-server gets planning, skill discovery, and spec management through the MCP plugin, backed by persistent queryable state." This is carefully worded to focus on the infrastructure (packages, scripts, manifests) rather than the intellectual content (prompts, personas, anti-rationalization patterns, workflow discipline).

### 7.2 What the Vision Actually Provides vs. What It Claims

| Claim                                                        | Reality                                                                                                                                                                                                                                                           |
| ------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| "Replace GSD's project management workflow"                  | Provides persistent, queryable storage for project/phase/plan/task state. Does NOT provide the agent personas, task format, verification philosophy, or orchestration logic that make GSD effective.                                                              |
| "Replace Superpowers' skill discovery and workflow pipeline" | Provides a searchable skill catalog and workflow state tracking. Does NOT provide the skill definitions themselves (the "how to brainstorm," "how to debug," "how to write tests" instructions) or the anti-rationalization design that prevents agent shortcuts. |
| "Replace Allium's spec management"                           | Provides structured spec storage, drift tracking, and cross-spec impact analysis. Does NOT provide the Allium language, the Tend/Weed agent personas, or the elicitation/distillation methodologies.                                                              |

### 7.3 The Over-Promise Risk

If the vision is taken at face value ("skills become unnecessary"), and the MCP plugin is built accordingly, agents will lose access to:

- GSD's 12 carefully crafted agent personas
- Superpowers' anti-rationalization design ("1% chance -> MUST invoke")
- Allium's specification language and authoring methodology

These are the parts that actually make agents effective. The hive-server APIs provide the backend that skills never had (persistent state, search, analytics). But the backend without the prompt engineering is like a database without an application -- technically correct and entirely useless.

### 7.4 Recommendation

The vision should adopt the skill-replacement-analysis's framing: "hive-server is the backend that skills never had." The MCP plugin should expose both (a) hive-server APIs for state management and (b) skill prompt content retrieved from the skills table for workflow guidance. The prompts and the API work together, not in place of each other.

The build plan's Phase 4 (skills domain) is correctly designed for this: the `skills.content` field stores the full skill instructions. The MCP plugin can retrieve a skill's content and inject it into the agent's context alongside the API tools. This is the architecture that works -- it just needs to be framed honestly.

---

## 8. Consensus Recommendation

### 8.1 Is This Plan Ready to Execute?

**Almost. Six changes are needed first:**

1. **Remove Step 0.1 or convert to verification.** The k8s/ directory is already deleted. The step should verify this and clean up any remaining documentation references, not delete a directory that does not exist.

2. **Add a minimal client step.** Without a client, the server APIs cannot be validated. Add a Step 1.6: create a minimal `hive` CLI tool or shell script that covers core operations (memory CRUD, task CRUD, session submit). This is the skeptic's and devex advocate's top recommendation and it is correct.

3. **Fix the vision's framing.** Either in the vision itself or in a brief addendum, acknowledge the skill-replacement-analysis's finding that 60-85% of skill value is prompt engineering. Reframe the vision as "hive-server is the backend that makes skills better" rather than "hive-server replaces skills." This prevents the MCP plugin (when built) from being designed without the prompt engineering content.

4. **Update Step 6.6 to reflect existing scripts.** The `script/` directory already exists with bootstrap, setup, test, server, docs, and e2e/integration subdirectories. Step 6.6 should say "verify and extend" not "create."

5. **Add a backup configuration step to Phase 6.** CRDB is the source of truth for all data. Backup is not optional.

6. **Add CI integration test configuration.** The plan creates integration tests with build tags but never updates CI to run them.

### 8.2 What Can Be Deferred Safely

- **Phase 5 (Gel DB):** The NoopGraphStore pattern means the system works without Gel. Phase 5 can be deferred indefinitely if Gel integration proves harder than expected or if SQL-based queries prove sufficient. The graph queries are genuinely useful for cross-spec impact analysis and dependency wave computation, but recursive CTEs in CockroachDB can approximate most of them.

- **Phase 4 steps 4.4-4.6 (specs domain):** Allium is the most inherently client-side of the three skills (the analysis says 10-25% replaceable). Spec storage and drift tracking can be done with the generic memory API using key conventions (`drift/auth-spec/2026-03-09`). Build the structured specs API only if the memory API proves insufficient.

- **Phase 6 steps 6.3, 6.5 (metrics, bulk ops):** Useful but not blocking. Can be added whenever convenient.

### 8.3 What Cannot Be Deferred

- **Phase 0:** CockroachDB migration is the foundation. Everything builds on it.
- **Phase 1:** Events, sessions, and error improvements are prerequisites for meaningful agent interaction.
- **Phase 2:** Meilisearch integration is the highest-value addition after core CRDB.
- **A minimal client** (not currently in the plan): Without this, there is no way to validate anything.

### 8.4 The Bottom Line

The plan is technically sound. The architecture is well-designed. The phasing is logical. The acceptance criteria are testable. The estimates are more realistic than prior iterations.

The plan's weakness is not technical -- it is strategic. It builds a 67-endpoint server without building the one thing that validates whether any of those endpoints are useful: a client that an agent can call. The plan should be executed with the six modifications above, and the client tool should be treated as a Phase 0/1 deliverable, not an out-of-scope future project.

If the six changes are made, this plan is ready to execute.

---

## Appendix: Document Quality Scores

| Document                      | Accuracy                                                          | Completeness                              | Actionability                              | Honesty                                           |
| ----------------------------- | ----------------------------------------------------------------- | ----------------------------------------- | ------------------------------------------ | ------------------------------------------------- |
| vision-v3.md                  | 8/10 (CockroachDB use is correct, Gel/Meili correctly positioned) | 9/10 (API surface is comprehensive)       | 7/10 (designed for server, ignores client) | 5/10 (over-promises on skill replacement)         |
| build-plan-v2.md              | 8/10 (k8s/ claim wrong, scripts claim outdated)                   | 9/10 (45 steps cover the vision's scope)  | 9/10 (each step is executable)             | 8/10 (realistic timeline, honest scope estimates) |
| skill-replacement-analysis.md | 10/10 (no inaccuracies found)                                     | 9/10 (covers all three skills thoroughly) | 6/10 (analysis, not a plan)                | 10/10 (the most honest document in the corpus)    |
