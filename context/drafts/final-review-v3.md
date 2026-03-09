# Final Review v3: Quality Gate for Build Plan v3

**Date:** 2026-03-09
**Status:** Final quality gate review of v4 architecture before implementation
**Documents reviewed:** vision-v4.md, directive-schema.md, injection-pipeline.md, build-plan-v3.md, skill-replacement-analysis.md, final-review-v2.md, ultrathink-architect.md, ultrathink-skeptic.md, ultrathink-devex.md, ultrathink-ops.md, cockroachdb.md, meilisearch.md, gel-db.md, hive-server-current.md
**Method:** Full cross-document analysis with schema comparison, technology reference validation, and previous-review issue tracking

---

## 0. Executive Summary

The v4 documents represent a genuine paradigm shift. The central contradiction identified in final-review-v2 -- vision says "replace skills" while analysis says "skills are irreplaceable" -- has been resolved. Vision-v4 reframes hive-server as a behavioral knowledge engine that decomposes skill prompts into atomic directives and injects them contextually. This is a coherent, intellectually honest architecture that aligns with the skill-replacement analysis rather than contradicting it.

The documents are well-crafted, technically detailed, and internally consistent at the conceptual level. However, there are seven material issues that need resolution before implementation can begin. Two are critical (schema conflicts between documents, unspecified LLM dependency), three are significant (cold start problem, Gel DB schema drift from directive-schema.md, injection-pipeline.md schema diverges from directive-schema.md), and two are advisory (endpoint count ambiguity, timeline realism).

**Verdict: Conditionally ready to build.** The architecture is sound. The critical issues are reconcilable -- they require choosing one schema as authoritative and specifying the LLM dependency, not rethinking the design. Fix these before starting Phase 1.

---

## 1. Internal Consistency

### 1.1 The Central Concept: Consistent

All four primary documents agree on the core model:

- Skill prompts are decomposed into atomic behavioral directives (vision-v4 Section 2, directive-schema Section 1, build-plan-v3 Phase 6)
- Directives are stored across CockroachDB (transactional source of truth), Meilisearch (semantic search), and Gel DB (relationship graph)
- The injection pipeline (`POST /api/v1/inject`) fans out to all three databases, ranks candidates, and returns directives within a token budget
- A feedback loop (`POST /api/v1/feedback`) tracks outcomes and updates effectiveness scores

This model is consistent across all documents. No document contradicts it.

### 1.2 Schema Conflicts: CRITICAL

Three documents define the directive schema, and they disagree on field names, types, and structure. This is the most serious consistency issue.

**Directive type taxonomy:**

| Field               | vision-v4.md (Section 2.2)                                            | directive-schema.md (Section 1)                       | injection-pipeline.md (Section 4.2)                | build-plan-v3 (Step 1.1)                          |
| ------------------- | --------------------------------------------------------------------- | ----------------------------------------------------- | -------------------------------------------------- | ------------------------------------------------- |
| Type enum name      | `kind`                                                                | `directive_type`                                      | `category`                                         | `DirectiveType`                                   |
| Type values         | behavioral, pattern, contextual, corrective, factual                  | behavioral, procedural, contextual, guardrail         | guardrail, contextual, behavioral, procedural      | behavioral, procedural, contextual, guardrail     |
| Priority field      | `weight` (float 0-2.0)                                                | `priority` (int 1-100)                                | `priority` (int, used in scoring)                  | `Priority` (int)                                  |
| Effectiveness field | `effectiveness` (float)                                               | `effectiveness_score` (float)                         | `feedback` (float in scoring formula)              | `EffectivenessScore` (float64)                    |
| Source field        | `source_type` + `source_id` + `source_name`                           | `source_skill` (enum) + `source_section`              | Not specified at storage level                     | `SourceSkill` (string) + `SourceSection` (string) |
| Trigger mechanism   | `trigger_tags` + `trigger_intent` + `trigger_phase` + `trigger_scope` | `context_triggers` (JSONB with structured sub-fields) | `activity_tags` + `project_tags` + `language_tags` | `ContextTriggers` (map[string]any)                |
| Active flag         | `active` (bool)                                                       | `is_active` (bool)                                    | `enabled` (bool)                                   | `IsActive` (bool)                                 |

This table reveals fundamental disagreements:

1. **vision-v4 uses `kind` with values {behavioral, pattern, contextual, corrective, factual}. directive-schema.md uses `directive_type` with values {behavioral, procedural, contextual, guardrail}. injection-pipeline.md uses `category` with yet another set.** These are three different taxonomies. The injection pipeline's scoring formula (Section 3.2) assigns specific weights to {guardrail, contextual, behavioral, procedural} -- these must match whatever the storage schema uses.

2. **vision-v4 defines `weight` as a 0.0-2.0 float alongside `effectiveness`. directive-schema.md defines `priority` as an integer 1-100. injection-pipeline.md uses `priority` in its scoring formula as a 0.0-1.0 normalized value.** The scoring formula in injection-pipeline.md (Section 3.1) says `priority * 0.25` where priority is described as "Normalized from the directive's category priority (guardrail=1.0, contextual=0.75, behavioral=0.5, procedural=0.25)." This is a category-based normalization that has nothing to do with vision-v4's per-directive `weight` field or directive-schema.md's 1-100 integer `priority`.

