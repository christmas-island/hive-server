# Recomposition Layer Design: LLM Synthesis

**Date:** 2026-03-09
**Status:** Defines the recomposition approach for vision-v5 Section 4.6
**Supersedes:** recomposition-design.md
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

Directives are stored as **principles and patterns** -- the atomic behavioral knowledge units described in the vision (Section 2). They are retrieved based on contextual relevance by the fan-out pipeline. Then an LLM synthesizes them into **contextual micro-prompts** that are specific, actionable, and grounded in what the agent is actually doing right now.

The LLM is not reasoning or planning. It is performing a focused synthesis task: "Given these principles and this situation, produce concrete instructions." This is a translation task, not a creative task.

---

## 2. Relationship to Vision Section 4.6

The vision (v5, Section 4.6) describes contextualization as:

> Contextualization is done by an LLM call (Sonnet-class) that takes the raw directive, the context frame, and produces a version specific to the current situation. If the LLM call fails or times out (50ms budget), the raw directive is returned as-is.

The vision's 50ms contextualization budget assumed a lightweight per-directive adaptation -- essentially a fast inline rewrite of each individual directive. The recomposition approach described in this document replaces that with a **batch LLM synthesis** of all selected directives together, which has two important consequences:

1. **Higher latency.** A batch LLM call with 5-10 directives and full request context takes seconds, not milliseconds. This is a deliberate tradeoff: the batch approach produces significantly better output because the LLM can see all directives together and combine, prioritize, and contextualize them as a coherent set rather than rewriting each one in isolation.

2. **The vision's fallback behavior is preserved.** The vision specifies that raw directives are returned as-is when the LLM call fails or times out. This document preserves that fallback: when the recomposition LLM is unavailable, the system returns raw directive `content` fields. The principles themselves are still useful to the agent even without contextualization.

The user's principle is accuracy over speed. The batch synthesis approach serves that principle.

---

## 3. The Recomposition Pipeline

### Overview

```
[Fan-out retrieval completes]
        |
        v
[Selected directives: 5-10 principles/patterns]
        +
[Request context: activity, project, files, summary, memories]
        |
        v
[Assemble LLM prompt]
        |
        v
[Call recomposition LLM (Sonnet-class)]
        |
        v
[Parse structured output: micro-prompt snippets with provenance]
        |
        v
[Token budget enforcement: trim if over budget]
        |
        v
[Response]
```

### Step-by-Step

**Step 1: Retrieval (unchanged from vision Section 4)**

