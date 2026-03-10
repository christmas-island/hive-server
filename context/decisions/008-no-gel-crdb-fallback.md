# Decision 008: No CRDB Fallback for Gel DB

**Date:** 2026-03-10
**Status:** Decided
**Decided by:** Jake

## Context

ZeroClaw recommended designing GraphStore with a CRDB recursive CTE fallback in case Gel DB proves unstable (flagged as 🔴 risk).

## Decision

**No fallback needed.** We run backups on Gel DB. If it goes down, we restore from backup. No need to maintain a parallel CRDB graph implementation.

## Rationale

- Operational recovery (backups + restore) is simpler than maintaining two graph implementations
- CRDB recursive CTEs are a poor substitute for Gel's native graph traversal anyway
- Dual implementation doubles testing surface for questionable benefit
- Gel DB is a core architectural choice, not a risky experiment to hedge against
