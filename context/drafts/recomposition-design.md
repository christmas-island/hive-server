# Recomposition Layer Design: LLM Synthesis

**Date:** 2026-03-09
**Status:** Replaces Section 4 of injection-pipeline.md
**Core principle:** Accuracy over speed. Directives are principles and patterns, not pre-formatted prompts. They require contextual synthesis by an LLM that understands what the agent is actually doing.

---

## 1. Why LLM Recomposition Is Essential

### The Template Approach Was Wrong

The original injection-pipeline.md Section 4 proposed template-based recomposition: store directives with `{{variable}}` placeholders, substitute at runtime using request context fields. This was a premature optimization driven by an invented 400ms latency constraint that was never specified.

The fundamental problem: **directives are not fill-in-the-blank prompts. They are principles and patterns.**

Consider a directive stored in the knowledge base:

> "Stable code needs to be well tested. Always cover the happy paths at a minimum and the expected behaviors, which usually nets you 80% coverage."

This is a principle. It does not contain template variables. It does not map to a single command. It encodes a philosophy about test quality that must be _applied_ to the agent's current situation by something that understands that situation.

### What Templates Produce vs. What LLMs Produce

**Template approach** (from the original design):

The directive is stored with variables like `{{package}}` and `{{test_command}}`. At runtime, hive-server substitutes `go test ./internal/handlers/...` for a Go project. The result:

> "After implementing changes, run the relevant test suite before claiming completion."

Or at best:

> "You just modified code in internal/handlers -- run `go test ./internal/handlers/...` to verify your changes before moving on."

This is generic. It works for any handler change. It does not know what the agent is _actually building_, what patterns already exist in the codebase, what coverage looks like, or what edge cases matter for this specific feature.

**LLM synthesis** with the same directive + full request context:

> "You are implementing the injection pipeline handler. The existing handlers in internal/handlers/ all have companion \_test.go files with table-driven tests. Write tests covering: (1) successful injection with all sources responding, (2) partial source failure with graceful degradation, (3) token budget enforcement, and (4) the deduplication path when previous_injection_id is provided. Run `go test -v -cover ./internal/handlers/...` and verify coverage exceeds 80% on the new handler."

The LLM can reference:

- **The actual feature** being built (from `context.summary` and `intent`)
- **The actual test patterns** in the codebase (from knowledgebase entries about the project)
- **The actual test command** with coverage flags (from language knowledge)
- **The actual file paths** being worked on (from `recent_files`)
- **The specific edge cases** that matter for this feature (reasoned from the directive principle + context)

No template variable system can do this. You would need to pre-define variables for every possible combination of feature type, test pattern, coverage tool, edge case category, and file structure. That is not a template system -- that is reimplementing an LLM badly.

### The Pre-Formatting Fallacy

The original design assumed directives could be "pre-formatted as micro-prompt snippets" during a curation process. This assumes the curation author can anticipate every context in which the directive will be surfaced. That defeats the purpose of contextual injection.

A directive about error handling:

> "Wrap errors with context so that when a failure occurs, the error chain tells you exactly where it happened and why."

Pre-formatted for Go: "Use `fmt.Errorf("operation: %w", err)` to wrap errors."

But the LLM, given the agent is implementing a new CockroachDB store method with retry logic, produces:

> "You're implementing CockroachDB store methods with `crdbpgx.ExecuteTx()`. Errors inside the transaction function must be wrapped with `fmt.Errorf("operation: %w", err)` -- critically, use `%w` not `%v`, because the cockroach-go retry wrapper inspects the error chain to detect retryable SQLSTATE 40001 errors. If you wrap with `%v`, retry logic breaks silently."

The LLM knows that the CockroachDB retry wrapper depends on error wrapping semantics. A template cannot know this unless someone explicitly authors a "cockroachdb-error-wrapping" variant of the "wrap-errors" directive. And then a "postgres-error-wrapping" variant. And a "sqlite-error-wrapping" variant. This path leads to a combinatorial explosion of directive variants that is unmaintainable.

### The Correct Model

Directives are stored as **principles and patterns** -- the atomic behavioral knowledge units described in directive-schema.md. They are retrieved based on contextual relevance by the fan-out pipeline. Then an LLM synthesizes them into **contextual micro-prompts** that are specific, actionable, and grounded in what the agent is actually doing right now.

The LLM is not reasoning or planning. It is performing a focused synthesis task: "Given these principles and this situation, produce concrete instructions." This is a translation task, not a creative task.

---

## 2. The Recomposition Pipeline

### Overview

```
[Fan-out retrieval completes]
        |
        v
[Selected directives: 5-15 principles/patterns]
        +
[Request context: activity, project, files, summary, memories]
        |
        v
[Assemble LLM prompt]
        |
        v
[Call recomposition LLM (Haiku-class)]
        |
        v
[Parse structured output: M micro-prompt snippets with provenance]
        |
        v
[Token budget enforcement: trim if over budget]
        |
        v
[Response]
```

### Step-by-Step

**Step 1: Retrieval (unchanged from injection-pipeline.md Sections 2-3)**

The fan-out queries Meilisearch, CockroachDB, and Gel DB in parallel. The ranking and selection step reduces candidates to 5-15 directives that fit within a _directive input budget_ (not the final output token budget). This step is identical to the existing design.

**Step 2: Context Assembly**

Gather all the inputs the recomposition LLM needs:

