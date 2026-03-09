# Final Review v4: Quality Gate for Build Plan v4

**Date:** 2026-03-09
**Status:** Final quality gate review before implementation
**Documents reviewed:** build-plan-v4.md (primary), vision-v4.md, recomposition-design.md, directive-schema.md, injection-pipeline.md (Sections 1-3, 5-8), final-review-v3.md, hive-server-current.md, cockroachdb.md, meilisearch.md, gel-db.md, skill-replacement-analysis.md
**Method:** Full cross-document schema comparison, codebase inspection, technology reference validation, previous-issue tracking

---

## 0. Executive Summary

Build plan v4 is a materially improved document. The seven structural changes from v3 are real and correct: LLM recomposition replaces templates, the LLM client moves early, the fabricated latency constraint is gone, directive storage is simpler, caching is layered, and the authoritative schema is declared. The final-review-v3 issue tracker is addressed with specificity -- each item has a concrete resolution or an explicit deferral with rationale.

There are four remaining issues: two moderate (Go struct vs SQL enum type mismatch, `static_content` fallback reference in recomposition-design.md that contradicts build-plan-v4), one minor (hive-server-current.md is stale on two points), and one advisory (the Gel SDL in build-plan-v4 Step 5.1 has minor syntax discrepancies from directive-schema.md Section 4). None are blocking.

**Verdict: Ready to execute.** The plan is internally consistent, technically accurate, and each step is handable to an LLM agent as a prompt. Begin Phase 0.

---

## 1. Internal Consistency

### 1.1 Schema Consistency: RESOLVED

final-review-v3 Issue C1 (three conflicting directive schemas) is the most important consistency check.

build-plan-v4 opens with an explicit "Authoritative Schema Declaration" section that names directive-schema.md as the single source of truth and lists the authoritative field names: `directive_type` (not `kind`), `priority` INT 1-100 (not `weight` float), `context_triggers` JSONB (not separate trigger fields). This is unambiguous.

Cross-checking build-plan-v4's Go structs against directive-schema.md Section 2:

| Field             | directive-schema.md (SQL)                         | build-plan-v4 Step 1.1 (Go)                  | Match?                   |
| ----------------- | ------------------------------------------------- | -------------------------------------------- | ------------------------ |
| Type field        | `directive_type directive_type NOT NULL` (enum)   | `DirectiveType DirectiveType` (string const) | See note 1               |
| Type values       | behavioral, procedural, contextual, guardrail     | Same four values                             | Yes                      |
| Priority          | `priority INT4 NOT NULL DEFAULT 50`               | `Priority int`                               | Yes                      |
| Effectiveness     | `effectiveness_score FLOAT8 NOT NULL DEFAULT 0.5` | `EffectivenessScore float64`                 | Yes                      |
| Source skill      | `source_skill skill_source NOT NULL` (enum)       | `SourceSkill string`                         | See note 2               |
| Triggers          | `context_triggers JSONB NOT NULL DEFAULT '{}'`    | `ContextTriggers map[string]any`             | Yes                      |
| Active flag       | `is_active BOOL NOT NULL DEFAULT true`            | `IsActive bool`                              | Yes                      |
| Rationale         | `rationale TEXT NOT NULL DEFAULT ''`              | `Rationale string`                           | Yes                      |
| Verification      | `verification_criteria TEXT NOT NULL DEFAULT ''`  | `VerificationCriteria string`                | Yes                      |
| Version           | `version INT4 NOT NULL DEFAULT 1`                 | `Version int`                                | Yes                      |
| Supersedes        | `supersedes_id UUID`                              | `SupersedesID *string`                       | Yes                      |
| Decomposition run | `decomposition_run_id UUID NOT NULL REFERENCES`   | `DecompositionRunID *string`                 | See note 3               |
| Tags              | Separate `directive_tags` table                   | `Tags []string` (on struct)                  | Yes (denormalized in Go) |

**Note 1:** The Go type uses `DirectiveType string` with const values. The SQL uses a `CREATE TYPE directive_type AS ENUM`. This is correct practice -- Go does not have native enums, so string constants with validation are standard. The pgx driver handles the enum-to-string mapping transparently.

**Note 2:** directive-schema.md defines `source_skill` as a `skill_source` enum (superpowers, gsd, allium, custom, derived). build-plan-v4's Go struct defines `SourceSkill string`. This works -- pgx maps PostgreSQL enums to Go strings. However, the Go code should validate against the allowed values on input. The build plan does not explicitly call this out.

**Note 3:** directive-schema.md marks `decomposition_run_id` as `NOT NULL`. build-plan-v4's Go struct marks it as `*string` (nullable). This is a real discrepancy. Seed directives (Step 0.5) have no decomposition run -- they are manually curated. Either the SQL column needs to allow NULL (add a sentinel "manual" decomposition run), or the seed loading must create a bootstrap decomposition run to reference. **This needs resolution during Step 0.5 implementation.** The cleanest fix: make `decomposition_run_id` nullable in the schema (`UUID REFERENCES decomposition_runs(id)` without `NOT NULL`) or create a well-known "seed" decomposition run during migration 002.

