# Build Plan Review

**Reviewer**: Senior Software Architect
**Date**: 2026-03-09
**Documents Reviewed**: All 8 context documents (hive-server-current, github-issues, gel-db, meilisearch, cockroachdb, openclaw, vision, build-plan)

---

## Overall Assessment

The build plan is thorough, well-structured, and demonstrates strong systems thinking. The 33-step decomposition across 7 phases is granular enough for LLM coding agents to execute. The dependency graph is internally consistent, the prompts are detailed, and the technology choices are well-researched. This is a high-quality planning document.

That said, there are real problems that need to be addressed before execution begins. The issues below are ordered by severity.

---

## 1. Gaps: Vision/Issues NOT Covered in the Build Plan

### 1.1 Issue #19 (Auto-report to only-claws) is Missing as a Step

The vision document (Section 7.1) says "#19 auto-report via MasterClaw depends on #25" and the dependency graph lists it under Phase 5 Advanced Features. But the build plan has no step implementing it. There is no Step 6.5 or 7.5 for the only-claws reporting integration. The build plan simply stops at 7.4.

**Recommendation**: Add a step in Phase 6 or Phase 7 for implementing the only-claws auto-reporting. If it is intentionally deferred beyond this plan's scope, say so explicitly.

### 1.2 Memory Scoping (private/project/global) is Underspecified

The vision document (Section 4.6) defines three memory scopes: agent-private, project-shared, and global. The CockroachDB schema in the vision includes `project_id` and `scope` columns. But the build plan's migration in Step 3B.1 uses the schema from the CockroachDB brief, which does NOT include `project_id` or `scope` columns. The expanded schema from the vision is never introduced as a migration step.

Step 4A.3 references `scope` in the Meilisearch index settings (filterable on `scope`), and Step 4A.4 references `scope` in the search input, but neither the initial CRDB migration (3B.1) nor any subsequent migration adds these columns.

**Recommendation**: Either add a migration step between 3B.1 and 4A.3 that adds `project_id` and `scope` to the memory table, or include them in the initial 001_initial_schema.sql in Step 3B.1. The schema in 3B.1 should match the vision's expanded schema from the start.

### 1.3 Issue #21 and #22 (LSP Plugin Lifecycle, Stateful Store Research) Not Addressed

These are acknowledged as out of scope in the vision's dependency graph (Phase 5: Advanced Features), but the build plan never explicitly states they are deferred. A brief note would prevent confusion.

### 1.4 No Step for docker-compose.yml Creation

Step 4B.1 references updating `docker-compose.yml` to add Gel. Step 4C.3 references updating it to add MasterClaw. Appendix A shows the full docker-compose. But there is no step that creates the initial docker-compose.yml. The current codebase does not have one. Either Step 3A.2 (scripts) or a new step should create it, since developers need CockroachDB running locally before Step 3B.1 can be verified.

**Recommendation**: Add docker-compose.yml creation as part of Step 3A.2 (scripts) or 3B.1 (CRDB store), at minimum with the CockroachDB service. Subsequent steps can extend it.

### 1.5 Hive-Local Caching Not Addressed

The resolved open question #4 states "hive-local should cache recent injections with a 30-second TTL." This is an important performance optimization for the memory injection pipeline. The build plan does not include any step for this. While hive-local is a separate repo, the hive-server API design should consider cache-friendliness (e.g., ETag headers on injection responses, or a cache-key field in the response).

**Recommendation**: Add a note to Step 5.2 about including cache-friendly response headers (ETag or a hash of the context blocks) so hive-local can implement TTL caching without further hive-server changes.

---

## 2. Conflicts

### 2.1 Step 3A.3 Creates Placeholder Packages That Conflict with Later Steps

Step 3A.3 creates `internal/search/search.go` with a `Searcher` interface and `NoopSearcher`. Step 4A.1 then "updates the placeholder from 3A.3" to define a more complete interface with different method signatures. This means:

