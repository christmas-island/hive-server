# Injection Pipeline Design v5

**Date:** 2026-03-09
**Status:** Supersedes injection-pipeline.md
**Authoritative source:** vision-v5.md
**Core principle:** Accuracy over speed. The injection pipeline delivers contextually synthesized behavioral directives to LLM agents. The LLM recomposition step takes 2.5-3.5 seconds. This is acceptable. Retrieval fan-out should be fast but has no hard deadline.

This document specifies the full pipeline from request to response. It aligns with the vision v5 architecture and the recomposition-design.md for LLM-based synthesis.

---

## 1. The Request

### Endpoint

```
POST /api/v1/inject
Authorization: Bearer <token>
X-Agent-ID: agent-007
```

No additional auth beyond the existing Bearer token. The `X-Agent-ID` header is required (not optional as it is for other endpoints). The agent ID is extracted from the header by middleware and injected into the request context.

### Request Payload

```json
{
  "agent_id": "agent-007",
  "session_id": "sess_xyz",
  "context": {
    "intent": "debugging a failing test in the memory store",
    "files": ["internal/store/memory_test.go", "internal/store/memory.go"],
    "repo": "christmas-island/hive-server",
    "phase": "debugging",
    "recent_actions": [
      "read internal/store/memory_test.go",
      "ran go test -run TestUpsertMemory",
      "observed: FAIL - expected version 2, got version 1"
    ],
    "conversation_summary": "Agent is investigating why optimistic concurrency check fails on upsert. The test creates a memory entry, updates it, and checks the version is incremented. The version stays at 1.",
    "open_requirements": ["AUTH-01", "AUTH-03"],
    "current_project": "proj_abc123"
  },
  "token_budget": 500,
  "previous_injection_id": "inj_prev123"
}
```

### Field Specification

| Field                          | Type     | Required | Description                                                                                                                                      |
| ------------------------------ | -------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| `agent_id`                     | string   | yes      | Agent identifier. Also sent via `X-Agent-ID` header.                                                                                             |
| `session_id`                   | string   | yes      | Stable identifier for the conversation session. Used for session history lookups and deduplication.                                              |
| `context.intent`               | string   | no       | What the agent is trying to do. When present, used as a primary signal for semantic search.                                                      |
| `context.files`                | []string | no       | Files the agent is working with. Used for scope matching and project-area relevance.                                                             |
| `context.repo`                 | string   | no       | Repository identifier. Used for scope filtering (e.g., `trigger_scope = 'repo:christmas-island/hive-server'`).                                   |
| `context.phase`                | string   | yes      | One of: `planning`, `implementation`, `debugging`, `review`, `brainstorming`. Drives which directives are relevant via `trigger_phase` matching. |
| `context.recent_actions`       | []string | no       | Last 3-5 agent actions. Provides behavioral context for the recomposition LLM.                                                                   |
| `context.conversation_summary` | string   | yes      | 1-3 sentence summary of what the agent is currently doing. This is the primary semantic search input for Meilisearch.                            |
| `context.open_requirements`    | []string | no       | Requirement IDs currently in scope. Enables contextual directives about relevant requirements.                                                   |
| `context.current_project`      | string   | no       | Project ID if applicable. Used for project-scoped directive filtering.                                                                           |
| `token_budget`                 | int      | no       | Maximum tokens for the response payload. Default 500. Server-side validation applies reasonable bounds.                                          |
| `previous_injection_id`        | string   | no       | ID of the last injection response this agent received. Used for deduplication (avoid repeating the same directives consecutively).               |

### Phase Enum

The `context.phase` field uses vision v5's 5 workflow phases:

| Phase            | When the MCP plugin sends this                                             |
| ---------------- | -------------------------------------------------------------------------- |
| `planning`       | Agent is designing, outlining an approach, or decomposing work into tasks. |
| `implementation` | Agent is actively writing or editing code.                                 |
| `debugging`      | Agent is investigating a failure or unexpected behavior.                   |
| `review`         | Agent is reviewing code, either its own or external.                       |
| `brainstorming`  | Agent is exploring ideas with the user. No code changes yet.               |

### When Does the MCP Plugin Call Inject?

The MCP plugin does not call inject on every user message. It calls inject on **context transitions**:

1. **Session start**: Agent begins a new session. The plugin sends a context frame with the repo, any open project, and initial context.
2. **Phase transition**: Agent shifts from brainstorming to planning, or from planning to implementation.
3. **Significant action**: Agent starts debugging, switches files, encounters an error.
4. **Periodic refresh**: Every N minutes (configurable, default 5), if the agent is still active.
5. **Explicit request**: The agent or user requests a refresh via the MCP tool.

---

## 2. The Retrieval Fan-Out

When hive-server receives the inject request, it launches parallel queries to all three data sources. Results are merged as they arrive. The fan-out uses an `errgroup` with a shared context. Sources that respond contribute their candidates; sources that fail or are slow are skipped gracefully.

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
                          [LLM Recomposition]
                                 |
                          [Response]