**Overall:** Schema consistency is good. The authoritative declaration works. The one real discrepancy (decomposition_run_id nullability for seeds) is easily fixed and is flagged here so the implementor catches it.

### 1.2 Endpoint Counts: CONSISTENT

Counting from build-plan-v4:

**Existing (kept, 12):**

- Memory: POST, GET (list), GET (by key), DELETE = 4
- Tasks: POST, GET (list), GET (by id), PATCH, DELETE = 5
- Agents: POST (heartbeat), GET (list), GET (by id) = 3

**New (10):**

- Directives CRUD: POST, GET (list), GET (by id), PATCH, DELETE = 5 (Step 1.2)
- Ingest: POST /api/v1/ingest = 1 (Step 1.2)
- Ingest status: GET /api/v1/ingest/{run_id} = 1 (Step 6.1)
- Inject: POST /api/v1/inject = 1 (Step 3.1)
- Feedback: POST /api/v1/feedback = 1 (Step 4.1)
- Session complete: POST /api/v1/feedback/session-complete = 1 (Step 4.1)

**Admin (3):**

- Sync: POST /api/v1/admin/sync = 1 (Step 2.2)
- Backup: POST /api/v1/admin/backup = 1 (Step 7.2)
- Integrity: POST /api/v1/admin/integrity = 1 (Step 7.2)

**Infrastructure (3):**

- GET /health, GET /ready (existing)
- GET /metrics (Step 7.1)

**Total: 12 existing + 10 new + 3 admin + 1 metrics = 26 endpoints.** This is 4 more than final-review-v3's count of 22 because v4 added GET /api/v1/ingest/{run_id}, POST /admin/backup, POST /admin/integrity, and GET /metrics. All reasonable additions.

vision-v4 does not specify exact endpoint counts (it describes the pipeline conceptually), so there is no conflict.

### 1.3 Build Plan Steps Implement What Vision Describes: YES

| Vision v4 Concept         | Build Plan v4 Step                  | Implementation                                   |
| ------------------------- | ----------------------------------- | ------------------------------------------------ |
| Decompose skill documents | Phase 6 (Step 6.1)                  | Sectioning + LLM extraction + enrichment + dedup |
| Store across 3 databases  | Phases 1 (CRDB), 2 (Meili), 5 (Gel) | Schema + CRUD + sync                             |
| Retrieve via fan-out      | Step 3.1                            | Parallel Meili + CRDB queries with errgroup      |
| Recompose via LLM         | Step 3.2                            | LLMRecomposer with Haiku-class model             |
| Feedback loop             | Step 4.1                            | Feedback + effectiveness scoring                 |
| Token budgeting           | Step 3.1 (ranking)                  | Greedy packing with guardrail guarantee          |
| Session dedup             | Step 3.1 (injection_log)            | injection_log table, previous_injection_id       |
| Behavioral chains (Gel)   | Step 5.2                            | Graph-enhanced injection with chain traversal    |

No vision concept is missing from the build plan. No build plan step implements something the vision does not describe.

### 1.4 Recomposition-Design.md vs Build-Plan-v4: ONE DISCREPANCY

recomposition-design.md Section 7 (Fallback) describes a "Fallback Tier 3: Static Content Field" that says: "If the directive has a `static_content` field (a pre-written, context-free version of the instruction), use that." This references a field that does not exist in directive-schema.md or build-plan-v4. The `static_content` field was from the template-based approach that recomposition-design.md itself superseded.

build-plan-v4 Step 3.2 correctly implements only two fallback levels: (1) pre-synthesis cache entries, (2) raw directive `content` as-is. It does not reference `static_content`.

**Impact:** Low. The build plan is correct. recomposition-design.md has a vestigial paragraph from an earlier draft. Any implementor reading recomposition-design.md Section 7 will encounter a reference to a non-existent field and may be confused.

**Recommendation:** Remove or annotate "Fallback Tier 3: Static Content Field" in recomposition-design.md as vestigial. The build plan already ignores it.

---

## 2. Previous Issues (final-review-v3): All 13 Addressed

### Critical Issues

**C1: Three conflicting directive schemas.**
Status: **RESOLVED.** build-plan-v4 opens with an authoritative schema declaration naming directive-schema.md as the single source of truth. Field names, types, and enum values are specified. All Go structs in the build plan match directive-schema.md. The one nullable discrepancy (decomposition_run_id) is minor and flagged in Section 1.1 above.

