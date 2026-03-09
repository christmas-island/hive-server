# Permanent GSD State in Gel DB: Architecture Analysis

**Date:** 2026-03-09
**Status:** Architecture evaluation -- not a build plan
**Depends on:** [gsd.md](gsd.md), [gel-db.md](gel-db.md), [hive-server-current.md](hive-server-current.md)

---

## 1. The Problem

GSD stores all project state as markdown files in `.planning/`. This works surprisingly well for a single developer on a single machine, but it has fundamental limitations:

- **No querying**: "Which requirements are still unmet across all my projects?" requires grep across multiple repos.
- **No relational integrity**: A requirement ID in REQUIREMENTS.md references a phase in ROADMAP.md by string match. Nothing prevents dangling references.
- **No history beyond git**: Performance metrics are a few lines in STATE.md. Historical trends require archaeology through git log.
- **No cross-project visibility**: Each `.planning/` directory is an island. There is no way to ask "what is the overall status of the christmas-island initiative?"
- **No concurrent access**: The filesystem is the concurrency model. Two agents writing STATE.md simultaneously will corrupt it.
- **Fragile parsing**: Every agent that reads STATE.md must parse markdown. Any formatting drift silently breaks downstream consumers.

Gel DB's graph-relational model directly addresses these limitations. The question is whether the added complexity is worth what it enables.

---

## 2. GSD's Implicit Data Model

GSD does not define a formal schema, but its markdown files imply one. Extracting it:

### Entities

| Entity       | Source File            | Identity                       | Key Fields                                                     |
| ------------ | ---------------------- | ------------------------------ | -------------------------------------------------------------- |
| Project      | PROJECT.md             | Name                           | Vision, constraints, key decisions                             |
| Requirement  | REQUIREMENTS.md        | Category-prefixed ID (AUTH-01) | Description, version scope (v1/v2/deferred), completion status |
| Phase        | ROADMAP.md             | Integer (1, 2, 2.1)            | Goal, dependencies, success criteria, status                   |
| Plan         | XX-YY-PLAN.md          | Phase.Plan number (02-01)      | Tasks, dependency wave, status                                 |
| Task         | XML blocks in PLAN.md  | Positional within plan         | Name, type, files, action, verify command, done criteria       |
| Agent        | Sub-agent spawns       | Agent type name                | Role, model profile                                            |
| Milestone    | MILESTONES.md          | Sequential                     | Phases included, completion date                               |
| Decision     | PROJECT.md, CONTEXT.md | Implicit                       | Description, rationale, timestamp                              |
| Research     | RESEARCH.md per phase  | Phase-scoped                   | Findings, sources                                              |
| Verification | VERIFICATION.md        | Phase-scoped                   | Pass/fail, diagnostics                                         |

### Relationships (currently implicit via string references)

```
Project ──1:N──> Milestone ──1:N──> Phase
Phase ──1:N──> Plan ──1:N──> Task
Requirement ──N:M──> Phase         (traceability matrix)
Phase ──depends_on──> Phase        (dependency graph)
Plan ──depends_on──> Plan          (wave ordering)
Task ──assigned_to──> Agent
Task ──modifies──> File
Task ──produces──> Commit
Verification ──validates──> Phase
Research ──informs──> Phase
Decision ──applies_to──> Phase | Project
```

This is a rich graph. In markdown, every one of these relationships is a string that an LLM must parse and hope matches something. In Gel, they become typed, navigable links.

---

## 3. Gel Schema for GSD

### 3.1 Core Schema (SDL)

