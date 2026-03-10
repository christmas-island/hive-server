## Vision v5 Summary

### Core Concept

Hive-server is a **behavioral knowledge engine** that decomposes skill/methodology documents into atomic behavioral directives, stores them across three purpose-built databases, and retrieves contextually relevant directives for injection into LLM agent conversations.

---

### Components to Build

- **Decomposition Pipeline**: LLM-based extraction of atomic directives from markdown docs

  - Sectioning (deterministic markdown parsing by headings)
  - LLM analysis per section → JSON directive array
  - Enrichment (token cost, cross-refs, scope, weight)
  - Deduplication (semantic similarity merge)
  - Chain detection (group related directives into ordered sequences)
  - Interfaces: `POST /api/v1/ingest`, `GET /api/v1/ingest/{id}`

- **Injection Pipeline**: Context-aware directive retrieval and recomposition

  - Parallel query: Meilisearch (semantic) + CRDB (structured) + Gel (chain traversal)
  - Ranking: `score = relevance*0.4 + effectiveness*0.3 + weight*0.2 + recency*0.1`
  - Token budget selection (greedy, with diminishing-returns cutoff)
  - Contextualization via Sonnet-class LLM (adapt raw directive to current situation; fallback to raw on timeout)
  - Session dedup, phase gating, cooldown for ignored directives
  - Interface: `POST /api/v1/inject`

- **Feedback Loop**: Outcome tracking and directive evolution

  - Record followed/ignored/negative outcomes per directive
  - Update effectiveness, weight; auto-deprecate on 3 negative outcomes
  - Generate experience-derived directives from session summaries
  - Interfaces: `POST /api/v1/feedback`, `POST /api/v1/feedback/session-complete`

- **Admin/Debug API**

  - `GET /api/v1/directives` — browse/search catalog
  - `GET /api/v1/directives/{id}` — single directive with metadata
  - `PATCH /api/v1/directives/{id}` — adjust weight/active
  - `GET /health`, `GET /ready`

- **Async Sync Workers**: CRDB write → async index to Meilisearch + Gel; reconciliation every 5 min

---

### Data Model

**CockroachDB tables:**

- `directives` — full directive catalog (id, content, kind, source*\*, trigger*\*, effectiveness metrics, weight, token_cost, active, tenant_id)
- `agent_sessions` — active agent session state (agent_id, repo, project_id, phase, summary)
- `injections` — injection history (session_id, context_hash, directives JSONB, tokens_used)
- `injection_outcomes` — per-directive outcomes (injection_id, directive_id, outcome, evidence)
- `ingestion_sources` — dedup tracking for ingested documents (name, content_hash, version, directives_count)

**Meilisearch index: `directives`**

- Searchable: content, trigger_intent, trigger_tags, source_name
- Filterable: kind, trigger_phase, trigger_scope, active, tenant_id, chain_id
- Sortable: effectiveness, weight, created_at
- BM25 keyword search (no vector/semantic — see Decision 003)

**Gel DB schema:**

- `Directive` — nodes with relationships (related_to, superseded_by, chain link)
- `DirectiveChain` — ordered sequences of directives (name, description, sequence_order)
- `Source` — origin nodes with aggregate effectiveness

**Key relationships:**

- Directive → DirectiveChain (many-to-one, with sequence_order)
- Directive → Directive (related_to, superseded_by)
- CRDB is source of truth; Meilisearch/Gel are eventual-consistency replicas

---

### External Dependencies

- **CockroachDB** — transactional state store
- **Meilisearch** — BM25 full-text search (keyword only, no embeddings)
- **Gel DB** (EdgeDB fork) — graph/relationship traversal
- **LLM (Sonnet-class)** — directive decomposition and contextualization
- **Tokenizer** — token cost estimation per directive

---

### Open Questions / Ambiguities

- ~~Meilisearch "hybrid search" implies vector embeddings~~ Scoped to BM25 only per Decision 003
- "Sonnet-class" contextualization latency: 2.5–3.5s per inject call; no caching strategy described
- Feedback outcome attribution mechanism unspecified — how does MCP plugin determine `followed` vs `ignored`?
- Multi-tenancy: `tenant_id` present everywhere but provisioning/auth model not defined
- Gel sync worker: failure handling and conflict resolution not specified
- Codebase pattern ingestion (repo scans) — trigger/scheduling mechanism unspecified
- No explicit auth model for the 9 API endpoints beyond `Bearer <token>`