**C2: LLM contextualization -- vision says yes, injection-pipeline says no.**
Status: **RESOLVED, but the resolution changed direction.** final-review-v3 recommended adopting injection-pipeline.md's template approach. build-plan-v4 went the other way: LLM wins. recomposition-design.md replaces injection-pipeline.md Section 4. Templates are gone entirely. This is explicitly acknowledged in the "What Changed" section and in the issue resolution table. The build plan implements LLMRecomposer (Step 3.2) with FallbackRecomposer as the degradation path. This is a coherent, well-justified reversal of the v3 reviewer's recommendation, backed by the three concrete examples in recomposition-design.md Section 9 that demonstrate the quality gap.

### Significant Issues

**S1: No seed directive bootstrap.**
Status: **RESOLVED.** Step 0.5 adds 30-50 curated directives embedded as JSON via `//go:embed`, loaded on first migration. The step includes specific examples drawn from the skill-replacement-analysis. Acceptance criteria require idempotent loading. This directly implements final-review-v3's recommendation.

**S2: Gel schema mismatch (build plan used simplified vision-v4 schema).**
Status: **RESOLVED.** Step 5.1 explicitly states "per **directive-schema.md Section 4** (authoritative, not the simplified vision-v4 version)" and includes the full SDL with 6 relationship types, link properties (strength, description), computed properties, and indexes. The SDL in build-plan-v4 Step 5.1 closely matches directive-schema.md Section 4. Minor syntax differences exist (see Section 4.3 below) but the structural model is the same.

**S3: directive_pins and agent_preferences tables missing.**
Status: **RESOLVED.** Step 3.1 creates both tables via goose migration 004 with full DDL including indexes and constraints. The injection pipeline queries them during retrieval.

**S4: No multi-tenancy.**
Status: **DEFERRED (explicitly).** build-plan-v4 includes a "What This Plan Does NOT Include" section that lists multi-tenancy as item 1 with rationale: "adding `tenant_id` to every table and every query is invasive and not needed for the initial single-tenant deployment." The issue resolution table marks it as deferred. This is a reasonable decision for v1.

**S5: No migration tool.**
Status: **RESOLVED.** Step 0.2 adds goose (`github.com/pressly/goose/v3`). All schema changes are goose migrations in a `migrations/` directory. The inline `CREATE TABLE IF NOT EXISTS` blocks are removed from store.go. A `--migrate-only` CLI flag is added.

### Advisory Issues

**A1: Meilisearch 10-word query limit.**
Status: **RESOLVED.** Step 2.1 includes "Query preprocessing: Truncate query to 10 meaningful terms (Meilisearch word limit). Extract key terms by removing stop words, then taking the 10 highest-signal words." A test (`TestQueryPreprocessing`) is specified.

**A2: Error messages for LLM agents.**
Status: **DEFERRED (explicitly).** Listed in "What This Plan Does NOT Include" item 4. Reasonable -- can be incrementally improved.

**A3: `repo` column missing from existing tables.**
Status: **DEFERRED (explicitly).** Listed in "What This Plan Does NOT Include" item 5.

**A4: OpenAI embedding dependency.**
Status: **DEFERRED (explicitly).** Listed in "What This Plan Does NOT Include" item 2 with rationale: "hybrid search is not needed until the directive count is large enough for keyword search to be insufficient."

**A5: Phase 6 ordering (empty pipeline during development).**
Status: **RESOLVED via seed directives.** The issue resolution table states: "Step 0.5 provides seed directives so the pipeline has content from day one. Phase 6 order is acceptable because seeds provide cold-start coverage." This is the correct approach -- seeds provide functional data for development and testing of Phases 1-5, decomposition comes last.

**A6: No total duration estimate.**
Status: **RESOLVED.** The build plan states: "Critical path (Phase 0-3) estimated at 4-6 weeks. Full plan (Phase 0-7) estimated at 10-14 weeks." The dependency graph section includes a critical path analysis.

**Scorecard: 13/13 addressed.** 9 resolved, 4 explicitly deferred with rationale.

---

## 3. LLM Recomposition Integration

### 3.1 LLM Client Placement in Build Order: CORRECT

The LLM client (Step 0.4) is in Phase 0, which is where it needs to be. Its dependencies are only Step 0.1 (project layout). Its consumers are:

- Step 3.2 (LLMRecomposer) -- needs the client for Haiku-class calls
- Step 6.1 (Decomposition Engine) -- needs the client for Sonnet-class calls

The dependency graph shows `0.4` feeding into both `3.2` and `6.1`. This is correctly modeled. Step 0.4 is parallelizable with Steps 0.2 and 0.3 after Step 0.1 completes.

### 3.2 Recomposer Interface Design: SOUND

The `Recomposer` interface is minimal and correct:

```go
type Recomposer interface {
    Recompose(ctx context.Context, input *RecompositionInput) (*RecompositionOutput, error)
}
```

Two implementations:

- `LLMRecomposer` -- calls Haiku-class LLM, parses JSON output, enforces token budget
- `FallbackRecomposer` -- returns raw directive content verbatim

