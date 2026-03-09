# Behavioral Directive Schema Design

**Date:** 2026-03-09
**Purpose:** Define the data model for decomposing skill prompts into atomic behavioral directives, storing them across CockroachDB, Meilisearch, and Gel DB, and recomposing them contextually at query time.

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

### Directive Types

| Type           | Definition                                      | Example                                                                                                            |
| -------------- | ----------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| **behavioral** | Steers how the agent approaches a class of work | "When brainstorming, enumerate constraints before generating solutions."                                           |
| **procedural** | Prescribes a specific step within a workflow    | "After writing a test, run it to confirm it fails before implementing."                                            |
| **contextual** | Adapts behavior based on project/codebase state | "If the project uses chi router, register new routes in the existing router setup rather than creating a new one." |
| **guardrail**  | Prevents a known failure mode                   | "Never claim a task is complete without running the verification command and reading its full output."             |

### Fields

| Field                   | Type        | Required | Description                                                                              |
| ----------------------- | ----------- | -------- | ---------------------------------------------------------------------------------------- |
| `id`                    | UUID        | yes      | Globally unique identifier                                                               |
| `content`               | text        | yes      | The directive text itself -- the actual behavioral instruction                           |
| `rationale`             | text        | yes      | Why this directive exists; what failure mode it prevents                                 |
| `directive_type`        | enum        | yes      | One of: behavioral, procedural, contextual, guardrail                                    |
| `source_skill`          | text        | yes      | Which skill system this came from (superpowers, gsd, allium)                             |
| `source_section`        | text        | yes      | Which section within the skill (e.g., "brainstorming", "verification-before-completion") |
| `source_text_hash`      | text        | yes      | SHA-256 of the source text this was extracted from, for traceability                     |
| `context_triggers`      | JSONB       | yes      | Structured conditions that cause this directive to activate                              |
| `verification_criteria` | text        | no       | How to tell if the directive was followed                                                |
| `effectiveness_score`   | float       | no       | 0.0-1.0, updated by feedback loop                                                        |
| `priority`              | int         | yes      | 1-100, determines ordering when multiple directives apply (100 = highest)                |
| `version`               | int         | yes      | Monotonically increasing version for this directive                                      |
| `supersedes_id`         | UUID        | no       | If this directive replaces an older one                                                  |
| `is_active`             | bool        | yes      | Soft-delete / deprecation flag                                                           |
| `created_at`            | timestamptz | yes      | When this directive was created                                                          |
| `updated_at`            | timestamptz | yes      | When this directive was last modified                                                    |
| `decomposition_run_id`  | UUID        | yes      | Which decomposition batch produced this directive                                        |

### Context Triggers Schema

The `context_triggers` JSONB field follows a structured schema:

```json
{
  "activity_types": [
    "brainstorming",
    "implementation",
    "debugging",
    "review",
    "planning"
  ],
  "keywords": ["auth", "testing", "refactor"],
  "codebase_patterns": ["*.test.ts", "internal/handlers/*"],
  "conversation_topics": ["feature request", "bug report", "performance"],
  "workflow_stages": [
    "pre-implementation",
    "mid-implementation",
    "post-implementation"
  ],
  "agent_states": ["stuck", "confident", "exploring"],
  "project_signals": ["has_tests", "uses_tdd", "ci_failing"],
  "complexity_threshold": "high",
  "prerequisite_directives": ["uuid-of-prerequisite"]
}
```

All fields are optional. A directive activates when **any** trigger matches (OR semantics within a field, AND semantics across fields when multiple fields are present).

---

## 2. CockroachDB Schema

CockroachDB is the transactional source of truth. All directive data lives here first. Meilisearch and Gel DB are derived views.

### Full DDL

