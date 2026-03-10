# Hive-Server v5: Build Graph

## What We're Building

A behavioral knowledge engine. It ingests skill documents (large markdown prompt libraries like GSD, Superpowers, Allium), decomposes them into atomic behavioral directives via LLM, stores those directives across three purpose-built databases, and recomposes contextually relevant subsets into micro-prompt snippets injected into agent conversations at runtime.

This is not a CRUD API. The existing hive-server (memory/tasks/agents endpoints) is the starting codebase, but the v5 architecture is fundamentally different in purpose.

## The Three Databases

Each serves a distinct retrieval need. All three participate in every injection query.

- **CockroachDB** — Source of truth. Transactional storage for directives, sessions, injections, outcomes. Structured filtering (by phase, scope, kind). Effectiveness counters. Tenant isolation.
- **Meilisearch** — Semantic discovery. Hybrid keyword + vector search over directive content. Returns relevance-scored candidates that structured queries can't find. Also used for dedup during decomposition.
- **Gel DB** — Relationship graph. Directive chains (ordered sequences), supersession links, source provenance, cross-directive relationships. Expands point queries into related directive neighborhoods.

CRDB is always authoritative. Meilisearch and Gel are eventual-consistency replicas synced from CRDB writes.

## The Two LLM-Powered Pipelines

### Decomposition (ingest-time)

Skill document → deterministic section parsing → LLM extraction per section → atomic directives with metadata → enrichment (token cost, weight, scope) → semantic dedup via Meilisearch → chain detection → write to CRDB → async sync to Meilisearch + Gel.

Input: a markdown file. Output: ~100-300 directives per skill document.

### Injection (query-time)

Agent request (context, intent, phase, files, conversation summary) → parallel fan-out to all 3 databases → merge + deduplicate candidates → rank by composite score `(relevance×0.4 + effectiveness×0.3 + weight×0.2 + recency×0.1)` → greedy token budget packing → LLM recomposition (Sonnet-class synthesizes raw directives into situational micro-prompts) → response with injection_id for feedback tracking.

Input: agent context (~500-2000 tokens). Output: 5-12 contextual micro-prompts.

### Feedback Loop

After injection, the MCP plugin reports per-directive outcomes (followed/ignored/negative). Negative outcomes decay directive weight (×0.8, floor 0.1). Three negatives auto-deprecate. Session completion can generate experience-derived directives (the system learns from agent behavior, not just from ingested documents).

## Build Graph

Issues are organized by what they produce, not when they ship. Dependencies are code-level ("this package imports that one"), not business-level.

### Infrastructure

**I1: Project layout restructure**
Rename `cmd/app/` → `cmd/hive-server/`. Create `internal/model/` (domain types), `internal/server/` (server setup). Extract shared types from handler-local definitions. Non-breaking — existing endpoints keep working.

- Depends on: nothing
- Produces: clean import paths for all subsequent work

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
- Produces: LLM abstraction used by both decomposition and recomposition

### Storage — Directives in CockroachDB

**S1: Directive data model**
`internal/model/directive.go` — `Directive` struct with all fields from schema doc: id (UUID), content, kind (behavioral/pattern/contextual/corrective/factual), source*\* fields, trigger*\* fields, effectiveness counters (times_injected/followed/ignored/negative), computed effectiveness, weight (0.0-2.0), token_cost, chain_id, supersedes_id, tenant_id, active, decomposition_run_id, source_text_hash. `DirectiveKind` enum. `DirectiveFilter` for queries.

- Depends on: I1
- Produces: core domain type everything else references

**S2: Directive CRDB schema + store**
Migration 003: `directives` table, `decomposition_runs` table, `ingestion_sources` table. All indexes (active+tenant, kind, phase, scope, effectiveness DESC, inverted on trigger_tags, chain_id, supersedes_id). `internal/store/directives.go` implementing `DirectiveStore` — Create, Get, List (with DirectiveFilter), Update (weight/active), bulk create for decomposition batches.

- Depends on: I2, I3, S1
- Produces: directive persistence layer