This is a clean strategy pattern. The Pipeline holds a `Recomposer` and does not know which implementation it is using. The fallback chain is:

1. Try `LLMRecomposer`
2. If LLM fails/times out, Pipeline falls back to `FallbackRecomposer`
3. If even `FallbackRecomposer` fails (unlikely -- it does no I/O), propagate error

The `RecompositionInput` struct includes all necessary context: directives, activity, project info, context summary, recent files, error context, intent, memories, and output token budget. The `RecompositionOutput` includes `Fallback bool` so the caller knows which path was taken.

**One observation:** The `LLMRecomposer` struct in recomposition-design.md Section 10 includes a `cache *ResponseCache` and `preSynth *PreSynthCache` directly on the struct. build-plan-v4 separates caching into Step 3.3 as a distinct layer. This is fine -- the caching can be integrated into the LLMRecomposer after Step 3.2 completes. The interface does not change.

### 3.3 Caching Strategy: WELL-DESIGNED

Three layers with clear purpose:

| Layer             | Key                                                             | TTL    | Hit Scenario                                      | Expected Rate |
| ----------------- | --------------------------------------------------------------- | ------ | ------------------------------------------------- | ------------- |
| 1: Response cache | SHA-256(directive_ids + activity + project + summary_first_100) | 5 min  | Same context, rapid-fire requests                 | 20-30%        |
| 2: Session cache  | session_id + directive_ids + activity                           | 10 min | Same session, same directives, activity unchanged | 30-40%        |
| 3: Pre-synthesis  | directive_id + language + activity                              | 1 hour | Universal directives, common contexts             | Background    |

The combined strategy is sound: L1 catches exact repeats, L2 catches session-level redundancy, L3 provides warm fallbacks. The cache key design avoids collisions while allowing sufficient fuzzy matching.

**One concern:** The Layer 1 key uses `context_summary_first_100_chars`. This is a pragmatic truncation, but summaries that differ only after character 100 will produce false cache hits. In practice this is unlikely to be harmful -- if the first 100 characters of a context summary are identical and the same directives were selected, the recomposition output will be nearly identical. Acceptable.

### 3.4 Fallback Chain: COMPLETE

The fallback hierarchy as implemented across build-plan-v4 Steps 3.2 and 3.3:

1. **Layer 1-3 cache hit** -- cached response, no LLM call (~5ms)
2. **LLM call succeeds** -- full contextual micro-prompts (~3.0-3.5s)
3. **LLM call fails** -- FallbackRecomposer: raw directive content, `action` mapped from type (guardrail->"rule", others->"suggest"), `Fallback: true` (~1ms)
4. **No directives found** -- empty response with `candidates_considered: 0`
5. **No LLM configured** (NoopClient) -- FallbackRecomposer always used

This covers all failure modes: LLM provider down, LLM timeout, LLM returns invalid JSON, LLM returns over-budget output, no API key configured, no directives in database. Each mode produces a usable (if degraded) response.

### 3.5 Latency Budget: REALISTIC

build-plan-v4 Step 3.2 provides this latency table:

| Phase                        | Duration             |
| ---------------------------- | -------------------- |
| Request parsing + validation | 1ms                  |
| Fan-out retrieval (parallel) | 50-200ms             |
| Ranking + selection          | 5ms                  |
| LLM prompt assembly          | 2ms                  |
| LLM TTFT (cached prompt)     | 300-400ms            |
| LLM generation (~250 tokens) | 2,500-3,000ms        |
| Parse + validate output      | 1ms                  |
| **Total**                    | **~3.0-3.5 seconds** |

This matches recomposition-design.md Section 5 exactly. The numbers are grounded in published Haiku 4.5 benchmarks (~94 tokens/sec generation). The TTFT assumes prompt caching, which is reasonable since the system prompt (500 tokens) will be cached after the first call.

**Comparison to the old 400ms budget:** The document is transparent that 3.0-3.5 seconds is ~7x the original target, and provides clear justification: (1) accuracy over speed is the stated requirement, (2) injection is non-blocking (the agent does not wait for it), (3) with caching the p50 is ~200ms.

---

## 4. Technical Accuracy

### 4.1 CockroachDB SQL: VALID

I checked the SQL in directive-schema.md Section 2 and build-plan-v4 against CockroachDB's documented capabilities:

- `CREATE TYPE ... AS ENUM`: Supported since CockroachDB v20.2. Valid.
- `INVERTED INDEX idx_directives_triggers (context_triggers)`: Valid CockroachDB syntax for JSONB inverted indexes.
- `gen_random_uuid()` as DEFAULT: Supported. Valid.
- `TIMESTAMPTZ NOT NULL DEFAULT now()`: Valid.
- `CREATE OR REPLACE FUNCTION ... LANGUAGE SQL`: Supported (basic SQL UDFs). Valid.
- `?|` operator for JSONB: Supported. Valid.
- `@>` operator for JSONB containment: Supported. Valid.
- `REFERENCES ... ON DELETE CASCADE`: Supported. Valid.
- Inline `INDEX` within `CREATE TABLE`: CockroachDB-specific syntax (not standard PostgreSQL), but valid and documented.
- `FLOAT8`: Alias for `DOUBLE PRECISION`. Valid.
- `INT4`: Alias for `INT`. Valid.

