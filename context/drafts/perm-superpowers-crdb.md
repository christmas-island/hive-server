# Permanent State for Superpowers via CockroachDB

**Date:** 2026-03-09
**Scope:** Deep analysis of how CockroachDB could serve as the persistence backbone for Superpowers' agentic skills framework, mediated by hive-server.

---

## Table of Contents

1. [The Core Problem](#1-the-core-problem)
2. [Schema Proposals](#2-schema-proposals)
3. [JSONB for Semi-Structured Skill Metadata](#3-jsonb-for-semi-structured-skill-metadata)
4. [Event Sourcing the Workflow Pipeline](#4-event-sourcing-the-workflow-pipeline)
5. [Multi-Agent Coordination via Distributed Transactions](#5-multi-agent-coordination-via-distributed-transactions)
6. [Tracking the 1% Rule and Anti-Rationalization](#6-tracking-the-1-rule-and-anti-rationalization)
7. [Cross-Session Memory](#7-cross-session-memory)
8. [Transaction Retries and Skill Execution](#8-transaction-retries-and-skill-execution)
9. [Practical Integration: hive-server as Mediator](#9-practical-integration-hive-server-as-mediator)
10. [Tradeoffs](#10-tradeoffs)
11. [Migration Path](#11-migration-path)

---

## 1. The Core Problem

Superpowers is stateless by design. Its architecture is pure markdown and shell scripts -- no database, no session store, no shared state. Every agent session starts from zero. The implications are severe for any organization that wants to use Superpowers beyond single-developer, single-session work:

- **No memory across sessions.** An agent that spent 45 minutes debugging a tricky race condition learns nothing that persists. The next session encountering the same codebase will make the same mistakes.
- **No coordination between agents.** When `dispatching-parallel-agents` fires off three agents to investigate independent failures, those agents are fire-and-forget. They cannot signal each other, cannot share intermediate findings, and cannot avoid duplicating work.
- **No outcome tracking.** There is no record of which skills were invoked, whether they helped, how long the brainstorm-to-verify pipeline took, or whether the 1% threshold rule was followed or rationalized away.
- **No searchable history.** Past plans, designs, and implementations exist only as filesystem artifacts in specific branches. There is no queryable index, no relational model, no way to ask "what plans have we written for authentication features?"

CockroachDB, mediated through hive-server's REST API, can fill every one of these gaps while preserving Superpowers' zero-infrastructure local experience as the default mode.

---

## 2. Schema Proposals

The schema below extends hive-server's existing four tables (`memory`, `tasks`, `task_notes`, `agents`) with five new tables purpose-built for Superpowers integration. All tables use UUID primary keys to avoid hot-spot ranges in CockroachDB's distributed storage.

### 2.1 Agent Sessions

Tracks the lifecycle of each agent session -- from session-start hook through completion or abandonment.

```sql
CREATE TABLE agent_sessions (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id        TEXT        NOT NULL,
    parent_session  UUID                 REFERENCES agent_sessions(id),
    session_type    TEXT        NOT NULL DEFAULT 'primary',
        -- 'primary', 'subagent', 'parallel'
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at        TIMESTAMPTZ,
    exit_reason     TEXT,
        -- 'completed', 'abandoned', 'error', 'compacted', 'timeout'
    context_summary JSONB       NOT NULL DEFAULT '{}'::JSONB,
        -- snapshot of what the agent was working on
    skills_loaded   JSONB       NOT NULL DEFAULT '[]'::JSONB,
        -- which skills were injected at session start
    tenant_id       UUID        NOT NULL,

    INDEX idx_sessions_agent (agent_id),
    INDEX idx_sessions_parent (parent_session),
    INDEX idx_sessions_type (session_type),
    INDEX idx_sessions_started (started_at DESC)
);
```

The `parent_session` foreign key creates a tree structure: a primary session spawns subagent sessions (via `subagent-driven-development`) and parallel sessions (via `dispatching-parallel-agents`). This tree is the coordination backbone that Superpowers currently lacks.

### 2.2 Skill Executions

Records every skill activation -- which skill, when, why, and what happened.

```sql
CREATE TABLE skill_executions (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID        NOT NULL REFERENCES agent_sessions(id),
    skill_name      TEXT        NOT NULL,
        -- e.g., 'systematic-debugging', 'test-driven-development'
    skill_source    TEXT        NOT NULL DEFAULT 'builtin',
        -- 'builtin', 'personal', 'organization'
    trigger_reason  TEXT        NOT NULL DEFAULT '',
        -- why the agent decided to invoke this skill
    activation_confidence FLOAT NOT NULL DEFAULT 1.0,
        -- 0.0 to 1.0: how confident the agent was this skill applied
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at        TIMESTAMPTZ,
    outcome         TEXT,
        -- 'completed', 'skipped', 'abandoned', 'overridden'
    outcome_notes   TEXT        NOT NULL DEFAULT '',
    metadata        JSONB       NOT NULL DEFAULT '{}'::JSONB,
        -- skill-specific structured data (see Section 3)
    tenant_id       UUID        NOT NULL,

    INDEX idx_skill_exec_session (session_id),
    INDEX idx_skill_exec_name (skill_name),
    INDEX idx_skill_exec_started (started_at DESC),
    INVERTED INDEX idx_skill_exec_meta (metadata)
);
```

### 2.3 Workflow Stages (Event Log)

An append-only event log for the brainstorm-plan-implement-review-verify pipeline. Each row is a state transition.

```sql
CREATE TABLE workflow_events (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID        NOT NULL REFERENCES agent_sessions(id),
    skill_exec_id   UUID                 REFERENCES skill_executions(id),
    stage           TEXT        NOT NULL,
        -- 'brainstorm', 'plan', 'implement', 'review', 'verify', 'finish'
    event_type      TEXT        NOT NULL,
        -- 'entered', 'completed', 'failed', 'skipped', 'retried'
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    duration_ms     INT8,
        -- time spent in this stage (filled on exit)
    payload         JSONB       NOT NULL DEFAULT '{}'::JSONB,
        -- stage-specific data: plan content, test results, review findings
    predecessor     UUID                 REFERENCES workflow_events(id),
        -- links to the previous event in the pipeline
    tenant_id       UUID        NOT NULL,

    INDEX idx_wf_session (session_id),
    INDEX idx_wf_stage (stage),
    INDEX idx_wf_occurred (occurred_at DESC),
    INVERTED INDEX idx_wf_payload (payload)
);
```

### 2.4 Outcomes

Structured records of what happened at the end of a workflow -- the deliverable, whether it met the plan, verification evidence.

```sql
CREATE TABLE outcomes (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID        NOT NULL REFERENCES agent_sessions(id),
    plan_reference  TEXT        NOT NULL DEFAULT '',
        -- path to the plan file, e.g., 'docs/plans/2026-03-09-auth-refactor.md'
    result          TEXT        NOT NULL,
        -- 'success', 'partial', 'failure', 'abandoned'
    verification    JSONB       NOT NULL DEFAULT '{}'::JSONB,
        -- {command: "go test ./...", exit_code: 0, output_hash: "abc123",
        --  passed: true, evidence: "all 47 tests passed"}
    tasks_planned   INT4        NOT NULL DEFAULT 0,
    tasks_completed INT4        NOT NULL DEFAULT 0,
    tasks_skipped   INT4        NOT NULL DEFAULT 0,
    commits         JSONB       NOT NULL DEFAULT '[]'::JSONB,
        -- [{hash: "abc123", message: "feat: add auth middleware"}]
    branch          TEXT        NOT NULL DEFAULT '',
    pr_url          TEXT        NOT NULL DEFAULT '',
    completed_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    duration_ms     INT8,
        -- total pipeline duration from brainstorm to finish
    tenant_id       UUID        NOT NULL,

    INDEX idx_outcomes_session (session_id),
    INDEX idx_outcomes_result (result),
    INDEX idx_outcomes_completed (completed_at DESC)
);
```

### 2.5 Skill Effectiveness (Materialized Analytics)

A denormalized table updated by triggers or periodic jobs. Tracks aggregate skill performance.

```sql
CREATE TABLE skill_effectiveness (
    skill_name          TEXT        NOT NULL,
    tenant_id           UUID        NOT NULL,
    period_start        DATE        NOT NULL,
    period_end          DATE        NOT NULL,
    invocation_count    INT4        NOT NULL DEFAULT 0,
    completion_count    INT4        NOT NULL DEFAULT 0,
    skip_count          INT4        NOT NULL DEFAULT 0,
    abandon_count       INT4        NOT NULL DEFAULT 0,
    avg_duration_ms     INT8        NOT NULL DEFAULT 0,
    success_rate        FLOAT       NOT NULL DEFAULT 0.0,
        -- completions that led to successful outcomes / total completions
    avg_confidence      FLOAT       NOT NULL DEFAULT 0.0,
    co_occurring_skills JSONB       NOT NULL DEFAULT '[]'::JSONB,
        -- skills frequently used in the same session

    PRIMARY KEY (skill_name, tenant_id, period_start),
    INDEX idx_effectiveness_tenant (tenant_id, period_start DESC)
);
```

### Entity Relationship Summary

```
agent_sessions
  |-- 1:N --> skill_executions
  |-- 1:N --> workflow_events
  |-- 1:1 --> outcomes
  |-- 1:N --> agent_sessions (parent_session, self-referential for subagents)

skill_executions
  |-- 1:N --> workflow_events (skill_exec_id)

workflow_events
  |-- 1:1 --> workflow_events (predecessor, linked list within a pipeline)

-- Existing hive-server tables remain unchanged:
agents (heartbeat, capabilities)
memory (cross-session key-value store)
tasks (coordination tasks)
task_notes (task discussion)
```

---

## 3. JSONB for Semi-Structured Skill Metadata

### The Problem

Superpowers skills are defined by YAML frontmatter in SKILL.md files:

```yaml
---
name: systematic-debugging
description: Use when debugging any issue - enforces root cause investigation before fix attempts
---
```

The content below the frontmatter is free-form markdown with rules, examples, flowcharts, and checklists. Different skills produce wildly different execution artifacts. The `test-driven-development` skill produces test results. The `brainstorming` skill produces a list of ideas with pros and cons. The `writing-plans` skill produces a structured task breakdown. There is no universal schema for "what a skill execution produced."

### JSONB as the Answer

CockroachDB's JSONB columns handle this naturally. The `metadata` column on `skill_executions` and the `payload` column on `workflow_events` can store skill-specific structured data without schema changes per skill type.

**Example: systematic-debugging execution metadata**

```json
{
  "skill_version": "4.3.1",
  "frontmatter": {
    "name": "systematic-debugging",
    "description": "Use when debugging any issue..."
  },
  "hypothesis_count": 3,
  "hypotheses": [
    {
      "description": "Race condition in connection pool",
      "tested": true,
      "result": "confirmed"
    },
    {
      "description": "Timeout too aggressive",
      "tested": true,
      "result": "ruled_out"
    },
    {
      "description": "Missing mutex on shared map",
      "tested": false,
      "result": "not_needed"
    }
  ],
  "root_cause": "Race condition in connection pool cleanup goroutine",
  "fix_approach": "Added sync.Mutex around pool.connections map access"
}
```

**Example: brainstorming stage payload**

```json
{
  "topic": "Authentication middleware redesign",
  "ideas": [
    {
      "title": "JWT with refresh tokens",
      "pros": ["stateless", "standard"],
      "cons": ["token revocation complexity"]
    },
    {
      "title": "Session cookies + Redis",
      "pros": ["simple revocation"],
      "cons": ["requires Redis", "not API-friendly"]
    },
    {
      "title": "API keys with rate limiting",
      "pros": ["simple", "no session state"],
      "cons": ["no user identity"]
    }
  ],
  "selected": "JWT with refresh tokens",
  "selection_rationale": "Best fit for multi-agent API access pattern"
}
```

### Querying JSONB

CockroachDB supports the full PostgreSQL JSONB operator set, enabling queries that would be impossible against Superpowers' current filesystem artifacts:

```sql
-- Find all debugging sessions where the root cause involved race conditions
SELECT se.id, se.session_id, se.metadata->>'root_cause' AS root_cause
FROM skill_executions se
WHERE se.skill_name = 'systematic-debugging'
  AND se.metadata->>'root_cause' ILIKE '%race condition%'
  AND se.tenant_id = $1
ORDER BY se.started_at DESC;

-- Find brainstorming sessions that considered JWT
SELECT we.session_id, we.payload->'ideas' AS ideas
FROM workflow_events we
WHERE we.stage = 'brainstorm'
  AND we.payload @> '{"ideas": [{"title": "JWT with refresh tokens"}]}'
  AND we.tenant_id = $1;

-- Find all TDD executions where tests failed
SELECT se.id, se.metadata->'test_results'->>'failures' AS failures
FROM skill_executions se
WHERE se.skill_name = 'test-driven-development'
  AND (se.metadata->'test_results'->>'exit_code')::INT != 0
  AND se.tenant_id = $1;
```

### Inverted Indexes on JSONB

The `INVERTED INDEX` on `metadata` and `payload` columns enables efficient containment queries (`@>`) without knowing the exact JSON structure in advance. This is critical because new skills can be added to Superpowers at any time, each with their own metadata shape, and the database does not need schema changes to support querying them.

```sql
-- Inverted index makes this fast even with millions of rows
CREATE INVERTED INDEX idx_skill_exec_meta ON skill_executions(metadata);

-- This query uses the inverted index
SELECT * FROM skill_executions
WHERE metadata @> '{"hypothesis_count": 3}'
  AND tenant_id = $1;
```

### Computed Columns for Hot Paths

For frequently-queried JSON paths, computed columns with standard indexes avoid the overhead of inverted index scans:

```sql
ALTER TABLE skill_executions ADD COLUMN outcome_result TEXT
    AS (metadata->>'outcome_result') STORED;
CREATE INDEX idx_skill_exec_outcome_result ON skill_executions(outcome_result);

-- Now this is a simple index scan, not an inverted index scan
SELECT * FROM skill_executions WHERE outcome_result = 'root_cause_found';
```

---

## 4. Event Sourcing the Workflow Pipeline

### The Pipeline as Events

Superpowers defines a clear pipeline: brainstorm, plan, implement (with TDD, subagents), review, verify, finish. Today this pipeline exists only as a sequence of skill invocations within a single agent context window. There is no durable record that it happened, no way to replay it, no way to audit whether steps were skipped.

Event sourcing changes this. Every state transition becomes an immutable event in the `workflow_events` table. The current state of any workflow is derived by reading the event chain forward from the first event.

### Event Chain Example

A complete workflow produces events like this:

```sql
INSERT INTO workflow_events (session_id, stage, event_type, payload) VALUES
($1, 'brainstorm', 'entered',   '{"topic": "auth middleware redesign"}'),
($1, 'brainstorm', 'completed', '{"ideas_count": 4, "selected": "JWT approach"}'),
($1, 'plan',       'entered',   '{"plan_file": "docs/plans/2026-03-09-auth.md"}'),
($1, 'plan',       'completed', '{"task_count": 7, "estimated_hours": 4}'),
($1, 'implement',  'entered',   '{"batch": 1, "tasks": ["task-1","task-2","task-3"]}'),
($1, 'implement',  'completed', '{"batch": 1, "passed": 3, "failed": 0}'),
($1, 'implement',  'entered',   '{"batch": 2, "tasks": ["task-4","task-5","task-6"]}'),
($1, 'implement',  'completed', '{"batch": 2, "passed": 3, "failed": 0}'),
($1, 'implement',  'entered',   '{"batch": 3, "tasks": ["task-7"]}'),
($1, 'implement',  'completed', '{"batch": 3, "passed": 1, "failed": 0}'),
($1, 'review',     'entered',   '{"reviewer": "code-reviewer-agent"}'),
($1, 'review',     'completed', '{"critical": 0, "important": 2, "suggestion": 5}'),
($1, 'verify',     'entered',   '{"command": "go test ./..."}'),
($1, 'verify',     'completed', '{"exit_code": 0, "tests_passed": 47, "coverage": "82%"}'),
($1, 'finish',     'entered',   '{"action": "create_pr"}'),
($1, 'finish',     'completed', '{"pr_url": "https://github.com/org/repo/pull/42"}');
```

### Querying Pipeline State

```sql
-- Get the current stage of a running workflow
SELECT stage, event_type, occurred_at
FROM workflow_events
WHERE session_id = $1
ORDER BY occurred_at DESC
LIMIT 1;

-- Get average time per stage across all completed workflows
SELECT stage,
       AVG(duration_ms) AS avg_ms,
       COUNT(*) AS total
FROM workflow_events
WHERE event_type = 'completed'
  AND tenant_id = $1
  AND occurred_at > now() - INTERVAL '30 days'
GROUP BY stage
ORDER BY AVG(duration_ms) DESC;

-- Find workflows where brainstorming was skipped (went straight to plan)
SELECT DISTINCT we1.session_id
FROM workflow_events we1
WHERE we1.stage = 'plan'
  AND we1.event_type = 'entered'
  AND we1.tenant_id = $1
  AND NOT EXISTS (
    SELECT 1 FROM workflow_events we2
    WHERE we2.session_id = we1.session_id
      AND we2.stage = 'brainstorm'
  );

-- Find workflows that had to retry the review stage
SELECT session_id, COUNT(*) AS retry_count
FROM workflow_events
WHERE stage = 'review'
  AND event_type = 'retried'
  AND tenant_id = $1
GROUP BY session_id
HAVING COUNT(*) > 1;
```

### Pipeline Duration Tracking

The `predecessor` column in `workflow_events` creates a linked list per pipeline. To compute total pipeline duration:

```sql
-- Total pipeline duration per session
SELECT session_id,
       MIN(occurred_at) AS pipeline_start,
       MAX(occurred_at) AS pipeline_end,
       EXTRACT(EPOCH FROM MAX(occurred_at) - MIN(occurred_at)) * 1000 AS total_ms
FROM workflow_events
WHERE tenant_id = $1
  AND occurred_at > now() - INTERVAL '7 days'
GROUP BY session_id
ORDER BY total_ms DESC;
```

### Why Append-Only Matters

The `workflow_events` table is append-only by convention. Events are never updated or deleted. This gives:

- **Auditability:** Full trace of every pipeline execution, including failures and retries.
- **Replayability:** The event chain can reconstruct the state at any point in time.
- **Debugging:** When a workflow fails at the verify stage, the entire history is available for postmortem analysis.
- **No lost information:** If an agent skips a step, the absence of the event is itself informative (see the "brainstorming skipped" query above).

---

## 5. Multi-Agent Coordination via Distributed Transactions

### The Current Vacuum

Superpowers' `dispatching-parallel-agents` skill fires off multiple agents with no coordination mechanism:

> "Deploy parallel agents only when: 3+ independent failures across different domains. No shared dependencies between investigations. Problems can be understood in isolation. Agents will not interfere with each other."

These constraints exist precisely because there is no shared state. If agents could coordinate, the constraints could be relaxed.

### CockroachDB Enables Coordination

CockroachDB's serializable distributed transactions provide the primitive that Superpowers is missing: concurrent agents can safely read and write shared state without conflicts, and when conflicts occur, the database handles resolution automatically.

**Pattern 1: Claim-based task dispatch**

```sql
-- Parent agent creates tasks for parallel agents
INSERT INTO tasks (id, title, description, status, creator, assignee, tags)
VALUES
    (gen_random_uuid(), 'Investigate auth failure', '...', 'open', 'agent-parent', '', '["parallel-batch-1"]'),
    (gen_random_uuid(), 'Investigate DB timeout',  '...', 'open', 'agent-parent', '', '["parallel-batch-1"]'),
    (gen_random_uuid(), 'Investigate memory leak',  '...', 'open', 'agent-parent', '', '["parallel-batch-1"]');

-- Each parallel agent claims a task atomically
-- CockroachDB's SERIALIZABLE isolation prevents double-claiming
UPDATE tasks
SET status = 'in_progress', assignee = $1, updated_at = now()
WHERE id = (
    SELECT id FROM tasks
    WHERE status = 'open'
      AND tags @> '["parallel-batch-1"]'
    ORDER BY priority DESC
    LIMIT 1
    FOR UPDATE
)
RETURNING *;
```

**Pattern 2: Shared findings board**

Parallel agents can post intermediate findings that other agents read:

```sql
-- Agent posts a finding
INSERT INTO memory (key, value, agent_id, tags, version, created_at, updated_at)
VALUES (
    'finding:parallel-batch-1:auth-investigation',
    'Root cause identified: expired token cache TTL set to 0',
    'agent-alpha',
    '["finding", "parallel-batch-1", "auth"]',
    1, now(), now()
);

-- Other agents query findings from the same batch
SELECT key, value, agent_id
FROM memory
WHERE tags @> '["finding", "parallel-batch-1"]'
ORDER BY created_at;
```

**Pattern 3: Agent session tree with status propagation**

```sql
-- Parent dispatches subagent, records the relationship
INSERT INTO agent_sessions (id, agent_id, parent_session, session_type, tenant_id)
VALUES ($1, 'subagent-auth', $2, 'parallel', $3);

-- Parent polls for subagent completion
SELECT id, agent_id, session_type, exit_reason, ended_at
FROM agent_sessions
WHERE parent_session = $1
  AND ended_at IS NOT NULL;

-- Or: parent waits until all children complete
SELECT COUNT(*) AS pending
FROM agent_sessions
WHERE parent_session = $1
  AND ended_at IS NULL;
```

### Transaction Guarantees

CockroachDB's SERIALIZABLE isolation means:

- Two agents cannot claim the same task (the second `UPDATE` will retry or fail).
- A parent reading subagent status sees a consistent snapshot -- it cannot read a "half-completed" state where one subagent's results are visible but another's are not.
- Findings posted by one agent are immediately visible to others after commit (no eventual consistency lag).

This is a fundamental capability upgrade over Superpowers' current fire-and-forget model. Agents gain the ability to divide work, share discoveries, avoid duplication, and reconvene -- all mediated by transactional state.

---

## 6. Tracking the 1% Rule and Anti-Rationalization

### The Problem

Superpowers' `using-superpowers` meta-skill defines an aggressive activation threshold:

> "If you think there is even a 1% chance a skill might apply, you ABSOLUTELY MUST invoke the skill."

This is currently enforced only by the LLM's interpretation of the skill text. There is no audit trail. If an agent rationalizes skipping the `systematic-debugging` skill because "the bug is obvious," that decision is lost when the session ends. Over time, organizations have no data on whether their agents are actually following the discipline.

### Database-Backed Enforcement

With the `skill_executions` table, every skill invocation (and every decision _not_ to invoke) can be recorded and analyzed.

**Recording invocations:**

```sql
-- Agent invokes a skill
INSERT INTO skill_executions
    (session_id, skill_name, trigger_reason, activation_confidence, tenant_id)
VALUES
    ($1, 'systematic-debugging', 'Test failure in auth_test.go line 42', 0.95, $2);
```

**Recording skips (anti-rationalization detection):**

The hive-server API could accept a "skill considered but not invoked" event. This is the key innovation: tracking what agents _chose not to do_.

```sql
-- Agent considered a skill but decided not to invoke it
INSERT INTO skill_executions
    (session_id, skill_name, trigger_reason, activation_confidence, outcome, outcome_notes, tenant_id)
VALUES
    ($1, 'systematic-debugging', 'Minor lint warning', 0.05, 'skipped',
     'Agent determined this was a style issue, not a bug', $2);
```

**Detecting rationalization patterns:**

```sql
-- Find sessions where debugging skill was skipped but the outcome was failure
SELECT se.session_id, se.outcome_notes, o.result
FROM skill_executions se
JOIN outcomes o ON o.session_id = se.session_id
WHERE se.skill_name = 'systematic-debugging'
  AND se.outcome = 'skipped'
  AND o.result = 'failure'
  AND se.tenant_id = $1
ORDER BY se.started_at DESC;

-- Skills most frequently skipped before failures
SELECT se.skill_name,
       COUNT(*) AS skip_before_failure_count
FROM skill_executions se
JOIN outcomes o ON o.session_id = se.session_id
WHERE se.outcome = 'skipped'
  AND o.result IN ('failure', 'partial')
  AND se.tenant_id = $1
  AND se.started_at > now() - INTERVAL '30 days'
GROUP BY se.skill_name
ORDER BY skip_before_failure_count DESC;

-- Average activation confidence for skills that were invoked vs. skipped
SELECT skill_name,
       outcome,
       AVG(activation_confidence) AS avg_confidence,
       COUNT(*) AS count
FROM skill_executions
WHERE tenant_id = $1
GROUP BY skill_name, outcome
ORDER BY skill_name, outcome;
```

**Enforcing the 1% rule:**

hive-server could expose an endpoint that returns a "compliance score" for a session or agent:

```sql
-- Per-agent 1% rule compliance: ratio of invocations to considerations
SELECT agent_id,
       COUNT(*) FILTER (WHERE outcome != 'skipped') AS invoked,
       COUNT(*) FILTER (WHERE outcome = 'skipped') AS skipped,
       COUNT(*) FILTER (WHERE outcome = 'skipped' AND activation_confidence >= 0.01) AS violated_1pct,
       ROUND(
           COUNT(*) FILTER (WHERE outcome != 'skipped')::FLOAT /
           NULLIF(COUNT(*), 0) * 100, 1
       ) AS invocation_rate_pct
FROM skill_executions se
JOIN agent_sessions s ON s.id = se.session_id
WHERE se.tenant_id = $1
  AND se.started_at > now() - INTERVAL '7 days'
GROUP BY agent_id
ORDER BY invocation_rate_pct ASC;
```

A `violated_1pct` count greater than zero means the agent considered a skill with >= 1% relevance and chose not to invoke it. This is exactly the rationalization behavior that Superpowers' skill text tries to prevent through strong language alone.

---

## 7. Cross-Session Memory

### What Persistence Enables

Today, when a Superpowers session ends, everything learned is lost except filesystem artifacts (plan files, code changes, git history). With CockroachDB, hive-server can maintain:

**7.1 Skill-Outcome Correlations**

```sql
-- What skills are most correlated with successful outcomes in this codebase?
SELECT se.skill_name,
       COUNT(*) AS total_uses,
       COUNT(*) FILTER (WHERE o.result = 'success') AS success_count,
       ROUND(
           COUNT(*) FILTER (WHERE o.result = 'success')::FLOAT /
           NULLIF(COUNT(*), 0) * 100, 1
       ) AS success_rate
FROM skill_executions se
JOIN outcomes o ON o.session_id = se.session_id
WHERE se.tenant_id = $1
  AND se.started_at > now() - INTERVAL '90 days'
GROUP BY se.skill_name
HAVING COUNT(*) >= 5
ORDER BY success_rate DESC;
```

**7.2 Failure Pattern Memory**

```sql
-- Store a failure pattern for future reference
INSERT INTO memory (key, value, agent_id, tags, version, created_at, updated_at)
VALUES (
    'failure-pattern:crdb-connection-pool-exhaustion',
    'CockroachDB connection pool exhaustion under load. Root cause: pgxpool.MaxConns too low (5) for concurrent agent requests. Fix: increase to 25, add MaxConnLifetime=1h. Symptoms: context deadline exceeded errors after ~30 concurrent requests.',
    'agent-debugger-42',
    '["failure-pattern", "cockroachdb", "connection-pool", "performance"]',
    1, now(), now()
);

-- Future session queries for relevant failure patterns before debugging
SELECT key, value, agent_id, created_at
FROM memory
WHERE tags @> '["failure-pattern", "connection-pool"]'
  AND tenant_id = $1
ORDER BY created_at DESC
LIMIT 5;
```

**7.3 Codebase-Specific Knowledge**

```sql
-- Record a codebase-specific insight
INSERT INTO memory (key, value, agent_id, tags, version, created_at, updated_at)
VALUES (
    'codebase:hive-server:testing-pattern',
    'hive-server tests use a real SQLite in-memory database, not mocks. Test setup: store.New(":memory:"). Always call defer s.Close() in test cleanup. The store.DB() method exposes the underlying *sql.DB for test assertions.',
    'agent-implementer-7',
    '["codebase-knowledge", "hive-server", "testing", "patterns"]',
    1, now(), now()
);

-- New session queries codebase knowledge before starting work
SELECT key, value
FROM memory
WHERE tags @> '["codebase-knowledge", "hive-server"]'
ORDER BY updated_at DESC;
```

**7.4 Plan Effectiveness Tracking**

```sql
-- How well do plans predict actual effort?
SELECT o.plan_reference,
       o.tasks_planned,
       o.tasks_completed,
       o.tasks_skipped,
       o.duration_ms,
       o.result
FROM outcomes o
WHERE o.tenant_id = $1
  AND o.plan_reference != ''
ORDER BY o.completed_at DESC
LIMIT 20;

-- Average plan accuracy (tasks completed / tasks planned)
SELECT ROUND(AVG(
    tasks_completed::FLOAT / NULLIF(tasks_planned, 0) * 100
), 1) AS avg_plan_accuracy_pct,
       AVG(duration_ms) / 1000 / 60 AS avg_duration_minutes
FROM outcomes
WHERE tenant_id = $1
  AND tasks_planned > 0
  AND completed_at > now() - INTERVAL '30 days';
```

### Session Bootstrap Query

When a new Superpowers session starts (via the session-start hook), hive-server can provide context that would normally require the agent to rediscover everything:

```sql
-- Fetch the most relevant context for a new session working on a specific area
WITH recent_sessions AS (
    SELECT id, context_summary, skills_loaded, started_at
    FROM agent_sessions
    WHERE tenant_id = $1
      AND ended_at IS NOT NULL
      AND context_summary @> $2  -- e.g., '{"repository": "hive-server"}'
    ORDER BY started_at DESC
    LIMIT 5
),
recent_failures AS (
    SELECT key, value
    FROM memory
    WHERE tags @> '["failure-pattern"]'
      AND tenant_id = $1
    ORDER BY updated_at DESC
    LIMIT 10
),
codebase_knowledge AS (
    SELECT key, value
    FROM memory
    WHERE tags @> $3  -- e.g., '["codebase-knowledge", "hive-server"]'
      AND tenant_id = $1
    ORDER BY updated_at DESC
    LIMIT 10
)
SELECT json_build_object(
    'recent_sessions', (SELECT json_agg(row_to_json(rs)) FROM recent_sessions rs),
    'failure_patterns', (SELECT json_agg(row_to_json(rf)) FROM recent_failures rf),
    'codebase_knowledge', (SELECT json_agg(row_to_json(ck)) FROM codebase_knowledge ck)
) AS session_context;
```

This single query provides the new session with knowledge that previously required separate sessions to discover. The agent starts with awareness of what was tried before, what failed, and what the codebase expects.

---

## 8. Transaction Retries and Skill Execution

### The Tension

CockroachDB requires client-side transaction retries for serialization conflicts (SQLSTATE 40001). The `crdbpgx.ExecuteTx()` wrapper handles this by re-executing the transaction function. This creates a tension with skill execution tracking: if a transaction that records a skill execution is retried, the skill execution is not literally repeated -- only the database write is.

### Safe Pattern: Separate Observation from Recording

The key principle is that skill execution happens in the agent's context window (outside the database), and recording happens in the database. These are inherently separated:

```go
// In hive-server's handler for POST /api/v1/skill-executions
func (h *Handler) RecordSkillExecution(w http.ResponseWriter, r *http.Request) {
    var req SkillExecutionRequest
    // ... parse request ...

    var result *SkillExecution
    err := crdbpgx.ExecuteTx(ctx, h.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
        // This function is idempotent: it inserts a record.
        // If retried, it inserts the same record again (with a new UUID).
        // The client provided the data; the transaction only persists it.
        var exec SkillExecution
        err := tx.QueryRow(ctx, `
            INSERT INTO skill_executions
                (session_id, skill_name, skill_source, trigger_reason,
                 activation_confidence, metadata, tenant_id)
            VALUES ($1, $2, $3, $4, $5, $6, $7)
            RETURNING id, session_id, skill_name, started_at
        `, req.SessionID, req.SkillName, req.SkillSource, req.TriggerReason,
           req.ActivationConfidence, req.Metadata, tenantID,
        ).Scan(&exec.ID, &exec.SessionID, &exec.SkillName, &exec.StartedAt)
        if err != nil {
            return fmt.Errorf("insert skill execution: %w", err)
        }
        result = &exec
        return nil
    })

    if err != nil {
        // handle error
        return
    }
    // return result
}
```

This is safe because:

- The transaction function has no side effects beyond the database write.
- If retried, a new UUID is generated (via `gen_random_uuid()` in the schema default or Go-side), so there is no duplicate key conflict.
- The agent's actual skill execution (in the LLM context) happened once regardless of how many times the database write is attempted.

### Dangerous Pattern: Multi-Step Workflows in a Single Transaction

What would be dangerous is trying to put an entire multi-step workflow transition inside a single transaction:

```go
// DANGEROUS: too much work in one transaction, high retry risk
err := crdbpgx.ExecuteTx(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
    // Record skill completion
    tx.Exec(ctx, "UPDATE skill_executions SET ended_at = now(), outcome = $1 ...", ...)
    // Record workflow event
    tx.Exec(ctx, "INSERT INTO workflow_events (...) VALUES (...)", ...)
    // Update outcome
    tx.Exec(ctx, "UPDATE outcomes SET tasks_completed = tasks_completed + 1 ...", ...)
    // Update effectiveness stats
    tx.Exec(ctx, "UPDATE skill_effectiveness SET invocation_count = invocation_count + 1 ...", ...)
    return nil
})
```

This touches four tables and multiple rows, increasing the contention window. Better to split into smaller transactions:

```go
// BETTER: separate transactions for independent operations
// 1. Record skill completion
err := crdbpgx.ExecuteTx(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
    _, err := tx.Exec(ctx, "UPDATE skill_executions SET ended_at = now(), outcome = $1 WHERE id = $2", outcome, execID)
    return err
})

// 2. Record workflow event (independent, can retry separately)
err = crdbpgx.ExecuteTx(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
    _, err := tx.Exec(ctx, "INSERT INTO workflow_events (...) VALUES (...)", ...)
    return err
})

// 3. Update effectiveness (can be async / batched)
// ... queue for periodic aggregation job instead of per-request transaction
```

### Idempotency Keys

For operations where the client might retry the HTTP request itself (network timeout, agent retry), hive-server should accept an idempotency key:

```sql
-- Add idempotency_key column
ALTER TABLE skill_executions ADD COLUMN idempotency_key UUID;
CREATE UNIQUE INDEX idx_skill_exec_idempotency ON skill_executions(idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- INSERT ... ON CONFLICT for idempotent upsert
INSERT INTO skill_executions (session_id, skill_name, ..., idempotency_key)
VALUES ($1, $2, ..., $8)
ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL
DO NOTHING
RETURNING *;
```

---

## 9. Practical Integration: hive-server as Mediator

### Architecture

```
[Superpowers Plugin]                [hive-server]                    [CockroachDB]

 session-start hook  --HTTP-->  POST /api/v1/sessions          -->  agent_sessions

 skill invocation    --HTTP-->  POST /api/v1/skill-executions  -->  skill_executions

 stage transition    --HTTP-->  POST /api/v1/workflow-events   -->  workflow_events

 context query       --HTTP-->  GET  /api/v1/session-context   <--  multi-table query

 subagent dispatch   --HTTP-->  POST /api/v1/sessions          -->  agent_sessions
                                     + POST /api/v1/tasks      -->  tasks

 completion          --HTTP-->  POST /api/v1/outcomes          -->  outcomes
                                     + PATCH /api/v1/sessions  -->  agent_sessions

 memory write        --HTTP-->  PUT  /api/v1/memory/:key       -->  memory

 memory query        --HTTP-->  GET  /api/v1/memory?tags=...   <--  memory
```

### New API Endpoints

hive-server needs these additional endpoints beyond its current API:

```
POST   /api/v1/sessions                  Create agent session
GET    /api/v1/sessions/:id              Get session with children
PATCH  /api/v1/sessions/:id              Update session (end, set exit_reason)
GET    /api/v1/sessions/:id/tree         Get full session tree (parent + children)

POST   /api/v1/skill-executions          Record skill activation
PATCH  /api/v1/skill-executions/:id      Update execution (set outcome, ended_at)
GET    /api/v1/skill-executions?skill=X  Query executions by skill, session, etc.

POST   /api/v1/workflow-events           Record pipeline stage transition
GET    /api/v1/workflow-events?session=X Get event chain for a session

POST   /api/v1/outcomes                  Record workflow outcome
GET    /api/v1/outcomes?result=success   Query outcomes

GET    /api/v1/session-context           Bootstrap context for new session
GET    /api/v1/analytics/skills          Skill effectiveness analytics
GET    /api/v1/analytics/pipeline        Pipeline duration analytics
```

### Hook Integration

Superpowers' session-start hook currently runs a shell script that injects skill context. With hive-server integration, it would also:

1. **Register the session:** `POST /api/v1/sessions` with agent ID, session type, parent session (if subagent).
2. **Fetch context:** `GET /api/v1/session-context?repo=hive-server` to load cross-session knowledge.
3. **Inject context:** Append the fetched knowledge to the skill context injected into the agent.

```bash
#!/bin/bash
# hooks/session-start/bootstrap.sh (modified for hive-server integration)

HIVE_URL="${HIVE_URL:-http://localhost:8080}"
AGENT_ID="${CLAUDE_AGENT_ID:-$(uuidgen)}"

# Register session
SESSION=$(curl -s -X POST "$HIVE_URL/api/v1/sessions" \
  -H "Authorization: Bearer $HIVE_TOKEN" \
  -H "X-Agent-ID: $AGENT_ID" \
  -H "Content-Type: application/json" \
  -d "{\"session_type\": \"primary\", \"context_summary\": {\"repository\": \"$(basename $(git rev-parse --show-toplevel))\"}}")

SESSION_ID=$(echo "$SESSION" | jq -r '.id')
export HIVE_SESSION_ID="$SESSION_ID"

# Fetch cross-session context
CONTEXT=$(curl -s "$HIVE_URL/api/v1/session-context?repo=$(basename $(git rev-parse --show-toplevel))" \
  -H "Authorization: Bearer $HIVE_TOKEN" \
  -H "X-Agent-ID: $AGENT_ID")

# Write context to a temp file that gets injected into agent context
echo "$CONTEXT" | jq -r '.codebase_knowledge[] | .value' > /tmp/hive-context.md

# ... existing skill loading logic ...
```

### Skill Execution Reporting

Each time an agent invokes a skill, a lightweight HTTP call records it. This can be done via a Superpowers hook or by modifying `skills-core.js` to report activations:

```javascript
// In skills-core.js, after skill resolution
async function reportSkillActivation(skillName, sessionId, triggerReason) {
  if (!process.env.HIVE_URL) return; // no-op if hive-server not configured

  try {
    await fetch(`${process.env.HIVE_URL}/api/v1/skill-executions`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${process.env.HIVE_TOKEN}`,
        "X-Agent-ID": process.env.CLAUDE_AGENT_ID,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        session_id: process.env.HIVE_SESSION_ID,
        skill_name: skillName,
        trigger_reason: triggerReason,
        activation_confidence: 1.0,
      }),
    });
  } catch (e) {
    // Graceful degradation: hive-server down doesn't break skill loading
  }
}
```

### Graceful Degradation

The integration must preserve Superpowers' zero-infrastructure default. If hive-server is not configured or not reachable:

- Skill loading and execution proceed normally (no dependency on hive-server).
- No cross-session context is injected (same as current behavior).
- No execution tracking occurs (same as current behavior).
- The `HIVE_URL` environment variable acts as the opt-in flag.

This means the entire CockroachDB integration adds capabilities without removing any existing ones. An agent running without hive-server works exactly as Superpowers works today.

---

## 10. Tradeoffs

### What Is Gained

| Capability                           | Impact                                                                                                                                            |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Cross-session memory**             | Agents stop repeating mistakes. Codebase knowledge accumulates over time. New sessions start with context instead of from zero.                   |
| **Multi-agent coordination**         | Parallel agents can claim tasks, share findings, and signal completion. Subagents can report progress to parents.                                 |
| **Pipeline auditability**            | Every brainstorm-to-verify pipeline execution is recorded. Skipped stages are detectable. Duration trends are visible.                            |
| **Skill effectiveness metrics**      | Organizations can measure which skills help, which are skipped, and where rationalization occurs. Data-driven skill improvement becomes possible. |
| **Anti-rationalization enforcement** | The 1% rule can be tracked and violations flagged. Correlation between skill skipping and failure outcomes becomes queryable.                     |
| **Searchable history**               | Past plans, debugging findings, and codebase knowledge are queryable by SQL and JSONB containment. No more grepping through git branches.         |
| **Distributed scalability**          | Multiple hive-server instances can serve agents concurrently across regions. CockroachDB handles the distributed state.                           |

### What Is Lost

| Concern                             | Impact                                                                                                                                                                                                                           |
| ----------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Zero-infrastructure simplicity**  | Not lost -- graceful degradation preserves the current experience when hive-server is not configured. But the _full_ experience now requires a running database.                                                                 |
| **Privacy / air-gap compatibility** | Skill execution data, codebase knowledge, and debugging findings are now stored in a database. Organizations using Superpowers on air-gapped systems cannot use the CockroachDB features without running CockroachDB internally. |
| **Determinism**                     | Cross-session context injection means two sessions with the same prompt may behave differently based on accumulated history. This is a feature, but it makes behavior less predictable.                                          |

### Complexity Added

| Complexity                  | Mitigation                                                                                                                                                                        |
| --------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Database operations**     | CockroachDB Cloud Basic tier for managed hosting. Single-node for local dev. Docker for CI.                                                                                       |
| **Schema migrations**       | goose migrations embedded in hive-server binary. Same pattern as current SQLite `migrate()`.                                                                                      |
| **Transaction retry logic** | `crdbpgx.ExecuteTx()` wrapper handles retries transparently. Application code uses the wrapper consistently.                                                                      |
| **Network dependency**      | Superpowers' session-start hook gains a network call to hive-server. Timeout + graceful degradation prevents blocking.                                                            |
| **Data model evolution**    | JSONB columns provide schema flexibility for skill-specific metadata. New skills do not require schema changes. But JSONB queries are harder to optimize than relational queries. |
| **Multi-tenant isolation**  | `tenant_id` column on all tables. Application-layer enforcement initially, RLS policies later if needed.                                                                          |
| **Monitoring**              | hive-server needs metrics for transaction retry rates, query latencies, and pool utilization. Standard Prometheus/OpenTelemetry integration.                                      |

### The Central Tradeoff

Superpowers' strength is that it is "just markdown files." Adding a database backend fundamentally changes the architecture from stateless-local to stateful-distributed. The question is whether the capabilities gained (memory, coordination, analytics, enforcement) justify the operational cost.

The answer depends on scale:

- **Solo developer, single project:** The database adds cost with marginal benefit. Filesystem artifacts are sufficient.
- **Team of 3-5, multiple projects:** Cross-session memory and skill analytics start paying off. Failure patterns shared across agents save debugging time.
- **Organization with 10+ agents running concurrently:** Multi-agent coordination and distributed state become essential. The fire-and-forget model breaks down when agents step on each other's work.
- **Enterprise with compliance requirements:** Pipeline auditability and anti-rationalization tracking become mandatory, not optional.

The graceful degradation design means the answer does not have to be all-or-nothing. Teams can adopt incrementally: start with memory (the existing hive-server API), add session tracking, then pipeline events, then analytics.

---

## 11. Migration Path

### Phase 1: Schema Extension (hive-server)

Add the five new tables to hive-server's migration system. Implement as a goose migration that runs alongside the existing schema:

```
internal/store/migrations/
    001_initial_schema.sql         -- existing tables (memory, tasks, task_notes, agents)
    002_superpowers_schema.sql     -- new tables (agent_sessions, skill_executions,
                                      workflow_events, outcomes, skill_effectiveness)
```

### Phase 2: API Endpoints (hive-server)

Add handlers for session lifecycle, skill execution recording, and workflow events. These are straightforward CRUD operations on the new tables, following the same chi router + auth middleware pattern as existing endpoints.

### Phase 3: Hook Integration (Superpowers fork or plugin)

Modify the session-start hook to register with hive-server and fetch cross-session context. This could be:

- A fork of Superpowers with hive-server integration.
- A personal skill that wraps the session-start hook.
- A separate Claude Code plugin that augments Superpowers.

### Phase 4: Skill Execution Reporting (Superpowers fork or plugin)

Instrument `skills-core.js` to report skill activations to hive-server. Alternatively, intercept at the hook level using `SessionStart` and `EnterPlanMode` hooks.

### Phase 5: Analytics Dashboard

Build a read-only analytics API that queries `skill_effectiveness`, aggregates `workflow_events`, and surfaces actionable insights:

- Which skills are most effective in this codebase?
- Which agents skip steps most frequently?
- How long does the average pipeline take?
- Where do pipelines most commonly fail?

### Phase 6: Context Injection Loop

Close the feedback loop: new sessions receive context from past sessions, improving first-attempt success rates. Measure whether agents with cross-session context outperform agents without it.

---

## Sources

- [Superpowers Repository](https://github.com/obra/superpowers) -- skill definitions, architecture, hook system
- [CockroachDB Documentation](https://www.cockroachlabs.com/docs/stable/) -- SQL reference, JSONB, transactions, CDC
- [hive-server codebase](./hive-server-current.md) -- current store interface, SQLite schema, API patterns
- [CockroachDB Technology Brief](./cockroachdb.md) -- detailed CockroachDB evaluation for hive-server
- [Superpowers Technology Brief](./superpowers.md) -- detailed Superpowers architecture analysis
