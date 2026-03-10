# Decision 001: Tokenizer Strategy

**Date:** 2026-03-10
**Status:** Decided
**Decided by:** Jake + JakeClaw

## Context

Directives have a `token_cost INT4` field computed at decomposition time, used for greedy budget packing during injection.

## Decision

Use **chars/4 approximation** for token cost estimation. No model-specific tokenizer.

## Rationale

- Avoids embedding/GPU dependency
- Budget is a soft target anyway — ranking quality matters more than exact token counts
- Confidence dropoff rule naturally limits injection before budget is hit
- Good enough at the granularity we're operating (50-100 tokens per directive)

## Implications

- No tokenizer dependency in the codebase
- `token_cost` is approximate, not precise
- Meilisearch uses keyword/BM25 search only, not hybrid vector mode
- Token budget default left unspecified — tune during integration testing
