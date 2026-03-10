# GUIDANCE.md — Guiding Principles for hive-server v5

> **This is the guiding star.** When resolving conflicts, making design decisions,
> or choosing between implementation approaches, refer to this document first.
> If something contradicts this doc, this doc wins.

## Purpose

hive-server v5 is a **behavioral knowledge engine** — not a CRUD API, not a chatbot
framework, not a prompt management system. It ingests skill documents, decomposes
them into atomic behavioral directives, and recomposes contextually relevant subsets
into agent conversations at runtime.

The goal: agents get better at their jobs over time, transparently, without knowing
the system exists.

## Core Principles

### 1. Transparency Above All

Agents never explicitly call inject or feedback. The system operates through
ContextEngine hooks (`assemble` and `afterTurn`). If an agent has to know about
directives to benefit from them, the design is wrong.

### 2. CRDB Is the Source of Truth

CockroachDB is the authoritative store. Meilisearch and Gel are derived indexes —
optimized read paths, not independent data stores. If there's a conflict between
databases, CRDB wins. Post-merge filtering against CRDB's `active` field is the
consistency mechanism.

### 3. Simplicity Over Cleverness

- Plain text over structured formats (Decision 003)
- `chars/4` over real tokenizers (Decision 001)
- No prompt versioning — living system with feedback (Decision 002)
- No dual-write fallbacks — operational recovery over implementation complexity (Decision 008)
- Skip concurrent decomposition concerns — not a real scenario (Decision 005)

When choosing between a simple approach that works and a clever approach that
might work better, choose simple. We can always add complexity later; removing
it is much harder.

### 4. Workflows Are Knowledge

Workflows and processes are themselves behavioral knowledge — directive chains
with gate conditions and phase progression (Decision 007). There is no separate
"workflow engine." The directive engine IS the workflow engine.

### 5. Best-Effort Over Blocking

- LLM recomposition has a `skip_recomposition` escape hatch
- Meilisearch/Gel being down doesn't block injection (degrade to CRDB-only)
- Feedback attribution doesn't need to be perfect — directional signal is enough
- Sync managers are eventually consistent, not transactional

The system should always return something useful, even if degraded.

### 6. Test What Matters

- Unit tests for business logic (store, handlers, pipeline stages)
- Integration tests for cross-database consistency
- E2E smoke tests for the full injection flow
- Don't test infrastructure plumbing or mock everything — test behavior

## Decision Authority

Architectural decisions are recorded in `context/decisions/`. The current
decisions (001–010) were made by Jake and JakeClaw during the v5 design phase.
They are binding unless explicitly superseded by a new decision doc.

**When you encounter a conflict:**

1. Check `context/decisions/` — if there's a relevant decision, follow it
2. Check `context/synthesis-v3.md` — the build graph is the implementation plan
3. Apply the principles above
4. If still ambiguous, make a decision, document it as a new `context/decisions/NNN-*.md`, and move forward

**Do not:**

- Reopen decided questions without new information
- Add abstraction layers "for future flexibility" — build what's needed now
- Introduce new databases or storage backends without a decision doc
- Change the three-database architecture (CRDB + Meilisearch + Gel)

## Technical Constraints

- **Language:** Go 1.25+
- **API framework:** Huma v2 + chi v5
- **Primary store:** CockroachDB (PostgreSQL wire protocol)
- **Search:** Meilisearch (BM25 keyword only — no embeddings, no vectors)
- **Graph:** Gel DB (for chain traversal and relationship queries)
- **LLM:** Anthropic Claude (Sonnet-class for decomposition/recomposition)
- **Deployment:** k8s on DigitalOcean, GHCR images, PR-per-deploy flow
- **Merge strategy:** Rebase only (`gh pr merge --rebase`)

## Build Order

The critical path is: **I1 → I2 → S2 → P1**

Infrastructure first, then the directive data model, then the injection pipeline.
Everything else fans out from there. See `context/synthesis-v3.md` for the full
dependency graph.

## What This Repo Is NOT

- Not a general-purpose MCP server (that's hive-local)
- Not a plugin system (that's hive-plugin)
- Not an agent framework (agents are OpenClaw's domain)
- Not a vector database or embedding system

The existing memory/tasks/agents endpoints remain as-is. v5 adds the directive
engine alongside them, not replacing them.