```

### 2.1 Meilisearch: Semantic Directive Search

**Purpose**: Find directives whose content is semantically relevant to the agent's current context.

**Query construction**: Build a search query from the `context.conversation_summary` and `context.intent` fields. The MCP plugin already summarized the conversation, so this summary becomes the search input. The 10-word query limit is managed by extracting key terms from the context frame before searching.

```go
query := request.Context.ConversationSummary
if request.Context.Intent != "" {
    query = request.Context.Intent + " " + query
}
```

**Filter construction**: Narrow the search space before semantic matching. All queries are scoped to the tenant.

```go
func buildDirectiveFilter(req *InjectRequest, tenantID string) string {
    filters := []string{}

    // Always scope to tenant
    filters = append(filters, fmt.Sprintf("tenant_id = '%s'", tenantID))

    // Only active directives
    filters = append(filters, "active = true")

    // Phase-specific directives
    filters = append(filters,
        fmt.Sprintf("(trigger_phase = '%s' OR trigger_phase = 'any')", req.Context.Phase))

    // Scope filtering
    if req.Context.Repo != "" {
        filters = append(filters,
            fmt.Sprintf("(trigger_scope = 'global' OR trigger_scope = 'repo:%s')", req.Context.Repo))
    }

    if req.Context.CurrentProject != "" {
        filters = append(filters,
            fmt.Sprintf("(trigger_scope = 'global' OR trigger_scope = 'project:%s')", req.Context.CurrentProject))
    }

    return strings.Join(filters, " AND ")
}
```

**Index configuration** (per vision Section 5.2):

```json
{
  "searchableAttributes": [
    "content",
    "trigger_intent",
    "trigger_tags",
    "source_name"
  ],
  "filterableAttributes": [
    "kind",
    "trigger_phase",
    "trigger_scope",
    "active",
    "tenant_id",
    "chain_id"
  ],
  "sortableAttributes": ["effectiveness", "weight", "created_at"],
  "rankingRules": [
    "words",
    "typo",
    "proximity",
    "attribute",
    "sort",
    "exactness"
  ],
  "synonyms": {
    "debug": ["fix", "investigate", "troubleshoot", "diagnose"],
    "test": ["spec", "assertion", "verification", "check"],
    "plan": ["design", "architect", "decompose", "structure"],
    "review": ["audit", "inspect", "evaluate", "assess"]
  }
}
```

**Fallback**: If Meilisearch is unavailable or slow, the pipeline continues with results from other sources. Meilisearch is the primary relevance signal but not the only one.

### 2.2 CockroachDB: Structured Queries

**Purpose**: Retrieve directives that match on structured criteria -- phase, scope, effectiveness, and session history for deduplication.

CockroachDB runs queries in parallel within a single goroutine group. All queries include `tenant_id` filtering.

**Query 1: Phase-matched and scope-matched directives**

```sql
SELECT id, content, kind, weight, effectiveness, token_cost
FROM directives
WHERE active = true
  AND tenant_id = $1
  AND (trigger_phase = $2 OR trigger_phase = 'any')
  AND (trigger_scope = 'global'
       OR trigger_scope = $3          -- repo scope
       OR trigger_scope = $4)         -- project scope
ORDER BY effectiveness DESC, weight DESC
LIMIT 50;
```

**Query 2: Session history deduplication**

```sql
SELECT directive_ids
FROM injections
WHERE session_id = $1
  AND created_at > now() - interval '30 minutes'
ORDER BY created_at DESC
LIMIT 10;
```

Returns recently-injected directive IDs so the ranking step can penalize repetition.

**Fallback**: If CockroachDB is slow, partial results from completed queries are used. If entirely unavailable, the pipeline degrades to Meilisearch-only results.

### 2.3 Gel DB: Behavioral Chains

**Purpose**: Retrieve behavioral sequences -- "if the agent is doing X, what should come next?" Gel DB's graph-relational model represents directive chains as linked nodes.

**Query**: Starting from directives found by Meilisearch and CRDB, traverse the behavioral chain graph to find related directives that form a coherent sequence:

```edgeql
SELECT Directive {
    content,
    kind,
    weight,
    effectiveness,
    token_cost,
    chain := .<member[IS DirectiveChain] {
        name,
        directives: {
            content,
            kind,
            weight,
            effectiveness
        } ORDER BY .sequence_order
    }
}
FILTER .id IN <uuid>array_unpack(<array<uuid>>$found_ids)
```

If a debugging directive is found, Gel also retrieves the chain it belongs to -- the full debugging methodology sequence (reproduce -> hypothesize -> test -> fix -> verify) -- so the injection can include the relevant next step.

**Fallback**: Behavioral chains are a value-add, not critical. If Gel DB is unavailable, the pipeline returns results from Meilisearch and CockroachDB only. The response still works; it lacks the "what should come next" signal.

### 2.4 Degradation Strategy

The fan-out uses `errgroup` with a shared context. The merge step starts as soon as any source returns. Sources that fail or are slow are skipped. There is no hard pipeline deadline -- the retrieval fan-out targets fast completion (sub-100ms) but degrades gracefully.

**Degradation priority** (what to drop first):

1. Gel DB behavioral chains (nice-to-have)
2. Meilisearch semantic results (primary relevance but replaceable with CRDB filtering)
3. CockroachDB structured results (source of truth, last to drop)

If all sources fail, return an empty directives array. The MCP plugin skips injection for this prompt.

---

## 3. Ranking and Selection

The fan-out produces candidate directives from all sources. These need to be merged, deduplicated, and reduced to fit within the token budget.

### 3.1 Scoring Model

Each candidate receives a composite score per vision Section 4.4:

```
score = (meilisearch_relevance * 0.4)
      + (effectiveness * 0.3)
      + (weight * 0.2)
      + (recency_bonus * 0.1)