**One note:** The `match_directives` function uses `to_jsonb(p_activity_type)` to produce a JSON string, then checks containment via `@>`. This works correctly when `context_triggers->'activity_types'` is a JSON array of strings. The example data in directive-schema.md confirms this is the intended shape (`"activity_types": ["brainstorming", "design", "architecture"]`). Valid.

**Transaction retry logic:** Step 0.2 adds `github.com/cockroachdb/cockroach-go/v2/crdb` for transaction retry. The plan uses `database/sql` (not standalone pgx), so it correctly specifies `crdb` (not `crdbpgx`). This matches the current codebase which uses `database/sql` via `pgx/v5/stdlib`.

### 4.2 Meilisearch Configuration: VALID

build-plan-v4 Step 2.1 derives from directive-schema.md Section 3:

- `searchableAttributes`, `filterableAttributes`, `sortableAttributes`: All valid Meilisearch settings.
- Custom ranking rules `effectiveness_score:desc` and `priority:desc` after standard rules: Valid syntax.
- `typoTolerance.disableOnAttributes` for enum fields: Valid and good practice.
- `synonyms` map: Valid structure.
- Query preprocessing to 10 terms: Correctly addresses the Meilisearch word limit.

**The embedder configuration** (OpenAI) is correctly deferred -- build-plan-v4 explicitly excludes hybrid search/embeddings from scope.

### 4.3 Gel SDL: MOSTLY VALID, MINOR SYNTAX DIFFERENCES

Comparing build-plan-v4 Step 5.1 SDL against directive-schema.md Section 4 SDL:

**build-plan-v4 uses:**

```sdl
scalar type DirectiveType extending enum<behavioral, procedural, contextual, guardrail>;
```

**directive-schema.md uses:**

```sdl
scalar type DirectiveType extending enum<
    'behavioral',
    'procedural',
    'contextual',
    'guardrail'
>;
```

The build-plan-v4 version omits the single quotes around enum values. In Gel SDL, enum values are quoted strings. The unquoted form may work in some Gel versions as bare identifiers, but the directive-schema.md form with quotes is more correct per Gel documentation. Similarly, directive-schema.md's `WorkflowStage` enum uses values like `'pre_implementation'` while build-plan-v4 uses `starting`, `brainstorming`, `planning`, `implementing`, `debugging`, `reviewing`, `testing`, `refactoring`, `deploying` -- a completely different set of values.

**Impact:** The build-plan-v4 Step 5.1 SDL should be treated as illustrative. The implementor should use directive-schema.md Section 4 as the authoritative SDL source, consistent with the plan's own authoritative schema declaration.

**Other SDL checks:**

- `multi chains_to: Directive { property strength: float64; property description: str; }` -- link properties are valid Gel SDL.
- `property incoming_chain_count := count(.<chains_to[IS Directive]);` -- backlink computed properties are valid EdgeQL.
- `constraint min_value(1); constraint max_value(100);` -- valid Gel constraints.
- `index on (.directive_type);` -- valid Gel index syntax.

### 4.4 pgx/crdbpgx Usage: CORRECT BUT NOTE INTERFACE CHOICE

The current codebase uses `database/sql` with pgx as the driver (`pgx/v5/stdlib`). build-plan-v4 Step 0.2 adds `cockroach-go/v2/crdb` (the `database/sql` variant of the retry wrapper, not `crdbpgx`).

cockroachdb.md Section 4 recommends pgx in standalone mode (not `database/sql`) for best performance. build-plan-v4 does not migrate to standalone pgx -- it keeps `database/sql`. This is pragmatic (avoids rewriting all store code) but means the project misses pgx's performance advantages. This is an acceptable tradeoff for Phase 0 -- a migration to standalone pgx could be a future hardening step.

### 4.5 Haiku 4.5 Cost Estimates: ACCURATE

recomposition-design.md Section 3 states:

- Input: $1.00 / 1M tokens
- Output: $5.00 / 1M tokens

Verified against current Anthropic pricing. These are correct for Claude Haiku 4.5 (model ID `claude-haiku-4-5-20251015`).

Per-request cost calculation:

- 2,000 input tokens \* $1.00/1M = $0.002
- 500 output tokens \* $5.00/1M = $0.0025
- Total: $0.0045

This is correctly computed. The scaling table (100-10,000 injections/day) follows linearly. The 30-50% caching reduction estimate is reasonable given the cache hit rate projections.