3. **The trigger mechanism is completely different.** vision-v4 uses four separate TEXT/JSONB fields (`trigger_tags`, `trigger_intent`, `trigger_phase`, `trigger_scope`). directive-schema.md uses a single JSONB field `context_triggers` with a structured sub-schema containing `activity_types`, `keywords`, `codebase_patterns`, `conversation_topics`, `workflow_stages`, `agent_states`, `project_signals`, `complexity_threshold`, `prerequisite_directives`. injection-pipeline.md uses flat filterable fields `activity_tags`, `project_tags`, `language_tags` in Meilisearch. These are three different trigger models.

**Recommendation:** directive-schema.md should be declared authoritative for the CockroachDB schema, since it is the most detailed and includes the full DDL with indexes, views, and functions. vision-v4.md's schema should be treated as conceptual/illustrative. injection-pipeline.md's Meilisearch schema should be updated to derive from directive-schema.md's CRDB schema via denormalization. build-plan-v3 should be updated to match directive-schema.md exactly.

### 1.3 Endpoint Counts

The review context states "The core API is ~9-28 endpoints." Let me count what the documents actually specify:

**Existing endpoints (12):**

- Memory: POST, GET (list), GET (by key), DELETE = 4
- Tasks: POST, GET (list), GET (by id), PATCH, DELETE = 5
- Agents: POST (heartbeat), GET (list), GET (by id) = 3

**New endpoints from build-plan-v3:**

- Directives CRUD: POST, GET (list), GET (by id), PATCH, DELETE = 5
- Ingest: POST /api/v1/ingest = 1
- Inject: POST /api/v1/inject = 1
- Feedback: POST /api/v1/feedback = 1
- Session complete: POST /api/v1/feedback/session-complete = 1
- Admin sync: POST /api/v1/admin/sync = 1
- Health/Ready: GET /health, GET /ready = 2 (existing)

**Total: 12 existing + 10 new = 22 endpoints.** This is within the stated range. The endpoint count is reasonable and each endpoint has a clear purpose. This is a massive improvement over v3's 66 endpoints.

### 1.4 Build Plan Alignment with Vision

build-plan-v3's phases implement what vision-v4 describes:

- Phase 0 (Foundation): Project layout + CockroachDB hardening. Addresses the architect's Store interface concerns and the current codebase state.
- Phase 1 (Directive Storage): Creates the CRDB directive schema and CRUD. Implements the storage layer described in vision-v4 Section 5.1.
- Phase 2 (Meilisearch): Adds semantic search. Implements vision-v4 Section 5.2.
- Phase 3 (Injection Pipeline): Implements the fan-out pipeline described in vision-v4 Section 4 and injection-pipeline.md.
- Phase 4 (Feedback Loop): Implements vision-v4 Section 6.
- Phase 5 (Gel DB): Implements vision-v4 Section 5.3.
- Phase 6 (Decomposition): Implements vision-v4 Section 3.

The ordering is logical: storage before search, search before injection, injection before feedback, decomposition last (because directives can be manually created before the LLM pipeline exists).

**One gap:** build-plan-v3 Step 5.1 defines a Gel schema that matches vision-v4's Gel schema (Section 5.3) but does NOT match directive-schema.md's Gel schema (Section 4). The two Gel schemas are structurally different -- see Section 7 below.

---

## 2. Previous Issues from final-review-v2

final-review-v2 identified 6 required changes. Here is their status:

### 2.1 "Vision vs. Skill Replacement Analysis: The Central Contradiction" (CRITICAL)

**Status: RESOLVED.**

final-review-v2 stated: "Vision-v3 says 'replace skills entirely' while the skill-replacement-analysis says 'skills are 40-85% prompt engineering that no server can replace.'"

vision-v4 explicitly resolves this. Section 0 states: "v4 recognizes what hive-server actually is: A prompt decomposition and contextual injection engine." The vision no longer claims to replace skills. It claims to decompose their behavioral knowledge into atomic directives and inject them contextually. The skill-replacement-analysis remains accurate -- the 60-85% that is prompt engineering is exactly what the decomposition pipeline extracts into directives. The skills themselves become optional once their knowledge is in the directive database.

This is a genuine resolution, not a reframing. The architecture actually does something different from v3.

### 2.2 "66 Endpoints" (CRITICAL)

**Status: RESOLVED.**

v4 reduces to ~22 endpoints (see Section 1.3 above). Each endpoint has a defined purpose. No speculative skill-specific API namespaces exist.

### 2.3 "Meilisearch/CockroachDB/Gel Order of Operations"

**Status: RESOLVED.**

build-plan-v3 correctly starts with CockroachDB (already running in production), adds Meilisearch in Phase 2, and adds Gel in Phase 5. All three databases are treated as active components, not speculative additions. The plan acknowledges the current codebase already uses pgx and partially uses CockroachDB idioms.

### 2.4 "Store Interface Monolith"

**Status: RESOLVED.**

build-plan-v3 Step 0.3 splits the monolithic Store interface into composed sub-interfaces: `MemoryStore`, `TaskStore`, `AgentStore`, `DirectiveStore`, `FeedbackStore`. This directly addresses the ultrathink-architect's Section 1 concern. The composed `Store` interface embeds all sub-interfaces for backward compatibility.

### 2.5 "Timeline Realism"