- Step 3A.3 defines `Search`, `Index`, `Delete`, `Configure`
- Step 4A.1 redefines the interface with `Search`, `Index`, `Delete`, `EnsureIndex`, `Healthy`

The signatures differ (`Configure` vs `EnsureIndex` + `Healthy`). Any code written between 3A.3 and 4A.1 that depends on the placeholder interface will break when 4A.1 changes it.

**Recommendation**: Either make the placeholder interfaces in 3A.3 intentionally minimal (e.g., just the package with a comment, no interface definition) or make them match what 4A.1 will define. The current approach creates unnecessary churn.

### 2.2 Step 3A.4 vs Existing Handlers

Step 3A.4 adds `Ping(ctx context.Context) error` to the Store interface and says the handlers' local `Store` interface should "add Ping." But the handlers' `Store` interface is used for test mocking. Adding `Ping` to the handlers' interface means every test mock must implement `Ping`. This is a minor issue but the prompt should mention updating test mocks explicitly.

### 2.3 Build Plan Migration Numbering vs Reality

Step 3B.1 creates `001_initial_schema.sql`. Step 5.3 creates `002_injection_log.sql`. Step 6.1 creates `003_subtask_hierarchy.sql`. But Step 6.1's prompt says "or update 001 if not yet deployed." This is dangerous advice for an LLM agent. If 001 has already been deployed to any environment, modifying it would cause migration drift. The prompt should not offer this as an option.

**Recommendation**: Remove the "or update 001 if not yet deployed" language from Step 6.1. Always use sequential migrations. The LLM agent has no way to know what has been deployed.

---

## 3. Ordering Issues

### 3.1 Step 3A.5 (Huma v2) Depends on 3A.4, but 3A.4 Depends on 3A.3

This is correct in the dependency graph, but Step 3A.3's prerequisites say "None (but best done after 3A.1)." This means 3A.3 could be done in parallel with 3A.1 and 3A.2. However, Step 3A.3 renames `cmd/app/` to `cmd/hive-server/` and updates the Dockerfile and goreleaser. Step 3A.2 creates `script/build` which references `cmd/app/`. If 3A.2 runs before 3A.3, the build script will reference the old path. If 3A.3 runs first, 3A.2 needs to use the new path.

**Recommendation**: Make 3A.2 depend on 3A.3 (or vice versa), or explicitly note in 3A.2's prompt that the build path should be `cmd/app/` and will be updated when 3A.3 runs. The current "both independent" framing will cause merge conflicts.

### 3.2 Step 4A.3 Depends on 3B.1, but 4A.1 and 4A.2 Do Not

The search interface (4A.1) and Meilisearch implementation (4A.2) are correctly independent of CockroachDB. But 4A.3 (sync pipeline) depends on 3B.1 (CRDB store) because the SyncStore wrapper wraps `store.Store`. However, the SyncStore wrapper in 4A.3 uses generic `store.Store` embedding, which should work with SQLite too. The prompt even says "keeps Meilisearch in sync with the primary store (CockroachDB or SQLite)." So the dependency on 3B.1 seems artificial.

**Recommendation**: Remove the hard dependency of 4A.3 on 3B.1. The sync pipeline should work against any Store implementation, including SQLite. This unlocks Meilisearch integration earlier, which is valuable for testing.

### 3.3 Step 3B.2 is Labeled as "Unit Tests" but Describes Integration Tests

Step 3B.2 is titled "CockroachDB Store Unit Tests" and creates tests with `//go:build crdb` that require a running CockroachDB instance. These are integration tests, not unit tests. Step 3B.3 is "Ephemeral CockroachDB for Integration Tests" with `//go:build integration`. Having two separate build tags for what are both integration tests against CRDB is confusing.

**Recommendation**: Merge 3B.2 and 3B.3 or clarify the distinction. The `crdb` tag tests require a manually started CRDB; the `integration` tag tests auto-start one. This is a valid distinction but the naming ("unit tests" vs "integration tests") is misleading.