```sql
-- ============================================================
-- Enums
-- ============================================================

CREATE TYPE directive_type AS ENUM (
    'behavioral',
    'procedural',
    'contextual',
    'guardrail'
);

CREATE TYPE skill_source AS ENUM (
    'superpowers',
    'gsd',
    'allium',
    'custom',
    'derived'
);

CREATE TYPE relationship_kind AS ENUM (
    'chains_to',        -- A should be followed by B
    'conflicts_with',   -- A and B contradict; only one should apply
    'alternative_to',   -- A and B achieve the same goal differently
    'refines',          -- A is a more specific version of B
    'requires',         -- A depends on B being active
    'equivalent_to'     -- A and B say the same thing from different sources
);

CREATE TYPE feedback_outcome AS ENUM (
    'followed',         -- Agent followed the directive
    'ignored',          -- Agent ignored the directive
    'partially_followed', -- Agent followed some aspects
    'inapplicable',     -- Directive was surfaced but didn't apply
    'helpful',          -- User/agent marked as helpful
    'unhelpful'         -- User/agent marked as unhelpful
);

-- ============================================================
-- Core Tables
-- ============================================================

-- Decomposition runs: batch provenance tracking
CREATE TABLE decomposition_runs (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    source_skill    TEXT        NOT NULL,   -- e.g., "superpowers"
    source_section  TEXT        NOT NULL,   -- e.g., "brainstorming"
    source_document TEXT        NOT NULL,   -- full path or URL
    source_text_hash TEXT       NOT NULL,   -- SHA-256 of the input text
    model_used      TEXT        NOT NULL,   -- e.g., "claude-opus-4-6"
    prompt_version  TEXT        NOT NULL,   -- version of the decomposition prompt
    directives_created INT4    NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Ensure we can detect when source material changes
    INDEX idx_runs_source (source_skill, source_section),
    INDEX idx_runs_hash (source_text_hash)
);

-- The directive table: the atomic behavioral unit
CREATE TABLE directives (
    id                  UUID            NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    content             TEXT            NOT NULL,
    rationale           TEXT            NOT NULL DEFAULT '',
    directive_type      directive_type  NOT NULL,
    source_skill        skill_source    NOT NULL,
    source_section      TEXT            NOT NULL,
    source_text_hash    TEXT            NOT NULL,
    context_triggers    JSONB           NOT NULL DEFAULT '{}'::JSONB,
    verification_criteria TEXT          NOT NULL DEFAULT '',
    effectiveness_score FLOAT8          NOT NULL DEFAULT 0.5,
    priority            INT4            NOT NULL DEFAULT 50,
    version             INT4            NOT NULL DEFAULT 1,
    supersedes_id       UUID            REFERENCES directives(id),
    is_active           BOOL            NOT NULL DEFAULT true,
    decomposition_run_id UUID           NOT NULL REFERENCES decomposition_runs(id),
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),

    INDEX idx_directives_type (directive_type),
    INDEX idx_directives_source (source_skill, source_section),
    INDEX idx_directives_active (is_active) WHERE is_active = true,
    INDEX idx_directives_priority (priority DESC),
    INDEX idx_directives_supersedes (supersedes_id) WHERE supersedes_id IS NOT NULL,
    INVERTED INDEX idx_directives_triggers (context_triggers)
);

-- Relationships between directives
CREATE TABLE directive_relationships (
    id              UUID                NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    from_directive  UUID                NOT NULL REFERENCES directives(id) ON DELETE CASCADE,
    to_directive    UUID                NOT NULL REFERENCES directives(id) ON DELETE CASCADE,
    kind            relationship_kind   NOT NULL,
    strength        FLOAT8              NOT NULL DEFAULT 1.0,  -- 0.0-1.0, how strong is this relationship
    description     TEXT                NOT NULL DEFAULT '',    -- human-readable explanation
    created_at      TIMESTAMPTZ         NOT NULL DEFAULT now(),

    -- A pair of directives can only have one relationship of each kind
    UNIQUE (from_directive, to_directive, kind),
    INDEX idx_rel_from (from_directive),
    INDEX idx_rel_to (to_directive),
    INDEX idx_rel_kind (kind)
);

-- Directive tags: flat tag list for fast filtering
CREATE TABLE directive_tags (
    directive_id    UUID    NOT NULL REFERENCES directives(id) ON DELETE CASCADE,
    tag             TEXT    NOT NULL,

    PRIMARY KEY (directive_id, tag),
    INDEX idx_tags_tag (tag)
);

-- Effectiveness feedback from actual agent usage
CREATE TABLE directive_feedback (
    id              UUID            NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    directive_id    UUID            NOT NULL REFERENCES directives(id) ON DELETE CASCADE,
    agent_id        TEXT            NOT NULL,
    session_id      TEXT            NOT NULL DEFAULT '',
    outcome         feedback_outcome NOT NULL,
    context_snapshot JSONB          NOT NULL DEFAULT '{}'::JSONB,  -- what was the agent doing
    notes           TEXT            NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ    NOT NULL DEFAULT now(),

    INDEX idx_feedback_directive (directive_id),
    INDEX idx_feedback_agent (agent_id),
    INDEX idx_feedback_outcome (outcome)
);

-- Directive sets: named collections of directives for specific workflows
CREATE TABLE directive_sets (
    id          UUID    NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT    NOT NULL UNIQUE,     -- e.g., "tdd-workflow", "debugging-chain"
    description TEXT    NOT NULL DEFAULT '',
    is_active   BOOL   NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE directive_set_members (
    set_id          UUID    NOT NULL REFERENCES directive_sets(id) ON DELETE CASCADE,
    directive_id    UUID    NOT NULL REFERENCES directives(id) ON DELETE CASCADE,
    ordinal         INT4    NOT NULL DEFAULT 0,  -- position in the set

    PRIMARY KEY (set_id, directive_id),
    INDEX idx_set_members_ordinal (set_id, ordinal)
);

-- ============================================================
-- Views
-- ============================================================

-- Active directives with their tag arrays and effectiveness stats
CREATE VIEW active_directives AS
SELECT
    d.id,
    d.content,
    d.rationale,
    d.directive_type,
    d.source_skill,
    d.source_section,
    d.context_triggers,
    d.verification_criteria,
    d.effectiveness_score,
    d.priority,
    d.version,
    d.created_at,
    d.updated_at,
    array_agg(DISTINCT dt.tag) FILTER (WHERE dt.tag IS NOT NULL) AS tags,
    count(DISTINCT df.id) FILTER (WHERE df.outcome = 'followed') AS times_followed,
    count(DISTINCT df.id) FILTER (WHERE df.outcome = 'ignored') AS times_ignored,
    count(DISTINCT df.id) AS total_feedback
FROM directives d
LEFT JOIN directive_tags dt ON dt.directive_id = d.id
LEFT JOIN directive_feedback df ON df.directive_id = d.id
WHERE d.is_active = true
GROUP BY d.id;

-- ============================================================
-- Functions
-- ============================================================

-- Update effectiveness score based on feedback history
-- Called periodically or on new feedback insertion
CREATE OR REPLACE FUNCTION update_effectiveness_score(target_id UUID)
RETURNS VOID AS $$
    UPDATE directives SET
        effectiveness_score = COALESCE(
            (SELECT
                (count(*) FILTER (WHERE outcome IN ('followed', 'helpful')))::FLOAT8
                / GREATEST(count(*)::FLOAT8, 1.0)
             FROM directive_feedback
             WHERE directive_id = target_id
            ), 0.5
        ),
        updated_at = now()
    WHERE id = target_id;
$$ LANGUAGE SQL;

-- Find directives matching a set of context signals
-- Returns directives ordered by relevance (trigger match count * priority * effectiveness)
CREATE OR REPLACE FUNCTION match_directives(
    p_activity_type TEXT DEFAULT NULL,
    p_keywords TEXT[] DEFAULT NULL,
    p_workflow_stage TEXT DEFAULT NULL,
    p_limit INT DEFAULT 20
) RETURNS TABLE (
    id UUID,
    content TEXT,
    rationale TEXT,
    directive_type directive_type,
    priority INT4,
    effectiveness_score FLOAT8,
    match_score FLOAT8
) AS $$
    SELECT
        d.id,
        d.content,
        d.rationale,
        d.directive_type,
        d.priority,
        d.effectiveness_score,
        (
            -- Score based on trigger matches
            (CASE WHEN p_activity_type IS NOT NULL
                  AND d.context_triggers->'activity_types' @> to_jsonb(p_activity_type)
             THEN 1.0 ELSE 0.0 END)
            +
            (CASE WHEN p_keywords IS NOT NULL
                  AND d.context_triggers->'keywords' ?| p_keywords
             THEN 1.0 ELSE 0.0 END)
            +
            (CASE WHEN p_workflow_stage IS NOT NULL
                  AND d.context_triggers->'workflow_stages' @> to_jsonb(p_workflow_stage)
             THEN 1.0 ELSE 0.0 END)
        ) * d.priority::FLOAT8 / 100.0 * d.effectiveness_score AS match_score
    FROM directives d
    WHERE d.is_active = true
    AND (
        (p_activity_type IS NOT NULL AND d.context_triggers->'activity_types' @> to_jsonb(p_activity_type))
        OR (p_keywords IS NOT NULL AND d.context_triggers->'keywords' ?| p_keywords)
        OR (p_workflow_stage IS NOT NULL AND d.context_triggers->'workflow_stages' @> to_jsonb(p_workflow_stage))
    )
    ORDER BY match_score DESC
    LIMIT p_limit;
$$ LANGUAGE SQL;
```

### Example Data Rows