**S3: Session + injection CRDB schema + store**
Migration 004: `agent_sessions`, `injections`, `injection_outcomes` tables. `internal/store/sessions.go` implementing `SessionStore` — upsert session, record injection, get recent injections for dedup. `internal/store/feedback.go` implementing `FeedbackStore` — record outcomes, atomic counter updates on directives, weight decay logic, auto-deprecation.

- Depends on: I2, I3, S1
- Produces: injection audit trail + feedback persistence

### Storage — Meilisearch

**S4: Meilisearch search package**
`internal/search/` — `Searcher` interface: `Search(ctx, query, filters) ([]ScoredDirective, error)`, `SimilarTo(ctx, content, threshold) ([]ScoredDirective, error)` (for dedup). `MeiliSearcher` implementation — index configuration (searchable/filterable/sortable fields, synonyms per vision §5.2). `NoopSearcher` for tests.

- Depends on: S1 (Directive type)
- Produces: semantic search capability

**S5: CRDB→Meilisearch sync manager**
`internal/search/sync.go` — `SyncManager`. Async upsert triggered on directive create/update. Delete (or mark inactive) on directive deprecation. 5-minute full reconciliation loop (diff CRDB vs Meili by updated_at). Health reporting for readiness probe.

- Depends on: S2, S4
- Produces: eventually-consistent search index

### Storage — Gel DB

**S6: Gel DB graph store**
`dbschema/default.esdl` — EdgeQL schema: `Directive` type (mirrors key CRDB fields + `crdb_id`, `related_to` multi-link, `superseded_by` multi-link, `chain` link, `sequence_in_chain`). `DirectiveChain` type (name, description, ordered members, computed total_tokens, avg_effectiveness). `Source` type (name, source_type, produced directives, computed counts).
`internal/graph/` — `GraphStore` interface: `GetChain(ctx, chainID)`, `GetRelated(ctx, directiveIDs)`, `ExpandChains(ctx, directiveIDs)`. `GelGraphStore` implementation. `NoopGraphStore` for tests.

- Depends on: S1
- Produces: relationship/chain traversal capability

**S7: CRDB→Gel sync manager**
Async sync on directive create/update — mirror to Gel types, maintain relationship links. Reconciliation loop (similar pattern to S5). Handle Gel-specific concerns: chain membership updates, supersession link maintenance, source aggregation.

- Depends on: S2, S6
- Produces: eventually-consistent graph replica

### Pipeline — Injection

**P1: Injection pipeline core**
`internal/inject/` — `Pipeline` struct. `POST /api/v1/inject` handler. Request validation (agent_id from X-Agent-ID, session_id, context with intent/files/repo/phase/conversation_summary/token_budget). Parallel errgroup fan-out to Meilisearch + CRDB + Gel. Merge + exact-ID dedup. Ranking by composite score formula. Greedy token budget packing using pre-computed token_cost. Session dedup (exclude directives from previous_injection_id). Cooldown penalty for 3× ignored directives. Phase gating. Store injection record. Return ranked directive set.

- Depends on: S2, S3, S4, S6 (all three storage backends + session/injection tracking)
- Produces: the core query endpoint (pre-recomposition — returns raw directives)

**P2: LLM recomposition layer**
`internal/inject/recompose.go` — `Recomposer` interface: `Recompose(ctx, *RecompositionInput) (*RecompositionOutput, error)`. `LLMRecomposer`: calls Sonnet-class model with fixed system prompt + selected directives + agent context → structured JSON (snippets[] with directive_ids/content/kind/action/reasoning + skipped[] with reasons). Temperature 0.0. Post-parse token trim (drop lowest-weight snippets until within budget). `FallbackRecomposer`: returns raw directive content on LLM error/timeout. `RecomposerConfig`: model, API key, max latency, output token budget, temperature, fallback toggle. 6 metrics: latency, fallback rate, tokens in/out, error rate, skip rate.

- Depends on: I4, P1
- Produces: contextual micro-prompt synthesis (the thing that makes raw directives actually useful)

### Pipeline — Decomposition

