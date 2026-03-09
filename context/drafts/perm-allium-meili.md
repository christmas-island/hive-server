# Meilisearch as a Search Backend for Allium Behavioral Specifications

**Purpose:** Deep analysis of how Meilisearch could enhance the Allium behavioral specification language by enabling full-text search, cross-project discovery, and agent-driven spec management through hive-server.

**Date:** 2026-03-09

---

## Table of Contents

1. [The Problem: Allium Specs Are Opaque Flat Files](#1-the-problem-allium-specs-are-opaque-flat-files)
2. [Index Schema Proposals](#2-index-schema-proposals)
3. [Typo-Tolerant Search for Specs](#3-typo-tolerant-search-for-specs)
4. [Faceted Search Across Spec Constructs](#4-faceted-search-across-spec-constructs)
5. [Agent Integration: Tend and Weed](#5-agent-integration-tend-and-weed)
6. [Cross-Project Spec Discovery](#6-cross-project-spec-discovery)
7. [The 10-Word Query Limit](#7-the-10-word-query-limit)
8. [LLM Context Loading via Search](#8-llm-context-loading-via-search)
9. [hive-server as Mediator](#9-hive-server-as-mediator)
10. [Tradeoffs](#10-tradeoffs)
11. [Recommendations](#11-recommendations)

---

## 1. The Problem: Allium Specs Are Opaque Flat Files

Allium deliberately produces plain-text `.allium` files with no runtime, no database, and no query capability. The Allium tech brief itself lists this as a known limitation: "You cannot query the spec for 'which rules affect entity X' programmatically."

For a single project with a handful of spec files, this is fine. An LLM can load all the `.allium` files into context and reason over them. But this approach breaks down along three axes:

1. **Scale within a project.** As a codebase grows, spec files accumulate. A mature system might have dozens of entities, hundreds of rules, and multiple surfaces. Loading all of them into an LLM context window wastes tokens and dilutes attention.

2. **Scale across projects.** Allium's library specs and `use` declarations envision reusable behavioral patterns (OAuth flows, payment processing, email delivery). Finding relevant specs across a portfolio of projects currently requires knowing they exist.

3. **Agent operations.** The Tend and Weed agents need to find related specs to do their jobs. Tend needs to know what other specs reference an entity it is modifying. Weed needs to find all specs that could conflict with observed implementation behavior. Both currently rely on the LLM reading every spec file, which is token-expensive and error-prone at scale.

Meilisearch addresses all three by making spec content searchable with sub-50ms response times, typo tolerance, and faceted filtering.

---

## 2. Index Schema Proposals

The Allium language has a well-defined set of constructs. Each maps naturally to a Meilisearch document type. The question is whether to use one index or many.

### 2.1 Recommended Approach: Single Index with Construct Type Faceting

A single `allium_specs` index with a `construct_type` field used for filtering and faceting. This aligns with Meilisearch's recommendation against many small indexes (they serialize task processing per-index) and keeps cross-construct search simple.

```go
settings := &meilisearch.Settings{
    SearchableAttributes: []string{
        "name",           // PascalCase construct name (highest priority)
        "body",           // full text of the construct block
        "description",    // prose annotations and comments
        "triggers",       // rule triggers (when clauses)
        "ensures",        // rule postconditions
        "fields",         // entity/value type field names
        "references",     // names of other constructs referenced
    },
    FilterableAttributes: []string{
        "construct_type", // entity, rule, surface, contract, invariant, etc.
        "module",         // source .allium file path
        "project",        // project identifier
        "severity",       // for invariants: critical, warning, info
        "domain",         // user-assigned domain tag (auth, billing, etc.)
        "actors",         // actor names involved (for surfaces)
        "version",        // allium version marker
        "updated_at",     // last modification timestamp
    },
    SortableAttributes: []string{
        "name",
        "updated_at",
        "module",
    },
    DisplayedAttributes: []string{
        "id", "name", "construct_type", "module", "project",
        "body", "description", "triggers", "ensures", "fields",
        "references", "severity", "domain", "actors", "version",
        "updated_at", "source_file", "line_start", "line_end",
    },
    Synonyms: map[string][]string{
        "rule":      {"behavior", "transition", "action"},
        "entity":    {"model", "object", "aggregate"},
        "surface":   {"boundary", "interface", "api", "endpoint"},
        "contract":  {"obligation", "protocol", "agreement"},
        "invariant": {"constraint", "assertion", "guarantee"},
        "ensures":   {"postcondition", "outcome", "effect"},
        "requires":  {"precondition", "guard", "prerequisite"},
    },
    TypoTolerance: &meilisearch.TypoTolerance{
        Enabled: true,
        MinWordSizeForTypos: &meilisearch.MinWordSizeForTypos{
            OneTypo:  4, // Allium names are often short (e.g., "User")
            TwoTypos: 8,
        },
    },
}
```

### 2.2 Document Shape

Each construct extracted from a parsed `.allium` file becomes one Meilisearch document:

```json
{
  "id": "proj-acme/auth.allium/rule/RequestPasswordReset",
  "name": "RequestPasswordReset",
  "construct_type": "rule",
  "module": "auth.allium",
  "project": "acme",
  "domain": "auth",
  "body": "rule RequestPasswordReset {\n    when: UserRequestsPasswordReset(email)\n    let user = User{email}\n    requires: exists user\n    requires: user.status in {active, locked}\n    ensures: existing reset tokens invalidated\n    ensures: new ResetToken created\n    ensures: email sent to user\n}",
  "description": "",
  "triggers": "UserRequestsPasswordReset(email)",
  "ensures": "existing reset tokens invalidated; new ResetToken created; email sent to user",
  "fields": "",
  "references": "User, ResetToken",
  "severity": "",
  "actors": "",
  "version": "2",
  "updated_at": "2026-03-09T12:00:00Z",
  "source_file": "/specs/auth.allium",
  "line_start": 45,
  "line_end": 53
}
```

```json
{
  "id": "proj-acme/auth.allium/entity/User",
  "name": "User",
  "construct_type": "entity",
  "module": "auth.allium",
  "project": "acme",
  "domain": "auth",
  "body": "entity User {\n    email: String\n    status: active | locked | suspended\n    password_hash: String\n    last_login: Timestamp?\n    reset_tokens: Set<ResetToken> with user = this\n}",
  "description": "",
  "triggers": "",
  "ensures": "",
  "fields": "email, status, password_hash, last_login, reset_tokens",
  "references": "ResetToken",
  "severity": "",
  "actors": "",
  "version": "2",
  "updated_at": "2026-03-09T12:00:00Z",
  "source_file": "/specs/auth.allium",
  "line_start": 10,
  "line_end": 17
}
```

```json
{
  "id": "proj-acme/billing.allium/surface/Dashboard",
  "name": "Dashboard",
  "construct_type": "surface",
  "module": "billing.allium",
  "project": "acme",
  "domain": "billing",
  "body": "surface Dashboard {\n    facing: Admin\n    context: Workspace\n    exposes: workspace.name, workspace.members\n    provides:\n        invite_member(email) when workspace.seats_available > 0\n    contracts:\n        demands Codec\n    @guarantee DataFreshness\n        -- data reflects state within 30 seconds\n    timeout: session_expiry\n}",
  "description": "data reflects state within 30 seconds",
  "triggers": "",
  "ensures": "",
  "fields": "",
  "references": "Admin, Workspace, Codec, DataFreshness",
  "severity": "",
  "actors": "Admin",
  "version": "2",
  "updated_at": "2026-03-09T12:00:00Z",
  "source_file": "/specs/billing.allium",
  "line_start": 30,
  "line_end": 41
}
```

```json
{
  "id": "proj-acme/billing.allium/contract/Codec",
  "name": "Codec",
  "construct_type": "contract",
  "module": "billing.allium",
  "project": "acme",
  "domain": "billing",
  "body": "contract Codec {\n    serialize: (value: Any) -> ByteArray\n    @invariant Roundtrip\n        -- deserialize(serialize(x)) = x\n}",
  "description": "deserialize(serialize(x)) = x",
  "triggers": "",
  "ensures": "",
  "fields": "serialize",
  "references": "Roundtrip",
  "severity": "",
  "actors": "",
  "version": "2",
  "updated_at": "2026-03-09T12:00:00Z",
  "source_file": "/specs/billing.allium",
  "line_start": 5,
  "line_end": 10
}
```

### 2.3 ID Strategy

The composite ID `{project}/{module}/{construct_type}/{name}` ensures global uniqueness across projects and enables deterministic upserts. When spec files are re-parsed and re-indexed, documents with the same ID are replaced rather than duplicated. The 511-byte primary key limit in Meilisearch is generous enough for this pattern.

### 2.4 Alternative: Multiple Indexes

A multi-index approach (one index per construct type: `allium_entities`, `allium_rules`, `allium_surfaces`, etc.) would allow per-type tuning of searchable attributes and ranking rules. For example, entity search could prioritize field names, while rule search could prioritize triggers and ensures clauses. However, this has drawbacks:

- Cross-type search ("find everything related to User") requires multi-search or federated search.
- More indexes means more task serialization overhead.
- Settings changes trigger full re-indexing per index.

The single-index approach with `construct_type` filtering is simpler and adequate for the expected data volumes. Allium spec corpora are unlikely to exceed thousands of constructs even across many projects, well within Meilisearch's single-index capabilities.

---

## 3. Typo-Tolerant Search for Specs

Typo tolerance is one of Meilisearch's strongest differentiators and it maps well to Allium spec search.

### 3.1 Why It Matters for Specs

Allium uses PascalCase names (`CircuitBreaker`, `RequestPasswordReset`) and snake_case fields (`failure_rate`, `seats_available`). These compound identifiers are easy to misspell. An agent or developer searching for `CircuitBraker` (missing an 'e') or `ResetPaswordToken` (missing a 's') should still find the relevant construct.

Meilisearch's Levenshtein automaton handles this naturally:

- 1 typo allowed for words of 5+ characters
- 2 typos allowed for words of 9+ characters

Examples of typo-tolerant matches:

| Query           | Actual Construct | Typos           | Would Match?                    |
| --------------- | ---------------- | --------------- | ------------------------------- |
| `CircuitBraker` | `CircuitBreaker` | 1 (missing 'e') | Yes (14 chars, 2 allowed)       |
| `pasword`       | `password`       | 1 (missing 's') | Yes (7 chars, 1 allowed)        |
| `UserRequsts`   | `UserRequests`   | 1 (missing 'e') | Yes (11 chars, 2 allowed)       |
| `tokn`          | `token`          | 1 (missing 'e') | No (4 chars, need 5 for 1 typo) |

### 3.2 Lowering the Typo Threshold

Allium construct names tend to be shorter than prose. Setting `OneTypo` to 4 characters (instead of default 5) and `TwoTypos` to 8 (instead of 9) would catch more common misspellings of short domain terms like `User`, `Rule`, `Token`.

### 3.3 When Typo Tolerance Hurts

For highly similar construct names (`UserCreated` vs `UserUpdated`), typo tolerance could surface false positives. Meilisearch's ranking rules handle this: exact matches rank above typo matches. The `exactness` ranking rule ensures that `UserCreated` appears before `UserUpdated` when searching for `UserCreated`.

---

## 4. Faceted Search Across Spec Constructs

Faceted search is where the single-index approach pays off. Agents and developers can explore specs along multiple dimensions simultaneously.

### 4.1 Useful Facets

**construct_type:** Filter to only entities, only rules, only surfaces, etc.

```json
POST /indexes/allium_specs/search
{
    "q": "password",
    "facets": ["construct_type", "domain", "module", "project"]
}
```

Response:

```json
{
    "hits": [ ... ],
    "facetDistribution": {
        "construct_type": {
            "rule": 3,
            "entity": 1,
            "surface": 1,
            "contract": 0
        },
        "domain": {
            "auth": 4,
            "billing": 1
        },
        "module": {
            "auth.allium": 4,
            "billing.allium": 1
        },
        "project": {
            "acme": 5
        }
    }
}
```

This tells the agent: "There are 3 rules, 1 entity, and 1 surface that mention passwords. They are concentrated in the auth domain."

**domain:** User-assigned tags like `auth`, `billing`, `notifications`, `inventory`. Enables domain-scoped exploration.

**module:** The source `.allium` file. Useful for understanding which spec files are relevant to a query.

**project:** For cross-project search, filter to a specific project or see which projects have related specs.

**severity:** For invariants, filter by how critical the constraint is. An agent fixing a bug can focus on `critical` invariants first.

**actors:** For surfaces, filter by which actor type is involved. "Show me all surfaces facing the Admin actor."

### 4.2 Faceted Exploration Workflow

An agent encountering an unfamiliar codebase could use faceted search as an exploration tool:

1. Search `""` (empty query) with `facets: ["domain"]` to see what domains exist.
2. Filter to `domain = 'auth'` with `facets: ["construct_type"]` to see what constructs exist in auth.
3. Filter to `domain = 'auth' AND construct_type = 'rule'` to see all auth rules.
4. Read the returned rule bodies to understand the auth domain's behavioral spec.

This is structured exploration that does not require loading every spec file into context.

---

## 5. Agent Integration: Tend and Weed

### 5.1 Tend Agent: Specification Stewardship

Tend creates, modifies, and restructures `.allium` files. Its key challenge is understanding the impact of changes: "If I modify this entity, what rules and surfaces are affected?"

**Impact analysis via reference search:**

```json
POST /indexes/allium_specs/search
{
    "q": "User",
    "filter": "references = 'User'",
    "facets": ["construct_type"]
}
```

This finds every construct that references the `User` entity. The facet distribution tells Tend how many rules, surfaces, contracts, and invariants are affected by changes to `User`.

**Finding related specs during creation:**

When Tend is creating a new rule for password reset, it can search for existing password-related constructs:

```json
POST /indexes/allium_specs/search
{
    "q": "password reset token",
    "filter": "project = 'acme'"
}
```

This surfaces existing entities, rules, and surfaces that Tend should be aware of before creating new constructs. It prevents Tend from creating duplicate or conflicting specs.

**Detecting naming conflicts:**

Before creating a new construct, Tend can search for existing constructs with the same or similar names:

```json
POST /indexes/allium_specs/search
{
    "q": "PasswordResetRequest",
    "filter": "construct_type = 'rule' AND project = 'acme'"
}
```

Typo tolerance helps here: if a `PasswordResetRequest` rule already exists and Tend tries to create `PasswordResetReqeust` (typo), the search catches the near-duplicate.

### 5.2 Weed Agent: Drift Detection

Weed compares `.allium` specs against implementation code. Its core operation is finding specs that are relevant to a given piece of code.

**Finding specs for a code change:**

When a developer modifies a function related to invoice processing, Weed needs to find all specs that govern invoice behavior:

```json
POST /indexes/allium_specs/search
{
    "q": "invoice payment processing",
    "filter": "domain = 'billing'",
    "facets": ["construct_type"]
}
```

**Detecting conflicting specs:**

Weed's most valuable capability is finding contradictions. If one rule says "locked users cannot reset passwords" and another rule says "all users can reset passwords," Weed needs to find both.

Search alone does not detect logical contradictions. But it dramatically narrows the search space. Weed can:

1. Search for all rules with related triggers: `"q": "UserRequestsPasswordReset"`.
2. Search for all invariants referencing the entity: `"q": "User", "filter": "construct_type = 'invariant'"`.
3. Load only those constructs into LLM context for contradiction analysis.

This is the key insight: Meilisearch does not replace the LLM's reasoning, it makes the LLM's reasoning tractable by selecting the right specs to reason about.

**Drift reporting with provenance:**

When Weed detects drift, it can include the spec's `source_file`, `line_start`, and `line_end` in its report, giving developers exact locations to examine:

```
DRIFT DETECTED:
  Code: POST /api/users/:id/reset-password allows suspended users
  Spec: RequestPasswordReset (auth.allium:45-53)
        requires: user.status in {active, locked}
  Classification: code bug (spec excludes suspended users)
```

---

## 6. Cross-Project Spec Discovery

### 6.1 The Reuse Opportunity

Allium envisions reusable spec libraries: OAuth flows, payment processing, email delivery patterns. These are behavioral patterns that repeat across projects. Currently, discovering them requires knowing they exist. Meilisearch enables discovery by content similarity.

**Finding existing patterns:**

A developer starting to spec out OAuth for a new project can search across all indexed projects:

```json
POST /indexes/allium_specs/search
{
    "q": "OAuth authorization token refresh",
    "facets": ["project"]
}
```

The response shows which projects have OAuth-related specs and what they look like. The developer can evaluate whether to `use` an existing spec or write a new one.

**Finding similar entities:**

```json
POST /indexes/allium_specs/search
{
    "q": "circuit breaker failure threshold",
    "filter": "construct_type = 'entity'",
    "facets": ["project"]
}
```

If three projects have a `CircuitBreaker` entity, that is a strong signal it should be extracted into a shared library spec.

### 6.2 Cross-Project Multi-Search

Meilisearch's federated search could merge results from a project-specific index and a shared-library index into a single ranked list, ensuring that library specs appear alongside project-specific ones:

```json
POST /multi-search
{
    "queries": [
        {
            "indexUid": "allium_specs",
            "q": "rate limiting",
            "filter": "project = 'acme'"
        },
        {
            "indexUid": "allium_specs",
            "q": "rate limiting",
            "filter": "project = 'shared-libraries'"
        }
    ]
}
```

### 6.3 Behavioral Pattern Fingerprinting

A more advanced use case: index not just the raw spec text but also normalized "behavioral fingerprints." Two rules that both follow the pattern "check precondition on entity status, then create a token, then send a notification" have similar structure regardless of their domain. Extracting and indexing these patterns would enable true behavioral similarity search.

This goes beyond what Meilisearch's keyword search can do natively but could be achieved with hybrid search. Configure an embedder to generate semantic vectors from spec bodies, then search with `semanticRatio: 0.7` to find structurally similar specs even when they use different domain terminology.

---

## 7. The 10-Word Query Limit

### 7.1 The Constraint

Meilisearch silently drops words beyond the 10th in a query. This is Meilisearch's most important limitation for LLM-driven search.

LLM agents tend to generate verbose, natural-language queries: "Find all rules that govern the behavior of the User entity when a password reset is requested by an active or locked user." That is 23 words. Meilisearch would process only: "Find all rules that govern the behavior of the User entity" (10 words), dropping the most specific part of the query.

### 7.2 Mitigation Strategies

**Strategy 1: Query preprocessing in hive-server.**

Before forwarding a query to Meilisearch, hive-server extracts key terms. A simple heuristic: remove stopwords ("the", "a", "an", "is", "that", "of", "by"), articles, and common verbs ("find", "show", "get"). The example above reduces to: "rules govern behavior User entity password reset requested active locked" -- still 10 words, but now the right 10.

```go
func preprocessQuery(raw string) string {
    stopwords := map[string]bool{
        "find": true, "show": true, "get": true, "all": true,
        "the": true, "a": true, "an": true, "is": true,
        "that": true, "of": true, "by": true, "when": true,
        "are": true, "for": true, "with": true, "from": true,
    }
    words := strings.Fields(raw)
    var kept []string
    for _, w := range words {
        if !stopwords[strings.ToLower(w)] {
            kept = append(kept, w)
        }
    }
    if len(kept) > 10 {
        kept = kept[:10]
    }
    return strings.Join(kept, " ")
}
```

**Strategy 2: Structured queries using filters.**

Instead of cramming everything into the `q` parameter, decompose the query:

```json
{
  "q": "password reset token",
  "filter": "construct_type = 'rule' AND domain = 'auth' AND references = 'User'"
}
```

Now the query is 3 words and the constraints are in the filter (which has no word limit). This is the better approach and should be the default for agent-generated queries.

**Strategy 3: Multiple targeted searches.**

Instead of one broad query, issue multiple narrow searches via multi-search:

```json
POST /multi-search
{
    "queries": [
        {
            "indexUid": "allium_specs",
            "q": "password reset",
            "filter": "construct_type = 'rule'"
        },
        {
            "indexUid": "allium_specs",
            "q": "User status locked active",
            "filter": "construct_type = 'entity'"
        },
        {
            "indexUid": "allium_specs",
            "q": "ResetToken",
            "filter": "construct_type = 'entity'"
        }
    ]
}
```

Three focused queries, each under 10 words, that together cover the original intent.

**Strategy 4: Hybrid/semantic search.**

If an embedder is configured, semantic search bypasses the 10-word tokenization limit entirely. The entire query text is embedded as a vector and matched against document embeddings. Set `semanticRatio: 1.0` for fully semantic search on verbose agent queries.

### 7.3 Recommendation

Combine strategies 1, 2, and 4. Have hive-server expose a spec-search endpoint that accepts both a natural-language query and structured filters. Preprocess the query text to extract key terms. Use hybrid search with a moderate semantic ratio (0.5-0.7) to catch both keyword and meaning matches.

---

## 8. LLM Context Loading via Search

### 8.1 The Core Value Proposition

This is the single most impactful use case. Today, when an LLM agent needs to work with Allium specs, it has two options:

1. **Load all specs.** Reliable but token-expensive. A project with 20 spec files averaging 200 lines each is 4,000 lines of spec text. At roughly 1.3 tokens per word and 8 words per line, that is 40,000+ tokens just for context.

2. **Load specs by filename.** Requires the agent to know which files are relevant, which is the problem spec search is supposed to solve.

Meilisearch enables a third option: **load only the relevant constructs.** An agent working on a password reset feature searches for "password reset" and loads only the 3-5 matching constructs (maybe 50 lines total) instead of 4,000 lines.

### 8.2 Token Savings Estimate

| Approach          | Specs Loaded     | Est. Tokens | Relevance                          |
| ----------------- | ---------------- | ----------- | ---------------------------------- |
| Load all          | 100 constructs   | 40,000      | Low (most are noise)               |
| Load by file      | 15-20 constructs | 6,000-8,000 | Medium (file-level granularity)    |
| Search by content | 3-5 constructs   | 1,200-2,000 | High (construct-level granularity) |

The search-based approach uses 95-97% fewer tokens than loading all specs and delivers higher relevance because every loaded construct was matched by the search.

### 8.3 Workflow

1. Agent receives a task: "Implement password reset endpoint."
2. Agent queries hive-server: `GET /api/v1/specs/search?q=password+reset&construct_type=rule,entity`
3. hive-server queries Meilisearch, returns matching constructs with their full `body` text.
4. Agent loads the 4 matching constructs (rule `RequestPasswordReset`, entity `User`, entity `ResetToken`, surface `AuthAPI`) into its context.
5. Agent implements the endpoint with full behavioral awareness, using 2,000 tokens of spec context instead of 40,000.

### 8.4 Progressive Disclosure

Search enables a "drill-down" pattern:

1. Start with a broad search to understand the landscape.
2. Use facets to narrow to the relevant domain.
3. Load specific constructs.
4. Follow references to load dependent constructs.

Step 4 is key: once the agent loads the `RequestPasswordReset` rule and sees it references `User` and `ResetToken`, it can issue targeted searches for those entities. This reference-following behavior builds a minimal but complete context window.

---

## 9. hive-server as Mediator

### 9.1 Architecture

hive-server sits between agents and both SQLite (primary data) and Meilisearch (search index). For Allium spec search, the flow is:

```
.allium files (git) --> allium-cli (parse to AST JSON) --> hive-server (ingest)
                                                               |
                                                               v
                                                     +---------+---------+
                                                     |                   |
                                                     v                   v
                                                  SQLite            Meilisearch
                                              (spec metadata,    (full-text search
                                               version history)    index)
```

### 9.2 Ingestion Pipeline

hive-server would expose an endpoint for spec ingestion. The Tend agent or a CI pipeline calls it after modifying `.allium` files:

```
POST /api/v1/specs/ingest
Content-Type: application/json
X-Agent-ID: tend-agent

{
    "project": "acme",
    "source_file": "/specs/auth.allium",
    "ast": { ... },  // JSON AST from allium-cli
    "raw": "-- allium: 2\n\nentity User { ... }"
}
```

hive-server would:

1. Parse the AST JSON to extract individual constructs.
2. Store metadata in SQLite (file path, project, version, timestamp).
3. Index each construct as a document in Meilisearch.

The AST JSON from `allium-cli` provides the structured data needed to populate the index fields (name, construct type, fields, references, etc.) without hive-server needing to parse `.allium` syntax itself.

### 9.3 Search API

```
GET /api/v1/specs/search?q=password+reset&construct_type=rule&domain=auth&project=acme
```

Or a POST for complex queries:

```
POST /api/v1/specs/search
{
    "q": "password reset token",
    "filters": {
        "construct_type": ["rule", "entity"],
        "domain": "auth",
        "project": "acme"
    },
    "facets": ["construct_type", "domain", "module"],
    "limit": 10,
    "include_body": true
}
```

hive-server translates this to a Meilisearch query, applies agent-scoping via `X-Agent-ID`, preprocesses the query text, and returns results with spec bodies suitable for LLM context loading.

### 9.4 Response Shape

```json
{
  "hits": [
    {
      "id": "proj-acme/auth.allium/rule/RequestPasswordReset",
      "name": "RequestPasswordReset",
      "construct_type": "rule",
      "module": "auth.allium",
      "body": "rule RequestPasswordReset { ... }",
      "source_file": "/specs/auth.allium",
      "line_start": 45,
      "line_end": 53,
      "score": 0.95,
      "references": ["User", "ResetToken"]
    }
  ],
  "facets": {
    "construct_type": { "rule": 3, "entity": 1 },
    "domain": { "auth": 4 }
  },
  "query": "password reset token",
  "processing_time_ms": 8,
  "total_hits": 4
}
```

### 9.5 Sync Strategy

**On spec change (primary path):** When Tend modifies a spec file, it calls the ingest endpoint. hive-server re-parses and re-indexes the affected constructs. Old constructs from the same file are deleted and replaced.

**Periodic full sync (reconciliation):** A background job periodically re-reads all spec files from their source locations, re-parses them, and re-indexes everything. This catches cases where specs were modified outside of Tend (e.g., manual edits, git merges). Uses Meilisearch's index swap for zero-downtime re-indexing.

**Deletion handling:** When a spec file is removed or a construct is deleted from a file, the sync job detects the missing IDs and issues `DeleteDocumentsByFilter("module = 'deleted-file.allium' AND project = 'acme'")`.

---

## 10. Tradeoffs

### 10.1 What Is Gained

1. **Sub-50ms spec search.** Agents and developers find relevant specs instantly instead of scanning files.

2. **Typo-tolerant discovery.** Misspelled construct names still find the right specs. Important for agents that hallucinate names.

3. **Faceted exploration.** Navigate a spec corpus by domain, construct type, module, and project without loading everything.

4. **95%+ token savings.** Load 3-5 relevant constructs instead of 100+. This directly translates to faster agent responses and lower API costs.

5. **Cross-project visibility.** Find reusable patterns and avoid reinventing behavioral specs that already exist elsewhere.

6. **Impact analysis for Tend.** Before modifying a construct, search for everything that references it.

7. **Targeted drift detection for Weed.** Search for specs related to a code change instead of comparing all specs.

8. **Provenance in results.** Source file paths and line numbers enable precise navigation from search results to spec files.

### 10.2 What Is Lost

1. **Simplicity.** Allium's value proposition includes "just flat files." Adding a search index, an ingestion pipeline, and a sync strategy adds significant infrastructure complexity.

2. **Self-containedness.** Allium specs are currently self-contained in git. Adding Meilisearch means the spec search capability depends on a running service, a populated index, and a sync mechanism.

3. **Consistency guarantees.** The Meilisearch index can drift from the actual `.allium` files. A developer edits a spec, the sync fails silently, and agents search stale data. The Meilisearch write model is async, so even successful writes have a brief consistency window.

4. **Semantic search limitations.** Meilisearch does keyword search (and optionally hybrid search). It cannot answer "which rules contradict each other" or "which entities have overlapping state machines." Those require LLM reasoning, not search.

5. **Logical querying is not search.** "Find all rules that affect entity X" is a graph traversal, not a text search. The `references` field approximates this, but a reference search for "User" also matches rules that mention "UserPreference" or "UserSession" in their body text. Meilisearch has no concept of structured references.

### 10.3 Complexity Added

| Component                | New Complexity                                           |
| ------------------------ | -------------------------------------------------------- |
| Meilisearch deployment   | New service to deploy, monitor, backup                   |
| AST-to-document pipeline | Code to parse allium-cli output into Meilisearch docs    |
| Sync mechanism           | Ingestion endpoint + periodic reconciliation job         |
| Query preprocessing      | Stopword removal, query decomposition for 10-word limit  |
| hive-server endpoints    | New search API routes, request/response types            |
| Index management         | Settings configuration, schema evolution, re-indexing    |
| Testing                  | Meilisearch mocks for unit tests, integration test setup |

Estimated integration effort: 7-10 days, building on the 5-7 day estimate from the Meilisearch brief (which covers general hive-server integration) plus the Allium-specific ingestion pipeline and search API.

### 10.4 When Not to Use Search

- **Small projects (<5 spec files):** Just load them all. The overhead of search is not justified.
- **Single-agent workflows:** If only one agent works on specs and it is already familiar with the codebase, search adds latency without benefit.
- **Logical queries:** "Do any two rules have contradictory preconditions?" This is not a search problem. Load specs into the LLM and ask it.
- **Version comparison:** "What changed in this spec since last week?" This is a git problem, not a search problem.

---

## 11. Recommendations

### 11.1 Start Narrow

Do not index every Allium construct on day one. Start with:

1. **Rules and entities only.** These are the most commonly searched constructs and the most valuable for agent context loading.
2. **Single-project scope.** Index one project's specs before tackling cross-project discovery.
3. **Keyword search only.** Skip hybrid/semantic search initially. It adds embedder complexity and cost.

### 11.2 Make Search Optional

Search should enhance the Allium workflow, not become a dependency. If Meilisearch is down, agents should fall back to loading spec files directly (the current behavior). This means:

- The hive-server search endpoint returns graceful errors when Meilisearch is unavailable.
- Agent prompts include fallback instructions: "If spec search is unavailable, load all .allium files from the specs directory."

### 11.3 Let the AST Do the Heavy Lifting

The `allium-cli` Rust parser already produces typed AST JSON output. hive-server should consume this JSON directly rather than parsing `.allium` syntax in Go. The AST provides precise construct boundaries, field names, references, and types -- exactly the structured data needed for the Meilisearch index.

### 11.4 Invest in Query Preprocessing

The 10-word query limit is the biggest friction point for LLM agents. Build robust query preprocessing into hive-server from the start:

1. Stopword removal.
2. Structured filter extraction (pull out construct type, domain, and project from natural language and move them to filter parameters).
3. Query decomposition (split compound queries into multiple targeted searches).

This is where hive-server's mediator role adds the most value -- translating agent intent into optimized Meilisearch queries.

### 11.5 Plan for Hybrid Search Later

Once the keyword search pipeline is stable, adding an embedder for semantic search is a natural evolution. This would enable:

- Behavioral similarity search across projects.
- Natural-language queries without keyword extraction.
- Finding specs related by meaning even when they use different terminology.

The index schema proposed above is compatible with hybrid search. Adding an embedder only requires a settings update and a re-index.

---

## Sources

- [Allium Technology Brief](/Users/shakefu/git/christmas-island/hive-server/context/allium.md)
- [Meilisearch Technology Brief](/Users/shakefu/git/christmas-island/hive-server/context/meilisearch.md)
- [Allium Language Reference (JUXT)](https://github.com/juxt/allium) -- MIT license, 2026
- [Meilisearch Known Limitations](https://www.meilisearch.com/docs/learn/resources/known_limitations)
- [Meilisearch Hybrid Search](https://www.meilisearch.com/solutions/hybrid-search)