**Prompt caching discount:** The document claims cached input at $0.10/1M tokens (90% discount). Anthropic's prompt caching pricing is $0.10/1M for cache reads and $1.25/1M for cache writes. The document's effective cost estimate of ~$0.003/request with caching is approximately correct.

---

## 5. Build Plan Executability

### 5.1 Can Each Step Be Handed to an LLM Agent?

Each step includes:

1. **Step ID** and dependency chain
2. **"What gets built"** with numbered items and concrete file paths
3. **Go interface/struct definitions** as code blocks
4. **SQL DDL** as code blocks
5. **Configuration** via named environment variables
6. **Test list** with named test functions and descriptions
7. **Acceptance criteria** as a bullet list of verifiable conditions

This format is excellent for LLM agent prompts. The Go code blocks are directly implementable. The SQL blocks are directly executable. The test names provide clear specification. An agent given Step 1.1 as a prompt could produce a working implementation without needing to read any other document.

**Potential friction points:**

- **Step 0.1 (project layout refactor)**: Requires touching many files (Dockerfile, .goreleaser.yaml, CI workflows). The step lists what to update but does not show the exact diffs. An agent will need to read each file and make targeted edits. This is fine -- the step is clear about what changes.

- **Step 0.5 (seed directives)**: Requires curating 30-50 directives from the skill-replacement-analysis. The step provides 7 examples but says "plus ~25 additional." An agent will need to read the skill documents and extract directives, which is itself a decomposition task. Consider providing the full seed set as part of the step specification, or accept that the agent will produce a reasonable initial set that can be refined.

- **Step 3.2 (LLM recomposer)**: The most complex step. Requires implementing the LLM call, JSON parsing, token budget enforcement, fallback chain, and memory integration. The recomposition-design.md system prompt is provided verbatim as a Go constant. The structured output format is defined. An agent can implement this, but should budget for iteration -- the LLM output parsing and validation will need testing against actual Haiku responses.

- **Step 6.1 (decomposition engine)**: Also complex. The decomposition prompt from vision-v4 Section 3.3 uses different field names than directive-schema.md (e.g., `kind` instead of `directive_type`, `trigger_tags` instead of `context_triggers`). The implementor must translate the vision-v4 prompt into directive-schema.md field names. This is a known mismatch that the authoritative schema declaration addresses, but the decomposition prompt should be rewritten to use the correct field names.

### 5.2 Are Acceptance Criteria Testable?

Every step's acceptance criteria can be verified by automated tests or concrete observations:

- "All timestamp columns are TIMESTAMPTZ" -- verifiable by schema inspection
- "No manual time.Parse or time.Format calls" -- verifiable by grep
- "Schema matches directive-schema.md Section 2 exactly" -- verifiable by DDL comparison
- "LLMRecomposer calls Haiku-class LLM" -- verifiable by mock test
- "Guardrail directives are never trimmed" -- verifiable by test with budget < total
- "Query preprocessing truncates to 10 meaningful terms" -- verifiable by unit test

No acceptance criterion is vague or subjective. This is a well-specified plan.

### 5.3 Are Scope Estimates Realistic?

| Step | Scope        | Assessment                                                                                                                                               |
| ---- | ------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0.1  | M (1-3 days) | Realistic. File moves + import updates + CI fixes. Tedious but straightforward.                                                                          |
| 0.2  | M (1-3 days) | Realistic. goose integration + timestamp migration. The timestamp changes touch many files.                                                              |
| 0.3  | S (< 1 day)  | Realistic. Extract interfaces, add compile-time checks. Mechanical refactor.                                                                             |
| 0.4  | M (1-3 days) | Realistic. HTTP client for Anthropic API. Well-specified request/response format.                                                                        |
| 0.5  | S (< 1 day)  | **Underestimated.** Curating 30-50 high-quality directives from skill documents is intellectually demanding. 1-2 days is more realistic.                 |
| 1.1  | M (1-3 days) | Realistic. Schema + CRUD store methods. Standard work.                                                                                                   |
| 1.2  | M (1-3 days) | Realistic. Huma handlers wrapping store methods. Standard work.                                                                                          |
| 2.1  | M (1-3 days) | Realistic. Meilisearch client with the official Go SDK.                                                                                                  |
| 2.2  | M (1-3 days) | Realistic. Sync logic with background reconciliation.                                                                                                    |
| 3.1  | L (3-5 days) | Realistic. Fan-out retrieval + ranking + new tables + handler. The most complex retrieval logic.                                                         |
| 3.2  | L (3-5 days) | Realistic. LLM recomposer + fallback + prompt engineering + output parsing.                                                                              |
| 3.3  | M (1-3 days) | Realistic. Three cache layers + background worker.                                                                                                       |
| 4.1  | M (1-3 days) | Realistic. Feedback recording + effectiveness scoring.                                                                                                   |
| 5.1  | M (1-3 days) | **May be underestimated.** Gel DB has a learning curve for developers unfamiliar with EdgeQL. The gel-go client is less mature than pgx. Allow 3-5 days. |
| 5.2  | M (1-3 days) | Realistic if 5.1 is done. The graph queries are specified.                                                                                               |
| 6.1  | L (3-5 days) | Realistic. The decomposition prompt will need iteration.                                                                                                 |
| 7.1  | M (1-3 days) | Realistic. slog + Prometheus + rate limiting. Standard ops work.                                                                                         |
| 7.2  | S (< 1 day)  | Realistic. Admin endpoints + script.                                                                                                                     |

