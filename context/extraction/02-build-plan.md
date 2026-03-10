## Build Plan v5 Summary

### Phases

- **Phase 0: Foundation** — project layout, CRDB hardening, interfaces, LLM client, seed data

  - 0.1: Rename `cmd/app/` → `cmd/hive-server/`; create `internal/model/` (domain types), `internal/server/`
  - 0.2: Add goose migrations; fix CRDB idioms (TIMESTAMPTZ, UUID); CRDB retry wrapper (`crdb.RetryTx`)
  - 0.3: Compose store interfaces in `internal/model/store.go` (MemoryStore, TaskStore, AgentStore, Store)
  - 0.4: `internal/llm/` package — `Client` interface, Anthropic HTTP impl, NoopClient
  - 0.5: `seed/directives.json` (30–50 directives); `migrations/002_seed_directives.sql`; `internal/store/seed.go`
  - Deps: 0.1 first; 0.2/0.3/0.4 parallel; 0.5 after 0.2

- **Phase 1: Directive Storage** — directive schema, CRUD, ingest endpoint

  - 1.1: `internal/model/directive.go` (Directive, DirectiveKind, DirectiveFilter); `migrations/003_directives.sql`; `internal/store/directives.go`
  - 1.2: Handlers — `GET/PATCH /api/v1/directives{/id}`, `POST /api/v1/ingest` (storage-only, 202 Accepted)
  - Deps: 0.2 + 0.3

- **Phase 2: Meilisearch** — search layer, CockroachDB→Meili sync

  - 2.1: `internal/search/` — `Searcher` interface, `MeiliSearcher`, `NoopSearcher`; index config per vision §5.2
  - 2.2: `internal/search/sync.go` — `SyncManager`; async sync on write; 5-min reconcile loop; health endpoint
  - Deps: 1.1

- **Phase 3: Injection Pipeline** — core value delivery: `POST /api/v1/inject`

  - 3.1: `internal/inject/` — `Pipeline`; parallel fan-out to Meili + CRDB; ranking formula `(relevance*0.4)+(effectiveness*0.3)+(weight*0.2)+(recency*0.1)`; `migrations/004_sessions_and_injections.sql` (agent_sessions, injections, injection_outcomes); handler
  - 3.2: `internal/inject/recompose.go` — `Recomposer` interface; `LLMRecomposer` (Sonnet-class, 10–15s timeout, temp 0.4); `FallbackRecomposer` (raw content pass-through); integrate into Pipeline
  - Deps: 3.1 needs 1.1 + 2.1; 3.2 needs 0.4 + 3.1

- **Phase 4: Feedback Loop** — outcome tracking, effectiveness scoring

  - 4.1: `internal/model/feedback.go`; `internal/store/feedback.go` — `RecordOutcome` with atomic counter updates; `migrations/005_session_completions.sql`; handlers `POST /api/v1/feedback` and `POST /api/v1/feedback/session-complete`
  - Deps: 3.1

- **Phase 5: Gel DB** — relationship graph, chain traversal

  - 5.1: `dbschema/default.esdl` (Directive, DirectiveChain, Source); `internal/graph/` — `GraphStore` interface, `GelGraphStore`, `NoopGraphStore`
  - 5.2: Add Gel as third retrieval source in Pipeline; 2-phase fan-out (Meili+CRDB then Gel)
  - Deps: 5.1 needs 1.1; 5.2 needs 3.1 + 5.1

- **Phase 6: Decomposition** — LLM-powered skill→directive extraction

  - 6.1: `internal/decompose/` — `Engine`; deterministic sectioning (markdown headings/lists); LLM extraction (JSON schema); enrichment (token cost, weight, scope); deduplication via Meili similarity; async trigger from `/api/v1/ingest`
  - Deps: 0.4 + 1.1 + 2.2

- **Phase 7: Hardening** — observability, operational tooling
  - 7.1: Replace `internal/log/` with `log/slog`; `GET /metrics` (Prometheus); pipeline timing/recompose metrics
  - 7.2: `--sync-only` CLI flag; `script/backup`; `--check-integrity` CLI flag
  - Deps: 7.1 needs 3.2+4.1; 7.2 needs 2.2+5.1; parallel with each other

### Tech Stack Decisions

- **Primary DB:** CockroachDB (via pgx/v5 + goose migrations)
- **Search:** Meilisearch (`meilisearch-go`)
- **Graph:** Gel DB (`gel-go`) — EdgeQL, SDL schema
- **LLM:** Anthropic API (raw HTTP, no SDK) — Sonnet-class for both recompose and decompose
- **Framework:** Huma v2 on chi v5 (already integrated)
- **Migrations:** goose v3
- **Metrics:** Prometheus (standard exposition format)

### Migration Path

1. Rename `cmd/app/` → `cmd/hive-server/`; extract `internal/model/` and `internal/server/`
2. Replace inline DDL with goose migrations; fix TEXT timestamps → TIMESTAMPTZ, TEXT UUIDs → UUID
3. Replace local handler Store interface with `model.Store` composed interfaces
4. Add new tables (directives, sessions, injections) via migrations — existing tables unchanged
5. Existing endpoints (memory, tasks, agents) keep working throughout all phases

### Milestones / Deliverables

- **Phase 0 done:** Build passes, all existing tests green, CRDB idioms correct, LLM client testable
- **Phase 1 done:** Directives stored in CRDB; `/api/v1/directives` browsable; `/api/v1/ingest` accepts documents
- **Phase 2 done:** Directives searchable in Meilisearch; sync operational; health reports Meili status
- **Phase 3 done:** `POST /api/v1/inject` returns contextually ranked, LLM-recomposed directives; fallback works
- **Phase 4 done:** Feedback updates effectiveness scores; negative outcomes reduce weight; session-complete records insights
- **Phase 5 done:** Chain/relationship traversal enriches injection results
- **Phase 6 done:** Ingested skill documents auto-decompose into atomic directives; dedup prevents bloat
- **Phase 7 done:** Prometheus metrics live; full re-sync and integrity check operable from CLI