---

## 4. Missing Tests

### 4.1 No Tests for the SyncStore Wrapper Pattern (Step 4A.3)

Step 4A.3 defines a `SyncStore` wrapper that embeds `store.Store` and adds sync hooks. The prompt creates `sync_test.go` but the tests only cover the `SyncService` methods, not the `SyncStore` wrapper itself. The wrapper has subtle concurrency behavior (fire-and-forget goroutines calling `sync.IndexMemory`). This needs testing:

- Verify that store operations succeed even when Meilisearch sync fails
- Verify that sync goroutines do not leak on rapid sequential calls
- Verify that context.Background() is used for sync (not the request context, which may be cancelled)

### 4.2 No Load/Stress Tests Mentioned Anywhere

For a system targeting 200ms injection latency with fan-out to 3 backends, there should be at least basic load testing. The build plan mentions no performance benchmarks or load tests at any step.

**Recommendation**: Add a step (or expand Step 3A.6) for basic Go benchmarks (`BenchmarkInjection`, `BenchmarkSearch`) and a latency test that verifies the 200ms injection budget under realistic conditions.

### 4.3 Step 4C.1 (MasterClaw Client) Tests Do Not Cover LLM Response Parsing Failures

The MasterClaw client expects the LLM to return valid JSON. The prompt mentions "If parsing fails, return a fallback (raw results without synthesis)." But the test prompt only lists: mock server tests, successful synthesis, and basic HTTP error handling. There is no test for malformed JSON from the LLM, partial JSON, or the LLM wrapping JSON in markdown code blocks (a very common LLM behavior).

