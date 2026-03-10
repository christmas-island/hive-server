# Hive-Server v5: Build Graph (v3)

> Guiding principles: [GUIDANCE.md](../GUIDANCE.md) · Decisions: [context/decisions/](decisions/)

## What We're Building

A behavioral knowledge engine. It ingests skill documents (large markdown prompt libraries like GSD, Superpowers, Allium), decomposes them into atomic behavioral directives via LLM, stores those directives across three purpose-built databases, and recomposes contextually relevant subsets into micro-prompt snippets injected into agent conversations at runtime.

Workflows and processes (like "what does good software development look like?") are themselves behavioral chains — directive chains with gate conditions and phase progression. The engine doesn't just serve knowledge; it orchestrates behavioral sequences.

Injection is transparent to agents: the OpenClaw ContextEngine plugin (`assemble` hook) weaves directives into context during assembly. Feedback is transparent too: the `afterTurn` hook evaluates agent output against injected directives. Agents never explicitly call inject or feedback.

This is not a CRUD API. The existing hive-server (memory/tasks/agents endpoints) is the starting codebase, but the v5 architecture is fundamentally different in purpose.

## The Three Databases

Each serves a distinct retrieval need. All three participate in every injection query.

- **CockroachDB** — Source of truth. Transactional storage for directives, sessions, injections, outcomes. Structured filtering (by phase, scope, kind). Effectiveness counters. Tenant isolation.
- **Meilisearch** — Keyword discovery. BM25 keyword search over directive content. Returns relevance-scored candidates that structured queries can't find. Also used for dedup during decomposition. No embeddings — keyword search only.
- **Gel DB** — Relationship graph. Directive chains (ordered sequences), supersession links, source provenance, cross-directive relationships. Expands point queries into related directive neighborhoods. Backed by backups for recovery — no CRDB fallback needed.

CRDB is always authoritative. Meilisearch and Gel are eventual-consistency replicas synced from CRDB writes via in-process callbacks + 5-min reconciliation loops.

## The Two LLM-Powered Pipelines

### Decomposition (ingest-time)

Skill document → deterministic section parsing → LLM extraction per section → atomic directives with metadata → enrichment (token cost via chars/4, weight, scope) → keyword dedup via Meilisearch → chain detection → write to CRDB → in-process sync to Meilisearch + Gel.

Input: a markdown file. Output: ~100-300 directives per skill document. Batch ingest process, not concurrent agent-driven.

### Injection (query-time)

Triggered by OpenClaw ContextEngine `assemble` hook. Agent context (intent, phase, files, conversation summary) → parallel fan-out to all 3 databases → merge + deduplicate candidates → filter against CRDB `active` field (post-merge consistency pass) → rank by composite score `(relevance×0.4 + effectiveness×0.3 + weight×0.2 + recency×0.1)` → greedy token budget packing using pre-computed token_cost → optional LLM recomposition (`skip_recomposition=true` returns raw directives) → response with injection_id for feedback tracking.

Input: agent context. Output: contextual micro-prompts (or raw ranked directives).

### Feedback Loop

Triggered by OpenClaw ContextEngine `afterTurn` hook. LLM evaluates agent output against injected directives, reports per-directive outcomes (followed/ignored/negative). Negative outcomes decay directive weight (×0.8, floor 0.1). Three negatives auto-deprecate. Session completion can generate experience-derived directives (the system learns from agent behavior, not just from ingested documents).

## Build Graph

Issues are organized by what they produce, not when they ship. Dependencies are code-level ("this package imports that one"), not business-level.

### Infrastructure

**I1: Project layout restructure + structured logging**
Rename `cmd/app/` → `cmd/hive-server/`. Create `internal/model/` (domain types), `internal/server/` (server setup). Extract shared types from handler-local definitions. Migrate from `internal/log/` to `log/slog` throughout — every subsequent issue benefits from structured logging.

- Depends on: nothing
- Produces: clean import paths, structured logging for all subsequent work

**I2: CRDB schema hardening + goose migrations**
Replace inline DDL with goose v3 managed migrations. Fix existing schema: TEXT timestamps → TIMESTAMPTZ, TEXT UUIDs → UUID type. Add `crdb.RetryTx` wrapper for serialization conflict retries. Migration 001 = existing schema in goose format. Migration 002 = type corrections.

