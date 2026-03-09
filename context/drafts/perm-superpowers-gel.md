# Permanent Analysis: Superpowers + Gel DB Integration

## Executive Summary

Superpowers is a structured agentic skills framework with one glaring architectural gap: **zero persistent state**. Every session starts from scratch. Parallel agents cannot coordinate. No skill is ever evaluated for effectiveness. No plan is ever recalled from history. Gel DB -- a graph-relational database with first-class links, path traversal, and schema-enforced types -- is a natural fit for filling this gap. This analysis explores concrete schema designs, query patterns, integration mechanics, and the tradeoffs involved in wiring Superpowers through hive-server to a Gel persistence layer.

---

## 1. The Core Problem: Superpowers Has No Memory

Superpowers operates entirely within ephemeral boundaries:

- **Context window**: Skills are injected per-session and forgotten when the session ends.
- **Filesystem**: Plans are markdown files with no index, no search, no relational structure.
- **Git**: Branch state tracks code changes but not agent reasoning, skill invocations, or task outcomes.

The consequences are severe for any multi-session or multi-agent workflow:

1. An agent cannot ask "has this type of bug been fixed before in this repo?"
2. A parallel agent cannot signal "I found the root cause, stop investigating."
3. No one can answer "which skills are actually helping vs. which are cargo-culted?"
4. Plans are write-once artifacts with no queryable structure after creation.

Gel addresses all four. Its graph-relational model maps directly to the relational structures implicit in Superpowers: skills reference other skills, agents execute tasks within plans, tasks have outcomes that link back to skills, and all of this forms a traversable graph.

---

## 2. Schema Design: Modeling Superpowers in Gel

### 2.1 Core Types

