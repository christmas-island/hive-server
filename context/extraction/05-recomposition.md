## Recomposition Design v5 Summary

### What Recomposition Does

Takes 5-10 retrieved directives (principles/patterns) + request context and calls an LLM to synthesize them into specific, actionable micro-prompt snippets grounded in what the agent is currently doing.

### LLM Integration

- **Model:** Sonnet-class (configurable); Opus-class upgrade path if quality warrants
- **Temperature:** 0.0 (deterministic synthesis)
- **Prompt structure:**
  - System: defines synthesis engine role, output rules, JSON format spec
  - User: selected directives (ID, content, kind, weight) + request context (activity, project, language, summary, recent_files, error_context, intent, memories)
- **Input:** ~2000–3000 tokens (5–10 directives + context)
- **Output:** Structured JSON — `snippets[]` + `skipped[]`
  ```json
  {
    "snippets": [
      {
        "directive_ids": ["dir_abc"],
        "content": "...",
        "kind": "...",
        "action": "rule|suggest|context",
        "reasoning": "..."
      }
    ],
    "skipped": [{ "directive_id": "dir_xyz", "reason": "..." }]
  }
  ```

### Synthesis Logic

- **Raw → contextual:** LLM transforms abstract principles into concrete instructions referencing actual files, commands, patterns from request context + memories
- **Grouping:** Multiple directives addressing the same concern merged into one snippet (references both IDs)
- **Deduplication:** Implicit via LLM merge (no separate dedup step)
- **Conflict handling:** Not specified — LLM prioritizes by kind + weight
- **Skipping:** LLM drops directives irrelevant to current context; must explain in `skipped[]`
- **Token budget enforcement:** Post-parse trim — drop lowest-weight snippets until within `OutputTokenBudget`; behavioral/corrective/high-weight preserved

### Caching / Optimization

- **No caching in v1** — build correct first, cache based on observed patterns
- Architecture accommodates future caching (e.g., per-session directive reuse)
- **Latency mitigations:** streaming, Anthropic prompt caching (stable system prompt), speculative pre-fetch (fire before agent processes user message), frequency control (inject every 3–5 prompts not every prompt)
- **Timeout:** configurable hard cutoff (default 10s)

### Interface

```go
type Recomposer interface {
    Recompose(ctx context.Context, input *RecompositionInput) (*RecompositionOutput, error)
}
// Implementations: LLMRecomposer (primary), FallbackRecomposer (raw content pass-through)
```

- **Input struct:** `RecompositionInput{Directives, Activity, ProjectName, ProjectLanguage, ContextSummary, RecentFiles, ErrorContext, Intent, ProjectMemories, AgentMemories, OutputTokenBudget}`
- **Output struct:** `RecompositionOutput{Snippets []MicroPrompt, Fallback bool}`
- **Fallback:** On LLM error/timeout, return raw directive `content` fields unchanged with `Fallback: true`
- **Config:** `RecomposerConfig{Model, APIKey, MaxLatency, OutputTokenBudget, Temperature, FallbackEnabled}`
- **Metrics:** latency_ms, fallback_rate, tokens_input, tokens_output, error_rate, skip_rate