```go
type RecompositionInput struct {
    // The selected directives (principles/patterns)
    Directives []DirectiveForSynthesis

    // The request context
    Activity        string   // e.g., "implementing"
    ProjectName     string   // e.g., "hive-server"
    ProjectLanguage string   // e.g., "go"
    ContextSummary  string   // What the agent is doing right now
    RecentFiles     []string // Files being worked on
    ErrorContext    string   // If debugging, the error
    Intent          string   // If stated, the user's goal

    // Additional context from the knowledge base
    ProjectMemories []MemorySnippet // Relevant memories about this project
    AgentMemories   []MemorySnippet // Relevant memories about this agent's patterns

    // Constraints
    OutputTokenBudget int // How many tokens the final output should be
}

type DirectiveForSynthesis struct {
    ID                   string
    Content              string // The principle/pattern text
    Rationale            string // Why this directive exists
    DirectiveType        string // guardrail, behavioral, contextual, procedural
    Priority             int
    VerificationCriteria string
    SourceSkill          string // superpowers, gsd, allium, custom
}
```

**Step 3: LLM Call**

Send the assembled prompt to a Haiku-class model. The system prompt (Section 4) instructs the model on exactly what to produce. The response is structured JSON.

**Step 4: Parse and Validate**

Parse the LLM's JSON output. Validate that:

- Each micro-prompt references a valid directive ID
- The total token count is within budget
- No micro-prompt exceeds a per-directive token limit (150 tokens)
- The output is well-formed

**Step 5: Token Budget Enforcement**

If the LLM's output exceeds the token budget (it sometimes will), trim from the bottom of the priority-sorted list. Guardrail directives are never trimmed.

**Step 6: Response Assembly**

Package the micro-prompts into the injection response format defined in injection-pipeline.md Section 5.

---

## 3. LLM Selection and Cost Model

### Why Haiku-Class Is Sufficient

The recomposition task is **synthesis, not reasoning**. The LLM receives:

- A clear, focused system prompt defining exactly what to do
- A small set of principles (5-15 directives, ~1000-2000 tokens)
- Concrete context about what the agent is doing (~300-500 tokens)
- A request for structured output (JSON micro-prompts)

This is well within the capability of a fast, cheap model. It does not require:

- Multi-step reasoning (no chain-of-thought needed)
- Creative generation (the principles already exist; the model contextualizes them)
- Long-context understanding (total input is ~3000 tokens)
- Tool use or agentic behavior

Claude Haiku 4.5 is the recommended model. It provides strong instruction following, structured output compliance, and fast inference at low cost.

### Token Budget Estimates

**Input tokens per request:**

| Component                                  | Tokens (estimate) |
| ------------------------------------------ | ----------------- |
| System prompt                              | ~500              |
| Directives (8 avg, ~120 tokens each)       | ~960              |
| Request context (summary, files, activity) | ~300              |
| Memories/knowledgebase snippets (2-3)      | ~200              |
| JSON formatting overhead                   | ~40               |
| **Total input**                            | **~2,000**        |

**Output tokens per request:**

| Component                                   | Tokens (estimate) |
| ------------------------------------------- | ----------------- |
| 5-8 micro-prompt snippets (~60 tokens each) | ~400              |
| Provenance metadata per snippet             | ~80               |
| JSON structure                              | ~20               |
| **Total output**                            | **~500**          |

### Cost Per Request

Using Claude Haiku 4.5 pricing:

- Input: $1.00 / 1M tokens
- Output: $5.00 / 1M tokens

**Per injection request:**

- Input cost: 2,000 tokens \* $1.00/1M = $0.002
- Output cost: 500 tokens \* $5.00/1M = $0.0025
- **Total per request: $0.0045** (~0.45 cents)

### Cost at Scale

| Daily volume          | Monthly cost | Notes                                  |
| --------------------- | ------------ | -------------------------------------- |
| 100 injections/day    | $13.50/month | Single developer, moderate usage       |
| 500 injections/day    | $67.50/month | Small team (3-5 developers)            |
| 1,000 injections/day  | $135/month   | Active team                            |
| 5,000 injections/day  | $675/month   | Large team or high-frequency injection |
| 10,000 injections/day | $1,350/month | Organization-wide deployment           |

**With caching (Section 6), expect 30-50% reduction in LLM calls.** Effective cost for 1,000 injections/day with caching: ~$70-95/month.

### Cost Comparison: What Does This Buy?

For context, $135/month (1,000 injections/day) is:

- Less than one hour of a senior engineer's time per month
- Less than a single GitHub Copilot Business seat ($19/user/month \* 8 users = $152/month)
- A fraction of the compute cost for the LLM agents themselves

The value proposition is clear: if contextual injection prevents even one wasted debugging session or one missed test suite per month across a team, it pays for itself many times over.

### Prompt Caching Optimization

Anthropic's prompt caching can reduce input costs by up to 90% for the system prompt and directive content that remains stable across requests. The system prompt (~500 tokens) and frequently-used directives can be cached:

- Cached input: $0.10 / 1M tokens (90% discount)
- With a stable system prompt cached: saves ~$0.00045 per request
- With common directives cached: additional savings of ~$0.0005 per request
- **Effective cost with caching: ~$0.003 per request** (~0.3 cents)

---

## 4. The System Prompt

This is the actual system prompt for the recomposition LLM.

````
You are a directive synthesis engine. Your job is to transform abstract behavioral
principles into contextual, specific, actionable micro-prompts for an AI coding agent.

## Input

You will receive:
1. A set of DIRECTIVES -- these are principles and patterns about how to develop software well.
   Each has an ID, content (the principle), type, priority, and rationale.
2. A REQUEST CONTEXT -- what the agent is currently doing, including:
   - activity (implementing, debugging, reviewing, etc.)
   - project name and language
   - summary of current work
   - recent files being edited
   - error context (if debugging)
   - user intent (if stated)
3. MEMORIES -- relevant past observations about this project or agent.

## Output