```sdl
module superpowers {

    # -------------------------------------------------------
    # Abstract mixins
    # -------------------------------------------------------

    abstract type HasTimestamps {
        required created_at: datetime {
            default := datetime_current();
        };
        required updated_at: datetime {
            default := datetime_current();
        };
    }

    abstract type HasAgent {
        required agent_id: str;
        agent_name: str;
    }

    # -------------------------------------------------------
    # Skill definitions (the catalog)
    # -------------------------------------------------------

    scalar type SkillType extending enum<
        'discipline',
        'technique',
        'pattern',
        'reference'
    >;

    scalar type SkillSource extending enum<
        'builtin',
        'personal',
        'organization'
    >;

    type Skill extending HasTimestamps {
        required name: str {
            constraint exclusive;
        };
        required description: str;
        required skill_type: SkillType;
        required source: SkillSource;
        content_hash: str;                      # SHA256 of SKILL.md content
        version: str;                           # Tracks skill version across updates

        # Graph relationships
        multi depends_on: Skill;                # Skill dependency graph
        multi triggers: Skill;                  # "This skill activates that skill"
        multi supporting_docs: SkillDocument;

        # Computed analytics
        property invocation_count := count(.<skill[is SkillInvocation]);
        property avg_effectiveness := math::mean(
            .<skill[is SkillInvocation].effectiveness_score
        );

        index on (.name);
    }

    type SkillDocument extending HasTimestamps {
        required filename: str;
        required content_hash: str;
        link parent_skill: Skill;
    }

    # -------------------------------------------------------
    # Workflow pipeline types
    # -------------------------------------------------------

    scalar type PipelineStage extending enum<
        'brainstorm',
        'plan',
        'implement',
        'review',
        'verify',
        'finish'
    >;

    scalar type StageOutcome extending enum<
        'success',
        'failure',
        'partial',
        'skipped',
        'abandoned'
    >;

    scalar type TaskStatus extending enum<
        'pending',
        'in_progress',
        'blocked',
        'done',
        'failed',
        'cancelled'
    >;

    type Workflow extending HasTimestamps, HasAgent {
        required title: str;
        description: str;
        required repo: str;                     # Repository identifier
        branch: str;

        multi stages: WorkflowStage {
            on target delete delete source;     # Cascade delete
        };
        multi plans: Plan;

        # Terminal state
        completed_at: datetime;
        outcome: StageOutcome;

        index on (.repo);
        index on (.agent_id);
    }

    type WorkflowStage extending HasTimestamps, HasAgent {
        required stage: PipelineStage;
        required outcome: StageOutcome;
        started_at: datetime;
        completed_at: datetime;
        duration_ms: int64;

        link workflow: Workflow;

        # What skills were invoked during this stage
        multi skill_invocations: SkillInvocation;

        # What the agent produced at this stage
        multi artifacts: Artifact;

        # Errors, blockers, notes
        multi notes: StageNote;

        index on (.stage);
    }

    type StageNote extending HasTimestamps {
        required content: str;
        required note_type: str;                # 'error', 'blocker', 'observation'
    }

    # -------------------------------------------------------
    # Plans and tasks (structured version of plan markdown)
    # -------------------------------------------------------

    type Plan extending HasTimestamps, HasAgent {
        required title: str;
        required summary: str;
        required file_path: str;                # docs/plans/YYYY-MM-DD-feature.md
        content_hash: str;

        link workflow: Workflow;
        multi tasks: PlanTask {
            on target delete delete source;
        };

        # Analytics
        property task_count := count(.tasks);
        property completed_count := count(
            .tasks filter .status = TaskStatus.done
        );
        property completion_pct := (
            count(.tasks filter .status = TaskStatus.done) /
            math::max({count(.tasks), 1})
        ) * 100;

        index on (.file_path);
    }

    type PlanTask extending HasTimestamps {
        required title: str;
        required sequence: int32;               # Task ordering
        required status: TaskStatus;
        description: str;

        link plan: Plan;
        link assigned_agent: AgentSession;

        # Dependencies between tasks
        multi depends_on: PlanTask;
        multi blocks: PlanTask;

        # Execution tracking
        started_at: datetime;
        completed_at: datetime;
        duration_ms: int64;
        outcome_notes: str;

        index on (.status);
    }

    # -------------------------------------------------------
    # Agent sessions and coordination
    # -------------------------------------------------------

    scalar type AgentRole extending enum<
        'orchestrator',
        'implementer',
        'reviewer',
        'debugger',
        'parallel_investigator'
    >;

    type AgentSession extending HasTimestamps, HasAgent {
        required role: AgentRole;
        required session_id: str {
            constraint exclusive;
        };
        parent_session: AgentSession;           # Subagent relationship

        link workflow: Workflow;
        multi tasks: PlanTask;                  # Tasks assigned to this agent
        multi skill_invocations: SkillInvocation;
        multi dispatched_agents: AgentSession;  # Children

        # Session lifecycle
        started_at: datetime;
        completed_at: datetime;
        exit_reason: str;                       # 'completed', 'failed', 'timeout'

        # Dispatch context (what the parent told this agent to do)
        dispatch_prompt: str;

        index on (.session_id);
        index on (.agent_id);
    }

    # -------------------------------------------------------
    # Skill invocation tracking
    # -------------------------------------------------------

    type SkillInvocation extending HasTimestamps, HasAgent {
        required skill: Skill;
        required invoked_at: datetime;
        link stage: WorkflowStage;
        link session: AgentSession;

        # Was this skill helpful?
        effectiveness_score: float32;           # 0.0 to 1.0
        outcome_notes: str;

        # Trigger analysis: what caused this skill to activate
        trigger_context: str;                   # The condition that matched

        index on (.skill);
    }

    # -------------------------------------------------------
    # Artifacts (things produced by stages)
    # -------------------------------------------------------

    scalar type ArtifactType extending enum<
        'plan_document',
        'design_document',
        'test_file',
        'implementation_file',
        'review_report',
        'commit_reference'
    >;

    type Artifact extending HasTimestamps {
        required artifact_type: ArtifactType;
        required path: str;                     # File path or git ref
        content_hash: str;
        metadata: json;                         # Flexible additional data

        link stage: WorkflowStage;

        index on (.artifact_type);
    }
}
```

### 2.2 Why Graph-Relational Matters Here

The schema above contains relationships that would be painful in a flat relational model:

- **Skill dependency graph**: `Skill.depends_on -> Skill` is a recursive self-link. In SQL, this requires a junction table and recursive CTEs. In Gel, it is a `multi` link traversed with path syntax.
- **Agent hierarchy**: `AgentSession.parent_session` and `AgentSession.dispatched_agents` form a tree. Traversing "all descendants of the orchestrator" is a path expression, not a recursive query.
- **Task dependency DAG**: `PlanTask.depends_on` and `PlanTask.blocks` model the dependency graph within a plan. Finding the critical path or blocked tasks is a traversal, not a join chain.
- **Cross-cutting analytics**: The computed property `Skill.avg_effectiveness` aggregates across all invocations through a backlink (`.<skill[is SkillInvocation]`), requiring zero application-side logic.

---

## 3. Pipeline Stage Tracking in Gel

