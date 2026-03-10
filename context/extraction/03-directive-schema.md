## Directive Schema v5 Summary

### What a Directive Is

An atomic behavioral instruction: "In situation X, agent should do Y." Has trigger conditions, instruction text, rationale, and verification criteria.

### Database Schemas

**CockroachDB** (source of truth)

Tables:

- `directives` — core table
  - `id` UUID PK, `content` TEXT, `kind` TEXT (behavioral/pattern/contextual/corrective/factual)
  - `source_type` TEXT, `source_id` TEXT, `source_name` TEXT, `source_section` TEXT
  - `trigger_tags` JSONB, `trigger_intent` TEXT, `trigger_phase` TEXT, `trigger_scope` TEXT
  - `times_injected/followed/ignored/negative` INT8, `effectiveness` FLOAT8 (computed)
  - `related_ids` JSONB, `supersedes_id` UUID nullable, `chain_id` UUID nullable
  - `weight` FLOAT8 (0–2), `token_cost` INT4, `active` BOOL, `tenant_id` UUID
  - `decomposition_run_id` UUID nullable, `source_text_hash` TEXT nullable
- `decomposition_runs` — batch provenance (source, model, prompt version, hash)
- `agent_sessions` — active agent contexts (agent_id, repo, phase)
- `injections` — what was served per session (context_hash, directives JSONB, tokens)
- `injection_outcomes` — per-directive feedback (followed/ignored/negative + evidence)
- `ingestion_sources` — dedup tracker (name+tenant unique, content_hash, last_ingested)

Key indexes: active+tenant, kind, phase, scope, effectiveness DESC, inverted on trigger_tags, chain_id, supersedes_id

**Meilisearch** (contextual discovery)

Index: `directives` (primaryKey: `id`)

- Searchable: `content`, `trigger_intent`, `trigger_tags`, `source_name`
- Filterable: `kind`, `trigger_phase`, `trigger_scope`, `active`, `tenant_id`, `chain_id`
- Sortable: `effectiveness`, `weight`, `created_at`
- Synonyms: debug↔fix/investigate, test↔spec/assertion, plan↔design/architect
- Sync: CRDB changefeed → upsert on create/update, delete on deactivate; 5-min reconciliation job

**Gel DB** (relationship graph)

Types:

- `Directive` — mirrors CRDB fields + `crdb_id` ref; links: `multi related_to`, `multi superseded_by`, `chain: DirectiveChain`, `sequence_in_chain: int32`
- `DirectiveChain` — `name`, `description`; `multi member directives` with `sequence_order: int32`; computed `total_tokens`, `avg_effectiveness`
- `Source` — `name` (unique), `source_type`; `multi produced: Directive`; computed `directive_count`, `avg_effectiveness`

### Decomposition Model

1. Parse skill doc into sections (deterministic, by headings/structure)
2. LLM call per section → extract atomic directives with content, kind, trigger_tags, trigger_intent, trigger_phase
3. Enrich: estimate token_cost, assign scope, set initial weight (anti-rationalization = 1.5–1.8)
4. Dedup via Meilisearch semantic similarity; multi-source duplicates get higher weight
5. Chain detection: group related directives by logical sequence → Gel DirectiveChain
6. Write to CRDB → async sync to Meilisearch + Gel

LLM involvement: one LLM call per section (Opus-class); also optional Sonnet-class call at injection time for contextualization.

Non-skill sources (no decomposition_run_id): `observation` (codebase scans), `user` (explicit preferences), `experience` (session feedback).

### Recomposition Model

At query time (given agent context + token budget):

1. **Meilisearch**: NL search with phase/tenant filters → up to 25 candidates with relevance scores
2. **CockroachDB**: structured filter by phase + scope → up to 50 candidates; merge+dedup with step 1
3. **Gel DB**: for merged set, expand behavioral chains to retrieve ordered siblings
4. **Rank**: `score = (relevance×0.4) + (effectiveness×0.3) + (weight×0.2) + (recency_bonus×0.1)`
5. **Budget**: greedy selection by score until token budget exhausted (default 500, reserve 50 for frame)
6. **Contextualize**: optional Sonnet call to rewrite raw directives for current situation

Dedup mechanisms: `previous_injection_id` avoids repeating last call; cooldown reduces weight of 3× ignored directives; phase gating excludes off-phase directives.

### Conflict Resolution

- **Explicit supersession** (`supersedes_id`): old directive suppressed, absolute
- **Score gap >10%**: higher-scored wins, lower suppressed for this injection
- **Score gap ≤10%**: both surfaced with conflict note to agent
- **Same chain**: chain `sequence_order` determines which step applies

Weight evolution:

- Negative outcome: `weight = GREATEST(weight × 0.8, 0.1)`; 3 negatives → auto-deprecate (`active=false`)
- Ignored 10× consecutive: flagged for review, weight reduced
- Supersession: new directive created with `supersedes_id`, old set inactive (immutable identity)
