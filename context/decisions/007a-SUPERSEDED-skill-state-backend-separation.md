# Decision 007: Skill State Backend — Separate Workstream

**Date:** 2026-03-10
**Status:** Superseded by 007-workflows-are-directive-chains.md
**Decided by:** JakeClaw (proposed)

## Context

ZeroClaw's review flagged that the entire skill-replacement state management layer is absent from the directive engine build graph. This includes: projects, phases, plans, tasks with dependencies, session snapshots, skills registry, allium specs, requirements traceability, progress metrics.

## Proposal

**Separate workstream.** The skill state backend shares databases (CRDB, Meilisearch, Gel) with the directive engine but has no code-level dependencies on it. Different API surface, different concerns (structured CRUD + graph queries vs. LLM-powered decomposition/recomposition).

## Rationale

- Vision v5 explicitly reframes hive-server as a "behavioral knowledge engine" — not a project management backend
- The state management layer is important but orthogonal to directive decomposition/injection/feedback
- Existing hive-server already has tasks + memory + agents (partial coverage)
- Can be built in parallel by different agents since infrastructure (CRDB, Meili, Gel) is shared
- Interleaving would bloat the directive engine graph with unrelated CRUD issues

## What Goes in Skill State Backend

- Projects (lifecycle container, config, status, phases)
- Plans (content, status enum, dependency edges)
- Dependency wave computation (adjacency graph → parallel execution groups)
- Requirements traceability (req→phase→plan→task chain)
- Session snapshots (pause/resume agent state, distinct from injection agent_sessions)
- Skills registry (name, content, priority, version + Meilisearch discovery)
- Allium specs (AST storage, drift reports, cross-spec impact via Gel)
- Progress/velocity metrics

## Shared Infrastructure

Both workstreams depend on Phase 0 infrastructure (I1-I4) and the store interface composition (I3). The skill state backend adds new `*Store` interfaces composed into the same `Store`.
