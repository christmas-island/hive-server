# Final Review: v5 Documentation Suite

**Date:** 2026-03-09
**Reviewer:** Quality gate review against vision-v5.md as sole authority
**Documents reviewed:**

1. vision-v5.md (THE AUTHORITY)
2. directive-schema-v5.md
3. injection-pipeline-v5.md
4. recomposition-design-v5.md
5. build-plan-v5.md

---

## Part 1: v4 Audit Checklist (15 Items)

### 1. Directive Taxonomy Consistency

**PASS (with minor exception)**

All five v5 documents use the correct 5 kinds: `behavioral`, `pattern`, `contextual`, `corrective`, `factual`. The field name is `kind` (TEXT) everywhere. No `procedural`, `guardrail`, or `directive_type` appears in any v5 document as a current definition.

**Exception:** The word `category` appears in vision-v5.md lines 1244 and 1298 in the context of ingest endpoint metadata (`"category": "debugging"`) and YAML frontmatter parsing (`category: process`). These are metadata fields on the ingestion source, not the directive taxonomy. This is acceptable -- `category` here describes the source document's category, not the directive kind.

---

### 2. Weight Not Priority

**FAIL**

**recomposition-design-v5.md** contains a stale `Priority int` field that was not corrected in the v5 rewrite:

- **Line 155:** `DirectiveForSynthesis` struct has `Priority int` instead of `Weight float64`
- **Line 385:** Example table header says "Priority" with integer values (70, 65, 95, 60)
- **Line 464:** Second example table header also says "Priority" with integer values (90, 65, 70)

The Go struct and both example tables in the concrete examples section still use the v4-era `Priority int` field. Every other v5 document correctly uses `weight FLOAT8` (0.0-2.0). The build-plan-v5.md's `DirectiveForSynthesis` struct (Step 3.2, line 968-975) correctly uses `Weight float64`, which confirms this is a missed fix in recomposition-design-v5.md specifically.

**Fix required:** In recomposition-design-v5.md:

1. Line 155: Change `Priority int` to `Weight float64`
2. Lines 385, 464: Change table header "Priority" to "Weight" and use float values (e.g., 1.4, 1.3, 1.8, 1.2)

---

### 3. Multi-Tenancy

**FAIL (partially)**

`tenant_id` is present on all table definitions in directive-schema-v5.md (decomposition_runs, directives, agent_sessions, injections, injection_outcomes, ingestion_sources). It is present on all CRDB indexes and Meilisearch filterable attributes.

However, there are three inconsistencies:

**3a. Vision feedback UPDATE queries missing tenant_id (vision-v5.md lines 832-855):**

The vision's feedback update SQL (Section 6.2) uses `WHERE id = $1;` without `AND tenant_id = $2`. The directive-schema-v5.md (lines 322-345) and injection-pipeline-v5.md (lines 591-614) correctly include `AND tenant_id = $2`. But build-plan-v5.md (lines 1121-1144) also omits `tenant_id`, matching the vision's bug.

Since the vision is the authority, this is technically "correct per vision" but it is a security hole -- any tenant could update any other tenant's directive effectiveness. The other docs got it right; the vision and build plan got it wrong.

**3b. Vision's injections and injection_outcomes tables missing tenant_id (vision-v5.md lines 633-649):**

The vision's `injections` table (Section 5.1) has no `tenant_id` column. The vision's `injection_outcomes` table also has no `tenant_id` column. However, directive-schema-v5.md (lines 257-283) correctly includes `tenant_id` on both tables. Build-plan-v5.md (lines 876-895) omits `tenant_id` on both tables, matching the vision's omission.

This is a gap in the authoritative vision itself. It is fixed in directive-schema-v5.md but not propagated to build-plan-v5.md.

**3c. Build plan Step 4.1 feedback SQL missing tenant_id (lines 1121-1144):**

The feedback update queries in build-plan-v5.md use `WHERE id = $1;` without tenant_id, matching the vision's omission.