```

Where:

- `meilisearch_relevance`: 0.0-1.0, Meilisearch's `_rankingScore` indicating how semantically relevant this directive is to the current context. For directives found only via CRDB or Gel, this defaults to 0.5 (neutral).
- `effectiveness`: 0.0-1.0, historical success rate. Computed as `(times_followed - times_negative) / GREATEST(times_injected, 1)`. Stored on the directive row.
- `weight`: 0.0-2.0, priority weight assigned during enrichment. Anti-rationalization directives from Superpowers' "1% rule" philosophy get higher initial weight because they counter known LLM failure modes.
- `recency_bonus`: 0.0-0.5, bonus for directives from the agent's recent experience with this repo. Experience-derived corrective directives that are recent get a boost.

### 3.2 Deduplication

Before scoring, candidates are deduplicated:

1. **Exact ID match**: Same directive returned by multiple sources. Merge metadata, keep highest relevance score.
2. **Semantic overlap**: Directives addressing the same concern are detected and the most specific, actionable version is kept. The deduplication approach uses semantic similarity (via Meilisearch's hybrid search or a dedicated embedding) to find near-duplicates. When duplicates are found, the most specific version is kept and provenance is preserved across sources.

The deduplication step operates on a bounded set of candidates (typically under 50) with short text fields, so it is fast.

### 3.3 Token Budget Packing

After scoring and deduplication, directives are packed into the token budget. Each directive has a `token_cost` field populated during enrichment (when the directive was created). This is used for budget accounting instead of runtime estimation heuristics.

```
sorted_candidates = sort by composite score descending
selected = []
remaining_budget = request.token_budget

for candidate in sorted_candidates:
    if candidate.token_cost <= remaining_budget:
        selected.append(candidate)
        remaining_budget -= candidate.token_cost

// Reserve tokens for the injection frame wrapper
```

A 500-token budget typically fits 8-12 directives. A 200-token budget fits 3-5.

### 3.4 Avoiding Overwhelm

The injection pipeline has several mechanisms to avoid overwhelming the agent:

1. **Token budget**: Hard cap on injection size. The MCP plugin decides how much context to spend.
2. **Diminishing returns**: If the top-scored directive has confidence 0.94 and the 8th has 0.31, the pipeline stops at the natural dropoff point rather than filling the budget with low-confidence noise.
3. **Session deduplication**: The `previous_injection_id` field lets the pipeline avoid re-injecting directives from the last call. If the agent is still debugging the same thing, it gets new directives, not the same ones repeated.
4. **Phase gating**: Only directives matching the current phase are candidates. If the agent is debugging, it does not receive planning directives.
5. **Cooldown**: A directive that was injected and ignored in the last 3 calls gets a temporary weight reduction. The agent clearly does not need it right now.

---

## 4. Recomposition

Section 4 of the original injection-pipeline.md proposed template-based recomposition: store directives with `{{variable}}` placeholders, substitute at runtime. That approach was wrong. Directives are principles and patterns, not fill-in-the-blank prompts. They require contextual synthesis by an LLM that understands what the agent is actually doing.

The full recomposition design is specified in **recomposition-design.md**. This section summarizes the approach.

### 4.1 Why LLM Recomposition

A directive stored in the knowledge base looks like:

> "Stable code needs to be well tested. Always cover the happy paths at a minimum and the expected behaviors, which usually nets you 80% coverage."

This is a principle. No template variable system can transform it into something like:

> "You are implementing the injection pipeline handler. The existing handlers in internal/handlers/ all have companion \_test.go files with table-driven tests. Write tests covering: (1) successful injection with all sources responding, (2) partial source failure with graceful degradation, (3) token budget enforcement, and (4) the deduplication path when previous_injection_id is provided. Run `go test -v -cover ./internal/handlers/...` and verify coverage exceeds 80% on the new handler."

The LLM can reference the actual feature being built, the actual test patterns in the codebase, the actual test command with coverage flags, the actual file paths, and the specific edge cases that matter. A template cannot anticipate every combination of feature type, test pattern, coverage tool, and file structure.

### 4.2 The Pipeline

```
[Selected directives: 5-15 principles/patterns]
        +
[Request context: phase, project, files, summary, intent]
        |
        v
[Assemble LLM prompt with system prompt + directives + context]
        |
        v