**Status: PARTIALLY RESOLVED.**

build-plan-v3 assigns size estimates (S/M/L) to each step but does not provide calendar dates or total elapsed time estimates. This is an improvement over v2's specific week ranges that the skeptic criticized. However, the plan still has 14 steps across 7 phases. A solo developer with LLM assistance will not complete all 7 phases quickly. The skeptic's timeline estimate of "3 months to anything useful" still applies -- but Phase 3 (injection pipeline) is the first point of real value delivery, and it can be reached in roughly 3-4 weeks of focused work (Phases 0-3).

### 2.6 "Error Messages for LLM Agents"

**Status: NOT ADDRESSED.**

final-review-v2 did not call this out as a required change, but the ultrathink-devex document (Section 4) provided detailed recommendations for LLM-friendly error messages with `recovery` guidance. None of the v4 documents mention error message improvements. The build plan does not include a step for improving error messages.

**Recommendation:** Add error message improvements to Phase 0 or Phase 1. The investment is small (~15 error sites) and the payoff for LLM agent usability is significant.

---

## 3. Ultra-Think Concerns

### 3.1 Architect's Hard Problems

The ultrathink-architect (Section 1) identified the Store interface monolith as the primary structural problem. **Addressed** by build-plan-v3 Step 0.3 (composed interfaces).

The architect also identified schema evolution (Section 2) as a concern -- how do you add columns to the directives table without breaking the API? **Partially addressed** by the use of CockroachDB's online schema changes, but no explicit migration strategy beyond "ALTER TABLE" in Phase 0. A formal migration tool (goose, as recommended in cockroachdb.md Section 5) is not mentioned in the build plan.

**Recommendation:** Add goose (or equivalent) to Phase 0.2 as the migration framework. The current "inline ALTER" approach will not scale past 3-4 schema changes.

### 3.2 Skeptic's Warnings

The ultrathink-skeptic's core objection was: "You are building infrastructure for consumers that do not exist." **Addressed.** The MCP plugin already exists (stated in the review context). The consumer for `POST /api/v1/inject` is the MCP plugin. The consumer for `POST /api/v1/feedback` is the MCP plugin. There is a concrete consumer.

The skeptic's second objection was: "Build the hive CLI tool first." **Not addressed but no longer critical.** The MCP plugin is the integration path, not a CLI tool. The hive CLI from the DevEx document is useful but not blocking.

The skeptic's third objection was: "Meilisearch is not free -- it adds operational complexity." **Acknowledged but accepted.** The v4 architecture genuinely needs search -- the injection pipeline's value depends on finding semantically relevant directives. LIKE queries over 400-700 directives would work at small scale, but the typo-tolerant semantic matching is core to the vision. The graceful degradation to NoopSearcher is correctly designed.

### 3.3 DevEx Priorities

The ultrathink-devex (Section 2) stressed: "Time to first successful API call under 60 seconds. Do not break this." **Not explicitly addressed.** The build plan's Phase 0 refactors cmd/app/ to cmd/hive-server/, which changes the build command. Documentation should be updated simultaneously.

The DevEx document's Section 3 defined a concrete `hive` CLI tool specification with subcommands. **Not in scope for build-plan-v3.** The MCP plugin is the primary consumer. A CLI tool is a nice-to-have.

The DevEx document's Section 9 identified "no repo field on any model" as a critical gap. **Still a gap.** The directive schema (all versions) includes scope/project fields, but the existing memory and task tables still lack a `repo` column. build-plan-v3 Step 0.2 does not add `repo` to existing tables. This should be included.

### 3.4 Ops Concerns

The ultrathink-ops (Section 1) provided a process inventory and resource budget. Gel DB's 1GB RAM minimum was called out. **Acknowledged in build-plan-v3 by deferring Gel to Phase 5.** This means the system runs on CockroachDB + Meilisearch for Phases 0-4, which is operationally simpler.

The ops document's Section 2 identified "Meilisearch is down during injection" as a top failure scenario. **Addressed by the NoopSearcher graceful degradation pattern and the pipeline's fallback-to-CRDB design.** The injection pipeline document (Section 2.4) defines a clear degradation priority: drop Gel first, then CRDB agent preferences, then Meilisearch, never drop pinned directives.

The ops document's Section 3 raised data durability concerns for SQLite. **No longer applicable** since CockroachDB is the primary store.

---

## 4. Technical Accuracy

### 4.1 CockroachDB

The CRDB schema in directive-schema.md is valid CockroachDB SQL. Specific checks:

- `CREATE TYPE ... AS ENUM` is supported in CockroachDB (since v20.2).
- `INVERTED INDEX idx_directives_triggers (context_triggers)` is valid syntax for JSONB inverted indexes.
- `gen_random_uuid()` as a default is supported.
- `TIMESTAMPTZ NOT NULL DEFAULT now()` is correct.
- The `CREATE OR REPLACE FUNCTION ... LANGUAGE SQL` syntax is supported (CockroachDB supports basic SQL UDFs).
- The `?|` operator for JSONB array containment checking is supported.

**One issue:** directive-schema.md Section 2 defines inline `INDEX` within `CREATE TABLE` statements (e.g., `INDEX idx_runs_source (source_skill, source_section)`). CockroachDB supports this syntax for secondary indexes, but it is non-standard PostgreSQL. It works, but some linters may flag it. Not a blocking issue.

