# GSD (Get Shit Done) -- Technology Brief

## What It Is

GSD is a meta-prompting and context engineering system for LLM-powered coding agents
(Claude Code, OpenCode, Gemini CLI, Codex). It solves "context rot" -- the quality
degradation that occurs as an LLM fills its context window during long development
sessions. GSD provides a structured, spec-driven workflow that decomposes projects into
phases, plans, and tasks, each executed with fresh context windows by specialized
sub-agents.

Distributed as an npm package (`get-shit-done-cc`), installed via
`npx get-shit-done-cc@latest`. MIT licensed, ~27k GitHub stars. Built by a solo developer
(TACHES). The system registers slash commands (e.g., `/gsd:plan-phase`) into the host
agent's command system.

## Architecture

### Multi-Agent Orchestration

GSD operates a hierarchical agent model:

- **Orchestrator**: Lightweight coordinator (~15% context budget). Discovers plans,
  analyzes dependencies, groups into waves, spawns sub-agents. Never does heavy lifting.
- **Sub-agents**: Specialized agents (researcher, planner, executor, verifier, debugger,
  etc.) each spawned with a fresh 200k-token context. This is the core mechanism for
  defeating context rot.

Agent definitions live in `agents/` as markdown files (12 agents total):

| Agent                      | Role                                                      |
| -------------------------- | --------------------------------------------------------- |
| `gsd-project-researcher`   | Initial project domain research                           |
| `gsd-phase-researcher`     | Phase-specific research                                   |
| `gsd-research-synthesizer` | Combines parallel research outputs                        |
| `gsd-planner`              | Creates PLAN.md files from research                       |
| `gsd-plan-checker`         | Validates plans against requirements (up to 3 iterations) |
| `gsd-executor`             | Executes individual plans/tasks                           |
| `gsd-verifier`             | Post-execution verification                               |
| `gsd-debugger`             | Systematic debugging of failures                          |
| `gsd-codebase-mapper`      | Analyzes existing codebases (brownfield)                  |
| `gsd-integration-checker`  | Verifies integrations                                     |
| `gsd-nyquist-auditor`      | Validates test coverage maps to requirements              |
| `gsd-roadmapper`           | Creates phase-based project roadmaps                      |

### Wave-Based Parallel Execution

Plans within a phase are organized into dependency waves:

- **Wave 1**: Independent tasks with no dependencies (run in parallel)
- **Wave 2**: Tasks depending on Wave 1 outputs (run after Wave 1 completes)
- **Wave N**: And so on

Concurrency is configurable (default max 3 parallel agents, minimum 2 plans to trigger
parallelism).

### Model Profile System

Agents can be assigned different model tiers:

| Profile    | Planning | Execution | Verification |
| ---------- | -------- | --------- | ------------ |
| `quality`  | Opus     | Opus      | Opus         |
| `balanced` | Opus     | Sonnet    | Sonnet       |
| `budget`   | Sonnet   | Sonnet    | Haiku        |

## Core Workflow

Six-step lifecycle, repeated per phase within a milestone:

1. **Initialize** (`/gsd:new-project`): Questions -> parallel research (4 agents) ->
   requirements extraction -> roadmap creation
2. **Discuss** (`/gsd:discuss-phase`): Capture user preferences (UI, API formats,
   patterns). Output: `CONTEXT.md`
3. **Plan** (`/gsd:plan-phase`): Research -> create 2-3 atomic task plans in XML ->
   verify against requirements -> iterate until plan-checker passes
4. **Execute** (`/gsd:execute-phase`): Run plans in dependency-ordered waves, each
   executor with fresh context, atomic git commits per task
5. **Verify** (`/gsd:verify-work`): User acceptance testing, automated diagnostics,
   fix plan generation for failures
6. **Complete** (`/gsd:complete-milestone`): Archive, tag release, start next milestone

Additional commands: `/gsd:quick` (fast path for small changes), `/gsd:map-codebase`
(brownfield analysis), `/gsd:debug`, `/gsd:add-phase`, `/gsd:insert-phase`,
`/gsd:audit-milestone`, `/gsd:progress`, plus ~20 more (32 commands total).

## Data Formats and Schemas

### File System as Database

All state is stored as markdown files in a `.planning/` directory tree. There is no
database, no server, no API. The filesystem IS the persistence layer.

```
.planning/
  PROJECT.md              # Vision, core value, constraints, key decisions
  REQUIREMENTS.md         # Scoped requirements with IDs (AUTH-01, etc.)
  ROADMAP.md              # Phase breakdown with progress tracking
  STATE.md                # Current position, metrics, session continuity
  MILESTONES.md           # Completed milestone archive
  config.json             # Workflow configuration
  phases/
    XX-phase-name/
      XX-YY-PLAN.md       # Atomic execution plans (XML-structured)
      XX-YY-SUMMARY.md    # Execution outcomes
      CONTEXT.md          # User preferences for this phase
      RESEARCH.md         # Domain research findings
      VERIFICATION.md     # Post-execution verification results
  research/               # Parallel research outputs
  todos/
    pending/              # Captured ideas
  debug/                  # Debug session artifacts
  codebase/               # Codebase analysis results
  quick/                  # Quick-mode task tracking
```