[Call recomposition LLM (Sonnet-class model)]
        |
        v
[Parse structured JSON output: micro-prompt snippets with provenance]
        |
        v
[Token budget enforcement: trim if over budget]
        |
        v
[Response assembly]
```

### 4.3 The Recomposition LLM

The recomposition task is **synthesis, not reasoning**. The LLM receives a focused system prompt, a small set of principles (5-15 directives, ~1000-2000 tokens), concrete context (~300-500 tokens), and produces structured JSON micro-prompts.

A Sonnet-class model is recommended. Total input is approximately 2,000 tokens; output approximately 500 tokens.

### 4.4 What the LLM Produces

Each micro-prompt snippet:

- Is grounded in a specific directive (references its ID)
- Is contextualized to the agent's current situation
- Is specific enough to act on immediately (mentions actual files, commands, patterns)
- Is concise (1-3 sentences)
- Uses imperative voice ("Run...", "Verify...", "Ensure...")

The LLM may combine related directives into a single snippet, and may skip directives that are not relevant to the current context.

### 4.5 Fallback

When the LLM is unavailable, slow (>10 seconds), or returns invalid output, the pipeline degrades gracefully:

1. **Raw directive content**: Return the directive `content` field as-is. The principles themselves are still useful even without contextualization.
2. **Empty response**: If everything fails, return an empty directives array. The MCP plugin skips injection for this prompt.

### 4.6 Cost

The recomposition step is cost-effective at Sonnet-class pricing. With caching, expect meaningful reduction in LLM calls.

See recomposition-design.md for the full system prompt, detailed cost model, concrete examples, and enhanced feedback schema.

---

## 5. The Response

### Response Payload

Per vision Section 4.7:

```json
{
  "injection_id": "inj_abc123",
  "directives": [
    {
      "id": "dir_001",
      "content": "You are debugging a version mismatch in TestUpsertMemory. Before changing any code, write a focused assertion that demonstrates exactly when the version fails to increment.",
      "kind": "behavioral",
      "source": "superpowers:systematic-debugging",
      "confidence": 0.94
    },
    {
      "id": "dir_002",
      "content": "In this codebase, optimistic concurrency uses a version column incremented by the UPDATE statement itself (SET version = version + 1). Check whether the test is reading the version before or after the UPDATE completes.",
      "kind": "pattern",
      "source": "codebase:hive-server/store",
      "confidence": 0.88
    },
    {
      "id": "dir_003",
      "content": "In 2 of the last 3 debugging sessions in this repo, the root cause was a transaction isolation issue -- a read happening outside the transaction boundary. Check whether the version read is inside or outside the crdbpgx.ExecuteTx() closure.",
      "kind": "corrective",
      "source": "experience:sess_prev1,sess_prev2",
      "confidence": 0.82
    },
    {
      "id": "dir_004",
      "content": "Do not make multiple changes at once. Change one thing in the upsert logic, run TestUpsertMemory, observe. If you change three things and the test passes, you will not know which change fixed it.",
      "kind": "corrective",
      "source": "superpowers:systematic-debugging",
      "confidence": 0.79
    },
    {
      "id": "dir_005",
      "content": "This work may address requirement AUTH-01 (user signup with email/password). If the upsert fix affects the authentication flow, note the connection.",
      "kind": "contextual",
      "source": "project:proj_abc123/requirements",
      "confidence": 0.45
    }
  ],
  "tokens_used": 487,
  "token_budget": 500,
  "candidates_considered": 38,
  "candidates_selected": 5
}
```

### Response Field Specification

| Field                     | Type   | Description                                                                                             |
| ------------------------- | ------ | ------------------------------------------------------------------------------------------------------- |
| `injection_id`            | string | Unique ID for this injection. Used for feedback reporting and deduplication on subsequent calls.        |
| `directives`              | array  | Ordered array of contextualized micro-prompt snippets. First item is highest confidence.                |
| `directives[].id`         | string | Directive ID. Used for feedback reporting.                                                              |
| `directives[].content`    | string | The contextualized micro-prompt text. Ready for direct injection into the agent's context.              |
| `directives[].kind`       | string | Directive kind: `behavioral`, `pattern`, `contextual`, `corrective`, `factual`. Per vision Section 2.3. |
| `directives[].source`     | string | Human-readable provenance. Which skill, codebase, or experience this directive derives from.            |
| `directives[].confidence` | float  | 0.0-1.0. Composite confidence score from the ranking step.                                              |
| `tokens_used`             | int    | Tokens consumed by the directive contents.                                                              |
| `token_budget`            | int    | The budget that was requested.                                                                          |
| `candidates_considered`   | int    | How many candidate directives were evaluated before selection.                                          |
| `candidates_selected`     | int    | How many directives made it into the response.                                                          |

### Directive Kinds

Per vision Section 2.3:

| Kind         | What It Is                     | Example Source              |
| ------------ | ------------------------------ | --------------------------- |
| `behavioral` | How to approach a type of work | Skill decomposition         |
| `pattern`    | Codebase-specific conventions  | Codebase observation        |
| `contextual` | Situation-specific awareness   | State queries, requirements |
| `corrective` | Learned from past mistakes     | Experience feedback         |
| `factual`    | Things the agent should know   | User memory, preferences    |

### How the MCP Plugin Uses the Response

The MCP plugin injects the `directives[].content` values into the agent's system prompt or context window. The exact injection mechanism depends on the host platform:

- **Claude Code**: Injected as a `system-reminder` block appended to the system prompt.
- **Cursor/Copilot**: Injected as context in the MCP tool response that the agent processes.

The MCP plugin stores the `injection_id` and passes it back on the next request as `previous_injection_id` for deduplication.

---

## 6. Latency

### Philosophy

**Accuracy over speed.** The injection pipeline uses an LLM for recomposition because contextually synthesized micro-prompts are meaningfully more useful than template-filled generic instructions. This comes with a latency cost that is acceptable.

### Expected Latency

| Phase                                  | Duration         | Notes                                        |
| -------------------------------------- | ---------------- | -------------------------------------------- |
| Request parsing + validation           | ~1ms             |                                              |
| Fan-out retrieval (parallel)           | 50-200ms         | Meilisearch, CockroachDB, Gel DB in parallel |
| Ranking + selection                    | ~5ms             | In-memory, bounded candidate set             |
| LLM prompt assembly                    | ~2ms             |                                              |
| LLM TTFT (with prompt caching)         | 300-400ms        | System prompt cached                         |
| LLM generation (~250 output tokens)    | 2,500-3,000ms    | Sonnet-class model                           |
| Parse + validate output                | ~1ms             |                                              |
| **Total without cache**                | **~3-4 seconds** |                                              |
| **Total with full response cache hit** | **sub-second**   |                                              |

### Why This Is Acceptable

1. **Accuracy over speed was stated as a design principle.** A 3-second delay that produces a contextual, specific, actionable micro-prompt is worth more than a 50ms delay that produces a generic template fill.

2. **Injection is not blocking.** The agent does not wait for injection before processing the user's prompt. Injection augments the agent's context -- it runs in parallel with the agent's initial response formation.

3. **Frequency control.** The MCP plugin does not inject on every prompt. It injects on context transitions (phase changes, significant actions, periodic refresh). At 3 seconds per injection every 3-5 prompts, the amortized latency per prompt is sub-second.

4. **Speculative pre-fetch.** The MCP plugin can fire the injection request before the agent processes the user's message, overlapping the pipeline with the agent's own thinking time.

### Caching

Caching reduces both latency and LLM cost. The caching strategy includes:

- **Full response cache**: Keyed on `sorted(directive_ids) + phase + project + context_summary_prefix`. When the same directives are selected for a similar context, the cached response is returned without an LLM call.
- **Session-level directive cache**: Within a session, if the same directives are re-selected and the phase has not changed, the previous injection's micro-prompts can be reused.

Specific TTLs, eviction policies, and hit rates are implementation details to be determined by measurement. The caching layers are purely additive optimizations.

---

## 7. The Feedback Loop

### 7.1 Feedback Endpoint

Per vision Section 6.1:

```
POST /api/v1/feedback
Authorization: Bearer <token>
X-Agent-ID: agent-007
```

```json
{
  "injection_id": "inj_abc123",
  "outcomes": [
    {
      "directive_id": "dir_001",
      "outcome": "followed",
      "evidence": "Agent wrote a focused test assertion for version increment before modifying any code"
    },
    {
      "directive_id": "dir_002",
      "outcome": "followed",
      "evidence": "Agent checked the UPDATE statement and found the version increment was correct"
    },
    {
      "directive_id": "dir_003",
      "outcome": "followed",
      "evidence": "Agent found the read was outside the transaction -- this was the root cause. Directive was correct."
    },
    {
      "directive_id": "dir_004",
      "outcome": "ignored",
      "evidence": "Agent made two changes simultaneously (moved read inside tx AND added explicit version check)"
    },
    {
      "directive_id": "dir_005",
      "outcome": "ignored",
      "evidence": "Agent did not reference AUTH-01 -- not relevant to this debugging session"
    }
  ],
  "session_outcome": "success",
  "session_summary": "Fixed version check bug. Root cause was reading version outside transaction boundary."
}
```

### Feedback Fields

| Field                     | Type   | Required | Description                                                                  |
| ------------------------- | ------ | -------- | ---------------------------------------------------------------------------- |
| `injection_id`            | string | yes      | Which injection this feedback is for.                                        |
| `outcomes[]`              | array  | yes      | One entry per directive in the original injection.                           |
| `outcomes[].directive_id` | string | yes      | Which directive.                                                             |
| `outcomes[].outcome`      | enum   | yes      | `followed`, `ignored`, or `negative`. Three outcomes per vision Section 6.1. |
| `outcomes[].evidence`     | string | yes      | What happened. What the agent did or did not do.                             |
| `session_outcome`         | string | no       | Overall session outcome: `success`, `failure`, `partial`, `ongoing`.         |
| `session_summary`         | string | no       | Brief summary of what happened in this session.                              |

### How Feedback Updates Directives

When outcomes are recorded, the directive's effectiveness metrics are updated:

```sql
-- For each 'followed' outcome
UPDATE directives
SET times_injected = times_injected + 1,
    times_followed = times_followed + 1,
    effectiveness = (times_followed::FLOAT8 - times_negative::FLOAT8) / GREATEST(times_injected::FLOAT8, 1),
    updated_at = now()
