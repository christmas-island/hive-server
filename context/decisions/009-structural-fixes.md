# Decision 009: Build Graph Structural Fixes

**Date:** 2026-03-10
**Status:** Decided
**Decided by:** JakeClaw (from ZeroClaw review, validated)

## Fixes Applied to SYNTHESIS-v3

### Merge S1 into S2

S1 (Directive data model — just a Go struct) merges into S2 (Directive CRDB schema + store). No independent value as a separate issue. Every consumer of the struct also needs the persistence layer.

### Split P4

P4 was two unrelated things:

- **P4a: Outcome feedback + weight evolution** — `POST /api/v1/feedback`, counter updates, weight decay (×0.8, floor 0.1), auto-deprecation after 3 negatives, cooldown for ignored. Depends on S2, S3. No LLM involvement.
- **P4b: Experience-derived directive generation** — `POST /api/v1/feedback/session-complete`, LLM analyzes session summary to create new directives (source_type=experience). Mini decomposition pipeline. Depends on P4a, I4 (LLM client), S4 (Meilisearch dedup), S5 (sync).

### Move slog into I1

`log/slog` migration happens during project layout restructure (I1), not late-stage hardening (X3). Every subsequent issue benefits from structured logging. Prometheus metrics remain in X3 (needs pipeline code to instrument).

### skip_recomposition flag

Add `skip_recomposition: bool` to the inject request from day one. Returns raw ranked directives when true, LLM-recomposed micro-prompts when false. Decouples P1 (pipeline core) from P2 (recomposition) at the API level — P1 is independently functional.