### STATE.md -- The Living Memory

Constrained to under 100 lines. Acts as short-term memory spanning sessions:

```markdown
## Current Position

Phase: [X] of [Y] ([Phase name])
Plan: [A] of [B] in current phase
Status: [Ready to plan | Planning | In progress | Phase complete]
Last activity: [YYYY-MM-DD] -- [What happened]
Progress: [----------] 0%

## Performance Metrics

Total plans completed: [N]
Average duration: [X] min

## Accumulated Context

### Decisions (recent; full log in PROJECT.md)

### Pending Todos

### Blockers/Concerns

## Session Continuity

Last session: [YYYY-MM-DD HH:MM]
Stopped at: [Description]
Resume file: [Path to .continue-here*.md]
```

### REQUIREMENTS.md -- Requirement Tracking

Requirements use category-prefixed IDs with checkbox tracking:

```markdown
## v1 Requirements

### Authentication

- [ ] **AUTH-01**: User can sign up with email and password
- [ ] **AUTH-02**: User receives email verification after signup

## v2 Requirements (deferred)

## Out of Scope (with reasons)

## Traceability (requirement -> phase mapping)
```

### ROADMAP.md -- Phase Progression

Phases use integer numbering (1, 2, 3) with decimal insertions (2.1, 2.2) for urgent
work. Each phase tracks:

- Goal, dependencies, linked requirements
- Success criteria (2-5 observable behaviors)
- Plan listing with checkbox completion
- Progress table with status values: Not started | In progress | Complete | Deferred

### PLAN.md -- XML Task Format

Plans are structured as prompts (not documents that become prompts). Each plan contains
2-3 tasks maximum, targeting ~50% context usage:

```xml
<task type="auto">
  <name>Create login endpoint</name>
  <files>src/app/api/auth/login/route.ts</files>
  <action>Implementation instructions with specific tech choices</action>
  <verify>curl -X POST localhost:3000/api/auth/login returns 200</verify>
  <done>Acceptance criteria</done>
</task>
```

Task types: `auto` (Claude executes fully), plus checkpoint types for human verification
or decisions. Every task must include specific file paths, action instructions (including
what to avoid and why), automated verification commands, and acceptance criteria.

### config.json -- Workflow Configuration

```json
{
  "mode": "interactive", // or "yolo" (auto-approve)
  "granularity": "standard", // coarse (3-5 phases), standard (5-8), fine (8-12)
  "workflow": {
    "research": true,
    "plan_check": true,
    "verifier": true,
    "nyquist_validation": true
  },
  "planning": {
    "commit_docs": true,
    "search_gitignored": false,
    "branching_strategy": "none" // or "phase" or "milestone"
  },
  "concurrency": {
    "parallel_execution": true,
    "max_agents": 3,
    "min_plans_for_parallel": 2,
    "skip_checkpoints": true
  }
}
```

## Git Integration

GSD commits outcomes, not process:

- **Committed**: Project init, completed tasks (one commit per task), plan completion
  metadata (SUMMARY + STATE + ROADMAP), handoff states
- **Not committed**: Intermediate planning artifacts (PLAN.md, RESEARCH.md, DISCOVERY.md)

Commit format follows conventional commits:

- `feat(02-01): create login endpoint`
- `docs: initialize project-name (6 phases)`
- `wip: phase-2 paused at task 3/5`

Branching strategies: none (all on current branch), phase-level branches, or
milestone-level branches.

## How It Integrates with LLM Agents

GSD integrates as a **slash command provider** into LLM coding agents. It registers
commands via the agent's custom command system (e.g., Claude Code's `.claude/commands/`
directory). Each command is a markdown file containing a structured prompt that the host
agent executes.

The integration pattern:

1. User invokes `/gsd:plan-phase 3`
2. Host agent loads the command's markdown prompt
3. Prompt instructs the agent to read specific `.planning/` files, perform operations,
   and write results back to `.planning/` files
4. Sub-agents are spawned via the host agent's `Task` tool with fresh contexts
5. All coordination happens through the filesystem -- agents read/write markdown files

There is no API, no RPC, no network protocol. The "protocol" is:

- Markdown files with specific naming conventions
- XML-structured task blocks within PLAN.md files
- Conventional commit messages in git
- File-based state machine (STATE.md tracks position)

## State Management and Persistence

### State Machine

State transitions are implicit, tracked via STATE.md status field:
`Ready to plan -> Planning -> Ready to execute -> In progress -> Phase complete`

No formal state machine -- it is a string field in a markdown file, updated by whichever
agent last wrote to it.

### Session Continuity

- `/gsd:pause-work`: Creates a `.continue-here.md` file capturing current state
- `/gsd:resume-work`: Reads `.planning/` files to reconstruct context
- STATE.md stores last session timestamp, last action, and resume file path
- The system recommends `/clear` between major commands to free context

