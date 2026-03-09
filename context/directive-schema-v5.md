# Behavioral Directive Schema Design

**Date:** 2026-03-09
**Purpose:** Define the data model for decomposing skill prompts into atomic behavioral directives, storing them across CockroachDB, Meilisearch, and Gel DB, and recomposing them contextually at query time.
**Supersedes:** directive-schema.md
**Authority:** vision-v5.md (all schema decisions derive from this document)

---

## Table of Contents

1. [What IS a Directive](#1-what-is-a-directive)
2. [CockroachDB Schema](#2-cockroachdb-schema)
3. [Meilisearch Index](#3-meilisearch-index)
4. [Gel DB Schema](#4-gel-db-schema)
5. [The Decomposition Model](#5-the-decomposition-model)
6. [The Recomposition Model](#6-the-recomposition-model)
7. [Conflict Resolution](#7-conflict-resolution)

---

## 1. What IS a Directive

A **behavioral directive** is the atomic unit of agent behavioral instruction. It is a single, self-contained instruction that steers agent behavior in a specific situation. It answers one question: **"In situation X, the agent should do Y."**

### Definition

A directive has four essential properties:

1. **Trigger condition**: When does this directive apply? What situation, activity, or context activates it?
2. **Instruction**: What should the agent do? A concrete behavioral command.
3. **Rationale**: Why should the agent do this? What failure mode does it prevent?
4. **Verification**: How can we tell the directive was followed? What observable evidence exists?

### Granularity

The right granularity is: **one behavioral decision per directive**. A directive should be independently actionable -- an agent could follow it without needing to read any other directive.

**Too granular** (sub-atomic):

- "Open the terminal." -- This is a mechanical step, not a behavioral choice.
- "Type `npm test`." -- This is a keystroke, not a behavior.

**Too coarse** (compound):

- "When implementing a feature, first brainstorm approaches, then write a plan with phases, then implement via TDD with red-green-refactor, then verify with fresh test runs." -- This is a workflow containing 4+ distinct behavioral decisions.

**Correct granularity** (atomic):

- "When implementing a feature, create a written plan before writing any code." -- One behavioral decision: plan first.
- "When running tests for verification, always run them fresh rather than relying on cached results." -- One behavioral decision: no cached results.
- "When a debugging session exceeds 3 failed fix attempts, stop and reformulate your hypothesis about the root cause." -- One behavioral decision: stop and rethink at threshold.

### Directive Kinds

Per vision-v5, there are exactly five directive kinds. The field name is `kind` (TEXT), not `directive_type` (ENUM).

| Kind         | What It Is                     | Example                                          | Typical Source       |
| ------------ | ------------------------------ | ------------------------------------------------ | -------------------- |
| `behavioral` | How to approach a type of work | "When debugging, reproduce before fixing"        | Skill decomposition  |
| `pattern`    | Codebase-specific conventions  | "This repo uses chi v5 with grouped routes"      | Codebase observation |
| `contextual` | Situation-specific awareness   | "There are 2 open AUTH requirements"             | State queries        |
| `corrective` | Learned from past mistakes     | "In 3/5 debug sessions, root cause was a race"   | Experience feedback  |
| `factual`    | Things the agent should know   | "The user prefers dark mode and terse responses" | User memory          |

### Fields

| Field                  | Type        | Required | Description                                                                                                 |
| ---------------------- | ----------- | -------- | ----------------------------------------------------------------------------------------------------------- |
| `id`                   | UUID        | yes      | Globally unique identifier                                                                                  |
| `content`              | TEXT        | yes      | The directive text itself -- the actual behavioral instruction                                              |
| `kind`                 | TEXT        | yes      | One of: `behavioral`, `pattern`, `contextual`, `corrective`, `factual`                                      |
| `source_type`          | TEXT        | yes      | Where it came from: `skill`, `experience`, `feedback`, `observation`, `user`                                |
| `source_id`            | TEXT        | yes      | ID of the skill, session, or event that produced this                                                       |
| `source_name`          | TEXT        | yes      | Human-readable: `superpowers:systematic-debugging`, `session:2026-03-08T14:00`                              |
| `source_section`       | TEXT        | yes      | Which part of the source: `step-3-hypothesis`, `anti-pattern-2` (default: `''`)                             |
| `trigger_tags`         | JSONB       | yes      | Semantic tags: `["debugging", "test-failure", "race-condition"]`                                            |
| `trigger_intent`       | TEXT        | yes      | Natural language: "Agent is debugging an intermittent failure" (default: `''`)                              |
| `trigger_phase`        | TEXT        | yes      | Workflow phase: `planning`, `implementation`, `debugging`, `review`, `brainstorming`, `any` (default: `''`) |
| `trigger_scope`        | TEXT        | yes      | Scope: `global`, `repo:christmas-island/hive-server`, `project:proj_abc` (default: `''`)                    |
| `times_injected`       | INT8        | yes      | How many times this was sent to an agent (default: 0)                                                       |
| `times_followed`       | INT8        | yes      | How many times the agent demonstrably followed it (default: 0)                                              |
| `times_ignored`        | INT8        | yes      | How many times the agent ignored it (default: 0)                                                            |
| `times_negative`       | INT8        | yes      | How many times following it led to a worse outcome (default: 0)                                             |
| `effectiveness`        | FLOAT8      | yes      | Computed: `(followed - negative) / GREATEST(injected, 1)`, or 0.0 if never injected (default: 0.0)          |
| `related_ids`          | JSONB       | yes      | UUIDs of related directives (default: `[]`)                                                                 |
| `supersedes_id`        | UUID        | no       | If this directive replaces an older one                                                                     |
| `chain_id`             | UUID        | no       | Group of directives that form a behavioral chain                                                            |
| `weight`               | FLOAT8      | yes      | Priority weight for injection ranking, range 0.0-2.0 (default: 1.0)                                         |
| `token_cost`           | INT4        | yes      | Estimated tokens when rendered (default: 0)                                                                 |
| `active`               | BOOLEAN     | yes      | Soft delete / disable (default: true)                                                                       |
| `tenant_id`            | UUID        | yes      | Multi-tenancy identifier                                                                                    |
| `decomposition_run_id` | UUID        | no       | Which decomposition batch produced this (nullable -- experience, user, observation directives have no run)  |
| `source_text_hash`     | TEXT        | no       | SHA-256 of the source text for traceability (nullable -- not all sources have hashable text)                |
| `created_at`           | TIMESTAMPTZ | yes      | When this directive was created                                                                             |
| `updated_at`           | TIMESTAMPTZ | yes      | When this directive was last modified                                                                       |

### Context Triggers

Trigger information is stored across four dedicated fields on the directives table rather than a single nested JSONB blob:

- **`trigger_tags`** (JSONB array): Semantic keywords. `["debugging", "test-failure", "race-condition"]`
- **`trigger_intent`** (TEXT): Natural language description. `"Agent is debugging an intermittent failure"`
- **`trigger_phase`** (TEXT): Workflow phase. `"debugging"`, `"planning"`, `"implementation"`, `"review"`, `"brainstorming"`, `"any"`
- **`trigger_scope`** (TEXT): Applicability scope. `"global"`, `"repo:christmas-island/hive-server"`, `"project:proj_abc"`

A directive activates when the agent's current context matches its triggers. Phase and scope are exact matches. Tags and intent are matched via Meilisearch semantic search. A directive with `trigger_phase = ''` or `trigger_phase = 'any'` is eligible in all phases.

### Directive Lifecycle

```
                 +-----------+
                 |  Created  |
                 +-----+-----+
                       |
              (decomposition, feedback,
               experience, or user input)
                       |
                 +-----v-----+
                 |   Active   |<-----------+
                 +-----+-----+             |
                       |                   |
              (injected into context)      |
                       |                   |
                 +-----v-----+        (effectiveness
                 |  Injected  |        improves)
                 +-----+-----+             |
                       |                   |
              (agent outcome observed)     |
                       |                   |
              +--------+--------+          |
              |                 |          |
        +-----v-----+   +------v-----+    |
        |  Followed  |   |  Ignored   |---+
        +-----+-----+   +------+-----+
              |                 |
              |          (if consistently ignored)
              |                 |
              |          +------v-----+
              |          | Deprecated |
              |          +------------+
              |
        (outcome evaluated)
              |
        +-----v------+
        |  Effective  |-----> weight increases, more likely to be injected
        +-----+------+
              |
        (or negative outcome)
              |
        +-----v------+
        |  Negative   |-----> weight decreases, may be deprecated
        +------------+
```

---

## 2. CockroachDB Schema

CockroachDB is the transactional source of truth. All directive data lives here first. Meilisearch and Gel DB are derived views.

### Full DDL

```sql
-- ============================================================
-- Core Tables
-- ============================================================

-- Decomposition runs: batch provenance tracking
CREATE TABLE decomposition_runs (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    source_type     TEXT        NOT NULL,   -- 'skill', 'document', 'user_input'
    source_name     TEXT        NOT NULL,   -- e.g., 'superpowers:brainstorming'
    source_document TEXT        NOT NULL,   -- full path or URL
    source_text_hash TEXT       NOT NULL,   -- SHA-256 of the input text
    model_used      TEXT        NOT NULL,   -- e.g., 'claude-opus-4-6'
    prompt_version  TEXT        NOT NULL,   -- version of the decomposition prompt
    directives_created INT4    NOT NULL DEFAULT 0,
    tenant_id       UUID        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_runs_source ON decomposition_runs (source_type, source_name, tenant_id);
CREATE INDEX idx_runs_hash ON decomposition_runs (source_text_hash, tenant_id);

-- The directive table: the atomic behavioral unit
CREATE TABLE directives (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The directive itself
    content         TEXT        NOT NULL,       -- The behavioral instruction text
    kind            TEXT        NOT NULL,       -- 'behavioral', 'pattern', 'contextual', 'corrective', 'factual'

    -- Provenance: where did this come from?
    source_type     TEXT        NOT NULL,       -- 'skill', 'experience', 'feedback', 'observation', 'user'
    source_id       TEXT        NOT NULL,       -- ID of the skill, session, or event that produced this
    source_name     TEXT        NOT NULL,       -- Human-readable: 'superpowers:systematic-debugging', 'session:2026-03-08T14:00'
    source_section  TEXT        NOT NULL DEFAULT '',  -- Which part of the source: 'step-3-hypothesis', 'anti-pattern-2'

    -- Context triggers: when should this directive activate?
    trigger_tags    JSONB       NOT NULL DEFAULT '[]'::JSONB,    -- Semantic tags: ["debugging", "test-failure", "race-condition"]
    trigger_intent  TEXT        NOT NULL DEFAULT '',              -- Natural language: "Agent is debugging an intermittent failure"
    trigger_phase   TEXT        NOT NULL DEFAULT '',              -- Workflow phase: "planning", "implementation", "debugging", "review", "brainstorming"
    trigger_scope   TEXT        NOT NULL DEFAULT '',              -- Scope: "global", "repo:christmas-island/hive-server", "project:proj_abc"

    -- Effectiveness: does this directive actually help?
    times_injected  INT8        NOT NULL DEFAULT 0,     -- How many times this was sent to an agent
    times_followed  INT8        NOT NULL DEFAULT 0,     -- How many times the agent demonstrably followed it
    times_ignored   INT8        NOT NULL DEFAULT 0,     -- How many times the agent ignored it
    times_negative  INT8        NOT NULL DEFAULT 0,     -- How many times following it led to a worse outcome
    effectiveness   FLOAT8      NOT NULL DEFAULT 0.0,   -- Computed: (followed - negative) / GREATEST(injected, 1), or 0.0 if never injected

    -- Relationships (denormalized for query performance; Gel has the full graph)
    related_ids     JSONB       NOT NULL DEFAULT '[]'::JSONB,    -- UUIDs of related directives
    supersedes_id   UUID,                                         -- If this directive replaces an older one
    chain_id        UUID,                                         -- Group of directives that form a behavioral chain

    -- Metadata
    weight          FLOAT8      NOT NULL DEFAULT 1.0,   -- Priority weight for injection ranking (0.0-2.0)
    token_cost      INT4        NOT NULL DEFAULT 0,     -- Estimated tokens when rendered
    active          BOOLEAN     NOT NULL DEFAULT true,   -- Soft delete / disable
    tenant_id       UUID        NOT NULL,                -- Multi-tenancy

    -- Optional provenance
    decomposition_run_id UUID   REFERENCES decomposition_runs(id),  -- nullable: experience/user/observation directives have no run
    source_text_hash TEXT,                                           -- nullable: not all sources have hashable text

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Indexes for the injection pipeline
CREATE INDEX idx_directives_active ON directives (active, tenant_id) WHERE active = true;
CREATE INDEX idx_directives_kind ON directives (kind, tenant_id) WHERE active = true;
CREATE INDEX idx_directives_phase ON directives (trigger_phase, tenant_id) WHERE active = true;
CREATE INDEX idx_directives_scope ON directives (trigger_scope, tenant_id) WHERE active = true;
CREATE INDEX idx_directives_effectiveness ON directives (effectiveness DESC, tenant_id) WHERE active = true;
CREATE INDEX idx_directives_source ON directives (source_type, source_name, tenant_id);
CREATE INDEX idx_directives_chain ON directives (chain_id, tenant_id) WHERE chain_id IS NOT NULL;
CREATE INDEX idx_directives_supersedes ON directives (supersedes_id) WHERE supersedes_id IS NOT NULL;
CREATE INVERTED INDEX idx_directives_tags ON directives (trigger_tags);

-- Agent sessions: track active agent contexts
CREATE TABLE agent_sessions (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id        TEXT        NOT NULL,
    tenant_id       UUID        NOT NULL,
    repo            TEXT        NOT NULL DEFAULT '',
    project_id      UUID,
    phase           TEXT        NOT NULL DEFAULT '',  -- current workflow phase
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_active_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    summary         TEXT        NOT NULL DEFAULT '',
    active          BOOLEAN     NOT NULL DEFAULT true
);

CREATE INDEX idx_sessions_agent ON agent_sessions (agent_id, tenant_id) WHERE active = true;
CREATE INDEX idx_sessions_active ON agent_sessions (active, tenant_id) WHERE active = true;

-- Injections: track what directives were served to which sessions
CREATE TABLE injections (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID        NOT NULL REFERENCES agent_sessions(id),
    tenant_id       UUID        NOT NULL,
    context_hash    TEXT        NOT NULL,       -- Hash of the context frame for dedup
    directives      JSONB       NOT NULL,       -- Array of {directive_id, confidence} pairs
    tokens_used     INT4        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_injections_session ON injections (session_id, tenant_id);
CREATE INDEX idx_injections_created ON injections (created_at DESC, tenant_id);

-- Injection outcomes: per-directive feedback from agent behavior
CREATE TABLE injection_outcomes (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    injection_id    UUID        NOT NULL REFERENCES injections(id),
    directive_id    UUID        NOT NULL REFERENCES directives(id),
    tenant_id       UUID        NOT NULL,
    outcome         TEXT        NOT NULL,       -- 'followed', 'ignored', 'negative'
    evidence        TEXT        NOT NULL DEFAULT '',  -- What happened
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_outcomes_injection ON injection_outcomes (injection_id, tenant_id);
CREATE INDEX idx_outcomes_directive ON injection_outcomes (directive_id, tenant_id);
CREATE INDEX idx_outcomes_outcome ON injection_outcomes (outcome, tenant_id);

-- Ingestion sources: track what has been ingested and when
CREATE TABLE ingestion_sources (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT        NOT NULL,       -- 'superpowers:systematic-debugging'
    source_type     TEXT        NOT NULL,       -- 'skill', 'document', 'user_input'
    content_hash    TEXT        NOT NULL,       -- SHA256 of source content
    version         INT4        NOT NULL DEFAULT 1,
    directives_count INT4       NOT NULL DEFAULT 0,
    last_ingested   TIMESTAMPTZ NOT NULL DEFAULT now(),
    tenant_id       UUID        NOT NULL,

    CONSTRAINT uq_source_name_tenant UNIQUE (name, tenant_id)
);

CREATE INDEX idx_ingestion_sources_type ON ingestion_sources (source_type, tenant_id);
```

### Effectiveness Computation

The effectiveness score is a denormalized field updated whenever feedback is recorded. The formula is:

```
effectiveness = (times_followed - times_negative) / GREATEST(times_injected, 1)
```

This produces a value in the range [-1.0, 1.0]:

- A directive that is always followed: `effectiveness = 1.0`
- A directive that is always ignored: `effectiveness = 0.0` (ignored does not subtract)
- A directive that is always negative: `effectiveness = -1.0`
- A directive never injected: `effectiveness = 0.0` (the default)

The `times_ignored` counter is tracked for analysis but does not directly affect the effectiveness score. Ignored directives are detected by the cooldown mechanism (Section 6) and have their weight reduced over time.

### Feedback Update Queries

```sql
-- For each 'followed' outcome
UPDATE directives
SET times_injected = times_injected + 1,
    times_followed = times_followed + 1,
    effectiveness = (times_followed::FLOAT8 - times_negative::FLOAT8) / GREATEST(times_injected::FLOAT8, 1),
    updated_at = now()
WHERE id = $1 AND tenant_id = $2;

-- For each 'ignored' outcome
UPDATE directives
SET times_injected = times_injected + 1,
    times_ignored = times_ignored + 1,
    effectiveness = (times_followed::FLOAT8 - times_negative::FLOAT8) / GREATEST(times_injected::FLOAT8, 1),
    updated_at = now()
WHERE id = $1 AND tenant_id = $2;

-- For each 'negative' outcome (directive was followed but made things worse)
UPDATE directives
SET times_injected = times_injected + 1,
    times_negative = times_negative + 1,
    effectiveness = (times_followed::FLOAT8 - times_negative::FLOAT8) / GREATEST(times_injected::FLOAT8, 1),
    weight = GREATEST(weight * 0.8, 0.1),  -- Reduce weight by 20%, floor at 0.1
    updated_at = now()
WHERE id = $1 AND tenant_id = $2;
```

### Structured Filter Query (Injection Pipeline)

```sql
SELECT id, content, kind, weight, effectiveness, token_cost
FROM directives
WHERE active = true
  AND tenant_id = $1
  AND (trigger_phase = $2 OR trigger_phase = 'any' OR trigger_phase = '')
  AND (trigger_scope = 'global'
       OR trigger_scope = $3          -- repo scope
       OR trigger_scope = $4)         -- project scope
ORDER BY effectiveness DESC, weight DESC
LIMIT 50;
```

### Example Data Rows

```sql
-- Decomposition run for superpowers brainstorming skill
INSERT INTO decomposition_runs (id, source_type, source_name, source_document, source_text_hash, model_used, prompt_version, directives_created, tenant_id)
VALUES (
    'a1b2c3d4-0000-0000-0000-000000000001',
    'skill',
    'superpowers:brainstorming',
    'superpowers/skills/brainstorming/SKILL.md',
    'sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
    'claude-opus-4-6',
    'decompose-v1',
    5,
    'aaaaaaaa-0000-0000-0000-000000000001'
);

-- Decomposition run for superpowers systematic-debugging skill
INSERT INTO decomposition_runs (id, source_type, source_name, source_document, source_text_hash, model_used, prompt_version, directives_created, tenant_id)
VALUES (
    'a1b2c3d4-0000-0000-0000-000000000002',
    'skill',
    'superpowers:systematic-debugging',
    'superpowers/skills/systematic-debugging/SKILL.md',
    'sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890',
    'claude-opus-4-6',
    'decompose-v1',
    4,
    'aaaaaaaa-0000-0000-0000-000000000001'
);

-- Decomposition run for GSD sub-agent execution
INSERT INTO decomposition_runs (id, source_type, source_name, source_document, source_text_hash, model_used, prompt_version, directives_created, tenant_id)
VALUES (
    'a1b2c3d4-0000-0000-0000-000000000003',
    'skill',
    'gsd:sub-agent-execution',
    'gsd/docs/agent-personas.md',
    'sha256:fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321',
    'claude-opus-4-6',
    'decompose-v1',
    2,
    'aaaaaaaa-0000-0000-0000-000000000001'
);

-- Decomposition run for Allium specification authority
INSERT INTO decomposition_runs (id, source_type, source_name, source_document, source_text_hash, model_used, prompt_version, directives_created, tenant_id)
VALUES (
    'a1b2c3d4-0000-0000-0000-000000000004',
    'skill',
    'allium:specification-authority',
    'allium/docs/tend-weed-methodology.md',
    'sha256:1111111122222222333333334444444455555555666666667777777788888888',
    'claude-opus-4-6',
    'decompose-v1',
    1,
    'aaaaaaaa-0000-0000-0000-000000000001'
);

-- Directive 1: Mandatory Brainstorming Gate (from brainstorming skill decomposition)
INSERT INTO directives (id, content, kind, source_type, source_id, source_name, source_section, trigger_tags, trigger_intent, trigger_phase, trigger_scope, weight, token_cost, tenant_id, decomposition_run_id, source_text_hash)
VALUES (
    'd1000000-0000-0000-0000-000000000001',
    'Before any creative or design work, invoke the brainstorming process. Never jump directly to implementation or planning without first brainstorming approaches.',
    'behavioral',
    'skill',
    'superpowers:brainstorming',
    'superpowers:brainstorming',
    'core-methodology',
    '["brainstorming", "design", "architecture", "feature-work"]'::JSONB,
    'Agent needs to explore options before committing to an approach',
    'brainstorming',
    'global',
    1.2,
    35,
    'aaaaaaaa-0000-0000-0000-000000000001',
    'a1b2c3d4-0000-0000-0000-000000000001',
    'sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
);

-- Directive 2: Generate Multiple Alternatives (from brainstorming skill decomposition)
INSERT INTO directives (id, content, kind, source_type, source_id, source_name, source_section, trigger_tags, trigger_intent, trigger_phase, trigger_scope, weight, token_cost, tenant_id, decomposition_run_id, source_text_hash)
VALUES (
    'd1000000-0000-0000-0000-000000000002',
    'During brainstorming, generate at least 3 meaningfully different approaches. Each alternative must differ in architecture, algorithm, or fundamental strategy -- not just surface-level variation.',
    'behavioral',
    'skill',
    'superpowers:brainstorming',
    'superpowers:brainstorming',
    'generating-options',
    '["brainstorming", "option-evaluation", "divergent-thinking"]'::JSONB,
    'Agent has generated multiple options and needs to present them',
    'brainstorming',
    'global',
    1.0,
    42,
    'aaaaaaaa-0000-0000-0000-000000000001',
    'a1b2c3d4-0000-0000-0000-000000000001',
    'sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
);

-- Directive 3: Enumerate Constraints Before Solutions (from brainstorming skill decomposition)
INSERT INTO directives (id, content, kind, source_type, source_id, source_name, source_section, trigger_tags, trigger_intent, trigger_phase, trigger_scope, weight, token_cost, tenant_id, decomposition_run_id, source_text_hash)
VALUES (
    'd1000000-0000-0000-0000-000000000003',
    'When brainstorming, explicitly enumerate all known constraints (technical, business, time, compatibility) before generating solutions. List constraints as a visible artifact.',
    'behavioral',
    'skill',
    'superpowers:brainstorming',
    'superpowers:brainstorming',
    'constraint-identification',
    '["brainstorming", "constraints", "requirements"]'::JSONB,
    'Agent is beginning a brainstorming session and needs to identify constraints',
    'brainstorming',
    'global',
    1.0,
    38,
    'aaaaaaaa-0000-0000-0000-000000000001',
    'a1b2c3d4-0000-0000-0000-000000000001',
    'sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
);

-- Directive 4: Evaluate Tradeoffs Explicitly (from brainstorming skill decomposition)
INSERT INTO directives (id, content, kind, source_type, source_id, source_name, source_section, trigger_tags, trigger_intent, trigger_phase, trigger_scope, weight, token_cost, tenant_id, decomposition_run_id, source_text_hash)
VALUES (
    'd1000000-0000-0000-0000-000000000004',
    'After generating brainstorming alternatives, evaluate each against the identified constraints. Produce a tradeoff comparison showing strengths and weaknesses of each approach.',
    'behavioral',
    'skill',
    'superpowers:brainstorming',
    'superpowers:brainstorming',
    'evaluating-tradeoffs',
    '["brainstorming", "tradeoffs", "evaluation", "decision"]'::JSONB,
    'Agent has presented brainstorming options and needs to evaluate tradeoffs',
    'brainstorming',
    'global',
    1.0,
    40,
    'aaaaaaaa-0000-0000-0000-000000000001',
    'a1b2c3d4-0000-0000-0000-000000000001',
    'sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
);

-- Directive 5: Aggressive Skill Activation (1% Threshold) (from using-superpowers skill decomposition)
INSERT INTO directives (id, content, kind, source_type, source_id, source_name, source_section, trigger_tags, trigger_intent, trigger_phase, trigger_scope, weight, token_cost, tenant_id, decomposition_run_id, source_text_hash)
VALUES (
    'd1000000-0000-0000-0000-000000000005',
    'If there is even a 1% chance a behavioral directive applies to the current situation, activate it. Never rationalize skipping a directive.',
    'corrective',
    'skill',
    'superpowers:using-superpowers',
    'superpowers:using-superpowers',
    'anti-rationalization',
    '["meta", "activation", "anti-rationalization", "1-percent"]'::JSONB,
    'Agent is considering whether a directive applies',
    'any',
    'global',
    1.8,
    28,
    'aaaaaaaa-0000-0000-0000-000000000001',
    'a1b2c3d4-0000-0000-0000-000000000001',
    'sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
);

-- Directive 6: Verification Requires Fresh Execution (from verification skill decomposition)
INSERT INTO directives (id, content, kind, source_type, source_id, source_name, source_section, trigger_tags, trigger_intent, trigger_phase, trigger_scope, weight, token_cost, tenant_id, decomposition_run_id, source_text_hash)
VALUES (
    'd1000000-0000-0000-0000-000000000006',
    'Before claiming any task is complete, run the verification command freshly. Never rely on cached results, prior test runs, or assumed state. Read the full output including exit codes.',
    'corrective',
    'skill',
    'superpowers:verification-before-completion',
    'superpowers:verification-before-completion',
    'fresh-execution',
    '["verification", "testing", "completion", "evidence"]'::JSONB,
    'Agent is about to claim a task is complete',
    'review',
    'global',
    1.5,
    38,
    'aaaaaaaa-0000-0000-0000-000000000001',
    'a1b2c3d4-0000-0000-0000-000000000001',
    'sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
);

-- Directive 7: TDD Red-Green-Refactor (from TDD skill decomposition)
INSERT INTO directives (id, content, kind, source_type, source_id, source_name, source_section, trigger_tags, trigger_intent, trigger_phase, trigger_scope, weight, token_cost, tenant_id, decomposition_run_id, source_text_hash)
VALUES (
    'd1000000-0000-0000-0000-000000000007',
    'When implementing a feature, write a failing test first (red), then write the minimum code to make it pass (green), then refactor. Never write implementation code before a failing test exists.',
    'behavioral',
    'skill',
    'superpowers:test-driven-development',
    'superpowers:test-driven-development',
    'red-green-refactor',
    '["tdd", "testing", "implementation", "test-first"]'::JSONB,
    'Agent is about to implement a feature',
    'implementation',
    'global',
    1.2,
    40,
    'aaaaaaaa-0000-0000-0000-000000000001',
    'a1b2c3d4-0000-0000-0000-000000000001',
    'sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
);

-- Directive 8: Debugging Hypothesis Before Fix (from systematic-debugging skill decomposition)
INSERT INTO directives (id, content, kind, source_type, source_id, source_name, source_section, trigger_tags, trigger_intent, trigger_phase, trigger_scope, weight, token_cost, tenant_id, decomposition_run_id, source_text_hash)
VALUES (
    'd1000000-0000-0000-0000-000000000008',
    'When debugging, form an explicit hypothesis about the root cause before attempting any fix. Write the hypothesis down. Do not try fixes speculatively.',
    'behavioral',
    'skill',
    'superpowers:systematic-debugging',
    'superpowers:systematic-debugging',
    'hypothesis-first',
    '["debugging", "root-cause", "hypothesis"]'::JSONB,
    'Agent encounters a bug or failing test and needs to diagnose it',
    'debugging',
    'global',
    1.2,
    32,
    'aaaaaaaa-0000-0000-0000-000000000001',
    'a1b2c3d4-0000-0000-0000-000000000002',
    'sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890'
);

-- Directive 9: GSD Fresh Context Per Sub-Agent (from GSD decomposition)
INSERT INTO directives (id, content, kind, source_type, source_id, source_name, source_section, trigger_tags, trigger_intent, trigger_phase, trigger_scope, weight, token_cost, tenant_id, decomposition_run_id, source_text_hash)
VALUES (
    'd1000000-0000-0000-0000-000000000009',
    'When spawning sub-agents for task execution, give each sub-agent a fresh context. Do not carry accumulated conversation context from the orchestrator into the executor.',
    'behavioral',
    'skill',
    'gsd:sub-agent-execution',
    'gsd:sub-agent-execution',
    'context-management',
    '["multi-agent", "sub-agent", "delegation", "context-rot"]'::JSONB,
    'Agent is about to spawn sub-agents for task execution',
    'implementation',
    'global',
    1.0,
    36,
    'aaaaaaaa-0000-0000-0000-000000000001',
    'a1b2c3d4-0000-0000-0000-000000000003',
    'sha256:fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321'
);

-- Directive 10: GSD Atomic Task Commits (from GSD decomposition)
INSERT INTO directives (id, content, kind, source_type, source_id, source_name, source_section, trigger_tags, trigger_intent, trigger_phase, trigger_scope, weight, token_cost, tenant_id, decomposition_run_id, source_text_hash)
VALUES (
    'd1000000-0000-0000-0000-000000000010',
    'Make one git commit per completed task. Each commit should represent a single, atomic, independently revertible unit of work.',
    'behavioral',
    'skill',
    'gsd:git-integration',
    'gsd:git-integration',
    'atomic-commits',
    '["git", "commits", "implementation", "atomic"]'::JSONB,
    'Agent has completed a unit of work and needs to commit',
    'implementation',
    'global',
    1.0,
    28,
    'aaaaaaaa-0000-0000-0000-000000000001',
    'a1b2c3d4-0000-0000-0000-000000000003',
    'sha256:fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321'
);

-- Directive 11: Allium Spec Authority Over Code (from Allium decomposition)
INSERT INTO directives (id, content, kind, source_type, source_id, source_name, source_section, trigger_tags, trigger_intent, trigger_phase, trigger_scope, weight, token_cost, tenant_id, decomposition_run_id, source_text_hash)
VALUES (
    'd1000000-0000-0000-0000-000000000011',
    'When a behavioral specification (.allium file) exists, treat it as the authoritative source of intended behavior. If code diverges from the spec, the code is wrong unless the spec is explicitly updated.',
    'pattern',
    'skill',
    'allium:specification-authority',
    'allium:specification-authority',
    'spec-over-code',
    '["allium", "specification", "authority", "behavior"]'::JSONB,
    'Agent is working on code that has an associated .allium spec',
    'implementation',
    'global',
    1.3,
    42,
    'aaaaaaaa-0000-0000-0000-000000000001',
    'a1b2c3d4-0000-0000-0000-000000000004',
    'sha256:1111111122222222333333334444444455555555666666667777777788888888'
);

-- Directive 12: Debugging Threshold Reset (from systematic-debugging skill decomposition)
INSERT INTO directives (id, content, kind, source_type, source_id, source_name, source_section, trigger_tags, trigger_intent, trigger_phase, trigger_scope, weight, token_cost, tenant_id, decomposition_run_id, source_text_hash)
VALUES (
    'd1000000-0000-0000-0000-000000000012',
    'If a debugging session exceeds 3 failed fix attempts, stop. Reformulate your root cause hypothesis from scratch. Consider whether your mental model of the system is wrong.',
    'corrective',
    'skill',
    'superpowers:systematic-debugging',
    'superpowers:systematic-debugging',
    'threshold-reset',
    '["debugging", "stuck", "fix-loop", "hypothesis-reset"]'::JSONB,
    'Agent has failed 3+ fix attempts during debugging',
    'debugging',
    'global',
    1.3,
    36,
    'aaaaaaaa-0000-0000-0000-000000000001',
    'a1b2c3d4-0000-0000-0000-000000000002',
    'sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890'
);

-- Example: Experience-derived directive (no decomposition_run_id, no source_text_hash)
INSERT INTO directives (id, content, kind, source_type, source_id, source_name, source_section, trigger_tags, trigger_intent, trigger_phase, trigger_scope, weight, token_cost, tenant_id)
VALUES (
    'd1000000-0000-0000-0000-000000000013',
    'In this codebase, ensure all reads that inform writes happen inside the crdbpgx.ExecuteTx() closure. Reading outside the closure and writing inside creates stale-read bugs because CockroachDB serializable isolation only protects reads within the transaction boundary.',
    'corrective',
    'experience',
    'session:sess_xyz',
    'session:2026-03-08T14:00',
    'key-insight',
    '["cockroachdb", "transaction", "stale-read", "ExecuteTx", "isolation"]'::JSONB,
    'Agent is writing or modifying CockroachDB transaction code',
    'implementation',
    'repo:christmas-island/hive-server',
    1.2,
    52,
    'aaaaaaaa-0000-0000-0000-000000000001'
);

-- Example: User-provided directive (no decomposition_run_id, no source_text_hash)
INSERT INTO directives (id, content, kind, source_type, source_id, source_name, source_section, trigger_tags, trigger_intent, trigger_phase, trigger_scope, weight, token_cost, tenant_id)
VALUES (
    'd1000000-0000-0000-0000-000000000014',
    'The user prefers terse commit messages. Keep them under 72 characters with no emoji.',
    'factual',
    'user',
    'user:shakefu',
    'user:shakefu',
    'preferences',
    '["git", "commits", "style", "preferences"]'::JSONB,
    'Agent is writing a git commit message',
    'implementation',
    'global',
    1.0,
    18,
    'aaaaaaaa-0000-0000-0000-000000000001'
);

-- Example injection outcome feedback
INSERT INTO injection_outcomes (injection_id, directive_id, tenant_id, outcome, evidence)
VALUES (
    'bbbbbbbb-0000-0000-0000-000000000001',
    'd1000000-0000-0000-0000-000000000001',
    'aaaaaaaa-0000-0000-0000-000000000001',
    'followed',
    'Agent generated 4 approaches for OAuth implementation. Selected approach 3 after tradeoff analysis.'
);
```

---

## 3. Meilisearch Index

Meilisearch provides contextual discovery -- fast, typo-tolerant, relevance-ranked retrieval of directives based on natural language queries and structured filters.

### Index Configuration

```json
{
  "uid": "directives",
  "primaryKey": "id"
}
```

### Full Settings

Per vision Section 5.2, the index configuration is:

```json
{
  "searchableAttributes": [
    "content",
    "trigger_intent",
    "trigger_tags",
    "source_name"
  ],
  "filterableAttributes": [
    "kind",
    "trigger_phase",
    "trigger_scope",
    "active",
    "tenant_id",
    "chain_id"
  ],
  "sortableAttributes": ["effectiveness", "weight", "created_at"],
  "rankingRules": [
    "words",
    "typo",
    "proximity",
    "attribute",
    "sort",
    "exactness"
  ],
  "synonyms": {
    "debug": ["fix", "investigate", "troubleshoot", "diagnose"],
    "test": ["spec", "assertion", "verification", "check"],
    "plan": ["design", "architect", "decompose", "structure"],
    "review": ["audit", "inspect", "evaluate", "assess"]
  }
}
```

Note: Embedding configuration (for hybrid semantic search) is an implementation decision. The vision specifies that Meilisearch's hybrid search is used, but the specific embedder model and configuration are chosen at implementation time based on available resources.

### Document Shape (Denormalized for Search)

Each directive document in Meilisearch is a projection of the CockroachDB record:

```json
{
  "id": "d1000000-0000-0000-0000-000000000001",
  "content": "Before any creative or design work, invoke the brainstorming process. Never jump directly to implementation or planning without first brainstorming approaches.",
  "kind": "behavioral",
  "source_type": "skill",
  "source_id": "superpowers:brainstorming",
  "source_name": "superpowers:brainstorming",
  "source_section": "core-methodology",
  "trigger_tags": ["brainstorming", "design", "architecture", "feature-work"],
  "trigger_intent": "Agent needs to explore options before committing to an approach",
  "trigger_phase": "brainstorming",
  "trigger_scope": "global",
  "effectiveness": 0.0,
  "weight": 1.2,
  "token_cost": 35,
  "active": true,
  "tenant_id": "aaaaaaaa-0000-0000-0000-000000000001",
  "chain_id": null,
  "created_at": "2026-03-09T00:00:00Z"
}
```

The Meilisearch document mirrors the CRDB fields directly. No complex denormalization of nested JSONB is needed because the trigger fields are already separate columns in the CRDB schema.

### How Search Queries Work

**Query: "I'm debugging a test failure"**

```json
POST /indexes/directives/search
{
  "q": "debugging test failure",
  "filter": "active = true AND tenant_id = 'aaaaaaaa-0000-0000-0000-000000000001'",
  "sort": ["effectiveness:desc", "weight:desc"],
  "limit": 15,
  "showRankingScore": true
}
```

This matches via:

1. **Full-text**: "debugging" matches content/trigger_intent, "test" matches via synonym expansion to "spec"/"assertion"/"verification"/"check", "failure" matches directly.
2. **Ranking**: Effectiveness and weight break ties between equally relevant results.

**Query: "planning an auth feature" with phase filter**

```json
POST /indexes/directives/search
{
  "q": "planning authentication feature",
  "filter": "active = true AND tenant_id = 'aaaaaaaa-0000-0000-0000-000000000001' AND (trigger_phase = 'planning' OR trigger_phase = 'any' OR trigger_phase = '')",
  "sort": ["weight:desc"],
  "limit": 20,
  "showRankingScore": true
}
```

**Query: Browse all corrective directives for debugging**

```json
POST /indexes/directives/search
{
  "q": "",
  "filter": "kind = 'corrective' AND trigger_phase = 'debugging' AND active = true AND tenant_id = 'aaaaaaaa-0000-0000-0000-000000000001'",
  "sort": ["weight:desc", "effectiveness:desc"],
  "limit": 50
}
```

### Sync Strategy (CockroachDB to Meilisearch)

CockroachDB is the source of truth. Sync to Meilisearch follows this strategy:

1. **On directive INSERT/UPDATE in CockroachDB**: Upsert document into Meilisearch `directives` index. The document shape maps directly from CRDB columns -- no complex transformation needed.
2. **On directive deactivation** (`active` set to false): Delete document from Meilisearch by ID.
3. **Reconciliation**: Every 5 minutes (configurable), compare CRDB directive timestamps against Meilisearch. Re-sync any drifted records.
4. **Full rebuild**: Available via admin endpoint for disaster recovery or schema migration.

---

## 4. Gel DB Schema

Gel DB models the relationships between directives. Its primary purpose is representing **behavioral chains** -- ordered sequences of directives that form a coherent workflow -- and **directive relationships** that the flat CRDB table and keyword-based Meilisearch cannot express.

### Full SDL

Per vision Section 5.3, the Gel schema uses `DirectiveChain` types with member links, not pairwise relationship links.

```sdl
module default {
    type Directive {
        required content: str;
        required kind: str;  -- behavioral, pattern, contextual, corrective, factual
        required source_name: str;
        required weight: float64 { default := 1.0; };
        required effectiveness: float64 { default := 0.0; };
        required token_cost: int32 { default := 0; };
        required active: bool { default := true; };
        crdb_id: uuid;  -- Reference back to CRDB

        # Relationships
        multi related_to: Directive;
        multi superseded_by: Directive;
        link chain: DirectiveChain;
        property sequence_in_chain: int32;

        # Computed
        property influence_score := .weight * .effectiveness;
    }

    type DirectiveChain {
        required name: str;          -- "systematic-debugging-methodology"
        required description: str;   -- "Full debugging workflow from reproduction to verification"
        multi member directives: Directive {
            property sequence_order: int32;
        };

        # Computed
        property total_tokens := sum(.directives.token_cost);
        property avg_effectiveness := math::mean(.directives.effectiveness);
    }

    type Source {
        required name: str { constraint exclusive; };
        required source_type: str;
        multi produced: Directive;
        property directive_count := count(.produced);
        property avg_effectiveness := math::mean(.produced.effectiveness);
    }
}
```

### Key Graph Queries

**Query 1: Get the full behavioral chain for a matched directive**

When a directive is found via Meilisearch or CRDB, traverse its chain to find the full ordered sequence:

```edgeql
SELECT Directive {
    content,
    kind,
    weight,
    effectiveness,
    token_cost,
    chain := .<member[IS DirectiveChain] {
        name,
        directives: {
            content,
            kind,
            weight,
            effectiveness
        } ORDER BY .sequence_order
    }
}
FILTER .id IN <uuid>array_unpack(<array<uuid>>$found_ids)
```

If a debugging directive is found, Gel also retrieves the chain it belongs to -- the full debugging methodology sequence (reproduce -> hypothesize -> test -> fix -> verify) -- so the injection can include the relevant next step.

**Query 2: Find related directives across sources**

When a directive from one skill is selected, find related directives from other skills:

```edgeql
SELECT Directive {
    content,
    kind,
    source_name,
    related_to: {
        content,
        kind,
        source_name,
        weight,
        effectiveness
    }
}
FILTER .crdb_id = <uuid>$target_id
```

**Query 3: Get the next step in a chain**

The agent has followed the "reproduce" directive. Gel traverses the chain to find the next directive in sequence:

```edgeql
WITH current := (SELECT Directive FILTER .crdb_id = <uuid>$current_id),
     chain := (SELECT DirectiveChain FILTER current IN .directives),
     current_order := (
         SELECT chain.directives@sequence_order
         FILTER chain.directives = current
     )
SELECT chain.directives {
    content,
    kind,
    weight,
    crdb_id
}
FILTER chain.directives@sequence_order > current_order
ORDER BY chain.directives@sequence_order
LIMIT 1;
```

**Query 4: Source effectiveness comparison**

Which sources produce the most effective directives?

```edgeql
SELECT Source {
    name,
    source_type,
    directive_count,
    avg_effectiveness,
    produced: {
        content,
        kind,
        effectiveness
    } ORDER BY .effectiveness DESC
    LIMIT 5
}
ORDER BY .avg_effectiveness DESC;
```

**Query 5: Full chain listing**

Return the complete planning chain as an ordered sequence:

```edgeql
SELECT DirectiveChain {
    name,
    description,
    total_tokens,
    avg_effectiveness,
    directives: {
        content,
        kind,
        weight,
        effectiveness,
        @sequence_order
    } ORDER BY @sequence_order
}
FILTER .name = 'brainstorming-methodology';
```

---

## 5. The Decomposition Model

Given a skill prompt document, how do we extract atomic directives? This section walks through the process and shows concrete output.

### Decomposition Process

1. **Section identification**: Parse the skill document into logical sections by heading structure, numbered lists, and markdown semantics. This is deterministic -- no LLM needed.

2. **LLM analysis**: Each section is sent to an LLM with a decomposition prompt that extracts atomic behavioral directives. Each directive includes content, kind, triggers, tags, and relationship hints.

3. **Enrichment**: After extraction, directives are enriched with:

   - Token cost estimation (run content through tokenizer for actual count)
   - Cross-references (link to related directives from other skills via semantic similarity)
   - Scope assignment (global vs. repo-specific, inferred from source and content)
   - Weight assignment (anti-rationalization directives get higher initial weight)

4. **Deduplication**: Multiple skills often teach the same lesson. Semantic similarity (via Meilisearch hybrid search) finds near-duplicates. When duplicates are found, the most specific version is kept and provenance is preserved. A directive with multiple independent sources gets a higher initial weight.

5. **Chain detection**: Related directives are grouped into behavioral chains. Chains are ordered by logical sequence and linked in Gel for traversal.

6. **Storage**: Directives are inserted into CockroachDB (synchronous, source of truth), then indexed in Meilisearch and Gel (async workers, eventual consistency).

### LLM Decomposition Prompt

```
You are a behavioral knowledge extractor. Given the following section from a
skill definition for LLM coding agents, extract atomic behavioral directives.

Each directive must be:
- A single, actionable instruction (not a summary or description)
- Self-contained (understandable without the full skill document)
- Specific enough to follow (not "be careful" but "check for X before doing Y")
- Written as a direct instruction to an LLM agent

For each directive, also provide:
- trigger_tags: semantic tags for when this directive should activate
- trigger_intent: a natural language description of when to use it
- trigger_phase: which workflow phase (planning, implementation, debugging, review, brainstorming, any)
- kind: behavioral, pattern, contextual, corrective, or factual
- related_to: which other directives in this batch it connects to (by index)

Section from "{skill_name}" skill:
---
{section_content}
---

Respond with a JSON array of directives.
```

### Worked Example: Decomposing systematic-debugging

The `superpowers:systematic-debugging` skill contains roughly 2,000 tokens of instructions about how to debug systematically. The decomposition pipeline extracts directives like:

**From the core methodology section:**

```json
[
  {
    "content": "When you encounter a bug or unexpected behavior, STOP. Do not attempt any fix yet. Your first action must be to reproduce the problem reliably.",
    "kind": "behavioral",
    "trigger_tags": ["debugging", "bug", "unexpected-behavior", "test-failure"],
    "trigger_intent": "Agent encounters a bug or failing test",
    "trigger_phase": "debugging",
    "token_cost": 32
  },
  {
    "content": "After reproducing the bug, form an explicit hypothesis about the root cause. State it as: 'I believe the cause is X because Y.' Do not skip this step -- it prevents random fix attempts.",
    "kind": "behavioral",
    "trigger_tags": ["debugging", "root-cause", "hypothesis"],
    "trigger_intent": "Agent has reproduced a bug and needs to diagnose it",
    "trigger_phase": "debugging",
    "token_cost": 42
  },
  {
    "content": "Before applying a fix, write a test that fails because of the bug. This test becomes your proof that the fix works. If you cannot write such a test, you do not yet understand the bug well enough.",
    "kind": "behavioral",
    "trigger_tags": ["debugging", "tdd", "test-first", "verification"],
    "trigger_intent": "Agent is about to fix a bug",
    "trigger_phase": "debugging",
    "token_cost": 44
  }
]
```

**From the anti-patterns section:**

```json
[
  {
    "content": "Do not make multiple changes at once when debugging. Change one thing, test, observe. If you change three things and the bug disappears, you do not know which change fixed it.",
    "kind": "corrective",
    "trigger_tags": ["debugging", "shotgun-debugging", "anti-pattern"],
    "trigger_intent": "Agent is making multiple simultaneous changes during debugging",
    "trigger_phase": "debugging",
    "token_cost": 40
  },
  {
    "content": "Do not say 'the fix should work' or 'this should resolve it.' Run the actual test. Read the actual output. Only claim success when you have evidence.",
    "kind": "corrective",
    "trigger_tags": [
      "debugging",
      "verification",
      "evidence",
      "completion-claim"
    ],
    "trigger_intent": "Agent is about to claim a bug is fixed",
    "trigger_phase": "debugging",
    "token_cost": 38
  }
]
```

A single skill document produces 15-30 directives. The entire Superpowers skill set (14 skills) produces roughly 200-350 directives. GSD's 12 agent personas and methodology produce another 150-250. Allium's Tend/Weed agents and language methodology produce 50-100. The total starting knowledge base is approximately 400-700 directives.

### Non-Skill Sources

Skills are not the only ingestion source. The system also ingests:

**Codebase patterns** (`source_type: observation`): A scan of the repository structure produces factual directives: "This repo uses chi v5," "Tests are alongside code in \_test.go files." These have no `decomposition_run_id` or `source_text_hash`.

**User preferences** (`source_type: user`): When a user explicitly states a preference ("I like terse commit messages"), it becomes a factual directive with high weight. No decomposition run.

**Session outcomes** (`source_type: experience`): Successful debugging and implementation sessions generate experience-derived corrective directives via the feedback loop. No decomposition run.

**External documents** (`source_type: skill`): Architecture decision records, coding standards, team guidelines -- any markdown document can be ingested through the decomposition pipeline.

---

## 6. The Recomposition Model

Given a query context -- "agent is brainstorming a new auth feature" -- here is how we query across all three databases and assemble a set of contextualized directives within a token budget.

### Step 1: Meilisearch -- Contextual Discovery

Find directives relevant to the current context using natural language search + filters.

```json
POST /indexes/directives/search
{
  "q": "brainstorming new authentication feature design approaches",
  "filter": "active = true AND tenant_id = 'aaaaaaaa-0000-0000-0000-000000000001' AND (trigger_phase = 'planning' OR trigger_phase = 'any' OR trigger_phase = '')",
  "sort": ["weight:desc", "effectiveness:desc"],
  "limit": 25,
  "showRankingScore": true
}
```

**Expected results** (by ranking score):

| Rank | ID       | Content (truncated)                               | Score | Kind       |
| ---- | -------- | ------------------------------------------------- | ----- | ---------- |
| 1    | d1...001 | Before any creative work, invoke brainstorming... | 0.97  | behavioral |
| 2    | d1...005 | If there is even a 1% chance...                   | 0.95  | corrective |
| 3    | d1...003 | Enumerate all known constraints...                | 0.92  | behavioral |
| 4    | d1...002 | Generate at least 3 meaningfully different...     | 0.90  | behavioral |
| 5    | d1...004 | Evaluate each against identified constraints...   | 0.88  | behavioral |
| 6    | d1...011 | When a .allium spec exists, treat it as...        | 0.72  | pattern    |

### Step 2: CockroachDB -- Structured Filtering + Metadata

Query for directives matching the explicit context triggers:

```sql
SELECT id, content, kind, weight, effectiveness, token_cost
FROM directives
WHERE active = true
  AND tenant_id = $1
  AND (trigger_phase = $2 OR trigger_phase = 'any' OR trigger_phase = '')
  AND (trigger_scope = 'global'
       OR trigger_scope = $3          -- repo scope
       OR trigger_scope = $4)         -- project scope
ORDER BY effectiveness DESC, weight DESC
LIMIT 50;
```

Merge with the Meilisearch results, deduplicating by ID.

### Step 3: Gel DB -- Relationship Traversal

For the merged directive set, expand the behavioral chains. Starting from the directives found by Meilisearch and CRDB, traverse the chain graph to find related directives that form a coherent sequence:

```edgeql
SELECT Directive {
    content,
    kind,
    weight,
    effectiveness,
    token_cost,
    chain := .<member[IS DirectiveChain] {
        name,
        directives: {
            content,
            kind,
            weight,
            effectiveness
        } ORDER BY .sequence_order
    }
}
FILTER .id IN <uuid>array_unpack(<array<uuid>>$found_ids)
```

If a brainstorming directive is found, Gel retrieves the chain it belongs to -- the full brainstorming methodology (enumerate constraints -> generate alternatives -> evaluate tradeoffs -> ask user) -- so the injection includes the complete workflow in order.

### Step 4: Ranking and Selection

The three result sets are merged and ranked:

```
score = (meilisearch_relevance * 0.4)
      + (effectiveness * 0.3)
      + (weight * 0.2)
      + (recency_bonus * 0.1)
```

Where:

- **`meilisearch_relevance`**: 0.0-1.0, how semantically relevant to the current context
- **`effectiveness`**: 0.0-1.0, historical success rate `(followed - negative) / GREATEST(injected, 1)`
- **`weight`**: 0.0-2.0, priority weight (anti-rationalization directives are higher)
- **`recency_bonus`**: 0.0-0.5, bonus for directives from the agent's recent experience with this repo

### Step 5: Token Budgeting

The agent requests a token budget (default: 500 tokens). The selection algorithm:

1. Sort candidates by score descending.
2. Greedily add directives until the budget is exhausted.
3. If a directive would exceed the budget, try the next one (smaller might fit).
4. Reserve 50 tokens for the injection frame (the wrapper that presents the directives to the agent).

A 500-token budget typically fits 8-12 directives. A 200-token budget fits 3-5.

### Step 6: Contextualization

Raw directives are generic. The injection pipeline contextualizes them to the agent's current situation before returning:

**Raw directive:**

```
"When you encounter a bug, STOP. Do not attempt any fix yet. Your first action must be to reproduce the problem reliably."
```

**Contextualized for the current situation:**

```
"You are debugging a version mismatch in TestUpsertMemory. Before changing any code, write a focused assertion that demonstrates exactly when the version fails to increment. This is your reproduction case."
```

Contextualization is done by an LLM call (Sonnet-class) that takes the raw directive, the context frame, and produces a version specific to the current situation. If the LLM call fails or is unreachable, the raw directive is returned as-is.

### Assembled Output

The final injection response for "agent is brainstorming a new auth feature":

```json
{
  "injection_id": "inj_abc123",
  "directives": [
    {
      "id": "d1000000-0000-0000-0000-000000000005",
      "content": "If there is even a 1% chance a directive applies to this auth brainstorming session, activate it. Never rationalize skipping.",
      "kind": "corrective",
      "source": "superpowers:using-superpowers",
      "confidence": 0.95
    },
    {
      "id": "d1000000-0000-0000-0000-000000000001",
      "content": "You are beginning to design an auth feature. Before committing to any approach, brainstorm at least 3 meaningfully different alternatives.",
      "kind": "behavioral",
      "source": "superpowers:brainstorming",
      "confidence": 0.94
    },
    {
      "id": "d1000000-0000-0000-0000-000000000003",
      "content": "Before generating auth approaches, enumerate constraints: What auth standards are required? What existing middleware must be preserved? What is the timeline? What team expertise is available?",
      "kind": "behavioral",
      "source": "superpowers:brainstorming",
      "confidence": 0.92
    },
    {
      "id": "d1000000-0000-0000-0000-000000000002",
      "content": "Generate at least 3 auth approaches that differ fundamentally: e.g., JWT stateless, session-based stateful, OAuth2 delegation. Not 3 variations on JWT.",
      "kind": "behavioral",
      "source": "superpowers:brainstorming",
      "confidence": 0.9
    },
    {
      "id": "d1000000-0000-0000-0000-000000000004",
      "content": "After generating auth alternatives, evaluate each against the constraints you identified. Show strengths, weaknesses, and effort estimate for each. Ask the user which direction interests them.",
      "kind": "behavioral",
      "source": "superpowers:brainstorming",
      "confidence": 0.88
    },
    {
      "id": "d1000000-0000-0000-0000-000000000011",
      "content": "If .allium specs exist for auth behavior in this codebase, treat them as authoritative when designing the new auth feature.",
      "kind": "pattern",
      "source": "allium:specification-authority",
      "confidence": 0.72
    }
  ],
  "tokens_used": 389,
  "token_budget": 500,
  "candidates_considered": 42,
  "candidates_selected": 6
}
```

### Avoiding Overwhelm

The injection pipeline has several mechanisms to avoid overwhelming the agent:

1. **Token budget**: Hard cap on injection size. The MCP plugin decides how much context to spend.
2. **Diminishing returns**: If the top-scored directive has confidence 0.94 and the 8th has 0.31, the pipeline stops at the natural dropoff point rather than filling the budget with low-confidence noise.
3. **Session deduplication**: The `previous_injection_id` field lets the pipeline avoid re-injecting directives from the last call. If the agent is still debugging the same thing, it gets new directives, not the same ones repeated.
4. **Phase gating**: Only directives matching the current phase are candidates. If the agent is debugging, it does not receive planning directives.
5. **Cooldown**: A directive that was injected and ignored in the last 3 calls gets a temporary weight reduction. The agent clearly does not need it right now.

---

## 7. Conflict Resolution

### Types of Conflict

1. **Direct contradiction**: Directive A says "always do X", Directive B says "never do X."
2. **Scope overlap**: Two directives apply to the same situation but prescribe different approaches.
3. **Temporal conflict**: An older directive says one thing, a newer directive says another, but neither explicitly supersedes.

### Resolution Strategy

Conflicts are resolved using the same ranking formula used for injection selection. There is no separate conflict resolution formula -- the injection ranking itself determines which directive wins:

```
score = (meilisearch_relevance * 0.4)
      + (effectiveness * 0.3)
      + (weight * 0.2)
      + (recency_bonus * 0.1)
```

Where:

- **`meilisearch_relevance`**: 0.0-1.0, how semantically relevant to the current context
- **`effectiveness`**: Historical effectiveness from feedback, computed as `(followed - negative) / GREATEST(injected, 1)`
- **`weight`**: 0.0-2.0, priority weight
- **`recency_bonus`**: 0.0-0.5, bonus for recently updated directives

### Resolution Rules

1. **Explicit supersession is absolute.** If directive B has `supersedes_id = A`, directive A is always suppressed when B is present. No scoring needed.

2. **Higher-scored directive wins.** When two directives conflict and one outscores the other by more than 10%, the higher-scored directive is selected and the lower is suppressed for this injection.

3. **If scores are within 10% of each other, surface both with a note.** The agent is informed of the conflict and given both options with their rationale. This prevents silent suppression of relevant guidance.

4. **Chain membership preserves order.** If two conflicting directives are in the same `DirectiveChain`, the chain ordering determines which applies in the current step. The agent receives the appropriate chain step, not both.

### Weight Evolution

The weight field (0.0-2.0) evolves over time based on directive outcomes:

- **Effective directives** maintain or gain weight. When a directive is consistently followed with positive outcomes, its effectiveness score rises, which increases its injection ranking.
- **Ignored directives** lose weight gradually. After being ignored in 10 consecutive injections, they are flagged for review.
- **Negative directives** lose weight rapidly via the `weight = GREATEST(weight * 0.8, 0.1)` penalty on each negative outcome. After 3 negative outcomes, they are auto-deprecated (`active = false`) and flagged for human review.
- **Experience directives** start with moderate weight (1.0-1.2) and earn their ranking through future injections and outcomes.
- **Anti-rationalization directives** start with high weight (1.5-1.8) because they counter known LLM failure modes.

### Versioning and Deprecation

**Versioning**: Directive identity is immutable. When a directive needs to change, a new directive is created with `supersedes_id` pointing to the old one. The old directive is set to `active = false`. The new directive inherits the chain membership and relationships from the old one.

**Deprecation workflow**:

1. **Soft deprecation**: Set `active = false`. Directive stops appearing in searches and injection candidates. All relationships are preserved for historical analysis.

2. **Supersession**: Create a new directive with `supersedes_id` pointing to the old one. Set the old directive `active = false`. The new directive inherits the old one's chain membership and relationships.

3. **Hard deletion**: Only for erroneous directives (duplicates, extraction errors). Cascades through injection_outcomes and the Meilisearch/Gel indices.

### Cross-Database Consistency

The three databases must remain consistent:

| Event                 | CockroachDB                                               | Meilisearch                      | Gel DB                                     |
| --------------------- | --------------------------------------------------------- | -------------------------------- | ------------------------------------------ |
| Directive created     | INSERT (source of truth)                                  | Sync: upsert document            | Sync: INSERT node + chain links            |
| Directive updated     | UPDATE                                                    | Sync: upsert document            | Sync: UPDATE properties                    |
| Directive deactivated | SET active=false                                          | Sync: delete document            | Sync: SET active=false                     |
| Chain created         | chain_id set on directives                                | Sync: update chain_id field      | Sync: create DirectiveChain + member links |
| Feedback recorded     | INSERT into injection_outcomes, UPDATE directive counters | Sync: update effectiveness field | Sync: update effectiveness                 |

Sync is event-driven via CockroachDB changefeeds with at-least-once delivery. Idempotent upserts in both Meilisearch and Gel handle duplicates. A reconciliation job runs every 5 minutes (configurable) to compare CRDB directive timestamps against Meilisearch/Gel and re-sync any drifted records. A full rebuild is available via admin endpoint.

---

## Summary

The directive schema decomposes monolithic skill prompts into atomic behavioral instructions stored across three purpose-fit databases:

- **CockroachDB**: Transactional source of truth. Full ACID compliance, JSONB trigger tag matching, denormalized effectiveness counters (`times_injected`, `times_followed`, `times_ignored`, `times_negative`), multi-tenant isolation via `tenant_id`, changefeed for downstream sync.
- **Meilisearch**: Contextual discovery engine. Full-text + optional semantic search over directive content and trigger intent, typo-tolerant matching, filterable by kind/phase/scope/tenant, ranked by effectiveness and weight.
- **Gel DB**: Relationship graph. `DirectiveChain` types with ordered member links for behavioral sequences. `related_to` and `superseded_by` links for cross-directive relationships. Source aggregation for effectiveness analysis.

The decomposition process extracts atomic directives from skill documents, classifies them by kind (`behavioral`, `pattern`, `contextual`, `corrective`, `factual`), infers context triggers, and groups them into behavioral chains. The recomposition process queries all three databases in parallel, merges results, ranks by the injection formula `(relevance * 0.4) + (effectiveness * 0.3) + (weight * 0.2) + (recency * 0.1)`, selects within token budget, and contextualizes for the agent's current situation.
