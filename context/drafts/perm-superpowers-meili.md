# Meilisearch as a Search Backend for Superpowers Skills

**Date:** 2026-03-09
**Status:** Analysis
**Depends on:** [superpowers.md](superpowers.md), [meilisearch.md](meilisearch.md)

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [The Problem: Superpowers Has No Memory](#2-the-problem-superpowers-has-no-memory)
3. [Index Schema Proposals](#3-index-schema-proposals)
4. [Skill Discovery via Fuzzy Search](#4-skill-discovery-via-fuzzy-search)
5. [Faceted Search for Skill Filtering](#5-faceted-search-for-skill-filtering)
6. [Cross-Session Search](#6-cross-session-search)
7. [The 10-Word Query Limit](#7-the-10-word-query-limit)
8. [hive-server as Mediator](#8-hive-server-as-mediator)
9. [Tradeoffs](#9-tradeoffs)
10. [Concrete Integration Design](#10-concrete-integration-design)
11. [What This Does Not Solve](#11-what-this-does-not-solve)

---

## 1. Executive Summary

Superpowers is a 14-skill agentic framework with no search, no memory, and no retrieval capability. Every session starts from zero. Meilisearch is a typo-tolerant, sub-50ms full-text search engine with faceting, filtering, and hybrid semantic search. The integration thesis is straightforward: index Superpowers' skill definitions, workflow artifacts, and session logs into Meilisearch, and expose search through hive-server's existing REST API. This gives agents the ability to discover skills by natural language description, find prior solutions to similar problems, and retrieve relevant context from past sessions -- capabilities that Superpowers explicitly lacks and explicitly identifies as architectural gaps.

The integration is not trivial. It introduces a new runtime dependency, a synchronization problem between SQLite and Meilisearch, and query-length constraints that conflict with how LLMs naturally construct queries. But the payoff is significant: it transforms Superpowers from a stateless skill loader into a system that accumulates and retrieves institutional knowledge.

---

## 2. The Problem: Superpowers Has No Memory

Superpowers' state management section is blunt about its own limitations:

> There is no database, no session store, no cross-session memory, and no shared state between agents. Each subagent starts fresh with only the context explicitly passed to it.

The five capability gaps it identifies map directly to search problems:

| Gap                               | Search Solution                                                  |
| --------------------------------- | ---------------------------------------------------------------- |
| Cannot remember across sessions   | Index session logs, retrieve relevant history by similarity      |
| Cannot share state between agents | Shared search index with agent-scoped filters                    |
| Cannot track historical outcomes  | Index task completion records, search by outcome                 |
| Cannot search prior work          | Full-text + semantic search over plans, designs, implementations |
| Cannot manage skill effectiveness | Index skill invocation events, facet by success/failure          |

Today, Superpowers' skill discovery works by loading all 14 SKILL.md files into the agent's context window at session start (via the session-start hook) and relying on the LLM to pattern-match the `description` field against the current task. This is a brute-force approach: every skill is always loaded, every session. It works at 14 skills. It will not work at 140 or 1,400.

More importantly, the current system has no way to search the _output_ of skill-driven workflows -- the plans that were written, the debugging sessions that were run, the code reviews that were conducted. All of that institutional knowledge evaporates when the context window closes.

---

## 3. Index Schema Proposals

### 3.1 Skills Index

This index stores Superpowers skill definitions for discovery and retrieval.

```go
// Index: "skills"
// Primary key: "id"
// Document example:
{
    "id":          "superpowers:systematic-debugging",
    "name":        "systematic-debugging",
    "source":      "superpowers",         // "superpowers" | "personal" | "organization"
    "type":        "discipline",          // "discipline" | "technique" | "pattern" | "reference"
    "category":    "debugging",           // "process" | "quality" | "debugging" | "collaboration" | "review" | "git" | "meta"
    "description": "Use when you encounter a bug, test failure, or unexpected behavior. Applies to ANY debugging situation - not just complex ones.",
    "trigger":     "bug test-failure unexpected-behavior error crash regression", // extracted trigger keywords
    "platforms":   ["claude-code", "cursor", "codex", "opencode"],
    "workflow_stage": ["implementation", "verification"],  // where in the pipeline this skill applies
    "content_hash": "sha256:abc123...",   // for change detection
    "content":     "Full SKILL.md content with frontmatter stripped...",
    "updated_at":  "2026-02-21T00:00:00Z"
}
```

```go
settings := &meilisearch.Settings{
    SearchableAttributes: []string{
        "description",    // highest priority: the "Use when..." clause
        "trigger",        // extracted trigger keywords
        "name",           // skill name
        "content",        // full skill body (lowest priority, but catches deep references)
    },
    FilterableAttributes: []string{
        "source",
        "type",
        "category",
        "platforms",
        "workflow_stage",
    },
    SortableAttributes: []string{
        "updated_at",
        "name",
    },
    DisplayedAttributes: []string{
        "id", "name", "source", "type", "category",
        "description", "platforms", "workflow_stage",
        "content", "updated_at",
    },
    Synonyms: map[string][]string{
        "bug":       {"defect", "issue", "error", "problem", "failure"},
        "test":      {"spec", "assertion", "check", "verification"},
        "plan":      {"design", "architecture", "proposal", "spec"},
        "review":    {"audit", "inspection", "critique", "feedback"},
        "debug":     {"troubleshoot", "diagnose", "investigate", "fix"},
        "tdd":       {"test-driven", "red-green-refactor"},
        "worktree":  {"branch", "isolation", "workspace"},
        "subagent":  {"sub-agent", "child-agent", "worker-agent"},
    },
    RankingRules: []string{
        "words",
        "typo",
        "proximity",
        "attribute",  // description matches rank above content matches
        "sort",
        "exactness",
    },
    TypoTolerance: &meilisearch.TypoTolerance{
        Enabled: true,
        MinWordSizeForTypos: &meilisearch.MinWordSizeForTypos{
            OneTypo:  4,  // slightly more aggressive than default (5)
            TwoTypos: 8,  // slightly more aggressive than default (9)
        },
    },
}
```

**Why `description` is the top searchable attribute:** Superpowers' own documentation says "Descriptions use 'Use when...' patterns to optimize for contextual matching." The description field is already engineered for retrieval. Placing it first in `searchableAttributes` means the `attribute` ranking rule will prefer matches in descriptions over matches buried in skill content.

**Why a separate `trigger` field:** The description contains natural language prose. The trigger field contains extracted keywords that an agent is likely to use when searching. For example, the systematic-debugging skill's description says "Use when you encounter a bug, test failure, or unexpected behavior." The trigger field distills this to `"bug test-failure unexpected-behavior error crash regression"`, giving Meilisearch more keyword surface area to match against.

### 3.2 Workflow Artifacts Index

This index stores the output of Superpowers workflows: plans, designs, brainstorm results, and code review reports.

```go
// Index: "artifacts"
// Primary key: "id"
// Document example:
{
    "id":            "artifact:2026-03-09-auth-refactor-plan",
    "agent_id":      "agent-abc123",
    "session_id":    "session-xyz789",
    "type":          "plan",              // "plan" | "design" | "brainstorm" | "review" | "debug-log"
    "title":         "Auth middleware refactor to support OAuth2",
    "content":       "## Problem\nThe current bearer token auth...",
    "skill_used":    "writing-plans",     // which skill produced this artifact
    "tags":          ["auth", "middleware", "oauth2", "security"],
    "status":        "completed",         // "draft" | "in-progress" | "completed" | "abandoned"
    "outcome":       "success",           // "success" | "partial" | "failure" | null
    "tasks_total":   8,
    "tasks_done":    8,
    "repo":          "christmas-island/hive-server",
    "branch":        "feat/oauth2-auth",
    "created_at":    "2026-03-09T14:00:00Z",
    "completed_at":  "2026-03-09T18:30:00Z"
}
```

```go
settings := &meilisearch.Settings{
    SearchableAttributes: []string{
        "title",
        "tags",
        "content",
    },
    FilterableAttributes: []string{
        "agent_id",
        "session_id",
        "type",
        "skill_used",
        "status",
        "outcome",
        "repo",
        "created_at",
    },
    SortableAttributes: []string{
        "created_at",
        "completed_at",
    },
    Synonyms: map[string][]string{
        "auth":     {"authentication", "authorization", "login", "credentials"},
        "api":      {"endpoint", "route", "handler", "rest"},
        "db":       {"database", "storage", "persistence", "sqlite"},
        "k8s":      {"kubernetes", "deployment", "cluster"},
    },
}
```

### 3.3 Session Logs Index

This index stores summaries of agent sessions for cross-session learning.

```go
// Index: "sessions"
// Primary key: "id"
// Document example:
{
    "id":              "session:2026-03-09-abc123",
    "agent_id":        "agent-abc123",
    "summary":         "Debugged flaky integration test in auth middleware. Root cause was race condition in token refresh. Fixed with sync.Mutex.",
    "skills_invoked":  ["systematic-debugging", "test-driven-development"],
    "problems_seen":   ["race condition", "flaky test", "token refresh"],
    "solutions_used":  ["sync.Mutex", "t.Parallel() removal", "retry with backoff"],
    "outcome":         "success",
    "repo":            "christmas-island/hive-server",
    "files_touched":   ["internal/handlers/auth.go", "internal/handlers/auth_test.go"],
    "duration_minutes": 45,
    "created_at":      "2026-03-09T10:00:00Z"
}
```

```go
settings := &meilisearch.Settings{
    SearchableAttributes: []string{
        "summary",
        "problems_seen",
        "solutions_used",
        "files_touched",
    },
    FilterableAttributes: []string{
        "agent_id",
        "skills_invoked",
        "outcome",
        "repo",
        "created_at",
    },
    SortableAttributes: []string{
        "created_at",
        "duration_minutes",
    },
}
```

---

## 4. Skill Discovery via Fuzzy Search

### 4.1 How It Works Today

Currently, Superpowers loads all skill descriptions into the context window and relies on the LLM to match. The using-superpowers meta-skill sets a low activation threshold: "If you think there is even a 1% chance a skill might apply, you ABSOLUTELY MUST invoke the skill."

This works because there are only 14 skills. The LLM can hold all descriptions in context simultaneously and pattern-match. But this approach has problems:

1. **It consumes context window tokens on every session**, even when most skills are irrelevant.
2. **It scales linearly with skill count.** At 100+ skills (personal + organizational), the context budget becomes a real constraint.
3. **It depends on the LLM's attention.** In long sessions, early-injected skill descriptions may be forgotten or deprioritized.
4. **It cannot match partial or misspelled intent.** If the agent's internal reasoning uses "troublshoot" instead of "troubleshoot", there is no fuzzy matching.

### 4.2 How Meilisearch Changes This

With skills indexed in Meilisearch, the agent can query for relevant skills dynamically:

**Example 1: Agent encounters a test failure**

```
Query: "test failure unexpected behavior"
```

Meilisearch returns (ranked by relevance):

1. `systematic-debugging` -- description matches "unexpected behavior" exactly
2. `test-driven-development` -- description matches "test" with proximity to development context
3. `verification-before-completion` -- description mentions test verification

**Example 2: Agent misspells a keyword**

```
Query: "debuging flaky integartion test"
```

Typo tolerance handles this:

- "debuging" matches "debugging" (1 typo, 8 chars, within the 1-typo threshold for 5+ char words)
- "integartion" matches "integration" (1 typo, 11 chars, within the 2-typo threshold for 9+ char words)
- "flaky" is an exact match

Result: `systematic-debugging` still ranks first.

**Example 3: Agent uses a synonym**

```
Query: "troubleshoot production error"
```

With the synonym map `"debug": ["troubleshoot", "diagnose", "investigate", "fix"]`, this matches skills containing "debug" in their descriptions.

**Example 4: Filtered discovery**

```
Query: "how to handle code review feedback"
Filter: "workflow_stage = 'review'"
```

Returns only review-stage skills:

1. `receiving-code-review`
2. `requesting-code-review`

Without the filter, skills like `brainstorming` and `writing-plans` would also appear (they mention review-related concepts in their content).

### 4.3 "Use When..." Pattern Matching

Superpowers' skill descriptions follow a consistent format:

```
Use when [trigger condition] - [what it does]
```

This format is already optimized for search. The trigger condition contains the keywords an agent would naturally use when looking for help. Meilisearch's `attribute` ranking rule, combined with `description` as the first searchable attribute, means these trigger conditions get the highest relevance weight.

Consider how different ranking rules interact for a query like `"parallel independent tasks"`:

| Rule          | Effect                                                                       |
| ------------- | ---------------------------------------------------------------------------- |
| **words**     | Skills matching all three terms rank above those matching two                |
| **typo**      | Exact matches of "parallel" rank above typo-corrected matches                |
| **proximity** | Skills where "parallel" and "independent" appear near each other rank higher |
| **attribute** | Matches in `description` rank above matches in `content`                     |
| **exactness** | Exact "parallel" ranks above prefix match "parallel..."                      |

This bucket-sort cascade naturally surfaces `dispatching-parallel-agents` (which contains "parallel" and "independent" in its description) over `subagent-driven-development` (which mentions parallel concepts only in its body content).

---

## 5. Faceted Search for Skill Filtering

### 5.1 Skill Type Facets

Superpowers defines four skill types. These map directly to filterable facets:

```
POST /indexes/skills/search
{
    "q": "debugging",
    "facets": ["type", "category", "workflow_stage"]
}
```

Response:

```json
{
    "hits": [...],
    "facetDistribution": {
        "type": {
            "discipline": 3,
            "technique": 5,
            "pattern": 4,
            "reference": 2
        },
        "category": {
            "debugging": 2,
            "quality": 3,
            "process": 1
        },
        "workflow_stage": {
            "implementation": 4,
            "verification": 3,
            "planning": 1
        }
    }
}
```

This tells the agent: "There are 2 discipline skills and 5 technique skills related to debugging. Most apply during implementation (4) or verification (3)."

### 5.2 Use Cases for Faceted Discovery

**Agent is in the planning phase and wants applicable skills:**

```
Filter: "workflow_stage = 'planning'"
Query: ""  (empty query, just browsing)
```

Returns: `brainstorming`, `writing-plans`

**Agent wants only discipline-type skills (rules and requirements, not how-to guides):**

```
Filter: "type = 'discipline'"
Query: "testing"
```

Returns: `test-driven-development`, `verification-before-completion` (discipline skills that enforce testing), excluding technique skills that merely describe testing approaches.

**Agent is using Claude Code specifically:**

```
Filter: "platforms IN ['claude-code']"
Query: "worktree isolation"
```

This becomes relevant when personal or organizational skills are platform-specific.

### 5.3 Faceted Search Over Artifacts

Facets become even more valuable when searching workflow artifacts:

```
POST /indexes/artifacts/search
{
    "q": "authentication middleware",
    "facets": ["type", "status", "outcome", "skill_used"],
    "filter": "repo = 'christmas-island/hive-server'"
}
```

Response:

```json
{
  "facetDistribution": {
    "type": {
      "plan": 3,
      "debug-log": 2,
      "review": 1
    },
    "outcome": {
      "success": 4,
      "failure": 1,
      "partial": 1
    },
    "skill_used": {
      "writing-plans": 3,
      "systematic-debugging": 2,
      "requesting-code-review": 1
    }
  }
}
```

This tells the agent: "There have been 3 plans, 2 debug sessions, and 1 code review related to authentication middleware in this repo. 4 succeeded, 1 failed, 1 was partial. The debugging skill was used twice -- there may be recurring issues here."

---

## 6. Cross-Session Search

### 6.1 The Core Capability Gap

Superpowers' most significant limitation is the lack of cross-session memory. From the Superpowers brief:

> Each subagent starts fresh with only the context explicitly passed to it.

This means an agent debugging a flaky test today cannot know that another agent debugged the same flaky test last week and found a race condition. The same root-cause investigation happens repeatedly.

### 6.2 Finding Similar Problems

With the sessions index, agents can search for prior encounters:

```
Query: "race condition token refresh"
Filter: "repo = 'christmas-island/hive-server' AND outcome = 'success'"
Sort: ["created_at:desc"]
```

Returns sessions where similar problems were successfully resolved. The agent reads the summary and solutions_used fields to learn from past debugging:

```json
{
  "summary": "Debugged flaky integration test in auth middleware. Root cause was race condition in token refresh. Fixed with sync.Mutex.",
  "solutions_used": ["sync.Mutex", "t.Parallel() removal", "retry with backoff"]
}
```

This is not semantic understanding -- Meilisearch is doing keyword matching with typo tolerance. But for structured session summaries written with search in mind, keyword matching is effective.

### 6.3 Finding Reusable Solutions

```
Query: "sqlite migration schema change"
Filter: "skills_invoked IN ['writing-plans'] AND outcome = 'success'"
```

Returns past planning sessions that dealt with database migrations. The agent can read prior plans to inform the current one, avoiding repeated design work.

### 6.4 Finding Past Debugging Approaches

```
Query: "timeout deadline exceeded kubernetes"
Filter: "skills_invoked IN ['systematic-debugging']"
Sort: ["created_at:desc"]
Limit: 5
```

Returns the 5 most recent debugging sessions involving timeout issues in Kubernetes. The `solutions_used` field acts as a quick-reference for what worked before.

### 6.5 Limitations of Cross-Session Search

This is not a knowledge graph. Meilisearch cannot reason about _relationships_ between sessions, skills, and outcomes. It cannot answer "what debugging approach works best for race conditions?" -- it can only return sessions that contain those keywords. The agent must synthesize meaning from the results.

Additionally, the quality of cross-session search depends entirely on the quality of session summaries. If summaries are vague ("Fixed a bug"), search will return unhelpful results. The system requires a disciplined summarization protocol, which could itself be a Superpowers skill.

---

## 7. The 10-Word Query Limit

### 7.1 The Constraint

Meilisearch silently drops query terms beyond the 10th word. This is documented as a hard limit. LLM agents naturally generate verbose queries because they think in sentences, not keywords.

An agent might internally construct a query like:

```
"how do I handle a situation where parallel subagents need to coordinate on shared state"
```

That is 15 words. Meilisearch sees:

```
"how do I handle a situation where parallel subagents need"
```

And drops: `"to coordinate on shared state"`. The most important terms -- "coordinate" and "shared state" -- are lost.

### 7.2 Mitigation Strategies

**Strategy 1: Keyword extraction preprocessing.** hive-server strips stop words and extracts content words before forwarding to Meilisearch:

```
Input:  "how do I handle a situation where parallel subagents need to coordinate on shared state"
Output: "handle parallel subagents coordinate shared state"
```

6 words, well within the limit. This can be done with a simple stop-word list in the hive-server handler.

```go
func extractKeywords(query string) string {
    stopWords := map[string]bool{
        "how": true, "do": true, "i": true, "a": true, "the": true,
        "where": true, "when": true, "what": true, "is": true, "are": true,
        "to": true, "on": true, "in": true, "for": true, "with": true,
        "that": true, "this": true, "it": true, "of": true, "and": true,
        "or": true, "not": true, "be": true, "an": true, "my": true,
        "need": true, "want": true, "should": true, "would": true,
        "situation": true, "handle": true, // domain-generic words
    }
    words := strings.Fields(query)
    var keywords []string
    for _, w := range words {
        if !stopWords[strings.ToLower(w)] {
            keywords = append(keywords, w)
        }
    }
    if len(keywords) > 10 {
        keywords = keywords[:10]
    }
    return strings.Join(keywords, " ")
}
```

**Strategy 2: Meilisearch's built-in stop words.** Configure stop words at the index level:

```go
settings := &meilisearch.Settings{
    StopWords: []string{
        "the", "a", "an", "is", "are", "was", "were", "be", "been",
        "being", "have", "has", "had", "do", "does", "did", "will",
        "would", "could", "should", "may", "might", "must", "shall",
        "can", "need", "dare", "ought", "used", "to", "of", "in",
        "for", "on", "with", "at", "by", "from", "as", "into",
        "through", "during", "before", "after", "above", "below",
        "between", "out", "off", "over", "under", "again", "further",
        "then", "once", "here", "there", "when", "where", "why",
        "how", "all", "each", "every", "both", "few", "more", "most",
        "other", "some", "such", "no", "nor", "not", "only", "own",
        "same", "so", "than", "too", "very", "just", "because",
        "but", "and", "or", "if", "while", "about", "up",
        "i", "my", "me", "we", "our",
    },
}
```

Note: Meilisearch stop words are removed _before_ the 10-word count is applied. So with stop words configured at the index level, the 15-word query above becomes effectively `"handle situation parallel subagents coordinate shared state"` -- 7 words, all content-bearing.

**Strategy 3: Hybrid/semantic search.** For queries where keyword extraction loses meaning, use Meilisearch's hybrid search with a configured embedder:

```go
searchReq := &meilisearch.SearchRequest{
    Hybrid: &meilisearch.SearchRequestHybrid{
        Embedder:      "openai",
        SemanticRatio: 0.7, // lean heavily on semantic meaning
    },
}
```

Semantic search uses the full query text for embedding generation and is not subject to the 10-word keyword limit. The tradeoff is latency (embedding generation) and cost (API calls to the embedding provider).

**Strategy 4: Multi-search decomposition.** Break a complex query into multiple focused searches:

```go
results, err := client.MultiSearch(&meilisearch.MultiSearchRequest{
    Queries: []meilisearch.SearchRequest{
        {IndexUID: "skills", Query: "parallel subagent coordination"},
        {IndexUID: "skills", Query: "shared state synchronization"},
    },
})
```

Two focused 3-word queries hit the same skills from different angles, then hive-server merges and deduplicates the results.

### 7.3 Recommendation

Use strategies 1 and 2 together as the default. They are zero-cost, require no external dependencies, and handle the vast majority of agent-generated queries. Reserve strategy 3 (hybrid search) for a future iteration where semantic similarity is explicitly needed. Strategy 4 is useful but adds implementation complexity for marginal gain.

---

## 8. hive-server as Mediator

### 8.1 Architecture

hive-server already sits between LLM agents and persistent storage. Adding Meilisearch as a search backend follows the established pattern:

```
Agent (Claude Code + Superpowers)
    |
    | HTTP + Bearer token + X-Agent-ID
    v
hive-server (Go + chi)
    |
    +---> SQLite (source of truth for all data)
    |
    +---> Meilisearch (search index, populated from SQLite)
```

Agents never talk to Meilisearch directly. hive-server handles:

1. **Query preprocessing** (keyword extraction, stop word removal, query truncation)
2. **Multi-tenancy** (injecting `agent_id` filters from X-Agent-ID header)
3. **Index management** (creating indexes, configuring settings, reindexing)
4. **Sync** (writing to SQLite first, then indexing in Meilisearch asynchronously)
5. **Graceful degradation** (falling back to SQLite LIKE queries if Meilisearch is down)

### 8.2 API Design

New endpoints under the existing `/api/v1/` prefix:

```
POST   /api/v1/search/skills          Search skill definitions
POST   /api/v1/search/artifacts        Search workflow artifacts
POST   /api/v1/search/sessions         Search session logs
POST   /api/v1/search                  Federated search across all indexes

POST   /api/v1/artifacts               Create a workflow artifact
GET    /api/v1/artifacts/{id}          Get artifact by ID
PUT    /api/v1/artifacts/{id}          Update artifact
DELETE /api/v1/artifacts/{id}          Delete artifact

POST   /api/v1/sessions                Create/update a session log
GET    /api/v1/sessions/{id}           Get session by ID
```

Search request body:

```json
{
  "q": "race condition debugging",
  "filters": {
    "type": "debug-log",
    "outcome": "success",
    "repo": "christmas-island/hive-server"
  },
  "sort": ["created_at:desc"],
  "facets": ["type", "outcome", "skill_used"],
  "limit": 10
}
```

Search response:

```json
{
  "hits": [
    {
      "id": "session:2026-03-08-def456",
      "summary": "Debugged race condition in token refresh...",
      "solutions_used": ["sync.Mutex"],
      "_rankingScore": 0.92
    }
  ],
  "query": "race condition debugging",
  "processingTimeMs": 8,
  "estimatedTotalHits": 3,
  "facetDistribution": {
    "type": { "debug-log": 2, "plan": 1 },
    "outcome": { "success": 2, "partial": 1 }
  }
}
```

### 8.3 Skill Index Population

The skills index is special because its source of truth is the filesystem (SKILL.md files), not SQLite. hive-server would need either:

**Option A: Bootstrap from filesystem at startup.** hive-server reads SKILL.md files from a configured directory and indexes them. This requires hive-server to have access to the Superpowers plugin directory, which breaks the deployment model (hive-server runs as a server; Superpowers lives in each developer's local environment).

**Option B: Agent-driven registration.** Agents register skills via the API during their session-start hook. The session-start hook already loads all skills; it would additionally POST each skill definition to hive-server.

```
POST /api/v1/skills
{
    "name": "systematic-debugging",
    "source": "superpowers",
    "type": "discipline",
    "category": "debugging",
    "description": "Use when you encounter a bug...",
    "content": "Full SKILL.md content..."
}
```

Option B is the correct approach. It respects Superpowers' skill shadowing model (personal skills override built-in ones per agent), works across distributed environments, and requires no filesystem access from hive-server. The skills index becomes a union of all skills registered by all agents, with source attribution.

### 8.4 Session Log Population

Superpowers has no built-in session logging. This is a new capability that hive-server would provide. The integration requires a new Superpowers skill (or hook) that produces session summaries at session end:

1. Agent completes work.
2. A session-end hook (or a `finishing-a-development-branch`-style skill) generates a structured summary.
3. The summary is POSTed to hive-server's `/api/v1/sessions` endpoint.
4. hive-server writes to SQLite and indexes in Meilisearch.

The summary format should be machine-optimized for search:

```markdown
## Session Summary

- **Problems:** race condition in token refresh, flaky test in auth_test.go
- **Root Cause:** concurrent goroutines accessing shared token cache without synchronization
- **Solution:** added sync.RWMutex to TokenCache struct, removed t.Parallel() from affected tests
- **Skills Used:** systematic-debugging, test-driven-development
- **Outcome:** success
- **Files:** internal/handlers/auth.go, internal/handlers/auth_test.go
```

### 8.5 Artifact Population

Workflow artifacts (plans, designs, reviews) are already written to the filesystem by Superpowers. The executing-plans skill writes to `docs/plans/YYYY-MM-DD-<feature>.md`. These can be captured at creation time:

1. Agent creates a plan using the writing-plans skill.
2. The skill (or a wrapper) POSTs the plan content to hive-server.
3. hive-server stores in SQLite, indexes in Meilisearch.

Alternatively, a periodic sync could scan `docs/plans/` and index new files. But the agent-driven approach is more reliable and provides richer metadata (which skill produced it, what the status is, which session it belongs to).

---

## 9. Tradeoffs

### 9.1 What is Gained

| Capability                               | Impact                                                                                                                                      |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| **Skill discovery by natural language**  | Agents find relevant skills without loading all skills into context. Scales to hundreds of skills.                                          |
| **Typo-tolerant skill matching**         | Agent reasoning that misspells keywords still finds the right skill. Reduces missed activations.                                            |
| **Cross-session memory**                 | Agents learn from past debugging sessions, plans, and decisions. Eliminates repeated root-cause investigations.                             |
| **Faceted skill browsing**               | Agents filter by type/category/stage to narrow choices. Reduces irrelevant skill activation.                                                |
| **Institutional knowledge accumulation** | Organization builds a searchable corpus of how problems were solved. New agents benefit from prior work.                                    |
| **Workflow artifact retrieval**          | Prior plans and designs are discoverable. Agents build on existing work instead of starting from scratch.                                   |
| **Context window efficiency**            | Instead of loading all 14+ skill descriptions at startup, agents query for relevant skills on demand. Frees context budget for actual work. |

### 9.2 What is Lost

| Concern                            | Impact                                                                                                                                                      |
| ---------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Zero-infrastructure simplicity** | Superpowers' major selling point is "no server, no database." Adding Meilisearch + hive-server dependency destroys this.                                    |
| **Offline capability**             | Without a running hive-server and Meilisearch instance, search-dependent features are unavailable. Superpowers today works entirely offline.                |
| **Deterministic skill loading**    | Today, all skills are always loaded. With search-based discovery, there is a risk of relevant skills being missed due to poor queries or ranking artifacts. |
| **Platform independence**          | The integration ties Superpowers to hive-server's API. Other platforms (Cursor, Codex, OpenCode) would need equivalent search backends or would degrade.    |

### 9.3 Complexity Added

| Component                       | Complexity                                                                                                  |
| ------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| **Meilisearch deployment**      | New service to deploy, monitor, back up. Kubernetes sidecar or separate pod. Persistent volume for data.    |
| **Data synchronization**        | SQLite-to-Meilisearch sync must handle failures, retries, and drift. Need periodic full-reindex capability. |
| **Query preprocessing**         | Keyword extraction, stop-word removal, query truncation. Small but non-trivial code path.                   |
| **Index schema management**     | Index settings must be versioned and migrated alongside application code.                                   |
| **Session summarization**       | New skill or hook to produce structured summaries. Quality of search depends on quality of summaries.       |
| **Skill registration protocol** | Agents must register skills on session start. Adds latency to session bootstrap.                            |
| **Testing**                     | Mock-based testing for Meilisearch interactions. Integration tests against a live Meilisearch instance.     |

### 9.4 Assessment

The tradeoff is worth it for teams that run multiple agents across sessions and want accumulated knowledge. It is not worth it for solo developers using Superpowers locally on a single project -- the zero-infrastructure model is strictly better for that use case.

The key design principle is **graceful degradation**: if Meilisearch is unavailable, the system falls back to Superpowers' existing brute-force skill loading. Search is an enhancement, not a dependency. Skills still work without it. Plans are still written to the filesystem. The only capability that is truly lost without Meilisearch is cross-session retrieval.

---

## 10. Concrete Integration Design

### 10.1 New Superpowers Skill: `session-logging`

```yaml
---
name: session-logging
description: Use when finishing any significant work session - creates structured session summary for cross-session search and institutional memory.
---
```

This skill instructs the agent to produce a structured summary and POST it to hive-server before session end. It would be a discipline-type skill, activated automatically by the `finishing-a-development-branch` skill or by a session-end hook.

### 10.2 Modified Session-Start Hook

The existing session-start hook loads skills from the filesystem. The modified version additionally:

1. Registers all loaded skills with hive-server (POST /api/v1/skills).
2. Queries hive-server for session history relevant to the current repository (POST /api/v1/search/sessions).
3. Injects a brief summary of relevant prior sessions into the agent's context.

```bash
# session-start hook (pseudocode)
# 1. Load skills from filesystem (existing behavior)
load_skills

# 2. Register skills with hive-server (new)
for skill in $SKILLS; do
    curl -X POST "$HIVE_URL/api/v1/skills" \
        -H "Authorization: Bearer $HIVE_TOKEN" \
        -H "X-Agent-ID: $AGENT_ID" \
        -d @"$skill"
done

# 3. Search for relevant prior sessions (new)
curl -X POST "$HIVE_URL/api/v1/search/sessions" \
    -H "Authorization: Bearer $HIVE_TOKEN" \
    -d '{"q": "", "filters": {"repo": "'$REPO'"}, "sort": ["created_at:desc"], "limit": 5}'
```

### 10.3 hive-server Search Handler

```go
// internal/handlers/search.go

func (h *Handler) SearchSkills(w http.ResponseWriter, r *http.Request) {
    agentID := middleware.AgentIDFromContext(r.Context())

    var req SearchRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "invalid request body", http.StatusBadRequest)
        return
    }

    // Preprocess query: extract keywords, enforce 10-word limit
    req.Q = preprocessQuery(req.Q)

    // Build Meilisearch search request
    meiliReq := &meilisearch.SearchRequest{
        Limit:            int64(req.Limit),
        ShowRankingScore: true,
    }

    // Build filter string from structured filters + agent scoping
    filters := buildFilterString(req.Filters, agentID)
    if filters != "" {
        meiliReq.Filter = filters
    }

    if len(req.Facets) > 0 {
        meiliReq.Facets = req.Facets
    }

    if len(req.Sort) > 0 {
        meiliReq.Sort = req.Sort
    }

    result, err := h.meili.Index("skills").Search(req.Q, meiliReq)
    if err != nil {
        // Graceful degradation: log error, return empty results
        h.logger.Error("meilisearch search failed", "error", err)
        json.NewEncoder(w).Encode(SearchResponse{Hits: []interface{}{}})
        return
    }

    json.NewEncoder(w).Encode(result)
}
```

### 10.4 Index Initialization on Startup

```go
// internal/search/init.go

func InitializeIndexes(client meilisearch.ServiceManager) error {
    indexes := map[string]*meilisearch.Settings{
        "skills":    skillsSettings(),
        "artifacts": artifactsSettings(),
        "sessions":  sessionsSettings(),
    }

    for uid, settings := range indexes {
        // Create index if it does not exist
        taskInfo, err := client.CreateIndex(&meilisearch.IndexConfig{
            Uid:        uid,
            PrimaryKey: "id",
        })
        if err != nil {
            // Index may already exist, continue
        } else {
            client.WaitForTask(taskInfo.TaskUID)
        }

        // Apply settings (idempotent)
        taskInfo, err = client.Index(uid).UpdateSettings(settings)
        if err != nil {
            return fmt.Errorf("configure index %s: %w", uid, err)
        }
        task, err := client.WaitForTask(taskInfo.TaskUID)
        if err != nil {
            return fmt.Errorf("wait for index %s settings: %w", uid, err)
        }
        if task.Status == meilisearch.TaskStatusFailed {
            return fmt.Errorf("index %s settings failed: %s", uid, task.Error.Message)
        }
    }
    return nil
}
```

---

## 11. What This Does Not Solve

Meilisearch addresses Superpowers' search and retrieval gap. It does not address:

| Gap                               | Why Meilisearch Does Not Help                                                                                                                                                                                                                    |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Real-time agent coordination**  | Meilisearch is a search engine, not a message bus. Parallel agents still cannot coordinate during execution.                                                                                                                                     |
| **Structured data relationships** | Meilisearch has no joins, no foreign keys, no graph traversal. It cannot answer "which skills were used by agents that also used the systematic-debugging skill."                                                                                |
| **Skill effectiveness metrics**   | Meilisearch can facet by outcome, but it cannot compute "systematic-debugging has a 73% success rate." That requires aggregation logic in hive-server or a proper analytics system.                                                              |
| **Skill contract enforcement**    | Meilisearch indexes text. It cannot validate that a skill's output conforms to expected structure. The "markdown-as-code fragility" limitation persists.                                                                                         |
| **Context window management**     | Meilisearch can return smaller, more relevant payloads than loading all skills. But the fundamental constraint -- that everything must fit in the context window -- is not solved by search alone. RAG with chunking is a separate architecture. |

The honest framing is: Meilisearch gives Superpowers a retrieval layer. It does not give it a brain. The agent still must decide what to search for, how to interpret results, and when to apply retrieved knowledge. But having retrieval at all is a qualitative improvement over the current "load everything, remember nothing" model.