**Recommendation:** The vision should be patched to add `tenant_id` to the `injections` and `injection_outcomes` tables and to all feedback UPDATE queries. Then build-plan-v5.md should be updated to match. The directive-schema-v5.md and injection-pipeline-v5.md already have the correct versions.

---

### 4. Ranking Formula Consistency

**PASS**

The formula `score = (meilisearch_relevance * 0.4) + (effectiveness * 0.3) + (weight * 0.2) + (recency_bonus * 0.1)` appears identically in:

- vision-v5.md Section 4.4 (lines 509-512)
- directive-schema-v5.md Section 6 (lines 1210-1214) and Section 7 (lines 1332-1337)
- injection-pipeline-v5.md Section 3.1 (lines 269-273)
- build-plan-v5.md Step 3.1 (lines 850-851)

No alternative formulas found. No `source_boost`, no type-level normalization.

---

### 5. Effectiveness Formula

**PASS**

The formula `(times_followed - times_negative) / GREATEST(times_injected, 1)` with default 0.0 appears consistently in:

- vision-v5.md Section 2.2 (line 114), Section 6.2 (lines 836, 844, 852)
- directive-schema-v5.md Section 2 (lines 208, 308, 326, 334, 343)
- injection-pipeline-v5.md Section 3.1 (line 277), Section 7.1 (lines 595, 603, 612)
- build-plan-v5.md Step 4.1 (lines 1125, 1133, 1142)

No `(followed + helpful) / total` variants found.

---

### 6. Feedback Outcomes

**PASS**

Exactly 3 outcomes everywhere: `followed`, `ignored`, `negative`. No `partially_followed`, `inapplicable`, `helpful`, or `unhelpful` in any v5 document (verified by grep).

---

### 7. No Fabricated Constraints

**FAIL (two instances)**

**7a. recomposition-design-v5.md line 169:** "No micro-prompt exceeds a per-directive token limit (150 tokens)"

**7b. recomposition-design-v5.md line 212:** "Is concise (1-3 sentences, max 150 tokens)"

Build-plan-v5.md line 28 explicitly calls out "No per-directive token limit (150 tokens)" as a fabricated constraint that was removed. Yet recomposition-design-v5.md still contains it in two places. This is a direct contradiction between the build plan and the recomposition design.

**7c. injection-pipeline-v5.md lines 497-504:** Contains specific latency estimates (50-200ms for fan-out, 300-400ms for TTFT, "~94 tokens/sec at Haiku class"). These are presented as estimates in a table, not as hard constraints, and the surrounding text says "accuracy over speed" with no hard deadline. This is borderline -- the numbers are informational rather than constraining. The build plan has similar informational latency tables (lines 1051-1058). These are acceptable as planning estimates.

**7d. directive-schema-v5.md line 1248:** "If the LLM call fails or times out (50ms budget), the raw directive is returned as-is."

This is a direct quote from the vision's Section 4.6 (before the recomposition design document corrected it). The recomposition-design-v5.md Section 2 (lines 79-87) explicitly discusses this 50ms budget and explains why it is replaced by a multi-second batch synthesis. Yet the directive-schema-v5.md in Section 6 still quotes the original 50ms as if it is current. This is a stale reference that contradicts both the recomposition design and the build plan.

---

### 8. Endpoint Count

**PASS**

Vision-v5.md Section 8.1 (lines 1146-1157) defines 9 endpoints. Build-plan-v5.md API Endpoint Summary (lines 1592-1607) lists exactly the same 9 endpoints. No admin endpoints in the core API. Operational tooling (metrics, sync, integrity) is noted as Phase 7 implementation detail, not core API.

---

### 9. Request/Response Payload Alignment

**PASS (with minor discrepancies)**

**Inject Request:**

Vision Section 8.2 (lines 1169-1183) defines the inject request body with no `agent_id` field -- agent identity comes from the `X-Agent-ID` header. However:

- Vision Section 4.2 example (lines 430-449) includes `"agent_id": "agent-007"` in the request body
- injection-pipeline-v5.md (lines 27-46) includes `"agent_id": "agent-007"` in the request body and lists it in the field spec (line 51)
- Vision scenarios (7.1, 7.2, 7.3) all include `"agent_id"` in the body