**One concern:** The `match_directives` function (directive-schema.md Section 2) uses `to_jsonb(p_activity_type)` which produces `"debugging"` (a JSON string), then checks if the array contains it via `@>`. This works only if `context_triggers->'activity_types'` is a JSON array of strings like `["debugging", "planning"]`. The function assumes this structure. The schema should document the expected JSON shape as a comment or constraint.

### 4.2 Meilisearch

The Meilisearch index configuration in directive-schema.md (Section 3) is valid:

- `searchableAttributes`, `filterableAttributes`, `sortableAttributes` are correctly specified.
- The `rankingRules` array includes custom rules `"effectiveness_score:desc"` and `"priority:desc"` after the standard rules. This is valid Meilisearch syntax for adding custom ranking rules.
- The `synonyms` map is correctly structured.
- The `embedders` configuration references `"source": "openAi"` with `"model": "text-embedding-3-small"`. This is valid Meilisearch hybrid search configuration. **However, this introduces an OpenAI API dependency** (see Section 5 below).
- The `stopWords` list is reasonable.
- `typoTolerance.disableOnAttributes` for enum-like fields is a good practice.

**One issue:** Meilisearch's 10-word query limit applies. The injection-pipeline.md (Section 2.1) addresses this by using `context.summary` as the search input, which the MCP plugin has already summarized. However, the `context.summary` field is defined as "max 500 characters" (~125 tokens, ~50-80 words). After stop word removal, this might still exceed 10 meaningful words. The pipeline should explicitly truncate to the 10 most relevant terms before searching. injection-pipeline.md does not specify this truncation step.

### 4.3 Gel DB

The Gel SDL in directive-schema.md (Section 4) is valid Gel syntax:

- `scalar type extending enum<...>` is correct.
- `multi` links with link properties (`strength: float64`, `description: str`) are supported.
- `property ... := count(.<chains_to[IS Directive])` backlink computed properties are valid EdgeQL.
- `constraint min_value / max_value` on properties is supported.
- `index on (...)` is supported.

**However:** The Gel schema in directive-schema.md defines relationship types as named multi-links on the Directive type (`chains_to`, `conflicts_with`, `alternative_to`, `refines`, `requires`, `equivalent_to`). The vision-v4 Gel schema (Section 5.3) uses simpler links (`related_to`, `superseded_by`, `chain`). The build-plan-v3 Step 5.1 uses the vision-v4 version. **These are three different graph models.** See Section 7 for the full comparison.

### 4.4 Latency Estimates

injection-pipeline.md (Section 2.4) claims a total pipeline deadline of 400ms + 150ms buffer = 550ms worst case. Breakdown:

- Meilisearch: 150ms timeout. The Meilisearch technology brief states "returns results in under 50 milliseconds." 150ms is generous. **Realistic.**
- CockroachDB: 200ms timeout for 4 parallel queries. Each query is an indexed lookup or short scan on a few hundred/thousand rows. Single-digit milliseconds expected. **Realistic.**
- Gel DB: 200ms timeout. Graph traversal on a dataset of hundreds of nodes with 1-2 hop depth. EdgeQL compiles to a single PostgreSQL query. 10-30ms expected. **Realistic.**
- Merge + Rank + Select: 50ms for in-memory operations on 50 candidates. **Realistic.**
- Recompose: 50ms for template substitution. **Realistic.**

**The 400ms total is achievable** for the described workload. The 550ms worst-case includes network latency and timeout buffers. This is a reasonable target for a contextual injection that happens on a per-prompt or per-interval basis.

**Exception:** If the Meilisearch `embedders` configuration is active (hybrid search), each search query requires computing an embedding vector. If using the OpenAI API for embeddings, this adds 100-300ms of external API call latency. This could push Meilisearch past its 150ms timeout. injection-pipeline.md does not account for this. Either disable hybrid search for the injection path, or use a local embedding model, or increase the Meilisearch timeout.

---

## 5. The LLM Dependency

### 5.1 Where LLMs Are Required

Two pipeline stages require LLM access:

1. **Decomposition pipeline** (vision-v4 Section 3.3, build-plan-v3 Phase 6): When a skill document is ingested, an LLM extracts atomic directives from each section. This is the core knowledge-extraction step. Without it, directives must be manually created.

2. **Contextualization step** (vision-v4 Section 4.6): After the injection pipeline selects directives, a "Haiku-class" LLM call contextualizes generic directives to the agent's current situation. This is described as optional with a 50ms timeout.

### 5.2 What Is Specified

build-plan-v3 Step 6.1 specifies:

- An `LLMClient` interface with `Complete(ctx, req) (*CompletionResponse, error)`
- An Anthropic API client implementation using raw HTTP
- Configuration via `ANTHROPIC_API_KEY` and `LLM_MODEL` (default: `claude-sonnet-4-20250514`)
- A `NoopLLMClient` that returns an error when no API key is configured

This is well-specified for the decomposition pipeline.

### 5.3 What Is NOT Specified

**The contextualization LLM call (vision-v4 Section 4.6) is contradicted by injection-pipeline.md.**

vision-v4 Section 4.6 says: "Contextualization is done by a fast LLM call (Haiku-class) that takes the raw directive, the context frame, and produces a version specific to the current situation. If the LLM call fails or times out (50ms budget), the raw directive is returned as-is."