Produce a JSON array of micro-prompt snippets. Each snippet:
- Is grounded in a specific directive (reference its ID)
- Is contextualized to the agent's CURRENT situation
- Is specific enough to act on immediately (mentions actual files, commands, patterns)
- Is concise (1-3 sentences, max 150 tokens)
- Uses imperative voice ("Run...", "Verify...", "Ensure...")

## Rules

1. CONTEXTUALIZE, don't parrot. Never repeat a directive verbatim. Transform the principle
   into a concrete instruction for THIS situation.
2. BE SPECIFIC. Reference actual file paths from recent_files. Use the correct test/build
   commands for the project language. Mention specific patterns if memories provide them.
3. PRIORITIZE. Guardrail directives produce "rule" action snippets (the agent MUST follow).
   Others produce "suggest" action snippets.
4. COMBINE when appropriate. If two directives address the same concern for this context,
   merge them into one snippet and reference both IDs.
5. SKIP when irrelevant. If a directive does not meaningfully apply to the current context,
   omit it. Explain why in the "skipped" array.
6. STAY WITHIN BUDGET. Target {{output_token_budget}} tokens total for all snippets combined.
   If you cannot fit all directives, drop the lowest-priority ones first. Never drop guardrails.
7. DO NOT add directives that were not provided. You synthesize, you do not invent.

## Output Format

```json
{
  "snippets": [
    {
      "directive_ids": ["dir_abc123"],
      "content": "The specific, contextual micro-prompt text.",
      "category": "guardrail|contextual|behavioral|procedural|memory",
      "action": "rule|suggest|context",
      "reasoning": "Brief explanation of how the directive was contextualized."
    }
  ],
  "skipped": [
    {
      "directive_id": "dir_xyz789",
      "reason": "Not applicable because the agent is brainstorming, not implementing."
    }
  ]
}
````

````

### Design Rationale for the System Prompt

**Why imperative voice?** LLM agents respond more reliably to direct instructions ("Run the tests") than to suggestions phrased as questions or observations ("You might want to consider running tests").

**Why "CONTEXTUALIZE, don't parrot"?** Without this instruction, small models will often copy the directive text verbatim and append a filename. The explicit instruction forces transformation.

**Why "SKIP when irrelevant"?** The retrieval pipeline uses semantic search, which is imperfect. A directive about "testing" might be retrieved when the agent is reviewing documentation. The LLM has enough context to determine actual relevance and should drop false positives.

**Why structured JSON output?** Hive-server needs to parse the result programmatically, attach provenance metadata, and enforce token budgets. Free-form text would require another parsing step.

**Why "DO NOT add directives that were not provided"?** Without this constraint, the model will sometimes generate useful-sounding advice that is not grounded in any directive. This would undermine the provenance tracking and feedback loop.

---

## 5. Latency Reality

### Actual Latency of a Haiku-Class LLM Call

For Claude Haiku 4.5 with ~2,000 input tokens and ~500 output tokens:

- **Time to first token (TTFT):** ~0.6 seconds (600ms)
- **Output generation:** ~500 tokens at ~94 tokens/second = ~5.3 seconds
- **Total wall-clock time:** ~5.9 seconds

That is significantly more than the original design's 400ms total pipeline budget. But the original design's 400ms constraint was invented -- it was never a stated requirement.

However, 5.9 seconds is too long even for an accuracy-first approach. We can reduce this substantially:

### Latency Optimizations

**1. Reduce output tokens.**

500 tokens of output is generous. Most micro-prompt snippets are 30-60 tokens. With 5-8 snippets, we need 200-350 output tokens. At 94 tokens/second:

- 250 output tokens = ~2.7 seconds generation
- Total: ~3.3 seconds

**2. Use streaming.**

Hive-server can begin processing the LLM response as tokens stream in. The first snippet is available within ~1.5 seconds. If the MCP plugin supports streaming injection (or if hive-server streams the HTTP response), the agent starts receiving directives before the LLM finishes generating.

**3. Use prompt caching to reduce TTFT.**

With the system prompt cached, TTFT drops because less processing is needed on the input side. Cached prompt TTFT is typically 30-50% lower:

- Cached TTFT: ~0.3-0.4 seconds
- Total with caching + reduced output: ~2.5-3.0 seconds

**4. Speculative pre-fetch.**

The MCP plugin knows when a prompt is being composed. It can fire the injection request *before* the agent processes the user's message, not after. This overlaps the injection pipeline with the agent's own thinking time.

### Realistic Latency Budget

| Phase | Duration | Notes |
|-------|----------|-------|
| Request parsing + validation | 1ms | |
| Fan-out retrieval (parallel) | 50-200ms | Same as original design |
| Ranking + selection | 5ms | |
| LLM prompt assembly | 2ms | |
| LLM TTFT (cached) | 300-400ms | Prompt caching |
| LLM generation (250 tokens) | 2,500-3,000ms | ~94 tokens/sec |
| Parse + validate output | 1ms | |
| **Total** | **~3.0-3.5 seconds** | |

### Why 3 Seconds Is Acceptable

1. **The user said accuracy over speed.** A 3-second delay that produces a contextual, specific, actionable micro-prompt is worth more than a 50ms delay that produces a generic template fill.

2. **Injection is not blocking.** The agent does not wait for injection before processing the user's prompt. Injection augments the agent's context -- it runs in parallel with the agent's initial response formation. The MCP plugin can inject directives into the context window at any point during the conversation turn.

3. **Frequency control.** Injection does not need to happen on every single prompt. The MCP plugin can inject:
   - On session start
   - When activity changes (implementing -> debugging)
   - When significant new context appears (new files, new errors)
   - At a configurable interval (every 3-5 prompts)

   At 3 seconds per injection with injection every 3-5 prompts, the amortized cost is sub-second per prompt.

4. **Perception of value.** Users will tolerate latency for something that demonstrably improves their agent's behavior. A generic "run your tests" message that arrives in 50ms provides less perceived value than a specific "run `go test -cover ./internal/handlers/...` and check that the injection handler has coverage on the degradation path" message that arrives in 3 seconds.

### Comparison to Alternatives

| Approach | Latency | Accuracy | Cost |
|----------|---------|----------|------|
| Template substitution | <5ms | Low (generic) | ~$0 |
| LLM recomposition (Haiku) | ~3 seconds | High (contextual) | ~$0.003-0.005/req |
| LLM recomposition (Sonnet) | ~5-8 seconds | Very high | ~$0.02/req |
| No recomposition (raw directives) | 0ms | Medium (principles are still useful) | $0 |

The Haiku-class LLM hits the sweet spot: meaningfully better accuracy than templates, acceptable latency, and negligible cost.

---

## 6. Caching Strategy

Even with acceptable LLM latency, caching reduces cost and improves response time for repeated contexts.

### Layer 1: Full Response Cache (hash-based)

**Cache key:** SHA-256 of `sorted(directive_ids) + activity + project_name + context_summary_first_100_chars`

**TTL:** 5 minutes

**Hit scenario:** The agent sends two injection requests 30 seconds apart while working on the same feature. The context summary is nearly identical, the same directives are retrieved, and the activity has not changed. The second request returns the cached response in <5ms.

**Expected hit rate:** 20-30%. Context summaries change frequently enough that exact matches are uncommon, but rapid-fire requests during active implementation often produce hits.

```go
type ResponseCache struct {
    mu    sync.RWMutex
    items map[string]*CachedResponse
}

