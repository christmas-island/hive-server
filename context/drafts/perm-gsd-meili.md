# Meilisearch as a Search Layer for GSD -- Permutation Analysis

**Purpose:** Deep evaluation of how Meilisearch could enhance GSD's filesystem-based
planning documents through full-text search, faceted filtering, and cross-project
knowledge retrieval, mediated by hive-server.

**Date:** 2026-03-09

---

## Table of Contents

1. [The Problem: GSD's Documents Are Write-Only Knowledge](#1-the-problem)
2. [What Gets Indexed: GSD Document Inventory](#2-what-gets-indexed)
3. [Index Schema Proposals](#3-index-schema-proposals)
4. [Search Scenarios and Query Examples](#4-search-scenarios)
5. [Typo Tolerance for Agent Queries](#5-typo-tolerance)
6. [Faceted Search Across Projects](#6-faceted-search)
7. [The 10-Word Query Limit and LLM Search Behavior](#7-the-10-word-limit)
8. [Async Indexing vs Filesystem Writes](#8-async-indexing)
9. [Cross-Project Search: Patterns, Decisions, Solutions](#9-cross-project-search)
10. [hive-server as Mediator](#10-hive-server-as-mediator)
11. [From Prompt Documents to Searchable Knowledge](#11-searchable-knowledge)
12. [Tradeoffs](#12-tradeoffs)
13. [Practical Integration Path](#13-practical-integration)
14. [Open Questions](#14-open-questions)

---

## 1. The Problem: GSD's Documents Are Write-Only Knowledge

GSD generates substantial structured knowledge over a project's lifetime:
REQUIREMENTS.md files with categorized requirement IDs, RESEARCH.md files with domain
analysis, PLAN.md files with XML-structured tasks, SUMMARY.md files with execution
outcomes, VERIFICATION.md files with test results, and PROJECT.md with accumulated
decisions. Across a multi-phase project, this easily reaches dozens of documents and
hundreds of kilobytes of structured text.

None of it is searchable. GSD's persistence model is explicitly described as having
**"no querying, no indexing, no search"**. The only retrieval mechanism is file reads
and grep. An agent resuming work on phase 7 has no way to ask "what authentication
decisions were made in phase 2?" without reading every file sequentially, burning
context tokens on potentially irrelevant content.

This matters because GSD's entire architecture is built around context economy -- fresh
200k-token windows per sub-agent, 100-line STATE.md cap, recommendations to `/clear`
between commands. The system works hard to keep context lean but provides no mechanism
to selectively retrieve past knowledge. The result: agents either re-read everything
(burning the context budget GSD carefully protects) or miss relevant prior work.

Meilisearch could turn this accumulated planning knowledge into an instantly queryable
corpus, giving agents targeted retrieval instead of exhaustive file scanning.

---

## 2. What Gets Indexed: GSD Document Inventory

Not all GSD documents are equal candidates for indexing. Here is a classification:

### High-Value Index Targets

| Document           | Content Type                       | Why Index It                                                                       |
| ------------------ | ---------------------------------- | ---------------------------------------------------------------------------------- |
| `REQUIREMENTS.md`  | Structured requirements with IDs   | Cross-reference requirements by keyword; find related requirements across projects |
| `PROJECT.md`       | Vision, constraints, key decisions | Decision archaeology -- find past reasoning for architectural choices              |
| `RESEARCH.md`      | Domain research findings           | Reuse research across phases; avoid duplicate investigation                        |
| `XX-YY-SUMMARY.md` | Execution outcomes per plan        | Find how similar problems were solved; learn from past failures                    |
| `VERIFICATION.md`  | Test results and failure modes     | Search for past failure patterns; find verification approaches                     |
| `CONTEXT.md`       | User preferences per phase         | Retrieve user decisions without re-asking                                          |

### Medium-Value Index Targets

| Document     | Content Type                 | Why Index It                                                    |
| ------------ | ---------------------------- | --------------------------------------------------------------- |
| `ROADMAP.md` | Phase breakdown and progress | Cross-project phase comparison; find similar project structures |
| `STATE.md`   | Current position and metrics | Primarily useful for active state, less for historical search   |

### Low-Value Index Targets

| Document            | Content Type         | Why Less Useful                                                       |
| ------------------- | -------------------- | --------------------------------------------------------------------- |
| `XX-YY-PLAN.md`     | XML task definitions | Plans are prompts, not knowledge; they are consumed once by executors |
| `config.json`       | Workflow settings    | Structured data better served by direct reads                         |
| `.continue-here.md` | Session resume state | Ephemeral; relevant only to next immediate session                    |

The high-value documents share a characteristic: they contain decisions, findings, and
outcomes that remain relevant long after the phase that produced them. Plans, by
contrast, are consumed once and their value is captured in summaries.

---

## 3. Index Schema Proposals

### 3.1 Index: `gsd_requirements`

Indexes individual requirements extracted from REQUIREMENTS.md files. Each requirement
becomes its own document rather than indexing the entire file, enabling per-requirement
search and filtering.

```json
{
  "uid": "gsd_requirements",
  "primaryKey": "id",
  "document_example": {
    "id": "myproject__AUTH-01",
    "project": "myproject",
    "requirement_id": "AUTH-01",
    "category": "Authentication",
    "text": "User can sign up with email and password",
    "status": "complete",
    "version": "v1",
    "phase_refs": ["02-auth-setup", "03-auth-flows"],
    "milestone": "v1.0",
    "created_at": "2026-02-15T10:00:00Z",
    "updated_at": "2026-03-01T14:30:00Z"
  },
  "settings": {
    "searchableAttributes": ["text", "requirement_id", "category"],
    "filterableAttributes": [
      "project",
      "category",
      "status",
      "version",
      "milestone"
    ],
    "sortableAttributes": ["created_at", "updated_at", "requirement_id"],
    "synonyms": {
      "auth": ["authentication", "login", "signin"],
      "signup": ["registration", "register", "create account"],
      "api": ["endpoint", "route", "rest"]
    }
  }
}
```

**Extraction logic:** Parse REQUIREMENTS.md for lines matching
`- [x] **ID**: description` or `- [ ] **ID**: description`. The checkbox state maps
to `status` (complete/incomplete). The heading hierarchy provides `category` and
`version`. The traceability section at the bottom of REQUIREMENTS.md provides
`phase_refs`.

**Composite primary key:** `{project}__{requirement_id}` ensures uniqueness across
projects while keeping IDs human-readable. The double underscore delimiter avoids
collision with requirement ID formats (which use single hyphens).

### 3.2 Index: `gsd_decisions`

Indexes decisions from PROJECT.md and CONTEXT.md. Decisions are the highest-value
knowledge GSD produces -- they capture _why_ something was done a certain way.

```json
{
  "uid": "gsd_decisions",
  "primaryKey": "id",
  "document_example": {
    "id": "myproject__dec-007",
    "project": "myproject",
    "decision": "Use JWT tokens with 15-minute expiry and refresh token rotation",
    "rationale": "Short-lived tokens reduce attack window; rotation prevents replay attacks",
    "context": "Authentication system design for multi-device support",
    "phase": "02-auth-setup",
    "source_file": "PROJECT.md",
    "decided_at": "2026-02-20T09:00:00Z",
    "tags": ["security", "authentication", "jwt"]
  },
  "settings": {
    "searchableAttributes": ["decision", "rationale", "context", "tags"],
    "filterableAttributes": ["project", "phase", "source_file", "tags"],
    "sortableAttributes": ["decided_at"]
  }
}
```

**Extraction challenge:** Decisions in PROJECT.md are semi-structured. They appear
under `## Key Decisions` headings as bullet points or short paragraphs. Extraction
requires pattern matching for decision-like statements (lines containing "decided",
"chose", "using X instead of Y", "will use", etc.) or parsing the explicit decision
log format if GSD templates are followed. This is the most fragile extraction target
and would benefit from a structured output format in GSD's templates.

### 3.3 Index: `gsd_research`

Indexes research findings from RESEARCH.md files and the `research/` directory.

```json
{
  "uid": "gsd_research",
  "primaryKey": "id",
  "document_example": {
    "id": "myproject__02__oauth-providers",
    "project": "myproject",
    "phase": "02-auth-setup",
    "title": "OAuth Provider Comparison",
    "content": "Evaluated Auth0, Clerk, and Supabase Auth. Auth0 has the most mature...",
    "findings": [
      "Auth0 supports RBAC natively",
      "Clerk has better DX for React"
    ],
    "source_file": "phases/02-auth-setup/RESEARCH.md",
    "researcher": "gsd-phase-researcher",
    "created_at": "2026-02-18T11:00:00Z",
    "tags": ["oauth", "authentication", "third-party"]
  },
  "settings": {
    "searchableAttributes": ["title", "content", "findings", "tags"],
    "filterableAttributes": ["project", "phase", "researcher", "tags"],
    "sortableAttributes": ["created_at"]
  }
}
```

**Extraction approach:** Research documents are less structured than requirements.
The entire RESEARCH.md file could be indexed as a single document per section
(splitting on `##` headings), or as one document per file. Splitting on headings
provides better granularity for search results -- a search for "OAuth" returns the
specific section, not the entire research document.

### 3.4 Index: `gsd_summaries`

Indexes execution summaries -- the outcomes of completed plans.

```json
{
  "uid": "gsd_summaries",
  "primaryKey": "id",
  "document_example": {
    "id": "myproject__02-01",
    "project": "myproject",
    "phase": "02-auth-setup",
    "plan": "02-01",
    "title": "Create authentication middleware",
    "outcome": "Implemented JWT validation middleware with role-based access control",
    "files_changed": [
      "src/middleware/auth.ts",
      "src/types/auth.ts",
      "tests/auth.test.ts"
    ],
    "issues_encountered": "Initial implementation failed with async token verification; switched to sync validation with cached keys",
    "status": "complete",
    "duration_minutes": 12,
    "completed_at": "2026-02-22T16:00:00Z",
    "tags": ["middleware", "jwt", "authentication"]
  },
  "settings": {
    "searchableAttributes": [
      "title",
      "outcome",
      "issues_encountered",
      "files_changed",
      "tags"
    ],
    "filterableAttributes": ["project", "phase", "status", "tags"],
    "sortableAttributes": ["completed_at", "duration_minutes"]
  }
}
```

**Why index `issues_encountered`:** This is where the real reusable knowledge lives.
When a future task encounters a similar problem, searching summaries for past issues
surfaces solutions that would otherwise be buried in an old SUMMARY.md file nobody
reads. An agent debugging async token verification could find this summary and
immediately learn the resolution.

### 3.5 Index: `gsd_documents` (Catch-All)

For documents that do not fit the specialized schemas above, or for initial
implementation before investing in per-type extraction. Indexes entire documents or
document sections as generic content.

```json
{
  "uid": "gsd_documents",
  "primaryKey": "id",
  "document_example": {
    "id": "myproject__phases__02__VERIFICATION",
    "project": "myproject",
    "phase": "02-auth-setup",
    "doc_type": "verification",
    "title": "Phase 02 Verification Results",
    "content": "Full markdown content of the file...",
    "source_path": ".planning/phases/02-auth-setup/VERIFICATION.md",
    "updated_at": "2026-02-23T10:00:00Z"
  },
  "settings": {
    "searchableAttributes": ["title", "content"],
    "filterableAttributes": ["project", "phase", "doc_type"],
    "sortableAttributes": ["updated_at"]
  }
}
```

This is the pragmatic starting point. It requires no intelligent extraction -- just
file reading, basic metadata extraction from path conventions, and full-content
indexing. It sacrifices granularity (you get the whole file, not the specific section)
for implementation simplicity.

---

## 4. Search Scenarios and Query Examples

### 4.1 Requirement Discovery

**Scenario:** An agent planning phase 5 (payment integration) needs to check whether
any existing requirements mention Stripe, payment processing, or billing.

```go
// Multi-index search: check requirements AND research
results, _ := client.MultiSearch(&meilisearch.MultiSearchRequest{
    Queries: []meilisearch.SearchRequest{
        {
            IndexUID: "gsd_requirements",
            Query:    "payment processing billing",
            Filter:   "project = 'myproject'",
            Limit:    10,
        },
        {
            IndexUID: "gsd_research",
            Query:    "payment stripe billing",
            Filter:   "project = 'myproject'",
            Limit:    5,
        },
    },
})
```

**What this finds:** Requirements like `PAY-01: User can add a credit card` and
research findings about payment provider evaluations. Without search, the agent would
need to read REQUIREMENTS.md (maybe finding it), plus scan all RESEARCH.md files
across phases (unlikely to happen).

### 4.2 Decision Archaeology

**Scenario:** An agent is implementing a caching layer and wants to know if any prior
decisions were made about caching strategy, TTL values, or cache invalidation.

```go
result, _ := client.Index("gsd_decisions").Search("cache invalidation TTL", &meilisearch.SearchRequest{
    Filter: "project = 'myproject'",
    Limit:  10,
    AttributesToHighlight: []string{"decision", "rationale"},
})
```

**What this finds:** A decision from phase 3 that said "Use Redis with 5-minute TTL
for session data; invalidate on password change." Without search, this decision is
buried in PROJECT.md under a growing list of accumulated decisions. An agent would
either re-read the entire PROJECT.md (burning tokens on unrelated decisions) or miss
the decision entirely and potentially contradict it.

### 4.3 Failure Pattern Search

**Scenario:** A verifier agent encounters a test timeout. Before debugging from
scratch, it searches for similar past failures.

```go
result, _ := client.Index("gsd_summaries").Search("test timeout async", &meilisearch.SearchRequest{
    Filter: "project = 'myproject' AND status = 'complete'",
    Limit:  5,
    AttributesToRetrieve: []string{"title", "issues_encountered", "plan", "phase"},
})
```

**What this finds:** A summary from phase 2 that documented "Initial implementation
failed with async token verification" and its resolution. The debugger agent gets
immediate context on how a similar class of problem was resolved, potentially
shortening debug time from minutes to seconds.

### 4.4 Cross-Project Pattern Reuse

**Scenario:** Starting a new project that needs OAuth integration. Search across all
projects for prior OAuth work.

```go
results, _ := client.MultiSearch(&meilisearch.MultiSearchRequest{
    Queries: []meilisearch.SearchRequest{
        {
            IndexUID: "gsd_research",
            Query:    "OAuth provider integration",
            Limit:    10,
            // No project filter -- search across all projects
        },
        {
            IndexUID: "gsd_summaries",
            Query:    "OAuth implementation",
            Limit:    10,
        },
        {
            IndexUID: "gsd_decisions",
            Query:    "OAuth authentication provider",
            Limit:    10,
        },
    },
})
```

**What this finds:** Research from three different projects comparing OAuth providers,
summaries documenting how OAuth was integrated in each, and decisions explaining why
specific providers were chosen. This is knowledge that currently dies with each
project's `.planning/` directory.

---

## 5. Typo Tolerance for Agent Queries

Meilisearch's typo tolerance uses a prefix Levenshtein automaton: 1 typo allowed for
words with 5+ characters, 2 typos for 9+ characters. This matters for GSD in two
distinct ways.

### 5.1 LLM-Generated Queries

LLMs generally produce correctly-spelled text, so typo tolerance might seem
unnecessary. But consider:

- **Requirement IDs are not natural language.** An agent searching for `AUTH-01` might
  generate `AUT-01` or `ATUH-01`. Typo tolerance catches this.
- **Technical terms vary.** "Kubernetes" vs "kuberentes", "middleware" vs "midleware".
  LLMs are good at spelling but not perfect, especially with technical jargon from
  less-represented domains.
- **User-originated content.** Requirements and decisions often preserve user phrasing.
  If a user typed "authetication" in a requirement, an LLM searching for
  "authentication" would miss it without typo tolerance.
- **Cross-language terms.** Project names, library names, and domain-specific terms
  (e.g., "meilisearch" itself) benefit from fuzzy matching.

### 5.2 Human-Initiated Searches

If hive-server exposes a search endpoint that humans can also query (via a CLI or web
interface), typo tolerance becomes immediately valuable. Humans searching for
"requirments" or "verifcation" still get results.

### 5.3 Synonym Configuration for GSD Domain

Beyond typo tolerance, synonyms provide domain-specific equivalence:

```go
synonyms := map[string][]string{
    "requirement":  {"req", "spec", "specification"},
    "phase":        {"milestone", "sprint", "iteration"},
    "plan":         {"task", "action", "work item"},
    "verification": {"test", "check", "validation"},
    "blocker":      {"impediment", "issue", "problem"},
    "decision":     {"choice", "ruling", "determination"},
    "research":     {"investigation", "analysis", "study"},
    "executor":     {"implementer", "developer", "builder"},
    "greenfield":   {"new project", "from scratch"},
    "brownfield":   {"existing codebase", "legacy"},
}
```

This compensates for vocabulary variance between different agents, different users, and
different projects. One project's REQUIREMENTS.md might say "specification" where
another says "requirement" -- synonyms bridge that gap.

---

## 6. Faceted Search Across Projects

Faceted search lets agents narrow results without knowing exact filter values in
advance. Instead of constructing a precise filter, the agent requests facet
distributions and then filters based on what exists.

### 6.1 Requirement Facets

```go
result, _ := client.Index("gsd_requirements").Search("user authentication", &meilisearch.SearchRequest{
    Facets: []string{"project", "category", "status", "version"},
})

// Response includes:
// "facetDistribution": {
//   "project": {"myproject": 3, "other-project": 7, "client-app": 2},
//   "category": {"Authentication": 8, "Authorization": 3, "User Management": 1},
//   "status": {"complete": 5, "incomplete": 7},
//   "version": {"v1": 9, "v2": 3}
// }
```

An agent sees that 7 matching requirements exist in "other-project", 3 of which are in
"Authorization" (not just "Authentication"). It can then refine:

```go
result, _ := client.Index("gsd_requirements").Search("user authentication", &meilisearch.SearchRequest{
    Filter: "project = 'other-project' AND category = 'Authorization'",
    Limit:  10,
})
```

This two-step pattern -- broad faceted query, then filtered refinement -- is natural
for LLM agents that can reason about intermediate results.

### 6.2 Summary Facets for Project Health

```go
result, _ := client.Index("gsd_summaries").Search("", &meilisearch.SearchRequest{
    Filter: "project = 'myproject'",
    Facets: []string{"phase", "status"},
})

// "facetDistribution": {
//   "phase": {"01-setup": 3, "02-auth": 4, "03-api": 2, "04-frontend": 0},
//   "status": {"complete": 7, "failed": 1, "in_progress": 1}
// }
```

This gives a project health overview without reading STATE.md or ROADMAP.md. The agent
instantly sees that phase 03 has 2 plans, one failed, and can investigate.

### 6.3 Cross-Project Phase Comparison

```go
result, _ := client.Index("gsd_summaries").Search("", &meilisearch.SearchRequest{
    Facets: []string{"project"},
    Filter: "status = 'failed'",
})

// "facetDistribution": {
//   "project": {"myproject": 1, "other-project": 5, "client-app": 0}
// }
```

Five failures in "other-project" compared to one in "myproject" -- a signal that
"other-project" may have systemic issues worth investigating before adopting similar
patterns.

---

## 7. The 10-Word Query Limit and LLM Search Behavior

Meilisearch silently drops query terms beyond the 10th word. This is a significant
constraint for LLM-generated queries, which tend toward verbose natural language.

### 7.1 The Problem

An LLM generating a search query might produce:

```
"how was the authentication middleware implemented in the second phase of the project"
```

This is 14 words. Meilisearch sees:

```
"how was the authentication middleware implemented in the second phase"
```

"of", "the", and "project" are dropped. With stop words configured ("how", "was",
"the", "in"), the effective query is:

```
"authentication middleware implemented second phase"
```

Which is actually a pretty good query. Stop words interact favorably with the 10-word
limit -- they are removed before counting (if configured), leaving room for meaningful
terms.

### 7.2 Mitigation Strategies

**Strategy 1: Stop word configuration.** Configure generous stop words so that LLM
verbosity does not consume the 10-word budget:

```go
stopWords := []string{
    "the", "a", "an", "is", "are", "was", "were", "be", "been",
    "being", "have", "has", "had", "do", "does", "did", "will",
    "would", "could", "should", "may", "might", "can", "shall",
    "of", "in", "to", "for", "with", "on", "at", "from", "by",
    "about", "into", "through", "during", "before", "after",
    "how", "what", "which", "who", "where", "when", "why",
    "this", "that", "these", "those", "it", "its",
}
```

This list is aggressive. With these stop words, most LLM-generated queries reduce to
their meaningful content terms before hitting the 10-word limit.

**Strategy 2: Query preprocessing in hive-server.** Before forwarding to Meilisearch,
hive-server extracts key terms:

```go
func preprocessQuery(rawQuery string) string {
    // Option A: Simple -- remove stop words, take first 10 terms
    terms := removeStopWords(rawQuery)
    if len(terms) > 10 {
        terms = terms[:10]
    }
    return strings.Join(terms, " ")

    // Option B: Smarter -- use the LLM to extract search terms
    // This adds latency and cost, but produces better queries.
    // Only worth it if search quality is poor with Option A.
}
```

**Strategy 3: Instruct agents to generate concise queries.** In the hive-server API
documentation or tool description, specify: "Search queries should be 3-7 keywords,
not natural language sentences." LLM agents follow instructions well; telling them the
constraint avoids the problem at the source.

**Strategy 4: Hybrid search.** Meilisearch's hybrid search mode uses embeddings for
semantic matching alongside keyword search. Embeddings do not have the 10-word limit.
Setting `semanticRatio: 0.5` means half the ranking comes from vector similarity,
which considers the full query meaning regardless of word count.

### 7.3 Practical Impact Assessment

The 10-word limit is less severe than it appears for GSD use cases:

- **Requirement searches** are naturally short: "authentication email verification" (3 words)
- **Decision lookups** are keyword-driven: "caching strategy TTL Redis" (4 words)
- **Failure searches** are specific: "test timeout async verification" (4 words)
- **Cross-project searches** use domain terms: "OAuth provider comparison" (3 words)

The cases where 10 words are insufficient are complex multi-concept queries like "which
phase implemented the user-facing authentication flow with OAuth and email
verification" -- but these are better served by multiple targeted searches than one
verbose query.

---

## 8. Async Indexing vs Filesystem Writes

### 8.1 The Timing Problem

GSD writes to the filesystem synchronously. An agent completes a plan, writes
SUMMARY.md, updates STATE.md and ROADMAP.md, and commits. If hive-server indexes
these documents in Meilisearch, there is a gap between write and searchability:

```
Agent writes SUMMARY.md  ──>  hive-server receives update  ──>  Meilisearch enqueues
     t=0                            t=1                           t=2

Meilisearch indexes  ──>  Document is searchable
       t=3                      t=4
```

For Meilisearch, the gap between t=2 and t=4 is typically milliseconds to low seconds.
But the gap between t=0 and t=2 depends entirely on how hive-server learns about
filesystem changes.

### 8.2 Indexing Trigger Models

**Model A: Push on commit.** GSD commits completed work via git. A post-commit hook or
CI webhook notifies hive-server of changed files. hive-server reads the changed
`.planning/` files and indexes them.

- Pros: Indexes only committed (stable) content; natural batching; works with
  `commit_docs: true`
- Cons: Requires git hook integration; does not index uncommitted research/plans;
  latency depends on commit frequency

**Model B: API-driven indexing.** Modify GSD's workflow to POST document content to
hive-server after writing to disk. Each GSD command that writes a `.planning/` file
also sends the content to hive-server's indexing endpoint.

- Pros: Immediate indexing; indexes all documents (not just committed ones); explicit
  control over what gets indexed
- Cons: Requires GSD modification; adds network dependency to a currently offline tool;
  breaks GSD's "no API, no network" design

**Model C: Filesystem watcher.** hive-server or a sidecar process watches the
`.planning/` directory for changes and indexes modified files.

- Pros: Zero changes to GSD; indexes everything in real time
- Cons: Requires co-location (same machine or shared filesystem); file watcher
  reliability issues (especially on macOS with FSEvents); double-write detection
  (GSD often writes a file multiple times in quick succession)

**Model D: Periodic sync.** hive-server periodically scans `.planning/` directories
for changes (by modification time or content hash) and indexes updates.

- Pros: Simple; no hooks or watchers needed; works with any GSD version
- Cons: Latency proportional to sync interval; wastes cycles checking unchanged files;
  requires filesystem access

**Model E: Agent-mediated push.** The LLM agent, after running a GSD command, calls
hive-server's indexing API with the updated document content as part of its workflow.
The agent is already reading these files; it simply also sends them to hive-server.

- Pros: No GSD modification needed; leverages existing agent-hive-server communication;
  indexes exactly what the agent produced
- Cons: Relies on agent compliance (agents might forget); increases agent context usage
  for the API call; adds a step to every GSD command

### 8.3 Recommended Approach

**Model B (API-driven) for new integrations, Model E (agent-mediated) as a bridge.**

Model E is immediately viable -- it requires no changes to GSD and works within the
existing agent-hive-server communication pattern. The agent runs `/gsd:plan-phase`,
the plan is written to disk, and the agent (or a post-command hook) sends the content
to hive-server. This is imperfect but functional.

Model B is the long-term answer. If GSD grows a server-aware mode (or if hive-server
becomes the coordination layer GSD currently lacks), direct push-on-write becomes
natural. This aligns with the broader hive-server vision of being the infrastructure
layer for multi-agent workflows.

### 8.4 Consistency Guarantees

With async indexing, search results may lag behind filesystem state. This is acceptable
because:

1. GSD's sub-agents operate on fresh contexts. They read files at the start of their
   task and do not expect real-time updates during execution.
2. The primary consumer of search is the orchestrator, which runs between tasks (not
   during them). The indexing gap only matters if the orchestrator searches immediately
   after a task completes and before indexing finishes.
3. Meilisearch's indexing latency (sub-second for small documents) is shorter than the
   time between GSD tasks (which involve spawning new agent processes).

The practical inconsistency window is negligible for GSD's workflow patterns.

---

## 9. Cross-Project Search: Patterns, Decisions, Solutions

This is the highest-value capability Meilisearch adds to GSD. Currently, each
project's `.planning/` directory is an isolated silo. Knowledge generated in one
project is invisible to all others.

### 9.1 What Cross-Project Search Enables

**Requirement similarity.** Starting a new e-commerce project? Search for "shopping
cart checkout payment" across all projects. Find that a previous project already defined
15 requirements for payment processing, complete with requirement IDs, status, and
phase mappings. The planner agent can use these as templates rather than starting from
scratch.

**Architecture pattern reuse.** Search summaries for "REST API authentication
middleware" across projects. Find three different implementations: one using JWT, one
using session cookies, one using API keys. Each summary documents what worked, what
failed, and why. The planner has comparative data instead of a blank slate.

**Failure avoidance.** Search for "deployment failed Kubernetes" across all projects.
Find that two projects encountered the same issue with resource limits and both resolved
it the same way. The executor avoids repeating the failure.

**Research reuse.** Search for "database comparison PostgreSQL" across projects. Find
research from 6 months ago that evaluated PostgreSQL vs MySQL vs SQLite for a similar
use case. The researcher agent can build on this rather than re-investigating from
first principles.

### 9.2 Multi-Tenancy for Cross-Project Search

In hive-server, projects could be scoped by agent ID, team ID, or organization. The
multi-tenancy model determines who can search what:

```go
// Agent-scoped: only search own project
filter := fmt.Sprintf("project = '%s'", agentProject)

// Team-scoped: search all team projects
filter := fmt.Sprintf("team = '%s'", agentTeam)

// Organization-scoped: search everything the org has produced
filter := fmt.Sprintf("org = '%s'", agentOrg)

// Global: search all indexed projects (opt-in)
filter := "" // no filter
```

The `X-Agent-ID` header already provides agent scoping. Extending to team/org scoping
requires additional metadata in the index documents and in hive-server's auth model.

### 9.3 Federated Search for Holistic Results

Meilisearch's federated search merges results from multiple indexes into a single
ranked list. For cross-project queries, this returns a unified view:

```go
// "What do we know about OAuth?" -- search everything
results, _ := client.MultiSearch(&meilisearch.MultiSearchRequest{
    Queries: []meilisearch.SearchRequest{
        {IndexUID: "gsd_requirements", Query: "OAuth", Limit: 5},
        {IndexUID: "gsd_decisions",    Query: "OAuth", Limit: 5},
        {IndexUID: "gsd_research",     Query: "OAuth", Limit: 5},
        {IndexUID: "gsd_summaries",    Query: "OAuth", Limit: 5},
    },
})
```

The agent receives requirements, decisions, research, and implementation summaries
about OAuth in one response. This is a qualitatively different capability than grep
across `.planning/` directories.

---

## 10. hive-server as Mediator

### 10.1 Why hive-server, Not Direct Meilisearch Access

GSD agents could theoretically query Meilisearch directly. But hive-server adds
essential value as a mediator:

1. **Query preprocessing.** Strip stop words, enforce the 10-word limit intelligently,
   add agent-scoped filters. Agents send natural queries; hive-server optimizes them.

2. **Multi-tenancy enforcement.** hive-server injects `agent_id` and project filters
   based on the authenticated context. Agents cannot bypass access controls.

3. **Index lifecycle management.** hive-server creates indexes, configures settings,
   manages schema migrations when GSD's document formats evolve.

4. **Write coordination.** hive-server handles the SQLite-Meilisearch sync,
   deduplication, and retry logic. Agents just write to one API.

5. **Graceful degradation.** If Meilisearch is down, hive-server can fall back to
   SQLite LIKE queries or return an appropriate error. Agents do not need to handle
   Meilisearch failures.

6. **Abstraction.** Agents interact with a "search GSD knowledge" tool, not with
   Meilisearch specifically. The backend could be swapped to Typesense, Elasticsearch,
   or a vector database without changing the agent-facing API.

### 10.2 Proposed API Endpoints

```
POST /api/v1/gsd/index
  Body: { "project": "...", "doc_type": "requirement|decision|research|summary|document",
          "content": "...", "metadata": {...} }
  Response: { "task_id": "...", "status": "enqueued" }
  Purpose: Index a GSD document or extracted record

POST /api/v1/gsd/search
  Body: { "query": "...", "doc_types": ["requirement", "decision"],
          "project": "..." (optional), "filters": {...}, "limit": 20 }
  Response: { "results": [...], "facets": {...}, "processing_time_ms": 12 }
  Purpose: Search across GSD indexes

POST /api/v1/gsd/search/similar
  Body: { "document_id": "myproject__AUTH-01", "index": "gsd_requirements" }
  Response: { "similar": [...] }
  Purpose: Find similar documents (leverages hybrid/vector search)

GET /api/v1/gsd/facets
  Query: ?doc_type=requirement&facet=category,status,project
  Response: { "facets": { "category": {"Auth": 5, "API": 3}, ... } }
  Purpose: Browse available facet values for UI/agent discovery

POST /api/v1/gsd/reindex
  Body: { "project": "...", "source_path": ".planning/" }
  Response: { "task_id": "...", "documents_queued": 47 }
  Purpose: Full re-index of a project's .planning/ directory
```

### 10.3 Tool Definition for LLM Agents

For agents to use search, hive-server exposes it as a tool:

```json
{
  "name": "search_gsd_knowledge",
  "description": "Search across GSD planning documents including requirements, decisions, research findings, and execution summaries. Use 3-7 keywords, not full sentences. Searches are scoped to your project by default; set cross_project=true to search all projects.",
  "parameters": {
    "query": {
      "type": "string",
      "description": "3-7 keywords describing what you're looking for"
    },
    "doc_types": {
      "type": "array",
      "items": { "enum": ["requirement", "decision", "research", "summary"] }
    },
    "cross_project": { "type": "boolean", "default": false },
    "filters": {
      "type": "object",
      "description": "Optional filters: phase, status, category"
    }
  }
}
```

The tool description is itself a prompt engineering surface. By instructing "3-7
keywords, not full sentences" directly in the tool schema, the agent naturally produces
Meilisearch-friendly queries.

---

## 11. From Prompt Documents to Searchable Knowledge

### 11.1 The Transformation

GSD's documents serve two very different purposes today:

1. **Active prompts.** PLAN.md files are consumed by executor agents. STATE.md is read
   by the orchestrator. These are working documents for current execution.

2. **Accumulated knowledge.** RESEARCH.md, SUMMARY.md, decisions in PROJECT.md,
   completed requirements in REQUIREMENTS.md. These capture what was learned, decided,
   and built. They are rarely read after the phase that produced them.

Category 2 is where Meilisearch transforms GSD. These documents go from "files on
disk that might be read if someone knows to look" to "indexed knowledge that surfaces
automatically when relevant." The distinction matters:

- **Before:** Agent starting phase 5 reads STATE.md, ROADMAP.md, and REQUIREMENTS.md.
  It does not read the RESEARCH.md from phase 2 because it does not know it is
  relevant. The knowledge from phase 2's research is effectively lost.

- **After:** Agent starting phase 5 searches "payment processing authentication
  integration" before planning. Meilisearch returns the phase 2 research section about
  payment-auth interactions, a decision about token scoping from phase 3, and a summary
  from phase 4 documenting a related implementation. The agent has context it would
  never have found via sequential file reading.

### 11.2 Knowledge Lifecycle

```
  GSD writes document         hive-server indexes         Agent searches
  ┌─────────────────┐        ┌──────────────────┐       ┌──────────────┐
  │  RESEARCH.md    │──push──│  Extract sections │──────>│ "OAuth       │
  │  SUMMARY.md     │        │  Add metadata     │       │  comparison" │
  │  PROJECT.md     │        │  Index in Meili   │       │              │
  │  REQUIREMENTS.md│        └──────────────────┘       │  Results:    │
  └─────────────────┘                                    │  3 research  │
                                                         │  2 decisions │
                                                         │  1 summary   │
                                                         └──────────────┘
```

The value compounds over time. A single project produces maybe 20-40 documents.
Ten projects produce 200-400. An organization running GSD across dozens of projects
accumulates a corpus of engineering knowledge that is only as valuable as its
retrievability.

### 11.3 What This Does NOT Do

Meilisearch does not make GSD documents _smarter_. It does not:

- Add structure where GSD templates leave it ambiguous
- Validate that requirements are well-formed
- Detect contradictions between decisions
- Summarize or synthesize across documents
- Replace the need for agents to read primary documents

It makes existing knowledge _findable_. The agent still needs to read and reason about
what it finds. Meilisearch is a retrieval layer, not a reasoning layer.

---

## 12. Tradeoffs

### 12.1 What Is Gained

| Gain                                  | Impact                                                                                                  |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| **Cross-project knowledge retrieval** | Prevents re-solving solved problems; enables institutional memory across agent sessions and projects    |
| **Targeted context loading**          | Agents find specific relevant documents instead of reading everything; reduces context usage            |
| **Decision traceability**             | "Why did we choose X?" becomes a searchable question with concrete answers                              |
| **Failure pattern database**          | Past failures and their resolutions become searchable, reducing debug time                              |
| **Typo-tolerant search**              | Robust retrieval despite vocabulary variance across agents, users, and projects                         |
| **Faceted exploration**               | Agents can browse and filter knowledge by project, phase, status, category without knowing exact values |
| **Research reuse**                    | Domain research from one project benefits future projects in the same domain                            |
| **Quantitative project visibility**   | Facet distributions provide instant project health metrics                                              |

### 12.2 What Is Lost

| Loss                               | Impact                                                                                                                                                |
| ---------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Simplicity**                     | GSD's zero-infrastructure design is a key strength. Adding Meilisearch adds a runtime dependency, deployment complexity, and operational surface area |
| **Offline operation**              | GSD works entirely offline. Search requires a running Meilisearch instance (unless hive-server provides fallback)                                     |
| **Single-source-of-truth clarity** | With search, there are now two copies of data: filesystem and Meilisearch index. Drift between them becomes a failure mode                            |
| **GSD's independence**             | Currently, GSD depends on nothing but the filesystem and git. Adding hive-server and Meilisearch creates a dependency chain                           |

### 12.3 Complexity Added

| Complexity                      | Mitigation                                                                                                                                   |
| ------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| **Document extraction/parsing** | Start with the `gsd_documents` catch-all index; invest in per-type extraction only for proven high-value document types                      |
| **Index-filesystem sync**       | Agent-mediated push (Model E) as bridge; periodic full re-sync as safety net; idempotent upserts prevent duplicates                          |
| **Schema evolution**            | Meilisearch supports index swapping for zero-downtime reindexing; version index settings alongside GSD template versions                     |
| **Deployment**                  | Meilisearch is a single binary with Docker support; Helm chart for k8s; minimal operational burden compared to Elasticsearch                 |
| **Query quality**               | Stop word configuration + tool description guidance handle the 10-word limit; hybrid search available for semantic matching                  |
| **Data consistency**            | Meilisearch is explicitly a secondary index, not source of truth; inconsistency window is sub-second and irrelevant to GSD's workflow timing |

### 12.4 When This Is NOT Worth It

- **Single-project, single-developer use.** If GSD is used for one project at a time
  by one developer, grep is probably sufficient. The search infrastructure pays off
  with accumulated knowledge across projects.
- **Short-lived projects.** If projects are completed in a day, there is not enough
  accumulated knowledge to make search valuable.
- **No hive-server deployment.** If hive-server is not already part of the stack,
  deploying it plus Meilisearch just for GSD search is heavy. The value proposition
  depends on hive-server already existing as agent infrastructure.

---

## 13. Practical Integration Path

### Phase 1: Catch-All Document Indexing (Minimum Viable Search)

**Effort:** 2-3 days

1. Implement the `gsd_documents` catch-all index in hive-server
2. Add `POST /api/v1/gsd/index` endpoint accepting raw document content + metadata
3. Add `POST /api/v1/gsd/search` endpoint with basic query + project filter
4. Configure stop words, typo tolerance, and basic synonyms
5. Define the `search_gsd_knowledge` tool for agents

**What this proves:** Whether search over GSD documents is valuable enough to justify
the infrastructure. Agents can search across all documents but with coarse granularity
(whole files, not extracted records).

### Phase 2: Structured Extraction for High-Value Documents

**Effort:** 3-5 days

1. Implement requirement extraction from REQUIREMENTS.md -> `gsd_requirements` index
2. Implement summary extraction from SUMMARY.md -> `gsd_summaries` index
3. Add faceted search to the API
4. Add multi-index search (requirements + summaries in one query)

**What this proves:** Whether granular, structured search (individual requirements,
specific summaries) provides meaningfully better results than catch-all document search.

### Phase 3: Cross-Project Search and Decision Mining

**Effort:** 3-5 days

1. Implement decision extraction from PROJECT.md + CONTEXT.md -> `gsd_decisions` index
2. Implement research section extraction from RESEARCH.md -> `gsd_research` index
3. Enable cross-project search (remove default project filter; add team/org scoping)
4. Add `POST /api/v1/gsd/search/similar` for finding related documents
5. Evaluate hybrid search (vector embeddings) for semantic matching

**What this proves:** Whether cross-project knowledge retrieval delivers on the promise
of institutional memory across agent sessions.

### Phase 4: Deep Integration

**Effort:** 5-8 days

1. Integrate search into GSD's workflow (agents automatically search before planning)
2. Add indexing to GSD's write paths (Model B: direct push on write)
3. Implement periodic full re-sync for consistency safety net
4. Add analytics: most-searched terms, search-to-action conversion, knowledge gaps

---

## 14. Open Questions

1. **Extraction fidelity.** How reliably can requirements, decisions, and research
   findings be extracted from GSD's markdown format? The format is template-guided but
   not schema-enforced. What is the error rate for extraction, and does noisy extraction
   degrade search quality below usefulness?

2. **GSD template evolution.** GSD is actively developed (27k stars, solo maintainer).
   Template formats may change. How does the extraction layer handle format changes?
   Schema versioning? Dual-format support during transitions?

3. **Hybrid search value.** Does Meilisearch's vector/semantic search provide
   meaningful improvement over keyword search for GSD documents? GSD documents use
   technical vocabulary where keyword matching is already strong. Semantic search adds
   embedding computation cost -- is it justified?

4. **Agent behavior change.** Adding a search tool changes how agents approach problems.
   Instead of reading primary documents, they may over-rely on search results (which
   are excerpts, not full context). Does this create a new class of errors where agents
   act on partial information? Does the tool description need to explicitly instruct
   "search to discover, then read the source document for full context"?

5. **Index size projections.** How large does the search index grow? A 10-phase project
   might produce 40-50 documents, each 1-10KB. 100 projects would be ~5,000 documents,
   ~25MB of text. This is trivial for Meilisearch. But does the per-project document
   count grow faster than linearly with project complexity?

6. **Privacy and data boundaries.** Cross-project search assumes projects can share
   knowledge. In a multi-user or multi-organization context, what are the data isolation
   requirements? Can an agent working on project A see decisions from project B? Should
   there be explicit opt-in for cross-project visibility?

7. **Meilisearch as GSD's "missing database."** GSD explicitly lacks a database. Is
   Meilisearch the right tool to fill that gap, or does GSD need a proper database
   (SQLite, Gel, etc.) for structured state and Meilisearch only for search? The
   analysis in this document assumes the latter (Meilisearch as search layer, not
   database), but the boundary may be worth revisiting.