```sql
-- Decomposition run
INSERT INTO decomposition_runs (id, source_skill, source_section, source_document, source_text_hash, model_used, prompt_version, directives_created)
VALUES (
    'a1b2c3d4-0000-0000-0000-000000000001',
    'superpowers',
    'brainstorming',
    'superpowers/skills/brainstorming/SKILL.md',
    'sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
    'claude-opus-4-6',
    'decompose-v1',
    12
);

-- Example directive
INSERT INTO directives (id, content, rationale, directive_type, source_skill, source_section, source_text_hash, context_triggers, verification_criteria, priority, decomposition_run_id)
VALUES (
    'd1000000-0000-0000-0000-000000000001',
    'When beginning any creative or design work, brainstorm multiple approaches before committing to one. Generate at least 3 distinct alternatives.',
    'LLM agents have a strong tendency to lock onto the first plausible approach. Brainstorming forces divergent thinking and prevents premature convergence.',
    'behavioral',
    'superpowers',
    'brainstorming',
    'sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
    '{
        "activity_types": ["brainstorming", "design", "architecture"],
        "workflow_stages": ["pre-implementation"],
        "conversation_topics": ["feature request", "new component", "refactor"]
    }'::JSONB,
    'The agent produced at least 3 distinct approaches before selecting one.',
    80,
    'a1b2c3d4-0000-0000-0000-000000000001'
);

-- Tags
INSERT INTO directive_tags (directive_id, tag) VALUES
    ('d1000000-0000-0000-0000-000000000001', 'brainstorming'),
    ('d1000000-0000-0000-0000-000000000001', 'divergent-thinking'),
    ('d1000000-0000-0000-0000-000000000001', 'pre-implementation');

-- Relationship: this directive chains to a planning directive
INSERT INTO directive_relationships (from_directive, to_directive, kind, strength, description)
VALUES (
    'd1000000-0000-0000-0000-000000000001',
    'd1000000-0000-0000-0000-000000000002',
    'chains_to',
    0.9,
    'After brainstorming alternatives, the agent should evaluate tradeoffs before selecting an approach.'
);

-- Feedback
INSERT INTO directive_feedback (directive_id, agent_id, session_id, outcome, context_snapshot, notes)
VALUES (
    'd1000000-0000-0000-0000-000000000001',
    'agent-claude-001',
    'session-2026-03-09-001',
    'followed',
    '{"activity": "brainstorming", "topic": "new auth feature", "alternatives_generated": 4}'::JSONB,
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

```json
{
  "searchableAttributes": [
    "content",
    "rationale",
    "verification_criteria",
    "tags",
    "source_section",
    "trigger_keywords"
  ],
  "filterableAttributes": [
    "directive_type",
    "source_skill",
    "source_section",
    "activity_types",
    "workflow_stages",
    "priority",
    "effectiveness_score",
    "is_active",
    "tags",
    "created_at",
    "version"
  ],
  "sortableAttributes": [
    "priority",
    "effectiveness_score",
    "created_at",
    "updated_at"
  ],
  "displayedAttributes": [
    "id",
    "content",
    "rationale",
    "directive_type",
    "source_skill",
    "source_section",
    "context_triggers",
    "verification_criteria",
    "effectiveness_score",
    "priority",
    "tags",
    "activity_types",
    "workflow_stages",
    "trigger_keywords",
    "created_at",
    "updated_at",
    "version"
  ],
  "rankingRules": [
    "words",
    "typo",
    "proximity",
    "attribute",
    "sort",
    "exactness",
    "effectiveness_score:desc",
    "priority:desc"
  ],
  "synonyms": {
    "test": ["spec", "check", "verify", "assertion"],
    "debug": ["troubleshoot", "diagnose", "investigate", "fix"],
    "brainstorm": ["ideate", "explore", "think", "design"],
    "plan": ["strategy", "roadmap", "outline", "blueprint"],
    "implement": ["code", "build", "develop", "create"],
    "review": ["inspect", "audit", "check", "examine"],
    "refactor": ["restructure", "reorganize", "clean up", "improve"],
    "verify": ["validate", "confirm", "check", "prove"],
    "tdd": ["test-driven", "red-green-refactor"],
    "auth": ["authentication", "authorization", "login", "credentials"],
    "bug": ["defect", "issue", "error", "failure"]
  },
  "stopWords": [
    "the",
    "a",
    "an",
    "is",
    "are",
    "was",
    "were",
    "be",
    "been",
    "being",
    "have",
    "has",
    "had",
    "do",
    "does",
    "did",
    "will",
    "would",
    "could",
    "should",
    "may",
    "might",
    "shall",
    "can",
    "of",
    "in",
    "to",
    "for",
    "with",
    "on",
    "at",
    "from",
    "by",
    "it",
    "its",
    "this",
    "that",
    "these",
    "those"
  ],
  "typoTolerance": {
    "enabled": true,
    "minWordSizeForTypos": {
      "oneTypo": 4,
      "twoTypos": 8
    },
    "disableOnAttributes": ["directive_type", "source_skill"]
  },
  "pagination": {
    "maxTotalHits": 1000
  },
  "faceting": {
    "maxValuesPerFacet": 100
  },
  "proximityPrecision": "byWord",
  "embedders": {
    "default": {
      "source": "openAi",
      "model": "text-embedding-3-small",
      "documentTemplate": "Behavioral directive: {{doc.content}}. Rationale: {{doc.rationale}}. Context: {{doc.trigger_keywords}}"
    }
  }
}
```

### Document Shape (Denormalized for Search)

Each directive document in Meilisearch is a denormalized view of the CockroachDB record with flattened trigger fields:

```json
{
  "id": "d1000000-0000-0000-0000-000000000001",
  "content": "When beginning any creative or design work, brainstorm multiple approaches before committing to one. Generate at least 3 distinct alternatives.",
  "rationale": "LLM agents have a strong tendency to lock onto the first plausible approach. Brainstorming forces divergent thinking and prevents premature convergence.",
  "directive_type": "behavioral",
  "source_skill": "superpowers",
  "source_section": "brainstorming",
  "context_triggers": {
    "activity_types": ["brainstorming", "design", "architecture"],
    "workflow_stages": ["pre-implementation"],
    "conversation_topics": ["feature request", "new component", "refactor"]
  },
  "verification_criteria": "The agent produced at least 3 distinct approaches before selecting one.",
  "effectiveness_score": 0.85,
  "priority": 80,
  "tags": ["brainstorming", "divergent-thinking", "pre-implementation"],
  "activity_types": ["brainstorming", "design", "architecture"],
  "workflow_stages": ["pre-implementation"],
  "trigger_keywords": [
    "brainstorming",
    "design",
    "architecture",
    "pre-implementation",
    "feature request",
    "new component",
    "refactor"
  ],
  "created_at": "2026-03-09T00:00:00Z",
  "updated_at": "2026-03-09T00:00:00Z",
  "version": 1
}
```

The `activity_types`, `workflow_stages`, and `trigger_keywords` fields are **flattened extractions** from `context_triggers` to enable Meilisearch filtering (Meilisearch cannot filter on nested JSON paths). This denormalization happens during the sync from CockroachDB to Meilisearch.

### How Search Queries Work

**Query: "I'm debugging a test failure"**

```json
POST /indexes/directives/search
{
  "q": "debugging test failure",
  "filter": "is_active = true",
  "sort": ["effectiveness_score:desc", "priority:desc"],
  "limit": 15,
  "showRankingScore": true,
  "facets": ["directive_type", "source_skill", "activity_types"],
  "hybrid": {
    "embedder": "default",
    "semanticRatio": 0.3
  }
}
```

This matches via:

1. **Full-text**: "debugging" matches content/rationale, "test" matches via synonym expansion to "spec"/"check"/"verify", "failure" matches directly.
2. **Semantic**: The embedding of "debugging test failure" finds semantically similar directives even if they use different words (e.g., "investigate root cause" or "systematic troubleshooting").
3. **Ranking**: Effectiveness score and priority break ties between equally relevant results.

**Query: "planning an auth feature" with activity filter**

```json
POST /indexes/directives/search
{
  "q": "planning authentication feature",
  "filter": "activity_types = 'planning' OR activity_types = 'brainstorming' OR activity_types = 'design'",
  "sort": ["priority:desc"],
  "limit": 20,
  "showRankingScore": true,
  "hybrid": {
    "embedder": "default",
    "semanticRatio": 0.4
  }
}
```

**Query: Browse all guardrails for implementation**

```json
POST /indexes/directives/search
{
  "q": "",
  "filter": "directive_type = 'guardrail' AND workflow_stages = 'mid-implementation'",
  "sort": ["priority:desc", "effectiveness_score:desc"],
  "limit": 50,
  "facets": ["source_skill", "tags"]
}
```

### Sync Strategy (CockroachDB to Meilisearch)

CockroachDB CDC (changefeed) publishes row changes from the `directives` table. A sync worker consumes these and upserts into Meilisearch:

1. On directive INSERT/UPDATE in CockroachDB: flatten context_triggers into top-level fields, upsert document into Meilisearch `directives` index.
2. On directive DELETE (or `is_active` set to false): delete document from Meilisearch by ID.
3. Full re-sync runs nightly as a consistency check.

---

## 4. Gel DB Schema

Gel DB models the **relationship graph** between directives. While CockroachDB stores relationships in a join table, Gel DB makes them first-class traversable links, enabling queries like "give me the full behavioral chain for implementing a feature."

### Full SDL

```sdl
# ============================================================
# Scalar Types
# ============================================================