The fan-out queries Meilisearch, CockroachDB, and Gel DB in parallel. The ranking and selection step reduces candidates to 5-10 directives that fit within a _directive input budget_ (not the final output token budget). This step is identical to the existing design.

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
    ID              string
    Content         string // The principle/pattern text
    Kind            string // behavioral, pattern, contextual, corrective, factual
    Weight          float64 // 0.0-2.0, default 1.0
    SourceSkill     string  // superpowers, gsd, allium, custom
}
```

**Step 3: LLM Call**

Send the assembled prompt to a Sonnet-class model. The system prompt (Section 4) instructs the model on exactly what to produce. The response is structured JSON.

**Step 4: Parse and Validate**

Parse the LLM's JSON output. Validate that:

- Each micro-prompt references a valid directive ID
- The total token count is within budget
- The output is well-formed

**Step 5: Token Budget Enforcement**

If the LLM's output exceeds the token budget (it sometimes will), trim from the bottom of the weight-sorted list. Behavioral and corrective directives with high effectiveness scores are preserved over lower-weight ones.

**Step 6: Response Assembly**

Package the micro-prompts into the injection response format defined in vision Section 4.7.

---

## 4. The System Prompt

This is the system prompt for the recomposition LLM.

````
You are a directive synthesis engine. Your job is to transform abstract behavioral
principles into contextual, specific, actionable micro-prompts for an AI coding agent.

## Input

You will receive:
1. A set of DIRECTIVES -- these are principles and patterns about how to develop software well.
   Each has an ID, content (the principle), kind, and weight.
   Kinds: behavioral (how to approach work), pattern (codebase conventions), contextual
   (situation awareness), corrective (learned from mistakes), factual (things to know).
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
- Is concise (1-3 sentences)
- Uses imperative voice ("Run...", "Verify...", "Ensure...")

## Rules

1. CONTEXTUALIZE, don't parrot. Never repeat a directive verbatim. Transform the principle
   into a concrete instruction for THIS situation.
2. BE SPECIFIC. Reference actual file paths from recent_files. Use the correct test/build
   commands for the project language. Mention specific patterns if memories provide them.
3. PRIORITIZE by kind and effectiveness. High-effectiveness corrective directives (learned
   from real mistakes) are especially valuable -- contextualize them carefully.
4. COMBINE when appropriate. If two directives address the same concern for this context,
   merge them into one snippet and reference both IDs.
5. SKIP when irrelevant. If a directive does not meaningfully apply to the current context,
   omit it. Explain why in the "skipped" array.
6. STAY WITHIN BUDGET. Target {{output_token_budget}} tokens total for all snippets combined.
   If you cannot fit all directives, drop the lowest-weight ones first.
7. DO NOT add directives that were not provided. You synthesize, you do not invent.

## Output Format

```json
{
  "snippets": [
    {
      "directive_ids": ["dir_abc123"],
      "content": "The specific, contextual micro-prompt text.",
      "kind": "behavioral|pattern|contextual|corrective|factual",
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

## 5. Directive Kinds in Recomposition

The vision defines five directive kinds (Section 2.3). The recomposition LLM handles each differently:

| Kind | What It Is | Recomposition Behavior |
|------|-----------|----------------------|
| `behavioral` | How to approach a type of work | Contextualize to the specific task. "When debugging, reproduce first" becomes "Run `go test -count=10 -run TestUpsert` to confirm the failure is consistent." |
| `pattern` | Codebase-specific conventions | Ground in actual files and patterns. "Use chi v5 grouped routes" becomes "Register the inject endpoint in the existing chi router in handlers.go using r.Route()." |
| `contextual` | Situation-specific awareness | Present as situational context. "There are 2 open AUTH requirements" becomes "This work may address AUTH-01. Note the connection if the fix affects authentication." |
| `corrective` | Learned from past mistakes | Emphasize the lesson with specifics. "3/5 debug sessions were race conditions" becomes "In this repo, race conditions have been the root cause in 3 of the last 5 debug sessions. Check for races first." |
| `factual` | Things the agent should know | Present as background knowledge. "The user prefers terse responses" becomes part of the contextual framing. |

---

## 6. LLM Selection and Latency

### Why Sonnet-Class Is the Right Fit

The recomposition task is **synthesis, not reasoning**. The LLM receives:
- A clear, focused system prompt defining exactly what to do
- A small set of principles (5-10 directives, ~1000-2000 tokens)
- Concrete context about what the agent is doing (~300-500 tokens)
- A request for structured output (JSON micro-prompts)

This is a focused synthesis task, but it requires enough capability to genuinely contextualize principles -- connecting directive content with project-specific memories, error messages, and codebase patterns to produce specific, actionable micro-prompts. It does not require:
- Multi-step reasoning (no chain-of-thought needed)
- Creative generation (the principles already exist; the model contextualizes them)
- Long-context understanding (total input is ~3000 tokens)
- Tool use or agentic behavior

The specific model to use is a configuration decision. Sonnet-class models are the starting point; the model can be upgraded to Opus-class if output quality from feedback data warrants the cost increase.

### Expected Latency

Sonnet-class models are expected to complete recomposition in a few seconds for a typical request (~2000 input tokens, ~300-500 output tokens). Exact latency depends on the model, prompt size, and load.

The user's principle is accuracy over speed. A few-second delay that produces contextual, specific, actionable micro-prompts is worth more than a sub-millisecond response that produces generic template fills. Furthermore, injection is not blocking -- the agent does not wait for injection before processing the user's prompt. The MCP plugin can inject directives into the context window at any point during the conversation turn.

### Latency Mitigations

Several factors reduce the practical impact of recomposition latency:

1. **Reduced output size.** Most micro-prompt snippets are 30-60 tokens. With 5-8 snippets, output is 200-350 tokens, which generates quickly.

2. **Streaming.** Hive-server can begin processing the LLM response as tokens stream in. The first snippet is available before the LLM finishes generating.

3. **Prompt caching.** The system prompt is stable across requests. Anthropic's prompt caching reduces processing time for repeated prefixes.

4. **Frequency control.** Injection does not happen on every prompt. The MCP plugin injects on context transitions (session start, phase change, significant action, periodic refresh). At a few seconds per injection with injection every 3-5 prompts, the amortized cost is sub-second per prompt.

5. **Speculative pre-fetch.** The MCP plugin knows when a prompt is being composed. It can fire the injection request before the agent processes the user's message, overlapping injection with the agent's own thinking time.

### Cost

Sonnet-class models are cost-effective for this use case. The total input per request is approximately 2000-3000 tokens and output is approximately 300-500 tokens. At Sonnet-class pricing, the per-request cost is a fraction of a cent. The exact cost depends on the model chosen and current pricing, but it is negligible relative to the cost of the LLM agents being guided.

### Caching

Caching is an optimization to be designed after measuring real usage patterns. The system should be built to work correctly without caching first, then caching layers added based on observed patterns (e.g., how often the same directives are re-selected within a session, how much context summaries vary between consecutive requests, which directives are surfaced most frequently). Caching is planned and the architecture accommodates it, but designing specific cache layers, TTLs, and hit rate projections before a single request has been served would be premature.

---

## 7. Fallback Behavior

When the recomposition LLM is unreachable, slow (exceeds a configurable timeout), or returns invalid output, the pipeline degrades gracefully by returning raw directive content.

### Fallback: Raw Directive Content

Return the directive `content` field as-is. The principles themselves are still useful to the agent even without contextualization:

> "Stable code needs to be well tested. Always cover the happy paths at a minimum and the expected behaviors."

An agent reading this will understand it means "write tests." It will not get the specific file paths or coverage commands, but the behavioral nudge still has value. This is the fallback behavior the vision specifies in Section 4.6.

### Fallback Go Code

```go
func (r *LLMRecomposer) Recompose(ctx context.Context, input *RecompositionInput) (*RecompositionOutput, error) {
    // Try LLM synthesis
    output, err := r.callLLM(ctx, input)
    if err == nil {
        return output, nil
    }

    log.Warn("LLM recomposition failed, falling back to raw directives", "error", err)

    // Fallback: return raw directive content
    snippets := make([]MicroPrompt, 0, len(input.Directives))
    for _, d := range input.Directives {
        snippets = append(snippets, MicroPrompt{
            DirectiveIDs: []string{d.ID},
            Content:      d.Content,
            Kind:         d.Kind,
            Action:       actionForKind(d.Kind),
            Reasoning:    "LLM unavailable; returning raw directive.",
        })
    }

    return &RecompositionOutput{
        Snippets: snippets,
        Fallback: true,
    }, nil
}
````

### Monitoring

Track fallback rate as a key metric. Persistent fallback means the LLM provider is degraded and injection quality is reduced.

---

## 8. Concrete Examples

### Example 1: Implementing a New Handler

**Raw directives retrieved (5-10 range, showing 4 for brevity):**

| ID      | Content                                                                                                                                         | Kind       | Weight |
| ------- | ----------------------------------------------------------------------------------------------------------------------------------------------- | ---------- | ------ |
| dir_001 | Stable code needs to be well tested. Always cover the happy paths at a minimum and the expected behaviors, which usually nets you 80% coverage. | behavioral | 1.4    |
| dir_002 | When adding new endpoints, follow the existing patterns in the codebase. Do not introduce new frameworks or routing patterns.                   | pattern    | 1.3    |
| dir_003 | Never claim a task is complete without running the verification command and reading its full output.                                            | corrective | 1.9    |
| dir_004 | Wrap errors with context so that when a failure occurs, the error chain tells you exactly where it happened and why.                            | behavioral | 1.2    |

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

**LLM output:**

```json
{
  "snippets": [
    {
      "directive_ids": ["dir_003"],
      "content": "Before claiming the inject endpoint is complete, run `go test -v -race ./internal/handlers/...` and `go vet ./internal/handlers/...`. Read the full output -- do not just check the exit code.",
      "kind": "corrective",
      "action": "rule",
      "reasoning": "High-weight corrective directive applied to the specific package being implemented. Added -race flag because the injection handler involves concurrent fan-out goroutines."
    },
    {
      "directive_ids": ["dir_001"],
      "content": "Create internal/handlers/inject_test.go following the httptest.NewRecorder() pattern used in handlers_test.go. Cover at minimum: (1) successful injection with valid context, (2) missing required fields (session_id, activity), (3) token budget enforcement, (4) behavior when a retrieval source times out. Run `go test -cover ./internal/handlers/...` and target 80% coverage on inject.go.",
      "kind": "behavioral",
      "action": "suggest",
      "reasoning": "Testing directive contextualized with the actual test patterns from project memories, the specific file being created, and edge cases derived from the fan-out architecture (source timeouts)."
    },
    {
      "directive_ids": ["dir_002"],
      "content": "The inject handler should follow the existing pattern: define an InjectHandler struct with a Store dependency, a NewInjectHandler() constructor, and method receivers for each route. Register routes in the existing chi router setup in handlers.go, not in a separate router.",
      "kind": "pattern",
      "action": "suggest",
      "reasoning": "Pattern-following directive contextualized with the specific handler being built and the project's established architecture from memories."
    },
    {
      "directive_ids": ["dir_004"],
      "content": "In the fan-out retrieval logic, wrap errors from each data source with their origin: `fmt.Errorf(\"meilisearch query: %w\", err)`, `fmt.Errorf(\"cockroachdb query: %w\", err)`. This is critical for debugging partial failures where only one source times out.",
      "kind": "behavioral",
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

| ID      | Content                                                                                                                             | Kind       | Weight |
| ------- | ----------------------------------------------------------------------------------------------------------------------------------- | ---------- | ------ |
| dir_010 | Reproduce the error reliably before attempting any fix. If you cannot reproduce it, you do not understand it.                       | behavioral | 1.8    |
| dir_011 | When debugging database-related failures, check whether the issue is in the query, the connection, or the schema -- do not assume.  | behavioral | 1.3    |
| dir_012 | Read the full error message and stack trace. Agents tend to fixate on the first line and miss critical context in subsequent lines. | corrective | 1.4    |

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
      "kind": "behavioral",
      "action": "rule",
      "reasoning": "Reproduce-first directive contextualized with the specific test name, the -count flag for flaky test detection, and the observation that intermittent failures suggest partial retry coverage."
    },
    {
      "directive_ids": ["dir_011", "dir_012"],
      "content": "The WriteTooOldError with HINT pointing to transaction-retry-error-reference means CockroachDB detected a write-write conflict under serializable isolation. Check cockroach.go for whether UpsertDirective uses `crdbpgx.ExecuteTx()` with a retry-safe transaction function. If it uses raw `pool.Begin()`/`tx.Commit()` instead, that is the root cause -- CockroachDB serialization errors MUST be retried by the client. Also verify the transaction function is idempotent (no side effects that accumulate on retry).",
      "kind": "behavioral",
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

## 9. Go Interface

```go
// Recomposer transforms raw directives into contextual micro-prompts.
type Recomposer interface {
    // Recompose takes selected directives + context and returns micro-prompts.
    Recompose(ctx context.Context, input *RecompositionInput) (*RecompositionOutput, error)
}

// LLMRecomposer implements Recomposer using an LLM for synthesis.
type LLMRecomposer struct {
    client       LLMClient         // LLM API client
    model        string            // Configurable model ID
    systemPrompt string            // The recomposition system prompt
    timeout      time.Duration     // Hard timeout for LLM call
    metrics      *RecomposerMetrics
}

// FallbackRecomposer returns raw directive content (no LLM).
type FallbackRecomposer struct{}

// RecompositionOutput is the result of recomposition.
type RecompositionOutput struct {
    Snippets []MicroPrompt
    Fallback bool // true if LLM was unavailable and raw directives were returned
}

// MicroPrompt is a single contextualized directive snippet.
type MicroPrompt struct {
    DirectiveIDs []string // Which directives this was synthesized from
    Content      string   // The contextualized micro-prompt text
    Kind         string   // behavioral, pattern, contextual, corrective, factual
    Action       string   // rule, suggest, context
    Reasoning    string   // Why the LLM contextualized it this way
}
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
    Model              string        // LLM model ID (configurable, Sonnet-class default)
    APIKey             string        // LLM API key
    MaxLatency         time.Duration // Hard timeout for LLM call (default 10s)
    OutputTokenBudget  int           // Default output token budget (default 500)
    Temperature        float64       // LLM temperature (default 0.0)
    FallbackEnabled    bool          // Enable fallback to raw directives (default true)
}
```

### Metrics to Track

| Metric                        | Type      | Description                                   |
| ----------------------------- | --------- | --------------------------------------------- |
| `recomposition_latency_ms`    | histogram | End-to-end LLM recomposition time             |
| `recomposition_fallback_rate` | gauge     | Fraction of requests using fallback           |
| `recomposition_tokens_input`  | counter   | Total input tokens sent to LLM                |
| `recomposition_tokens_output` | counter   | Total output tokens received                  |
| `recomposition_error_rate`    | gauge     | Fraction of LLM calls that fail               |
| `recomposition_skip_rate`     | gauge     | Average fraction of directives skipped by LLM |

---

## 10. Scope

This document defines:

- **The recomposition approach**: Why LLM synthesis is the right model and how it works.
- **The LLM prompt and interface**: The system prompt, input/output formats, and directive kind handling.
- **Fallback behavior**: What happens when the LLM is unavailable.
- **The Go interface**: The `Recomposer` abstraction and its implementations.

This document does **not** define:

- The retrieval pipeline (vision Section 4, Sections 4.1-4.5).
- The feedback loop (vision Section 6). Feedback is tracked against directive IDs as the vision specifies; no extensions to the feedback schema are introduced here.
- Caching strategy (to be designed based on observed usage patterns).
- Migration planning or phased rollout.
- The injection response format (vision Section 4.7).
