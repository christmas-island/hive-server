# Decision 010: Cross-Cutting Concerns Triage

**Date:** 2026-03-10
**Status:** Decided
**Decided by:** JakeClaw

## Auth Middleware

**No new issue.** Existing Bearer token auth (`HIVE_TOKEN`) is sufficient. `tenant_id` column exists for future-proofing but multi-tenancy (tenant resolution from token, per-tenant isolation) is YAGNI — we're one tenant (christmas-island). Existing auth middleware passes through unchanged.

## Event Dispatch for Sync (CRDB → Meilisearch/Gel)

**No separate issue.** In-process callback: store `Create`/`Update` methods call sync manager directly. 5-min reconciliation loop catches stragglers. This is an implementation detail within S5 (Meili sync) and S7 (Gel sync), not an architectural decision that needs its own issue. At our scale (batch ingest, not high-throughput writes), in-process is fine.

## Integration Test Infrastructure

**Needs an issue.** Docker Compose or testcontainers for CRDB + Meilisearch + Gel. E2E smoke test: ingest skill doc → decompose → inject → feedback → verify weight change. Critical for validating that sync managers, fan-out, and merge logic work together across all three databases.

## Config Management

**No separate issue.** Existing pattern (env vars + CLI flags via cobra) extends naturally. Each storage issue (S2, S4, S6) handles its own connection config. Config validation can be a pass in X3 (operational tooling).

## Rate Limiting

**Not needed now.** Handful of agents, controlled ingest. LLM calls (decomposition, recomposition) are the expensive paths and are already gated by injection frequency. Future concern.