The formal endpoint spec says no `agent_id` in the body; all examples include it. Build-plan-v5.md's `InjectRequest` struct (lines 787-792) correctly omits `agent_id` from the Go struct (matching the formal spec), getting agent_id from the middleware context.

This is an inconsistency within the vision itself (formal spec vs. examples). Since the formal spec (Section 8.2) is authoritative over examples, the build plan got it right. The injection-pipeline-v5.md should drop `agent_id` from the request body field spec.

**Inject Response:**

Vision Section 4.7 (line 594) includes `"context_hash": "sha256:abc123..."` in the response. Vision Section 8.2 (lines 1185-1201) does NOT include `context_hash`. injection-pipeline-v5.md response (lines 401-444) does NOT include `context_hash`. Build-plan-v5.md `InjectionResponse` struct (lines 806-813) does NOT include `context_hash`.

Minor inconsistency within the vision (narrative example vs. formal spec). Since the formal spec omits it, the other docs are correct.

---

### 10. Session Tables

**PASS**

All v5 documents use `agent_sessions`, `injections`, `injection_outcomes` -- not a flat `injection_log`. Verified in:

- vision-v5.md Section 5.1
- directive-schema-v5.md Section 2
- build-plan-v5.md Step 3.1
- injection-pipeline-v5.md Section 2.2

---

### 11. No Governance Inversion

**FAIL (stale references)**

No document declares itself as overriding the vision. However, several v5 documents still reference **vision-v4.md** as the authority instead of **vision-v5.md**:

- **directive-schema-v5.md line 6:** `**Authority:** vision-v4.md`
- **directive-schema-v5.md line 53:** `Per vision-v4, there are exactly five directive kinds.`
- **injection-pipeline-v5.md line 5:** `**Authoritative source:** vision-v4.md`
- **recomposition-design-v5.md line 4:** `Defines the recomposition approach for vision-v4 Section 4.6`
- **build-plan-v5.md lines 5, 12, 14, 42, 584:** Multiple references to `vision-v4.md`

The vision document itself says `**Supersedes:** vision-v4.md` (line 5). All other v5 documents should reference `vision-v5.md` as the authority, not `vision-v4.md`. These are stale references from when the supporting documents were written against v4 and then updated to become "v5" without fixing the authority references.

This is not a governance inversion (no doc claims to override the vision), but it is a document hygiene issue that could cause confusion about which version of the vision is authoritative.

---

### 12. Source Fields

**PASS**

Three TEXT fields (`source_type`, `source_id`, `source_name`) everywhere. No fixed ENUM for source types. Verified in:

- vision-v5.md Section 2.2 (lines 98-100)
- directive-schema-v5.md Section 1 (lines 70-72) and Section 2 (lines 192-194)
- build-plan-v5.md Step 1.1 (lines 426-429)

---

### 13. Gel Schema

**PASS**

All Gel schemas use `DirectiveChain` with `multi member directives` links, not pairwise relationship links on the Directive type:

- vision-v5.md Section 5.3 (lines 723-733)
- directive-schema-v5.md Section 4 (lines 851-892)
- build-plan-v5.md Step 5.1 (lines 1217-1257)

All three Gel schemas are structurally identical. The Directive type has `related_to` and `superseded_by` multi links for its own relationships, but chains use the DirectiveChain type with member links. This matches the required pattern.

---

### 14. Model References

**FAIL (one document)**

**injection-pipeline-v5.md** references a specific model version:

- Line 350: `[Call recomposition LLM (Haiku-class, e.g., Claude Haiku 4.5)]`
- Line 366: `Claude Haiku 4.5 is the recommended model.`
- Line 388: `Per-request cost with Claude Haiku 4.5 is approximately $0.003-0.005`

The other documents correctly use "Haiku-class model" generically:

- vision-v5.md Section 4.6: "Haiku-class"
- recomposition-design-v5.md Section 6: "Haiku-class models"
- build-plan-v5.md Step 3.2: "Haiku-class by default; let configuration determine the specific model"