### 3.1 Recording Pipeline Progression

When Superpowers transitions between stages (brainstorm -> plan -> implement -> review -> verify), hive-server records each transition:

```edgeql
# Start a new workflow stage
INSERT WorkflowStage {
    stage := PipelineStage.brainstorm,
    outcome := StageOutcome.partial,
    started_at := datetime_current(),
    workflow := (
        SELECT Workflow FILTER .id = <uuid>$workflow_id
    ),
    agent_id := <str>$agent_id
};
```

```edgeql
# Complete a stage with outcome
UPDATE WorkflowStage
FILTER .id = <uuid>$stage_id
SET {
    outcome := StageOutcome.success,
    completed_at := datetime_current(),
    duration_ms := <int64>$duration_ms
};
```

### 3.2 Pipeline Analytics

```edgeql
# Average duration per stage across all workflows in a repo
SELECT PipelineStage {
    stage_name := <str>.stage,
    avg_duration := math::mean(
        (SELECT WorkflowStage
         FILTER .stage = PipelineStage AND
                .workflow.repo = <str>$repo
        ).duration_ms
    ),
    success_rate := (
        count(
            (SELECT WorkflowStage
             FILTER .stage = PipelineStage AND
                    .outcome = StageOutcome.success AND
                    .workflow.repo = <str>$repo)
        ) /
        math::max({
            count(
                (SELECT WorkflowStage
                 FILTER .stage = PipelineStage AND
                        .workflow.repo = <str>$repo)
            ),
            1
        })
    )
};
```

```edgeql
# Find workflows that stalled at a particular stage
SELECT Workflow {
    title,
    repo,
    stalled_stage := (
        SELECT .stages {
            stage,
            started_at,
            duration_ms
        }
        FILTER .outcome = StageOutcome.partial
        ORDER BY .started_at DESC
        LIMIT 1
    )
}
FILTER EXISTS (
    .stages FILTER .outcome = StageOutcome.partial
)
AND .completed_at IS NOT SET;
```

### 3.3 Stage Transition Validation

Gel's access policies or application-level checks can enforce valid transitions:

```edgeql
# Before allowing 'implement' stage, verify 'plan' stage completed
SELECT assert(
    EXISTS (
        SELECT WorkflowStage
        FILTER .workflow.id = <uuid>$workflow_id
        AND .stage = PipelineStage.plan
        AND .outcome = StageOutcome.success
    ),
    message := 'Cannot start implementation without a completed plan'
);
```

---

## 4. Skill Dependencies and Path Traversal

### 4.1 Modeling the Skill Graph

Superpowers skills have implicit dependencies that are currently expressed only in natural language ("Invoke the superpowers:brainstorming skill and follow it exactly"). In Gel, these become explicit, queryable links:

```edgeql
# Populate the skill dependency graph
UPDATE Skill FILTER .name = 'executing-plans'
SET {
    depends_on += (SELECT Skill FILTER .name = 'writing-plans'),
    triggers += (
        SELECT Skill FILTER .name IN {
            'subagent-driven-development',
            'dispatching-parallel-agents',
            'test-driven-development'
        }
    )
};
```

### 4.2 Traversal Queries

```edgeql
# Find all skills reachable from 'brainstorming' (2 hops)
SELECT Skill {
    name,
    description,
    downstream := .triggers {
        name,
        further_downstream := .triggers { name }
    }
}
FILTER .name = 'brainstorming';
```

```edgeql
# Find skills with no dependencies (entry points)
SELECT Skill { name, description }
FILTER NOT EXISTS .depends_on;
```

```edgeql
# Find the most commonly co-invoked skill pairs
WITH
    pairs := (
        SELECT SkillInvocation {
            skill_name := .skill.name,
            stage_id := .stage.id
        }
    ),
    co_occurrences := (
        SELECT {
            skill_a := pairs.skill_name,
            skill_b := (
                SELECT SkillInvocation.skill.name
                FILTER SkillInvocation.stage.id = pairs.stage_id
                AND SkillInvocation.skill.name != pairs.skill_name
            )
        }
    )
SELECT co_occurrences {
    skill_a,
    skill_b,
    count := count(co_occurrences)
}
ORDER BY .count DESC
LIMIT 10;
```

### 4.3 Agent Relationship Traversal