- Depends on: I1 (import paths)
- Produces: migration framework, correct CRDB types, retry-safe transactions

**I3: Store interface composition**
Define composed interfaces in `internal/model/store.go`: `MemoryStore`, `TaskStore`, `AgentStore`, `DirectiveStore`, `SessionStore`, `FeedbackStore` → composed `Store` interface. Refactor existing handler Store usage to use composed interface.

- Depends on: I1
- Produces: extensible store contract that all new storage implements against

**I4: LLM client package**
`internal/llm/` — `Client` interface with `Complete(ctx, messages, opts) (Response, error)`. `AnthropicClient` (raw HTTP to Messages API, no SDK). `NoopClient` (returns canned responses for tests). Configurable model, temperature, max tokens, timeout.

- Depends on: I1
- Produces: LLM abstraction used by decomposition, recomposition, and feedback attribution

### Storage — Directives in CockroachDB

**S2: Directive data model, CRDB schema + store**
`internal/model/directive.go` — `Directive` struct with all fields: id (UUID), content (plain text), kind (behavioral/pattern/contextual/corrective/factual), source*\* fields, trigger*\* fields, effectiveness counters, computed effectiveness, weight (0.0-2.0), token_cost (chars/4), chain_id, sequence_in_chain, supersedes_id, tenant_id, active, decomposition_run_id, source_text_hash. `DirectiveKind` enum. `DirectiveFilter` for queries.
Migration 003: `directives` table, `decomposition_runs` table, `ingestion_sources` table. All indexes (active+tenant, kind, phase, scope, effectiveness DESC, inverted on trigger_tags, chain_id, supersedes_id). `internal/store/directives.go` implementing `DirectiveStore` — Create, Get, List (with DirectiveFilter), Update (weight/active), bulk create for decomposition batches.

- Depends on: I2, I3
- Produces: directive data model + persistence layer

**S3: Session, injection, and workflow state CRDB schema + store**
Migration 004: `agent_sessions` (with workflow state: current_chain_id, current_step, completed_steps, blockers), `injections`, `injection_outcomes` tables. `internal/store/sessions.go` implementing `SessionStore` — upsert session, update workflow position, record injection, get recent injections for dedup. `internal/store/feedback.go` implementing `FeedbackStore` — record outcomes, atomic counter updates on directives, weight decay logic, auto-deprecation.

- Depends on: I2, I3, S2
- Produces: injection audit trail + feedback persistence + workflow progression tracking

### Storage — Meilisearch

**S4: Meilisearch search package**
`internal/search/` — `Searcher` interface: `Search(ctx, query, filters) ([]ScoredDirective, error)`, `SimilarTo(ctx, content, threshold) ([]ScoredDirective, error)` (for dedup). `MeiliSearcher` implementation — index configuration (searchable/filterable/sortable fields, synonyms). BM25 keyword search only — no embeddings, no vector mode. `NoopSearcher` for tests.

- Depends on: S2 (Directive type)
- Produces: keyword search capability

**S5: CRDB→Meilisearch sync manager**
`internal/search/sync.go` — `SyncManager`. In-process callback triggered from store `Create`/`Update` methods. Delete (or mark inactive) on directive deprecation. 5-minute full reconciliation loop (diff CRDB vs Meili by updated_at). Health reporting for readiness probe.

- Depends on: S2, S4
- Produces: eventually-consistent search index

### Storage — Gel DB

**S6: Gel DB graph store**
`dbschema/default.esdl` — EdgeQL schema: `Directive` type (mirrors key CRDB fields + `crdb_id`, `related_to` multi-link, `superseded_by` multi-link, `chain` link, `sequence_in_chain`). `DirectiveChain` type (name, description, ordered members, computed total_tokens, avg_effectiveness, gate conditions per link). `Source` type (name, source_type, produced directives, computed counts).
`internal/graph/` — `GraphStore` interface: `GetChain(ctx, chainID)`, `GetRelated(ctx, directiveIDs)`, `ExpandChains(ctx, directiveIDs)`, `CheckGateConditions(ctx, chainID, sessionState)`. `GelGraphStore` implementation. `NoopGraphStore` for tests.