The injection-pipeline-v5.md should say "Haiku-class model" without naming `Claude Haiku 4.5` specifically. The cost estimate is also tied to a specific model's pricing.

---

### 15. Cross-Document Schema Consistency

**PASS (with noted exceptions already flagged)**

**CRDB table definitions:**
The `directives` table DDL matches across vision-v5.md (Section 2.2), directive-schema-v5.md (Section 2), and build-plan-v5.md (Step 1.1 and Step 3.1). Column names, types, defaults, and constraints are identical.

Exceptions already noted: `tenant_id` gaps on `injections` and `injection_outcomes` tables (see item 3).

**Meilisearch config:**
The index configuration (searchableAttributes, filterableAttributes, sortableAttributes, rankingRules, synonyms) is identical across:

- vision-v5.md Section 5.2
- directive-schema-v5.md Section 3
- injection-pipeline-v5.md Section 2.1
- build-plan-v5.md Step 2.1

**Go struct fields vs SQL columns:**
Build-plan-v5.md's `Directive` struct (Step 1.1, lines 420-456) maps correctly to the SQL columns. `DirectiveKind` is a `string` type with constants matching the 5 kinds. `Weight` is `float64`, `Effectiveness` is `float64`, `TenantID` is `string`, `TriggerTags` is `[]string`.

**Gel schema:**
All three Gel SDL definitions (vision, directive-schema, build-plan) are structurally identical.

---

## Part 2: New Issues Found in v5

### NEW-1: Phase enum inconsistency (`brainstorming` as phase)

The inject request's `context.phase` field has an inconsistent enum:

- **Vision Section 8.2** (line 1175): Lists `planning|implementation|debugging|review|brainstorming`
- **injection-pipeline-v5.md** Phase Enum table (line 58): Lists `planning`, `implementation`, `debugging`, `review`, `brainstorming`
- **Vision Section 2.2** `trigger_phase` column comment (line 106): Lists `"planning", "implementation", "debugging", "review"` -- NO `brainstorming`
- **directive-schema-v5.md** field spec (line 76): Lists `planning`, `implementation`, `debugging`, `review`, `any` -- NO `brainstorming`
- **directive-schema-v5.md** DDL comment (line 200): Lists `"planning", "implementation", "debugging", "review"` -- NO `brainstorming`
- **Vision Section 7.1** brainstorming scenario: Uses `"phase": "planning"` -- NOT `"brainstorming"`
- **build-plan-v5.md** InjectContext struct (line 798): Comment lists `planning, implementation, debugging, review, brainstorming`
- **build-plan-v5.md** Directive struct TriggerPhase (line 434): Comment lists `'planning', 'implementation', 'debugging', 'review', 'any'`

**The problem:** The request can send `brainstorming` as a phase, but the `trigger_phase` column on directives does not recognize `brainstorming` as a valid value. Directives stored with `trigger_phase = 'planning'` or `trigger_phase = 'any'` would match, but there is no way to store a directive that only activates during brainstorming and not during planning.

This needs a decision: either add `brainstorming` to the trigger_phase enum everywhere, or keep the 4+any trigger phases and map `brainstorming` to `planning` at query time.

---

### NEW-2: Stale vision version references throughout

All v5 supporting documents still reference `vision-v4.md` instead of `vision-v5.md`. Since vision-v5.md `Supersedes: vision-v4.md`, these references are stale. Detailed in checklist item 11.

---

### NEW-3: `SourceSkill` field in recomposition-design-v5.md

recomposition-design-v5.md line 156 has `SourceSkill string` in the `DirectiveForSynthesis` struct. Build-plan-v5.md line 975 has the correct `SourceName string`. The field name does not match.

---

### NEW-4: Specific latency numbers in injection-pipeline-v5.md

injection-pipeline-v5.md Section 6 (lines 494-504) contains specific latency values: "300-400ms" for TTFT, "~94 tokens/sec at Haiku class", "2,500-3,000ms" for generation. While presented as estimates, these are tied to a specific model (Haiku 4.5) and will become stale as models change. The build plan's similar table (lines 1051-1058) is more generic ("2,500-3,500ms" without tokens/sec claim).