```sdl
module gsd {

    # --- Abstract base types ---

    abstract type HasTimestamps {
        required created_at: datetime { default := datetime_current(); };
        required updated_at: datetime { default := datetime_current(); };
    }

    abstract type HasStatus {
        required status: str;
    }

    # --- Scalar types ---

    scalar type RequirementScope extending enum<'v1', 'v2', 'deferred', 'out_of_scope'>;

    scalar type PhaseStatus extending enum<
        'not_started',
        'ready_to_plan',
        'planning',
        'ready_to_execute',
        'in_progress',
        'phase_complete',
        'deferred'
    >;

    scalar type PlanStatus extending enum<
        'not_started',
        'in_progress',
        'complete',
        'failed',
        'skipped'
    >;

    scalar type TaskType extending enum<'auto', 'checkpoint', 'human_verify', 'decision'>;

    scalar type TaskOutcome extending enum<
        'pending',
        'success',
        'failure',
        'skipped'
    >;

    scalar type ModelTier extending enum<'opus', 'sonnet', 'haiku'>;

    scalar type AgentRole extending enum<
        'project_researcher',
        'phase_researcher',
        'research_synthesizer',
        'planner',
        'plan_checker',
        'executor',
        'verifier',
        'debugger',
        'codebase_mapper',
        'integration_checker',
        'nyquist_auditor',
        'roadmapper',
        'orchestrator'
    >;

    # --- Core entities ---

    type Project extending HasTimestamps {
        required name: str { constraint exclusive; };
        required vision: str;
        constraints: str;
        repo_url: str;
        multi milestones: Milestone {
            on target delete delete source;
        };
        multi requirements: Requirement;
        multi decisions: Decision;

        # Computed: overall progress
        progress := math::mean(
            (SELECT .milestones.phases.progress_pct)
        );
    }

    type Milestone extending HasTimestamps {
        required number: int32;
        required name: str;
        description: str;
        multi phases: Phase {
            on target delete delete source;
        };
        completed_at: datetime;

        constraint exclusive on ((.number, .<milestones[is Project]));
    }

    type Requirement extending HasTimestamps {
        required req_id: str { constraint exclusive; };  # AUTH-01, API-03, etc.
        required category: str;                          # AUTH, API, UI, etc.
        required description: str;
        required scope: RequirementScope { default := 'v1'; };
        required completed: bool { default := false; };
        out_of_scope_reason: str;

        # Back-links
        multi phases := .<requirements[is Phase];
        multi tasks := .<implements[is Task];
    }

    type Phase extending HasTimestamps {
        required number: float32;  # Integer phases (1,2,3) or insertions (2.1, 2.2)
        required name: str;
        required goal: str;
        required status: PhaseStatus { default := 'not_started'; };
        multi success_criteria: str;
        multi requirements: Requirement;      # Which requirements this phase addresses
        multi depends_on: Phase;              # Phase-level dependencies
        multi plans: Plan {
            on target delete delete source;
        };
        context_notes: str;                   # From CONTEXT.md
        research_summary: str;                # From RESEARCH.md

        # Computed
        progress_pct := (
            SELECT count((SELECT .plans FILTER .status = 'complete'))
                / math::max({count(.plans), 1})
                * 100
        );

        # Back-links
        milestone := .<phases[is Milestone];
        depended_on_by := .<depends_on[is Phase];
    }

    type Plan extending HasTimestamps {
        required plan_number: str;  # "02-01", "03-02"
        required name: str;
        required status: PlanStatus { default := 'not_started'; };
        required wave: int32 { default := 1; };  # Dependency wave for parallel execution
        multi depends_on: Plan;                   # Plan-level dependencies
        multi tasks: Task {
            on target delete delete source;
        };
        summary: str;                             # From SUMMARY.md

        # Back-links
        phase := .<plans[is Phase];
        depended_on_by := .<depends_on[is Plan];
    }

    type Task extending HasTimestamps {
        required name: str;
        required task_type: TaskType { default := 'auto'; };
        required outcome: TaskOutcome { default := 'pending'; };
        action: str;                              # Implementation instructions
        multi target_files: str;                  # File paths this task modifies
        verify_command: str;                      # Automated verification command
        done_criteria: str;                       # Acceptance criteria
        commit_sha: str;                          # Git commit produced
        multi implements: Requirement;            # Direct requirement traceability
        duration_seconds: int32;                  # Execution time
        context_tokens_used: int32;               # Token usage tracking

        # Agent assignment
        assigned_agent: AgentExecution;

        # Back-links
        plan := .<tasks[is Plan];
    }

    # --- Agent tracking ---

    type AgentExecution extending HasTimestamps {
        required role: AgentRole;
        required model_tier: ModelTier;
        started_at: datetime;
        completed_at: datetime;
        tokens_input: int64;
        tokens_output: int64;
        success: bool;
        error_message: str;

        # What this agent worked on
        multi tasks := .<assigned_agent[is Task];
    }

    # --- Supporting entities ---

    type Decision extending HasTimestamps {
        required description: str;
        required rationale: str;
        applies_to_phase: Phase;
        # Back-link
        project := .<decisions[is Project];
    }

    type Verification extending HasTimestamps {
        required phase: Phase;
        required passed: bool;
        diagnostics: str;
        multi failed_criteria: str;
        multi fix_suggestions: str;
    }

    type DebugSession extending HasTimestamps {
        required phase: Phase;
        required task: Task;
        required problem_description: str;
        resolution: str;
        resolved: bool { default := false; };
    }
}
```