type CachedResponse struct {
    Response  *InjectionResponse
    CreatedAt time.Time
    HitCount  int
}

func (c *ResponseCache) Key(directiveIDs []string, activity, project, summary string) string {
    sort.Strings(directiveIDs)
    h := sha256.New()
    h.Write([]byte(strings.Join(directiveIDs, ",")))
    h.Write([]byte(activity))
    h.Write([]byte(project))
    // First 100 chars of summary for fuzzy matching
    if len(summary) > 100 {
        summary = summary[:100]
    }
    h.Write([]byte(summary))
    return hex.EncodeToString(h.Sum(nil))
}
````

### Layer 2: Session-Level Directive Cache

**Observation:** Within a single session, the same directives are often re-selected because the agent stays in the same activity on the same project. The _raw directive content_ does not change between requests. Only the context changes.

**Optimization:** If the set of selected directives is identical to the previous injection in this session AND the activity has not changed, skip the LLM call and return the previous injection's micro-prompts. The context may have shifted slightly, but the same principles applied to a nearly-identical situation produce nearly-identical micro-prompts.

**Cache key:** `session_id + sorted(directive_ids) + activity`

**TTL:** 10 minutes or until activity changes.

**Expected hit rate:** 30-40% within active sessions.

### Layer 3: Directive Pre-Synthesis Cache

**Observation:** Some directives are universal -- they apply to every Go project, or every implementing activity. For these high-frequency directives, we can pre-generate contextualizations for common contexts.

**Implementation:** A background job periodically generates micro-prompts for the top 20 most-frequently-surfaced directives across common project/activity combinations:

```
directive_id + project_language + activity -> pre-synthesized micro-prompt
```

Example: The "run tests after implementing" directive, pre-synthesized for Go + implementing:

> "Run `go test -v ./...` to verify your changes. Check for test failures and review coverage on modified packages."

This pre-synthesized version is better than a raw template (it includes `-v` and coverage guidance) but less specific than a full LLM synthesis (it does not reference specific files or features). It serves as a warm fallback when the full LLM call is unavailable or when latency needs to be reduced.

**TTL:** 1 hour. Regenerated by background worker.

### Combined Cache Strategy

```
Request arrives
    |
    v
[Check Layer 1: exact response cache] --hit--> return cached response (< 5ms)
    |miss
    v
[Check Layer 2: session directive cache] --hit--> return previous session response (< 5ms)
    |miss
    v
[Run retrieval fan-out]
    |
    v
[Check Layer 3: pre-synthesis cache for each directive]
    |
    +--all directives have pre-synthesis--> assemble from cache (< 10ms)
    |
    +--some miss--> call LLM for missing, merge with cached (< 2s)
    |
    +--all miss--> full LLM call (~ 3s)
```

### Expected Effective Latency

With all cache layers active:

- **p50:** ~200ms (cache hit, common during active sessions)
- **p75:** ~2.0 seconds (partial cache, reduced LLM output)
- **p90:** ~3.0 seconds (full LLM call)
- **p99:** ~4.5 seconds (LLM call with retries or slow response)

---

## 7. Fallback: LLM Unavailable

When the recomposition LLM is unreachable, slow (>10 seconds), or returns invalid output, the pipeline degrades gracefully.

### Fallback Tier 1: Pre-Synthesis Cache

If Layer 3 (directive pre-synthesis cache) has entries for the selected directives, use them. These are contextual enough to be useful (language-specific commands, activity-appropriate phrasing) even though they lack full situational awareness.

### Fallback Tier 2: Raw Directive Content

Return the directive `content` field as-is. The principles themselves are still useful to the agent even without contextualization:

> "Stable code needs to be well tested. Always cover the happy paths at a minimum and the expected behaviors."

An agent reading this will understand it means "write tests." It will not get the specific file paths or coverage commands, but the behavioral nudge still has value.

### Fallback Tier 3: Static Content Field