```edgeql
# Full agent tree for a workflow
SELECT AgentSession {
    agent_id,
    role,
    session_id,
    dispatch_prompt,
    tasks: { title, status },
    dispatched_agents: {
        agent_id,
        role,
        tasks: { title, status },
        dispatched_agents: {
            agent_id,
            role,
            tasks: { title, status }
        }
    }
}
FILTER .workflow.id = <uuid>$workflow_id
AND NOT EXISTS .parent_session;
```

This query returns the entire agent tree rooted at the orchestrator, three levels deep, with task assignments at each level. In SQL, this would require recursive CTEs with multiple joins. In EdgeQL, it is a nested shape.

---

## 5. Schema System Mapping to Skill Types

Superpowers defines four skill types: discipline, technique, pattern, reference. Each type has different testing criteria and usage patterns. Gel's schema makes these first-class:

```sdl
# The SkillType enum from the schema above
scalar type SkillType extending enum<
    'discipline',
    'technique',
    'pattern',
    'reference'
>;
```

But more importantly, Gel enables **type-specific analytics**:

```edgeql
# Effectiveness by skill type
SELECT SkillType {
    type_name := <str>SkillType,
    skill_count := count(
        (SELECT Skill FILTER .skill_type = SkillType)
    ),
    avg_effectiveness := math::mean(
        (SELECT SkillInvocation
         FILTER .skill.skill_type = SkillType
        ).effectiveness_score
    ),
    invocation_count := count(
        (SELECT SkillInvocation
         FILTER .skill.skill_type = SkillType)
    )
};
```

This answers questions like: "Are discipline skills more effective than technique skills?" or "Which skill type gets invoked most but has the lowest effectiveness?" These questions are currently unanswerable because Superpowers has no data collection at all.

### 5.1 Skill Version Tracking

Skills evolve. The `content_hash` field on `Skill` enables tracking whether a skill update improved or degraded effectiveness:

```edgeql
# Compare effectiveness before and after a skill update
WITH
    old_version := (
        SELECT SkillInvocation
        FILTER .skill.name = <str>$skill_name
        AND .invoked_at < <datetime>$update_timestamp
    ),
    new_version := (
        SELECT SkillInvocation
        FILTER .skill.name = <str>$skill_name
        AND .invoked_at >= <datetime>$update_timestamp
    )
SELECT {
    skill_name := <str>$skill_name,
    old_avg := math::mean(old_version.effectiveness_score),
    new_avg := math::mean(new_version.effectiveness_score),
    old_count := count(old_version),
    new_count := count(new_version)
};
```

---

## 6. Enhancing Subagent Dispatch with Graph-Based Routing

### 6.1 Current Dispatch Model

Today, subagent dispatch is a fire-and-forget protocol:

1. Orchestrator creates a task prompt.
2. Subagent is spawned with that prompt.
3. Subagent works independently.
4. Results are collected when the subagent completes.

There is no routing intelligence, no history-based dispatch, and no inter-agent communication during execution.

### 6.2 Graph-Enhanced Dispatch

With Gel tracking agent sessions and task outcomes, dispatch decisions can be informed by history:

```edgeql
# Find agents that historically succeed at tasks involving a specific skill
SELECT AgentSession {
    agent_id,
    success_rate := (
        count(.tasks FILTER .status = TaskStatus.done) /
        math::max({count(.tasks), 1})
    ),
    avg_duration := math::mean(.tasks.duration_ms),
    skill_proficiency := math::mean(
        .skill_invocations.effectiveness_score
    )
}
FILTER <str>$target_skill IN .skill_invocations.skill.name
ORDER BY .success_rate DESC
LIMIT 5;
```

```edgeql
# Before dispatching parallel agents, check for shared dependencies
# that would violate the "no interference" rule
WITH
    task_a := (SELECT PlanTask FILTER .id = <uuid>$task_a_id),
    task_b := (SELECT PlanTask FILTER .id = <uuid>$task_b_id)
SELECT {
    shared_deps := (
        SELECT task_a.depends_on FILTER .id IN task_b.depends_on.id
    ) { title },
    shared_blocks := (
        SELECT task_a.blocks FILTER .id IN task_b.blocks.id
    ) { title },
    safe_to_parallelize := (
        NOT EXISTS (
            SELECT task_a.depends_on FILTER .id IN task_b.depends_on.id
        )
        AND NOT EXISTS (
            SELECT task_a.blocks FILTER .id IN task_b.blocks.id
        )
    )
};
```

### 6.3 Real-Time Agent Coordination

Gel's PostgreSQL backend supports `LISTEN`/`NOTIFY`. While Gel does not expose this directly in EdgeQL, hive-server could implement a coordination layer:

1. Agent A completes a task and writes the result to Gel.
2. hive-server detects the write and checks if any blocked tasks are now unblocked.
3. hive-server notifies waiting agents via their polling endpoint.

This transforms the fire-and-forget model into a reactive coordination model without requiring Superpowers itself to change.

---

## 7. Addressing the "No Persistent State" Limitation

This is the highest-value integration point. Here is what becomes possible with Gel as a persistence layer behind hive-server:

### 7.1 Cross-Session Memory

```edgeql
# Recall prior plans for the same repo
SELECT Plan {
    title,
    summary,
    file_path,
    created_at,
    completion_pct,
    tasks: {
        title,
        status,
        outcome_notes
    } ORDER BY .sequence
}
FILTER .workflow.repo = <str>$repo
ORDER BY .created_at DESC
LIMIT 5;
```

An agent starting a new session can ask hive-server: "What plans have been created for this repo? Which tasks failed? What were the outcomes?" This is currently impossible.

### 7.2 Skill Effectiveness Metrics

```edgeql
# Top 10 most effective skills in the last 30 days
SELECT Skill {
    name,
    skill_type,
    invocation_count,
    avg_effectiveness,
    recent_invocations := count(
        .<skill[is SkillInvocation]
        FILTER .invoked_at > datetime_current() - <duration>'30 days'
    )
}
ORDER BY .avg_effectiveness DESC THEN .invocation_count DESC
LIMIT 10;
```

### 7.3 Failure Pattern Detection

```edgeql
# Find recurring failure patterns: same stage failing in same repo
SELECT {
    repo := WorkflowStage.workflow.repo,
    stage := WorkflowStage.stage,
    failure_count := count(
        WorkflowStage
        FILTER .outcome = StageOutcome.failure
    ),
    last_failure := max(
        (SELECT WorkflowStage
         FILTER .outcome = StageOutcome.failure
        ).completed_at
    ),
    common_skills := (
        SELECT WorkflowStage.skill_invocations.skill {
            name
        }
    )
}
FILTER WorkflowStage.outcome = StageOutcome.failure
GROUP BY WorkflowStage.workflow.repo, WorkflowStage.stage
ORDER BY .failure_count DESC;
```

### 7.4 Agent Coordination State

```edgeql
# Check what parallel agents are currently working on for a workflow
SELECT AgentSession {
    agent_id,
    role,
    session_id,
    current_task := (
        SELECT .tasks { title, status, started_at }
        FILTER .status = TaskStatus.in_progress
        LIMIT 1
    ),
    completed_tasks := count(.tasks FILTER .status = TaskStatus.done),
    total_tasks := count(.tasks)
}
FILTER .workflow.id = <uuid>$workflow_id
AND .completed_at IS NOT SET;
```

---

## 8. New Capabilities Enabled

### 8.1 Skill Recommendation Engine

With invocation and effectiveness data, hive-server can recommend skills proactively:

```edgeql
# Given a task description, find skills that were effective
# in similar past workflows
SELECT Skill {
    name,
    description,
    avg_effectiveness,
    relevance := count(
        .<skill[is SkillInvocation]
        FILTER .stage.workflow.repo = <str>$repo
        AND .effectiveness_score > 0.7
    )
}
FILTER .relevance > 0
ORDER BY .relevance DESC THEN .avg_effectiveness DESC
LIMIT 5;
```

### 8.2 Workflow Templates

Successful workflows become templates for future work:

```edgeql
# Clone a successful workflow's task structure for a new feature
WITH
    template := (
        SELECT Workflow
        FILTER .id = <uuid>$template_workflow_id
    )
FOR task IN template.plans.tasks
UNION (
    INSERT PlanTask {
        title := task.title,
        sequence := task.sequence,
        status := TaskStatus.pending,
        description := task.description,
        plan := (SELECT Plan FILTER .id = <uuid>$new_plan_id)
    }
);
```

### 8.3 Organization-Wide Skill Sharing

The `SkillSource.organization` enum value enables multi-repo skill management:

```edgeql
# Find organization skills that are effective across multiple repos
SELECT Skill {
    name,
    description,
    repos_used_in := count(DISTINCT
        .<skill[is SkillInvocation].stage.workflow.repo
    ),
    avg_effectiveness
}
FILTER .source = SkillSource.organization
AND count(DISTINCT .<skill[is SkillInvocation].stage.workflow.repo) > 1
ORDER BY .avg_effectiveness DESC;
```