**Total critical path (0.1 -> 0.2 -> 1.1 -> 2.1 -> 3.1 -> 3.2):** 12-20 step-days, or ~4-6 weeks with context switches and testing. The plan's 4-6 week estimate is realistic.

**Total full plan:** 28-50 step-days, or ~10-14 weeks. The plan's 10-14 week estimate is realistic.

---

## 6. Missing Pieces

### 6.1 Covered but Worth Highlighting

These are addressed in the plan but may need extra attention:

1. **Decomposition prompt field names.** vision-v4 Section 3.3's decomposition prompt uses `kind`, `trigger_tags`, `trigger_intent`, `trigger_phase` -- all superseded field names. Step 6.1 must rewrite the prompt to use `directive_type`, `context_triggers`, and the structured trigger schema from directive-schema.md. The plan acknowledges this implicitly (Step 6.1 says "Requests JSON array of directives with specific fields matching directive-schema.md") but the prompt itself will need to be authored fresh.

2. **CRDB-to-Gel sync.** Step 5.1 defines `SyncDirective` and `SyncRelationship` methods but does not specify a sync manager or reconciliation loop for Gel (analogous to Step 2.2 for Meilisearch). The `search.SyncManager` pattern from Step 2.2 should be replicated for Gel. This is implied but not explicit.

3. **Experience-derived directives.** vision-v4 Section 6.3 describes creating new directives from session completion summaries. build-plan-v4 Step 4.1 records session completions but does not implement the LLM-based directive creation from session insights. This is a Phase 6+ feature (requires the decomposition LLM). The plan does not explicitly defer this -- it just does not include it. It should be listed in "What This Plan Does NOT Include."

### 6.2 Not Covered (Should Be)

1. **Graceful shutdown for background workers.** Steps 2.2 (reconciliation loop), 3.3 (cache cleanup, pre-synthesis worker), and 7.1 (metrics) all introduce background goroutines. The plan does not specify how these are shut down gracefully when the server receives SIGTERM. The current codebase has signal handling in cmd/app/main.go. The new `internal/server/` package (Step 0.1) should manage context cancellation for all background workers. This is a small addition but important for clean production behavior.

2. **Integration test infrastructure.** Steps 2.1 and 5.1 specify integration tests tagged with `//go:build integration`. The plan does not specify how CI runs these (they require running Meilisearch and Gel instances). A `docker-compose.yaml` or CI configuration for integration test dependencies should be mentioned. This does not need its own step -- it can be folded into Steps 2.1 and 5.1.

3. **Huma operation metadata.** The existing codebase uses `huma.Register` with `huma.Operation` structs for OpenAPI documentation. The plan specifies new handlers but does not detail the Huma operation metadata (operation ID, summary, description, tags, request/response types). This is minor -- the implementor will follow the existing pattern in the codebase.

---

## 7. The Honest Verdict

### 7.1 Is This Plan Ready to Execute?

**Yes.** The architecture is sound, the technical choices are validated, the schemas are consistent, every previous review issue is tracked, and the steps are concrete enough to hand to an LLM agent. The plan has been through four iterations of increasing rigor. There is no structural flaw remaining.

### 7.2 The Single Biggest Risk: LLM Recomposition Quality

The plan bets heavily on LLM recomposition. If Haiku 4.5 cannot reliably produce well-structured, contextual micro-prompts from the system prompt in recomposition-design.md Section 4, the entire injection pipeline degrades to the FallbackRecomposer (raw directive content).

This risk is mitigated by:

- The FallbackRecomposer exists and works -- the system is usable without LLM recomposition
- The system prompt is specific and well-designed with explicit rules and structured output
- The recomposition task is synthesis, not reasoning -- well within Haiku's capability
- The caching layers reduce the number of LLM calls needed
- Feedback data will reveal if LLM-contextualized snippets outperform raw directives

But the risk is real. The first time an LLM agent implements Step 3.2 and tests it against real Haiku responses, the system prompt may need tuning. The JSON parsing may hit edge cases (Haiku occasionally produces markdown-wrapped JSON, or adds commentary outside the JSON structure). Budget time for iteration on Step 3.2.

### 7.3 The Second Biggest Risk: Operational Complexity

The full deployment is: hive-server + CockroachDB + Meilisearch + (optionally) Gel DB + Anthropic API access. For a single developer's workflow tool, this is substantial. The plan correctly defers Gel to Phase 5 and makes Meilisearch optional (NoopSearcher), so the minimum deployment is hive-server + CockroachDB + Anthropic API key. That is manageable.