### 3.2 What the Schema Captures That Markdown Cannot

**Typed enums for status fields.** In markdown, STATE.md contains `Status: In progress` as a free-text string. An LLM might write "In Progress", "in_progress", "IN PROGRESS", or "in progress" -- all different strings. Gel's `PhaseStatus` enum rejects invalid values at write time.

**Bidirectional links.** `Phase.requirements` links to requirements, and `Requirement.phases` is a computed back-link. In markdown, if ROADMAP.md says "Phase 2 covers AUTH-01, AUTH-02" but REQUIREMENTS.md does not list Phase 2 under those requirements, the data is inconsistent. Gel maintains both directions from a single link declaration.

**Cascading deletes.** Deleting a milestone cascades to its phases, plans, and tasks. In markdown, deleting a phase directory leaves orphaned references in ROADMAP.md, REQUIREMENTS.md, and STATE.md.

**Computed properties.** `Phase.progress_pct` is always correct because it is derived from actual plan completion counts. In markdown, progress percentages are manually maintained strings that drift from reality.

---

## 4. EdgeQL Queries That Replace Grep

### 4.1 Current state of a project (replaces reading STATE.md + ROADMAP.md)

```edgeql
SELECT gsd::Project {
    name,
    progress,
    milestones: {
        number,
        name,
        phases: {
            number,
            name,
            status,
            progress_pct,
            plans: {
                plan_number,
                status,
                wave
            } ORDER BY .wave THEN .plan_number
        } ORDER BY .number
        FILTER .status != 'phase_complete'
    }
}
FILTER .name = 'christmas-island';
```

This single query returns a nested object with the full project state. In GSD today, this requires reading STATE.md, ROADMAP.md, and walking the `phases/` directory to check individual plan statuses.

### 4.2 Requirement traceability (replaces manual ID cross-referencing)

```edgeql
# Which requirements are unmet, and what phases/tasks address them?
SELECT gsd::Requirement {
    req_id,
    description,
    scope,
    completed,
    phases: {
        number,
        name,
        status
    },
    tasks: {
        name,
        outcome,
        plan: {
            plan_number,
            phase: {
                number,
                name
            }
        }
    }
}
FILTER NOT .completed AND .scope = gsd::RequirementScope.v1
ORDER BY .req_id;
```

In GSD, answering "which v1 requirements are still incomplete?" means reading REQUIREMENTS.md, finding unchecked boxes, then cross-referencing each ID against ROADMAP.md's traceability section, then checking actual plan completion status. This is exactly the kind of multi-file, multi-hop query that LLMs get wrong when their context window is degraded.

### 4.3 Dependency graph analysis (replaces implicit wave ordering)

```edgeql
# Find all plans that are blocked (dependencies not yet complete)
SELECT gsd::Plan {
    plan_number,
    name,
    wave,
    phase: { number, name },
    blocking_deps := (
        SELECT .depends_on {
            plan_number,
            status
        }
        FILTER .status != 'complete'
    )
}
FILTER EXISTS .blocking_deps;
```

### 4.4 Cross-project queries (impossible with filesystem)

```edgeql
# What is every agent working on across all projects?
SELECT gsd::AgentExecution {
    role,
    model_tier,
    started_at,
    tasks: {
        name,
        outcome,
        plan: {
            phase: {
                milestone: {
                    .<milestones[is gsd::Project]: {
                        name
                    }
                }
            }
        }
    }
}
FILTER NOT EXISTS .completed_at
ORDER BY .started_at;
```

### 4.5 Historical analytics (replaces "last 5 plans" in STATE.md)

