# Decision 002: No Decomposition Prompt Versioning

**Date:** 2026-03-10
**Status:** Decided
**Decided by:** Jake + JakeClaw

## Context

ZeroClaw flagged: what happens when the LLM extraction prompt changes? Re-decompose everything? Only new sources? Versioned directive sets?

## Decision

**No prompt versioning.** This is a living, evolving system.

## Rationale

- Decomposition prompts evolve, directives evolve — versioning treats it like a reproducible build when it's a living knowledge base
- Old directives that become irrelevant get weeded by the feedback loop (weight decay, auto-deprecation after 3 negatives)
- New decomposition runs against the same source produce better directives that supersede old ones via `supersedes_id`
- The existing feedback + supersession mechanisms already handle directive lifecycle

## Implications

- `decomposition_runs` table tracks provenance (which prompt produced which directives) but doesn't enforce version compatibility
- Re-ingesting a source is safe — dedup + supersession handles overlap
- No need for bulk migration tooling when prompts change