**Recommendation**: Add explicit test cases for: malformed JSON response, JSON wrapped in ```json code blocks, empty response, timeout, and response exceeding expected size.

### 4.4 No Migration Rollback Tests

The goose migrations include `-- +goose Down` blocks, but no step tests that rollback works correctly. If a deployment fails partway through a migration, the down path matters.

---

## 5. Unclear Prompts

### 5.1 Step 3A.5 (Huma v2) is the Largest and Vaguest

Step 3A.5 is sized "L" and asks the LLM agent to convert all 12 endpoints to Huma operations, update all tests, and ensure backward compatibility. The prompt gives one example (memory upsert) and says "convert all 12 endpoints" without providing the Huma operation definitions for the other 11. An LLM agent will have to infer the correct Huma input/output struct shapes for tasks, agents, health, etc.

This is the highest-risk prompt in the entire plan because:

- Huma has specific conventions for path params, query params, body structs, and error responses
- The existing tests use chi-specific patterns (direct handler calls, middleware chains)
- Backward compatibility is required but the test migration strategy is vague ("use humatest or continue using httptest")

**Recommendation**: Either split this into 3 sub-steps (memory endpoints, task endpoints, agent endpoints) or provide the full Huma operation definitions for all 12 endpoints in the prompt. The current prompt relies too heavily on the LLM agent "figuring it out."

### 5.2 Step 5.1 (Query Router) is Underspecified for the Inject Intent

The prompt says "Extract key terms from the prompt text (simple keyword extraction -- split on whitespace, remove stop words, take top N terms)" but does not specify N, does not provide a stop words list, and does not explain how to handle prompts that are code (not natural language). An LLM agent prompt like "fix the error in main.go where line 42 panics" has very different keyword extraction needs than "what was the deployment issue last week."

**Recommendation**: Provide a concrete stop words list (or point to a standard one), specify N (e.g., top 10 terms), and add handling for code-like prompts (preserve function names, file paths, error messages as keywords).

### 5.3 Step 4B.2 (Gel-Go Client) EdgeQL Syntax is Imprecise

The prompt shows EdgeQL queries with positional parameters (`$0`, `$1`). However, the gel-go client uses named parameters in some versions and positional in others. The EdgeQL syntax shown mixes styles. An LLM agent unfamiliar with gel-go may produce syntactically invalid queries.

**Recommendation**: Verify the exact gel-go parameter binding syntax (named vs positional) and standardize the examples in the prompt. The gel-go documentation should be the authority here.

---

## 6. Risk Areas

### 6.1 HIGHEST RISK: Huma v2 Migration (Step 3A.5)

This is the critical path bottleneck. Everything after it depends on it. If the Huma migration breaks backward compatibility or the tests cannot be cleanly adapted, it blocks the entire chain. Huma's request/response model (struct-based with doc tags) is fundamentally different from chi's handler model (raw http.Handler). The auth middleware interaction is particularly risky -- Huma has its own middleware model that may conflict with chi middleware.

**Mitigation**: Write a spike/proof-of-concept for one endpoint before committing to the full migration. Verify that chi middleware (auth, request ID) works correctly with Huma operations registered on the same router.

### 6.2 HIGH RISK: MasterClaw Reliability (Steps 4C.1-4C.3, 5.2, 6.2-6.4)

MasterClaw is an OpenClaw instance that must return valid JSON. OpenClaw has critical CVEs (CVE-2026-25253, CVE-2026-25157), 63% of exposed instances are vulnerable, and CrowdStrike has published detection guidance. The build plan correctly isolates MasterClaw to ClusterIP-only, but:

- The plan does not specify which OpenClaw version to pin to
- The plan does not address how to patch MasterClaw when CVE fixes are released
- The plan does not specify container image scanning in CI for the MasterClaw image
- OpenClaw has no documented HA/clustering -- a single replica is a single point of failure for all LLM-powered features

**Mitigation**: Pin to a specific OpenClaw version in docker-compose.yml (not `:latest`). Add image scanning for the OpenClaw container. Document the upgrade path. Since MasterClaw is optional (graceful degradation), the SPOF risk is somewhat mitigated, but the plan should acknowledge it.

### 6.3 HIGH RISK: Gel DB Go Client Maturity

The gel-db.md brief explicitly notes: "Go client less mature than TypeScript client." The build plan proceeds with gel-go integration without a validation step. If gel-go has blocking bugs (missing features, incorrect serialization, connection pool issues), it could stall Phases 4B, 5, 6, and 7.

**Mitigation**: Add a validation step before 4B.2 that writes a small standalone Go program exercising the critical gel-go operations (Connect, Query, QuerySingle, Execute, Tx) against the local Gel instance from 4B.1. If this fails, the plan needs an escape hatch (e.g., using Gel's PostgreSQL wire protocol with pgx instead of gel-go).

### 6.4 MEDIUM RISK: Meilisearch 10-Word Query Limit

The meilisearch.md brief documents that queries longer than 10 words silently drop excess terms. LLM agents generate verbose queries. Step 4A.2 addresses this ("truncate to 10 most significant words, strip stop words first") but the implementation is left vague. A bad keyword extraction algorithm could make search nearly useless for agent workloads.

**Mitigation**: The keyword extraction in 4A.2 and the query term extraction in 5.1 should share a single, well-tested utility function. Consider using TF-IDF or similar lightweight term importance scoring rather than simple "remove stop words, take first 10."

### 6.5 MEDIUM RISK: Fire-and-Forget Goroutines

The build plan uses `go func() { ... }()` for async operations in multiple places:

- Step 4A.3: SyncStore fires goroutines to index in Meilisearch
- Step 5.3: Injection logging is async
- Step 4B.3: Knowledge graph sync is async

None of these have backpressure, error tracking, or shutdown coordination. Under load, these goroutines could pile up unbounded. On graceful shutdown, in-flight goroutines could be killed before completing, causing data loss.

**Mitigation**: Use a bounded worker pool (e.g., `golang.org/x/sync/errgroup` with semaphore, or a channel-based work queue). Track in-flight goroutines and drain on shutdown. Add metrics for queue depth.

### 6.6 LOW RISK: CockroachDB Licensing

The build plan resolves this: "Enterprise Free is acceptable for christmas-island org (sub-$10M revenue)." This is fine for now, but the mandatory telemetry and annual renewal should be documented in an operational runbook. The plan does not mention where to track the renewal date.

---

## 7. Quick Wins: Parallelization and Combination Opportunities

### 7.1 Steps 3A.1, 3A.2, and 3A.3 Can Be Partially Parallelized

3A.1 (remove k8s/) and 3A.2 (scripts) are both independent. They can be developed in parallel on separate branches and merged sequentially. However, see the ordering issue in Section 3.1 regarding 3A.2 and 3A.3 path conflicts.

### 7.2 Steps 3B.2 and 3B.3 Should Be Combined

These are both "test the CRDB store" with slightly different approaches (manual CRDB vs ephemeral CRDB). The test code is nearly identical. Combining them into a single step that provides both `//go:build crdb` (for dev) and `//go:build integration` (for CI) test entrypoints would reduce duplication.