scalar type DirectiveType extending enum<
    'behavioral',
    'procedural',
    'contextual',
    'guardrail'
>;

scalar type SkillSource extending enum<
    'superpowers',
    'gsd',
    'allium',
    'custom',
    'derived'
>;

scalar type RelationshipKind extending enum<
    'chains_to',
    'conflicts_with',
    'alternative_to',
    'refines',
    'requires',
    'equivalent_to'
>;

scalar type WorkflowStage extending enum<
    'pre_implementation',
    'mid_implementation',
    'post_implementation',
    'debugging',
    'review',
    'planning',
    'brainstorming',
    'verification'
>;

# ============================================================
# Abstract Types
# ============================================================

abstract type HasTimestamps {
    required created_at: datetime {
        default := datetime_current();
    };
    required updated_at: datetime {
        default := datetime_current();
    };
}

# ============================================================
# Core Types
# ============================================================

type Directive extending HasTimestamps {
    required content: str;
    required rationale: str {
        default := '';
    };
    required directive_type: DirectiveType;
    required source_skill: SkillSource;
    required source_section: str;

    required priority: int32 {
        default := 50;
        constraint min_value(1);
        constraint max_value(100);
    };
    required effectiveness_score: float64 {
        default := 0.5;
        constraint min_value(0.0);
        constraint max_value(1.0);
    };
    required is_active: bool {
        default := true;
    };
    required version: int32 {
        default := 1;
    };

    # External ID for cross-database reference
    required crdb_id: uuid {
        constraint exclusive;
    };

    # Relationships as first-class links
    multi chains_to: Directive {
        strength: float64 {
            default := 1.0;
        };
        description: str;
    };
    multi conflicts_with: Directive {
        strength: float64 {
            default := 1.0;
        };
        description: str;
    };
    multi alternative_to: Directive {
        strength: float64 {
            default := 1.0;
        };
        description: str;
    };
    multi refines: Directive {
        strength: float64 {
            default := 1.0;
        };
        description: str;
    };
    multi requires: Directive;
    multi equivalent_to: Directive {
        description: str;
    };

    # Supersession chain
    supersedes: Directive;

    # Tags
    multi tags: str;

    # Trigger metadata (denormalized for graph queries)
    multi activity_types: str;
    multi workflow_stages: WorkflowStage;

    # Computed: how many directives chain into this one
    property inbound_chain_count := count(.<chains_to[IS Directive]);

    # Computed: how many conflicts exist
    property conflict_count := count(.conflicts_with);

    # Index for common query patterns
    index on (.source_skill);
    index on (.directive_type);
    index on (.is_active);
    index on (.crdb_id);
}

type DirectiveSet extending HasTimestamps {
    required name: str {
        constraint exclusive;
    };
    required description: str {
        default := '';
    };
    required is_active: bool {
        default := true;
    };
    multi members: Directive {
        ordinal: int32;
    };

    # External ID
    required crdb_id: uuid {
        constraint exclusive;
    };
}

type DecompositionRun {
    required source_skill: str;
    required source_section: str;
    required source_document: str;
    required source_text_hash: str;
    required model_used: str;
    required created_at: datetime {
        default := datetime_current();
    };
    multi produced: Directive;

    # External ID
    required crdb_id: uuid {
        constraint exclusive;
    };
}
```

### Key Graph Queries

**Query 1: Get the full behavioral chain for "implementing a feature"**

This traverses `chains_to` links starting from brainstorming-phase directives and walking forward through the workflow.

```edgeql
# Find all directives reachable via chains_to from brainstorming/planning directives
# that relate to feature implementation

WITH
    entry_points := (
        SELECT Directive
        FILTER
            .is_active = true
            AND 'brainstorming' IN .activity_types
            AND .directive_type = DirectiveType.behavioral
    )
SELECT entry_points {
    content,
    rationale,
    directive_type,
    source_skill,
    priority,
    effectiveness_score,

    # First-level chain
    chains_to: {
        content,
        rationale,
        directive_type,
        source_skill,
        priority,
        @strength,

        # Second-level chain
        chains_to: {
            content,
            rationale,
            directive_type,
            source_skill,
            priority,
            @strength,

            # Third-level chain
            chains_to: {
                content,
                directive_type,
                priority,
                @strength,
            }
        }
    }
}
ORDER BY .priority DESC;
```

**Query 2: Find cross-skill equivalences**

When GSD's verification step says the same thing as Superpowers' verification-before-completion, find those connections.

```edgeql
SELECT Directive {
    content,
    source_skill,
    source_section,
    equivalent_to: {
        content,
        source_skill,
        source_section,
        @description,
    }
}
FILTER
    EXISTS .equivalent_to
    AND .is_active = true
ORDER BY .source_skill;
```

**Query 3: Detect conflicts for a given directive set**

Before serving directives to an agent, check for contradictions.

```edgeql
WITH target_ids := <array<uuid>>$directive_ids
SELECT Directive {
    content,
    conflicts_with: {
        content,
        source_skill,
        @strength,
        @description,
    } FILTER .crdb_id IN array_unpack(target_ids)
}
FILTER
    .crdb_id IN array_unpack(target_ids)
    AND EXISTS .conflicts_with;
```

**Query 4: Get the dependency tree for a directive (requires chain)**

```edgeql
WITH target := (SELECT Directive FILTER .crdb_id = <uuid>$target_id)
SELECT target {
    content,
    requires: {
        content,
        directive_type,
        source_skill,
        requires: {
            content,
            directive_type,
            source_skill,
        }
    }
};
```

**Query 5: Walk from a project-specific directive up to its universal parent**

```edgeql
# A contextual directive "Use chi router for new endpoints" refines
# a universal directive "Register routes in the existing router setup"
SELECT Directive {
    content,
    directive_type,
    source_skill,

    # Walk up the refinement chain to find the universal directive
    refined_by := .<refines[IS Directive] {
        content,
        directive_type,
        source_skill,
    }
}
FILTER
    .directive_type = DirectiveType.contextual
    AND .is_active = true;