### 8.4 Context Window Optimization via Selective Retrieval

Instead of loading all skills into context, hive-server can retrieve only the relevant ones:

```edgeql
# Retrieve skills ranked by relevance to the current repo and stage
SELECT Skill {
    name,
    description,
    content_hash,
    relevance_score := (
        count(
            .<skill[is SkillInvocation]
            FILTER .stage.workflow.repo = <str>$repo
            AND .stage.stage = <PipelineStage>$current_stage
            AND .effectiveness_score > 0.5
        )
    )
}
FILTER .relevance_score > 0
   OR .name IN {'using-superpowers', 'verification-before-completion'}
ORDER BY .relevance_score DESC;
```

This directly addresses Superpowers' context window limitation by using historical effectiveness data to prioritize which skills to load.

### 8.5 Gel AI Extension for Semantic Search

Gel's `ext::ai` extension with pgvector enables semantic similarity search over past plans, designs, and agent outputs:

```sdl
# Add to Plan type
type Plan extending HasTimestamps, HasAgent {
    # ... existing fields ...
    embedding: ext::ai::embedding;  # Vector embedding of plan content
}
```

```edgeql
# Find semantically similar past plans
SELECT Plan {
    title,
    summary,
    completion_pct,
    created_at
}
ORDER BY ext::ai::cosine_similarity(.embedding, <array<float32>>$query_embedding) DESC
LIMIT 5;
```

This enables "find me plans that dealt with similar problems" -- a RAG pipeline grounded in the team's actual history rather than generic training data.

---

## 9. Tradeoffs

### 9.1 What Is Gained

| Capability               | Current State            | With Gel                                                                               |
| ------------------------ | ------------------------ | -------------------------------------------------------------------------------------- |
| Cross-session memory     | None                     | Full workflow history, queryable by repo, agent, skill, stage, outcome                 |
| Skill effectiveness      | Unknown                  | Quantified per-skill, per-type, per-repo effectiveness scores                          |
| Agent coordination       | Fire-and-forget          | Task dependency tracking, status visibility, reactive unblocking                       |
| Plan search              | Grep over markdown files | Graph traversal + semantic similarity via ext::ai                                      |
| Workflow analytics       | None                     | Duration tracking, success rates, failure pattern detection, bottleneck identification |
| Skill recommendations    | Manual "1% rule"         | Data-driven relevance scoring based on historical effectiveness                        |
| Multi-repo skill sharing | Clone per repo           | Organization-scoped skills with cross-repo effectiveness data                          |
| Context optimization     | Load everything          | Selective retrieval based on relevance scoring                                         |

### 9.2 What Is Lost

| Current Advantage           | Impact of Adding Gel                                                                                                                                                            |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Zero infrastructure         | Gel requires a server process (1 GB RAM minimum) plus PostgreSQL. Local dev can still use SQLite for hive-server's basic features, but Gel-backed features require Gel running. |
| Instant setup               | `gel project init` + migrations add setup steps. Docker Compose mitigates this but is still more than "clone + symlink."                                                        |
| Platform agnosticism        | Gel integration only benefits agents that talk to hive-server. Cursor/Codex/OpenCode agents not using hive-server get nothing.                                                  |
| Simplicity                  | The skill graph, invocation tracking, and effectiveness scoring add conceptual overhead. Developers must understand the data model to benefit from it.                          |
| Pure-markdown composability | Skills remain markdown files, but the metadata layer in Gel creates a parallel source of truth. Drift between SKILL.md files and Gel records is a risk.                         |

### 9.3 Complexity Added

| Area                      | Complexity                                                                                                                                                                                     |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Schema maintenance**    | 10+ types in the Gel schema. Migrations required for schema changes. Need CI pipeline for `gel migration create` + `gel migrate`.                                                              |
| **Data ingestion**        | hive-server needs endpoints to receive skill invocation events, stage transitions, plan structures. The agent or Superpowers hooks must emit these events.                                     |
| **Effectiveness scoring** | Who determines effectiveness? The agent self-reports? The orchestrator rates the subagent? Automated (tests pass = effective)? This is a hard design problem with no obvious answer.           |
| **Consistency**           | Gel records must stay in sync with filesystem artifacts (plan markdown files, git branches). If an agent modifies a plan file without going through hive-server, the Gel record becomes stale. |
| **Operational burden**    | Gel server monitoring, PostgreSQL backend management, backup strategy, resource scaling for multi-tenant use.                                                                                  |

---