**P3: Decomposition engine**
`internal/decompose/` — `Engine` struct. Deterministic markdown sectioning (split by headings/structure, not LLM). LLM extraction per section → structured JSON array of directive candidates (content, kind, trigger_tags, trigger_intent, trigger_phase). Enrichment pass: estimate token_cost, assign scope, set initial weight (anti-rationalization directives get 1.5-1.8). Semantic dedup via Meilisearch `SimilarTo` (multi-source duplicates get higher weight instead of being deleted). Chain detection: group logically sequential directives into DirectiveChain entries. Batch write to CRDB → async sync to Meilisearch + Gel. Provenance tracked in `decomposition_runs` table.
Wired into `POST /api/v1/ingest` — upgrades from storage-only (202) to full decomposition pipeline. `GET /api/v1/ingest/{id}` returns status.

- Depends on: I4, S2, S4, S5, S6, S7 (needs all three storage backends + sync managers + LLM client)
- Produces: automated skill→directive extraction

### Pipeline — Feedback

**P4: Feedback + effectiveness evolution**
`POST /api/v1/feedback` handler — accepts per-directive outcomes (followed/ignored/negative with evidence). Atomic counter updates on directives table. Weight decay on negative (×0.8, floor 0.1). Auto-deprecation after 3 cumulative negatives (active=false). Cooldown tracking for ignored directives.
`POST /api/v1/feedback/session-complete` handler — accepts session summary + outcome. Generates experience-derived directives (source_type=experience, no decomposition_run_id). These are directives the system learns from agent behavior, not from ingested documents.

- Depends on: S2, S3
- Produces: the learning loop that makes directives evolve over time

### Integration

**X1: Directive API handlers**
`GET /api/v1/directives` — list with filters (kind, phase, scope, tenant, active, search query via Meilisearch). `GET /api/v1/directives/{id}` — single directive with full metadata + Gel relationships. `PATCH /api/v1/directives/{id}` — update weight, active status. Admin/debug surface for inspecting the directive catalog.

- Depends on: S2, S4, S6
- Produces: human-readable window into the knowledge base

**X2: Seed directive catalog**
`seed/directives.json` — 30-50 hand-crafted behavioral directives covering common agent patterns (testing, code review, error handling, planning). Goose migration to load. `internal/store/seed.go` loader. These bootstrap the system before any skill documents are decomposed — they're the "common sense" baseline.

- Depends on: S2
- Produces: non-empty directive catalog for testing and early integration

**X3: Observability + operational tooling**
Replace `internal/log/` with `log/slog`. `GET /metrics` (Prometheus standard format). Pipeline timing metrics, recompose latency, fallback rate, fan-out per-source latency, sync lag. `--sync-only` CLI flag (run Meili+Gel reconciliation then exit). `script/backup`. `--check-integrity` CLI flag (verify CRDB↔Meili↔Gel consistency).

- Depends on: P1, P2, P4, S5, S7
- Produces: production operability

## Open Questions

These are underspecified in the design docs and need answers before or during implementation:

1. **Embedding model for Meilisearch hybrid search** — Meilisearch's hybrid mode requires vector embeddings. Which embedding model/provider? OpenAI text-embedding-3-small (already used for memory-core)? Self-hosted?
2. **Feedback attribution** — How does the MCP plugin determine whether an agent "followed" vs "ignored" a directive? Is this LLM-judged from conversation analysis? Heuristic?
3. **Multi-tenancy provisioning** — `tenant_id` is on every table but there's no tenant CRUD, auth model, or provisioning flow described. Is this just a future-proofing column, or does it need to work at launch?
4. **Gel DB sync failure handling** — What happens when Gel is down during a directive write? Queue? Retry? Just let reconciliation catch it?
5. **Codebase pattern ingestion** — The vision mentions learning from repo scans (source_type=observation). What triggers these? Webhook? Cron? Manual?
6. **Recomposition caching** — No cache in v1 per the docs, but 3-4s per inject call may be too slow for real-time MCP use. Should we design the cache interface even if we don't implement it yet?

## Issue Count: 18

- Infrastructure: 4 (I1-I4)
- Storage: 7 (S1-S7)
- Pipeline: 4 (P1-P4)
- Integration: 3 (X1-X3)