```

---

## 5. The Decomposition Model

Given a skill prompt document, how do we extract atomic directives? This section walks through actual text from the Superpowers brainstorming skill and shows the concrete output.

### Decomposition Process

1. **Section identification**: Parse the skill document into logical sections (by headers, by topic shifts, by numbered/bulleted lists).
2. **Imperative extraction**: For each section, identify every imperative statement -- every instruction that tells the agent what to do or not do.
3. **Atomization**: Split compound imperatives into individual behavioral decisions.
4. **Trigger inference**: From the surrounding context, determine when this directive should activate.
5. **Type classification**: Assign a directive type based on the nature of the instruction.
6. **Relationship detection**: Compare against existing directives to find chains, conflicts, and equivalences.
7. **Priority assignment**: Based on the language intensity ("MUST" = 90+, "should" = 60-70, "consider" = 30-50).

### Worked Example: Superpowers Brainstorming Skill

Based on the skill taxonomy and workflow described in the Superpowers brief, the brainstorming skill enforces structured ideation before planning. Here are the directives extracted from the brainstorming concept as described across the Superpowers documentation:

#### Directive 1: Mandatory Brainstorming Gate

```sql
INSERT INTO directives (id, content, rationale, directive_type, source_skill, source_section, source_text_hash, context_triggers, verification_criteria, priority, decomposition_run_id) VALUES (
    'd1000000-0000-0000-0000-000000000001',
    'Before any creative or design work, invoke the brainstorming process. Never jump directly to implementation or planning without first brainstorming approaches.',
    'Superpowers enforces brainstorming as a mandatory gate. Agents that skip brainstorming produce lower-quality solutions because they lock onto the first approach they consider.',
    'guardrail',
    'superpowers',
    'brainstorming',
    'sha256:abc001',
    '{
        "activity_types": ["design", "architecture", "feature-work"],
        "workflow_stages": ["pre-implementation"],
        "conversation_topics": ["feature request", "new component", "refactor", "redesign"]
    }'::JSONB,
    'Agent performed brainstorming before starting implementation or detailed planning.',
    95,
    'a1b2c3d4-0000-0000-0000-000000000001'
);
```

#### Directive 2: Generate Multiple Alternatives

```sql
INSERT INTO directives (id, content, rationale, directive_type, source_skill, source_section, source_text_hash, context_triggers, verification_criteria, priority, decomposition_run_id) VALUES (
    'd1000000-0000-0000-0000-000000000002',
    'During brainstorming, generate at least 3 meaningfully different approaches. Each alternative must differ in architecture, algorithm, or fundamental strategy -- not just surface-level variation.',
    'Agents default to producing variations on a single theme. Requiring structural diversity forces genuinely different perspectives.',
    'behavioral',
    'superpowers',
    'brainstorming',
    'sha256:abc001',
    '{
        "activity_types": ["brainstorming"],
        "workflow_stages": ["pre-implementation"]
    }'::JSONB,
    'At least 3 alternatives were generated, and they differ in fundamental approach (not just naming or minor parameter changes).',
    80,
    'a1b2c3d4-0000-0000-0000-000000000001'
);
```

#### Directive 3: Enumerate Constraints Before Solutions

```sql
INSERT INTO directives (id, content, rationale, directive_type, source_skill, source_section, source_text_hash, context_triggers, verification_criteria, priority, decomposition_run_id) VALUES (
    'd1000000-0000-0000-0000-000000000003',
    'When brainstorming, explicitly enumerate all known constraints (technical, business, time, compatibility) before generating solutions. List constraints as a visible artifact.',
    'Solutions generated without explicit constraint awareness frequently violate requirements discovered later, causing rework.',
    'procedural',
    'superpowers',
    'brainstorming',
    'sha256:abc001',
    '{
        "activity_types": ["brainstorming", "design"],
        "workflow_stages": ["pre-implementation"]
    }'::JSONB,
    'A list of constraints was produced before solution alternatives were generated.',
    75,
    'a1b2c3d4-0000-0000-0000-000000000001'
);
```

#### Directive 4: Evaluate Tradeoffs Explicitly

```sql
INSERT INTO directives (id, content, rationale, directive_type, source_skill, source_section, source_text_hash, context_triggers, verification_criteria, priority, decomposition_run_id) VALUES (
    'd1000000-0000-0000-0000-000000000004',
    'After generating brainstorming alternatives, evaluate each against the identified constraints. Produce a tradeoff comparison showing strengths and weaknesses of each approach.',
    'Without explicit tradeoff analysis, agents select approaches based on familiarity or simplicity rather than fitness for the problem.',
    'procedural',
    'superpowers',
    'brainstorming',
    'sha256:abc001',
    '{
        "activity_types": ["brainstorming", "design"],
        "workflow_stages": ["pre-implementation"],
        "prerequisite_directives": ["d1000000-0000-0000-0000-000000000002"]
    }'::JSONB,
    'A comparison of alternatives against constraints was produced before selecting an approach.',
    70,
    'a1b2c3d4-0000-0000-0000-000000000001'
);
```

#### Directive 5: Aggressive Skill Activation (1% Threshold)

```sql
INSERT INTO directives (id, content, rationale, directive_type, source_skill, source_section, source_text_hash, context_triggers, verification_criteria, priority, decomposition_run_id) VALUES (
    'd1000000-0000-0000-0000-000000000005',
    'If there is even a 1% chance a behavioral directive applies to the current situation, activate it. Never rationalize skipping a directive.',
    'Superpowers using-superpowers meta-skill: agents frequently rationalize skipping guidance. The 1% threshold prevents agent self-deception about directive applicability.',
    'guardrail',
    'superpowers',
    'using-superpowers',
    'sha256:abc002',
    '{
        "activity_types": ["brainstorming", "implementation", "debugging", "review", "planning", "design", "architecture"],
        "workflow_stages": ["pre-implementation", "mid-implementation", "post-implementation"]
    }'::JSONB,
    'Agent did not skip a relevant directive. When uncertain, the agent erred toward activating the directive.',
    99,
    'a1b2c3d4-0000-0000-0000-000000000001'
);
```

#### Directive 6: Verification Requires Fresh Execution

```sql
INSERT INTO directives (id, content, rationale, directive_type, source_skill, source_section, source_text_hash, context_triggers, verification_criteria, priority, decomposition_run_id) VALUES (
    'd1000000-0000-0000-0000-000000000006',
    'Before claiming any task is complete, run the verification command freshly. Never rely on cached results, prior test runs, or assumed state. Read the full output including exit codes.',
    'Agents claim success based on stale or assumed test results. Fresh verification catches regressions introduced during implementation.',
    'guardrail',
    'superpowers',
    'verification-before-completion',
    'sha256:abc003',
    '{
        "activity_types": ["implementation", "debugging", "refactoring"],
        "workflow_stages": ["post-implementation"],
        "agent_states": ["confident", "claiming-complete"]
    }'::JSONB,
    'Agent ran verification command fresh (not cached) and read full output before claiming completion.',
    95,
    'a1b2c3d4-0000-0000-0000-000000000001'
);
```

#### Directive 7: TDD Red-Green-Refactor

```sql
INSERT INTO directives (id, content, rationale, directive_type, source_skill, source_section, source_text_hash, context_triggers, verification_criteria, priority, decomposition_run_id) VALUES (
    'd1000000-0000-0000-0000-000000000007',
    'When implementing a feature, write a failing test first (red), then write the minimum code to make it pass (green), then refactor. Never write implementation code before a failing test exists.',
    'Agents default to writing implementation first and tests after. This inverts the quality signal: tests written after implementation tend to test what the code does rather than what it should do.',
    'procedural',
    'superpowers',
    'test-driven-development',
    'sha256:abc004',
    '{
        "activity_types": ["implementation"],
        "workflow_stages": ["mid-implementation"],
        "project_signals": ["uses_tdd", "has_tests"]
    }'::JSONB,
    'A failing test existed before the implementation code was written. Implementation was the minimum needed to pass.',
    85,
    'a1b2c3d4-0000-0000-0000-000000000001'
);
```

#### Directive 8: Debugging Hypothesis Before Fix (Superpowers systematic-debugging)

```sql
INSERT INTO directives (id, content, rationale, directive_type, source_skill, source_section, source_text_hash, context_triggers, verification_criteria, priority, decomposition_run_id) VALUES (
    'd1000000-0000-0000-0000-000000000008',
    'When debugging, form an explicit hypothesis about the root cause before attempting any fix. Write the hypothesis down. Do not try fixes speculatively.',
    'Agents guess at fixes, applying them one after another. This wastes tokens and context, and can introduce new bugs. Hypothesis-first debugging is more systematic and converges faster.',
    'behavioral',
    'superpowers',
    'systematic-debugging',
    'sha256:abc005',
    '{
        "activity_types": ["debugging"],
        "workflow_stages": ["mid-implementation", "post-implementation"],
        "agent_states": ["stuck"]
    }'::JSONB,
    'A written hypothesis existed before the first fix attempt.',
    85,
    'a1b2c3d4-0000-0000-0000-000000000001'
);
```

#### Directive 9: GSD Fresh Context Per Sub-Agent

```sql
INSERT INTO directives (id, content, rationale, directive_type, source_skill, source_section, source_text_hash, context_triggers, verification_criteria, priority, decomposition_run_id) VALUES (
    'd1000000-0000-0000-0000-000000000009',
    'When spawning sub-agents for task execution, give each sub-agent a fresh context. Do not carry accumulated conversation context from the orchestrator into the executor.',
    'Context rot degrades LLM output quality as the context window fills. Fresh contexts for execution agents ensure maximum output quality.',
    'procedural',
    'gsd',
    'sub-agent-execution',
    'sha256:abc006',
    '{
        "activity_types": ["implementation", "planning"],
        "workflow_stages": ["mid-implementation"],
        "conversation_topics": ["multi-agent", "sub-agent", "delegation"]
    }'::JSONB,
    'Sub-agents were spawned with fresh context windows, not appended to the orchestrators conversation.',
    80,
    'a1b2c3d4-0000-0000-0000-000000000002'
);
```

#### Directive 10: GSD Atomic Task Commits

```sql
INSERT INTO directives (id, content, rationale, directive_type, source_skill, source_section, source_text_hash, context_triggers, verification_criteria, priority, decomposition_run_id) VALUES (
    'd1000000-0000-0000-0000-000000000010',
    'Make one git commit per completed task. Each commit should represent a single, atomic, independently revertible unit of work.',
    'Fine-grained commits enable precise bisection when bugs are found later and allow independent reversion of individual changes.',
    'procedural',
    'gsd',
    'git-integration',
    'sha256:abc007',
    '{
        "activity_types": ["implementation"],
        "workflow_stages": ["mid-implementation", "post-implementation"],
        "project_signals": ["has_git"]
    }'::JSONB,
    'Each task produced exactly one commit. Commits are independently revertible.',
    70,
    'a1b2c3d4-0000-0000-0000-000000000002'
);
```

#### Directive 11: Allium Spec Authority Over Code

```sql
INSERT INTO directives (id, content, rationale, directive_type, source_skill, source_section, source_text_hash, context_triggers, verification_criteria, priority, decomposition_run_id) VALUES (
    'd1000000-0000-0000-0000-000000000011',
    'When a behavioral specification (.allium file) exists, treat it as the authoritative source of intended behavior. If code diverges from the spec, the code is wrong unless the spec is explicitly updated.',
    'Agents treat existing code as ground truth, but code contains bugs and expedient decisions. Specs capture intended behavior, providing a reference to distinguish bugs from features.',
    'guardrail',
    'allium',
    'specification-authority',
    'sha256:abc008',
    '{
        "activity_types": ["implementation", "debugging", "review"],
        "workflow_stages": ["mid-implementation", "post-implementation"],
        "codebase_patterns": ["*.allium"]
    }'::JSONB,
    'When spec and code diverged, agent treated the spec as authoritative and either fixed the code or explicitly updated the spec with rationale.',
    90,
    'a1b2c3d4-0000-0000-0000-000000000003'
);
```

#### Directive 12: Debugging Threshold Reset

```sql
INSERT INTO directives (id, content, rationale, directive_type, source_skill, source_section, source_text_hash, context_triggers, verification_criteria, priority, decomposition_run_id) VALUES (
    'd1000000-0000-0000-0000-000000000012',
    'If a debugging session exceeds 3 failed fix attempts, stop. Reformulate your root cause hypothesis from scratch. Consider whether your mental model of the system is wrong.',
    'Agents in fix loops apply increasingly desperate patches. After 3 failures, the hypothesis is likely wrong and continuing burns context budget without progress.',
    'guardrail',
    'superpowers',
    'systematic-debugging',
    'sha256:abc005',
    '{
        "activity_types": ["debugging"],
        "agent_states": ["stuck"],
        "complexity_threshold": "high"
    }'::JSONB,
    'After 3 failed fix attempts, agent stopped and reformulated hypothesis rather than trying a 4th speculative fix.',
    90,
    'a1b2c3d4-0000-0000-0000-000000000001'
);
```

### Relationships Between These Directives

```sql
-- Brainstorming chain: gate -> enumerate constraints -> generate alternatives -> evaluate tradeoffs
INSERT INTO directive_relationships (from_directive, to_directive, kind, strength, description) VALUES
    ('d1000000-0000-0000-0000-000000000001', 'd1000000-0000-0000-0000-000000000003', 'chains_to', 0.95, 'After activating brainstorming, first enumerate constraints.'),
    ('d1000000-0000-0000-0000-000000000003', 'd1000000-0000-0000-0000-000000000002', 'chains_to', 0.90, 'After enumerating constraints, generate multiple alternatives.'),
    ('d1000000-0000-0000-0000-000000000002', 'd1000000-0000-0000-0000-000000000004', 'chains_to', 0.90, 'After generating alternatives, evaluate tradeoffs.'),