### Persistence Model

- **Storage**: Local filesystem (`.planning/` directory)
- **Format**: Markdown files + one JSON config
- **Sharing**: Via git (if `commit_docs: true`)
- **Querying**: File reads + grep (no indexing, no search)
- **Concurrency**: None -- single-agent-at-a-time within a phase (parallelism is across
  plans, not within them, and coordinated by the orchestrator)
- **Schema enforcement**: None -- templates provide structure but nothing validates
  conformance
- **History**: Git log (if committed) or lost

## Verification Architecture (Nyquist Layer)

The "Nyquist Rule" requires that verification includes automated checks for every task.
If tests do not exist, Wave 0 creates them first (TDD mode). The plan-checker rejects
plans lacking automated verification commands.

Verification patterns include:

- Existence checks (file at expected path)
- Stub detection (TODO/FIXME/placeholder patterns)
- Wiring verification (component -> API -> database connections)
- Substantive content checks (not just boilerplate)
- Human verification triggers (visual appearance, user flows, real-time behavior)

## Strengths

1. **Context rot mitigation**: Fresh 200k contexts per sub-agent is genuinely effective
   at maintaining output quality across long projects.
2. **Structured decomposition**: Forces clear requirements, phases, plans, and tasks
   before execution. Prevents the "vibe coding" failure mode.
3. **Parallel execution**: Wave-based dependency resolution enables meaningful
   parallelism across independent tasks.
4. **Atomic git history**: Per-task commits enable precise bisection and independent
   reversion.
5. **Low barrier to entry**: `npx` install, markdown files, no infrastructure needed.
6. **Agent-agnostic design**: Works with Claude Code, OpenCode, Gemini CLI, Codex.
7. **Comprehensive verification**: Nyquist layer ensures test coverage maps to
   requirements before implementation begins.
8. **Brownfield support**: Can analyze and work with existing codebases, not just
   greenfield projects.

## Limitations

1. **No real database**: All state is markdown on the local filesystem. No querying, no
   indexing, no relational integrity, no concurrent access from multiple machines or
   agents writing simultaneously.
2. **No network/API layer**: Cannot share state across machines, teams, or services
   without git. No real-time collaboration, no webhooks, no event streaming.
3. **No schema validation**: Templates suggest structure but nothing enforces it.
   Malformed STATE.md or REQUIREMENTS.md will silently degrade behavior.
4. **Single-machine, single-user**: Designed for one developer on one machine. No
   multi-user coordination, no conflict resolution beyond git merge.
5. **Fragile state**: STATE.md is a 100-line markdown file tracking complex workflow
   state. Any corruption, accidental edit, or parsing ambiguity breaks the system.
6. **No search/discovery**: Finding information across accumulated research, plans, and
   summaries requires grep. No semantic search, no indexing, no cross-referencing
   beyond manual requirement ID traceability.
7. **No structured data**: Requirements, decisions, metrics, and traceability are all
   embedded in markdown prose/tables. Extracting structured data for analysis requires
   parsing markdown, which is inherently brittle.
8. **No historical analytics**: Performance metrics are a few lines in STATE.md.
   No time-series data, no trend analysis beyond "last 5 plans" velocity.
9. **Git-coupled persistence**: If `commit_docs: false`, planning artifacts can be lost.
   If `commit_docs: true`, git history becomes the only audit trail with no independent
   backup or replication.
10. **Context window assumptions**: The entire system is designed around Claude's 200k
    token context. Different models with different context sizes may not work well with
    the hardcoded sizing assumptions.

## Opportunities for Database Enhancement

Given GSD's limitations, database technologies could address several gaps:

**Structured State Storage** (any database): Replace fragile markdown state files with
typed, validated records. STATE.md's position tracking, REQUIREMENTS.md's requirement
IDs, and ROADMAP.md's phase progression would all benefit from schema-enforced storage.

**Full-Text and Semantic Search** (Meilisearch): Research documents, plan content, and
summaries accumulate across phases with no way to search them. Meilisearch could index
all `.planning/` content for instant retrieval, typo-tolerant search, and faceted
filtering by phase/milestone/status.

**Multi-Agent Coordination** (CockroachDB): The current single-machine, filesystem-based
state prevents true distributed agent coordination. A distributed database could enable
multiple agents (or multiple users) to work on the same project with transactional
consistency.

**Graph Relationships** (Gel DB): Requirements -> phases -> plans -> tasks -> commits
form a natural graph. Requirement traceability (which phases cover which requirements,
which tasks implement which plans) is currently done via markdown tables with manual ID
references. A graph database could make these relationships first-class, queryable, and
automatically maintained.

**Time-Series Metrics**: Velocity tracking, context usage patterns, and execution
duration trends are currently limited to "last 5 plans" in STATE.md. Any database with
time-series support could provide rich analytics.

**Event Sourcing**: GSD's workflow is inherently event-driven (phase started, plan
created, task completed, verification passed). An event-sourced persistence model would
provide complete audit trails and enable replaying/debugging workflow issues.
