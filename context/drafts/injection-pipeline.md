# Injection Pipeline Design

The injection pipeline is the core value delivery mechanism of hive-server. On every prompt (or at a configurable interval), the MCP plugin calls hive-server with context about what the agent is doing. Hive-server fans out to multiple data sources, retrieves candidate directives, ranks and selects within a token budget, and returns micro-prompt snippets that steer agent behavior.

This document specifies the full pipeline from request to response.

---

## 1. The Request

### Endpoint

```
POST /api/v1/inject
```

No additional auth beyond the existing Bearer token. The `X-Agent-ID` header is required (not optional as it is for other endpoints).

### Request Payload

```json
{
  "session_id": "ses_abc123",
  "activity": "implementing",
  "project": {
    "name": "hive-server",
    "language": "go",
    "path": "/Users/dev/git/hive-server"
  },
  "context": {
    "summary": "Implementing the injection pipeline endpoint. Added a new handler in internal/handlers/inject.go. Working on the fan-out query logic.",
    "recent_files": [
      "internal/handlers/inject.go",
      "internal/store/directives.go"
    ],
    "recent_tools": ["Read", "Edit", "Bash"],
    "error_context": null
  },
  "intent": null,
  "token_budget": 500,
  "previous_injection_id": "inj_prev456"
}
```

### Field Specification