This is borderline. The numbers are informational, not constraining, but they pin to a specific model performance profile.

---

### NEW-5: `directive_ids` column missing from injections table

The `injections` table stores `directives JSONB` (an array of `{directive_id, confidence}` pairs). However, the deduplication query in injection-pipeline-v5.md line 205 queries `SELECT directive_ids FROM injections` -- a column name that does not exist. The actual column name is `directives`. This is a minor SQL typo but would be a runtime error.

---

### NEW-6: Vision scenario 7.1 uses `phase: "planning"` for brainstorming

Vision Section 7.1 is titled "Agent Starts a Brainstorming Session" but the request payload (line 919) uses `"phase": "planning"`. Meanwhile, the injection-pipeline-v5.md Scenario 1 (line 686) for the same brainstorming scenario uses `"phase": "brainstorming"`. This is directly contradictory between the vision and the injection-pipeline for the same conceptual scenario.

---

### NEW-7: Cost estimates tied to specific model pricing

injection-pipeline-v5.md line 388 states: "Per-request cost with Claude Haiku 4.5 is approximately $0.003-0.005 (~0.3-0.5 cents). At 1,000 injections/day, monthly cost is approximately $90-150." These are fabricated specifics tied to a model version that may not exist or have different pricing. The recomposition-design-v5.md Section 6 correctly avoids specific cost numbers ("a fraction of a cent").

---

## Part 3: Cross-Document Consistency Matrix

| Check                                                          | vision-v5                 | directive-schema-v5              | injection-pipeline-v5             | recomposition-design-v5                    | build-plan-v5             |
| -------------------------------------------------------------- | ------------------------- | -------------------------------- | --------------------------------- | ------------------------------------------ | ------------------------- |
| **5 kinds (behavioral/pattern/contextual/corrective/factual)** | PASS                      | PASS                             | PASS                              | PASS                                       | PASS                      |
| **Field name `kind` (not `directive_type`)**                   | PASS                      | PASS                             | PASS                              | PASS                                       | PASS                      |
| **`weight` FLOAT8 (not `priority` INT)**                       | PASS                      | PASS                             | PASS                              | **FAIL** (Priority int, lines 155/385/464) | PASS                      |
| **`tenant_id` on directives table**                            | PASS                      | PASS                             | PASS                              | n/a                                        | PASS                      |
| **`tenant_id` on agent_sessions**                              | PASS                      | PASS                             | n/a                               | n/a                                        | PASS                      |
| **`tenant_id` on injections**                                  | **FAIL** (missing)        | PASS                             | n/a                               | n/a                                        | **FAIL** (missing)        |
| **`tenant_id` on injection_outcomes**                          | **FAIL** (missing)        | PASS                             | n/a                               | n/a                                        | **FAIL** (missing)        |
| **`tenant_id` in feedback UPDATE queries**                     | **FAIL** (uses `$1` only) | PASS (`$1 AND $2`)               | PASS (`$1 AND $2`)                | n/a                                        | **FAIL** (uses `$1` only) |
| **`tenant_id` in Meilisearch filters**                         | PASS                      | PASS                             | PASS                              | n/a                                        | PASS                      |
| **Ranking formula (0.4/0.3/0.2/0.1)**                          | PASS                      | PASS                             | PASS                              | n/a                                        | PASS                      |
| **Effectiveness formula**                                      | PASS                      | PASS                             | PASS                              | n/a                                        | PASS                      |
| **3 feedback outcomes**                                        | PASS                      | PASS                             | PASS                              | n/a                                        | PASS                      |
| **9 endpoints**                                                | PASS                      | n/a                              | n/a                               | n/a                                        | PASS                      |
| **Session tables (not injection_log)**                         | PASS                      | PASS                             | PASS                              | n/a                                        | PASS                      |
| **Source: 3 TEXT fields (not ENUM)**                           | PASS                      | PASS                             | PASS                              | n/a                                        | PASS                      |
| **Gel: DirectiveChain with member links**                      | PASS                      | PASS                             | PASS                              | n/a                                        | PASS                      |
| **Model: "Haiku-class" (not specific version)**                | PASS                      | n/a                              | **FAIL** (names Claude Haiku 4.5) | PASS                                       | PASS                      |
| **No 50ms fabricated timeout**                                 | PASS (corrected in 4.6)   | **FAIL** (line 1248 quotes 50ms) | PASS                              | PASS (discusses correction)                | PASS                      |
| **No 150-token per-directive cap**                             | n/a                       | n/a                              | n/a                               | **FAIL** (lines 169, 212)                  | PASS (explicitly removes) |
| **Phase enum includes `brainstorming`**                        | INCONSISTENT              | NO                               | YES                               | n/a                                        | INCONSISTENT              |
| **Authority references vision-v5**                             | PASS (is the vision)      | **FAIL** (says v4)               | **FAIL** (says v4)                | **FAIL** (says v4)                         | **FAIL** (says v4)        |

