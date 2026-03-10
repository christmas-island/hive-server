## Injection Pipeline v5 Summary

### Pipeline Stages

- **Parse + Validate**: Receives POST body, extracts agent_id from `X-Agent-ID` header, validates required fields (agent_id, session_id, phase, conversation_summary). In: HTTP request. Out: typed request struct.

- **Retrieval Fan-Out** (parallel errgroup): Launches 3 concurrent queries; merges results as they arrive; skips failed/slow sources.

  - **Meilisearch**: Semantic search on `conversation_summary + intent`. Filtered by tenant_id, active=true, trigger_phase, trigger_scope. Returns ranked candidates with `_rankingScore`.
  - **CockroachDB**: Two queries — (1) phase+scope matched directives ordered by effectiveness/weight, LIMIT 50; (2) recent injection IDs for this session (last 30min) for deduplication.
  - **Gel DB**: Graph traversal from found directive IDs → behavioral chains → ordered sequence members.

- **Merge + Deduplicate**: Exact ID dedup (merge metadata, keep highest score); semantic near-dup detection (keep most specific/actionable version).

- **Rank + Select**: Score each candidate, sort descending, pack into token budget greedily using pre-computed `token_cost` field. Apply cooldown penalty for directives ignored in last 3 calls.

- **LLM Recomposition**: Sonnet-class model synthesizes selected directives + request context → structured JSON micro-prompts. Fallback: return raw directive content.

- **Response Assembly**: Build response JSON, store injection record for audit + deduplication.

### Endpoints

**POST /api/v1/inject**

- Auth: `Bearer <token>`, required header `X-Agent-ID`
- Request: `{agent_id, session_id, context: {intent, files, repo, phase, recent_actions, conversation_summary, open_requirements, current_project}, token_budget (default 500), previous_injection_id}`
- `phase` enum: `planning | implementation | debugging | review | brainstorming`
- Response: `{injection_id, directives: [{id, content, kind, source, confidence}], tokens_used, token_budget, candidates_considered, candidates_selected}`
- Directive kinds: `behavioral | pattern | contextual | corrective | factual`

**POST /api/v1/feedback**

- Request: `{injection_id, outcomes: [{directive_id, outcome: followed|ignored|negative, evidence}], session_outcome, session_summary}`
- Updates `directives` effectiveness metrics; negative outcome reduces weight by 20% (floor 0.1); 3 negatives → auto-deprecate.

**POST /api/v1/feedback/session-complete**

- Request: `{session_id, summary, repo, outcome, key_insight}`
- Triggers experience-derived directive creation from session outcomes.

### Ranking / Scoring

```
score = (meilisearch_relevance * 0.4)
      + (effectiveness * 0.3)
      + (weight * 0.2)
      + (recency_bonus * 0.1)
```

- `meilisearch_relevance`: 0.0–1.0 from `_rankingScore`; defaults to 0.5 for CRDB/Gel-only results
- `effectiveness`: `(times_followed - times_negative) / GREATEST(times_injected, 1)`
- `weight`: 0.0–2.0, set at creation; anti-rationalization directives get higher initial weight
- `recency_bonus`: 0.0–0.5 for recent experience-derived corrective directives
- Stop filling budget at natural confidence dropoff (don't pad with low-confidence noise)
- Session dedup: penalize directives already in `previous_injection_id`'s set

### Token Budget Management

- Each directive has pre-computed `token_cost` (set during enrichment, not runtime estimation)
- Greedy pack: sort by score desc, add while `token_cost <= remaining_budget`
- Reserve tokens for response wrapper
- 500-token budget → 8–12 directives; 200-token budget → 3–5
- LLM output capped to fit within requested budget

### Performance Characteristics

- Fan-out target: sub-100ms (parallel Meilisearch + CRDB + Gel)
- LLM recomposition: 2.5–3.5s total (300–400ms TTFT + 2,500–3,000ms generation)
- **Total P50: ~3–4 seconds cold; sub-second on cache hit**
- Parallelism: all 3 retrieval sources concurrent via errgroup; merge on first arrival
- Degradation order: drop Gel first, then Meilisearch, then CRDB (source of truth)
- Caching: full response cache keyed on `sorted(directive_ids) + phase + project + context_summary_prefix`; session-level directive cache for unchanged directive sets
- Injection is non-blocking — MCP plugin fires speculatively, agent doesn't wait