| Field                   | Type     | Required | Description                                                                                                                                                                       |
| ----------------------- | -------- | -------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `session_id`            | string   | yes      | Stable identifier for the conversation session. Used for session history lookups and deduplication.                                                                               |
| `activity`              | enum     | yes      | One of: `starting`, `brainstorming`, `planning`, `implementing`, `debugging`, `reviewing`, `deploying`, `testing`, `refactoring`. Drives which directive categories are relevant. |
| `project.name`          | string   | yes      | Project identifier. Used for project-specific directive lookup.                                                                                                                   |
| `project.language`      | string   | no       | Primary language. Enables language-specific directives (e.g., "run `go test`" vs "run `pytest`").                                                                                 |
| `project.path`          | string   | no       | Local filesystem path. Informational only; not used for file access.                                                                                                              |
| `context.summary`       | string   | yes      | 1-3 sentence summary of what the agent is currently doing. This is the primary semantic search input. Max 500 characters.                                                         |
| `context.recent_files`  | []string | no       | Files touched in the last few interactions. Used for project-area matching. Max 10 entries.                                                                                       |
| `context.recent_tools`  | []string | no       | MCP tools used recently. Signals agent behavior patterns. Max 10 entries.                                                                                                         |
| `context.error_context` | string   | no       | If debugging, the error message or stack trace. Max 1000 characters. Triggers debugging-specific directives.                                                                      |
| `intent`                | string   | no       | Explicit user-stated intent (e.g., "deploy to staging", "fix the auth bug"). When present, overrides inferred context for ranking. Max 200 characters.                            |
| `token_budget`          | int      | no       | Maximum tokens for the response payload. Default 500. Min 100, max 2000.                                                                                                          |
| `previous_injection_id` | string   | no       | ID of the last injection response this agent received. Used for deduplication (don't repeat the same directives consecutively).                                                   |

### Token Budget for the Request Itself

The request payload should stay under 800 tokens. The `context.summary` field is the largest variable component at 500 chars (~125 tokens). The `error_context` field adds up to 1000 chars (~250 tokens) when debugging. Total worst case: ~500 tokens for the request body.

The MCP plugin is responsible for summarizing conversation context into the `context.summary` field. It does not send raw message history. The summarization happens client-side to keep the request small and to avoid sending potentially sensitive conversation content over the wire.

### Activity Enum Semantics

| Activity        | When the MCP plugin sends this                                         |
| --------------- | ---------------------------------------------------------------------- |
| `starting`      | New session, agent has just been initialized. Cold start.              |
| `brainstorming` | Agent is exploring ideas with the user. No code changes yet.           |
| `planning`      | Agent is designing or outlining an approach. May be writing plan docs. |
| `implementing`  | Agent is actively writing or editing code.                             |
| `debugging`     | Agent is investigating a failure. `error_context` should be populated. |
| `reviewing`     | Agent is reviewing code, either its own or external.                   |
| `testing`       | Agent is running or writing tests.                                     |
| `refactoring`   | Agent is restructuring existing code without changing behavior.        |
| `deploying`     | Agent is running deployment commands or configuring infrastructure.    |

---

## 2. The Retrieval Fan-Out

When hive-server receives the inject request, it launches parallel queries to all available data sources. The fan-out uses a context with a hard deadline, and each source has an independent timeout. Results are merged as they arrive.

### Architecture

```
                          POST /api/v1/inject
                                 |
                          [Parse + Validate]
                                 |
                    +------------+------------+
                    |            |            |
              [Meilisearch] [CockroachDB]  [Gel DB]
              semantic       structured     behavioral
              search         queries        chains
                    |            |            |
                    +------------+------------+
                                 |
                          [Merge Candidates]
                                 |
                          [Rank + Select]
                                 |
                          [Recompose]
                                 |
                          [Response]
```

### 2.1 Meilisearch: Semantic Directive Search

**Purpose**: Find directives whose content is semantically relevant to the agent's current context.

**Query construction**: Build a search query from the `context.summary` and `activity` fields. The MCP plugin already summarized the conversation, so this summary becomes the search input directly.

```go
// Build the search query
query := request.Context.Summary
if request.Intent != "" {
    query = request.Intent + " " + query
}

// Meilisearch search request
searchReq := &meilisearch.SearchRequest{
    Limit:            30,
    Filter:           buildDirectiveFilter(request),
    ShowRankingScore: true,
    AttributesToRetrieve: []string{
        "id", "content", "category", "priority",
        "activity_tags", "project_tags", "language_tags",
    },
}
```

**Filter construction**: Narrow the search space before semantic matching.

```go
func buildDirectiveFilter(req *InjectRequest) string {
    filters := []string{}

    // Always filter to directives active for this activity
    filters = append(filters,
        fmt.Sprintf("activity_tags = '%s' OR activity_tags = 'all'", req.Activity))

    // Project-specific directives
    if req.Project.Name != "" {
        filters = append(filters,
            fmt.Sprintf("(project_tags = '%s' OR project_tags = 'all')", req.Project.Name))
    }

    // Language-specific directives
    if req.Project.Language != "" {
        filters = append(filters,
            fmt.Sprintf("(language_tags = '%s' OR language_tags = 'all')", req.Project.Language))
    }

    return strings.Join(filters, " AND ")
}
```

**Index configuration** (`directives` index):

```go
settings := &meilisearch.Settings{
    SearchableAttributes: []string{"content", "description", "tags"},
    FilterableAttributes: []string{
        "category", "priority", "activity_tags",
        "project_tags", "language_tags", "agent_id", "enabled",
    },
    SortableAttributes: []string{"priority", "created_at", "usage_count"},
    RankingRules: []string{
        "words", "typo", "proximity", "attribute", "sort", "exactness",
    },
}
```

**Timeout**: 150ms. Meilisearch typically responds in under 50ms. The 150ms budget allows for network latency and indexing delays.

**Fallback**: If Meilisearch is unavailable or exceeds the timeout, the pipeline continues with results from other sources. Meilisearch is the primary relevance signal but not the only one.

### 2.2 CockroachDB: Structured Queries

**Purpose**: Retrieve directives that match on structured criteria -- session history, agent preferences, project configuration, and pinned directives.

CockroachDB runs 4 queries in parallel within a single goroutine group:

**Query 1: Pinned directives for this project**

```sql
SELECT d.id, d.content, d.category, d.priority, d.metadata
FROM directives d
JOIN directive_pins dp ON d.id = dp.directive_id
WHERE dp.project = $1
  AND dp.enabled = true
  AND (dp.activity = $2 OR dp.activity = 'all')
ORDER BY d.priority DESC
LIMIT 10;
```

These are directives explicitly pinned to a project by the user. They always appear regardless of semantic relevance (they are "always-on" rules for that project).

**Query 2: Agent preferences and learned patterns**

```sql
SELECT d.id, d.content, d.category, d.priority, d.metadata
FROM directives d
JOIN agent_preferences ap ON d.id = ap.directive_id
WHERE ap.agent_id = $1
  AND ap.weight > 0.5
  AND d.enabled = true
ORDER BY ap.weight DESC, d.priority DESC
LIMIT 10;
```

These are directives that have been positively reinforced for this specific agent through the feedback loop (Section 7).

**Query 3: Session history deduplication**

```sql
SELECT directive_id
FROM injection_log
WHERE session_id = $1
  AND created_at > now() - interval '30 minutes'
ORDER BY created_at DESC
LIMIT 50;
```

Returns recently-injected directive IDs so the ranking step can penalize repetition.

**Query 4: User-specific directives**

```sql
SELECT d.id, d.content, d.category, d.priority, d.metadata
FROM directives d
WHERE d.scope = 'user'
  AND d.owner_id = $1
  AND d.enabled = true
  AND (d.activity_filter = $2 OR d.activity_filter = 'all')
ORDER BY d.priority DESC
LIMIT 10;
```

These are directives created by the user that apply to all their agents/projects (personal preferences).

**Timeout**: 100ms per query, 200ms total for the CockroachDB phase. These are indexed point lookups and short scans; they should complete in single-digit milliseconds under normal load.

**Fallback**: If CockroachDB is slow, partial results from completed queries are used. If entirely unavailable, the pipeline degrades to Meilisearch-only results plus any cached directives.

### 2.3 Gel DB: Behavioral Chains

**Purpose**: Retrieve behavioral sequences -- "if the agent is doing X, what should come next?" Gel DB's graph-relational model represents activity chains as linked nodes.

**Schema concept**:

```sdl
type BehavioralChain {
    required trigger_activity: str;
    required trigger_condition: str;
    required directive: Directive;
    required sequence_order: int32;
    next_chain: BehavioralChain;
    confidence: float64 { default := 1.0; };
}
```

**Query**: "Given the agent is implementing, what verification/follow-up directives should be suggested?"

```edgeql
SELECT BehavioralChain {
    directive: {
        id, content, category, priority
    },
    sequence_order,
    confidence,
    next_chain: {
        directive: { id, content, category, priority },
        sequence_order
    }
}
FILTER .trigger_activity = <str>$activity
   AND .confidence > 0.3
ORDER BY .confidence DESC
LIMIT 10;
```

**Examples of behavioral chains**:

| Trigger Activity | Trigger Condition | Directive                                                            |
| ---------------- | ----------------- | -------------------------------------------------------------------- |
| `implementing`   | file written      | "Run tests for the package you just modified."                       |
| `implementing`   | new handler added | "Verify the route is registered and reachable."                      |
| `debugging`      | error identified  | "Reproduce the error before attempting a fix."                       |
| `deploying`      | deploy command    | "Verify the test suite passes before deploying."                     |
| `reviewing`      | review started    | "Check for missing error handling and edge cases."                   |
| `planning`       | plan created      | "Estimate complexity and identify dependencies before implementing." |

**Timeout**: 200ms. Gel DB queries are more complex (graph traversal) but the dataset is small (hundreds of chains, not millions).

**Fallback**: Behavioral chains are a value-add, not critical. If Gel DB is unavailable, the pipeline returns results from Meilisearch and CockroachDB only. The response still works; it just lacks the "what should come next" signal.

### 2.4 Timeout and Fallback Strategy

```
Total pipeline deadline: 400ms
  |
  +-- Meilisearch:  150ms timeout, primary relevance signal
  +-- CockroachDB:  200ms timeout, structured/pinned directives
  +-- Gel DB:       200ms timeout, behavioral chains (optional)
  |
  +-- Merge window:  50ms (waits for stragglers after first results arrive)
  +-- Rank + Select: 50ms
  +-- Recompose:     50ms (template-based, no LLM)
  |
  = Total: 400ms + 150ms buffer = 550ms worst case
```

The fan-out uses `errgroup` with a shared context. The merge step starts as soon as any source returns. A 50ms merge window allows slower sources to contribute. After the merge window closes, ranking proceeds with whatever candidates have arrived.

**Degradation priority** (what to drop first):

1. Gel DB behavioral chains (nice-to-have)
2. Agent preferences from CockroachDB (learned, not critical)
3. Meilisearch semantic results (primary relevance)
4. Pinned directives from CockroachDB (never dropped -- these are explicit user config)

---

## 3. Ranking and Selection

The fan-out produces up to 50 candidate directives from all sources. These need to be reduced to fit within the token budget (default ~500 tokens, roughly 5-8 directives).

### 3.1 Scoring Model

Each candidate receives a composite score:

```
score = (relevance * 0.35) + (priority * 0.25) + (freshness * 0.15) + (source_boost * 0.15) + (feedback * 0.10)
```

| Factor         | Range     | How It's Computed                                                                                                                                                 |
| -------------- | --------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `relevance`    | 0.0 - 1.0 | Meilisearch `_rankingScore` for semantic results. 1.0 for pinned directives. 0.7 base for behavioral chains.                                                      |
| `priority`     | 0.0 - 1.0 | Normalized from the directive's category priority (guardrail=1.0, contextual=0.75, behavioral=0.5, procedural=0.25).                                              |
| `freshness`    | 0.0 - 1.0 | 1.0 if never injected this session. Decays by 0.2 for each time injected in the last 30 minutes. 0.0 if injected in the last 2 minutes.                           |
| `source_boost` | 0.0 - 1.0 | Bonus for coming from multiple sources. 0.5 base. +0.25 for each additional source that returned the same directive.                                              |
| `feedback`     | 0.0 - 1.0 | Historical feedback score for this directive with this agent. 0.5 default (no data). Higher if agent has followed it; lower if agent has consistently ignored it. |

### 3.2 Category Priority Tiers

Directives are categorized into tiers that establish minimum representation in the output:

| Category     | Priority | Min Slots | Description                                                                                        |
| ------------ | -------- | --------- | -------------------------------------------------------------------------------------------------- |
| `guardrail`  | 1.0      | 1         | Safety and correctness rules. "Never force-push to main." "Always run tests before claiming done." |
| `contextual` | 0.75     | 0         | Relevant to the current activity. "You're implementing a handler -- remember to add tests."        |
| `behavioral` | 0.5      | 0         | Learned patterns. "This agent tends to skip error handling -- remind about it."                    |
| `procedural` | 0.25     | 0         | Process rules. "Commit messages should use conventional commits format."                           |

Guardrails always get at least 1 slot if any guardrail directives are in the candidate pool. Remaining slots are filled by descending composite score.

### 3.3 Deduplication

Before scoring, candidates are deduplicated:

1. **Exact ID match**: Same directive returned by multiple sources. Merge metadata, boost `source_boost`. Keep highest relevance score.
2. **Content similarity**: Directives with Jaccard similarity > 0.7 on their tokenized content are grouped. The highest-scoring one in each group is kept. Others are dropped.
3. **Semantic overlap**: If two directives address the same concern (e.g., both say "run tests"), keep the more specific one (the one that mentions the actual test command for this project/language).

The deduplication step is fast because it operates on at most 50 candidates with short text fields.

### 3.4 Token Budget Packing

After scoring and deduplication, directives are packed into the token budget using a greedy algorithm:

```
sorted_candidates = sort by composite score descending
selected = []
remaining_budget = request.token_budget

// Pass 1: Guarantee minimum slots per category
for category in [guardrail, contextual, behavioral, procedural]:
    if category.min_slots > 0:
        best = highest-scoring candidate in this category
        if best and best.estimated_tokens <= remaining_budget:
            selected.append(best)
            remaining_budget -= best.estimated_tokens

// Pass 2: Fill remaining budget by score
for candidate in sorted_candidates:
    if candidate not in selected:
        if candidate.estimated_tokens <= remaining_budget:
            selected.append(candidate)
            remaining_budget -= candidate.estimated_tokens

// Pass 3: If budget remains, consider including a memory item
if remaining_budget > 50:
    best_memory = highest-relevance memory from agent's history
    if best_memory:
        selected.append(as_memory_directive(best_memory))
```

Token estimation uses a simple heuristic: `len(content) / 4` (roughly 4 characters per token for English text). This is intentionally conservative.

### 3.5 Breadth vs. Depth Balancing

The packing algorithm implicitly balances breadth by deduplicating overlapping directives. When the token budget is large (>1000 tokens), an explicit breadth check is applied:

- No more than 3 directives from the same category in a single injection.
- No more than 2 directives addressing the same file or package.

When the budget is small (<300 tokens), depth is prioritized: the single highest-scoring directive is returned with its full content rather than truncating multiple directives.

---

## 4. Recomposition

### 4.1 The Core Question: LLM or Templates?

**Templates for everything except memory contextualization.**

Directives are stored pre-formatted as micro-prompt snippets. They are written by humans (or by an LLM during a curation process) to be injected directly. The recomposition step does not use an LLM at runtime.

Reasons:

- **Latency**: An LLM call adds 500ms-2000ms. The entire pipeline budget is 400ms.
- **Predictability**: Pre-formatted directives produce consistent, tested agent behavior. LLM recomposition introduces variance.
- **Cost**: Running an LLM on every injection request at high frequency would dwarf the cost of all other operations.
- **Simplicity**: Template-based recomposition can be tested deterministically.

### 4.2 Directive Storage Format

Directives are stored with both a raw content field and template variables:

```json
{
  "id": "dir_test_after_impl",
  "content": "You just modified code in {{package}} -- run `{{test_command}}` to verify your changes before moving on.",
  "static_content": "After implementing changes, run the relevant test suite before claiming completion.",
  "category": "contextual",
  "priority": 3,
  "activity_tags": ["implementing", "refactoring"],
  "language_tags": ["go", "python", "rust"],
  "variables": {
    "test_command": {
      "go": "go test ./{{package}}/...",
      "python": "pytest {{package}}/",
      "rust": "cargo test -p {{package}}",
      "default": "run the test suite for the modified package"
    },
    "package": {
      "source": "context.recent_files",
      "transform": "extract_package_path"
    }
  }
}
```

### 4.3 Template Resolution

The recomposition step substitutes template variables using the request context:

```go
func recompose(directive *Directive, request *InjectRequest) string {
    content := directive.Content

    // Substitute language-specific commands
    if lang := request.Project.Language; lang != "" {
        for varName, variants := range directive.Variables {
            if cmd, ok := variants[lang]; ok {
                content = strings.ReplaceAll(content, "{{"+varName+"}}", cmd)
            } else if def, ok := variants["default"]; ok {
                content = strings.ReplaceAll(content, "{{"+varName+"}}", def)
            }
        }
    }

    // Substitute context-derived values
    if pkg := extractPackage(request.Context.RecentFiles); pkg != "" {
        content = strings.ReplaceAll(content, "{{package}}", pkg)
    }

    // Fall back to static content if any variables remain unresolved
    if strings.Contains(content, "{{") {
        return directive.StaticContent
    }

    return content
}
```

### 4.4 Contextualization Examples

**Generic directive (stored)**:

> After implementing changes, run the relevant test suite before claiming completion.

**Contextualized (after template resolution with Go project, `internal/handlers` package)**:

> You just modified code in internal/handlers -- run `go test ./internal/handlers/...` to verify your changes before moving on.

**Generic directive (stored)**:

> Before deploying, verify all tests pass and the build succeeds.

**Contextualized (with Go project)**:

> Before deploying, verify all tests pass (`go test ./...`) and the build succeeds (`go build ./cmd/app/`).

**Generic directive (stored, debugging)**:

> Reproduce the error before attempting a fix. Identify the root cause, not just the symptom.

**Contextualized (with error_context populated)**:

> Reproduce this error before attempting a fix: `{{error_context_first_line}}`. Identify the root cause, not just the symptom.

### 4.5 When Would an LLM Be Needed?

An LLM-based recomposition service could be added as a **background process** for:

1. **Memory contextualization**: Turning raw memory entries into contextual nudges (e.g., "Last time you worked on auth, you discovered that the token middleware needs to handle expired tokens gracefully -- keep that in mind."). This can be pre-computed and cached.
2. **Directive authoring**: When a user creates a new directive from natural language, an LLM can structure it into the template format.
3. **Feedback synthesis**: After accumulating enough feedback data, an LLM can suggest new directives or modifications to existing ones.

These are all offline/async operations, not in the hot path.

---

## 5. The Response

### Response Payload

```json
{
  "injection_id": "inj_789xyz",
  "directives": [
    {
      "id": "dir_test_after_impl",
      "content": "You just modified code in internal/handlers -- run `go test ./internal/handlers/...` to verify your changes before moving on.",
      "category": "contextual",
      "priority": "high",
      "action": "suggest",
      "source": "semantic+pinned",
      "confidence": 0.92
    },
    {
      "id": "dir_no_force_push",
      "content": "Never force-push to main or master. If you need to fix a commit, create a new commit instead.",
      "category": "guardrail",
      "priority": "critical",
      "action": "rule",
      "source": "pinned",
      "confidence": 1.0
    },
    {
      "id": "dir_error_handling",
      "content": "Wrap errors with context using fmt.Errorf(\"operation: %w\", err) rather than returning raw errors.",
      "category": "behavioral",
      "priority": "medium",
      "action": "suggest",
      "source": "semantic",
      "confidence": 0.78
    },
    {
      "id": "mem_auth_discovery",
      "content": "Previous work on this project found that the auth middleware silently drops requests when X-Agent-ID is empty -- ensure new handlers account for this.",
      "category": "memory",
      "priority": "medium",
      "action": "context",
      "source": "memory",
      "confidence": 0.71
    }
  ],
  "metadata": {
    "latency_ms": 187,
    "candidates_considered": 34,
    "sources_responded": ["meilisearch", "cockroachdb", "geldb"],
    "token_estimate": 412,
    "budget_remaining": 88
  }
}
```

### Response Field Specification

| Field                            | Type     | Description                                                                                         |
| -------------------------------- | -------- | --------------------------------------------------------------------------------------------------- |
| `injection_id`                   | string   | Unique ID for this injection. Used for feedback reporting and deduplication on subsequent calls.    |
| `directives`                     | array    | Ordered array of micro-prompt snippets. First item is highest priority.                             |
| `directives[].id`                | string   | Directive ID. Used for feedback reporting.                                                          |
| `directives[].content`           | string   | The micro-prompt text. Ready for direct injection into the agent's context.                         |
| `directives[].category`          | enum     | `guardrail`, `contextual`, `behavioral`, `procedural`, `memory`.                                    |
| `directives[].priority`          | enum     | `critical`, `high`, `medium`, `low`. Human-readable priority.                                       |
| `directives[].action`            | enum     | `rule` (must follow), `suggest` (should consider), `context` (background info).                     |
| `directives[].source`            | string   | Provenance. Which data sources contributed. Pipe-delimited when multiple (e.g., `semantic+pinned`). |
| `directives[].confidence`        | float    | 0.0 - 1.0. Composite confidence score.                                                              |
| `metadata.latency_ms`            | int      | Total pipeline latency.                                                                             |
| `metadata.candidates_considered` | int      | How many candidates were evaluated before selection.                                                |
| `metadata.sources_responded`     | []string | Which data sources responded within the timeout.                                                    |
| `metadata.token_estimate`        | int      | Estimated tokens consumed by the directive contents.                                                |
| `metadata.budget_remaining`      | int      | Tokens remaining in the budget after selection.                                                     |

### How the MCP Plugin Uses the Response

The MCP plugin injects the `directives[].content` values into the agent's system prompt or context window. The exact injection mechanism depends on the host platform:

- **Claude Code**: Injected as a `system-reminder` block appended to the system prompt.
- **Cursor/Copilot**: Injected as context in the MCP tool response that the agent processes.

The MCP plugin uses the `action` field to determine injection strength:

- `rule`: Injected as a hard constraint ("You MUST follow this rule: ...").
- `suggest`: Injected as a recommendation ("Consider: ...").
- `context`: Injected as background ("For your information: ...").

The MCP plugin stores the `injection_id` and passes it back on the next request as `previous_injection_id` for deduplication.

---

## 6. Latency Budget

### Target: < 300ms p50, < 500ms p95, < 800ms p99

The injection happens on every prompt. Users perceive delays over 500ms. The pipeline must stay fast.

### Timing Breakdown (p50 target)

| Phase                          | Target     | Notes                    |
| ------------------------------ | ---------- | ------------------------ |
| Request parsing + validation   | 1ms        | Trivial                  |
| Fan-out dispatch               | 1ms        | Goroutine launch         |
| Meilisearch query              | 30ms       | Typical sub-50ms         |
| CockroachDB queries (parallel) | 20ms       | Indexed lookups          |
| Gel DB query                   | 40ms       | Graph traversal          |
| Merge window                   | 10ms       | Wait for stragglers      |
| Ranking + selection            | 5ms        | In-memory, 50 candidates |
| Template recomposition         | 2ms        | String substitution      |
| Response serialization         | 1ms        | JSON marshal             |
| **Total**                      | **~110ms** | Well under 300ms target  |

### Caching Strategy

Three caching layers keep the pipeline fast:

**Layer 1: Directive cache (in-process, 5 minute TTL)**

Directives change rarely. The full directive set is cached in-memory on hive-server, refreshed every 5 minutes or on write. This eliminates CockroachDB queries for directive content lookups after the first request.

```go
type DirectiveCache struct {
    mu         sync.RWMutex
    directives map[string]*Directive  // id -> directive
    byProject  map[string][]string    // project -> directive IDs
    byActivity map[string][]string    // activity -> directive IDs
    refreshAt  time.Time
}
```

**Layer 2: Session injection log (in-process, session-scoped)**

Track which directives were injected in the current session to avoid querying CockroachDB for deduplication history on every request. Evicted when the session ends or after 30 minutes of inactivity.

```go
type SessionLog struct {
    mu        sync.RWMutex
    injected  map[string]time.Time  // directive_id -> last injected time
    sessionID string
}
```

**Layer 3: Meilisearch result cache (in-process, 60 second TTL)**

If the same agent sends very similar `context.summary` values within 60 seconds (common during rapid iteration), cache the Meilisearch results. Cache key is a hash of `activity + summary_first_100_chars`.

### What Happens When Things Are Slow

| Condition               | Response                                                                                                                            |
| ----------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| Meilisearch > 150ms     | Return results from CockroachDB and Gel DB only. Fewer candidates, but pinned directives and behavioral chains still provide value. |
| CockroachDB > 200ms     | Return Meilisearch semantic results only. Pinned directives come from the directive cache (Layer 1).                                |
| Gel DB > 200ms          | Drop behavioral chains. Not critical.                                                                                               |
| All sources > 400ms     | Return cached directives from Layer 1 based on activity + project match. No semantic relevance, but better than nothing.            |
| Entire pipeline > 800ms | Return empty directives array with metadata indicating timeout. The MCP plugin skips injection for this prompt.                     |

---

## 7. The Feedback Signal

After the agent acts on (or ignores) the injected directives, the MCP plugin reports back.

### Feedback Endpoint

```
POST /api/v1/inject/feedback
```

### Feedback Payload

```json
{
  "injection_id": "inj_789xyz",
  "session_id": "ses_abc123",
  "outcomes": [
    {
      "directive_id": "dir_test_after_impl",
      "followed": true,
      "outcome": "positive",
      "detail": "Agent ran go test ./internal/handlers/... and tests passed."
    },
    {
      "directive_id": "dir_error_handling",
      "followed": false,
      "outcome": "neutral",
      "detail": "Agent did not wrap errors in the new code."
    }
  ]
}
```

### Feedback Field Specification

| Field                     | Type   | Required | Description                                                                                                              |
| ------------------------- | ------ | -------- | ------------------------------------------------------------------------------------------------------------------------ |
| `injection_id`            | string | yes      | Which injection this feedback is for.                                                                                    |
| `session_id`              | string | yes      | Session context.                                                                                                         |
| `outcomes[]`              | array  | yes      | One entry per directive in the original injection.                                                                       |
| `outcomes[].directive_id` | string | yes      | Which directive.                                                                                                         |
| `outcomes[].followed`     | bool   | yes      | Did the agent follow the directive?                                                                                      |
| `outcomes[].outcome`      | enum   | yes      | `positive` (followed, good result), `negative` (followed, bad result), `neutral` (not followed or no observable effect). |
| `outcomes[].detail`       | string | no       | Brief description of what happened. Max 500 chars.                                                                       |

### How the MCP Plugin Determines Outcomes

The MCP plugin observes agent behavior after injection:

1. **Tool usage signals**: If a directive suggested running tests and the agent subsequently called a Bash tool with `go test`, that is `followed: true`.
2. **Absence signals**: If a directive suggested running tests and the agent proceeded to commit without testing, that is `followed: false`.
3. **Error signals**: If the agent followed a directive and encountered an error, that is `followed: true, outcome: negative`.
4. **Timeout**: If no observable action related to a directive occurs within 5 minutes (or by the next injection request), report `followed: false, outcome: neutral`.

### How Feedback Closes the Loop

Feedback data flows to two places:

**1. Agent preferences table (CockroachDB)**:

```sql
INSERT INTO agent_preferences (agent_id, directive_id, weight, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (agent_id, directive_id)
DO UPDATE SET
    weight = CASE
        WHEN $4 = 'positive' THEN LEAST(agent_preferences.weight + 0.1, 1.0)
        WHEN $4 = 'negative' THEN GREATEST(agent_preferences.weight - 0.2, 0.0)
        WHEN $4 = 'neutral' AND $5 = false THEN GREATEST(agent_preferences.weight - 0.05, 0.0)
        ELSE agent_preferences.weight
    END,
    updated_at = now();
```

Directives that consistently produce positive outcomes for an agent get boosted. Directives that are consistently ignored get demoted (but never removed -- they might be guardrails that the agent should follow even if it doesn't want to).

**2. Directive effectiveness metrics (CockroachDB)**:

```sql
INSERT INTO directive_metrics (directive_id, period, follow_count, ignore_count, positive_count, negative_count)
VALUES ($1, date_trunc('day', now()), 1, 0, 1, 0)
ON CONFLICT (directive_id, period)
DO UPDATE SET
    follow_count = directive_metrics.follow_count + CASE WHEN $2 THEN 1 ELSE 0 END,
    ignore_count = directive_metrics.ignore_count + CASE WHEN NOT $2 THEN 1 ELSE 0 END,
    positive_count = directive_metrics.positive_count + CASE WHEN $3 = 'positive' THEN 1 ELSE 0 END,
    negative_count = directive_metrics.negative_count + CASE WHEN $3 = 'negative' THEN 1 ELSE 0 END;
```

This produces aggregate metrics on directive effectiveness: "Directive X is followed 80% of the time and produces positive outcomes 90% of the time" vs. "Directive Y is ignored 70% of the time." Over time, this data informs directive curation -- poorly performing directives can be rewritten or retired.

---

## 8. End-to-End Scenarios

### Scenario 1: New Session Start (Cold Start)

The agent has just been initialized. No prior context exists for this session.

**Request**:

```json
{
  "session_id": "ses_new001",
  "activity": "starting",
  "project": {
    "name": "hive-server",
    "language": "go",
    "path": "/Users/dev/git/hive-server"
  },
  "context": {
    "summary": "Starting a new session on the hive-server project. No specific task yet.",
    "recent_files": [],
    "recent_tools": [],
    "error_context": null
  },
  "intent": null,
  "token_budget": 500,
  "previous_injection_id": null
}
```

**Fan-out**:

- **Meilisearch**: Searches `"Starting new session hive-server project"`. Returns project-general directives (coding conventions, testing patterns, project structure reminders).
- **CockroachDB**: Queries pinned directives for `hive-server` project. Returns 3 pinned directives (conventional commits rule, test-before-merge rule, Go error handling convention). No session history (new session). Agent preferences loaded.
- **Gel DB**: Queries behavioral chains for `starting` activity. Returns "Orient the agent to the codebase before diving in" chain.

**Ranking**: 7 candidates after dedup. Guardrail directives score highest. Project orientation directive from Gel DB scores well for `starting` activity.

**Response**:

```json
{
  "injection_id": "inj_cold001",
  "directives": [
    {
      "id": "dir_conventional_commits",
      "content": "This project uses conventional commits. Format: type(scope): description. Types: feat, fix, docs, refactor, test, chore.",
      "category": "procedural",
      "priority": "medium",
      "action": "rule",
      "source": "pinned",
      "confidence": 1.0
    },
    {
      "id": "dir_test_before_merge",
      "content": "Always run `go test ./...` and verify all tests pass before claiming any implementation is complete.",
      "category": "guardrail",
      "priority": "critical",
      "action": "rule",
      "source": "pinned",
      "confidence": 1.0
    },
    {
      "id": "dir_project_orientation",
      "content": "hive-server structure: cmd/app/ (CLI entrypoint), internal/handlers/ (HTTP handlers, chi router), internal/store/ (persistence layer). Read CLAUDE.md for project conventions.",
      "category": "contextual",
      "priority": "medium",
      "action": "context",
      "source": "behavioral",
      "confidence": 0.85
    },
    {
      "id": "dir_go_error_wrap",
      "content": "Wrap errors with context: fmt.Errorf(\"operation: %w\", err). Use sentinel errors (ErrNotFound, ErrConflict) for expected failure modes.",
      "category": "behavioral",
      "priority": "medium",
      "action": "suggest",
      "source": "pinned+semantic",
      "confidence": 0.9
    }
  ],
  "metadata": {
    "latency_ms": 95,
    "candidates_considered": 7,
    "sources_responded": ["meilisearch", "cockroachdb", "geldb"],
    "token_estimate": 347,
    "budget_remaining": 153
  }
}
```

**Latency**: 95ms. CockroachDB pinned directive lookup was fastest (12ms). Meilisearch returned in 35ms. Gel DB returned in 68ms.

---

### Scenario 2: Mid-Brainstorm

The agent is actively exploring ideas with the user about how to implement the injection pipeline.

**Request**:

```json
{
  "session_id": "ses_brain002",
  "activity": "brainstorming",
  "project": {
    "name": "hive-server",
    "language": "go",
    "path": "/Users/dev/git/hive-server"
  },
  "context": {
    "summary": "Brainstorming the injection pipeline architecture. Discussing whether to use an LLM for recomposition or template-based approach. Considering latency tradeoffs.",
    "recent_files": ["context/injection-pipeline.md"],
    "recent_tools": ["Read", "Write"],
    "error_context": null
  },
  "intent": "Design the injection pipeline for hive-server",
  "token_budget": 400,
  "previous_injection_id": "inj_brain001"
}
```

**Fan-out**:

- **Meilisearch**: Searches `"Design injection pipeline hive-server. Brainstorming injection pipeline architecture LLM recomposition template-based latency tradeoffs."` Returns directives about design thinking, architecture decision records, and API design patterns.
- **CockroachDB**: Pinned directives for hive-server (same as always). Session history shows `inj_brain001` was delivered 3 minutes ago -- those directives get freshness penalty. Agent preferences loaded.
- **Gel DB**: Behavioral chains for `brainstorming` return "capture decisions in writing" and "consider failure modes" chains.

**Ranking**: 12 candidates. The previous injection's directives get demoted by freshness decay. Brainstorming-specific directives score high.

**Response**:

```json
{
  "injection_id": "inj_brain002",
  "directives": [
    {
      "id": "dir_capture_decisions",
      "content": "As you explore options, capture key decisions and their rationale. Record what was considered and why alternatives were rejected.",
      "category": "behavioral",
      "priority": "medium",
      "action": "suggest",
      "source": "behavioral",
      "confidence": 0.82
    },
    {
      "id": "dir_consider_failure",
      "content": "For each component in the design, ask: what happens when this fails? Design the degradation path before the happy path.",
      "category": "contextual",
      "priority": "medium",
      "action": "suggest",
      "source": "semantic+behavioral",
      "confidence": 0.79
    },
    {
      "id": "dir_latency_budget",
      "content": "When designing request pipelines, define explicit latency budgets per stage. The total budget is the user-facing constraint; each stage gets a fraction.",
      "category": "contextual",
      "priority": "medium",
      "action": "suggest",
      "source": "semantic",
      "confidence": 0.75
    }
  ],
  "metadata": {
    "latency_ms": 142,
    "candidates_considered": 12,
    "sources_responded": ["meilisearch", "cockroachdb", "geldb"],
    "token_estimate": 278,
    "budget_remaining": 122
  }
}
```

**Latency**: 142ms. Meilisearch took slightly longer (62ms) because the query was more complex. Directives from `inj_brain001` were filtered out by freshness decay.

---

### Scenario 3: Debugging a Test Failure

The agent is investigating a failing test.

**Request**:

```json
{
  "session_id": "ses_debug003",
  "activity": "debugging",
  "project": {
    "name": "hive-server",
    "language": "go",
    "path": "/Users/dev/git/hive-server"
  },
  "context": {
    "summary": "Test TestUpsertMemory is failing with 'unexpected status code: got 409, want 200'. Investigating the optimistic concurrency logic in the store layer.",
    "recent_files": [
      "internal/store/memory.go",
      "internal/store/memory_test.go",
      "internal/handlers/memory.go"
    ],
    "recent_tools": ["Bash", "Read", "Grep"],
    "error_context": "--- FAIL: TestUpsertMemory (0.02s)\n    memory_test.go:45: unexpected status code: got 409, want 200\n    memory_test.go:46: response body: {\"error\":\"conflict\",\"message\":\"version mismatch\"}"
  },
  "intent": null,
  "token_budget": 600,
  "previous_injection_id": "inj_debug002"
}
```

**Fan-out**:

- **Meilisearch**: Searches on the error context. Returns directives about optimistic concurrency debugging, version conflict handling, and test isolation.
- **CockroachDB**: Pinned directives. Session history. Agent preferences. Also queries memory store for any past memories about this test or this error pattern.
- **Gel DB**: Behavioral chains for `debugging` return the systematic debugging sequence: reproduce -> isolate -> identify root cause -> fix -> verify.

**Ranking**: 18 candidates. Debugging-specific directives score highest. The systematic debugging chain from Gel DB scores well because `activity=debugging`.

**Response**:

```json
{
  "injection_id": "inj_debug003",
  "directives": [
    {
      "id": "dir_reproduce_first",
      "content": "Before attempting a fix, reproduce the error reliably. Run `go test -run TestUpsertMemory -v ./internal/store/...` and confirm you see the same 409 response.",
      "category": "guardrail",
      "priority": "critical",
      "action": "rule",
      "source": "behavioral+semantic",
      "confidence": 0.95
    },
    {
      "id": "dir_root_cause",
      "content": "The 409 (version mismatch) means the optimistic concurrency check is rejecting the update. Check whether the test is sending the correct version number, and whether a previous test case is leaving stale state.",
      "category": "contextual",
      "priority": "high",
      "action": "suggest",
      "source": "semantic",
      "confidence": 0.88
    },
    {
      "id": "dir_test_isolation",
      "content": "Go tests within a package run sequentially by default, but state may leak between test functions if they share a database. Verify each test creates a fresh store or uses a unique key prefix.",
      "category": "contextual",
      "priority": "high",
      "action": "suggest",
      "source": "semantic",
      "confidence": 0.82
    },
    {
      "id": "dir_debug_sequence",
      "content": "Systematic debugging: (1) Reproduce reliably, (2) Isolate the failing component, (3) Identify the root cause with evidence, (4) Fix only the root cause, (5) Verify the fix and run the full test suite.",
      "category": "behavioral",
      "priority": "medium",
      "action": "suggest",
      "source": "behavioral",
      "confidence": 0.8
    },
    {
      "id": "mem_concurrency_note",
      "content": "Previous debugging session on this project found that the store's UpsertMemory increments version on every call, including the initial insert. Tests that call Upsert twice need to pass version=2 on the second call.",
      "category": "memory",
      "priority": "high",
      "action": "context",
      "source": "memory",
      "confidence": 0.76
    }
  ],
  "metadata": {
    "latency_ms": 203,
    "candidates_considered": 18,
    "sources_responded": ["meilisearch", "cockroachdb", "geldb"],
    "token_estimate": 521,
    "budget_remaining": 79
  }
}
```

**Latency**: 203ms. Higher than typical because Meilisearch searched against the longer `error_context` field (105ms), and CockroachDB ran a memory search query in addition to the standard queries.

---

### Scenario 4: Planning a Multi-File Change

The agent needs to implement a new feature that touches multiple packages.

**Request**:

```json
{
  "session_id": "ses_plan004",
  "activity": "planning",
  "project": {
    "name": "hive-server",
    "language": "go",
    "path": "/Users/dev/git/hive-server"
  },
  "context": {
    "summary": "Planning the implementation of the /api/v1/inject endpoint. Need to add: new handler, new store methods for directives, Meilisearch integration, and response types. Multiple packages will be modified.",
    "recent_files": [
      "internal/handlers/handlers.go",
      "internal/store/store.go",
      "context/injection-pipeline.md"
    ],
    "recent_tools": ["Read"],
    "error_context": null
  },
  "intent": "Implement the injection pipeline endpoint",
  "token_budget": 500,
  "previous_injection_id": null
}
```

**Fan-out**:

- **Meilisearch**: Searches on intent + summary. Returns directives about API design, multi-package changes, dependency ordering, and interface design.
- **CockroachDB**: Pinned directives. Queries for project-specific directives about hive-server's architecture patterns (Store interface, handler registration pattern).
- **Gel DB**: Behavioral chains for `planning` return "identify dependencies first", "design interfaces before implementations", and "estimate scope" chains.

**Response**:

```json
{
  "injection_id": "inj_plan004",
  "directives": [
    {
      "id": "dir_interface_first",
      "content": "For multi-package changes in this codebase, define or extend the Store interface first (internal/handlers/handlers.go), then implement the store methods, then add the handlers. This ensures type-checking catches integration issues early.",
      "category": "contextual",
      "priority": "high",
      "action": "suggest",
      "source": "pinned+semantic",
      "confidence": 0.91
    },
    {
      "id": "dir_dependency_order",
      "content": "Implementation order for new endpoints: (1) data types/models, (2) store interface methods, (3) store implementation, (4) store tests, (5) handler implementation, (6) handler tests, (7) route registration.",
      "category": "procedural",
      "priority": "medium",
      "action": "suggest",
      "source": "behavioral+semantic",
      "confidence": 0.87
    },
    {
      "id": "dir_test_before_merge",
      "content": "Always run `go test ./...` and verify all tests pass before claiming any implementation is complete.",
      "category": "guardrail",
      "priority": "critical",
      "action": "rule",
      "source": "pinned",
      "confidence": 1.0
    },
    {
      "id": "dir_scope_check",
      "content": "Multi-file changes are high-risk for scope creep. Define the minimal set of files to touch and the specific changes in each before starting. If more than 8 files need changes, consider breaking into smaller PRs.",
      "category": "behavioral",
      "priority": "medium",
      "action": "suggest",
      "source": "semantic",
      "confidence": 0.79
    }
  ],
  "metadata": {
    "latency_ms": 156,
    "candidates_considered": 15,
    "sources_responded": ["meilisearch", "cockroachdb", "geldb"],
    "token_estimate": 438,
    "budget_remaining": 62
  }
}
```

**Latency**: 156ms. The `intent` field gave Meilisearch a strong signal, producing highly relevant results quickly (41ms).

---

### Scenario 5: User Asks to Deploy

The agent needs to verify and validate before proceeding with deployment.

**Request**:

```json
{
  "session_id": "ses_deploy005",
  "activity": "deploying",
  "project": {
    "name": "hive-server",
    "language": "go",
    "path": "/Users/dev/git/hive-server"
  },
  "context": {
    "summary": "User asked to deploy hive-server to the staging Kubernetes cluster. Preparing to run deployment commands.",
    "recent_files": [
      "k8s/deployment.yaml",
      "k8s/kustomization.yaml",
      ".github/workflows/release.yaml"
    ],
    "recent_tools": ["Read", "Bash"],
    "error_context": null
  },
  "intent": "Deploy to staging",
  "token_budget": 600,
  "previous_injection_id": "inj_deploy004"
}
```

**Fan-out**:

- **Meilisearch**: Searches on deployment context. Returns directives about pre-deployment checks, k8s deployment verification, and rollback procedures.
- **CockroachDB**: Pinned directives. Deployment-specific directives for this project.
- **Gel DB**: Behavioral chains for `deploying` return a strong sequence: test -> build -> verify image -> deploy -> verify health -> monitor.

**Response**:

```json
{
  "injection_id": "inj_deploy005",
  "directives": [
    {
      "id": "dir_pre_deploy_check",
      "content": "BEFORE deploying: (1) Run `go test ./...` and confirm all tests pass. (2) Verify the Docker image builds: `docker build -t hive-server:test .`. (3) Check that the current branch is clean: `git status`. Do NOT proceed until all three checks pass.",
      "category": "guardrail",
      "priority": "critical",
      "action": "rule",
      "source": "pinned+behavioral",
      "confidence": 1.0
    },
    {
      "id": "dir_deploy_verify",
      "content": "After deploying, verify the deployment is healthy: (1) `kubectl rollout status deployment/hive-server -n staging`, (2) `kubectl get pods -n staging -l app=hive-server`, (3) Hit the health endpoint: `curl https://staging.hive-server/health`.",
      "category": "contextual",
      "priority": "high",
      "action": "suggest",
      "source": "behavioral+semantic",
      "confidence": 0.93
    },
    {
      "id": "dir_deploy_rollback",
      "content": "If the deployment fails or the health check does not pass, roll back immediately: `kubectl rollout undo deployment/hive-server -n staging`. Do not debug a broken deployment in staging -- roll back first, then debug locally.",
      "category": "guardrail",
      "priority": "critical",
      "action": "rule",
      "source": "pinned",
      "confidence": 1.0
    },
    {
      "id": "dir_no_prod_deploy",
      "content": "This project uses CI/CD for production deployments. Never deploy directly to production from the command line. Production deploys happen through the release.yaml workflow triggered by semantic release.",
      "category": "guardrail",
      "priority": "critical",
      "action": "rule",
      "source": "pinned",
      "confidence": 1.0
    },
    {
      "id": "mem_staging_config",
      "content": "The staging cluster uses namespace 'staging' and requires HIVE_TOKEN to be set. Last deployment used image tag v0.3.2 from ghcr.io/christmas-island/hive-server.",
      "category": "memory",
      "priority": "medium",
      "action": "context",
      "source": "memory",
      "confidence": 0.84
    }
  ],
  "metadata": {
    "latency_ms": 178,
    "candidates_considered": 22,
    "sources_responded": ["meilisearch", "cockroachdb", "geldb"],
    "token_estimate": 573,
    "budget_remaining": 27
  }
}
```

**Latency**: 178ms. Deployment directives are heavily pinned, so CockroachDB contributed the most candidates (fast indexed lookups). The `deploying` activity triggered strong behavioral chains from Gel DB (pre-deploy verification sequence). Guardrail directives consumed most of the token budget (3 of 5 directives are guardrails), which is correct -- deployment is a high-risk activity.

---

## Appendix: Data Model Summary

### New Tables (CockroachDB)

```sql
-- Directive definitions
CREATE TABLE directives (
    id              TEXT PRIMARY KEY,
    content         TEXT NOT NULL,           -- Template with {{variables}}
    static_content  TEXT NOT NULL,           -- Fallback without variables
    description     TEXT NOT NULL DEFAULT '',
    category        TEXT NOT NULL,           -- guardrail, contextual, behavioral, procedural
    priority        INTEGER NOT NULL DEFAULT 2,
    activity_filter TEXT NOT NULL DEFAULT 'all',
    scope           TEXT NOT NULL DEFAULT 'global',  -- global, project, user
    owner_id        TEXT NOT NULL DEFAULT '',
    enabled         BOOLEAN NOT NULL DEFAULT true,
    variables       JSONB NOT NULL DEFAULT '{}',
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Project-specific pinning
CREATE TABLE directive_pins (
    directive_id TEXT NOT NULL REFERENCES directives(id) ON DELETE CASCADE,
    project      TEXT NOT NULL,
    activity     TEXT NOT NULL DEFAULT 'all',
    enabled      BOOLEAN NOT NULL DEFAULT true,
    PRIMARY KEY (directive_id, project)
);

-- Agent-specific preference weights (feedback loop)
CREATE TABLE agent_preferences (
    agent_id     TEXT NOT NULL,
    directive_id TEXT NOT NULL REFERENCES directives(id) ON DELETE CASCADE,
    weight       REAL NOT NULL DEFAULT 0.5,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, directive_id)
);