WHERE id = $1 AND tenant_id = $2;

-- For each 'ignored' outcome
UPDATE directives
SET times_injected = times_injected + 1,
    times_ignored = times_ignored + 1,
    effectiveness = (times_followed::FLOAT8 - times_negative::FLOAT8) / GREATEST(times_injected::FLOAT8, 1),
    updated_at = now()
WHERE id = $1 AND tenant_id = $2;

-- For each 'negative' outcome (directive was followed but made things worse)
UPDATE directives
SET times_injected = times_injected + 1,
    times_negative = times_negative + 1,
    effectiveness = (times_followed::FLOAT8 - times_negative::FLOAT8) / GREATEST(times_injected::FLOAT8, 1),
    weight = GREATEST(weight * 0.8, 0.1),  -- Reduce weight by 20%, floor at 0.1
    updated_at = now()
WHERE id = $1 AND tenant_id = $2;
```

Outcomes are also recorded in the `injection_outcomes` table for audit and analysis:

```sql
INSERT INTO injection_outcomes (injection_id, directive_id, outcome, evidence)
VALUES ($1, $2, $3, $4);
```

### 7.2 Directive Evolution

Over time, the directive population evolves:

- **Effective directives** gain weight and are injected more often.
- **Ignored directives** lose weight. After being ignored in 10 consecutive injections, they are flagged for review.
- **Negative directives** lose weight rapidly. After 3 negative outcomes, they are auto-deprecated (`active=false`) and flagged for human review.
- **Experience directives** accumulate as agents work. A repo that has been worked on for 6 months has dozens of repo-specific corrective directives learned from actual debugging sessions.
- **Supersession**: When a new directive contradicts or improves on an older one, the old one is marked as superseded (`supersedes_id`). The new one inherits the old one's injection history for continuity.

### 7.3 Session-Complete Endpoint

Per vision Section 6.3:

```
POST /api/v1/feedback/session-complete
Authorization: Bearer <token>
X-Agent-ID: agent-007
```

```json
{
  "session_id": "sess_xyz",
  "summary": "Debugged version check. Root cause: reading version outside ExecuteTx closure.",
  "repo": "christmas-island/hive-server",
  "outcome": "success",
  "key_insight": "CockroachDB transaction isolation means reads inside ExecuteTx see the transaction's snapshot. Reads outside see the committed state, which may be stale."
}
```

When a session ends, hive-server analyzes the session outcomes to create new experience-derived directives. The example above can produce:

```json
{
  "content": "In this codebase, ensure all reads that inform writes happen inside the crdbpgx.ExecuteTx() closure. Reading outside the closure and writing inside creates stale-read bugs because CockroachDB's serializable isolation only protects reads within the transaction boundary.",
  "kind": "corrective",
  "source_type": "experience",
  "source_name": "session:sess_xyz",
  "trigger_tags": [
    "cockroachdb",
    "transaction",
    "stale-read",
    "ExecuteTx",
    "isolation"
  ],
  "trigger_intent": "Agent is writing or modifying CockroachDB transaction code",
  "trigger_scope": "repo:christmas-island/hive-server",
  "weight": 1.2
}
```

Experience-derived directives start with a moderate weight (1.0-1.2) and earn their effectiveness score through future injections and outcomes. Over time, the most useful experience-derived directives outrank the original skill-derived directives -- the system learns what actually works in practice.

---

## 8. End-to-End Scenarios

### Scenario 1: Agent Starts a Brainstorming Session

**Setup:** A user says "Let's brainstorm how to add rate limiting to the API." The MCP plugin detects the brainstorming intent and calls hive-server.

**Request:**

```json
{
  "agent_id": "agent-007",
  "session_id": "sess_new",
  "context": {
    "intent": "brainstorming rate limiting approaches",
    "repo": "christmas-island/hive-server",
    "phase": "brainstorming",
    "recent_actions": ["user said 'let's brainstorm rate limiting'"],
    "conversation_summary": "User wants to add rate limiting to the API. No approach has been discussed yet."
  },
  "token_budget": 400
}
```

**Fan-out:**

- **Meilisearch**: Searches `"brainstorming rate limiting approaches. User wants to add rate limiting to the API."` Returns directives about brainstorming methodology, API design patterns, and rate limiting.
- **CockroachDB**: Queries for directives with `trigger_phase = 'brainstorming'` and `trigger_scope` matching the repo.
- **Gel DB**: Traverses chains from found directives, returns the brainstorming methodology chain (generate options -> evaluate tradeoffs -> ask user).

**Ranking**: Candidates scored using `(meilisearch_relevance * 0.4) + (effectiveness * 0.3) + (weight * 0.2) + (recency_bonus * 0.1)`. Brainstorming methodology directives score highest.

**Recomposition**: The LLM synthesizes the selected directives with the context, producing micro-prompts specific to rate limiting for this repo.

**Response:**

```json
{
  "injection_id": "inj_bs_001",
  "directives": [
    {
      "id": "dir_bs_01",
      "content": "The user is beginning a brainstorming session. Before converging on any solution, generate at least 3 distinct approaches. For each approach, state: what it is, one advantage, one disadvantage, and an effort estimate. Present them as a numbered list, then ask the user which direction interests them.",
      "kind": "behavioral",
      "source": "superpowers:brainstorming",
      "confidence": 0.97
    },
    {
      "id": "dir_bs_02",
      "content": "Ask about edge cases before the user commits to a solution. For rate limiting specifically: What happens when the limit is hit? Per-user or per-IP? Should different endpoints have different limits? Should authenticated users get higher limits?",
      "kind": "behavioral",
      "source": "superpowers:brainstorming + experience:rate-limiting-patterns",
      "confidence": 0.91
    },
    {
      "id": "dir_bs_03",
      "content": "This codebase uses chi v5 middleware for cross-cutting concerns (see internal/handlers/handlers.go). Rate limiting should be implemented as chi middleware, not per-handler logic. Check if golang.org/x/time/rate is already a dependency.",
      "kind": "pattern",
      "source": "codebase:hive-server/handlers",
      "confidence": 0.88
    },
    {
      "id": "dir_bs_04",
      "content": "In a previous project (proj_widget_api), rate limiting was implemented with a token bucket. The main lesson learned was: store rate limit state in the database (not in-memory) because multiple hive-server instances share load. CockroachDB is already the backend here.",
      "kind": "corrective",
      "source": "experience:proj_widget_api",
      "confidence": 0.73
    }
  ],
  "tokens_used": 389,
  "token_budget": 400,
  "candidates_considered": 42,
  "candidates_selected": 4
}
```

**What the agent does with this:** The MCP plugin injects these directives into the agent's context. The agent now knows to generate multiple approaches before converging (from the brainstorming skill), probe specific edge cases relevant to rate limiting (from experience), use chi middleware as the implementation pattern (from codebase knowledge), and consider distributed state because of multi-instance deployment (from past project experience).

### Scenario 2: Agent Is Debugging a Failing Test

**Setup:** The agent ran `go test ./internal/store/...` and got a race condition failure.

**Request:**

```json
{
  "agent_id": "agent-007",
  "session_id": "sess_debug",
  "context": {
    "intent": "debugging a test failure",
    "files": ["internal/store/memory_test.go", "internal/store/memory.go"],
    "repo": "christmas-island/hive-server",
    "phase": "debugging",
    "recent_actions": [
      "ran go test ./internal/store/...",
      "observed: FAIL TestUpsertMemory/concurrent_updates - race detected"
    ],
    "conversation_summary": "Test detects a race condition in concurrent upsert operations. The race detector flagged simultaneous writes to a shared variable."
  },
  "token_budget": 500
}
```

**Response:**

```json
{
  "injection_id": "inj_dbg_001",
  "directives": [
    {
      "id": "dir_dbg_01",
      "content": "STOP. You have a race condition detected by Go's race detector. Do not attempt to fix it yet. First, run the test with -count=10 to see if it fails consistently or intermittently. Then, read the race detector output carefully -- it tells you exactly which two goroutines are conflicting and on which variable.",
      "kind": "behavioral",
      "source": "superpowers:systematic-debugging",
      "confidence": 0.96
    },
    {
      "id": "dir_dbg_02",
      "content": "In 3 of the last 4 race conditions in this codebase, the root cause was a shared *testing.T or shared test fixture being accessed by parallel subtests without t.Parallel() isolation or proper mutex protection. Check if TestUpsertMemory uses t.Run() with shared state.",
      "kind": "corrective",
      "source": "experience:sess_race1,sess_race2,sess_race3",
      "confidence": 0.89
    },
    {
      "id": "dir_dbg_03",
      "content": "Before applying a fix, write a test that reliably triggers the race. Use -race flag and -count=100. If you cannot reproduce it reliably, the fix cannot be verified.",
      "kind": "behavioral",
      "source": "superpowers:systematic-debugging + superpowers:test-driven-development",
      "confidence": 0.84
    },
    {
      "id": "dir_dbg_04",
      "content": "When you fix the race, change ONE thing. Do not simultaneously refactor the test structure and fix the concurrency bug. Fix the race, verify it passes with -race -count=100, then refactor if needed.",
      "kind": "corrective",
      "source": "superpowers:systematic-debugging",
      "confidence": 0.78
    }
  ],
  "tokens_used": 478,
  "token_budget": 500,
  "candidates_considered": 51,
  "candidates_selected": 4
}
```

**What makes this powerful:** The agent gets the systematic debugging methodology (from Superpowers), but it also gets repo-specific knowledge (historical race condition patterns in this codebase). A static skill would say "investigate root cause." Hive-server says "in this repo, 3/4 race conditions were caused by shared test fixtures in parallel subtests."

### Scenario 3: Agent Is Planning a Multi-Step Feature

**Setup:** The agent is planning a new authentication system.

**Request:**

```json
{
  "agent_id": "agent-007",
  "session_id": "sess_plan",
  "context": {
    "intent": "planning multi-step authentication feature",
    "repo": "christmas-island/hive-server",
    "phase": "planning",
    "recent_actions": [
      "user described requirement: JWT auth with refresh tokens",
      "agent is about to create a phase decomposition"
    ],
    "conversation_summary": "User wants to add JWT authentication with refresh tokens, role-based access control, and API key support. This spans multiple implementation phases.",
    "open_requirements": ["AUTH-01", "AUTH-02", "AUTH-03", "AUTH-04"],
    "current_project": "proj_abc123"
  },
  "token_budget": 600
}
```

**Response:**

```json
{
  "injection_id": "inj_plan_001",
  "directives": [
    {
      "id": "dir_plan_01",
      "content": "Before decomposing into phases, verify you understand the full scope. Four requirements are open (AUTH-01 through AUTH-04). List each one with your interpretation, and ask the user to confirm or correct. Do not plan against assumptions.",
      "kind": "behavioral",
      "source": "gsd:planning-methodology + allium:elicitation",
      "confidence": 0.95
    },
    {
      "id": "dir_plan_02",
      "content": "Decompose the work into phases where each phase produces a working, testable increment. Phase 1 should be the simplest useful auth (e.g., basic JWT with a single role). Each subsequent phase adds a capability (refresh tokens, RBAC, API keys). Do not create a phase that only adds 'scaffolding' with no user-visible behavior.",
      "kind": "behavioral",
      "source": "gsd:roadmapping + superpowers:writing-plans",
      "confidence": 0.92
    },
    {
      "id": "dir_plan_03",
      "content": "This codebase already has auth middleware in internal/handlers/handlers.go that checks a Bearer token against HIVE_TOKEN env var. Your JWT implementation must replace this, not layer on top of it. Plan for a migration path: existing token auth keeps working during Phase 1 implementation.",
      "kind": "pattern",
      "source": "codebase:hive-server/handlers",
      "confidence": 0.88
    },
    {
      "id": "dir_plan_04",
      "content": "In a previous project (proj_widget_api), the auth implementation started with JWT but had to be reworked when RBAC was added because the initial token structure did not include role claims. Plan the JWT token structure in Phase 1 to include role claims even if RBAC is not implemented until Phase 3. This avoids a migration.",
      "kind": "corrective",
      "source": "experience:proj_widget_api/auth-rework",
      "confidence": 0.83
    }
  ],
  "tokens_used": 489,
  "token_budget": 600,
  "candidates_considered": 67,
  "candidates_selected": 4
}
```

**What is happening here:** The planning directives come from three different skill lineages (GSD's roadmapping, Superpowers' plan writing, Allium's elicitation methodology) -- but the agent receives them as a unified, non-contradictory set because the decomposition pipeline already resolved overlaps during ingestion. The agent also gets codebase-specific pattern knowledge (existing auth middleware), past-project lessons (JWT structure that avoids rework), and user-behavioral observations. No single skill could provide this combination.

---

## Appendix: Data Model Reference

The injection pipeline uses the directive schema and supporting tables defined in vision v5 Section 2.2 and Section 5.1. The key tables are:

### Directives (CockroachDB, source of truth)

Per vision Section 2.2. The `directives` table stores the full directive catalog with effectiveness metrics, provenance, trigger metadata, and `tenant_id` for multi-tenancy. Key fields used by the injection pipeline:

- `content` -- the behavioral instruction text
- `kind` -- behavioral, pattern, contextual, corrective, factual
- `trigger_phase` -- which workflow phase activates this directive
- `trigger_scope` -- global, repo-scoped, or project-scoped
- `trigger_tags` -- semantic tags (JSONB array)
- `trigger_intent` -- natural language description of when to use
- `effectiveness` -- historical success rate
- `weight` -- priority weight for ranking
- `token_cost` -- estimated tokens when rendered (populated during enrichment)
- `active` -- soft delete / disable
- `tenant_id` -- multi-tenancy

### Agent Sessions (CockroachDB)

Per vision Section 5.1. Tracks active agent sessions for deduplication and session history.

### Injections (CockroachDB)

Per vision Section 5.1. Audit log of injection responses, including the context hash and directive IDs returned.

### Injection Outcomes (CockroachDB)

Per vision Section 5.1. Records per-directive outcomes from feedback, linked to specific injections.

### Directives Index (Meilisearch)

Per vision Section 5.2. Indexes directive content and triggers for semantic retrieval. Configuration as specified in Section 2.1 above.

### Directive Graph (Gel DB)

Per vision Section 5.3. Models relationships between directives (related_to, superseded_by) and behavioral chains (DirectiveChain) for sequence traversal.