### 7.3 Phase 4A, 4B, and 4C Can Be Fully Parallelized

After Phase 3 completes, all three Phase 4 sub-phases are independent until their integration point (Phase 5). The dependency graph already reflects this, but the plan presents them sequentially. Three developers (or agents) could work all three simultaneously.

### 7.4 Steps 5.3 and 5.4 Can Be Parallelized

Injection logging (5.3) and token budget management (5.4) both depend on 5.2 but not on each other.

### 7.5 Steps 7.1, 7.2, and 7.3 Are Partially Redundant

Step 7.1 creates discovery endpoints including GET /api/v1/discover/tools and GET /api/v1/discover/agents. Steps 7.2 and 7.3 enhance those same endpoints. These could be a single step with richer initial implementation, avoiding two rounds of modifying the same handler file.

### 7.6 Steps 3B.4 (Deep Health) and 7.4 (Deep Health All) Are Duplicative

Step 3B.4 adds `/healthz` with store ping. Step 7.4 upgrades `/healthz` to check all backends. This is two passes over the same endpoint. Consider implementing 3B.4 with the extensible structure from 7.4 (a map of backend statuses) so that 7.4 merely adds new backends to the existing map rather than rewriting the handler.

---

## 8. Verification: Dependency Chain (#20 -> #16 -> #12+#18 -> #13/#14/#15)

**Status: RESPECTED**

The build plan maps this exactly:

- Step 3A.3 = Issue #20 (project layout) -- no prerequisites
- Step 3A.5 = Issue #16 (Huma v2) -- depends on 3A.4, which depends on 3A.3
- Step 3B.1 = Issues #12 + #18 (CRDB + tx retries) -- depends on 3A.5
- Step 3B.2 = Issue #13 (test updates) -- depends on 3B.1
- Step 3B.3 = Issue #14 (ephemeral CRDB) -- depends on 3B.1
- Step 3B.4 = Issue #15 (k8s deploy, partial) -- depends on 3B.1

The chain is intact. The only concern is that Step 3A.4 (formalize Store interface) was inserted between #20 and #16. This is a good addition that reduces the risk of the Huma migration, not a violation.

---

## 9. Verification: 8 Locked Design Decisions from ops#82