-- Injection audit log
CREATE TABLE injection_log (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL,
    agent_id     TEXT NOT NULL,
    directive_ids JSONB NOT NULL DEFAULT '[]',
    latency_ms   INTEGER NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_injection_log_session ON injection_log(session_id, created_at DESC);

-- Directive effectiveness metrics (aggregated daily)
CREATE TABLE directive_metrics (
    directive_id   TEXT NOT NULL REFERENCES directives(id) ON DELETE CASCADE,
    period         DATE NOT NULL,
    follow_count   INTEGER NOT NULL DEFAULT 0,
    ignore_count   INTEGER NOT NULL DEFAULT 0,
    positive_count INTEGER NOT NULL DEFAULT 0,
    negative_count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (directive_id, period)
);
```

### Meilisearch Index

Index name: `directives`

Documents mirror the `directives` table with additional denormalized fields:

```json
{
  "id": "dir_test_after_impl",
  "content": "You just modified code in {{package}} -- run `{{test_command}}` to verify your changes before moving on.",
  "description": "Remind agent to run tests after implementing changes.",
  "category": "contextual",
  "priority": 3,
  "activity_tags": ["implementing", "refactoring"],
  "project_tags": ["all"],
  "language_tags": ["go", "python", "rust"],
  "enabled": true,
  "usage_count": 1547,
  "created_at": "2026-03-01T00:00:00Z"
}
```

### Gel DB Schema

```sdl
type BehavioralChain {
    required trigger_activity: str;
    required trigger_condition: str;
    required directive: Directive;
    required sequence_order: int32;
    next_chain: BehavioralChain;
    confidence: float64 { default := 1.0; };
    created_at: datetime { default := datetime_current(); };
}

type Directive {
    required external_id: str { constraint exclusive; };
    required content: str;
    required category: str;
    multi chains: BehavioralChain;
}
```

This schema enables queries like "given the agent is implementing, what verification steps should follow?" by traversing the `next_chain` links from the `implementing` trigger to the verification directives.
