# Hive-Server Vision v4: The Behavioral Knowledge Engine

**Date:** 2026-03-09
**Status:** Architectural paradigm shift from CRUD API to prompt decomposition and contextual injection engine
**Supersedes:** vision-v3.md
**Core Insight:** Hive-server is not a CRUD API with 66 endpoints. It is a behavioral knowledge engine that decomposes skill prompts into atomic directives, stores them across purpose-built databases, and recomposes them contextually for injection into LLM agent conversations.

---

## 0. What Changed From v3

Vision v3 got the fundamental model wrong. It treated hive-server as a platform that replaces skills by reimplementing their APIs -- 66 CRUD endpoints across planning, orchestration, memory, specs, search, and analytics. That is a massive surface area that recreates the _structure_ of the skills without understanding their _purpose_.

The skills are not APIs. They are behavioral instruction sets. GSD's 12 agent personas, Superpowers' anti-rationalization design, Allium's Tend/Weed methodology -- these are prompt documents that teach LLMs how to think. The skill-replacement analysis (which v3 itself commissioned) proved that 60-85% of each skill's value is prompt engineering that no API can replicate. v3 then proposed to replicate it anyway, with 66 endpoints.

v4 recognizes what hive-server actually is:

**A prompt decomposition and contextual injection engine.**

It takes large behavioral documents (skills, methodologies, accumulated experience), breaks them into atomic directives, stores those directives in a knowledge base optimized for contextual retrieval, and returns micro-prompt snippets to the MCP plugin at the moment they are relevant -- not verbatim copies of skill documents, but contextualized behavioral nudges that fit the agent's current situation.

---

## 1. What Hive-Server Is

### 1.1 The Model

Hive-server is a **behavioral knowledge engine**. It has four jobs:

1. **Decompose**: Take large prompt documents (skill definitions, methodology guides, accumulated agent experience) and break them into atomic behavioral directives using an LLM.

2. **Store**: Persist those directives across three purpose-built databases, each serving a different access pattern -- transactional state (CockroachDB), contextual discovery (Meilisearch), and relationship traversal (Gel DB).

3. **Retrieve**: When an MCP plugin asks "what should I know right now?", find the relevant directives by matching the agent's current context against the directive knowledge base.

4. **Recompose**: Assemble the retrieved directives into a coherent, token-budgeted injection that fits the agent's situation -- not a dump of raw directives, but a synthesized behavioral context.

### 1.2 What This Is Not

This is **not** a CRUD API for skills. There is no `/api/v1/planning/create_project` endpoint. There is no `/api/v1/skills/discover` endpoint that returns skill metadata. There is no attempt to replicate GSD's wave scheduler or Superpowers' brainstorm-plan-execute pipeline as server-side state machines.

The MCP plugin has one primary interaction: ask for behavioral context, receive behavioral context.

The skills (GSD, Superpowers, Allium) are **training data** for the knowledge base. They are ingested, decomposed, and their behavioral wisdom is extracted into atomic directives. The skills themselves are no longer loaded, executed, or maintained. Their knowledge lives in the engine.

### 1.3 Why This Model Is Right

The skill-replacement analysis proved that the irreducible core of every skill is prompt engineering -- behavioral instructions that guide LLM reasoning. No API replaces "when debugging, investigate the root cause before attempting a fix." But the analysis also proved that these instructions are static, monolithic, and context-blind:

- All 14 Superpowers skills load into every session, burning tokens regardless of relevance.
- GSD's 12 agent personas are fixed markdown documents that cannot adapt to what the agent has learned.
- Allium's Tend/Weed agents have no memory of past drift patterns.
- None of them learn. None of them adapt. None of them know what worked before.

The behavioral knowledge engine solves this. Instead of dumping 14 skill documents into context, it injects 5-10 precisely targeted directives that are relevant to what the agent is doing _right now_, informed by what has worked _before_, and adapted to the agent's specific patterns.

---

## 2. The Directive: The Atomic Unit

### 2.1 What Is a Directive

A **directive** is the atomic unit of behavioral knowledge. It is a single, actionable instruction that an LLM can follow. It is not a summary. It is not a description. It is a behavioral nudge.

Examples:

```
"Before proposing a fix for this bug, reproduce it with a minimal test case first. State what you expect vs. what happens."
```

```
"The user tends to approve plans quickly without reviewing edge cases. Before presenting a plan, explicitly list 2-3 edge cases and ask if they matter."
```

```
"This codebase uses chi v5 for routing. When adding new endpoints, follow the pattern in internal/handlers/handlers.go: group routes under r.Route(), apply middleware at the group level."
```

```
"There are 2 open requirements related to authentication (AUTH-01, AUTH-03). Consider whether this work addresses either of them."
```

```
"In 3 of the last 5 debugging sessions, the root cause was a goroutine race condition. When you see intermittent test failures, check for races first."
```

### 2.2 Directive Schema (CockroachDB)

```sql
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
    trigger_phase   TEXT        NOT NULL DEFAULT '',              -- Workflow phase: "planning", "implementation", "debugging", "review"
    trigger_scope   TEXT        NOT NULL DEFAULT '',              -- Scope: "global", "repo:christmas-island/hive-server", "project:proj_abc"

    -- Effectiveness: does this directive actually help?
    times_injected  INT8        NOT NULL DEFAULT 0,     -- How many times this was sent to an agent
    times_followed  INT8        NOT NULL DEFAULT 0,     -- How many times the agent demonstrably followed it
    times_ignored   INT8        NOT NULL DEFAULT 0,     -- How many times the agent ignored it
    times_negative  INT8        NOT NULL DEFAULT 0,     -- How many times following it led to a worse outcome
    effectiveness   FLOAT8      NOT NULL DEFAULT 0.0,   -- Computed: (followed - negative) / injected, or 0 if never injected

    -- Relationships (denormalized for query performance; Gel has the full graph)
    related_ids     JSONB       NOT NULL DEFAULT '[]'::JSONB,    -- UUIDs of related directives
    supersedes_id   UUID,                                         -- If this directive replaces an older one
    chain_id        UUID,                                         -- Group of directives that form a behavioral chain

    -- Metadata
    weight          FLOAT8      NOT NULL DEFAULT 1.0,   -- Priority weight for injection ranking
    token_cost      INT4        NOT NULL DEFAULT 0,     -- Estimated tokens when rendered
    active          BOOLEAN     NOT NULL DEFAULT true,   -- Soft delete / disable
    tenant_id       UUID        NOT NULL,                -- Multi-tenancy
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Indexes for the injection pipeline
CREATE INDEX idx_directives_active ON directives (active, tenant_id) WHERE active = true;
CREATE INDEX idx_directives_phase ON directives (trigger_phase, tenant_id) WHERE active = true;
CREATE INDEX idx_directives_scope ON directives (trigger_scope, tenant_id) WHERE active = true;
CREATE INDEX idx_directives_effectiveness ON directives (effectiveness DESC, tenant_id) WHERE active = true;
CREATE INVERTED INDEX idx_directives_tags ON directives (trigger_tags);
```