-- Verification: implementation -> fresh verification
    ('d1000000-0000-0000-0000-000000000007', 'd1000000-0000-0000-0000-000000000006', 'chains_to', 0.85, 'After TDD implementation, verify with fresh test run.'),

-- Debugging chain: hypothesis first -> threshold reset if stuck
    ('d1000000-0000-0000-0000-000000000008', 'd1000000-0000-0000-0000-000000000012', 'chains_to', 0.80, 'If hypothesis-based debugging fails 3 times, trigger reset.'),

-- Cross-skill equivalences: GSD verification ~ Superpowers verification
-- (GSD verifier agent performs fresh verification = Superpowers verification-before-completion)
    ('d1000000-0000-0000-0000-000000000006', 'd1000000-0000-0000-0000-000000000010', 'equivalent_to', 0.70, 'Both require atomic verification of completed work, though GSD focuses on git commits and Superpowers on test execution.'),

-- Meta-directive: skill activation threshold applies to all others
    ('d1000000-0000-0000-0000-000000000005', 'd1000000-0000-0000-0000-000000000001', 'requires', 1.0, 'The 1% activation threshold is what makes brainstorming mandatory.'),
    ('d1000000-0000-0000-0000-000000000005', 'd1000000-0000-0000-0000-000000000006', 'requires', 1.0, 'The 1% activation threshold is what makes verification mandatory.'),
    ('d1000000-0000-0000-0000-000000000005', 'd1000000-0000-0000-0000-000000000008', 'requires', 1.0, 'The 1% activation threshold is what makes hypothesis-first debugging mandatory.');