If the directive has a `static_content` field (a pre-written, context-free version of the instruction), use that. This preserves the original design's template approach as a last resort.

### Fallback Behavior

```go
func (r *Recomposer) Recompose(ctx context.Context, input *RecompositionInput) (*RecompositionOutput, error) {
    // Try LLM synthesis
    output, err := r.callLLM(ctx, input)
    if err == nil {
        return output, nil
    }

    log.Warn("LLM recomposition failed, falling back", "error", err)

    // Fallback: use pre-synthesis cache or raw directive content
    snippets := make([]MicroPrompt, 0, len(input.Directives))
    for _, d := range input.Directives {
        if preSynth, ok := r.preSynthCache.Get(d.ID, input.Activity, input.ProjectLanguage); ok {
            snippets = append(snippets, preSynth)
        } else {
            snippets = append(snippets, MicroPrompt{
                DirectiveIDs: []string{d.ID},
                Content:      d.Content,
                Category:     d.DirectiveType,
                Action:       actionForType(d.DirectiveType),
                Reasoning:    "LLM unavailable; returning raw directive.",
            })
        }
    }

    return &RecompositionOutput{
        Snippets: snippets,
        Fallback: true,
    }, nil
}
```

### Monitoring

Track fallback rate as a key metric. If fallback rate exceeds 5% over a 1-hour window, alert. Persistent fallback means the LLM provider is degraded and injection quality is reduced.

---

## 8. The Feedback Loop Enhanced

### How LLM Recomposition Changes Feedback

With template-based recomposition, feedback answers a narrow question: "Did the agent follow this directive?" The directive is the same text every time, so effectiveness tracking is straightforward.

With LLM recomposition, feedback becomes richer because the micro-prompt is different each time. The same directive might produce:

- Session A: "Run `go test -cover ./internal/handlers/...` and check coverage on the new inject handler."
- Session B: "Run `go test -race ./internal/store/...` to verify the new CockroachDB methods are race-free."

Both derive from the same "test your code" directive, but they are different micro-prompts with different specificity levels. Feedback now answers a richer question: **"Did this _contextualization_ of the directive produce the desired behavior?"**

### Enhanced Feedback Schema

Extend the feedback payload to capture the actual micro-prompt that was delivered:

```json
{
  "injection_id": "inj_789xyz",
  "session_id": "ses_abc123",
  "outcomes": [
    {
      "directive_id": "dir_test_after_impl",
      "snippet_content": "Run `go test -cover ./internal/handlers/...` and check coverage on the new inject handler.",
      "followed": true,
      "outcome": "positive",
      "detail": "Agent ran go test -cover, coverage was 84% on inject.go.",
      "specificity_helpful": true
    }
  ]
}
```

The `specificity_helpful` field (boolean, optional) indicates whether the contextual specificity of the micro-prompt contributed to the agent following it. This can be inferred by the MCP plugin: if the agent's tool call closely matches the specific command in the micro-prompt (rather than a generic version), specificity was helpful.

### What This Enables

**1. Directive effectiveness is measured more accurately.**

With templates, a directive that says "run tests" gets credit any time the agent runs tests -- even if the agent would have run tests anyway. With contextual micro-prompts, we can distinguish:

- The agent ran _exactly_ the command suggested (specificity was valuable)
- The agent ran tests but used a different command (the principle helped, specificity did not)
- The agent did not run tests (directive was ignored regardless of contextualization)

**2. Contextualization quality can be optimized.**

By tracking which contextualized snippets produce higher follow rates than the raw directive, we can measure the _value added_ by LLM recomposition:

```sql
-- Compare follow rate for LLM-contextualized vs. fallback (raw) delivery
SELECT
    d.id,
    d.content,
    AVG(CASE WHEN il.fallback = false AND df.followed THEN 1.0 ELSE 0.0 END) AS llm_follow_rate,
    AVG(CASE WHEN il.fallback = true AND df.followed THEN 1.0 ELSE 0.0 END) AS raw_follow_rate
FROM directives d
JOIN injection_log il ON il.directive_id = d.id
JOIN directive_feedback df ON df.injection_id = il.injection_id AND df.directive_id = d.id
WHERE il.created_at > now() - interval '30 days'
GROUP BY d.id, d.content
HAVING COUNT(*) > 20
ORDER BY (llm_follow_rate - raw_follow_rate) DESC;
```

If LLM-contextualized micro-prompts consistently produce higher follow rates, the recomposition layer is earning its cost. If not, we know to adjust the system prompt or model.

**3. The system prompt can be tuned empirically.**

Because we capture the LLM's `reasoning` field for each snippet, we can analyze _why_ certain contextualizations work and others do not. This feeds back into system prompt refinement.

**4. Directive content can be improved.**

When the LLM consistently skips a directive (returns it in the `skipped` array) despite it being selected by retrieval, that is signal that the directive's content or triggers need refinement. The `reason` field in the skip entry tells us why.

---

## 9. Concrete Examples

### Example 1: Implementing a New Handler

**Raw directives retrieved:**

| ID      | Content                                                                                                                                         | Type       | Priority |
| ------- | ----------------------------------------------------------------------------------------------------------------------------------------------- | ---------- | -------- |
| dir_001 | Stable code needs to be well tested. Always cover the happy paths at a minimum and the expected behaviors, which usually nets you 80% coverage. | behavioral | 70       |
| dir_002 | When adding new endpoints, follow the existing patterns in the codebase. Do not introduce new frameworks or routing patterns.                   | contextual | 65       |
| dir_003 | Never claim a task is complete without running the verification command and reading its full output.                                            | guardrail  | 95       |
| dir_004 | Wrap errors with context so that when a failure occurs, the error chain tells you exactly where it happened and why.                            | behavioral | 60       |