- Depends on: S2
- Produces: relationship/chain traversal + gate condition evaluation

**S7: CRDB→Gel sync manager**
In-process callback sync on directive create/update — mirror to Gel types, maintain relationship links. 5-minute reconciliation loop (same pattern as S5). Handle Gel-specific concerns: chain membership updates, supersession link maintenance, source aggregation.

- Depends on: S2, S6
- Produces: eventually-consistent graph replica

### Pipeline — Injection

**P1: Injection pipeline core**
`internal/inject/` — `Pipeline` struct. `POST /api/v1/inject` handler. Request validation (agent_id from X-Agent-ID, session_id, context with intent/files/repo/phase/conversation_summary/token_budget, `skip_recomposition` bool flag). Parallel errgroup fan-out to Meilisearch + CRDB + Gel. Merge + exact-ID dedup. Post-merge CRDB `active` field filter (consistency pass — catches stale Meili/Gel results). Ranking by composite score formula. Greedy token budget packing using pre-computed token_cost (chars/4). Session dedup (exclude directives from previous_injection_id). Cooldown penalty for 3× ignored directives. Phase gating. Workflow-aware chain expansion: if session has current_chain_id, expand and surface next gated step. Graceful degradation: drop Gel first, then Meilisearch, CRDB always available. Store injection record. Return ranked directive set (raw when skip_recomposition=true).
Cache interface (`Cache` with `NoopCache` default) — designed for future implementation without restructuring.

- Depends on: S2, S3, S4, S6 (all three storage backends + session/injection tracking)
- Produces: the core query endpoint (independently valuable without recomposition)

**P2: LLM recomposition layer**
`internal/inject/recompose.go` — `Recomposer` interface: `Recompose(ctx, *RecompositionInput) (*RecompositionOutput, error)`. `LLMRecomposer`: calls Sonnet-class model with fixed system prompt + selected directives + agent context → structured JSON (snippets[] with directive_ids/content/kind/action/reasoning + skipped[] with reasons). Temperature 0.0. Post-parse token trim (drop lowest-weight snippets until within budget). `FallbackRecomposer`: returns raw directive content on LLM error/timeout. Metrics: latency, fallback rate, tokens in/out, error rate, skip rate.

- Depends on: I4, P1
- Produces: contextual micro-prompt synthesis

### Pipeline — Decomposition

**P3: Decomposition engine**
`internal/decompose/` — `Engine` struct. Deterministic markdown sectioning (split by headings/structure, not LLM). LLM extraction per section → structured JSON array of directive candidates (content as plain text, kind, trigger_tags, trigger_intent, trigger_phase). Enrichment pass: estimate token_cost (chars/4), assign scope, set initial weight (anti-rationalization directives get 1.5-1.8). Keyword dedup via Meilisearch `SimilarTo` (multi-source duplicates get higher weight instead of being deleted). Chain detection: group logically sequential directives into DirectiveChain entries. Batch write to CRDB → in-process sync callbacks to Meilisearch + Gel. Provenance tracked in `decomposition_runs` table. No prompt versioning — the system evolves; old directives are weeded by feedback loops and superseded via `supersedes_id`.
Wired into `POST /api/v1/ingest` — upgrades from storage-only (202) to full decomposition pipeline. `GET /api/v1/ingest/{id}` returns status.

- Depends on: I4, S2, S4, S5, S6, S7 (needs all three storage backends + sync managers + LLM client)
- Produces: automated skill→directive extraction

### Pipeline — Feedback

**P4a: Outcome feedback + weight evolution**
`POST /api/v1/feedback` handler — accepts per-directive outcomes (followed/ignored/negative with evidence). Atomic counter updates on directives table. Weight decay on negative (×0.8, floor 0.1). Auto-deprecation after 3 cumulative negatives (active=false). Cooldown tracking for ignored directives. Update workflow state in agent_sessions (advance chain position on followed outcomes).

- Depends on: S2, S3
- Produces: the learning loop that makes directives evolve + workflow progression