```

---

## 6. The Recomposition Model

Given a query context -- "agent is brainstorming a new auth feature" -- here is how we query across all three databases and assemble a set of micro-prompt snippets.

### Step 1: Meilisearch -- Contextual Discovery

Find directives relevant to the current context using natural language search + filters.

```json
POST /indexes/directives/search
{
  "q": "brainstorming new authentication feature design approaches",
  "filter": "is_active = true AND (activity_types = 'brainstorming' OR activity_types = 'design' OR activity_types = 'architecture')",
  "sort": ["priority:desc", "effectiveness_score:desc"],
  "limit": 25,
  "showRankingScore": true,
  "hybrid": {
    "embedder": "default",
    "semanticRatio": 0.35
  },
  "facets": ["directive_type", "source_skill"]
}
```

**Expected results** (by ranking score):

| Rank | ID       | Content (truncated)                               | Score | Type       |
| ---- | -------- | ------------------------------------------------- | ----- | ---------- |
| 1    | d1...001 | Before any creative work, invoke brainstorming... | 0.97  | guardrail  |
| 2    | d1...005 | If there is even a 1% chance...                   | 0.95  | guardrail  |
| 3    | d1...003 | Enumerate all known constraints...                | 0.92  | procedural |
| 4    | d1...002 | Generate at least 3 meaningfully different...     | 0.90  | behavioral |
| 5    | d1...004 | Evaluate each against identified constraints...   | 0.88  | procedural |
| 6    | d1...011 | When a .allium spec exists, treat it as...        | 0.72  | guardrail  |

### Step 2: CockroachDB -- Precise Trigger Matching + Metadata

For the IDs returned by Meilisearch, fetch full records with feedback stats from the source of truth.

```sql
SELECT
    d.id,
    d.content,
    d.rationale,
    d.directive_type,
    d.source_skill,
    d.source_section,
    d.context_triggers,
    d.verification_criteria,
    d.effectiveness_score,
    d.priority,
    d.version,
    array_agg(DISTINCT dt.tag) FILTER (WHERE dt.tag IS NOT NULL) AS tags,
    count(DISTINCT df.id) FILTER (WHERE df.outcome IN ('followed', 'helpful')) AS positive_feedback,
    count(DISTINCT df.id) AS total_feedback
FROM directives d
LEFT JOIN directive_tags dt ON dt.directive_id = d.id
LEFT JOIN directive_feedback df ON df.directive_id = d.id
WHERE d.id = ANY($1::UUID[])   -- IDs from Meilisearch
AND d.is_active = true
GROUP BY d.id
ORDER BY d.priority DESC, d.effectiveness_score DESC;
```

Parameter `$1` = the array of UUIDs from Meilisearch results.

Also fetch any additional directives matched purely by structured trigger matching (the JSONB inverted index query catches directives whose triggers are very precise but whose content might not match the search text):

```sql
SELECT * FROM match_directives(
    p_activity_type := 'brainstorming',
    p_keywords := ARRAY['auth', 'authentication', 'feature'],
    p_workflow_stage := 'pre-implementation',
    p_limit := 10
);
```

Merge these results with the Meilisearch results, deduplicating by ID.

### Step 3: Gel DB -- Relationship Traversal

For the merged directive set, expand the graph: find chains, detect conflicts, and pull in required prerequisites.

```edgeql
WITH
    target_ids := <array<uuid>>$directive_crdb_ids
SELECT Directive {
    crdb_id,
    content,
    directive_type,
    priority,

    # What comes next in the chain?
    chains_to: {
        crdb_id,
        content,
        directive_type,
        priority,
        @strength,
    } ORDER BY @strength DESC,

    # Are there conflicts within our selected set?
    conflicts_with: {
        crdb_id,
        content,
        @strength,
        @description,
    } FILTER .crdb_id IN array_unpack(target_ids),

    # What prerequisites are we missing?
    requires: {
        crdb_id,
        content,
        directive_type,
        priority,
    } FILTER .crdb_id NOT IN array_unpack(target_ids),

    # Any equivalences from other skills worth surfacing?
    equivalent_to: {
        crdb_id,
        content,
        source_skill,
    } FILTER .crdb_id NOT IN array_unpack(target_ids)
    LIMIT 2
}
FILTER .crdb_id IN array_unpack(target_ids)
ORDER BY .priority DESC;
```

### Step 4: Assembly

The recomposition engine takes the combined results and assembles a micro-prompt payload:

1. **Conflict resolution** (see Section 7): If any `conflicts_with` links were found within the set, resolve them using priority, effectiveness, and specificity.

2. **Chain ordering**: Using `chains_to` relationships, order directives into a workflow sequence:

   - Brainstorming gate (d1...001)
   - Enumerate constraints (d1...003)
   - Generate alternatives (d1...002)
   - Evaluate tradeoffs (d1...004)
   - (then, post-brainstorm, the chain continues to planning/implementation)

3. **Prerequisite injection**: If `requires` returned directives not in the original set, add them with a note.

4. **Priority-weighted selection**: If the total token count exceeds the budget, drop lower-priority directives first. Guardrails are never dropped.

### Assembled Output

The final micro-prompt payload for "agent is brainstorming a new auth feature":

```markdown
## Active Directives

### Guardrails (always active)

- **[P99]** If there is even a 1% chance a directive applies, activate it. Never rationalize skipping.
- **[P95]** Before any creative work, invoke brainstorming. Never jump to implementation without brainstorming first.

### Brainstorming Workflow (sequential)

1. **[P75]** Enumerate all known constraints (technical, business, time, compatibility) before generating solutions.
2. **[P80]** Generate at least 3 meaningfully different approaches. Each must differ in fundamental strategy.
3. **[P70]** After generating alternatives, evaluate each against constraints. Produce a tradeoff comparison.