```edgeql
# Average task duration by agent role and model tier
SELECT (
    GROUP gsd::AgentExecution { role, model_tier }
    USING
        role := .role,
        tier := .model_tier
    BY role, tier
) {
    role := .key.role,
    tier := .key.tier,
    avg_duration := math::mean(.elements.tasks.duration_seconds),
    total_tasks := count(.elements.tasks),
    success_rate := (
        count((SELECT .elements.tasks FILTER .outcome = gsd::TaskOutcome.success))
        / math::max({count(.elements.tasks), 1})
        * 100
    )
};
```

```edgeql
# Velocity trend: plans completed per day over the last 30 days
WITH
    cutoff := datetime_current() - <duration>'30 days',
    completed_plans := (
        SELECT gsd::Plan
        FILTER .status = 'complete' AND .updated_at > cutoff
    )
SELECT {
    date := <str>cal::to_local_date(.updated_at, 'UTC'),
    count := count(completed_plans)
}
GROUP BY .date
ORDER BY .date;
```

### 4.6 Find requirements with no implementation path

```edgeql
# Requirements that no phase addresses -- gaps in the roadmap
SELECT gsd::Requirement {
    req_id,
    description
}
FILTER
    .scope = gsd::RequirementScope.v1
    AND NOT EXISTS .phases
    AND NOT .completed;
```

This is a gap detection query that is genuinely impossible with GSD's current markdown approach without an LLM reading every file and reasoning about completeness.

---

## 5. How hive-server Mediates

### 5.1 Architecture

GSD agents do not talk to Gel directly. hive-server is the intermediary:

```
Agent (Claude Code)
    |
    | /gsd:execute-phase 3
    |
    v
GSD Slash Command (markdown prompt)
    |
    | Instructs agent to call hive tool
    |
    v
hive-local (Go, localhost:18820)
    |
    | HTTP POST /api/v1/gsd/phases/3/execute
    |
    v
hive-server (Go, k8s)
    |
    | EdgeQL via gel-go client
    |
    v
Gel DB (EdgeQL -> PostgreSQL)
```

### 5.2 API Surface on hive-server

hive-server exposes GSD-specific endpoints that map to the Gel schema. These sit alongside the existing memory/task/agent endpoints:

```
# Project lifecycle
POST   /api/v1/gsd/projects                    # Create project
GET    /api/v1/gsd/projects/{name}              # Full project state (nested)
GET    /api/v1/gsd/projects/{name}/status       # Current position (replaces STATE.md read)

# Requirements
POST   /api/v1/gsd/projects/{name}/requirements           # Create requirement
GET    /api/v1/gsd/projects/{name}/requirements            # List with traceability
PATCH  /api/v1/gsd/projects/{name}/requirements/{req_id}   # Update status/scope
GET    /api/v1/gsd/projects/{name}/requirements/gaps        # Unaddressed requirements

# Phases
POST   /api/v1/gsd/projects/{name}/phases                  # Create phase
GET    /api/v1/gsd/projects/{name}/phases/{num}             # Phase detail with plans
PATCH  /api/v1/gsd/projects/{name}/phases/{num}/status      # Transition status
GET    /api/v1/gsd/projects/{name}/phases/{num}/blocked     # Blocked dependencies

# Plans and tasks
POST   /api/v1/gsd/projects/{name}/phases/{num}/plans              # Create plan
PATCH  /api/v1/gsd/projects/{name}/phases/{num}/plans/{id}/status  # Update plan status
POST   /api/v1/gsd/projects/{name}/phases/{num}/plans/{id}/tasks   # Record task outcome

# Analytics
GET    /api/v1/gsd/analytics/velocity?project={name}&days=30
GET    /api/v1/gsd/analytics/agents?project={name}
GET    /api/v1/gsd/analytics/requirements?project={name}
```

### 5.3 The Sync Problem: Markdown as Source vs. Database as Source

The critical design decision: **which is the source of truth?**

**Option A: Gel is source of truth, markdown is generated.**
GSD commands write to hive-server API. hive-server writes to Gel. STATE.md, ROADMAP.md, and REQUIREMENTS.md are generated from Gel on demand (or cached locally). The `.planning/` directory becomes a read-only cache.