## 10. Practical Integration: hive-server as Mediator

### 10.1 Architecture

```
Superpowers (Claude Code plugin)
    |
    | (hooks emit events: skill invoked, stage started, plan created, etc.)
    |
    v
hive-server REST API (existing /api/v1/ prefix)
    |
    +--> /api/v1/skills/           Skill catalog CRUD
    +--> /api/v1/workflows/        Workflow lifecycle
    +--> /api/v1/stages/           Stage transitions
    +--> /api/v1/plans/            Plan structure + task tracking
    +--> /api/v1/agents/           Agent session registration
    +--> /api/v1/invocations/      Skill invocation recording
    +--> /api/v1/analytics/        Read-only analytics queries
    |
    v
GelStore (implements Store interface)
    |
    v
Gel Server --> PostgreSQL
```

### 10.2 Store Interface Extension

The existing `Store` interface in hive-server handles basic CRUD. For Superpowers integration, it needs additional methods:

```go
type SuperpowersStore interface {
    // Skill catalog
    UpsertSkill(ctx context.Context, skill *Skill) error
    GetSkill(ctx context.Context, name string) (*Skill, error)
    ListSkills(ctx context.Context, filter SkillFilter) ([]*Skill, error)
    GetSkillDependencies(ctx context.Context, name string, depth int) ([]*Skill, error)

    // Workflow lifecycle
    CreateWorkflow(ctx context.Context, wf *Workflow) error
    GetWorkflow(ctx context.Context, id string) (*Workflow, error)
    ListWorkflows(ctx context.Context, repo string) ([]*Workflow, error)

    // Stage tracking
    StartStage(ctx context.Context, workflowID string, stage PipelineStage) (*WorkflowStage, error)
    CompleteStage(ctx context.Context, stageID string, outcome StageOutcome) error

    // Plan management
    CreatePlan(ctx context.Context, plan *Plan) error
    UpdateTaskStatus(ctx context.Context, taskID string, status TaskStatus) error
    GetBlockedTasks(ctx context.Context, workflowID string) ([]*PlanTask, error)

    // Agent coordination
    RegisterAgent(ctx context.Context, session *AgentSession) error
    GetActiveAgents(ctx context.Context, workflowID string) ([]*AgentSession, error)
    GetAgentTree(ctx context.Context, workflowID string) (*AgentSession, error)

    // Invocation tracking
    RecordInvocation(ctx context.Context, inv *SkillInvocation) error
    GetSkillEffectiveness(ctx context.Context, filter EffectivenessFilter) (*EffectivenessReport, error)

    // Analytics
    GetPipelineAnalytics(ctx context.Context, repo string) (*PipelineAnalytics, error)
    GetFailurePatterns(ctx context.Context, repo string) ([]*FailurePattern, error)
    RecommendSkills(ctx context.Context, repo string, stage PipelineStage) ([]*SkillRecommendation, error)
}
```

### 10.3 Hook Integration Points

Superpowers hooks are the natural emission points for Gel data. The session-start hook already runs synchronously. Additional hooks could emit events:

1. **SessionStart hook**: Register `AgentSession` with hive-server. Receive recommended skills based on repo + context.
2. **Skill activation**: When a skill is loaded (in `skills-core.js`), emit a `SkillInvocation` event to hive-server.
3. **Stage transitions**: When `/brainstorm`, `/write-plan`, `/execute-plan` commands are invoked, emit stage start/complete events.
4. **Plan creation**: When a plan markdown file is written, parse it and emit structured `Plan` + `PlanTask` records.
5. **Task completion**: When a plan task checkbox is toggled, emit a status update.

The `EnterPlanMode` intercept hook (added in v4.3.0) is a precedent for this kind of lifecycle interception.

### 10.4 Dual-Backend Strategy

For local development without Gel:

```go
func NewStore(cfg Config) (SuperpowersStore, error) {
    if cfg.GelDSN != "" {
        return NewGelStore(cfg.GelDSN)
    }
    // SQLite fallback: implements the interface but with flat tables
    // and no graph traversal. Analytics queries return empty results.
    return NewSQLiteStore(cfg.SQLitePath)
}
```

The SQLite backend would implement the interface with degraded functionality: no graph traversal, no computed properties, no semantic search. But basic CRUD (skill catalog, workflow tracking, plan storage) would work.

### 10.5 Deployment