### 7.4 What I Would Start Building First

Phase 0 (Steps 0.1 through 0.5) in dependency order:

1. Step 0.1 (project layout) -- unblocks everything
2. Steps 0.2, 0.3, 0.4 in parallel -- foundation work
3. Step 0.5 (seed directives) -- gives the pipeline something to work with

Then Phase 1 (Steps 1.1, 1.2) and Phase 2 (Steps 2.1, 2.2). Get directive CRUD and search working with the seed set. Then Step 3.1 (retrieval + ranking) to prove the fan-out pipeline works. Step 3.2 (LLM recomposer) is the moment of truth -- the first real validation of the architecture's core value proposition.

---

## 8. Issue Tracker

### Moderate (fix during implementation of the affected step)

| #   | Issue                                                                                                                                                    | Location                                             | Resolution                                                                                                                                          |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| M1  | `decomposition_run_id` is NOT NULL in directive-schema.md but nullable (\*string) in build-plan-v4 Go struct. Seed directives have no decomposition run. | directive-schema.md S2 vs build-plan-v4 Step 1.1/0.5 | Either make the column nullable in the schema, or create a well-known "seed" decomposition run during migration 002 that seed directives reference. |
| M2  | recomposition-design.md Section 7 references a `static_content` fallback field that does not exist in any schema. Vestigial from the template approach.  | recomposition-design.md S7                           | Remove or annotate "Fallback Tier 3: Static Content Field" as vestigial. Build plan correctly ignores it.                                           |

### Minor (non-blocking, fix when convenient)

| #   | Issue                                                                                                                           | Location                             | Resolution                                                                                                      |
| --- | ------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------ | --------------------------------------------------------------------------------------------------------------- |
| m1  | hive-server-current.md lists `modernc.org/sqlite` as a dependency. The actual go.mod shows `pgx/v5` and `huma/v2` instead.      | hive-server-current.md S2            | Update hive-server-current.md to reflect the actual go.mod (huma v2, pgx v5, no sqlite).                        |
| m2  | hive-server-current.md lists k8s/ directory. It no longer exists in the actual codebase.                                        | hive-server-current.md S1            | Remove k8s/ from hive-server-current.md project structure. (build-plan-v4 correctly notes "No k8s/ directory".) |
| m3  | build-plan-v4 Step 5.1 Gel SDL uses unquoted enum values and different WorkflowStage values than directive-schema.md Section 4. | build-plan-v4 Step 5.1               | Use directive-schema.md Section 4 SDL verbatim during implementation. The build plan's version is illustrative. |
| m4  | vision-v4 Section 3.3 decomposition prompt uses superseded field names (kind, trigger_tags, etc.).                              | vision-v4 S3.3                       | When implementing Step 6.1, rewrite the prompt to use directive-schema.md field names.                          |
| m5  | Experience-derived directives (vision-v4 S6.3) are not in scope but not listed in "What This Plan Does NOT Include."            | build-plan-v4 "Not Included" section | Add to the out-of-scope list for completeness.                                                                  |
| m6  | Graceful shutdown for background workers (sync loops, cache cleanup, pre-synthesis) is not specified.                           | build-plan-v4 Steps 2.2, 3.3, 7.1    | Ensure internal/server/ manages context cancellation for all goroutines during Step 0.1.                        |

### Advisory

| #   | Issue                                                                                                | Location                     | Resolution                                                                            |
| --- | ---------------------------------------------------------------------------------------------------- | ---------------------------- | ------------------------------------------------------------------------------------- |
| a1  | Integration test infrastructure (docker-compose for Meilisearch/Gel CI) not specified.               | build-plan-v4 Steps 2.1, 5.1 | Add a docker-compose.yaml for integration test dependencies. Fold into Steps 2.1/5.1. |
| a2  | Step 0.5 scope estimate (S, < 1 day) is likely underestimated for curating 30-50 quality directives. | build-plan-v4 Step 0.5       | Budget 1-2 days instead of < 1 day.                                                   |
| a3  | CRDB-to-Gel sync reconciliation loop is not specified (unlike the Meilisearch sync in Step 2.2).     | build-plan-v4 Step 5.1       | Add a Gel sync manager analogous to the Meilisearch SyncManager.                      |

---

## 9. Conclusion

Build plan v4 is a well-crafted, technically accurate, internally consistent plan that addresses all 13 issues from the previous review. The LLM recomposition design is the boldest change -- replacing templates with Haiku-class synthesis -- and it is well-justified by the concrete examples that demonstrate the quality gap.

The remaining issues are implementation-level details (nullable column for seeds, vestigial static_content reference, enum syntax normalization), not architectural problems. None require rethinking any aspect of the design.

The plan is ready to build. Start Phase 0.
