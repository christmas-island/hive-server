# Decision 005: Concurrent Decomposition — Not a Concern

**Date:** 2026-03-10
**Status:** Decided
**Decided by:** Jake + JakeClaw

## Context

ZeroClaw flagged: what if two agents ingest the same skill doc simultaneously?

## Decision

**Not a design concern.** Skill ingestion is a controlled batch load/bootstrap process, not a concurrent agent-driven operation. The `ingestion_sources` unique constraint on `(name, tenant_id)` handles the trivial edge case via `INSERT ... ON CONFLICT DO NOTHING`.

## Rationale

- Skills are loaded as a batch operation during bootstrap
- Agents don't independently submit skills for decomposition in normal operation
- Race conditions are not expected in practice
- CRDB unique constraint is sufficient if it ever happens