| #   | Decision                                             | Build Plan Status                                                                                            |
| --- | ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| 1   | Two repos for local stack (hive-local + hive-plugin) | HONORED. Build plan is scoped to hive-server only. No conflation with hive-local or hive-plugin.             |
| 2   | Port 18820 for local server                          | NOT APPLICABLE. hive-server uses PORT env var (default 8080). Port 18820 is hive-local's concern.            |
| 3   | Factory registration pattern                         | NOT APPLICABLE. This is a hive-plugin concern.                                                               |
| 4   | HTTP headers for context (X-Agent-ID, etc.)          | HONORED. Auth middleware preserving X-Agent-ID is explicitly maintained in Steps 3A.5, 4A.4, and throughout. |
| 5   | CockroachDB for production                           | HONORED. Step 3B.1 implements the CRDB store. SQLite preserved for local dev.                                |
| 6   | Huma v2 before CRDB migration                        | HONORED. Step 3A.5 (Huma) precedes Step 3B.1 (CRDB) in the dependency chain.                                 |
| 7   | Token broker in hive-local                           | NOT APPLICABLE. hive-local concern.                                                                          |
| 8   | Node crypto for JWT                                  | NOT APPLICABLE. hive-plugin concern.                                                                         |

**Status: All applicable decisions are honored.**

---

## 10. Verification: k8s#58 Fan-Out Architecture

**Status: PROPERLY IMPLEMENTED**

The build plan faithfully implements the k8s#58 architecture:

- Hive fans out to Gel (graph-relational) + Meilisearch (full-text/fuzzy) via the Query Router (Step 5.1)
- MasterClaw (in-cluster OpenClaw) synthesizes results (Steps 4C.1-4C.2)
- Results return through hive-server (Step 5.2)
- No standalone Postgres (Gel uses its own bundled PG, CRDB uses pgwire)
- No vector/embedding search (no GPU) -- correctly not implemented
- MasterClaw is internal-only with ClusterIP (Step 4C.3)

The resolved open question #1 (Gel MUST NOT use CockroachDB as its backend) is a critical clarification that prevents a likely failure. Well caught.

---

## 11. Verification: Token Efficiency

**Status: PARTIALLY ADDRESSED**

- Single-tool pattern: Referenced in the vision but not implemented in hive-server (it is a hive-local concern). The build plan correctly does not try to implement it.
- Memory injection budgets: Step 5.4 implements token budget management with `EstimateTokens` and `TrimToTokenBudget`. The 200ms latency target is stated in the resolved open questions. However, there is no step that measures or enforces this latency target.
- Discovery API replacing static TOOLS.md: Steps 7.1-7.3 implement this correctly.

**Gap**: No latency measurement or alerting. Add Prometheus metrics for injection latency (histogram) in Step 5.2 or 5.3. Without metrics, the 200ms budget is aspirational, not enforceable.

---

## 12. Verification: Security Considerations

### 12.1 MasterClaw Exposure

**Status: ADDRESSED**

Step 4C.3 correctly restricts MasterClaw to localhost in docker-compose (`127.0.0.1:3000:3000`) and specifies ClusterIP-only in production. Both CVEs are referenced. The SOUL.md explicitly states MasterClaw is internal-only.

**Gap**: The MasterClaw docker-compose service does not specify `restart: unless-stopped` or health checks. If MasterClaw crashes (e.g., due to a CVE exploit from another in-cluster service), there is no auto-restart.

### 12.2 CVEs (CVE-2026-25253, CVE-2026-25157)

**Status: PARTIALLY ADDRESSED**

The CVEs are mentioned and MasterClaw is network-isolated. However:

- No container image pinning (uses `:latest`)
- No image vulnerability scanning step in CI
- No mention of network policies in k8s to restrict which pods can talk to MasterClaw

**Recommendation**: Pin MasterClaw to a specific version tag that post-dates the CVE fixes. Add a note about k8s NetworkPolicy restricting access to the MasterClaw service to only the hive-server pods.

### 12.3 Auth

**Status: WELL HANDLED**

The build plan preserves the existing `HIVE_TOKEN` bearer auth pattern. Inter-service credentials (MEILI_API_KEY, GEL_DSN, DATABASE_URL, MASTERCLAW_TOKEN) are environment variables intended for k8s Secrets injection. The auth middleware is preserved through the Huma migration. Agent-scoped Meilisearch filters are injected server-side (agents cannot bypass).

### 12.4 SQL Injection Risk in Meilisearch Filter Construction