injection-pipeline.md Section 4.1 says: "Templates for everything except memory contextualization. ... The recomposition step does not use an LLM at runtime." It explicitly rejects the LLM contextualization approach for latency, predictability, and cost reasons.

build-plan-v3 Step 3.1 implements injection-pipeline.md's template approach, not vision-v4's LLM approach. The recompose.go file does template variable substitution, not LLM calls.

**This is the correct decision** but it should be explicitly acknowledged in vision-v4 as a superseded design choice. As written, a reader of vision-v4 alone would expect LLM contextualization at injection time.

### 5.4 The Meilisearch Embedding Dependency

directive-schema.md Section 3 configures an OpenAI embedder for Meilisearch hybrid search:

```json
"embedders": {
    "default": {
        "source": "openAi",
        "model": "text-embedding-3-small",
        "documentTemplate": "..."
    }
}
```

This introduces a second external API dependency (OpenAI) in addition to Anthropic. It is not mentioned in vision-v4, injection-pipeline.md, or build-plan-v3. The build plan's Step 2.1 (Meilisearch integration) does not include embedder configuration.

**Recommendation:** Either remove the embedder configuration from directive-schema.md (hybrid search can be added later), or add it to build-plan-v3 Phase 2 with explicit OpenAI dependency documentation.

### 5.5 Cost Model

No document provides a cost estimate for LLM usage. Rough estimates:

**Decomposition (one-time per skill document):**

- 14 Superpowers skills + GSD methodology + Allium methodology = ~20 documents
- Each document is 1,000-5,000 tokens input, producing 15-30 directives
- At ~5 sections per document, each section sent to the LLM: ~100 LLM calls
- Using claude-sonnet-4-20250514: roughly $0.30-1.00 total for initial decomposition
- Negligible ongoing cost (re-decomposition only when skills change)

**Injection (per-request, if LLM contextualization were enabled -- currently it is not):**

- Per the template-based approach in injection-pipeline.md, there is zero LLM cost at injection time
- This is the correct decision

**Meilisearch embeddings (if enabled):**

- OpenAI text-embedding-3-small: $0.02 per million tokens
- ~700 directives \* ~50 tokens each = 35,000 tokens for initial indexing = negligible
- Embedding on each search query: ~50 tokens per query = negligible
- Total: effectively free at this scale

### 5.6 LLM Unavailability

build-plan-v3 correctly handles this:

- Decomposition: `NoopLLMClient` returns an error; ingest endpoint returns `"decomposition_unavailable"`. Directives can still be manually created via the CRUD API.
- Injection: No LLM dependency at injection time (template-based recomposition).
- Meilisearch: Falls back to keyword-only search without hybrid/semantic search if embedder is unavailable.

This is a clean degradation model. The system works without any LLM -- it just cannot auto-decompose skill documents.

---

## 6. The Cold Start Problem

### 6.1 When Hive-Server Launches Empty

On day zero, all three databases are empty. The injection pipeline returns `{"directives": [], "candidates_considered": 0}`. The MCP plugin gets nothing to inject. The system provides zero value.

### 6.2 How Directives Get Bootstrapped

The build plan provides two paths:

**Path 1: Manual creation via CRUD API (Phase 1)**
After Phase 1 (directive CRUD), a developer can `POST /api/v1/directives` to create directives manually. This is tedious but immediately available. A bootstrap script could create a starter set.

**Path 2: LLM decomposition via ingest endpoint (Phase 6)**
After Phase 6, `POST /api/v1/ingest` accepts a skill document and the LLM decomposes it into directives automatically. This is the intended primary path.

**The problem:** Phase 6 is the last phase. If the developer builds Phases 0-5 first (as specified), the injection pipeline exists but has no directives to serve for potentially weeks or months. The pipeline is architecturally complete but functionally empty.

### 6.3 What's Missing

**A bootstrap strategy.** The documents should specify:

1. **A seed directive set** -- a manually curated set of 20-50 high-value directives that can be loaded on first startup. These would be the most universal, highest-confidence directives extracted from the skill analysis (e.g., "reproduce before fixing," "run tests after changes," "commit atomic changes"). They could be embedded in the binary as a JSON file or loaded via a `script/seed` command.

2. **An accelerated decomposition path** -- the ingest endpoint exists in Phase 1 (build-plan-v3 Step 1.2) and creates a decomposition run. But the actual LLM decomposition does not happen until Phase 6. Consider making Phase 6 (or at least Step 6.1 + 6.2) available sooner, perhaps as Phase 2 or Phase 3, so that by the time the injection pipeline is built, there are directives to inject.

**Recommendation:** Add a "Step 0.4: Seed Directives" to Phase 0 that embeds a JSON file of 30-50 manually curated directives in the binary and loads them on first migration. This gives the injection pipeline something to work with immediately. Alternatively, reorder Phase 6 to before Phase 3 so decomposition runs before injection.

### 6.4 Minimum Viable Directive Set

Based on the skill-replacement-analysis, the highest-value directives are:

From **Superpowers** (anti-rationalization directives, highest weight):