### 2.3 Directive Kinds

| Kind         | What It Is                     | Example                                          | Source               |
| ------------ | ------------------------------ | ------------------------------------------------ | -------------------- |
| `behavioral` | How to approach a type of work | "When debugging, reproduce before fixing"        | Skill decomposition  |
| `pattern`    | Codebase-specific conventions  | "This repo uses chi v5 with grouped routes"      | Codebase observation |
| `contextual` | Situation-specific awareness   | "There are 2 open AUTH requirements"             | State queries        |
| `corrective` | Learned from past mistakes     | "In 3/5 debug sessions, root cause was a race"   | Experience feedback  |
| `factual`    | Things the agent should know   | "The user prefers dark mode and terse responses" | User memory          |

### 2.4 Directive Lifecycle

```
                 +-----------+
                 |  Created  |
                 +-----+-----+
                       |
              (decomposition or feedback)
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

## 3. The Decomposition Pipeline

### 3.1 Overview

When a skill prompt is ingested (e.g., the entire `superpowers:systematic-debugging` SKILL.md), hive-server runs it through a decomposition pipeline that extracts atomic directives.

```
                   +------------------+
                   |  Skill Document  |
                   |  (raw markdown)  |
                   +--------+---------+
                            |
                   +--------v---------+
                   |   Sectioning     |
                   |  Split into      |
                   |  logical blocks  |
                   +--------+---------+
                            |
                   +--------v---------+
                   |   LLM Analysis   |
                   |  Extract atomic  |
                   |  directives      |
                   +--------+---------+
                            |
                   +--------v---------+
                   |   Enrichment     |
                   |  Add triggers,   |
                   |  tags, metadata  |
                   +--------+---------+
                            |
                   +--------v---------+
                   |   Deduplication  |
                   |  Merge with      |
                   |  existing base   |
                   +--------+---------+
                            |
                   +--------v---------+
                   |   Storage        |
                   |  CRDB + Meili    |
                   |  + Gel           |
                   +------------------+
```

### 3.2 Sectioning

The raw document is split into logical blocks. For a skill like `superpowers:systematic-debugging`, this might produce:

1. Activation conditions (the "Use when..." frontmatter)
2. Core methodology steps (reproduce, isolate, hypothesize, test, fix)
3. Anti-patterns / things to avoid
4. Examples of good vs. bad behavior
5. Integration notes (how this skill connects to TDD, verification, etc.)

Sectioning is deterministic -- it uses heading structure, numbered lists, and markdown semantics. No LLM needed for this step.

### 3.3 LLM Analysis

Each section is sent to an LLM with a decomposition prompt:

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
- trigger_phase: which workflow phase (planning, implementation, debugging, review, any)
- kind: behavioral, pattern, contextual, corrective, or factual
- related_to: which other directives in this batch it connects to (by index)

Section from "systematic-debugging" skill:
---
{section_content}
---

Respond with a JSON array of directives.
```

### 3.4 Example: Decomposing systematic-debugging

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

### 3.5 Enrichment

After LLM extraction, directives are enriched with:

- **Token cost estimation**: Run the content through a tokenizer to get actual token count.
- **Cross-references**: If a directive from `systematic-debugging` mentions "write a test first," link it to the TDD skill's directives about red-green-refactor.
- **Scope assignment**: Some directives are universal ("reproduce before fixing"). Some are repo-specific ("this repo uses chi v5"). Scope is inferred from the source and content.
- **Weight assignment**: Anti-rationalization directives (from Superpowers' "1% rule" philosophy) get higher initial weight because they counter known LLM failure modes.

### 3.6 Deduplication

Multiple skills often teach the same lesson:

- GSD's `gsd-verifier` agent says "verify outcomes match acceptance criteria."
- Superpowers' `verification-before-completion` skill says "run the command freshly, read full output."
- These are the same behavioral directive expressed differently.

The deduplication step uses semantic similarity (via Meilisearch's hybrid search or a dedicated embedding) to find near-duplicates. When duplicates are found:

- The most specific, actionable version is kept.
- The others are merged (their provenance is preserved -- the directive knows it came from multiple sources).
- A directive with multiple independent sources gets a higher initial weight (multiple skills independently concluded this behavior matters).

---

## 4. The Injection Pipeline

### 4.1 Overview

When the MCP plugin calls hive-server, it sends a context frame describing what the agent is currently doing. Hive-server returns a set of directives appropriate for that context, within a token budget.

```
MCP Plugin                          Hive-Server
    |                                    |
    |  POST /api/v1/inject               |
    |  {                                 |
    |    agent_id: "agent-007",          |
    |    session_id: "sess_xyz",         |
    |    context: {                      |
    |      intent: "debugging a          |
    |        failing test",              |
    |      files: ["internal/store/      |
    |        memory_test.go"],           |
    |      repo: "hive-server",          |
    |      phase: "debugging",           |
    |      recent_actions: [             |
    |        "read test file",           |
    |        "ran go test -run           |
    |          TestUpsert"               |
    |      ],                            |
    |      conversation_summary:         |
    |        "Agent is investigating     |
    |         a test failure in the      |
    |         memory store upsert        |
    |         logic"                     |
    |    },                              |
    |    token_budget: 500               |
    |  }                                 |
    |                                    |
    |                                    |  1. Query Meilisearch for directives
    |                                    |     matching "debugging failing test
    |                                    |     memory store upsert"
    |                                    |
    |                                    |  2. Query CRDB for directives with
    |                                    |     trigger_phase = "debugging" and
    |                                    |     trigger_scope matching repo
    |                                    |
    |                                    |  3. Query Gel for directives connected
    |                                    |     to the debugging behavioral chain
    |                                    |
    |                                    |  4. Rank by relevance * effectiveness
    |                                    |     * weight
    |                                    |
    |                                    |  5. Select top N within token budget
    |                                    |
    |                                    |  6. Contextualize: adapt directive
    |                                    |     language to current situation
    |                                    |
    |  <--- Response -----------------   |
    |  {                                 |
    |    directives: [...],              |
    |    injection_id: "inj_abc",        |
    |    tokens_used: 487                |
    |  }                                 |
    |                                    |
```

### 4.2 Context Frame

The MCP plugin sends a **context frame** with each injection request:

```json
{
  "agent_id": "agent-007",
  "session_id": "sess_xyz",
  "context": {
    "intent": "debugging a failing test in the memory store",
    "files": ["internal/store/memory_test.go", "internal/store/memory.go"],
    "repo": "christmas-island/hive-server",
    "phase": "debugging",
    "recent_actions": [
      "read internal/store/memory_test.go",
      "ran go test -run TestUpsertMemory",
      "observed: FAIL - expected version 2, got version 1"
    ],
    "conversation_summary": "Agent is investigating why optimistic concurrency check fails on upsert. The test creates a memory entry, updates it, and checks the version is incremented. The version stays at 1.",
    "open_requirements": ["AUTH-01", "AUTH-03"],
    "current_project": "proj_abc123"
  },
  "token_budget": 500,
  "previous_injection_id": "inj_prev123"
}
```

### 4.3 Retrieval Strategy

The injection pipeline queries all three databases in parallel:

**Meilisearch (semantic discovery):**
Search the `directives` index with the conversation summary and intent as the query. This finds directives that are semantically related to what the agent is doing, even if the exact tags do not match. Meilisearch's typo tolerance and relevance ranking handle the fuzzy matching.

```
Query: "debugging optimistic concurrency version check fails upsert"
Returns: directives about debugging, test-first debugging, concurrency issues, database testing
```

**CockroachDB (structured filtering):**
Query for directives matching the explicit context:

```sql
SELECT id, content, kind, weight, effectiveness, token_cost
FROM directives
WHERE active = true
  AND tenant_id = $1
  AND (trigger_phase = $2 OR trigger_phase = 'any')
  AND (trigger_scope = 'global'
       OR trigger_scope = $3          -- repo scope
       OR trigger_scope = $4)         -- project scope
ORDER BY effectiveness DESC, weight DESC
LIMIT 50;
```

**Gel DB (relationship traversal):**
Starting from the directives found by Meilisearch and CRDB, traverse the behavioral chain graph to find related directives that form a coherent sequence:

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

### 4.4 Ranking and Selection

The three result sets are merged and ranked:

```
score = (meilisearch_relevance * 0.4)
      + (effectiveness * 0.3)
      + (weight * 0.2)
      + (recency_bonus * 0.1)
```

Where:

- `meilisearch_relevance`: 0.0-1.0, how semantically relevant to the current context
- `effectiveness`: 0.0-1.0, historical success rate when this directive was followed
- `weight`: 0.0-2.0, priority weight (anti-rationalization directives are higher)
- `recency_bonus`: 0.0-0.5, bonus for directives from the agent's recent experience with this repo

### 4.5 Token Budgeting

The agent requests a token budget (default: 500 tokens). The selection algorithm:

1. Sort candidates by score descending.
2. Greedily add directives until the budget is exhausted.
3. If a directive would exceed the budget, try the next one (smaller might fit).
4. Reserve 50 tokens for the injection frame (the wrapper that presents the directives to the agent).

A 500-token budget typically fits 8-12 directives. A 200-token budget fits 3-5.

### 4.6 Contextualization

Raw directives are generic. The injection pipeline contextualizes them to the agent's current situation before returning:

**Raw directive:**

```
"When you encounter a bug, STOP. Do not attempt any fix yet. Your first action must be to reproduce the problem reliably."
```

**Contextualized for the current situation:**

```
"You are debugging a version mismatch in TestUpsertMemory. Before changing any code, write a focused assertion that demonstrates exactly when the version fails to increment. This is your reproduction case."
```

Contextualization is done by a fast LLM call (Haiku-class) that takes the raw directive, the context frame, and produces a version specific to the current situation. If the LLM call fails or times out (50ms budget), the raw directive is returned as-is.

### 4.7 Injection Response

```json
{
  "injection_id": "inj_abc123",
  "directives": [
    {
      "id": "dir_001",
      "content": "You are debugging a version mismatch in TestUpsertMemory. Before changing any code, write a focused assertion that demonstrates exactly when the version fails to increment.",
      "kind": "behavioral",
      "source": "superpowers:systematic-debugging",
      "confidence": 0.94
    },
    {
      "id": "dir_002",
      "content": "In this codebase, optimistic concurrency uses a version column incremented by the UPDATE statement itself (SET version = version + 1). Check whether the test is reading the version before or after the UPDATE completes.",
      "kind": "pattern",
      "source": "codebase:hive-server/store",
      "confidence": 0.88
    },
    {
      "id": "dir_003",
      "content": "In 2 of the last 3 debugging sessions in this repo, the root cause was a transaction isolation issue -- a read happening outside the transaction boundary. Check whether the version read is inside or outside the crdbpgx.ExecuteTx() closure.",
      "kind": "corrective",
      "source": "experience:sess_prev1,sess_prev2",
      "confidence": 0.82
    },
    {
      "id": "dir_004",
      "content": "Do not make multiple changes at once. Change one thing in the upsert logic, run TestUpsertMemory, observe. If you change three things and the test passes, you will not know which change fixed it.",
      "kind": "corrective",
      "source": "superpowers:systematic-debugging",
      "confidence": 0.79
    },
    {
      "id": "dir_005",
      "content": "This work may address requirement AUTH-01 (user signup with email/password). If the upsert fix affects the authentication flow, note the connection.",
      "kind": "contextual",
      "source": "project:proj_abc123/requirements",
      "confidence": 0.45
    }
  ],
  "tokens_used": 487,
  "token_budget": 500,
  "candidates_considered": 38,
  "candidates_selected": 5,
  "context_hash": "sha256:abc123..."
}
```

### 4.8 Avoiding Overwhelm

The injection pipeline has several mechanisms to avoid overwhelming the agent:

1. **Token budget**: Hard cap on injection size. The MCP plugin decides how much context to spend.
2. **Diminishing returns**: If the top-scored directive has confidence 0.94 and the 8th has 0.31, the pipeline stops at the natural dropoff point rather than filling the budget with low-confidence noise.
3. **Session deduplication**: The `previous_injection_id` field lets the pipeline avoid re-injecting directives from the last call. If the agent is still debugging the same thing, it gets new directives, not the same ones repeated.
4. **Phase gating**: Only directives matching the current phase are candidates. If the agent is debugging, it does not receive planning directives.
5. **Cooldown**: A directive that was injected and ignored in the last 3 calls gets a temporary weight reduction. The agent clearly does not need it right now.

---

## 5. Database Roles Reimagined

### 5.1 CockroachDB: Directive State and Agent State

CockroachDB is the transactional source of truth. It stores:

**Directives table** (see schema in Section 2.2): The full directive catalog with effectiveness metrics, provenance, and trigger metadata.

**Agent state:**

```sql
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

CREATE TABLE injections (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID        NOT NULL REFERENCES agent_sessions(id),
    context_hash    TEXT        NOT NULL,       -- Hash of the context frame for dedup
    directives      JSONB       NOT NULL,       -- Array of {directive_id, confidence} pairs
    tokens_used     INT4        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE injection_outcomes (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    injection_id    UUID        NOT NULL REFERENCES injections(id),
    directive_id    UUID        NOT NULL REFERENCES directives(id),
    outcome         TEXT        NOT NULL,       -- 'followed', 'ignored', 'negative'
    evidence        TEXT        NOT NULL DEFAULT '',  -- What happened
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

**Ingestion tracking:**

```sql
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
```

### 5.2 Meilisearch: Contextual Discovery

Meilisearch indexes directives for semantic retrieval. Its job is answering: "Given what the agent is doing right now, which directives are relevant?"

**Index: `directives`**

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

**Why Meilisearch, not vector search alone:**

The agent's context description uses natural language. Meilisearch's typo-tolerant, relevance-ranked full-text search handles the fuzzy matching between "I'm debugging a failing test" and directives tagged with "test-failure" or "unexpected-behavior." Meilisearch's hybrid search (keyword + semantic) provides both exact-match precision and semantic recall.

The 10-word query limit is managed by extracting key terms from the context frame before searching. The conversation summary is not sent raw -- it is preprocessed into search-optimized terms.

### 5.3 Gel DB: Behavioral Chains and Relationships

Gel models the relationships between directives that the flat CRDB table and keyword-based Meilisearch cannot express.

**Schema:**

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

**What Gel answers:**

1. "Give me the next step in this debugging chain": The agent has followed the "reproduce" directive. Gel traverses the chain to find the next directive in sequence ("form a hypothesis").

2. "What directives are related to this one?": An agent receives a directive about TDD. Gel traverses the `related_to` links to find the verification directive, the anti-pattern directive about skipping tests, and the debugging directive about test-first diagnosis.

3. "Which sources produce the most effective directives?": Gel computes aggregate effectiveness per source, revealing which skill documents are contributing the most value.

4. "What is the full behavioral chain for planning?": Gel returns the complete planning chain -- brainstorm, evaluate options, select approach, decompose into tasks, validate against requirements, define verification criteria -- as an ordered sequence.

### 5.4 Sync Between Databases

```
Write path (directive creation):
    1. Insert into CockroachDB (synchronous, source of truth)
    2. Index in Meilisearch (async worker, eventual consistency)
    3. Create/update in Gel (async worker, eventual consistency)

Read path (injection):
    1. Meilisearch: semantic search for candidates (fast, <50ms)
    2. CRDB: structured filter for candidates (fast, <10ms)
    3. Gel: chain traversal for related directives (fast, <20ms)
    4. Merge, rank, select, contextualize (hive-server compute, <100ms)
    Total target: <200ms for an injection request

Reconciliation:
    - Every 5 minutes: compare CRDB directive timestamps against Meilisearch/Gel
    - Re-sync any drifted records
    - Full rebuild available via admin endpoint
```

---

## 6. The Feedback Loop

### 6.1 How Feedback Is Captured

The MCP plugin reports outcomes back to hive-server after each significant agent action:

```json
POST /api/v1/feedback
{
  "injection_id": "inj_abc123",
  "outcomes": [
    {
      "directive_id": "dir_001",
      "outcome": "followed",
      "evidence": "Agent wrote a focused test assertion for version increment before modifying any code"
    },
    {
      "directive_id": "dir_002",
      "outcome": "followed",
      "evidence": "Agent checked the UPDATE statement and found the version increment was correct"
    },
    {
      "directive_id": "dir_003",
      "outcome": "followed",
      "evidence": "Agent found the read was outside the transaction -- this was the root cause. Directive was correct."
    },
    {
      "directive_id": "dir_004",
      "outcome": "ignored",
      "evidence": "Agent made two changes simultaneously (moved read inside tx AND added explicit version check)"
    },
    {
      "directive_id": "dir_005",
      "outcome": "ignored",
      "evidence": "Agent did not reference AUTH-01 -- not relevant to this debugging session"
    }
  ],
  "session_outcome": "success",
  "session_summary": "Fixed version check bug. Root cause was reading version outside transaction boundary."
}
```

### 6.2 How Feedback Updates Directives

When outcomes are recorded:

```sql
-- For each 'followed' outcome
UPDATE directives
SET times_injected = times_injected + 1,
    times_followed = times_followed + 1,
    effectiveness = (times_followed::FLOAT8 - times_negative::FLOAT8) / GREATEST(times_injected::FLOAT8, 1),
    updated_at = now()
WHERE id = $1;

-- For each 'ignored' outcome
UPDATE directives
SET times_injected = times_injected + 1,
    times_ignored = times_ignored + 1,
    effectiveness = (times_followed::FLOAT8 - times_negative::FLOAT8) / GREATEST(times_injected::FLOAT8, 1),
    updated_at = now()
WHERE id = $1;

-- For each 'negative' outcome (directive was followed but made things worse)
UPDATE directives
SET times_injected = times_injected + 1,
    times_negative = times_negative + 1,
    effectiveness = (times_followed::FLOAT8 - times_negative::FLOAT8) / GREATEST(times_injected::FLOAT8, 1),
    weight = GREATEST(weight * 0.8, 0.1),  -- Reduce weight by 20%, floor at 0.1
    updated_at = now()
WHERE id = $1;
```

### 6.3 Experience-Derived Directives

When a session ends, hive-server analyzes the session outcomes to create new experience-derived directives:

```json
POST /api/v1/feedback/session-complete
{
  "session_id": "sess_xyz",
  "summary": "Debugged version check. Root cause: reading version outside ExecuteTx closure.",
  "repo": "christmas-island/hive-server",
  "outcome": "success",
  "key_insight": "CockroachDB transaction isolation means reads inside ExecuteTx see the transaction's snapshot. Reads outside see the committed state, which may be stale."
}
```

This can produce a new directive:

```json
{
  "content": "In this codebase, ensure all reads that inform writes happen inside the crdbpgx.ExecuteTx() closure. Reading outside the closure and writing inside creates stale-read bugs because CockroachDB's serializable isolation only protects reads within the transaction boundary.",
  "kind": "corrective",
  "source_type": "experience",
  "source_name": "session:sess_xyz",
  "trigger_tags": [
    "cockroachdb",
    "transaction",
    "stale-read",
    "ExecuteTx",
    "isolation"
  ],
  "trigger_intent": "Agent is writing or modifying CockroachDB transaction code",
  "trigger_scope": "repo:christmas-island/hive-server",
  "weight": 1.2
}
```

Experience-derived directives start with a moderate weight (1.0-1.2) and earn their effectiveness score through future injections and outcomes. Over time, the most useful experience-derived directives outrank the original skill-derived directives -- the system learns what actually works in practice, not just what the skill documents prescribe.

### 6.4 Directive Evolution

Over time, the directive population evolves:

- **Effective directives** gain weight and are injected more often.
- **Ignored directives** lose weight. After being ignored in 10 consecutive injections, they are flagged for review.
- **Negative directives** lose weight rapidly. After 3 negative outcomes, they are auto-deprecated (active=false) and flagged for human review.
- **Experience directives** accumulate as agents work. A repo that has been worked on for 6 months has dozens of repo-specific corrective directives learned from actual debugging sessions.
- **Supersession**: When a new directive contradicts or improves on an older one, the old one is marked as superseded. The new one inherits the old one's injection history for continuity.

---

## 7. Concrete Scenarios

### 7.1 Scenario: Agent Starts a Brainstorming Session

**Setup:** A user says "Let's brainstorm how to add rate limiting to the API." The MCP plugin detects the brainstorming intent and calls hive-server.

**Injection request:**

```json
{
  "agent_id": "agent-007",
  "session_id": "sess_new",
  "context": {
    "intent": "brainstorming rate limiting approaches",
    "repo": "christmas-island/hive-server",
    "phase": "planning",
    "recent_actions": ["user said 'let's brainstorm rate limiting'"],
    "conversation_summary": "User wants to add rate limiting to the API. No approach has been discussed yet."
  },
  "token_budget": 400
}
```

**Injection response:**

```json
{
  "injection_id": "inj_bs_001",
  "directives": [
    {
      "id": "dir_bs_01",
      "content": "The user is beginning a brainstorming session. Before converging on any solution, generate at least 3 distinct approaches. For each approach, state: what it is, one advantage, one disadvantage, and an effort estimate. Present them as a numbered list, then ask the user which direction interests them.",
      "kind": "behavioral",
      "source": "superpowers:brainstorming",
      "confidence": 0.97
    },
    {
      "id": "dir_bs_02",
      "content": "Ask about edge cases before the user commits to a solution. For rate limiting specifically: What happens when the limit is hit? Per-user or per-IP? Should different endpoints have different limits? Should authenticated users get higher limits?",
      "kind": "behavioral",
      "source": "superpowers:brainstorming + experience:rate-limiting-patterns",
      "confidence": 0.91
    },
    {
      "id": "dir_bs_03",
      "content": "This codebase uses chi v5 middleware for cross-cutting concerns (see internal/handlers/handlers.go). Rate limiting should be implemented as chi middleware, not per-handler logic. Check if golang.org/x/time/rate is already a dependency.",
      "kind": "pattern",
      "source": "codebase:hive-server/handlers",
      "confidence": 0.88
    },
    {
      "id": "dir_bs_04",
      "content": "In a previous project (proj_widget_api), rate limiting was implemented with a token bucket. The main lesson learned was: store rate limit state in the database (not in-memory) because multiple hive-server instances share load. CockroachDB is already the backend here.",
      "kind": "corrective",
      "source": "experience:proj_widget_api",
      "confidence": 0.73
    },
    {
      "id": "dir_bs_05",
      "content": "There is an open requirement SEC-02 ('API rate limiting for abuse prevention') in the current project. This brainstorming session likely addresses it. Reference SEC-02 when the approach is finalized.",
      "kind": "contextual",
      "source": "project:proj_abc123/requirements",
      "confidence": 0.85
    }
  ],
  "tokens_used": 389,
  "candidates_considered": 42
}
```

**What the agent does with this:** The MCP plugin injects these directives into the agent's system prompt or as a tool-use response. The agent now knows to:

1. Generate multiple approaches before converging (from the brainstorming skill)
2. Probe specific edge cases relevant to rate limiting (from experience)
3. Use chi middleware as the implementation pattern (from codebase knowledge)
4. Consider distributed state because of multi-instance deployment (from past project experience)
5. Link back to the SEC-02 requirement (from project context)

None of this was possible with static skill loading. Superpowers' brainstorming skill would have given the agent a generic brainstorming methodology. Hive-server gives it a methodology contextualized to _this repo, this project, this user's past decisions_.

### 7.2 Scenario: Agent Is Debugging a Failing Test

**Setup:** The agent ran `go test ./internal/store/...` and got a failure. It calls the MCP plugin for guidance.

**Injection request:**

```json
{
  "agent_id": "agent-007",
  "session_id": "sess_debug",
  "context": {
    "intent": "debugging a test failure",
    "files": ["internal/store/memory_test.go", "internal/store/memory.go"],
    "repo": "christmas-island/hive-server",
    "phase": "debugging",
    "recent_actions": [
      "ran go test ./internal/store/...",
      "observed: FAIL TestUpsertMemory/concurrent_updates - race detected"
    ],
    "conversation_summary": "Test detects a race condition in concurrent upsert operations. The race detector flagged simultaneous writes to a shared variable."
  },
  "token_budget": 500
}
```

**Injection response:**

```json
{
  "injection_id": "inj_dbg_001",
  "directives": [
    {
      "id": "dir_dbg_01",
      "content": "STOP. You have a race condition detected by Go's race detector. Do not attempt to fix it yet. First, run the test with -count=10 to see if it fails consistently or intermittently. Then, read the race detector output carefully -- it tells you exactly which two goroutines are conflicting and on which variable.",
      "kind": "behavioral",
      "source": "superpowers:systematic-debugging",
      "confidence": 0.96
    },
    {
      "id": "dir_dbg_02",
      "content": "In 3 of the last 4 race conditions in this codebase, the root cause was a shared *testing.T or shared test fixture being accessed by parallel subtests without t.Parallel() isolation or proper mutex protection. Check if TestUpsertMemory uses t.Run() with shared state.",
      "kind": "corrective",
      "source": "experience:sess_race1,sess_race2,sess_race3",
      "confidence": 0.89
    },
    {
      "id": "dir_dbg_03",
      "content": "The store layer in this codebase uses a single SQLite connection with MaxOpenConns=1 (see internal/store/store.go). Concurrent test access to the store requires either serialized access or separate store instances per subtest. This is a known architectural constraint.",
      "kind": "pattern",
      "source": "codebase:hive-server/store",
      "confidence": 0.87
    },
    {
      "id": "dir_dbg_04",
      "content": "Before applying a fix, write a test that reliably triggers the race. Use -race flag and -count=100. If you cannot reproduce it reliably, the fix cannot be verified.",
      "kind": "behavioral",
      "source": "superpowers:systematic-debugging + superpowers:test-driven-development",
      "confidence": 0.84
    },
    {
      "id": "dir_dbg_05",
      "content": "When you fix the race, change ONE thing. Do not simultaneously refactor the test structure and fix the concurrency bug. Fix the race, verify it passes with -race -count=100, then refactor if needed.",
      "kind": "corrective",
      "source": "superpowers:systematic-debugging",
      "confidence": 0.78
    }
  ],
  "tokens_used": 478,
  "candidates_considered": 51
}
```

**What makes this powerful:** The agent gets the systematic debugging methodology (from Superpowers), but it also gets repo-specific knowledge (SQLite single-connection constraint, historical race condition patterns in this codebase). A static skill would say "investigate root cause." Hive-server says "in this repo, 3/4 race conditions were caused by shared test fixtures in parallel subtests."

### 7.3 Scenario: Agent Is Planning a Multi-Step Feature

**Setup:** The agent is planning a new authentication system across multiple phases. This is a planning session, not implementation.

**Injection request:**

```json
{
  "agent_id": "agent-007",
  "session_id": "sess_plan",
  "context": {
    "intent": "planning multi-step authentication feature",
    "repo": "christmas-island/hive-server",
    "phase": "planning",
    "recent_actions": [
      "user described requirement: JWT auth with refresh tokens",
      "agent is about to create a phase decomposition"
    ],
    "conversation_summary": "User wants to add JWT authentication with refresh tokens, role-based access control, and API key support. This spans multiple implementation phases.",
    "open_requirements": ["AUTH-01", "AUTH-02", "AUTH-03", "AUTH-04"],
    "current_project": "proj_abc123"
  },
  "token_budget": 600
}
```

**Injection response:**

```json
{
  "injection_id": "inj_plan_001",
  "directives": [
    {
      "id": "dir_plan_01",
      "content": "Before decomposing into phases, verify you understand the full scope. Four requirements are open (AUTH-01 through AUTH-04). List each one with your interpretation, and ask the user to confirm or correct. Do not plan against assumptions.",
      "kind": "behavioral",
      "source": "gsd:planning-methodology + allium:elicitation",
      "confidence": 0.95
    },
    {
      "id": "dir_plan_02",
      "content": "Decompose the work into phases where each phase produces a working, testable increment. Phase 1 should be the simplest useful auth (e.g., basic JWT with a single role). Each subsequent phase adds a capability (refresh tokens, RBAC, API keys). Do not create a phase that only adds 'scaffolding' with no user-visible behavior.",
      "kind": "behavioral",
      "source": "gsd:roadmapping + superpowers:writing-plans",
      "confidence": 0.92
    },
    {
      "id": "dir_plan_03",
      "content": "For each phase, define 2-3 acceptance criteria that can be verified with a curl command or test. Example: 'POST /api/auth/login with valid credentials returns 200 and a JWT.' If you cannot state the acceptance criteria as a concrete, runnable check, the phase is too vague.",
      "kind": "behavioral",
      "source": "gsd:nyquist-verification + superpowers:verification-before-completion",
      "confidence": 0.9
    },
    {
      "id": "dir_plan_04",
      "content": "This codebase already has auth middleware in internal/handlers/handlers.go that checks a Bearer token against HIVE_TOKEN env var. Your JWT implementation must replace this, not layer on top of it. Plan for a migration path: existing token auth keeps working during Phase 1 implementation.",
      "kind": "pattern",
      "source": "codebase:hive-server/handlers",
      "confidence": 0.88
    },
    {
      "id": "dir_plan_05",
      "content": "In a previous project (proj_widget_api), the auth implementation started with JWT but had to be reworked when RBAC was added because the initial token structure did not include role claims. Plan the JWT token structure in Phase 1 to include role claims even if RBAC is not implemented until Phase 3. This avoids a migration.",
      "kind": "corrective",
      "source": "experience:proj_widget_api/auth-rework",
      "confidence": 0.83
    },
    {
      "id": "dir_plan_06",
      "content": "The user tends to approve plans quickly. Before presenting the phase decomposition, explicitly list 2-3 risks or open questions: How will existing API clients migrate? What is the token expiry strategy? Will refresh tokens be stored server-side or stateless?",
      "kind": "corrective",
      "source": "observation:user-pattern-quick-approval",
      "confidence": 0.76
    },
    {
      "id": "dir_plan_07",
      "content": "When creating task breakdowns within each phase, keep tasks to 2-3 per plan. Each task must specify: files to modify, implementation action, verification command, and acceptance criteria. This is the atomic unit of work that a sub-agent can execute with a fresh context.",
      "kind": "behavioral",
      "source": "gsd:task-format + superpowers:executing-plans",
      "confidence": 0.74
    }
  ],
  "tokens_used": 583,
  "candidates_considered": 67
}
```

**What is happening here:** The planning directives come from three different skill lineages (GSD's roadmapping, Superpowers' plan writing, Allium's elicitation methodology) -- but the agent receives them as a unified, non-contradictory set because the decomposition pipeline already resolved overlaps during ingestion. The agent also gets codebase-specific pattern knowledge (existing auth middleware), past-project lessons (JWT structure that avoids rework), and user-behavioral observations (tendency to approve quickly). No single skill could provide this combination.

---

## 8. The API

### 8.1 The MCP Plugin Interface

The MCP plugin is a multi-tool with subcommands. It talks to hive-server via a small API:

| Endpoint                            | Method | Purpose                                                 |
| ----------------------------------- | ------ | ------------------------------------------------------- |
| `/api/v1/inject`                    | POST   | Get contextualized directives for the current situation |
| `/api/v1/feedback`                  | POST   | Report outcomes for a previous injection                |
| `/api/v1/feedback/session-complete` | POST   | Report session summary and key insights                 |
| `/api/v1/ingest`                    | POST   | Submit a skill document or experience for decomposition |
| `/api/v1/directives`                | GET    | Browse/search the directive catalog (admin/debug)       |
| `/api/v1/directives/{id}`           | GET    | Get a single directive with full metadata (admin/debug) |
| `/api/v1/directives/{id}`           | PATCH  | Manually adjust a directive's weight or active status   |
| `/health`                           | GET    | Health probe                                            |
| `/ready`                            | GET    | Readiness probe                                         |

That is **9 endpoints**, not 66.

### 8.2 The Inject Endpoint (Primary)

This is the endpoint the MCP plugin calls on every significant context change:

```
POST /api/v1/inject
Authorization: Bearer <token>
X-Agent-ID: agent-007

{
  "session_id": "sess_xyz",
  "context": {
    "intent": "string - what the agent is trying to do",
    "files": ["string array - files the agent is working with"],
    "repo": "string - repository identifier",
    "phase": "string - planning|implementation|debugging|review|brainstorming",
    "recent_actions": ["string array - last 3-5 agent actions"],
    "conversation_summary": "string - 1-3 sentence summary of the current conversation",
    "open_requirements": ["string array - requirement IDs that are in scope"],
    "current_project": "string - project ID if applicable"
  },
  "token_budget": 500,
  "previous_injection_id": "inj_prev_id or null"
}

Response 200:
{
  "injection_id": "uuid",
  "directives": [
    {
      "id": "uuid",
      "content": "string - the contextualized directive text",
      "kind": "behavioral|pattern|contextual|corrective|factual",
      "source": "string - human-readable provenance",
      "confidence": 0.0-1.0
    }
  ],
  "tokens_used": 487,
  "token_budget": 500,
  "candidates_considered": 38,
  "candidates_selected": 5
}
```

### 8.3 The Feedback Endpoint

```
POST /api/v1/feedback
Authorization: Bearer <token>
X-Agent-ID: agent-007

{
  "injection_id": "uuid",
  "outcomes": [
    {
      "directive_id": "uuid",
      "outcome": "followed|ignored|negative",
      "evidence": "string - what happened"
    }
  ],
  "session_outcome": "success|failure|partial|ongoing",
  "session_summary": "string - what happened in this session"
}

Response 200:
{
  "recorded": true,
  "directives_updated": 5,
  "new_directives_created": 1
}
```

### 8.4 The Ingest Endpoint

```
POST /api/v1/ingest
Authorization: Bearer <token>

{
  "name": "superpowers:systematic-debugging",
  "source_type": "skill",
  "content": "string - the full markdown content of the skill document",
  "metadata": {
    "version": "4.3.1",
    "category": "debugging",
    "tags": ["debugging", "methodology", "root-cause"]
  }
}

Response 202 (Accepted - decomposition is async):
{
  "ingestion_id": "uuid",
  "status": "processing",
  "estimated_directives": 15,
  "poll_url": "/api/v1/ingest/uuid"
}

-- After processing completes --
GET /api/v1/ingest/{id}

Response 200:
{
  "ingestion_id": "uuid",
  "status": "complete",
  "directives_created": 18,
  "directives_merged": 3,
  "directives_total": 15,
  "source_hash": "sha256:abc..."
}
```

### 8.5 When Does the MCP Plugin Call Inject?

The MCP plugin does not call inject on every user message. It calls inject on **context transitions**:

1. **Session start**: Agent begins a new session. The plugin sends a context frame with the repo, any open project, and an intent of "starting session." The response provides the baseline behavioral context.

2. **Phase transition**: Agent shifts from brainstorming to planning, or from planning to implementation. The phase change triggers a new injection with updated directives.

3. **Significant action**: Agent starts debugging, switches files, encounters an error. The plugin detects the shift and requests updated directives.

4. **Periodic refresh**: Every N minutes (configurable, default 5), if the agent is still active, the plugin requests a refresh with an updated conversation summary. This catches gradual context drift.

5. **Explicit request**: The agent can request a refresh by calling the MCP tool directly: "What should I know about debugging race conditions?"

---

## 9. How Skills Become Data

### 9.1 The Ingestion Process

A skill document goes through these stages:

```
Raw skill document (markdown)
         |
    [1. Parse metadata]
         |
    YAML frontmatter extracted: name, description, category
         |
    [2. Section]
         |
    Document split into logical blocks by heading structure
    Each section: heading, content, context (parent heading chain)
         |
    [3. Decompose]
         |
    Each section sent to LLM with decomposition prompt
    LLM extracts 2-8 atomic directives per section
    Each directive has: content, kind, triggers, tags
         |
    [4. Enrich]
         |
    Token cost estimated per directive
    Cross-references detected via semantic similarity
    Scope assigned (global vs. repo-specific)
    Initial weight assigned (anti-rationalization > general > contextual)
         |
    [5. Deduplicate]
         |
    Semantic similarity check against existing directive base
    Near-duplicates merged (strongest version kept, provenance combined)
    Unique directives added to the catalog
         |
    [6. Chain]
         |
    Related directives grouped into behavioral chains
    Chains ordered by logical sequence
    Chains linked in Gel for traversal
         |
    [7. Index]
         |
    CockroachDB: directive rows with full metadata
    Meilisearch: directive content + triggers for semantic search
    Gel: directive nodes with chain/relationship links
```

### 9.2 Concrete Example: Ingesting superpowers:brainstorming

**Input:** The `brainstorming` SKILL.md from Superpowers (approximately 1,500 tokens of structured methodology about creative exploration before planning).

**Step 1 - Parse metadata:**

```yaml
name: brainstorming
description: "Use when beginning any creative or design work - structured exploration before committing to a direction"
category: process
```

**Step 2 - Section:**
The document splits into 5 sections:

1. "When to activate" (activation conditions)
2. "The brainstorming process" (core methodology)
3. "Generating options" (how to produce ideas)
4. "Evaluating tradeoffs" (how to compare options)
5. "Converging on a direction" (how to decide)

**Step 3 - Decompose:** The LLM processes each section. Section 2 ("The brainstorming process") produces:

```json
[
  {
    "content": "When the user describes a problem or feature request, do not jump to a solution. First, generate at least 3 distinct approaches. Distinct means they differ in architecture, technology, or methodology -- not just parameter variations of the same idea.",
    "kind": "behavioral",
    "trigger_tags": ["brainstorming", "planning", "design", "new-feature"],
    "trigger_intent": "Agent needs to explore options before committing to an approach",
    "trigger_phase": "planning"
  },
  {
    "content": "For each brainstormed option, state: (1) the core idea in one sentence, (2) the primary advantage, (3) the primary risk or disadvantage, (4) a rough effort estimate (hours/days/weeks). Present as a numbered list.",
    "kind": "behavioral",
    "trigger_tags": ["brainstorming", "option-evaluation", "tradeoffs"],
    "trigger_intent": "Agent has generated multiple options and needs to present them",
    "trigger_phase": "planning"
  },
  {
    "content": "After presenting options, do not immediately recommend one. Ask the user: 'Which of these directions interests you? Are there constraints I have not considered?' The user may have context that changes the evaluation.",
    "kind": "behavioral",
    "trigger_tags": ["brainstorming", "user-input", "decision"],
    "trigger_intent": "Agent has presented brainstorming options",
    "trigger_phase": "planning"
  }
]
```

**Step 4 - Enrich:**

- Token costs: 47, 52, 41 tokens respectively.
- Cross-references: Directive 1 relates to GSD's planning methodology (also says "decompose before executing"). Directive 3 relates to Allium's elicitation methodology ("gather stakeholder input before committing").
- Scope: All three are `global` -- applicable everywhere.
- Weight: All three get weight 1.0 (standard behavioral directives).

**Step 5 - Deduplicate:**

- Directive 1 ("generate at least 3 approaches") is semantically similar to an existing directive from GSD ingestion: "Explore multiple decomposition strategies before committing to a phase structure." They are merged: the Superpowers version is kept (more specific), and the provenance records both sources. The merged directive gets weight 1.3 (bonus for independent convergence).

**Step 6 - Chain:**
The three directives form a natural chain: `brainstorming-methodology`

- Sequence 1: Generate options
- Sequence 2: Evaluate tradeoffs
- Sequence 3: Ask user for direction

This chain is linked in Gel. When an agent is brainstorming and has already generated options (the Meilisearch search detects "I've listed 3 approaches"), the injection pipeline can follow the chain to serve the next step ("evaluate tradeoffs") rather than re-serving the first step.

**Step 7 - Index:**

- CRDB: 3 directive rows (after dedup: 2 new + 1 merged)
- Meilisearch: 3 documents in the `directives` index
- Gel: 3 Directive nodes, 1 DirectiveChain node with ordered links

### 9.3 Ingestion for Non-Skill Sources

Skills are not the only ingestion source. The system also ingests:

**Codebase patterns:** A scan of the repository structure produces factual directives: "This repo uses chi v5," "Tests are alongside code in \_test.go files," "The store interface is in internal/store/store.go."

**User preferences:** When a user explicitly states a preference ("I like terse commit messages," "Always use table-driven tests in Go"), it becomes a factual directive with `source_type: user` and high weight.

**Session outcomes:** As described in Section 6, successful debugging sessions, planning sessions, and implementation sessions generate experience-derived corrective directives.

**External documents:** Architecture decision records, coding standards documents, team guidelines -- any markdown document can be ingested. The decomposition pipeline extracts behavioral and factual directives from them.

---

## 10. Implementation Priorities

### 10.1 Phase 1: Core Engine (Weeks 1-3)

**Directive model and storage:**

- CockroachDB schema: `directives`, `agent_sessions`, `injections`, `injection_outcomes`, `ingestion_sources`
- Meilisearch index: `directives` with full settings
- Gel schema: `Directive`, `DirectiveChain`, `Source`
- Sync mechanism: CRDB write -> async Meilisearch/Gel indexing

**Inject endpoint:**

- Accept context frame
- Query all three databases in parallel
- Merge, rank, select within token budget
- Return raw (non-contextualized) directives

**Ingest endpoint:**

- Accept skill documents
- Sectioning (deterministic markdown parsing)
- LLM decomposition (single call per section)
- Store to all three databases
- No deduplication yet (append-only)

**Feedback endpoint:**

- Accept outcome reports
- Update effectiveness metrics in CRDB

### 10.2 Phase 2: Intelligence (Weeks 4-6)

**Contextualization:**

- Add Haiku-class LLM call to adapt directives to current situation
- Fallback to raw directives on timeout

**Deduplication:**

- Semantic similarity detection during ingestion
- Merge logic with provenance preservation

**Chain detection:**

- Automatic grouping of related directives into behavioral chains
- Gel chain traversal in the injection pipeline

**Experience-derived directives:**

- Session-complete endpoint creates new directives from outcomes
- Experience directives enter the standard injection ranking

### 10.3 Phase 3: Refinement (Weeks 7-9)

**Token budgeting improvements:**

- Diminishing returns cutoff
- Session deduplication (do not re-inject same directives)
- Cooldown for ignored directives

**MCP plugin integration:**

- Context transition detection (phase changes, significant actions)
- Periodic refresh mechanism
- Feedback reporting automation

**Admin tooling:**

- Directive browser with effectiveness dashboards
- Ingestion status monitoring
- Manual directive creation/editing
- Source effectiveness comparison

### 10.4 What Is Not Built

- No planning state machine (GSD's phase/plan/task hierarchy is not recreated)
- No workflow orchestration (wave scheduling, parallel dispatch stay client-side)
- No slash command system (the MCP plugin is a tool, not a command framework)
- No spec storage (Allium's .allium files stay on disk; the directives extracted from the methodology are what matters)
- No skill registry (skills are ingested as training data, not maintained as a catalog)

---

## 11. Summary

Hive-server v4 is a behavioral knowledge engine with four capabilities:

1. **Decompose** skill prompts and experience into atomic behavioral directives
2. **Store** directives across CRDB (state), Meilisearch (discovery), and Gel (relationships)
3. **Retrieve** contextually relevant directives when an agent asks "what should I know?"
4. **Recompose** directives into token-budgeted, contextualized injections

It has 9 API endpoints, not 66. It treats skills as training data, not APIs to replicate. It learns from agent outcomes and evolves its directive population over time. It provides the same behavioral guidance that static skill documents provide, but contextualized to the current situation, informed by past experience, and adapted to what actually works.

The key insight that drives everything: **the skills are prompt engineering documents that teach LLMs how to behave. Hive-server decomposes that behavioral wisdom, stores it, and recomposes it contextually. The skills become unnecessary not because their APIs are replicated, but because their knowledge is absorbed into a system that delivers it more precisely.**