**Request context:**

```json
{
  "activity": "implementing",
  "project": { "name": "hive-server", "language": "go" },
  "context": {
    "summary": "Implementing the /api/v1/inject endpoint. Created internal/handlers/inject.go with the handler struct. Working on the fan-out retrieval logic.",
    "recent_files": [
      "internal/handlers/inject.go",
      "internal/handlers/handlers.go",
      "internal/store/directives.go"
    ],
    "error_context": null
  },
  "intent": "Implement the injection pipeline endpoint"
}
```

**Relevant memories:**

- "hive-server handlers follow a pattern: handler struct with Store interface dependency, constructor function NewXHandler(), methods that take http.ResponseWriter and \*http.Request."
- "The handlers_test.go file uses httptest.NewRecorder() for testing handlers."

**Assembled LLM prompt (user message portion):**

```
## Directives to Synthesize

1. [dir_001] (behavioral, P70): "Stable code needs to be well tested. Always cover the happy paths at a minimum and the expected behaviors, which usually nets you 80% coverage."
   Rationale: Prevents shipping untested code that breaks in production.

2. [dir_002] (contextual, P65): "When adding new endpoints, follow the existing patterns in the codebase. Do not introduce new frameworks or routing patterns."
   Rationale: Codebase consistency reduces cognitive load and merge conflicts.

3. [dir_003] (guardrail, P95): "Never claim a task is complete without running the verification command and reading its full output."
   Rationale: Agents often claim completion without verifying, leading to broken code.

4. [dir_004] (behavioral, P60): "Wrap errors with context so that when a failure occurs, the error chain tells you exactly where it happened and why."
   Rationale: Raw errors without context make debugging production issues extremely difficult.

## Current Context

- Activity: implementing
- Project: hive-server (Go)
- Summary: Implementing the /api/v1/inject endpoint. Created internal/handlers/inject.go with the handler struct. Working on the fan-out retrieval logic.
- Recent files: internal/handlers/inject.go, internal/handlers/handlers.go, internal/store/directives.go
- Error context: none
- Intent: Implement the injection pipeline endpoint

## Relevant Memories

- hive-server handlers follow a pattern: handler struct with Store interface dependency, constructor function NewXHandler(), methods that take http.ResponseWriter and *http.Request.
- The handlers_test.go file uses httptest.NewRecorder() for testing handlers.

## Token Budget

Target: 400 tokens for all snippets combined.
```

**LLM output:**