1. "Before claiming any task is complete, run the verification command and read its full output." (verification-before-completion)
2. "When debugging, reproduce the problem first. Do not attempt a fix before you can reliably trigger the failure." (systematic-debugging)
3. "If there is even a 1% chance a behavioral skill applies to what you are doing, invoke it." (activation mandate)
4. "Before any creative work, brainstorm at least 3 meaningfully different approaches." (brainstorming)
5. "When writing tests, follow red-green-refactor: write a failing test first, then make it pass, then refactor." (TDD)

From **GSD** (workflow discipline): 6. "Commit completed work atomically. One commit per completed task with conventional commit messages." 7. "When a sub-agent finishes, verify its output against the acceptance criteria before marking the task done."

From **Allium** (specification discipline): 8. "When specs and code disagree, classify the mismatch: is it a spec bug, code bug, aspirational design, or intentional gap?"

A seed set of 20-30 directives covering the universal behavioral guardrails would be sufficient to make the injection pipeline useful from day one.

---

## 7. Schema Conflicts: Detailed Comparison

### 7.1 CockroachDB Schema: Three Versions

**Version A: vision-v4.md Section 2.2**

- Table `directives` with fields: `id`, `content`, `kind`, `source_type`, `source_id`, `source_name`, `source_section`, `trigger_tags` (JSONB), `trigger_intent`, `trigger_phase`, `trigger_scope`, `times_injected`, `times_followed`, `times_ignored`, `times_negative`, `effectiveness`, `related_ids` (JSONB), `supersedes_id`, `chain_id`, `weight`, `token_cost`, `active`, `tenant_id`, `created_at`, `updated_at`
- Inline effectiveness counters (denormalized)
- `tenant_id` for multi-tenancy
- `related_ids` as denormalized JSONB array

**Version B: directive-schema.md Section 2**

- Table `directives` with fields: `id`, `content`, `rationale`, `directive_type` (enum), `source_skill` (enum), `source_section`, `source_text_hash`, `context_triggers` (JSONB), `verification_criteria`, `effectiveness_score`, `priority` (int), `version`, `supersedes_id`, `is_active`, `decomposition_run_id` (FK), `created_at`, `updated_at`
- Separate `directive_tags` table (normalized)
- Separate `directive_relationships` table (normalized with kind enum: chains_to, conflicts_with, etc.)
- Separate `directive_feedback` table (normalized)
- Separate `directive_sets` + `directive_set_members` tables
- `decomposition_runs` table for provenance
- SQL view `active_directives` with aggregated tags and feedback stats
- SQL functions `update_effectiveness_score` and `match_directives`
- **No tenant_id column** (multi-tenancy not addressed)

**Version C: build-plan-v3 Step 1.1**

- Go struct `Directive` with fields matching directive-schema.md Version B
- Go struct `DecompositionRun` matching Version B
- `DirectiveStore` interface with CRUD methods
- References "CockroachDB schema for directives in internal/store/store.go migration" per directive-schema.md

**Assessment:** build-plan-v3 (Version C) follows directive-schema.md (Version B). vision-v4 (Version A) is a different, simpler schema with denormalized counters and multi-tenancy. **directive-schema.md should be authoritative.**

**Missing from directive-schema.md but present in vision-v4:** `tenant_id`. Multi-tenancy is not addressed in directive-schema.md. This is a significant gap -- the existing hive-server was designed for multi-tenant use via the X-Agent-ID header. If multi-tenancy is deferred, it should be explicitly stated. If it is needed, every table needs a `tenant_id` column and the indexes need to include it.

### 7.2 Meilisearch Index: Two Versions

**Version A: vision-v4.md Section 5.2**

- Index `directives` with searchableAttributes: `content`, `trigger_intent`, `trigger_tags`, `source_name`
- filterableAttributes: `kind`, `trigger_phase`, `trigger_scope`, `active`, `tenant_id`, `chain_id`
- Synonyms for debug/test/plan/review

**Version B: directive-schema.md Section 3**

- Index `directives` with searchableAttributes: `content`, `rationale`, `verification_criteria`, `tags`, `source_section`, `trigger_keywords`
- filterableAttributes: `directive_type`, `source_skill`, `source_section`, `activity_types`, `workflow_stages`, `priority`, `effectiveness_score`, `is_active`, `tags`, `created_at`, `version`
- More extensive synonyms, embedding configuration, custom ranking rules

**Version C: injection-pipeline.md Section 2.1**

- searchableAttributes: `content`, `description`, `tags`
- filterableAttributes: `category`, `priority`, `activity_tags`, `project_tags`, `language_tags`, `agent_id`, `enabled`

These are three different configurations. **directive-schema.md (Version B) is the most complete.** injection-pipeline.md introduces `project_tags` and `language_tags` which are not in directive-schema.md -- these would need to be added as flattened fields during the CRDB-to-Meilisearch sync.

### 7.3 Gel DB Schema: Three Versions

**Version A: vision-v4.md Section 5.3**

```
Directive { content, kind, source_name, weight, effectiveness, token_cost, active, crdb_id }
  - multi related_to: Directive
  - multi superseded_by: Directive
  - link chain: DirectiveChain
DirectiveChain { name, description, multi members: Directive }
Source { name, source_type, multi produced: Directive }
```

**Version B: directive-schema.md Section 4**