- Pro: Single source of truth. No sync drift. All queries go through Gel.
- Pro: Markdown files still exist for git history and agent context loading.
- Con: GSD's entire command system (32 commands) needs rewriting to use the API instead of file I/O.
- Con: Offline operation breaks (no network = no state).

**Option B: Markdown is source of truth, Gel is a read replica.**
GSD continues to write markdown files. A sync process (triggered by hive-local or a git hook) parses `.planning/` files and upserts the structured data into Gel via hive-server. Queries go through Gel, writes go through markdown.

- Pro: GSD works unmodified. Zero changes to the 32 commands.
- Pro: Offline operation preserved. Sync happens when network is available.
- Con: Dual source of truth. Markdown can drift from Gel if sync fails.
- Con: Parsing markdown is inherently brittle (the exact problem we're trying to solve).
- Con: Gel data is eventually consistent, not real-time.

**Option C: Hybrid -- Gel is source of truth for structured data, markdown for prose.**
Structured fields (status, requirement IDs, phase numbers, completion flags, metrics) live in Gel. Prose content (research findings, context notes, action instructions, verification diagnostics) lives in markdown files referenced by Gel records. GSD commands write structured data to hive-server API and prose to local files.

- Pro: Structured data has integrity guarantees. Prose stays where LLMs can read it directly.
- Pro: Minimal GSD command changes -- only status transitions and requirement tracking move to API calls.
- Con: Split persistence. Must keep file references in Gel pointing to valid local files.
- Con: Cross-machine operation requires syncing prose files (git handles this, but adds latency).

**Recommendation: Option C (hybrid) for initial integration, with a migration path to Option A.**

Option C gives the highest value for the lowest integration cost. The structured data that benefits most from Gel (statuses, dependencies, requirement traceability, metrics) moves to the database. The prose content that LLMs need to read directly (research, action instructions, verification details) stays in markdown. Over time, as hive-server's memory system matures, prose content can migrate to the memory API (searchable via Meilisearch), and Option A becomes the end state.

### 5.4 Integration Flow (Option C)

```
GSD Command: /gsd:execute-phase 3
    |
    +--> Agent reads phase detail from hive-server API
    |    GET /api/v1/gsd/projects/foo/phases/3
    |    Response includes plan list, dependency waves, requirement links
    |
    +--> Agent reads prose from local .planning/phases/03-*/
    |    RESEARCH.md, CONTEXT.md, PLAN.md files (action instructions)
    |
    +--> Agent spawns executor sub-agents per plan
    |
    +--> Each executor:
    |    1. Reads task details from hive-server (structured)
    |    2. Reads action instructions from local PLAN.md (prose)
    |    3. Executes the task
    |    4. Reports outcome to hive-server API:
    |       PATCH /api/v1/gsd/.../tasks/{id}
    |       { outcome: "success", duration_seconds: 142, commit_sha: "abc123" }
    |    5. Writes SUMMARY.md locally (prose)
    |
    +--> Orchestrator updates phase status via API
         PATCH /api/v1/gsd/.../phases/3/status
         { status: "phase_complete" }
```

### 5.5 gel-go Implementation Pattern

The hive-server GSD handler layer would follow the existing `Store` interface pattern, but backed by gel-go:

```go
// internal/gelstore/gsd.go

type GSDStore struct {
    client *gel.Client
}

func (s *GSDStore) GetProjectStatus(ctx context.Context, name string) (*ProjectStatus, error) {
    var result ProjectStatus
    err := s.client.QuerySingle(ctx, `
        SELECT gsd::Project {
            name,
            progress,
            current_phase := (
                SELECT .milestones.phases {
                    number,
                    name,
                    status,
                    progress_pct
                }
                FILTER .status NOT IN {'not_started', 'phase_complete', 'deferred'}
                ORDER BY .number
                LIMIT 1
            )
        }
        FILTER .name = <str>$0
    `, &result, name)
    return &result, err
}

func (s *GSDStore) RecordTaskOutcome(ctx context.Context, params TaskOutcomeParams) error {
    return s.client.Tx(ctx, func(ctx context.Context, tx geltypes.Tx) error {
        // Update the task
        err := tx.Execute(ctx, `
            UPDATE gsd::Task
            FILTER .name = <str>$0 AND .plan.plan_number = <str>$1
            SET {
                outcome := <gsd::TaskOutcome>$2,
                duration_seconds := <int32>$3,
                commit_sha := <optional str>$4,
                updated_at := datetime_current()
            }
        `, params.TaskName, params.PlanNumber, params.Outcome,
           params.DurationSeconds, params.CommitSHA)
        if err != nil {
            return err
        }

        // Check if all tasks in the plan are complete, auto-update plan status
        return tx.Execute(ctx, `
            UPDATE gsd::Plan
            FILTER .plan_number = <str>$0
                AND NOT EXISTS (
                    SELECT .tasks FILTER .outcome = gsd::TaskOutcome.pending
                )
            SET {
                status := 'complete',
                updated_at := datetime_current()
            }
        `, params.PlanNumber)
    })
}

func (s *GSDStore) GetBlockedPlans(ctx context.Context, project string) ([]BlockedPlan, error) {
    var results []BlockedPlan
    err := s.client.Query(ctx, `
        SELECT gsd::Plan {
            plan_number,
            name,
            wave,
            phase: { number, name },
            blocking_deps := (
                SELECT .depends_on { plan_number, status }
                FILTER .status != 'complete'
            )
        }
        FILTER
            EXISTS .blocking_deps
            AND .phase.milestone.<milestones[is gsd::Project].name = <str>$0
    `, &results, project)
    return results, err
}
```

---

## 6. New Capabilities Enabled

### 6.1 Cross-Project Dashboard

With multiple projects stored in Gel, a single query produces an executive summary:

```edgeql
SELECT gsd::Project {
    name,
    progress,
    open_requirements := count((
        SELECT .requirements FILTER NOT .completed AND .scope = gsd::RequirementScope.v1
    )),
    active_phases := (
        SELECT .milestones.phases { number, name, status, progress_pct }
        FILTER .status = 'in_progress'
    ),
    blocked_plans := count((
        SELECT .milestones.phases.plans
        FILTER EXISTS (SELECT .depends_on FILTER .status != 'complete')
    ))
}
ORDER BY .name;
```

This is impossible with filesystem GSD. Each project's `.planning/` directory is a silo.

### 6.2 Agent Performance Analytics

Track which model tiers actually produce better outcomes:

```edgeql
# Does Opus actually outperform Sonnet on executor tasks?
WITH executions := (
    SELECT gsd::AgentExecution
    FILTER .role = gsd::AgentRole.executor
)
SELECT {
    tier := .model_tier,
    total := count(executions),
    success_rate := (
        count((SELECT .tasks FILTER .outcome = gsd::TaskOutcome.success))
        / math::max({count(.tasks), 1})
        * 100
    ),
    avg_duration := math::mean(.tasks.duration_seconds),
    avg_tokens := math::mean(.tokens_input + .tokens_output)
}
GROUP executions BY .model_tier;
```

This directly informs the model profile system. If Sonnet achieves 95% success rate at 40% of the token cost, the `balanced` profile should be the default. Today, GSD has no data to make this decision.

### 6.3 Dependency Cycle Detection

```edgeql
# Find phases that (transitively) depend on themselves
# Gel supports recursive path traversal
WITH RECURSIVE
    phase_deps := (
        SELECT gsd::Phase {
            number,
            name,
            transitive_deps := .depends_on.depends_on.depends_on  # Depth-limited
        }
    )
SELECT phase_deps
FILTER phase_deps IN phase_deps.transitive_deps;
```

Circular dependencies in GSD's current system silently cause infinite loops in the wave scheduler. With Gel, they can be detected before execution begins.

### 6.4 Requirement Coverage Heatmap

```edgeql
# Requirements sorted by implementation coverage
SELECT gsd::Requirement {
    req_id,
    description,
    phase_count := count(.phases),
    task_count := count(.tasks),
    completed_task_count := count((
        SELECT .tasks FILTER .outcome = gsd::TaskOutcome.success
    )),
    coverage_pct := (
        count((SELECT .tasks FILTER .outcome = gsd::TaskOutcome.success))
        / math::max({count(.tasks), 1})
        * 100
    )
}
FILTER .scope = gsd::RequirementScope.v1
ORDER BY .coverage_pct ASC;
```

The Nyquist auditor currently works by having an LLM read REQUIREMENTS.md and test files to assess coverage. This query replaces guesswork with data.

### 6.5 Session Continuity Without Fragile STATE.md

Instead of a 100-line markdown file that must be parsed correctly every time:

```edgeql
# What STATE.md tries to be, but reliable
SELECT gsd::Project {
    name,
    current_milestone := (
        SELECT .milestones { number, name }
        FILTER NOT EXISTS .completed_at
        ORDER BY .number
        LIMIT 1
    ),
    current_phase := (
        SELECT .milestones.phases {
            number,
            name,
            status,
            progress_pct,
            current_plan := (
                SELECT .plans { plan_number, name, status }
                FILTER .status IN {'not_started', 'in_progress'}
                ORDER BY .wave THEN .plan_number
                LIMIT 1
            ),
            total_plans := count(.plans),
            completed_plans := count((SELECT .plans FILTER .status = 'complete'))
        }
        FILTER .status NOT IN {'phase_complete', 'deferred', 'not_started'}
        ORDER BY .number
        LIMIT 1
    ),
    recent_decisions := (
        SELECT .decisions { description, created_at }
        ORDER BY .created_at DESC
        LIMIT 5
    ),
    metrics := {
        total_completed := count((
            SELECT .milestones.phases.plans FILTER .status = 'complete'
        )),
        avg_task_duration := math::mean(
            .milestones.phases.plans.tasks.duration_seconds
        )
    }
}
FILTER .name = <str>$project_name;
```

This query returns everything STATE.md contains, computed from actual data rather than maintained by string manipulation. It cannot become stale or inconsistent because it is derived, not stored.

---

## 7. Tradeoffs

### 7.1 What Is Gained

| Capability                         | Markdown GSD                 | Gel GSD                            |
| ---------------------------------- | ---------------------------- | ---------------------------------- |
| Query requirements by status/scope | Grep through REQUIREMENTS.md | EdgeQL with typed filters          |
| Requirement-to-task traceability   | Manual ID cross-referencing  | Link traversal                     |
| Cross-project visibility           | Impossible                   | Single query                       |
| Dependency cycle detection         | Runtime failure              | Pre-execution validation           |
| Historical velocity metrics        | "Last 5 plans" in STATE.md   | Full time-series analytics         |
| Session continuity                 | Parse 100-line markdown      | Computed query, always correct     |
| Concurrent multi-agent writes      | Filesystem corruption        | ACID transactions (via PostgreSQL) |
| Schema validation                  | None (templates only)        | Type system + constraints          |
| Agent performance comparison       | No data                      | Structured execution records       |
| Offline operation                  | Full capability              | Degraded (see below)               |

### 7.2 What Is Lost

**Simplicity.** GSD today is `npx install` and you have markdown files. Adding Gel means running a database server (1GB RAM minimum), managing schema migrations, and maintaining a network connection to hive-server. The "zero infrastructure" advantage disappears.

**Direct file readability.** Any developer can open `.planning/STATE.md` in a text editor and see exactly what is going on. Database state requires tooling to inspect. This matters for debugging.

**Offline-first operation.** GSD works on an airplane. Gel-backed GSD requires network access to hive-server (or a local Gel instance, which adds yet more infrastructure).

**Git as audit trail.** Every STATE.md change is visible in `git log -p .planning/STATE.md`. Database state changes require explicit audit logging.

**Agent-agnostic operation.** GSD works with any LLM agent that can read/write files. Adding hive-server API calls means agents need HTTP client capabilities (which most have, but it is an additional assumption).

### 7.3 Complexity Added

| Component                    | New Dependency                | Resource Cost           |
| ---------------------------- | ----------------------------- | ----------------------- |
| Gel DB server                | geldata/gel Docker image      | 1 GB RAM minimum        |
| PostgreSQL (Gel backend)     | Managed PostgreSQL or bundled | Standard PG resources   |
| gel-go client in hive-server | `github.com/geldata/gel-go`   | Compile-time dependency |
| Schema migrations            | `.esdl` files + `gel migrate` | CI pipeline step        |
| GSD command modifications    | Modified slash commands       | Development effort      |
| hive-server GSD endpoints    | New handler + store layer     | ~500-1000 LOC           |
| Monitoring                   | Gel metrics + health probes   | Dashboard setup         |

Total new infrastructure: 1 additional server process, 1 additional database, ~1000 LOC in hive-server, modifications to GSD slash commands.

### 7.4 Risk Assessment

**Schema evolution.** GSD's data model is implicit and changes with each release. The schema above is inferred from current behavior. If GSD introduces new concepts (sub-tasks, task groups, conditional phases), the Gel schema must evolve in lockstep. This is manageable because Gel has diff-based migrations, but it adds a coupling that does not exist with markdown.

**Parsing reliability (Option B/C).** If markdown remains any part of the source of truth, the parsing layer must handle format drift across GSD versions. This is the same brittleness problem we are trying to solve, just moved to a different layer.

**Performance.** EdgeQL queries compile to single PostgreSQL queries (no N+1), but the Gel server adds a network hop and compilation step that reading a local file does not have. For the read-heavy, write-light pattern of GSD state checks, this latency is likely negligible. For bulk operations (importing a large existing `.planning/` tree), it could be noticeable.

**Adoption barrier.** Any GSD user who wants Gel-backed state must run hive-server infrastructure. This makes the feature opt-in for power users rather than the default. The vanilla markdown workflow must remain fully functional.

---

## 8. Phased Implementation Path

### Phase 1: Read-only analytics (lowest risk)

- Add the Gel schema to hive-server
- Build a one-time import tool that parses existing `.planning/` directories and populates Gel
- Expose read-only analytics endpoints (`/api/v1/gsd/analytics/*`)
- GSD continues writing markdown as before
- Value: Cross-project visibility, historical metrics, requirement gap analysis
- Cost: Schema + import tool + analytics endpoints (~500 LOC)

### Phase 2: Structured writes for status transitions

- GSD commands that change status (phase transitions, plan completion, task outcomes) write to hive-server API in addition to markdown
- Dual-write ensures markdown stays current for direct file reading
- Value: ACID status transitions, dependency validation before execution, real-time progress tracking
- Cost: Modified GSD commands for status writes (~200 LOC per command, 5-8 key commands)

### Phase 3: Gel as source of truth for structured data (Option C)

- Status fields, requirement tracking, dependency graphs, and metrics move to Gel as source of truth
- Markdown files are generated from Gel for agent context loading and git history
- Prose content (research, action instructions) stays in local files
- Value: Full schema validation, no more stale STATE.md, reliable session continuity
- Cost: GSD command rewrite for structured data reads + markdown generation layer

### Phase 4: Full database backing (Option A, long-term)

- Prose content moves to hive-server memory API (backed by Meilisearch for search)
- `.planning/` directory becomes entirely generated (or eliminated)
- All state lives in the database, queryable, searchable, consistent
- Value: Complete solution. No dual source of truth. Full-text search across all project artifacts.
- Cost: Major GSD architectural change. Requires mature hive-server memory + search infrastructure.

---

## 9. Conclusion

Gel DB is a strong fit for GSD's implicit graph of requirements, phases, plans, tasks, and agents. The graph-relational model maps naturally to these relationships, and EdgeQL's path traversal syntax makes queries like "which requirements have no implementation path" trivial instead of impossible.

The practical question is not whether the model fits -- it clearly does -- but whether the infrastructure cost is justified by the capabilities gained. For a single developer on a single project, markdown files in `.planning/` are good enough. For multi-project, multi-agent coordination (which is what hive-server exists to enable), a real database is not optional.

The hybrid approach (Option C) with phased implementation provides the best risk/reward profile: start with read-only analytics that add zero risk to existing GSD workflows, progressively move structured data to Gel as confidence builds, and preserve the ability to fall back to pure markdown at any point.

The key insight is that GSD already has a data model. It is just encoded as formatting conventions in markdown files, enforced by nothing, and queryable only by an LLM reading prose. Gel makes that data model explicit, enforced, and queryable by machines. That is a meaningful upgrade for infrastructure that coordinates autonomous agents.