```json
{
  "snippets": [
    {
      "directive_ids": ["dir_003"],
      "content": "Before claiming the inject endpoint is complete, run `go test -v -race ./internal/handlers/...` and `go vet ./internal/handlers/...`. Read the full output -- do not just check the exit code.",
      "category": "guardrail",
      "action": "rule",
      "reasoning": "Guardrail directive applied to the specific package being implemented. Added -race flag because the injection handler involves concurrent fan-out goroutines."
    },
    {
      "directive_ids": ["dir_001"],
      "content": "Create internal/handlers/inject_test.go following the httptest.NewRecorder() pattern used in handlers_test.go. Cover at minimum: (1) successful injection with valid context, (2) missing required fields (session_id, activity), (3) token budget enforcement, (4) behavior when a retrieval source times out. Run `go test -cover ./internal/handlers/...` and target 80% coverage on inject.go.",
      "category": "behavioral",
      "action": "suggest",
      "reasoning": "Testing directive contextualized with the actual test patterns from project memories, the specific file being created, and edge cases derived from the fan-out architecture (source timeouts)."
    },
    {
      "directive_ids": ["dir_002"],
      "content": "The inject handler should follow the existing pattern: define an InjectHandler struct with a Store dependency, a NewInjectHandler() constructor, and method receivers for each route. Register routes in the existing chi router setup in handlers.go, not in a separate router.",
      "category": "contextual",
      "action": "suggest",
      "reasoning": "Pattern-following directive contextualized with the specific handler being built and the project's established architecture from memories."
    },
    {
      "directive_ids": ["dir_004"],
      "content": "In the fan-out retrieval logic, wrap errors from each data source with their origin: `fmt.Errorf(\"meilisearch query: %w\", err)`, `fmt.Errorf(\"cockroachdb query: %w\", err)`. This is critical for debugging partial failures where only one source times out.",
      "category": "behavioral",
      "action": "suggest",
      "reasoning": "Error wrapping directive applied specifically to the fan-out pattern where multiple error sources need disambiguation."
    }
  ],
  "skipped": []
}
```

**What the template approach would have produced for the same directives:**

1. "After implementing changes, run `go test ./internal/handlers/...` to verify your changes before moving on."
2. "Follow existing patterns in the codebase when adding new endpoints."
3. "Never claim completion without running verification commands."
4. "Wrap errors with context using `fmt.Errorf(\"operation: %w\", err)`."

The template versions are generic and obvious. The LLM versions reference the specific test patterns, the specific edge cases of the fan-out architecture, the specific race conditions from concurrent goroutines, and the specific error disambiguation needed when multiple sources can fail independently.

---

### Example 2: Debugging a CockroachDB Retry Failure

**Raw directives retrieved:**

| ID      | Content                                                                                                                             | Type       | Priority |
| ------- | ----------------------------------------------------------------------------------------------------------------------------------- | ---------- | -------- |
| dir_010 | Reproduce the error reliably before attempting any fix. If you cannot reproduce it, you do not understand it.                       | guardrail  | 90       |
| dir_011 | When debugging database-related failures, check whether the issue is in the query, the connection, or the schema -- do not assume.  | behavioral | 65       |
| dir_012 | Read the full error message and stack trace. Agents tend to fixate on the first line and miss critical context in subsequent lines. | behavioral | 70       |

**Request context:**

```json
{
  "activity": "debugging",
  "project": { "name": "hive-server", "language": "go" },
  "context": {
    "summary": "The CockroachDB store's UpsertDirective method is failing intermittently in tests. The error appears to be a transaction retry error.",
    "recent_files": [
      "internal/store/cockroach.go",
      "internal/store/cockroach_test.go"
    ],
    "error_context": "--- FAIL: TestUpsertDirective/concurrent_upserts (0.15s)\n    cockroach_test.go:112: upsert failed: ERROR: restart transaction: TransactionRetryWithProtoRefreshError: WriteTooOldError\n    cockroach_test.go:113: HINT: See: https://www.cockroachlabs.com/docs/v24.3/transaction-retry-error-reference.html#retry_write_too_old"
  },
  "intent": null
}
```

**Relevant memories:**

- "CockroachDB requires client-side retry logic using crdbpgx.ExecuteTx(). The transaction function must be idempotent because it may be called multiple times."
- "WriteTooOldError in CockroachDB means a concurrent transaction wrote to the same key between this transaction's read and write."

**LLM output:**

```json
{
  "snippets": [
    {
      "directive_ids": ["dir_010"],
      "content": "Reproduce the WriteTooOldError reliably first. Run `go test -v -count=10 -run TestUpsertDirective/concurrent_upserts ./internal/store/...` to confirm the failure is consistent under concurrency. If it only fails sometimes, the retry logic may be partially working but insufficient.",
      "category": "guardrail",
      "action": "rule",
      "reasoning": "Reproduce-first directive contextualized with the specific test name, the -count flag for flaky test detection, and the observation that intermittent failures suggest partial retry coverage."
    },
    {
      "directive_ids": ["dir_011", "dir_012"],
      "content": "The WriteTooOldError with HINT pointing to transaction-retry-error-reference means CockroachDB detected a write-write conflict under serializable isolation. Check cockroach.go for whether UpsertDirective uses `crdbpgx.ExecuteTx()` with a retry-safe transaction function. If it uses raw `pool.Begin()`/`tx.Commit()` instead, that is the root cause -- CockroachDB serialization errors MUST be retried by the client. Also verify the transaction function is idempotent (no side effects that accumulate on retry).",
      "category": "behavioral",
      "action": "suggest",
      "reasoning": "Combined database debugging and error-reading directives. Contextualized with the specific CockroachDB retry pattern from project memories, the exact error type, and the most likely root cause based on the error message and hint URL."
    }
  ],
  "skipped": []
}
```

**What the template approach would have produced:**

1. "Reproduce the error before attempting a fix. Run `go test -run TestUpsertDirective ./internal/store/...`."
2. "Check whether the issue is in the query, the connection, or the schema."
3. "Read the full error message and stack trace carefully."

The template version for directive dir_011 ("check query, connection, or schema") is actively misleading here -- the issue is none of those three. It is a transaction retry problem. The LLM version correctly identifies the actual failure mode (write-write conflict under serializable isolation) and points to the specific fix (use `crdbpgx.ExecuteTx()` instead of raw transactions). This is possible because the LLM has access to the error message, the CockroachDB-specific memories, and enough understanding to connect them.

---

### Example 3: Reviewing a Pull Request

**Raw directives retrieved:**

| ID      | Content                                                                                                                            | Type       | Priority |
| ------- | ---------------------------------------------------------------------------------------------------------------------------------- | ---------- | -------- |
| dir_020 | When reviewing code, check for missing error handling first. Unhandled errors are the most common source of production incidents.  | behavioral | 75       |
| dir_021 | Review changes in the context of the system, not just the diff. Ask: how does this change interact with existing code paths?       | behavioral | 70       |
| dir_022 | Security-sensitive changes (auth, crypto, user input) require extra scrutiny. Look for injection, bypass, and information leakage. | guardrail  | 85       |

**Request context:**

```json
{
  "activity": "reviewing",
  "project": { "name": "hive-server", "language": "go" },
  "context": {
    "summary": "Reviewing a PR that adds the /api/v1/inject endpoint. The PR modifies handlers.go to add routes, adds inject.go with the new handler, and adds inject_test.go with tests.",
    "recent_files": [
      "internal/handlers/inject.go",
      "internal/handlers/handlers.go",
      "internal/handlers/inject_test.go"
    ],
    "error_context": null
  },
  "intent": "Review the injection pipeline PR"
}
```

**Relevant memories:**

- "hive-server uses bearer token auth via HIVE_TOKEN env var. New endpoints under /api/v1/ are automatically protected by the auth middleware."
- "The inject endpoint accepts user-provided context.summary and context.error_context fields that get passed to downstream queries."

**LLM output:**

```json
{
  "snippets": [
    {
      "directive_ids": ["dir_022"],
      "content": "The inject endpoint accepts user-provided text in context.summary and context.error_context that gets forwarded to Meilisearch queries and potentially to the recomposition LLM. Verify that these fields are sanitized or length-bounded before use. Check for: (1) query injection into Meilisearch filter syntax, (2) prompt injection via context fields that reach the LLM, (3) unbounded field lengths that could cause memory issues or exceed downstream limits.",
      "category": "guardrail",
      "action": "rule",
      "reasoning": "Security review directive contextualized with the specific attack surfaces of the inject endpoint: Meilisearch filter injection and LLM prompt injection via user-controlled context fields."
    },
    {
      "directive_ids": ["dir_020"],
      "content": "In inject.go, check every error return from the fan-out goroutines (Meilisearch, CockroachDB, Gel DB queries). The handler must gracefully degrade when a source fails, not panic or return 500. Verify that partial failures (one source down, others up) are handled and that the response metadata accurately reports which sources responded.",
      "category": "behavioral",
      "action": "suggest",
      "reasoning": "Error handling review directive contextualized with the specific architecture of the inject handler (concurrent fan-out to three data sources) where partial failure handling is the critical error path."
    },
    {
      "directive_ids": ["dir_021"],
      "content": "Check how the new inject routes interact with the existing auth middleware in handlers.go. Verify: (1) the inject endpoint requires X-Agent-ID header (the pipeline spec says it is required, unlike other endpoints where it is optional), (2) the bearer token auth applies to inject routes, (3) the new route registration does not accidentally change middleware ordering for existing routes.",
      "category": "behavioral",
      "action": "suggest",
      "reasoning": "System-context review directive applied to the specific integration point: route registration in handlers.go where middleware ordering and header requirements differ from existing endpoints."
    }
  ],
  "skipped": []
}
```

**What the template approach would have produced:**

1. "Check for missing error handling in the new code."
2. "Consider how these changes interact with existing code paths."
3. "This PR touches auth-adjacent code. Check for security issues."

The template version of the security directive ("check for security issues") is vague enough to be unhelpful. The LLM version identifies the _specific_ attack surfaces: Meilisearch filter injection and LLM prompt injection via user-controlled fields. These are not generic security concerns -- they are the actual risks of this specific endpoint, derived from understanding what the endpoint does with user input. A template cannot identify these without someone pre-authoring a "meilisearch-injection-risk" variant of the security directive.

---

## 10. Implementation Notes

### Go Interface

```go
// Recomposer transforms raw directives into contextual micro-prompts.
type Recomposer interface {
    // Recompose takes selected directives + context and returns micro-prompts.
    Recompose(ctx context.Context, input *RecompositionInput) (*RecompositionOutput, error)
}

// LLMRecomposer implements Recomposer using an LLM for synthesis.
type LLMRecomposer struct {
    client       LLMClient         // Anthropic API client
    model        string            // e.g., "claude-haiku-4-5-20251015"
    systemPrompt string            // The recomposition system prompt
    cache        *ResponseCache    // Layer 1 cache
    preSynth     *PreSynthCache    // Layer 3 cache
    metrics      *RecomposerMetrics
}

// FallbackRecomposer returns raw directive content (no LLM).
type FallbackRecomposer struct{}
```

### LLM Client Abstraction

```go
// LLMClient abstracts the LLM API call for testability.
type LLMClient interface {
    Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
}

type CompletionRequest struct {
    Model        string
    SystemPrompt string
    UserMessage  string
    MaxTokens    int
    Temperature  float64 // 0.0 for deterministic synthesis
}
```

### Configuration

```go
type RecomposerConfig struct {
    Model              string        // LLM model ID
    APIKey             string        // Anthropic API key
    MaxLatency         time.Duration // Hard timeout for LLM call (default 10s)
    OutputTokenBudget  int           // Default output token budget (default 500)
    Temperature        float64       // LLM temperature (default 0.0)
    CacheTTL           time.Duration // Response cache TTL (default 5m)
    SessionCacheTTL    time.Duration // Session cache TTL (default 10m)
    PreSynthEnabled    bool          // Enable pre-synthesis background job
    PreSynthInterval   time.Duration // How often to regenerate pre-synth cache (default 1h)
    FallbackEnabled    bool          // Enable fallback to raw directives (default true)
}
```

### Metrics to Track

| Metric                         | Type      | Description                                   |
| ------------------------------ | --------- | --------------------------------------------- |
| `recomposition_latency_ms`     | histogram | End-to-end LLM recomposition time             |
| `recomposition_cache_hit_rate` | gauge     | Fraction of requests served from cache        |
| `recomposition_fallback_rate`  | gauge     | Fraction of requests using fallback           |
| `recomposition_tokens_input`   | counter   | Total input tokens sent to LLM                |
| `recomposition_tokens_output`  | counter   | Total output tokens received                  |
| `recomposition_cost_usd`       | counter   | Estimated cost based on token counts          |
| `recomposition_error_rate`     | gauge     | Fraction of LLM calls that fail               |
| `recomposition_skip_rate`      | gauge     | Average fraction of directives skipped by LLM |

---

## 11. Migration Path

### Phase 1: LLM Recomposer with Fallback

Implement the `LLMRecomposer` alongside the `FallbackRecomposer`. Both implement the same interface. A feature flag (`HIVE_RECOMPOSER=llm|fallback`) controls which is active. This allows A/B testing of LLM recomposition vs. raw directives.

### Phase 2: Caching Layers

Add Layers 1 and 2 (response cache and session cache). These are purely additive and reduce LLM call volume without changing behavior.

### Phase 3: Pre-Synthesis Background Job

Add Layer 3 (pre-synthesis cache). This requires a background goroutine that periodically calls the LLM to pre-generate micro-prompts for common directive/context combinations. This is the most complex cache layer but provides the best latency improvement.

### Phase 4: Feedback Integration

Extend the feedback loop to capture micro-prompt content and measure LLM recomposition effectiveness vs. raw directive delivery. Use this data to tune the system prompt and model selection.

### Phase 5: Prompt Optimization

Based on feedback data, iterate on:

- The system prompt (which instructions produce the best contextualizations)
- The model (is Haiku 4.5 sufficient or does Sonnet produce meaningfully better results for the cost?)
- The temperature (0.0 for determinism vs. 0.1-0.2 for slightly more creative contextualizations)
- The input context (which memories and context fields are most valuable for producing good micro-prompts)