**P4b: Experience-derived directive generation**
`POST /api/v1/feedback/session-complete` handler — accepts session summary + outcome. LLM analyzes session to extract behavioral patterns worth capturing. Generates experience-derived directives (source_type=experience, no decomposition_run_id). Keyword dedup via Meilisearch to avoid duplicating existing directives. In-process sync to Meili + Gel.

- Depends on: P4a, I4, S4, S5, S7
- Produces: system learns from agent behavior, not just ingested documents

### Integration

**X1: Directive API handlers**
`GET /api/v1/directives` — list with filters (kind, phase, scope, tenant, active, search query via Meilisearch). `GET /api/v1/directives/{id}` — single directive with full metadata + Gel relationships. `PATCH /api/v1/directives/{id}` — update weight, active status. Admin/debug surface for inspecting the directive catalog.

- Depends on: S2, S4, S6
- Produces: human-readable window into the knowledge base

**X2: Seed directive catalog**
`seed/directives.json` — 30-50 hand-crafted behavioral directives covering common agent patterns (testing, code review, error handling, planning). Include 5-10 golden test cases with known-good trigger conditions and expected injection scenarios (regression tests for the full pipeline). Goose migration to load. `internal/store/seed.go` loader. These bootstrap the system before any skill documents are decomposed.

- Depends on: S2
- Produces: non-empty directive catalog for testing and early integration. Should run early — having real directives makes every subsequent issue's testing better.

**X3: Observability + operational tooling**
`GET /metrics` (Prometheus standard format). Pipeline timing metrics, recompose latency, fallback rate, fan-out per-source latency, sync lag. `--sync-only` CLI flag (run Meili+Gel reconciliation then exit). `script/backup`. `--check-integrity` CLI flag (verify CRDB↔Meili↔Gel consistency). Config validation pass.

- Depends on: P1, P2, P4a, S5, S7
- Produces: production operability

**X4: Integration test infrastructure**
Docker Compose or testcontainers for CRDB + Meilisearch + Gel. E2E smoke test: ingest skill doc → decompose → inject → feedback → verify weight change. Verify sync managers, fan-out, merge logic across all three databases. CI pipeline configuration.

- Depends on: P1, P3, P4a (needs full pipeline to test end-to-end)
- Produces: confidence that the three-database system works as a unit

## Resolved Design Decisions

All captured in `context/decisions/001-010.md`:

1. **Tokenizer:** chars/4 approximation. No embeddings, no GPU dependency.
2. **Prompt versioning:** None. Living system, feedback loop handles lifecycle.
3. **Content format:** Plain text. Optimized for BM25 search.
4. **Chain ordering:** Present in sequence order, don't enforce execution order.
5. **Concurrent decomposition:** Not a concern. Batch ingest, CRDB unique constraint.
6. **Injection mechanism:** OpenClaw ContextEngine hooks (assemble, afterTurn).
7. **Workflows:** Directive chains ARE workflows. Not a separate system.
8. **Gel DB fallback:** None needed. Backups for recovery.
9. **Structural:** S1→S2, P4 split, slog in I1, skip_recomposition flag.
10. **Cross-cutting:** Auth existing is fine. Sync in-process. Tests need issue. Config per-storage.

## Open Questions

All 12 original questions resolved. Remaining unknowns are implementation-level:

1. **Token budget default** — tune during integration testing. Configurable per-request.
2. **ContextEngine hook contract** — exact `assemble` hook API (what context is available, return format) needs research against OpenClaw 2026.3.7 source.
3. **Feedback attribution precision** — how precisely can the `afterTurn` LLM judge followed/ignored/negative? May need iterative prompt tuning.
4. **Codebase pattern ingestion** — source_type=observation triggers (webhook? cron? manual?). Deferred — build the pipeline first, add observation sources later.

## Issue Count: 19

- Infrastructure: 4 (I1-I4)
- Storage: 6 (S2-S7)
- Pipeline: 5 (P1-P3, P4a, P4b)
- Integration: 4 (X1-X4)
- Workflow state: integrated into S3, S6, P1, P4a (not separate issues)
- Note: S1 was merged into S2 per ZeroClaw review (hence 19, not 20)
