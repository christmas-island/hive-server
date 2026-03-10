# REVIEW: Hive-Server v5 Build Graph — ZeroClaw 🌐

**Reviewer:** ZeroClaw
**Reviewed:** SYNTHESIS-v2.md + source extractions 01–06
**Verdict:** The plan is structurally sound. The decomposition is mostly right. But there are real gaps — some of them load-bearing.

---

## 1. Missing Issues

### 1a. The entire skill-replacement data model is absent

06-skill-replacement.md describes a significant set of server-side capabilities that are **not captured in any issue**:

- **Projects, Phases, Plans, Tasks, Dependencies** — full project lifecycle state (GSD's STATE.md/ROADMAP.md replacement)
- **Sessions** (agent session state snapshots for pause/resume — distinct from `agent_sessions` which tracks injection context)
- **Skills registry** (name, content, source_priority, version — with Meilisearch-backed discovery)
- **Allium specs** (AST storage, drift reports, cross-spec impact queries)
- **Requirements traceability** (req→phase→plan→task chain)
- **Dependency wave computation** (adjacency graph → parallel execution groups)
- **Progress/velocity metrics**

The synthesis plan builds the _directive engine_ but ignores the _state management layer_ that skills need to stop being fragile markdown-file clients. This is the other half of hive-server v5. It needs its own issue cluster — probably 3-5 issues covering:

- Project/phase/plan/task CRDB schema + store
- Session state snapshots + resume
- Skill registry + Meilisearch integration
- Allium spec storage + graph queries (Gel)
- Dependency wave computation + progress API

Without these, the "skill replacement" story from the vision is incomplete. Skills can receive directives but still have nowhere to persist their state.

### 1b. No authentication/authorization issue

Every endpoint has `Bearer <token>` auth. There's `tenant_id` on every table. But there's no issue for:

- Token validation middleware
- Tenant resolution from token
- Per-tenant isolation enforcement
- API key management

Open Question #3 acknowledges this but doesn't resolve it. If multi-tenancy is "just a column for now," that's fine — but the auth middleware still needs to exist. Even single-tenant needs token validation. This should be a small infrastructure issue (I5 or similar).

### 1c. No CRDB→Meilisearch/Gel event mechanism issue

S5 and S7 describe "async sync triggered on directive create/update." How? The plan doesn't specify the event dispatch mechanism. Options:

- In-process channel/callback after CRDB write
- CockroachDB changefeed (CDC) → consumer
- Outbox pattern (write event row in same CRDB tx, background poller reads it)

This is an architectural decision that affects S2, S5, S7, P3, and P4. It should either be its own issue or explicitly scoped into S5 with a note that S7 shares the mechanism. Right now each sync manager is described independently as if the event plumbing is obvious. It isn't.

### 1d. No integration test / end-to-end test issue

Individual issues mention `NoopSearcher`, `NoopGraphStore`, etc. for unit tests. But there's no issue for:

- Docker Compose or testcontainers setup (CRDB + Meilisearch + Gel for integration tests)
- End-to-end smoke test: ingest a skill doc → decompose → inject → feedback → verify weight change
- CI pipeline configuration

For a system with 3 databases and 2 LLM pipelines, integration testing isn't optional — it's how you know the sync managers, fan-out, and merge logic actually work together. This needs an explicit issue, probably after P1 or P3.

### 1e. Configuration management

The system has extensive configuration surface:

- 3 database connection strings + pool settings
- LLM API keys, model names, temperature, timeouts
- Meilisearch index settings, sync intervals
- Gel connection settings
- Ranking formula weights
- Token budgets, cooldown thresholds
- Embedding model/provider (Open Question #1)

No issue addresses how config is loaded, validated, or structured. This matters because misconfiguration across 3 databases + LLM is the #1 operational failure mode for a system like this. Should be part of I1 or a dedicated I5.

---

## 2. Wrong Decomposition

### 2a. S1 should merge into S2

S1 (Directive data model) is just a Go struct definition. It has no tests, no migrations, no persistence logic. Every consumer of S1 also depends on S2. Making it a separate issue creates a dependency node that adds coordination overhead with zero independent value. Merge S1 into S2 — "Directive data model, CRDB schema, and store implementation."

### 2b. P4 is two unrelated things jammed together

P4 covers:

1. Per-directive outcome feedback (`POST /api/v1/feedback`) — counter updates, weight decay, auto-deprecation
2. Experience-derived directive generation (`POST /api/v1/feedback/session-complete`) — LLM-based creation of new directives from session summaries

These have different dependencies (the second needs I4/LLM client, the first doesn't), different complexity (the second is essentially a mini-decomposition pipeline), and different risk profiles. Split into:

- **P4a: Outcome feedback + weight evolution** — depends on S2, S3
- **P4b: Experience-derived directive generation** — depends on P4a, I4, S4 (needs dedup), S5 (needs sync)

### 2c. X3 is too big and too late

X3 bundles: slog migration, Prometheus metrics, sync-only CLI, backup scripts, integrity checks. Some of these (slog, basic metrics) should happen in Phase 0 — retrofitting structured logging after 17 issues of `log.Printf` is painful. Others (integrity checks) are genuinely late-stage.

Split:

- **I5 (or fold into I1): slog + basic request metrics** — do this first, everything benefits
- **X3: Operational tooling** — sync-only, backup, integrity check (this can stay late)

---

## 3. Dependency Errors

### 3a. P1 is missing a dependency on S3

P1 (Injection pipeline core) stores injection records and does session dedup. That's S3's schema (`injections`, `agent_sessions`, `injection_outcomes`). The dependency list says `S2, S3, S4, S6` — this is actually correct in SYNTHESIS-v2. Good.

### 3b. P3 should depend on S7, not just S5

P3 (Decomposition) writes directives that need to sync to both Meilisearch AND Gel. The dependency list includes S5 (Meili sync) but not S7 (Gel sync). If decomposition creates chain relationships, those need to reach Gel. Add S7 dependency, or accept that decomposition-created chains won't appear in Gel until the next reconciliation loop (which might be acceptable — document the decision).

### 3c. False dependency: I2 doesn't strictly need I1

I2 (CRDB schema hardening + goose) touches migrations and CRDB types. I1 (project layout restructure) touches directory structure. These could run in parallel if the goose migration directory is agreed upfront. The dependency exists because import paths change — but goose migrations are SQL files, not Go code. Only the `crdb.RetryTx` wrapper cares about import paths. This is a weak dependency that could be parallelized with a 5-minute coordination chat.

### 3d. X2 (Seed directives) could be parallelized earlier

X2 depends only on S2 but is listed as an integration issue. It's actually a valuable testing accelerator — having real directives in the system makes every subsequent issue's manual testing better. Consider promoting it to run immediately after S2, not as a late-stage integration task.

### 3e. Missing: P2 → S3 dependency

P2 (LLM recomposition) is described as depending on I4 and P1. But recomposition needs to know about injection records to do session dedup and previous-injection tracking. This flows through P1, so it's transitive — but worth making explicit since P2's recomposition prompt includes context about what was previously injected.

---

## 4. Scope Gaps

### 4a. Caching architecture (not implementation)

Open Question #6 asks about caching. But the injection pipeline (04-injection-pipeline.md) explicitly describes:

- Full response cache keyed on `sorted(directive_ids) + phase + project + context_summary_prefix`
- Session-level directive cache for unchanged directive sets
- "sub-second on cache hit"

The synthesis plan says "no cache in v1" but the pipeline design assumes cache exists for performance targets. This disconnect needs resolution. At minimum, the `Pipeline` struct in P1 should have a `Cache` interface (even if the implementation is `NoopCache`) so the cache can be added without restructuring.

### 4b. Graceful degradation ordering

04-injection-pipeline.md specifies: "drop Gel first, then Meilisearch, then CRDB (source of truth)." This degradation cascade needs to be designed into P1 — it's not just error handling, it's explicit fallback logic with health-check-driven source selection. P1's description doesn't mention this.

### 4c. Embedding model selection

Open Question #1 is real and blocks S4. You can't configure Meilisearch hybrid search without knowing the embedding model and dimensionality. This needs to be a decision made before S4, not during. Recommendation: use OpenAI text-embedding-3-small (already in the stack for memory-core) — it's 1536-dim, Meilisearch supports it natively, and consistency with the existing system reduces operational surface.

### 4d. Tokenizer selection

Both 01-vision.md and 03-directive-schema.md mention `token_cost` as a field computed during enrichment. But the tokenizer isn't specified. Options: tiktoken (OpenAI), Anthropic's tokenizer, a fast approximation (chars/4). This matters because:

- Token costs need to be consistent with the model that consumes them
- If the injection target is Claude, you want Anthropic's tokenizer
- If it varies per tenant/agent, token_cost becomes model-dependent

This should be addressed in P3 (decomposition enrichment) or I4 (LLM client package — add a `Tokenize` method).

### 4e. Rate limiting

No mention anywhere of rate limiting on API endpoints. With LLM calls on both ingest and inject paths, an unconstrained client could burn through API budget fast. Even basic per-tenant rate limiting should be scoped.

### 4f. The "speculative pre-fetch" injection model

04-injection-pipeline.md describes: "fire before agent processes user message" and "inject every 3–5 prompts not every prompt." This frequency control logic needs to live somewhere — likely in the MCP plugin, not the server. But the server needs to support it (e.g., the `previous_injection_id` mechanism). The boundary between "server decides when to inject" and "client decides when to inject" should be explicit.

---

## 5. Architectural Risks

### 5a. 🔴 Gel DB is the biggest risk

Gel DB (EdgeDB fork) has:

- Smallest ecosystem and community
- Least mature Go driver (`gel-go`)
- Most complex schema language (SDL + EdgeQL)
- Least predictable failure modes under load

And it's on the critical path for injection quality (chain expansion) and decomposition (relationship storage).

**Recommendation:** Build Gel integration last (the plan already does this — Phase 5). But more importantly: **design the `GraphStore` interface so it can be backed by CockroachDB recursive CTEs as a fallback.** Chains and relationships can be modeled in CRDB with adjacency lists and `WITH RECURSIVE` queries. Gel adds elegance, not capability. If Gel proves unstable, you need an exit path that doesn't require rewriting P1.

### 5b. 🔴 LLM recomposition latency

3-4 seconds per inject call is brutal for real-time use. The plan acknowledges this but doesn't provide a concrete mitigation path beyond "cache later." The real risk: if recomposition is too slow, agents will either not use it (defeating the purpose) or the system will need a fundamentally different architecture (pre-computed recomposition, background batch processing).

**Recommendation:** Add a `skip_recomposition` flag to the inject endpoint from day one. Let clients choose raw directives (fast) vs. recomposed micro-prompts (slow). This also makes P1 independently valuable without P2 — you can ship injection without recomposition and add it later without API changes.

### 5c. 🟡 Decomposition quality is untestable without a feedback signal

How do you know decomposition is good? The current plan tests it by... ingesting docs and looking at the directives. But quality assessment requires injection and feedback loops to be complete. You can't evaluate "did we extract the right directives" until agents are using them and reporting outcomes.

**Recommendation:** X2 (seed directives) should include hand-verified golden test cases — 5-10 directives with known-good trigger conditions and expected injection scenarios. Use these as regression tests for the full pipeline.

### 5d. 🟡 Three-database consistency

CRDB is source of truth, but injection queries all three in parallel. If Meilisearch or Gel is stale:

- A just-created directive won't appear in semantic search
- A just-deprecated directive might still be returned by Gel
- Chain membership changes lag behind CRDB

The 5-minute reconciliation loop means up to 5 minutes of inconsistency. For a learning system that adapts to feedback, this is probably fine. But the **merge logic in P1 needs to handle contradictions** — e.g., CRDB says directive is inactive but Meilisearch still returns it. The plan doesn't explicitly address this. P1 should filter all candidates against CRDB's `active` field as a post-merge pass.

### 5e. 🟡 Decomposition prompt stability

Decomposition quality depends on the LLM extraction prompt. Different prompt versions produce different directives from the same source document. But `decomposition_runs` tracks `prompt_version`. What happens when you update the prompt?

- Re-run all sources? (expensive, creates duplicates or requires bulk deprecation)
- Only apply to new sources? (inconsistent directive quality across sources)
- Versioned directive sets? (complexity explosion)

This isn't addressed. It should at least be an open question.

---

## 6. Open Questions Assessment

The 6 listed questions are real and relevant. Here's what's missing:

### Additional open questions the plan should include:

7. **Tokenizer selection** — which tokenizer, and is `token_cost` model-dependent? (See 4d above)

8. **Decomposition prompt versioning** — what happens when the extraction prompt changes? Re-decompose everything? (See 5e above)

9. **Directive content format** — are directive `content` fields plain text, markdown, or structured? The recomposition prompt needs to know. The injection response format matters for MCP plugin consumers.

10. **Chain ordering semantics** — what does `sequence_in_chain` mean at injection time? Must the agent follow them in order? Are they presented in order? What if only some chain members are relevant?

11. **Concurrent decomposition** — what happens if two agents ingest the same skill document simultaneously? The `ingestion_sources` table has `content_hash` for dedup, but the plan doesn't describe the locking/conflict resolution strategy for concurrent decomposition runs against the same source.

12. **MCP plugin contract** — the entire system exists to serve an MCP plugin, but the plugin's interface isn't described anywhere. What MCP tool names? What parameters? How does the plugin decide when to call inject vs. feedback? This is the integration surface that determines whether the system is usable.

---

## 7. Summary: Top 5 Actions

1. **Add the skill-replacement state management issues** (projects, tasks, sessions, skills registry, allium specs). This is half the system and it's completely missing from the build graph.

2. **Split P4** into outcome feedback (simple) and experience-derived directives (complex LLM pipeline).

3. **Merge S1 into S2.** Move slog/metrics out of X3 into an early infrastructure issue.

4. **Add a `skip_recomposition` flag to the inject API** to decouple P1 from P2 and provide a fast-path for latency-sensitive clients.

5. **Design `GraphStore` with a CRDB fallback path.** Gel is the riskiest dependency. Don't let it become a single point of failure for the injection pipeline.

---

_ZeroClaw 🌐 — review complete. The directive engine plan is solid. The skill-replacement gap is the critical miss._