```
Directive { content, rationale, directive_type, source_skill, source_section, priority, effectiveness_score, is_active, version, crdb_id }
  - multi chains_to: Directive (with strength, description)
  - multi conflicts_with: Directive (with strength, description)
  - multi alternative_to: Directive (with strength, description)
  - multi refines: Directive (with strength, description)
  - multi requires: Directive
  - multi equivalent_to: Directive (with description)
  - supersedes: Directive
  - multi tags: str
  - multi activity_types: str
  - multi workflow_stages: WorkflowStage
DirectiveSet { name, description, is_active, multi members: Directive }
DecompositionRun { source_skill, source_section, source_document, source_text_hash, model_used, multi produced: Directive }
```

**Version C: build-plan-v3 Step 5.1**
Matches vision-v4 (Version A), not directive-schema.md (Version B).

**Assessment:** directive-schema.md (Version B) is dramatically more detailed. It models 6 distinct relationship types as separate multi-links with link properties, includes full provenance tracking, and has indexes on key fields. The vision-v4 schema is simplified to the point of losing the relationship type information -- a `related_to` link cannot distinguish "chains_to" from "conflicts_with."

**Recommendation:** Use directive-schema.md's Gel schema (Version B). Update build-plan-v3 Step 5.1 to match.

### 7.4 Injection Pipeline Schema: Additional Tables

injection-pipeline.md Section 2.2 references tables not defined in directive-schema.md:

- `directive_pins` (project-specific pinned directives)
- `agent_preferences` (learned per-agent directive weights)
- `injection_log` (injection history for deduplication)

build-plan-v3 Step 3.1 only creates `injection_log`. The `directive_pins` and `agent_preferences` tables are not in the build plan.

**Recommendation:** Add `directive_pins` and `agent_preferences` to the build plan, or remove those CRDB queries from injection-pipeline.md. The pinned directives feature is valuable (users can pin project-specific rules), but it needs a table and a management API.

---

## 8. The Honest Assessment

### 8.1 Is This Plan Buildable?

**Yes.** The architecture is coherent, the technology choices are sound, and the build plan breaks down into implementable steps with clear acceptance criteria. A solo developer with LLM assistance can build this.

The critical insight that makes v4 buildable where v3 was not: **v4 has one core data model (directives) with one core pipeline (inject) and one feedback loop (feedback).** Everything in the system serves one of these three functions. v3 had 6 separate domain models, 66 endpoints, and no unifying pipeline.

### 8.2 Biggest Risk: The Decomposition Quality

The entire system's value depends on whether the LLM decomposition (Phase 6) produces good directives. If the directives extracted from skill documents are too generic, too specific, too numerous, or too few, the injection pipeline will return unhelpful noise.

This risk is partially mitigated by:

- The manual CRUD API (Phase 1) allows human curation of directives
- The feedback loop (Phase 4) allows ineffective directives to be deprecated
- The skill-replacement-analysis provides worked examples of what good directives look like

But ultimately, the decomposition prompt (vision-v4 Section 3.3) will need significant iteration. The first decomposition of a skill document will probably produce 50% good directives and 50% junk. The prompt will need to be refined through multiple passes.

**Recommendation:** Budget Phase 6 for 2-3 iterations of the decomposition prompt, not just one implementation pass. The first version will need manual review and prompt tuning.

### 8.3 What's Most Likely to Go Wrong

1. **Schema confusion.** Three different schemas for the same data model will cause implementation bugs. A developer implementing Step 1.1 who references vision-v4 instead of directive-schema.md will create the wrong table. **Fix this before starting: declare directive-schema.md as the single source of truth for all schemas.**

2. **Meilisearch query preprocessing.** The injection pipeline depends on turning a conversation summary into a good Meilisearch query. The 10-word limit, stop word removal, and term extraction are all hand-waved. In practice, "Implementing the injection pipeline endpoint. Added a new handler in internal/handlers/inject.go. Working on the fan-out query logic." needs to become something like "injection pipeline handler fan-out query" to be a useful 10-word search. This preprocessing logic will need iteration. injection-pipeline.md Section 2.1 shows using the raw summary as the query, which will exceed the word limit.

3. **Gel DB operational overhead.** Gel requires 1GB RAM minimum and runs PostgreSQL internally. Adding Gel means the production deployment needs CockroachDB + Meilisearch + Gel + hive-server, which is 4 processes / containers. For a solo developer's workflow tool, this is heavy. The NoopGraphStore degradation is correctly designed, but in practice, Gel might never get deployed because the operational cost is not justified until the directive population is large enough for graph traversal to add value over flat queries.

### 8.4 What I Would Change

1. **Declare directive-schema.md as the authoritative schema document.** Update vision-v4 and injection-pipeline.md to reference it rather than defining their own schemas. Add a note at the top of vision-v4 Section 2.2: "The conceptual schema below is illustrative. See directive-schema.md for the authoritative DDL."

2. **Reorder Phase 6 (Decomposition) to Phase 2.** The current ordering means the injection pipeline exists for weeks with no directives. Moving decomposition earlier means directives are available when the injection pipeline is first tested. The dependency chain allows this: Phase 6 depends on Step 1.1 (directive store) and Step 6.1 (LLM client), neither of which depends on Meilisearch or the injection pipeline.

3. **Add a seed directives step.** Embed 30-50 manually curated directives as a JSON file in the binary. Load them on first migration. This provides immediate value and serves as test data for development.