```yaml
# docker-compose.yml addition
services:
  gel:
    image: geldata/gel:6
    environment:
      GEL_SERVER_SECURITY: insecure_dev_mode
    volumes:
      - ./dbschema:/dbschema
      - gel-data:/var/lib/gel/data
    ports:
      - "5656:5656"
    healthcheck:
      test: ["CMD", "gel", "query", "SELECT 1"]
      interval: 10s
      timeout: 5s
      retries: 5

  hive-server:
    # ... existing config ...
    environment:
      GEL_DSN: gel://gel:5656
    depends_on:
      gel:
        condition: service_healthy
```

For production Kubernetes deployment, Gel runs as a separate pod with a PersistentVolumeClaim, or more robustly, against a managed PostgreSQL instance via `GEL_SERVER_BACKEND_DSN`.

---

## 11. Implementation Phases

### Phase 1: Skill Catalog and Invocation Tracking (Low Risk, High Signal)

- Sync SKILL.md files into Gel on startup (parse frontmatter, compute content hash).
- Record skill invocations from hooks.
- Build the skill dependency graph from analysis of skill content ("Invoke the superpowers:X skill").
- Expose `/api/v1/skills/` and `/api/v1/invocations/` endpoints.
- **Value**: Answers "which skills are used?" and "how often?" for the first time.

### Phase 2: Workflow and Stage Tracking (Medium Risk, High Value)

- Record workflow creation, stage transitions, outcomes.
- Parse plan files into structured `Plan` + `PlanTask` records.
- Expose `/api/v1/workflows/` and `/api/v1/stages/` endpoints.
- **Value**: Pipeline analytics, failure pattern detection, duration tracking.

### Phase 3: Agent Coordination (Higher Complexity, Highest Value)

- Register agent sessions with parent-child relationships.
- Track task assignments per agent.
- Implement blocked-task detection and reactive notification.
- Expose `/api/v1/agents/` endpoints.
- **Value**: Transforms fire-and-forget dispatch into coordinated multi-agent execution.

### Phase 4: Intelligence Layer (Experimental)

- Skill recommendation engine based on effectiveness data.
- Semantic search over past plans via `ext::ai`.
- Workflow templates from successful past workflows.
- Context window optimization via selective skill retrieval.
- **Value**: The system gets smarter over time instead of starting from zero every session.

---

## 12. Open Questions

1. **Effectiveness scoring source**: Self-reported by agents? Derived from test outcomes? Rated by orchestrators? A combination? The scoring methodology directly determines the quality of all analytics.

2. **Event emission mechanism**: Should Superpowers hooks call hive-server directly (tight coupling)? Should there be an event bus? Should hive-server watch filesystem artifacts and infer events (loose coupling, higher latency)?

3. **Schema ownership**: If Gel holds the canonical skill catalog, what happens when a SKILL.md file is edited but the Gel record is not updated? Git hooks? Startup sync? Eventual consistency?

4. **Multi-tenancy**: If hive-server serves multiple users/repos, how is data isolated? Gel's access policies can enforce row-level security, but the agent identity model needs design.

5. **Cold start**: When Gel has no data yet, all analytics return empty results and skill recommendations are useless. How long until the system accumulates enough data to be valuable? Is there a bootstrap strategy (import from git history)?

6. **Performance at scale**: If every skill invocation in every agent session across every repo is recorded, how fast does the data grow? What are the query performance characteristics with millions of invocation records? Gel's PostgreSQL backend handles scale well, but indexes and query patterns need benchmarking.

---

## 13. Conclusion

Superpowers is a remarkably effective framework constrained by a deliberate architectural choice: no persistence. This choice keeps it simple, portable, and zero-infrastructure. But it means every agent session starts from zero, every skill is assumed equally effective, every dispatch is blind to history, and every plan is an island.

Gel DB, accessed through hive-server, can fill this gap without modifying Superpowers itself. The integration is additive: hooks emit events, hive-server records them in Gel, and agents query hive-server for historical context. Superpowers continues to work identically without hive-server -- it just works better with it.

The graph-relational model is not just convenient here; it is structurally appropriate. Skills form dependency graphs. Agents form dispatch trees. Tasks form dependency DAGs. Plans chain into workflows. These are graph problems wearing relational clothing, and Gel handles both natively.

The primary risks are operational complexity (running Gel in production), data freshness (keeping Gel in sync with filesystem artifacts), and the cold start problem (analytics are useless until enough data accumulates). These are manageable risks for the capability gains: cross-session memory, skill effectiveness metrics, coordinated multi-agent execution, and a system that genuinely learns from its own history.