### Contextual (auth-specific)

- **[P90]** If .allium specs exist for auth behavior, treat them as authoritative. Code divergence = code is wrong.

### Upcoming (chain continues after brainstorming)

- Next: Create a written plan with phases before implementation.
- Then: Implement via TDD (write failing test first).
- Then: Verify freshly before claiming completion.
```

This payload is ~200 tokens -- compact enough to inject into any agent context without meaningful cost, yet precise enough to steer behavior through the entire brainstorming workflow.

---

## 7. Conflict Resolution

### Types of Conflict

1. **Direct contradiction**: Directive A says "always do X", Directive B says "never do X."
2. **Scope overlap**: Two directives apply to the same situation but prescribe different approaches.
3. **Priority inversion**: A low-priority directive from a trusted source conflicts with a high-priority directive from a less trusted source.
4. **Temporal conflict**: An older directive says one thing, a newer directive says another, but neither explicitly supersedes.

### Resolution Strategy

Conflicts are resolved by a deterministic scoring function that considers multiple factors:

```
resolution_score(directive) =
    (priority / 100) * 0.35
    + effectiveness_score * 0.25
    + specificity_score * 0.20
    + recency_score * 0.10
    + source_trust_score * 0.10
```

Where:

- **priority**: The directive's assigned priority (1-100, normalized to 0-1)
- **effectiveness_score**: Historical effectiveness from feedback (0-1)
- **specificity_score**: How specific the context triggers are (more trigger fields = more specific = higher score). Computed as `min(1.0, non_empty_trigger_fields / 5.0)`.
- **recency_score**: `1.0 / (1.0 + days_since_update / 365.0)` -- newer directives score slightly higher
- **source_trust_score**: Per-source weight. Default: superpowers=0.8, gsd=0.8, allium=0.9, custom=0.6, derived=0.5

### Resolution Rules

1. **Guardrails always win over non-guardrails.** If a guardrail conflicts with a behavioral or procedural directive, the guardrail takes precedence unconditionally.

2. **More specific wins over less specific.** A directive triggered by `{"activity_types": ["debugging"], "codebase_patterns": ["*_test.go"], "agent_states": ["stuck"]}` is more specific than one triggered by `{"activity_types": ["debugging"]}`. The more specific directive wins.

3. **If scores are within 10% of each other, surface both with a note.** The agent is informed of the conflict and given both options with their rationale. This prevents silent suppression of relevant guidance.

4. **Explicit supersession is absolute.** If directive B has `supersedes_id = A`, directive A is always suppressed when B is present. No scoring needed.

### Versioning and Deprecation

**Versioning model**: Each directive has a monotonically increasing `version` integer. When a directive is updated, its `version` increments and its `updated_at` changes. The old version is not preserved in the directives table -- it exists only in the CockroachDB audit log (changefeed to object storage) and as a git history entry if the decomposition input is tracked.

**Deprecation workflow**:

1. **Soft deprecation**: Set `is_active = false`. Directive stops appearing in searches and trigger matches. All relationships are preserved for historical analysis.

2. **Supersession**: Create a new directive with `supersedes_id` pointing to the old one. Set the old directive `is_active = false`. The new directive inherits the old one's relationships (the sync worker copies `chains_to`, `requires`, etc. from the superseded directive to the new one, unless explicitly overridden).

3. **Hard deletion**: Only for erroneous directives (duplicates, extraction errors). Cascades through `directive_tags`, `directive_feedback`, `directive_relationships`, and the Meilisearch index. The Gel DB record is also deleted.

### Conflict Detection Query

Run periodically or on directive insertion to detect new conflicts:

```sql
-- Find directive pairs that could conflict:
-- Same activity_type triggers, different instructions, high priority both
WITH active AS (
    SELECT id, content, context_triggers, priority, directive_type
    FROM directives
    WHERE is_active = true AND priority >= 50
)
SELECT
    a.id AS directive_a,
    b.id AS directive_b,
    a.content AS content_a,
    b.content AS content_b,
    a.priority AS priority_a,
    b.priority AS priority_b
FROM active a
JOIN active b ON a.id < b.id  -- avoid self-joins and duplicates
WHERE
    -- Same activity type overlap
    a.context_triggers->'activity_types' ?|
        (SELECT array_agg(value::TEXT) FROM jsonb_array_elements_text(b.context_triggers->'activity_types'))
    -- Same workflow stage overlap
    AND (
        NOT (a.context_triggers ? 'workflow_stages')
        OR NOT (b.context_triggers ? 'workflow_stages')
        OR a.context_triggers->'workflow_stages' ?|
            (SELECT array_agg(value::TEXT) FROM jsonb_array_elements_text(b.context_triggers->'workflow_stages'))
    )
    -- Not already in a known relationship
    AND NOT EXISTS (
        SELECT 1 FROM directive_relationships dr
        WHERE (dr.from_directive = a.id AND dr.to_directive = b.id)
           OR (dr.from_directive = b.id AND dr.to_directive = a.id)
    )
ORDER BY a.priority + b.priority DESC
LIMIT 50;
```

Candidate conflicts are reviewed by a human or a classification LLM, which creates `conflicts_with`, `alternative_to`, or `equivalent_to` relationships as appropriate.

### Cross-Database Consistency

The three databases must remain consistent:

| Event                 | CockroachDB                         | Meilisearch                            | Gel DB                           |
| --------------------- | ----------------------------------- | -------------------------------------- | -------------------------------- |
| Directive created     | INSERT (source of truth)            | Sync: upsert document                  | Sync: INSERT type + links        |
| Directive updated     | UPDATE                              | Sync: upsert document                  | Sync: UPDATE properties          |
| Directive deactivated | SET is_active=false                 | Sync: delete document                  | Sync: SET is_active=false        |
| Relationship created  | INSERT into directive_relationships | N/A (no relationships in Meili)        | Sync: add link                   |
| Feedback recorded     | INSERT into directive_feedback      | Sync: update effectiveness_score field | Sync: update effectiveness_score |

Sync is event-driven via CockroachDB changefeeds with an at-least-once delivery guarantee. Idempotent upserts in both Meilisearch and Gel DB handle duplicates. A nightly full-sync job detects and repairs any drift.

---

## Summary

The directive schema decomposes monolithic skill prompts into atomic behavioral instructions stored across three purpose-fit databases:

- **CockroachDB**: Transactional source of truth. Full ACID compliance, JSONB trigger matching, effectiveness tracking, conflict detection queries, changefeed for downstream sync.
- **Meilisearch**: Contextual discovery engine. Full-text + semantic search over directive content, typo-tolerant matching, faceted filtering by type/source/activity, ranking by effectiveness and priority.
- **Gel DB**: Relationship graph. First-class traversable links for directive chains, conflicts, equivalences, and refinements. Enables "give me the full workflow chain" queries that would require recursive CTEs in SQL.

The decomposition process extracts atomic directives from skill documents, classifies them, infers context triggers, and detects inter-directive relationships. The recomposition process queries all three databases in parallel, merges results, resolves conflicts, orders by workflow chain, and assembles a compact micro-prompt payload for injection into agent context.