4. **Defer Gel DB to a future version.** The directive-schema.md defines relationships in CockroachDB via the `directive_relationships` join table with recursive CTE potential. This provides 80% of Gel's value (relationship traversal) without the operational overhead. Gel's primary advantage is cleaner query syntax for graph traversal -- a real but non-critical benefit. Build Phase 5 when the directive population is large enough (500+ directives) for graph queries to matter.

5. **Add multi-tenancy to directive-schema.md.** The vision-v4 schema includes `tenant_id`. The authoritative schema does not. Either add it now (recommended -- it is much harder to add later) or document the explicit decision to defer it.

6. **Specify the Meilisearch query preprocessing step.** Add a concrete function signature and algorithm for converting the `context.summary` field into a Meilisearch-friendly query that respects the 10-word limit. This is a small but critical piece that currently lives between injection-pipeline.md's search query construction and Meilisearch's documented limitations.

7. **Add a formal migration tool (goose) to Phase 0.** The build plan describes schema changes as inline DDL in store.go. This does not scale. goose with embedded SQL migration files is recommended in cockroachdb.md Section 5 and should be adopted from the start.

---

## 9. Issue Tracker

### CRITICAL (must fix before implementation)

| #   | Issue                                                                                      | Location                                                     | Resolution                                                                                                                                   |
| --- | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------- |
| C1  | Three conflicting directive schemas across documents                                       | vision-v4 S2.2, directive-schema S2, injection-pipeline S4.2 | Declare directive-schema.md as authoritative. Update all other documents to reference it.                                                    |
| C2  | LLM contextualization at injection time: vision-v4 says yes, injection-pipeline.md says no | vision-v4 S4.6 vs injection-pipeline S4.1                    | Adopt injection-pipeline.md's template approach (already implemented in build-plan). Add note to vision-v4 acknowledging this as superseded. |

### SIGNIFICANT (should fix before starting the affected phase)

| #   | Issue                                                                                                                               | Location                | Resolution                                                                    |
| --- | ----------------------------------------------------------------------------------------------------------------------------------- | ----------------------- | ----------------------------------------------------------------------------- |
| S1  | No seed directive bootstrap strategy                                                                                                | All documents           | Add Step 0.4: embed 30-50 curated directives as JSON, load on first migration |
| S2  | Gel schema in build-plan-v3 uses simplified vision-v4 schema, not the detailed directive-schema.md version                          | build-plan-v3 Step 5.1  | Update Step 5.1 to use directive-schema.md Section 4 SDL                      |
| S3  | `directive_pins` and `agent_preferences` tables referenced in injection-pipeline.md but not in directive-schema.md or build-plan-v3 | injection-pipeline S2.2 | Add these tables to directive-schema.md and build-plan Phase 3                |
| S4  | No multi-tenancy in directive-schema.md                                                                                             | directive-schema S2     | Add `tenant_id UUID NOT NULL` to directives, feedback, and related tables     |
| S5  | No formal migration tool specified                                                                                                  | build-plan-v3 Phase 0   | Add goose to Phase 0.2                                                        |

### ADVISORY (recommended improvements)

| #   | Issue                                                                                            | Location                      | Resolution                                                                                     |
| --- | ------------------------------------------------------------------------------------------------ | ----------------------------- | ---------------------------------------------------------------------------------------------- |
| A1  | Meilisearch 10-word query limit not explicitly handled in injection pipeline preprocessing       | injection-pipeline S2.1       | Add query truncation/extraction step with concrete algorithm                                   |
| A2  | Error messages for LLM agents not addressed                                                      | ultrathink-devex S4           | Add error message improvements to Phase 0 or 1                                                 |
| A3  | `repo` column still missing from existing memory/tasks tables                                    | ultrathink-devex S9.1         | Add to Phase 0.2 schema hardening                                                              |
| A4  | OpenAI embedding dependency in directive-schema.md Meilisearch config not in build plan          | directive-schema S3 embedders | Either remove embedder config or add to Phase 2 with dependency documentation                  |
| A5  | Phase 6 decomposition occurs after injection pipeline, leaving pipeline empty during development | build-plan-v3 phase ordering  | Consider reordering Phase 6 before Phase 3, or adding seed directives                          |
| A6  | No estimate of total project duration                                                            | build-plan-v3                 | Add rough calendar estimate: Phase 0-3 (~4-6 weeks), Phase 4-6 (~4-6 weeks), total ~2-3 months |

---

## 10. Conclusion

The v4 architecture is a substantial improvement over v3. The paradigm shift from "66 CRUD endpoints replacing skills" to "decompose skills into directives, inject them contextually" is the right move. It aligns with the honest assessment in the skill-replacement-analysis, addresses the skeptic's core objection (now there is a concrete consumer), and produces a system with a clear value proposition: agents get the right behavioral guidance at the right time, and the guidance improves over time through feedback.

The documents are well-written, technically detailed, and mostly consistent. The critical issues (schema conflicts, LLM dependency specification) are reconcilable without redesigning the architecture. The significant issues (cold start, missing tables, multi-tenancy) are additive -- they require adding things, not changing things.

**The plan is ready to build once the critical issues are resolved.** The most productive first action is to update directive-schema.md with multi-tenancy support, declare it as the single authoritative schema, and then begin Phase 0.