Step 4A.2 constructs filter strings by interpolating agent_id values:

```go
searchReq.Filter = fmt.Sprintf("agent_id = '%s'", req.AgentID)
```

This is a filter injection risk. If `req.AgentID` contains a single quote, it could break the filter syntax or inject arbitrary filter clauses. Meilisearch's filter language is not SQL, but it has its own injection risks.

**Recommendation**: Sanitize or validate `agent_id` before interpolation. Reject agent IDs containing single quotes, parentheses, or boolean operators (`AND`, `OR`, `NOT`). Or use Meilisearch tenant tokens for guaranteed filter enforcement.

---

## 13. What is Done Well

1. **Dependency graph is correct and clearly documented.** Both the textual description and the ASCII graph are consistent. Prerequisites are listed for every step.

2. **Every step has verification criteria.** This is essential for LLM agent execution. The verification blocks tell the agent how to confirm its work.

3. **Graceful degradation is consistently applied.** Every backend is independently optional. The NoopSearcher/NoopKnowledgeStore/NoopClient pattern is clean and testable.

4. **The resolved open questions are excellent.** The five design decisions at the top of the build plan (Gel not on CRDB, tiered MasterClaw cost model, async sync acceptable, 200ms latency budget, licensing) preempt exactly the questions that would block implementation.

5. **Interface-first design.** The plan consistently defines interfaces before implementations (Store, Searcher, KnowledgeStore, MasterClaw Client). This enables parallel development and clean test mocking.

6. **The SyncStore wrapper pattern (Step 4A.3) is well designed.** Decorating the store without polluting implementations is a clean architectural pattern. The separation of concerns between the store (ACID writes) and sync (best-effort indexing) is correct.

7. **Step sizing is realistic.** The XS/S/M/L estimates correlate well with the actual complexity of each step. The two L steps (3A.5 Huma, 3B.1 CRDB) are correctly identified as the highest-effort items.

8. **Environment variable reference (Appendix B) is complete.** This is a small but important detail that prevents configuration confusion.

---

## 14. Summary of Recommendations (Priority Order)

| Priority | Issue                                                                         | Section |
| -------- | ----------------------------------------------------------------------------- | ------- |
| **P0**   | Add `project_id` and `scope` columns to CRDB initial migration                | 2.1     |
| **P0**   | Fix Meilisearch filter injection risk in Step 4A.2                            | 12.4    |
| **P0**   | Provide full Huma operation definitions in Step 3A.5, or split into sub-steps | 5.1     |
| **P1**   | Remove "or update 001 if not yet deployed" from Step 6.1                      | 2.3     |
| **P1**   | Add docker-compose.yml creation step for CockroachDB (before 3B.1)            | 1.4     |
| **P1**   | Add gel-go validation spike before Step 4B.2                                  | 6.3     |
| **P1**   | Pin MasterClaw to specific version, add NetworkPolicy note                    | 12.2    |
| **P1**   | Add bounded worker pool for fire-and-forget goroutines                        | 6.5     |
| **P2**   | Resolve 3A.2/3A.3 build path ordering conflict                                | 3.1     |
| **P2**   | Remove artificial 4A.3 dependency on 3B.1                                     | 3.2     |
| **P2**   | Add injection latency Prometheus metrics                                      | 11      |
| **P2**   | Add MasterClaw JSON parsing failure tests                                     | 4.3     |
| **P2**   | Standardize placeholder interface in 3A.3 to match final 4A.1                 | 2.1     |
| **P3**   | Combine Steps 3B.2 and 3B.3                                                   | 7.2     |
| **P3**   | Combine Steps 7.1, 7.2, and 7.3                                               | 7.5     |
| **P3**   | Add Step for only-claws auto-reporting (#19)                                  | 1.1     |
| **P3**   | Add load/benchmark tests                                                      | 4.2     |
| **P3**   | Specify keyword extraction parameters for Query Router                        | 5.2     |
