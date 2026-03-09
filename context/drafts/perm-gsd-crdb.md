# Permutation Analysis: GSD + CockroachDB

**Date:** 2026-03-09
**Purpose:** Deep analysis of how CockroachDB could enhance GSD's (Get Shit Done) skill system, exploring schema designs, coordination primitives, tradeoffs, and practical integration through hive-server.

---

## Table of Contents

1. [The Fundamental Tension](#1-the-fundamental-tension)
2. [Schema Design: GSD's Data Model in SQL](#2-schema-design-gsds-data-model-in-sql)
3. [JSONB for Semi-Structured Fields](#3-jsonb-for-semi-structured-fields)
4. [Explicit State Machines with CHECK Constraints](#4-explicit-state-machines-with-check-constraints)
5. [Event Sourcing for State Transitions](#5-event-sourcing-for-state-transitions)
6. [Multi-Agent Coordination via Distributed Transactions](#6-multi-agent-coordination-via-distributed-transactions)
7. [Cross-Project Querying](#7-cross-project-querying)
8. [Transaction Retries and GSD Workflows](#8-transaction-retries-and-gsd-workflows)
9. [Practical Integration: hive-server as Mediator](#9-practical-integration-hive-server-as-mediator)
10. [Tradeoff Analysis](#10-tradeoff-analysis)
11. [Migration Path](#11-migration-path)
12. [Conclusions](#12-conclusions)

---

## 1. The Fundamental Tension

GSD's design philosophy is radical simplicity: markdown files on disk, no server, no database, no API. This works because GSD targets a single developer on a single machine running a single orchestrator. The filesystem IS the database, git IS the replication layer, and the orchestrator's in-memory context IS the coordination mechanism.

CockroachDB represents the opposite end of the spectrum: distributed, strongly consistent, schema-enforced, multi-node, multi-writer. Introducing it means fundamentally changing what GSD _is_ -- from a local-first tool to a networked service.

The question is not "should GSD use CockroachDB?" (it shouldn't, directly). The question is: **what capabilities emerge when GSD's data model is projected into CockroachDB through a mediating API server (hive-server)?** And are those capabilities worth the complexity?

The key insight: GSD stays local-first. Agents still read/write `.planning/` files. But hive-server becomes a **sync target** -- a place where GSD state is mirrored, queryable, and available for cross-agent coordination that the filesystem cannot provide.

---

## 2. Schema Design: GSD's Data Model in SQL

GSD's `.planning/` directory contains six conceptual entities spread across markdown files: projects, requirements, phases, plans, tasks, and agents. Below is a normalized schema that captures these relationships.

### Core Tables

```sql
-- 001_gsd_schema.sql

-- Projects: one per GSD-managed codebase
CREATE TABLE gsd_projects (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL,
    name            TEXT        NOT NULL,
    slug            TEXT        NOT NULL,          -- e.g., "hive-server"
    vision          TEXT        NOT NULL DEFAULT '',
    constraints     JSONB       NOT NULL DEFAULT '[]'::JSONB,
    key_decisions   JSONB       NOT NULL DEFAULT '[]'::JSONB,
    config          JSONB       NOT NULL DEFAULT '{}'::JSONB,
    repo_url        TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (tenant_id, slug)
);

-- Requirements: tracked items with category-prefixed IDs
CREATE TABLE gsd_requirements (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID        NOT NULL REFERENCES gsd_projects(id) ON DELETE CASCADE,
    requirement_id  TEXT        NOT NULL,          -- e.g., "AUTH-01", "API-03"
    category        TEXT        NOT NULL,          -- e.g., "AUTH", "API", "UI"
    title           TEXT        NOT NULL,
    description     TEXT        NOT NULL DEFAULT '',
    scope           TEXT        NOT NULL DEFAULT 'v1',
    completed       BOOL        NOT NULL DEFAULT false,
    deferred_reason TEXT        NOT NULL DEFAULT '',
    tags            JSONB       NOT NULL DEFAULT '[]'::JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (project_id, requirement_id),
    CHECK (scope IN ('v1', 'v2', 'out-of-scope'))
);

-- Milestones: top-level grouping above phases
CREATE TABLE gsd_milestones (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID        NOT NULL REFERENCES gsd_projects(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    ordinal         INT4        NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'active',
    completed_at    TIMESTAMPTZ,
    git_tag         TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (project_id, ordinal),
    CHECK (status IN ('active', 'complete', 'archived'))
);

-- Phases: numbered stages within a milestone
CREATE TABLE gsd_phases (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    milestone_id    UUID        NOT NULL REFERENCES gsd_milestones(id) ON DELETE CASCADE,
    project_id      UUID        NOT NULL REFERENCES gsd_projects(id) ON DELETE CASCADE,
    ordinal         DECIMAL     NOT NULL,          -- supports 2.1, 2.2 for inserted phases
    name            TEXT        NOT NULL,
    slug            TEXT        NOT NULL,          -- e.g., "02-authentication"
    goal            TEXT        NOT NULL DEFAULT '',
    status          TEXT        NOT NULL DEFAULT 'not_started',
    dependencies    JSONB       NOT NULL DEFAULT '[]'::JSONB,
    success_criteria JSONB      NOT NULL DEFAULT '[]'::JSONB,
    context_notes   TEXT        NOT NULL DEFAULT '',  -- user preferences (CONTEXT.md content)
    research_notes  TEXT        NOT NULL DEFAULT '',  -- RESEARCH.md content
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (milestone_id, ordinal),
    CHECK (status IN ('not_started', 'researching', 'planning', 'ready_to_execute',
                       'in_progress', 'verifying', 'complete', 'deferred'))
);

-- Plans: atomic execution units within a phase (2-3 tasks each)
CREATE TABLE gsd_plans (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    phase_id        UUID        NOT NULL REFERENCES gsd_phases(id) ON DELETE CASCADE,
    project_id      UUID        NOT NULL REFERENCES gsd_projects(id) ON DELETE CASCADE,
    ordinal         INT4        NOT NULL,          -- plan number within phase
    name            TEXT        NOT NULL,
    wave            INT4        NOT NULL DEFAULT 1, -- dependency wave (1 = independent)
    status          TEXT        NOT NULL DEFAULT 'pending',
    assigned_agent  TEXT        NOT NULL DEFAULT '',
    model_profile   TEXT        NOT NULL DEFAULT 'balanced',
    dependencies    JSONB       NOT NULL DEFAULT '[]'::JSONB,  -- plan IDs this depends on
    summary         TEXT        NOT NULL DEFAULT '',           -- execution outcome
    duration_ms     INT8,
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (phase_id, ordinal),
    CHECK (status IN ('pending', 'queued', 'in_progress', 'complete',
                       'failed', 'blocked', 'deferred')),
    CHECK (model_profile IN ('quality', 'balanced', 'budget'))
);

-- Tasks: individual work items within a plan (the XML <task> blocks)
CREATE TABLE gsd_tasks (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id         UUID        NOT NULL REFERENCES gsd_plans(id) ON DELETE CASCADE,
    project_id      UUID        NOT NULL REFERENCES gsd_projects(id) ON DELETE CASCADE,
    ordinal         INT4        NOT NULL,
    name            TEXT        NOT NULL,
    task_type       TEXT        NOT NULL DEFAULT 'auto',
    files           JSONB       NOT NULL DEFAULT '[]'::JSONB,   -- target file paths
    action          TEXT        NOT NULL DEFAULT '',             -- implementation instructions
    verify_command  TEXT        NOT NULL DEFAULT '',             -- automated verification
    done_criteria   TEXT        NOT NULL DEFAULT '',             -- acceptance criteria
    status          TEXT        NOT NULL DEFAULT 'pending',
    git_commit      TEXT        NOT NULL DEFAULT '',             -- commit SHA on completion
    error_output    TEXT        NOT NULL DEFAULT '',
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (plan_id, ordinal),
    CHECK (status IN ('pending', 'in_progress', 'complete', 'failed', 'skipped')),
    CHECK (task_type IN ('auto', 'checkpoint', 'human_verify', 'decision'))
);

-- Phase-to-requirement traceability (many-to-many)
CREATE TABLE gsd_phase_requirements (
    phase_id        UUID        NOT NULL REFERENCES gsd_phases(id) ON DELETE CASCADE,
    requirement_id  UUID        NOT NULL REFERENCES gsd_requirements(id) ON DELETE CASCADE,
    PRIMARY KEY (phase_id, requirement_id)
);

-- Verification results
CREATE TABLE gsd_verifications (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    phase_id        UUID        NOT NULL REFERENCES gsd_phases(id) ON DELETE CASCADE,
    project_id      UUID        NOT NULL REFERENCES gsd_projects(id) ON DELETE CASCADE,
    verifier_agent  TEXT        NOT NULL DEFAULT '',
    passed          BOOL        NOT NULL,
    diagnostics     JSONB       NOT NULL DEFAULT '{}'::JSONB,
    fix_plan        TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Indexes for common query patterns
CREATE INDEX idx_requirements_project   ON gsd_requirements(project_id);
CREATE INDEX idx_requirements_category  ON gsd_requirements(project_id, category);
CREATE INDEX idx_phases_milestone       ON gsd_phases(milestone_id);
CREATE INDEX idx_phases_project_status  ON gsd_phases(project_id, status);
CREATE INDEX idx_plans_phase            ON gsd_plans(phase_id);
CREATE INDEX idx_plans_wave             ON gsd_plans(phase_id, wave);
CREATE INDEX idx_plans_status           ON gsd_plans(status);
CREATE INDEX idx_plans_agent            ON gsd_plans(assigned_agent);
CREATE INDEX idx_tasks_plan             ON gsd_tasks(plan_id);
CREATE INDEX idx_tasks_status           ON gsd_tasks(project_id, status);
CREATE INDEX idx_verifications_phase    ON gsd_verifications(phase_id);
```

### STATE.md as a Computed View

GSD's `STATE.md` is a derived summary. In CockroachDB, it becomes a view or a computed query rather than a stored artifact:

```sql
CREATE VIEW gsd_project_state AS
SELECT
    p.id AS project_id,
    p.name AS project_name,
    m.ordinal AS milestone_number,
    m.name AS milestone_name,
    ph.ordinal AS phase_number,
    (SELECT COUNT(*) FROM gsd_phases WHERE milestone_id = m.id) AS total_phases,
    ph.name AS phase_name,
    ph.status AS phase_status,
    (SELECT COUNT(*) FROM gsd_plans WHERE phase_id = ph.id AND status = 'complete')
        AS plans_completed,
    (SELECT COUNT(*) FROM gsd_plans WHERE phase_id = ph.id)
        AS plans_total,
    (SELECT COUNT(*) FROM gsd_tasks t
     JOIN gsd_plans pl ON t.plan_id = pl.id
     WHERE pl.phase_id = ph.id AND t.status = 'complete')
        AS tasks_completed,
    (SELECT COUNT(*) FROM gsd_tasks t
     JOIN gsd_plans pl ON t.plan_id = pl.id
     WHERE pl.phase_id = ph.id)
        AS tasks_total,
    p.updated_at AS last_activity
FROM gsd_projects p
JOIN gsd_milestones m ON m.project_id = p.id AND m.status = 'active'
JOIN gsd_phases ph ON ph.milestone_id = m.id
    AND ph.status NOT IN ('complete', 'deferred')
ORDER BY ph.ordinal ASC
LIMIT 1;
```

This is strictly better than a 100-line markdown file: it cannot go stale, cannot be corrupted by a bad edit, and computes progress from ground truth rather than duplicating it.

---

## 3. JSONB for Semi-Structured Fields

Several GSD data structures are semi-structured -- they have a general shape but vary across projects and evolve over time. JSONB columns handle this without schema migrations.

### Where JSONB is appropriate

| Column             | Table               | Content                                                 | Why JSONB                                               |
| ------------------ | ------------------- | ------------------------------------------------------- | ------------------------------------------------------- |
| `config`           | `gsd_projects`      | GSD's `config.json` (mode, granularity, workflow flags) | Configuration varies per project and evolves frequently |
| `key_decisions`    | `gsd_projects`      | Accumulated architectural decisions                     | Append-only list of varying structure                   |
| `constraints`      | `gsd_projects`      | Project constraints from PROJECT.md                     | Free-form list                                          |
| `tags`             | `gsd_requirements`  | Requirement labels and categories                       | Variable cardinality                                    |
| `dependencies`     | `gsd_phases`        | Phase dependency references                             | Array of phase ordinals or IDs                          |
| `success_criteria` | `gsd_phases`        | Observable behaviors to verify                          | Array of strings                                        |
| `files`            | `gsd_tasks`         | Target file paths                                       | Variable-length array                                   |
| `diagnostics`      | `gsd_verifications` | Verification test results                               | Deeply nested, schema varies                            |

### JSONB query examples

```sql
-- Find all projects using "yolo" mode
SELECT * FROM gsd_projects
WHERE config->>'mode' = 'yolo';

-- Find projects with parallel execution enabled
SELECT * FROM gsd_projects
WHERE config->'concurrency'->>'parallel_execution' = 'true';

-- Find requirements tagged with a specific label
CREATE INVERTED INDEX idx_req_tags ON gsd_requirements(tags);

SELECT * FROM gsd_requirements
WHERE tags @> '["security"]'::JSONB;

-- Find tasks touching a specific file
CREATE INVERTED INDEX idx_task_files ON gsd_tasks(files);

SELECT t.*, pl.name AS plan_name, ph.name AS phase_name
FROM gsd_tasks t
JOIN gsd_plans pl ON t.plan_id = pl.id
JOIN gsd_phases ph ON pl.phase_id = ph.id
WHERE t.files @> '["src/app/api/auth/login/route.ts"]'::JSONB;

-- Aggregate decisions across all projects for a tenant
SELECT
    p.name,
    jsonb_array_elements(p.key_decisions)->>'decision' AS decision,
    jsonb_array_elements(p.key_decisions)->>'rationale' AS rationale,
    jsonb_array_elements(p.key_decisions)->>'date' AS decided_on
FROM gsd_projects p
WHERE p.tenant_id = $1;
```

### Where JSONB is NOT appropriate

Status fields, ordinals, foreign keys, timestamps, and anything used in WHERE clauses or JOINs should be relational columns. The `status` field on phases, plans, and tasks is the most critical example -- it drives the workflow state machine and must be indexed and constrained. Putting it in JSONB would sacrifice both the CHECK constraint and index efficiency.

---

## 4. Explicit State Machines with CHECK Constraints

GSD's state machine is implicit. The `STATUS` field in `STATE.md` is a free-text string updated by whichever agent last wrote to the file. There is no enforcement of valid transitions. An agent could write "In progress" when the phase was never planned, or skip verification entirely.

CockroachDB makes this explicit at two levels.

### Level 1: Valid States via CHECK Constraints

The schema above already constrains valid states:

```sql
CHECK (status IN ('not_started', 'researching', 'planning', 'ready_to_execute',
                   'in_progress', 'verifying', 'complete', 'deferred'))
```

Any attempt to set `status = 'Ready to plan'` (GSD's markdown spelling) or `status = 'borked'` will be rejected at the database level.

### Level 2: Valid Transitions via Application Logic

CockroachDB does not have native trigger-based state machine enforcement (triggers are limited), but the application layer in hive-server can enforce transitions:

```go
// Valid state transitions for phases
var validPhaseTransitions = map[string][]string{
    "not_started":       {"researching", "planning", "deferred"},
    "researching":       {"planning"},
    "planning":          {"ready_to_execute", "researching"},  // can loop back
    "ready_to_execute":  {"in_progress"},
    "in_progress":       {"verifying", "complete"},  // complete if no verifier
    "verifying":         {"complete", "in_progress"},  // fail -> back to in_progress
    "complete":          {},  // terminal
    "deferred":          {"not_started"},  // can be un-deferred
}

func (s *Store) UpdatePhaseStatus(ctx context.Context, phaseID uuid.UUID, newStatus string) error {
    return crdbpgx.ExecuteTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
        var currentStatus string
        err := tx.QueryRow(ctx,
            `SELECT status FROM gsd_phases WHERE id = $1 FOR UPDATE`, phaseID,
        ).Scan(&currentStatus)
        if err != nil {
            return fmt.Errorf("get phase status: %w", err)
        }

        allowed := validPhaseTransitions[currentStatus]
        valid := false
        for _, s := range allowed {
            if s == newStatus {
                valid = true
                break
            }
        }
        if !valid {
            return fmt.Errorf("invalid transition: %s -> %s", currentStatus, newStatus)
        }

        _, err = tx.Exec(ctx,
            `UPDATE gsd_phases SET status = $1, updated_at = now() WHERE id = $2`,
            newStatus, phaseID,
        )
        return err
    })
}
```

The `SELECT ... FOR UPDATE` ensures that concurrent agents cannot race on the same phase's status. This is impossible with GSD's filesystem-based state -- two agents writing to `STATE.md` simultaneously will produce a corrupted file.

### Level 3: Cascading Status Updates

When a task completes, the plan's status may need updating; when all plans complete, the phase status updates. This cascading logic is currently implicit in GSD's orchestrator context. In CockroachDB, it becomes an explicit transaction:

```sql
-- When task completes, check if plan is done
WITH task_update AS (
    UPDATE gsd_tasks
    SET status = 'complete', completed_at = now(), git_commit = $2
    WHERE id = $1
    RETURNING plan_id
),
plan_check AS (
    SELECT plan_id,
           COUNT(*) FILTER (WHERE status != 'complete') AS remaining
    FROM gsd_tasks
    WHERE plan_id = (SELECT plan_id FROM task_update)
    GROUP BY plan_id
)
UPDATE gsd_plans
SET status = CASE
    WHEN (SELECT remaining FROM plan_check) = 0 THEN 'complete'
    ELSE status
    END,
    completed_at = CASE
    WHEN (SELECT remaining FROM plan_check) = 0 THEN now()
    ELSE completed_at
    END
WHERE id = (SELECT plan_id FROM task_update);
```

---

## 5. Event Sourcing for State Transitions

GSD's workflow is inherently event-driven: phases start, plans are created, tasks execute, verifications pass or fail. Currently, the only audit trail is git log (if `commit_docs: true`). CockroachDB enables a proper event sourcing pattern.

### Event Log Table

```sql
CREATE TABLE gsd_events (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID        NOT NULL REFERENCES gsd_projects(id) ON DELETE CASCADE,
    entity_type     TEXT        NOT NULL,  -- 'project', 'phase', 'plan', 'task', 'verification'
    entity_id       UUID        NOT NULL,
    event_type      TEXT        NOT NULL,
    agent_id        TEXT        NOT NULL DEFAULT '',
    previous_state  JSONB,                 -- snapshot of changed fields before
    new_state       JSONB,                 -- snapshot of changed fields after
    metadata        JSONB       NOT NULL DEFAULT '{}'::JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (entity_type IN ('project', 'milestone', 'phase', 'plan', 'task', 'verification')),
    CHECK (event_type IN (
        'created', 'status_changed', 'assigned', 'unassigned',
        'started', 'completed', 'failed', 'retried',
        'verified', 'verification_failed',
        'deferred', 'resumed', 'deleted',
        'config_changed', 'requirement_added', 'requirement_completed'
    ))
);

CREATE INDEX idx_events_project     ON gsd_events(project_id, created_at DESC);
CREATE INDEX idx_events_entity      ON gsd_events(entity_type, entity_id, created_at DESC);
CREATE INDEX idx_events_type        ON gsd_events(event_type, created_at DESC);
CREATE INDEX idx_events_agent       ON gsd_events(agent_id, created_at DESC);
```

### Recording Events in Transactions

Every state mutation emits an event within the same transaction:

```go
func (s *Store) CompleteTask(ctx context.Context, taskID uuid.UUID, gitCommit string) error {
    return crdbpgx.ExecuteTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
        var task Task
        err := tx.QueryRow(ctx,
            `UPDATE gsd_tasks
             SET status = 'complete', completed_at = now(), git_commit = $2
             WHERE id = $1
             RETURNING id, plan_id, project_id, name, status`,
            taskID, gitCommit,
        ).Scan(&task.ID, &task.PlanID, &task.ProjectID, &task.Name, &task.Status)
        if err != nil {
            return fmt.Errorf("complete task: %w", err)
        }

        // Emit event
        _, err = tx.Exec(ctx,
            `INSERT INTO gsd_events (project_id, entity_type, entity_id, event_type, metadata)
             VALUES ($1, 'task', $2, 'completed', $3)`,
            task.ProjectID, task.ID,
            map[string]any{"git_commit": gitCommit, "task_name": task.Name},
        )
        return err
    })
}
```

### Querying the Event Log

Event sourcing enables queries GSD currently cannot answer:

```sql
-- Timeline of a phase's execution (full audit trail)
SELECT
    e.event_type,
    e.entity_type,
    e.agent_id,
    e.metadata,
    e.created_at
FROM gsd_events e
WHERE e.entity_id = $1
   OR e.entity_id IN (
       SELECT id FROM gsd_plans WHERE phase_id = $1
       UNION
       SELECT t.id FROM gsd_tasks t
       JOIN gsd_plans p ON t.plan_id = p.id
       WHERE p.phase_id = $1
   )
ORDER BY e.created_at ASC;

-- Which agent completed the most tasks this week?
SELECT
    agent_id,
    COUNT(*) AS tasks_completed
FROM gsd_events
WHERE event_type = 'completed'
  AND entity_type = 'task'
  AND created_at > now() - INTERVAL '7 days'
GROUP BY agent_id
ORDER BY tasks_completed DESC;

-- Average time from plan start to completion
SELECT
    AVG(EXTRACT(EPOCH FROM (completed.created_at - started.created_at))) AS avg_duration_seconds
FROM gsd_events started
JOIN gsd_events completed ON started.entity_id = completed.entity_id
WHERE started.event_type = 'started' AND started.entity_type = 'plan'
  AND completed.event_type = 'completed' AND completed.entity_type = 'plan';

-- Failure rate by phase (how often do tasks fail before succeeding?)
SELECT
    ph.name AS phase_name,
    COUNT(*) FILTER (WHERE e.event_type = 'failed') AS failures,
    COUNT(*) FILTER (WHERE e.event_type = 'completed') AS completions,
    ROUND(
        COUNT(*) FILTER (WHERE e.event_type = 'failed')::DECIMAL /
        NULLIF(COUNT(*) FILTER (WHERE e.event_type IN ('failed', 'completed')), 0)
        * 100, 1
    ) AS failure_rate_pct
FROM gsd_events e
JOIN gsd_tasks t ON e.entity_id = t.id
JOIN gsd_plans pl ON t.plan_id = pl.id
JOIN gsd_phases ph ON pl.phase_id = ph.id
WHERE e.entity_type = 'task'
GROUP BY ph.name;
```

### CDC for Real-Time Event Streaming

CockroachDB's changefeed capability means event consumers do not need to poll:

```sql
-- Stream task completion events to a webhook
CREATE CHANGEFEED FOR TABLE gsd_events
    WITH webhook = 'https://hive-server/hooks/gsd-events',
         updated, resolved = '10s',
         format = json;
```

This could trigger downstream actions: update a dashboard, notify a Slack channel, or signal the next wave's agents to start.

---

## 6. Multi-Agent Coordination via Distributed Transactions

This is where CockroachDB provides capabilities GSD fundamentally lacks. GSD's current concurrency model is "the orchestrator serializes everything." Multiple agents run in parallel, but they coordinate through the orchestrator's context window, not through shared state. Two independent GSD sessions cannot safely work on the same project.

### Agent Registration and Heartbeat

```sql
-- Agents working on GSD projects
CREATE TABLE gsd_agents (
    id              TEXT        NOT NULL PRIMARY KEY,
    name            TEXT        NOT NULL DEFAULT '',
    status          TEXT        NOT NULL DEFAULT 'idle',
    capabilities    JSONB       NOT NULL DEFAULT '[]'::JSONB,
    current_task    UUID        REFERENCES gsd_tasks(id),
    current_project UUID        REFERENCES gsd_projects(id),
    last_heartbeat  TIMESTAMPTZ NOT NULL DEFAULT now(),
    registered_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (status IN ('idle', 'working', 'offline'))
);
```

### Wave-Based Task Assignment with Locking

GSD's wave execution model maps naturally to a work-claiming pattern:

```go
// ClaimNextTask atomically assigns the next available task from a wave to an agent.
// Returns nil if no tasks are available.
func (s *Store) ClaimNextTask(ctx context.Context, agentID string, phaseID uuid.UUID) (*Task, error) {
    var task Task

    err := crdbpgx.ExecuteTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
        // Find the current active wave (lowest wave with pending tasks)
        var activeWave int
        err := tx.QueryRow(ctx, `
            SELECT MIN(p.wave)
            FROM gsd_plans p
            WHERE p.phase_id = $1
              AND p.status = 'pending'
              AND NOT EXISTS (
                  -- All plans in lower waves must be complete
                  SELECT 1 FROM gsd_plans p2
                  WHERE p2.phase_id = $1
                    AND p2.wave < p.wave
                    AND p2.status NOT IN ('complete', 'deferred')
              )
        `, phaseID).Scan(&activeWave)
        if err != nil {
            return fmt.Errorf("find active wave: %w", err)
        }

        // Claim a pending plan in the active wave
        err = tx.QueryRow(ctx, `
            UPDATE gsd_plans
            SET status = 'in_progress',
                assigned_agent = $1,
                started_at = now(),
                updated_at = now()
            WHERE id = (
                SELECT id FROM gsd_plans
                WHERE phase_id = $2
                  AND wave = $3
                  AND status = 'pending'
                ORDER BY ordinal ASC
                LIMIT 1
                FOR UPDATE SKIP LOCKED
            )
            RETURNING id
        `, agentID, phaseID, activeWave).Scan(&task.PlanID)
        if err != nil {
            return fmt.Errorf("claim plan: %w", err)
        }

        // Get the first pending task in the claimed plan
        err = tx.QueryRow(ctx, `
            UPDATE gsd_tasks
            SET status = 'in_progress', started_at = now(), updated_at = now()
            WHERE id = (
                SELECT id FROM gsd_tasks
                WHERE plan_id = $1 AND status = 'pending'
                ORDER BY ordinal ASC
                LIMIT 1
                FOR UPDATE
            )
            RETURNING id, name, task_type, files, action, verify_command, done_criteria
        `, task.PlanID).Scan(
            &task.ID, &task.Name, &task.TaskType,
            &task.Files, &task.Action, &task.VerifyCommand, &task.DoneCriteria,
        )
        return err
    })

    if err != nil {
        return nil, err
    }
    return &task, nil
}
```

The `FOR UPDATE SKIP LOCKED` pattern is critical: multiple agents can concurrently claim tasks without blocking each other or double-claiming. This is a primitive GSD cannot express with filesystem locking.

### Cross-Agent Visibility

With state in CockroachDB, any agent can see what every other agent is doing:

```sql
-- What is everyone working on right now?
SELECT
    a.name AS agent,
    p.name AS project,
    ph.name AS phase,
    pl.name AS plan,
    t.name AS task,
    t.status AS task_status,
    pl.started_at AS working_since
FROM gsd_agents a
JOIN gsd_projects p ON a.current_project = p.id
JOIN gsd_plans pl ON pl.assigned_agent = a.id AND pl.status = 'in_progress'
JOIN gsd_phases ph ON pl.phase_id = ph.id
LEFT JOIN gsd_tasks t ON t.plan_id = pl.id AND t.status = 'in_progress'
WHERE a.status = 'working';

-- Detect stalled agents (no heartbeat in 5 minutes)
SELECT id, name, current_task, last_heartbeat
FROM gsd_agents
WHERE status = 'working'
  AND last_heartbeat < now() - INTERVAL '5 minutes';
```

### Distributed Orchestration

The most significant capability: multiple orchestrators can coordinate across machines. Two GSD instances on different machines (or different developers' laptops) can work on the same project with CockroachDB mediating task assignment and status:

```
Developer A (laptop)              hive-server (CockroachDB)              Developer B (laptop)
     |                                    |                                    |
     |--- /gsd:execute-phase 3 ---------->|                                    |
     |    ClaimNextTask("agent-A", ph3)   |                                    |
     |<-- task: "Create login endpoint"   |                                    |
     |                                    |<--- /gsd:execute-phase 3 ----------|
     |                                    |     ClaimNextTask("agent-B", ph3)  |
     |                                    |---> task: "Create signup endpoint" |
     |                                    |                                    |
     |--- CompleteTask(task1, sha1) ------>|                                    |
     |                                    |<--- CompleteTask(task2, sha2) -----|
     |                                    |                                    |
     |    [wave 1 complete, start wave 2] |                                    |
```

This is fundamentally impossible with GSD's current filesystem model.

---

## 7. Cross-Project Querying

GSD's `.planning/` directory is per-project. There is no mechanism to ask questions across projects. CockroachDB makes cross-project queries trivial.

### Examples

```sql
-- Which projects have stalled? (no activity in 3 days)
SELECT name, updated_at, slug
FROM gsd_projects
WHERE tenant_id = $1
  AND updated_at < now() - INTERVAL '3 days'
  AND id IN (
      SELECT project_id FROM gsd_phases WHERE status NOT IN ('complete', 'deferred')
  );

-- Requirement coverage across all projects
SELECT
    p.name AS project,
    COUNT(*) AS total_requirements,
    COUNT(*) FILTER (WHERE r.completed) AS completed,
    COUNT(*) FILTER (WHERE r.scope = 'out-of-scope') AS descoped,
    ROUND(
        COUNT(*) FILTER (WHERE r.completed)::DECIMAL /
        NULLIF(COUNT(*) FILTER (WHERE r.scope = 'v1'), 0) * 100, 1
    ) AS completion_pct
FROM gsd_projects p
JOIN gsd_requirements r ON r.project_id = p.id
WHERE p.tenant_id = $1
GROUP BY p.name
ORDER BY completion_pct ASC;

-- Find all tasks that touched a specific file across any project
SELECT
    p.name AS project,
    ph.name AS phase,
    t.name AS task,
    t.status,
    t.git_commit
FROM gsd_tasks t
JOIN gsd_plans pl ON t.plan_id = pl.id
JOIN gsd_phases ph ON pl.phase_id = ph.id
JOIN gsd_projects p ON ph.project_id = p.id
WHERE t.files @> '["internal/store/store.go"]'::JSONB;

-- Velocity comparison across projects (plans completed per day)
SELECT
    p.name,
    COUNT(*) AS plans_completed,
    MIN(pl.completed_at) AS first_completion,
    MAX(pl.completed_at) AS last_completion,
    ROUND(
        COUNT(*)::DECIMAL /
        GREATEST(EXTRACT(DAY FROM MAX(pl.completed_at) - MIN(pl.completed_at)), 1),
        2
    ) AS plans_per_day
FROM gsd_plans pl
JOIN gsd_phases ph ON pl.phase_id = ph.id
JOIN gsd_projects p ON ph.project_id = p.id
WHERE pl.status = 'complete'
  AND p.tenant_id = $1
GROUP BY p.name;
```

None of these queries are possible in GSD today without manually grepping across multiple `.planning/` directories and parsing markdown.

---

## 8. Transaction Retries and GSD Workflows

CockroachDB's SERIALIZABLE isolation means transactions that conflict will fail with `SQLSTATE 40001` and must be retried. This has specific implications for GSD's workflow patterns.

### Where retries will occur

**High contention scenarios:**

- Multiple agents completing tasks in the same plan simultaneously (updating task status, then checking if the plan is complete)
- Wave transition logic (multiple agents finishing wave N tasks, all trying to check if wave N+1 should start)
- Status cascading (task complete -> plan complete -> phase complete)

**Low contention scenarios:**

- Task claiming with `SKIP LOCKED` (contention is handled by the lock, not serialization failure)
- Independent plan execution within the same wave (different rows)
- Event log inserts (append-only, no conflicts)

### Retry-safe design principles for GSD

```go
// CORRECT: idempotent, no side effects inside the transaction
func (s *Store) CompleteTaskAndCascade(ctx context.Context, taskID uuid.UUID, commit string) error {
    return crdbpgx.ExecuteTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
        // 1. Mark task complete
        var planID, projectID uuid.UUID
        err := tx.QueryRow(ctx, `
            UPDATE gsd_tasks SET status = 'complete', completed_at = now(), git_commit = $2
            WHERE id = $1
            RETURNING plan_id, project_id`, taskID, commit,
        ).Scan(&planID, &projectID)
        if err != nil {
            return err
        }

        // 2. Check if all tasks in the plan are done
        var remaining int
        err = tx.QueryRow(ctx, `
            SELECT COUNT(*) FROM gsd_tasks
            WHERE plan_id = $1 AND status NOT IN ('complete', 'skipped')`,
            planID,
        ).Scan(&remaining)
        if err != nil {
            return err
        }

        if remaining == 0 {
            // 3. Mark plan complete
            _, err = tx.Exec(ctx, `
                UPDATE gsd_plans SET status = 'complete', completed_at = now()
                WHERE id = $1`, planID)
            if err != nil {
                return err
            }

            // 4. Emit event (all inside same transaction)
            _, err = tx.Exec(ctx, `
                INSERT INTO gsd_events (project_id, entity_type, entity_id, event_type)
                VALUES ($1, 'plan', $2, 'completed')`, projectID, planID)
        }
        return err
    })
}
```

The key rule: **no side effects inside the transaction closure**. This means:

- Do not spawn sub-agents inside a transaction
- Do not make HTTP calls inside a transaction
- Do not write to the filesystem inside a transaction
- Only perform database reads and writes

The agent spawning and file-system operations happen _after_ the transaction commits successfully.

### Expected retry frequency

For a typical GSD execution (3 agents running in parallel within a wave), contention will be low. The plans touch different rows, and the only shared contention point is the wave-completion check. Retry rates should be well under 1% for this workload pattern. The `crdbpgx.ExecuteTx` wrapper handles retries transparently -- GSD agents will not notice them.

---

## 9. Practical Integration: hive-server as Mediator

GSD agents do not talk to CockroachDB directly. hive-server mediates all access, providing a REST API that maps GSD operations to database transactions.

### API Surface for GSD

```
# Project lifecycle
POST   /api/v1/gsd/projects                  Create project
GET    /api/v1/gsd/projects/:id              Get project state (replaces STATE.md read)
GET    /api/v1/gsd/projects/:id/state        Computed state summary (the STATE.md equivalent)

# Requirements
POST   /api/v1/gsd/projects/:id/requirements       Add requirement
GET    /api/v1/gsd/projects/:id/requirements        List requirements (filterable by scope, category)
PATCH  /api/v1/gsd/requirements/:id                 Update requirement (mark complete, defer, etc.)

# Phases
POST   /api/v1/gsd/projects/:id/phases              Create phase
GET    /api/v1/gsd/projects/:id/phases               List phases
GET    /api/v1/gsd/phases/:id                        Get phase detail
PATCH  /api/v1/gsd/phases/:id/status                 Transition phase status

# Plans
POST   /api/v1/gsd/phases/:id/plans                 Create plan
GET    /api/v1/gsd/phases/:id/plans                  List plans (includes wave info)
POST   /api/v1/gsd/phases/:id/plans/claim            Claim next available plan (agent assignment)
PATCH  /api/v1/gsd/plans/:id/status                  Update plan status

# Tasks
POST   /api/v1/gsd/plans/:id/tasks                  Create task
GET    /api/v1/gsd/plans/:id/tasks                   List tasks
PATCH  /api/v1/gsd/tasks/:id/status                  Update task status (with cascading)
POST   /api/v1/gsd/tasks/:id/complete                Complete task (with git commit SHA)

# Verification
POST   /api/v1/gsd/phases/:id/verify                Record verification result

# Events and analytics
GET    /api/v1/gsd/projects/:id/events               Event log (filterable by entity, type, agent)
GET    /api/v1/gsd/projects/:id/analytics             Velocity, failure rates, agent stats

# Cross-project
GET    /api/v1/gsd/projects                           List all projects (with summary stats)
GET    /api/v1/gsd/search/tasks?file=path             Find tasks by file path across projects
```

### Sync Model: Filesystem to Database

GSD agents continue to work with `.planning/` files locally. A sync layer pushes state to hive-server:

```
┌─────────────────────────────────────────────────────────┐
│ Developer Machine                                       │
│                                                         │
│  ┌──────────┐     ┌──────────────┐     ┌─────────────┐ │
│  │ GSD      │────>│ .planning/   │────>│ gsd-sync    │ │
│  │ Agents   │<────│ files        │<────│ (hook/cmd)  │ │
│  └──────────┘     └──────────────┘     └──────┬──────┘ │
│                                               │        │
└───────────────────────────────────────────────┼────────┘
                                                │ HTTP
                                          ┌─────▼─────┐
                                          │hive-server │
                                          │  (REST)    │
                                          └─────┬─────┘
                                                │ pgx
                                          ┌─────▼─────┐
                                          │CockroachDB│
                                          └───────────┘
```

The sync could work three ways:

**Option A: Post-commit hook.** After GSD commits a task, a git hook parses the committed `.planning/` files and pushes structured data to hive-server. This is the simplest approach: no changes to GSD itself, works with any GSD version.

**Option B: GSD command wrapper.** A custom GSD command (e.g., `/gsd:sync`) reads `.planning/` files and pushes to hive-server. The user runs it explicitly after each phase.

**Option C: Filesystem watcher.** A daemon watches `.planning/` for changes and syncs incrementally. Most seamless but most complex to build.

**Option D: Native GSD integration.** GSD is modified to call hive-server's API directly as part of its workflow. This is the deepest integration but requires forking or contributing to GSD upstream.

For initial implementation, **Option A (post-commit hook)** is the pragmatic choice. It requires zero changes to GSD and captures the ground-truth state (what was committed) rather than intermediate working state.

### Handler Implementation Pattern

```go
// internal/handlers/gsd.go

func (h *Handler) claimPlan(w http.ResponseWriter, r *http.Request) {
    phaseID, err := uuid.Parse(chi.URLParam(r, "phaseID"))
    if err != nil {
        respondError(w, http.StatusBadRequest, "invalid phase ID")
        return
    }

    agentID := r.Context().Value(agentIDKey).(string)

    task, err := h.store.ClaimNextTask(r.Context(), agentID, phaseID)
    if errors.Is(err, pgx.ErrNoRows) {
        respondJSON(w, http.StatusNoContent, nil) // no tasks available
        return
    }
    if err != nil {
        respondError(w, http.StatusInternalServerError, err.Error())
        return
    }

    respondJSON(w, http.StatusOK, task)
}
```

---

## 10. Tradeoff Analysis

### What is Gained

| Capability                   | Current (GSD filesystem)          | With CockroachDB                                                       |
| ---------------------------- | --------------------------------- | ---------------------------------------------------------------------- |
| **Multi-agent coordination** | Single orchestrator, one machine  | Multiple orchestrators, multiple machines, transactional task claiming |
| **State integrity**          | Free-text markdown, no validation | Schema-enforced, CHECK-constrained, FK-validated                       |
| **State machine**            | Implicit string field             | Explicit transitions with application-enforced rules                   |
| **Querying**                 | grep across markdown files        | Full SQL: joins, aggregations, filters, JSONB operators                |
| **Cross-project visibility** | None (isolated `.planning/` dirs) | Cross-project queries, fleet-wide analytics                            |
| **Audit trail**              | Git log (if committed)            | Complete event log with agent attribution                              |
| **Analytics**                | "Last 5 plans" in STATE.md        | Time-series velocity, failure rates, agent stats, duration tracking    |
| **Concurrency safety**       | File locking (unreliable)         | Serializable transactions with `FOR UPDATE SKIP LOCKED`                |
| **Availability**             | Local filesystem                  | Multi-node, survives zone/region failure                               |
| **Session continuity**       | 100-line STATE.md file            | Computed from ground truth, cannot go stale                            |
| **Requirement traceability** | Manual markdown table             | Relational joins with automatic integrity                              |

### What is Lost

| Property                | Current (GSD filesystem)                        | With CockroachDB                                                                        |
| ----------------------- | ----------------------------------------------- | --------------------------------------------------------------------------------------- |
| **Zero infrastructure** | `npx get-shit-done-cc@latest`, done             | Requires hive-server + CockroachDB cluster                                              |
| **Offline capability**  | Fully offline, works on airplanes               | Requires network to reach hive-server (unless Option A sync, which degrades gracefully) |
| **Simplicity**          | Read a markdown file, understand the state      | Read SQL schema, understand the schema + API + sync layer                               |
| **Transparency**        | `cat .planning/STATE.md`                        | `curl hive-server/api/v1/gsd/projects/X/state`                                          |
| **Git-native workflow** | `.planning/` files are versioned alongside code | Database state is separate from code history                                            |
| **Agent agnosticism**   | Works with any LLM that reads files             | Requires hive-server API integration or sync tooling                                    |
| **Hackability**         | Edit markdown with any text editor              | Need API calls or SQL access                                                            |
| **Local-first**         | Everything on your machine                      | State of truth moves to a remote database                                               |

### Complexity Added

| Component                        | Effort                                   | Ongoing Cost                                  |
| -------------------------------- | ---------------------------------------- | --------------------------------------------- |
| CockroachDB schema + migrations  | Medium (one-time)                        | Low (schema evolves slowly)                   |
| hive-server GSD handlers         | Medium (new API surface)                 | Medium (maintain alongside GSD updates)       |
| Sync layer (hook/watcher/native) | Medium-High (parsing markdown)           | High (fragile: GSD format changes break sync) |
| Transaction retry logic          | Low (crdbpgx handles it)                 | Low                                           |
| Event sourcing                   | Low-Medium (append-only table + inserts) | Low                                           |
| CockroachDB operations           | Low (Cloud Basic) to High (self-hosted)  | Ongoing                                       |
| Testing against CockroachDB      | Medium (Docker in CI)                    | Medium                                        |

### The Core Question

The tradeoff reduces to: **is multi-agent/multi-user coordination valuable enough to justify the infrastructure dependency?**

For a solo developer on a single machine: **no**. GSD's filesystem model is simpler, faster, and more transparent. The database adds complexity without proportional benefit.

For a team of developers (or a fleet of autonomous agents) coordinating on the same project: **yes**. The filesystem model fundamentally cannot support concurrent writers, cross-machine coordination, or cross-project analytics. These capabilities require shared, transactional state -- exactly what CockroachDB provides.

The hybrid model (GSD stays local, hive-server syncs state to CockroachDB) preserves GSD's simplicity for the common case while enabling coordination when needed.

---

## 11. Migration Path

### Phase 1: Schema and API (No GSD Changes)

Build the CockroachDB schema and hive-server API endpoints. Test with manually created data. No GSD integration yet.

Deliverables:

- Migration files for all GSD tables
- CRUD handlers for projects, requirements, phases, plans, tasks
- Event logging
- State computation view/endpoint

### Phase 2: Import Tool

Build a CLI tool that parses a `.planning/` directory and imports it into hive-server via the API. This validates the schema against real GSD data and identifies gaps.

```bash
gsd-import --planning-dir .planning/ --hive-url https://hive-server/api/v1
```

This is a one-shot tool, not a sync layer. It answers the question: "does the schema actually capture everything GSD stores?"

### Phase 3: Post-Commit Sync Hook

Build a git post-commit hook that detects changes to `.planning/` files and pushes structured updates to hive-server. This enables ongoing sync without modifying GSD.

### Phase 4: Multi-Agent Coordination

Build the task-claiming and wave-coordination API. Test with multiple GSD instances pointing at the same hive-server. This is where the CockroachDB investment pays off.

### Phase 5: Analytics and Cross-Project Queries

Build the analytics endpoints. This is the "nice to have" layer that becomes compelling once multiple projects are tracked.

---

## 12. Conclusions

CockroachDB addresses GSD's most fundamental architectural limitations: lack of concurrent access, absence of schema enforcement, no cross-project querying, and fragile state management. The distributed transaction model maps naturally to GSD's wave-based execution pattern, enabling genuine multi-agent coordination that filesystem-based state cannot support.

The cost is real: infrastructure dependency, operational complexity, and a sync layer that must parse GSD's markdown format. The hybrid model (local-first GSD with optional CockroachDB sync through hive-server) is the right architecture because it preserves GSD's core strength (simplicity, zero infrastructure) while unlocking coordination capabilities for teams and agent fleets.

The event sourcing pattern is particularly valuable and relatively cheap to implement. Even if multi-agent coordination is not immediately needed, the audit trail, analytics, and cross-project visibility provide immediate value once the data is in CockroachDB.

The recommended approach is incremental: schema first, import tool second, sync hook third, multi-agent coordination fourth. Each phase delivers value independently and validates assumptions before committing to the next level of integration.