---

## Part 4: Verdict

### Issues Requiring Fixes Before Execution

**Must fix (correctness/security):**

1. **recomposition-design-v5.md: `Priority int` -> `Weight float64`** (3 locations). This is a direct contradiction of the weight system that every other document uses.

2. **Vision-v5.md: Add `tenant_id` to `injections` and `injection_outcomes` tables.** Without this, multi-tenant isolation is broken for session tracking and feedback. directive-schema-v5.md already has the fix; it just needs to be backported to the vision and build plan.

3. **Build-plan-v5.md: Add `tenant_id` to `injections` and `injection_outcomes` tables** (Step 3.1 migration, lines 876-895). Also add `tenant_id` to feedback UPDATE queries (Step 4.1, lines 1121-1144).

4. **Resolve the `brainstorming` phase inconsistency.** Either add `brainstorming` to trigger_phase everywhere (vision DDL comment, directive-schema field spec, build plan Directive struct comment), or document that `brainstorming` is mapped to `planning` at query time.

**Should fix (consistency):**

5. **recomposition-design-v5.md: Remove 150-token per-directive cap** (lines 169, 212). The build plan explicitly calls this out as a fabricated constraint.

6. **directive-schema-v5.md line 1248: Remove stale 50ms timeout reference.** Replace with language consistent with the recomposition design's multi-second batch synthesis.

7. **injection-pipeline-v5.md: Replace "Claude Haiku 4.5" with "Haiku-class model"** (lines 350, 366, 388). Remove specific cost estimates tied to model pricing.

8. **All supporting docs: Update authority references from `vision-v4.md` to `vision-v5.md`.** This is a search-and-replace across directive-schema-v5.md, injection-pipeline-v5.md, recomposition-design-v5.md, and build-plan-v5.md.

9. **recomposition-design-v5.md line 156: Change `SourceSkill` to `SourceName`** to match the build plan's `DirectiveForSynthesis` struct.

10. **injection-pipeline-v5.md line 205: Fix `directive_ids` to `directives`** to match the actual column name on the `injections` table.

**Nice to fix (cleanup):**

11. **injection-pipeline-v5.md: Remove `agent_id` from request body field spec** (line 51). The formal vision endpoint spec (Section 8.2) puts agent_id in the header only; the body should not duplicate it.

12. **Vision-v5.md Section 4.7: Remove `context_hash` from the narrative example response** (line 594). The formal endpoint spec (Section 8.2) does not include it, and no other document includes it in the response.

### Overall Assessment

**Not yet ready to execute.** The v5 suite is dramatically better than v4. The major structural problems (taxonomy, weight vs priority, fabricated constraints, governance inversion) have been resolved in 4 of 5 documents. But recomposition-design-v5.md has 5 stale issues that slipped through the rewrite, and there is a genuine multi-tenancy gap in the vision itself that needs patching.

The fix list is small and mechanical. After addressing items 1-4 (the must-fix issues), the suite is ready to execute. Items 5-10 should be fixed in the same pass. Items 11-12 are cosmetic and can be addressed during implementation.

**Estimated effort to reach "ready to execute": 1-2 hours of targeted edits.**
