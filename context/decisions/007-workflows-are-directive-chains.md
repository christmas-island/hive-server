# Decision 007: Workflows Are Directive Chains (Supersedes separation proposal)

**Date:** 2026-03-10
**Status:** Decided
**Decided by:** Jake + JakeClaw

## Context

Initially proposed separating "skill state backend" (projects, phases, plans, workflows) from the directive engine as a parallel workstream. Jake corrected the framing: workflows and processes ARE behavioral knowledge. The behavioral knowledge engine includes behaviors that are essentially workflows — like "what does good software development look like?"

## Decision

**Workflows are directive chains, not a separate system.** The skill-replacement state management (projects, phases, plans, dependency waves, gate conditions) is part of the directive engine's data model, not an adjacent CRUD backend.

## How It Maps

The directive engine already has the primitives:

- **DirectiveChains** — ordered sequences with `sequence_in_chain` = workflow steps
- **Phase gating** — `trigger_phase` = which stage of a workflow fires which directives
- **Feedback loops** — outcomes evolve directive surfacing over time
- **agent_sessions** — already tracks `phase` and `project_id`

A "good software development" workflow is a DirectiveChain:

1. Review the issue → behavioral directive for issue analysis
2. Research the topic → directive for domain investigation
3. Research existing solutions → directive for prior art survey
4. Propose architecture → directive for architecture documentation
5. Review → directive for self-review methodology
6. ...through implementation, testing, deployment, monitoring

Each link fires when the agent reaches that phase. The recomposition LLM contextualizes for the specific project.

## What's Missing (needs issues)

- **Richer workflow state in agent_sessions** — not just "which phase" but "which step within which chain, what's completed, what's blocked"
- **Project-scoped directive filtering** — directives scoped to project types (Go API, React app, infrastructure)
- **Gate conditions on chain links** — a directive might require a previous link's outcome (followed/completed) before surfacing the next one
- **Dependency wave computation** — parallel execution groups within a workflow (some steps can run concurrently)
- **Progress tracking** — completion %, velocity, blockers per project/workflow

## Implications

- No separate "skill state backend" workstream — it's all one engine
- The injection pipeline needs to be aware of workflow progression, not just current phase
- `agent_sessions` schema becomes richer (current_chain_id, current_step, completed_steps)
- DirectiveChain gets gate conditions (prerequisite outcomes before next step surfaces)
- The existing tasks/memory CRUD in hive-server remains as-is (utility endpoints), but isn't the workflow tracking mechanism
