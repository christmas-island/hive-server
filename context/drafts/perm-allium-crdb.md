# Permutation Analysis: Allium + CockroachDB

**Date:** 2026-03-09
**Purpose:** Explore how CockroachDB could serve as a persistence and query layer for Allium behavioral specifications, with hive-server as the mediating API.

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [The Impedance Problem](#2-the-impedance-problem)
3. [Relational Schema for Allium Constructs](#3-relational-schema-for-allium-constructs)
4. [JSONB for Semi-Structured Spec Content](#4-jsonb-for-semi-structured-spec-content)
5. [Transactional Consistency for Spec Updates](#5-transactional-consistency-for-spec-updates)
6. [Event Sourcing for Spec Evolution](#6-event-sourcing-for-spec-evolution)
7. [Agent Queries: SQL Instead of File Parsing](#7-agent-queries-sql-instead-of-file-parsing)
8. [Runtime Trace Validation](#8-runtime-trace-validation)
9. [Cross-Project Spec Queries and Composition](#9-cross-project-spec-queries-and-composition)
10. [Distributed Spec Sharing](#10-distributed-spec-sharing)
11. [hive-server as Mediator](#11-hive-server-as-mediator)
12. [Tradeoffs](#12-tradeoffs)
13. [Implementation Roadmap](#13-implementation-roadmap)

---

## 1. Executive Summary

Allium is a behavioral specification language that produces flat `.allium` files read by humans and LLMs. It has no runtime, no database, and no query capability beyond what the Rust parser's JSON AST output provides. CockroachDB is a distributed SQL database with JSONB support, ACID transactions, and PostgreSQL-compatible querying.

The central thesis of this analysis: **storing parsed Allium ASTs in CockroachDB transforms specs from passive documents into queryable, versionable, composable artifacts** that agents can interrogate programmatically. The Allium limitation "you cannot query the spec for 'which rules affect entity X' programmatically" becomes a solved problem.

The cost is real: a new persistence layer, a synchronization mechanism between `.allium` files and database records, and additional infrastructure. Whether that cost is justified depends on the scale of spec usage -- a single project with three spec files does not need a database; an organization with fifty projects and hundreds of cross-referencing specs does.

---

## 2. The Impedance Problem

Allium specs are hierarchical, semi-structured documents. CockroachDB stores normalized relational data. The mapping between them is not trivial.

### What Maps Cleanly

- **Entities, rules, surfaces, contracts, actors, invariants** are discrete, named constructs with well-defined boundaries. Each becomes a row in a typed table.
- **Cross-references** (an entity referenced by a rule, a contract demanded by a surface) are foreign key relationships.
- **Config values** are key-value pairs with optional defaults and arithmetic expressions.
- **Tags, metadata, annotations** are naturally semi-structured and fit JSONB.

### What Maps Awkwardly

- **Expression language**: Rule preconditions (`requires:`) and postconditions (`ensures:`) are mini-programs. They can be stored as text or as parsed AST fragments in JSONB, but the database cannot evaluate them.
- **Section ordering**: Allium files have a defined section order that carries structural meaning. Relational tables have no inherent row ordering.
- **Indentation-significant blocks**: The visual structure of an Allium file conveys hierarchy. Flattening into rows loses this.
- **Prose annotations**: `@invariant` prose descriptions are human-readable text embedded in structural context. Extracting them from that context reduces their value.

### Resolution Strategy

Store **both** the structured decomposition (for querying) **and** the original source text (for reconstruction). The database is an index over the specs, not a replacement for them.

---

## 3. Relational Schema for Allium Constructs

### Core Tables

```sql
-- A project that contains Allium specs
CREATE TABLE allium_projects (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT        NOT NULL UNIQUE,
    description     TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A single .allium file (a "module" in Allium terms)
CREATE TABLE allium_modules (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID        NOT NULL REFERENCES allium_projects(id) ON DELETE CASCADE,
    file_path       TEXT        NOT NULL,       -- relative path within project
    version_marker  INT4        NOT NULL DEFAULT 2, -- "-- allium: 2"
    source_text     TEXT        NOT NULL,       -- full original .allium source
    source_hash     TEXT        NOT NULL,       -- SHA-256 of source_text
    parsed_ast      JSONB       NOT NULL,       -- full AST from Rust parser
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, file_path)
);

-- Entities declared in a module
CREATE TABLE allium_entities (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    module_id       UUID        NOT NULL REFERENCES allium_modules(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,       -- PascalCase entity name
    kind            TEXT        NOT NULL DEFAULT 'entity',  -- 'entity', 'value_type', 'variant'
    parent_entity   TEXT,                       -- for variants: the parent entity name
    discriminator   TEXT,                       -- for sum types: the discriminator field
    fields          JSONB       NOT NULL DEFAULT '[]'::JSONB,
    -- fields: [{"name": "status", "type": "closed | open | half_open", "derived": false}, ...]
    relationships   JSONB       NOT NULL DEFAULT '[]'::JSONB,
    -- relationships: [{"name": "service", "target": "ExternalService", "cardinality": "one"}, ...]
    projections     JSONB       NOT NULL DEFAULT '[]'::JSONB,
    source_fragment TEXT        NOT NULL DEFAULT '',  -- the entity block from source
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (module_id, name)
);

-- Rules declared in a module
CREATE TABLE allium_rules (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    module_id       UUID        NOT NULL REFERENCES allium_modules(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,       -- PascalCase rule name
    trigger_type    TEXT        NOT NULL,       -- 'external', 'state_transition', 'temporal', 'creation', 'derived', 'chained'
    trigger_expr    TEXT        NOT NULL,       -- the "when:" clause text
    preconditions   JSONB       NOT NULL DEFAULT '[]'::JSONB,
    -- preconditions: [{"type": "requires", "expr": "exists user"}, ...]
    postconditions  JSONB       NOT NULL DEFAULT '[]'::JSONB,
    -- postconditions: [{"type": "ensures", "expr": "new ResetToken created"}, ...]
    let_bindings    JSONB       NOT NULL DEFAULT '[]'::JSONB,
    -- let_bindings: [{"name": "user", "expr": "User{email}"}, ...]
    referenced_entities JSONB   NOT NULL DEFAULT '[]'::JSONB,
    -- referenced_entities: ["User", "ResetToken"] -- extracted for cross-reference queries
    source_fragment TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (module_id, name)
);

-- Surfaces declared in a module
CREATE TABLE allium_surfaces (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    module_id       UUID        NOT NULL REFERENCES allium_modules(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    facing          TEXT        NOT NULL,       -- the actor this surface faces
    context_entity  TEXT,                       -- the "context:" entity
    exposes         JSONB       NOT NULL DEFAULT '[]'::JSONB,
    -- exposes: ["workspace.name", "workspace.members"]
    provides        JSONB       NOT NULL DEFAULT '[]'::JSONB,
    -- provides: [{"action": "invite_member", "params": ["email"], "guard": "workspace.seats_available > 0"}]
    demanded_contracts JSONB    NOT NULL DEFAULT '[]'::JSONB,
    -- demanded_contracts: ["Codec"]
    guarantees      JSONB       NOT NULL DEFAULT '[]'::JSONB,
    -- guarantees: [{"name": "DataFreshness", "prose": "data reflects state within 30 seconds"}]
    timeout         TEXT,
    source_fragment TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (module_id, name)
);

-- Contracts declared in a module
CREATE TABLE allium_contracts (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    module_id       UUID        NOT NULL REFERENCES allium_modules(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    signatures      JSONB       NOT NULL DEFAULT '[]'::JSONB,
    -- signatures: [{"name": "serialize", "params": [{"name": "value", "type": "Any"}], "returns": "ByteArray"}]
    invariants      JSONB       NOT NULL DEFAULT '[]'::JSONB,
    -- invariants: [{"name": "Roundtrip", "prose": "deserialize(serialize(x)) = x"}]
    source_fragment TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (module_id, name)
);

-- Actors declared in a module
CREATE TABLE allium_actors (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    module_id       UUID        NOT NULL REFERENCES allium_modules(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    identity_mapping TEXT,
    scope           TEXT,       -- the "within" clause
    source_fragment TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (module_id, name)
);

-- Invariants declared at module or entity scope
CREATE TABLE allium_invariants (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    module_id       UUID        NOT NULL REFERENCES allium_modules(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    scope           TEXT        NOT NULL DEFAULT 'module', -- 'module', 'entity', 'contract'
    scope_target    TEXT,       -- entity/contract name if scoped
    form            TEXT        NOT NULL DEFAULT 'expression', -- 'expression' or 'prose'
    expression      TEXT,       -- for expression-bearing invariants
    prose           TEXT,       -- for prose annotations
    source_fragment TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (module_id, name)
);

-- Config parameters declared in a module
CREATE TABLE allium_config (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    module_id       UUID        NOT NULL REFERENCES allium_modules(id) ON DELETE CASCADE,
    key             TEXT        NOT NULL,       -- snake_case config key
    default_value   TEXT,
    default_expr    TEXT,       -- for arithmetic defaults
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (module_id, key)
);

-- Module imports (use declarations)
CREATE TABLE allium_imports (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    module_id       UUID        NOT NULL REFERENCES allium_modules(id) ON DELETE CASCADE,
    imported_path   TEXT        NOT NULL,       -- the import path
    alias           TEXT,                       -- optional alias
    resolved_module_id UUID    REFERENCES allium_modules(id),  -- resolved if available
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (module_id, imported_path)
);
```

### Indexes

```sql
-- Standard B-tree indexes for lookup patterns
CREATE INDEX idx_modules_project    ON allium_modules(project_id);
CREATE INDEX idx_modules_hash       ON allium_modules(source_hash);
CREATE INDEX idx_entities_module    ON allium_entities(module_id);
CREATE INDEX idx_entities_kind      ON allium_entities(kind);
CREATE INDEX idx_rules_module       ON allium_rules(module_id);
CREATE INDEX idx_rules_trigger_type ON allium_rules(trigger_type);
CREATE INDEX idx_surfaces_module    ON allium_surfaces(module_id);
CREATE INDEX idx_surfaces_facing    ON allium_surfaces(facing);
CREATE INDEX idx_contracts_module   ON allium_contracts(module_id);
CREATE INDEX idx_actors_module      ON allium_actors(module_id);
CREATE INDEX idx_invariants_module  ON allium_invariants(module_id);
CREATE INDEX idx_invariants_scope   ON allium_invariants(scope, scope_target);

-- Inverted indexes for JSONB querying
CREATE INVERTED INDEX idx_entities_fields       ON allium_entities(fields);
CREATE INVERTED INDEX idx_entities_relationships ON allium_entities(relationships);
CREATE INVERTED INDEX idx_rules_preconditions   ON allium_rules(preconditions);
CREATE INVERTED INDEX idx_rules_postconditions  ON allium_rules(postconditions);
CREATE INVERTED INDEX idx_rules_refs            ON allium_rules(referenced_entities);
CREATE INVERTED INDEX idx_surfaces_exposes      ON allium_surfaces(exposes);
CREATE INVERTED INDEX idx_surfaces_contracts    ON allium_surfaces(demanded_contracts);
CREATE INVERTED INDEX idx_modules_ast           ON allium_modules(parsed_ast);
```

### Design Rationale

The schema follows a **decomposed-with-source** strategy:

1. Each Allium construct type gets its own table. This enables typed queries ("find all rules with temporal triggers") without parsing JSONB.
2. Fields within constructs that have variable structure (entity fields, rule preconditions, surface operations) use JSONB columns. This avoids a combinatorial explosion of join tables for every possible field type.
3. Every construct row retains its `source_fragment` -- the verbatim text from the `.allium` file. This allows reconstruction without loss.
4. The `allium_modules` table stores both the full source text and the complete parsed AST. The per-construct tables are effectively a denormalized index over the AST.

---

## 4. JSONB for Semi-Structured Spec Content

### Why JSONB Is Essential

Allium constructs are not uniformly structured. An entity might have two fields or twenty. A rule might have one precondition or five. The expression language is free-form text within a structural frame. JSONB handles this naturally.

### Concrete JSONB Patterns

**Entity fields as JSONB:**

```json
[
  {
    "name": "service",
    "type": "ExternalService",
    "derived": false,
    "optional": false
  },
  {
    "name": "status",
    "type": "closed | open | half_open",
    "derived": false,
    "optional": false
  },
  {
    "name": "failure_rate",
    "type": "derived",
    "derived": true,
    "derived_from": "recent failures"
  },
  {
    "name": "is_tripped",
    "type": "Boolean",
    "derived": true,
    "optional": false
  }
]
```

**Rule postconditions as JSONB:**

```json
[
  {
    "type": "ensures",
    "expr": "existing reset tokens invalidated",
    "affects": ["ResetToken"]
  },
  {
    "type": "ensures",
    "expr": "new ResetToken created",
    "affects": ["ResetToken"]
  },
  { "type": "ensures", "expr": "email sent to user", "affects": [] }
]
```

**Surface operations as JSONB:**

```json
[
  {
    "action": "invite_member",
    "params": [{ "name": "email", "type": "String" }],
    "guard": "workspace.seats_available > 0",
    "guard_entities": ["Workspace"]
  }
]
```

### Querying JSONB

CockroachDB supports PostgreSQL JSONB operators. Combined with inverted indexes, these queries are efficient:

```sql
-- Find all entities that have a field of type Boolean
SELECT e.name, e.module_id
FROM allium_entities e
WHERE e.fields @> '[{"type": "Boolean"}]';

-- Find all rules that reference a specific entity
SELECT r.name, r.trigger_expr
FROM allium_rules r
WHERE r.referenced_entities @> '["ResetToken"]';

-- Find all surfaces that demand the Codec contract
SELECT s.name, s.facing
FROM allium_surfaces s
WHERE s.demanded_contracts @> '["Codec"]';
```

### Computed Columns for Hot Paths

For frequently queried JSONB paths, CockroachDB supports computed columns with standard indexes:

```sql
-- Materialize the number of preconditions for quick filtering
ALTER TABLE allium_rules ADD COLUMN precondition_count INT4
    AS (jsonb_array_length(preconditions)) STORED;

CREATE INDEX idx_rules_precondition_count ON allium_rules(precondition_count);

-- Find rules with no preconditions (potential spec smell)
SELECT name FROM allium_rules WHERE precondition_count = 0;
```

### Full AST Storage

The `allium_modules.parsed_ast` column stores the entire Rust parser JSON output. This serves two purposes:

1. **Reconstruction**: The AST can regenerate the per-construct table rows if the decomposition logic changes.
2. **Ad-hoc queries**: For questions the decomposed tables do not anticipate, the full AST is available for JSONB path queries.

```sql
-- Query the raw AST for any construct type not yet decomposed into its own table
SELECT m.file_path,
       jsonb_array_elements(m.parsed_ast->'open_questions') AS question
FROM allium_modules m
WHERE m.parsed_ast ? 'open_questions';
```

---

## 5. Transactional Consistency for Spec Updates

### The Consistency Problem

An Allium spec update might touch multiple constructs simultaneously: renaming an entity requires updating every rule, surface, and contract that references it. In a file-based workflow, the LLM makes all changes in a single file write. In a database-backed workflow, this becomes a multi-table update that must be atomic.

### CockroachDB's ACID Guarantees

CockroachDB provides serializable isolation by default. A spec update transaction looks like this:

```go
err := crdbpgx.ExecuteTx(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
    // 1. Update the module source and AST
    _, err := tx.Exec(ctx, `
        UPDATE allium_modules
        SET source_text = $1, source_hash = $2, parsed_ast = $3, updated_at = now()
        WHERE id = $4`,
        newSource, newHash, newAST, moduleID)
    if err != nil {
        return fmt.Errorf("update module: %w", err)
    }

    // 2. Delete all existing constructs for this module
    for _, table := range []string{
        "allium_entities", "allium_rules", "allium_surfaces",
        "allium_contracts", "allium_actors", "allium_invariants",
        "allium_config", "allium_imports",
    } {
        _, err := tx.Exec(ctx,
            fmt.Sprintf("DELETE FROM %s WHERE module_id = $1", table), moduleID)
        if err != nil {
            return fmt.Errorf("clear %s: %w", table, err)
        }
    }

    // 3. Re-insert decomposed constructs from the new AST
    for _, entity := range parsedEntities {
        _, err := tx.Exec(ctx, `
            INSERT INTO allium_entities (module_id, name, kind, fields, relationships, source_fragment)
            VALUES ($1, $2, $3, $4, $5, $6)`,
            moduleID, entity.Name, entity.Kind, entity.Fields, entity.Rels, entity.Source)
        if err != nil {
            return fmt.Errorf("insert entity %s: %w", entity.Name, err)
        }
    }
    // ... repeat for rules, surfaces, contracts, etc.

    return nil
})
```

Key properties this guarantees:

- **Atomicity**: Either all construct tables are updated or none are. No reader sees a half-updated spec.
- **Consistency**: Foreign key constraints (e.g., `allium_imports.resolved_module_id`) are enforced within the transaction.
- **Isolation**: Concurrent reads see either the old spec or the new spec, never a mix. Serializable isolation prevents phantom reads.
- **Durability**: Once committed, the spec update survives node failures (Raft replication ensures at least 3 copies).

### Optimistic Concurrency for Spec Edits

Multiple agents or developers editing the same spec simultaneously need conflict detection:

```sql
-- Add a version column to modules
ALTER TABLE allium_modules ADD COLUMN version INT8 NOT NULL DEFAULT 1;
```

```go
err := crdbpgx.ExecuteTx(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
    // Check version matches what the editor started with
    var currentVersion int64
    err := tx.QueryRow(ctx,
        "SELECT version FROM allium_modules WHERE id = $1", moduleID,
    ).Scan(&currentVersion)
    if err != nil {
        return err
    }
    if currentVersion != expectedVersion {
        return fmt.Errorf("spec was modified by another editor (expected v%d, found v%d)",
            expectedVersion, currentVersion)
    }

    // Proceed with update, incrementing version
    _, err = tx.Exec(ctx, `
        UPDATE allium_modules SET source_text = $1, version = version + 1, updated_at = now()
        WHERE id = $2`, newSource, moduleID)
    return err
})
```

### Cross-Module Reference Integrity

When module A imports module B and references entity `B.User`, a database can enforce that `B.User` actually exists:

```sql
-- Materialized cross-reference table (populated during spec ingestion)
CREATE TABLE allium_cross_refs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    source_module   UUID        NOT NULL REFERENCES allium_modules(id) ON DELETE CASCADE,
    source_construct TEXT       NOT NULL,  -- e.g., "RequestPasswordReset" (a rule name)
    source_type     TEXT        NOT NULL,  -- e.g., "rule"
    target_module   UUID        REFERENCES allium_modules(id),
    target_construct TEXT       NOT NULL,  -- e.g., "User" (an entity name)
    target_type     TEXT        NOT NULL,  -- e.g., "entity"
    resolved        BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_crossrefs_target ON allium_cross_refs(target_module, target_construct);
CREATE INDEX idx_crossrefs_unresolved ON allium_cross_refs(resolved) WHERE NOT resolved;
```

This enables queries like "what breaks if I rename entity User?" -- a question that is impossible to answer reliably from flat files without parsing every `.allium` file in every project.

---

## 6. Event Sourcing for Spec Evolution

### Why Event Sourcing Matters for Specs

Allium specs are living documents. Understanding _why_ a spec changed is as important as knowing _what_ changed. Git provides file-level diffs, but a database can provide construct-level change tracking.

### Event Log Schema

```sql
CREATE TABLE allium_events (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    module_id       UUID        NOT NULL REFERENCES allium_modules(id) ON DELETE CASCADE,
    event_type      TEXT        NOT NULL,
    -- event types: 'module_created', 'module_updated',
    --   'entity_added', 'entity_modified', 'entity_removed',
    --   'rule_added', 'rule_modified', 'rule_removed',
    --   'surface_added', 'surface_modified', 'surface_removed',
    --   'contract_added', 'contract_modified', 'contract_removed',
    --   'invariant_added', 'invariant_modified', 'invariant_removed'
    construct_type  TEXT,       -- 'entity', 'rule', 'surface', 'contract', 'actor', 'invariant'
    construct_name  TEXT,       -- PascalCase name of the affected construct
    agent_id        TEXT        NOT NULL DEFAULT '',  -- which agent/user made the change
    change_summary  TEXT        NOT NULL DEFAULT '',  -- human-readable summary
    old_value       JSONB,      -- previous state (for modifications)
    new_value       JSONB,      -- new state (for additions and modifications)
    module_version  INT8        NOT NULL,  -- module version at time of event
    source_hash     TEXT        NOT NULL,  -- source hash after this event
    git_commit      TEXT,       -- optional: git SHA if known
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_events_module     ON allium_events(module_id, created_at DESC);
CREATE INDEX idx_events_construct  ON allium_events(construct_type, construct_name);
CREATE INDEX idx_events_agent      ON allium_events(agent_id);
CREATE INDEX idx_events_type       ON allium_events(event_type);
```

### Event Generation

When a module is updated, the ingestion pipeline diffs the old and new decomposed constructs:

```go
func diffAndRecordEvents(ctx context.Context, tx pgx.Tx, moduleID uuid.UUID,
    oldEntities, newEntities []Entity, moduleVersion int64, agentID string) error {

    oldMap := indexByName(oldEntities)
    newMap := indexByName(newEntities)

    // Detect additions
    for name, entity := range newMap {
        if _, existed := oldMap[name]; !existed {
            _, err := tx.Exec(ctx, `
                INSERT INTO allium_events
                    (module_id, event_type, construct_type, construct_name,
                     agent_id, new_value, module_version, source_hash)
                VALUES ($1, 'entity_added', 'entity', $2, $3, $4, $5, $6)`,
                moduleID, name, agentID, entity.ToJSON(), moduleVersion, sourceHash)
            if err != nil {
                return err
            }
        }
    }

    // Detect removals
    for name, entity := range oldMap {
        if _, exists := newMap[name]; !exists {
            _, err := tx.Exec(ctx, `
                INSERT INTO allium_events
                    (module_id, event_type, construct_type, construct_name,
                     agent_id, old_value, module_version, source_hash)
                VALUES ($1, 'entity_removed', 'entity', $2, $3, $4, $5, $6)`,
                moduleID, name, agentID, entity.ToJSON(), moduleVersion, sourceHash)
            if err != nil {
                return err
            }
        }
    }

    // Detect modifications (compare JSON representations)
    for name, newEntity := range newMap {
        if oldEntity, existed := oldMap[name]; existed {
            if !jsonEqual(oldEntity, newEntity) {
                _, err := tx.Exec(ctx, `
                    INSERT INTO allium_events
                        (module_id, event_type, construct_type, construct_name,
                         agent_id, old_value, new_value, module_version, source_hash)
                    VALUES ($1, 'entity_modified', 'entity', $2, $3, $4, $5, $6, $7)`,
                    moduleID, name, agentID, oldEntity.ToJSON(), newEntity.ToJSON(),
                    moduleVersion, sourceHash)
                if err != nil {
                    return err
                }
            }
        }
    }

    return nil
}
```

### Evolution Queries

```sql
-- Timeline of changes to a specific entity
SELECT event_type, change_summary, agent_id, created_at
FROM allium_events
WHERE construct_type = 'entity' AND construct_name = 'CircuitBreaker'
ORDER BY created_at DESC;

-- All spec changes made by a specific agent in the last 24 hours
SELECT module_id, event_type, construct_type, construct_name, change_summary
FROM allium_events
WHERE agent_id = 'tend-agent-01'
  AND created_at > now() - INTERVAL '24 hours'
ORDER BY created_at DESC;

-- Frequency of changes per construct (identify volatile specs)
SELECT construct_type, construct_name, COUNT(*) AS change_count
FROM allium_events
WHERE module_id = $1
GROUP BY construct_type, construct_name
ORDER BY change_count DESC;

-- Reconstruct entity state at a specific point in time
-- (requires replaying events from initial state)
SELECT new_value
FROM allium_events
WHERE construct_type = 'entity'
  AND construct_name = 'User'
  AND module_id = $1
  AND created_at <= $2  -- target timestamp
ORDER BY created_at DESC
LIMIT 1;
```

### Comparison with Git

| Dimension          | Git                            | CockroachDB Event Log          |
| ------------------ | ------------------------------ | ------------------------------ |
| Granularity        | File-level diffs               | Construct-level diffs          |
| Query capability   | `git log --follow file.allium` | SQL queries on any dimension   |
| Cross-project      | Per-repository                 | Cross-project queries          |
| Agent attribution  | Commit author                  | `agent_id` per event           |
| Offline support    | Full history locally           | Requires database connectivity |
| Storage efficiency | Delta compression              | Full snapshots in JSONB        |

Git remains the source of truth for `.allium` files. The event log is a derived, queryable index over the change history, not a replacement for version control.

---

## 7. Agent Queries: SQL Instead of File Parsing

### Current Workflow

Today, the Tend and Weed agents work by:

1. Reading `.allium` files from disk
2. Parsing them (either via the Rust parser or by LLM comprehension)
3. Performing their analysis
4. Writing updated `.allium` files back

This requires file system access and parsing on every invocation. The agents cannot efficiently answer questions like "across all our specs, which entities have no rules that reference them?" without reading and parsing every file.

### Database-Backed Workflow

With specs stored in CockroachDB, agents query the database via hive-server's REST API:

**Tend agent: "What entities exist in project X that have no associated rules?"**

```sql
SELECT e.name, m.file_path
FROM allium_entities e
JOIN allium_modules m ON e.module_id = m.id
JOIN allium_projects p ON m.project_id = p.id
WHERE p.name = 'payment-service'
  AND NOT EXISTS (
      SELECT 1 FROM allium_rules r
      WHERE r.module_id = m.id
        AND r.referenced_entities @> jsonb_build_array(e.name)
  );
```

**Weed agent: "What surfaces expose entities that have been removed?"**

```sql
SELECT s.name AS surface, s.exposes, m.file_path
FROM allium_surfaces s
JOIN allium_modules m ON s.module_id = m.id
WHERE EXISTS (
    SELECT 1
    FROM jsonb_array_elements_text(s.exposes) AS exposed_path
    WHERE split_part(exposed_path, '.', 1) NOT IN (
        SELECT e.name FROM allium_entities e WHERE e.module_id = s.module_id
    )
);
```

**Cross-module impact analysis: "What rules reference entity User across all modules?"**

```sql
SELECT r.name AS rule_name, m.file_path, p.name AS project
FROM allium_rules r
JOIN allium_modules m ON r.module_id = m.id
JOIN allium_projects p ON m.project_id = p.id
WHERE r.referenced_entities @> '["User"]';
```

**Contract compliance: "Which surfaces demand contracts that are not defined in their module or imports?"**

```sql
SELECT s.name AS surface, dc.contract_name, m.file_path
FROM allium_surfaces s
JOIN allium_modules m ON s.module_id = m.id
CROSS JOIN LATERAL jsonb_array_elements_text(s.demanded_contracts) AS dc(contract_name)
WHERE dc.contract_name NOT IN (
    SELECT c.name FROM allium_contracts c WHERE c.module_id = m.id
)
AND dc.contract_name NOT IN (
    SELECT c.name
    FROM allium_imports i
    JOIN allium_contracts c ON c.module_id = i.resolved_module_id
    WHERE i.module_id = m.id
);
```

### API Endpoints for Agent Queries

hive-server would expose these as REST endpoints:

```
GET /api/v1/specs/projects                      -- list projects
GET /api/v1/specs/projects/{project}/modules     -- list modules in a project
GET /api/v1/specs/entities?project=X&unused=true -- entities with no rule references
GET /api/v1/specs/rules?entity=User             -- rules referencing an entity
GET /api/v1/specs/surfaces?contract=Codec       -- surfaces demanding a contract
GET /api/v1/specs/cross-refs?target=User        -- all cross-references to an entity
GET /api/v1/specs/events?module={id}&since=...  -- change history
POST /api/v1/specs/modules                       -- ingest a new/updated .allium file
```

Agents call these endpoints instead of reading files directly. The benefit is that the query is executed by the database engine, which has indexes, rather than by the agent's LLM context, which is slow and token-expensive.

---

## 8. Runtime Trace Validation

### The Planned Feature

Allium's roadmap includes "runtime trace validation": deriving trace event schemas from surface definitions and comparing production execution traces against spec contracts. Two architectures are under consideration: a standalone trace file validator, or language-specific middleware.

### CockroachDB as Trace Storage

CockroachDB can store execution traces alongside the specs they are validated against:

```sql
-- A trace is a sequence of events from a system execution
CREATE TABLE allium_traces (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID        NOT NULL REFERENCES allium_projects(id) ON DELETE CASCADE,
    surface_name    TEXT        NOT NULL,  -- which surface this trace exercises
    module_id       UUID        REFERENCES allium_modules(id),
    environment     TEXT        NOT NULL DEFAULT 'production', -- 'production', 'staging', 'test'
    status          TEXT        NOT NULL DEFAULT 'pending',    -- 'pending', 'valid', 'violation', 'error'
    started_at      TIMESTAMPTZ NOT NULL,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Individual events within a trace
CREATE TABLE allium_trace_events (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    trace_id        UUID        NOT NULL REFERENCES allium_traces(id) ON DELETE CASCADE,
    sequence_num    INT4        NOT NULL,
    event_type      TEXT        NOT NULL,  -- 'action_invoked', 'state_changed', 'precondition_checked', 'postcondition_verified'
    construct_type  TEXT,       -- 'rule', 'surface', 'contract'
    construct_name  TEXT,       -- name of the spec construct
    payload         JSONB       NOT NULL DEFAULT '{}'::JSONB,
    -- payload contains the actual event data: parameters, state snapshots, results
    timestamp       TIMESTAMPTZ NOT NULL,
    UNIQUE (trace_id, sequence_num)
);

-- Validation results: specific violations found
CREATE TABLE allium_trace_violations (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    trace_id        UUID        NOT NULL REFERENCES allium_traces(id) ON DELETE CASCADE,
    trace_event_id  UUID        REFERENCES allium_trace_events(id),
    violation_type  TEXT        NOT NULL,
    -- violation types: 'precondition_unmet', 'postcondition_failed',
    --   'invariant_violated', 'contract_breach', 'unexpected_state', 'timeout_exceeded'
    construct_type  TEXT        NOT NULL,
    construct_name  TEXT        NOT NULL,
    expected        TEXT,       -- what the spec says should happen
    actual          TEXT,       -- what actually happened
    severity        TEXT        NOT NULL DEFAULT 'error', -- 'warning', 'error', 'critical'
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_traces_project    ON allium_traces(project_id);
CREATE INDEX idx_traces_surface    ON allium_traces(surface_name);
CREATE INDEX idx_traces_status     ON allium_traces(status);
CREATE INDEX idx_trace_events_trace ON allium_trace_events(trace_id, sequence_num);
CREATE INDEX idx_violations_trace  ON allium_trace_violations(trace_id);
CREATE INDEX idx_violations_type   ON allium_trace_violations(violation_type);
CREATE INVERTED INDEX idx_trace_events_payload ON allium_trace_events(payload);
```

### Validation Flow

```
1. Application middleware captures execution trace events
2. Events are posted to hive-server: POST /api/v1/traces
3. hive-server stores events in allium_trace_events
4. A validation job (or synchronous check) compares events against spec:
   a. Load the surface definition from allium_surfaces
   b. For each action invoked, verify preconditions were met
   c. For each state change, verify postconditions hold
   d. Check invariants at each step
   e. Record violations in allium_trace_violations
5. Update trace status to 'valid' or 'violation'
```

### Trace Queries

```sql
-- Find all contract violations in production over the past week
SELECT v.construct_name, v.violation_type, v.expected, v.actual, t.started_at
FROM allium_trace_violations v
JOIN allium_traces t ON v.trace_id = t.id
WHERE t.environment = 'production'
  AND t.created_at > now() - INTERVAL '7 days'
ORDER BY t.started_at DESC;

-- Compliance rate per surface
SELECT t.surface_name,
       COUNT(*) AS total_traces,
       COUNT(*) FILTER (WHERE t.status = 'valid') AS valid_traces,
       ROUND(100.0 * COUNT(*) FILTER (WHERE t.status = 'valid') / COUNT(*), 2) AS compliance_pct
FROM allium_traces t
WHERE t.project_id = $1
GROUP BY t.surface_name
ORDER BY compliance_pct ASC;

-- Most frequently violated rules
SELECT v.construct_name, v.violation_type, COUNT(*) AS violation_count
FROM allium_trace_violations v
JOIN allium_traces t ON v.trace_id = t.id
WHERE t.project_id = $1
GROUP BY v.construct_name, v.violation_type
ORDER BY violation_count DESC
LIMIT 20;
```

### CockroachDB's Advantage for Traces

Trace data grows linearly with system usage. CockroachDB's horizontal scalability means trace storage does not become a bottleneck as the number of monitored surfaces and execution volume increase. Additionally, CockroachDB's TTL (time-to-live) row expiration can automatically age out old traces:

```sql
-- Keep traces for 90 days, then automatically delete
ALTER TABLE allium_traces SET (ttl_expire_after = '90 days');
ALTER TABLE allium_trace_events SET (ttl_expire_after = '90 days');
ALTER TABLE allium_trace_violations SET (ttl_expire_after = '90 days');
```

---

## 9. Cross-Project Spec Queries and Composition

### The Problem

Allium supports modular composition via `use` declarations with git SHAs or content hashes. But discovering _what is available_ to import, or understanding the dependency graph across projects, requires manual exploration. There is no registry, no search, no dependency resolution beyond what git provides.

### A Spec Registry in CockroachDB

The `allium_projects`, `allium_modules`, and `allium_imports` tables already form a basic registry. Adding explicit dependency tracking:

```sql
-- Cross-project dependency graph
CREATE TABLE allium_dependencies (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    source_project  UUID        NOT NULL REFERENCES allium_projects(id),
    source_module   UUID        NOT NULL REFERENCES allium_modules(id),
    target_project  UUID        REFERENCES allium_projects(id),  -- NULL if external
    target_module   UUID        REFERENCES allium_modules(id),   -- NULL if unresolved
    target_path     TEXT        NOT NULL,  -- the import path as written
    target_hash     TEXT,       -- content hash or git SHA
    status          TEXT        NOT NULL DEFAULT 'resolved', -- 'resolved', 'unresolved', 'stale'
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_deps_source ON allium_dependencies(source_project, source_module);
CREATE INDEX idx_deps_target ON allium_dependencies(target_project, target_module);
```

### Composition Queries

```sql
-- Dependency graph: what does project X depend on?
SELECT DISTINCT tp.name AS dependency, d.target_path, d.status
FROM allium_dependencies d
JOIN allium_projects sp ON d.source_project = sp.id
LEFT JOIN allium_projects tp ON d.target_project = tp.id
WHERE sp.name = 'payment-service'
ORDER BY tp.name;

-- Reverse dependencies: who depends on project Y?
SELECT DISTINCT sp.name AS dependent, d.target_path
FROM allium_dependencies d
JOIN allium_projects sp ON d.source_project = sp.id
JOIN allium_projects tp ON d.target_project = tp.id
WHERE tp.name = 'auth-library'
ORDER BY sp.name;

-- Find reusable contracts across all projects
SELECT c.name, p.name AS project, m.file_path,
       COUNT(*) OVER (PARTITION BY c.name) AS usage_count
FROM allium_contracts c
JOIN allium_modules m ON c.module_id = m.id
JOIN allium_projects p ON m.project_id = p.id
ORDER BY usage_count DESC, c.name;

-- Detect conflicting entity definitions across projects
SELECT e1.name,
       p1.name AS project1, m1.file_path AS file1,
       p2.name AS project2, m2.file_path AS file2
FROM allium_entities e1
JOIN allium_modules m1 ON e1.module_id = m1.id
JOIN allium_projects p1 ON m1.project_id = p1.id
JOIN allium_entities e2 ON e1.name = e2.name AND e1.id != e2.id
JOIN allium_modules m2 ON e2.module_id = m2.id
JOIN allium_projects p2 ON m2.project_id = p2.id
WHERE p1.id < p2.id  -- avoid duplicate pairs
  AND e1.fields::TEXT != e2.fields::TEXT;  -- different field definitions
```

### Library Specs

Allium describes "Library Specs" as reusable contracts for common integration patterns (OAuth, payment processing, etc.). CockroachDB can host a searchable library:

```sql
-- Tag modules as library specs
ALTER TABLE allium_modules ADD COLUMN is_library BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE allium_modules ADD COLUMN library_tags JSONB NOT NULL DEFAULT '[]'::JSONB;

CREATE INVERTED INDEX idx_modules_library_tags ON allium_modules(library_tags);

-- Search for library specs by tag
SELECT m.file_path, p.name AS project, m.library_tags
FROM allium_modules m
JOIN allium_projects p ON m.project_id = p.id
WHERE m.is_library = TRUE
  AND m.library_tags @> '["authentication"]';
```

---

## 10. Distributed Spec Sharing

### The Scenario

An organization has multiple teams working on different services. Each team maintains its own Allium specs. Cross-team contracts (e.g., service A's surface contract with service B) need to be visible, consistent, and version-tracked across team boundaries.

### CockroachDB's Distributed Nature

CockroachDB's architecture directly serves this use case:

**Multi-region deployment**: If teams are geographically distributed, CockroachDB can pin spec data to the team's region for low-latency reads while maintaining global consistency:

```sql
-- Make the specs table regional by row
ALTER TABLE allium_modules ADD COLUMN region crdb_internal_region NOT NULL DEFAULT 'us-east-1';
ALTER TABLE allium_modules SET LOCALITY REGIONAL BY ROW AS region;

-- Global reference tables (rarely updated, read everywhere)
ALTER TABLE allium_projects SET LOCALITY GLOBAL;
```

**Concurrent access**: Multiple Tend and Weed agents across different teams can query and update specs simultaneously without coordination. CockroachDB's serializable transactions prevent conflicting updates from corrupting the data.

**Survivability**: Spec data survives node failures. No team loses access to specs because a single server went down.

### Access Control

CockroachDB's Row-Level Security can enforce that teams only modify their own specs while reading anyone's:

```sql
-- Add team ownership
ALTER TABLE allium_projects ADD COLUMN team_id TEXT NOT NULL DEFAULT '';

-- Policy: anyone can read, only owning team can write
CREATE POLICY project_read ON allium_projects FOR SELECT USING (true);
CREATE POLICY project_write ON allium_projects FOR ALL
    USING (team_id = current_setting('app.team_id'));

ALTER TABLE allium_projects ENABLE ROW LEVEL SECURITY;
```

### Practical Considerations

Distributed spec sharing is only valuable at organizational scale. A single team with a handful of services gains nothing from geographic distribution. The benefit emerges when:

- Multiple teams need to reference each other's specs
- Cross-team contract changes need to be detected automatically
- Spec search across the entire organization is needed
- Audit trails for spec changes must be centralized

---

## 11. hive-server as Mediator

### Architecture

hive-server sits between Allium tooling and CockroachDB. It provides:

1. **Ingestion endpoint**: Accepts `.allium` source text, invokes the Rust parser, decomposes the AST, and stores everything transactionally.
2. **Query API**: Exposes SQL-backed queries as REST endpoints for agents.
3. **Trace collection**: Receives execution trace events from application middleware.
4. **Change notification**: Uses CockroachDB changefeeds or polling to notify agents of spec changes.

### Proposed hive-server Extensions

```go
// internal/handlers/specs.go

// New REST routes under /api/v1/specs
r.Route("/api/v1/specs", func(r chi.Router) {
    // Projects
    r.Get("/projects", a.handleListProjects)
    r.Post("/projects", a.handleCreateProject)

    // Modules (spec files)
    r.Get("/projects/{project}/modules", a.handleListModules)
    r.Post("/projects/{project}/modules", a.handleIngestModule)    // accepts .allium source
    r.Get("/modules/{id}", a.handleGetModule)
    r.Get("/modules/{id}/source", a.handleGetModuleSource)         // returns raw .allium text
    r.Get("/modules/{id}/ast", a.handleGetModuleAST)               // returns parsed AST JSON

    // Cross-cutting queries
    r.Get("/entities", a.handleQueryEntities)                      // ?project=X&unused=true
    r.Get("/rules", a.handleQueryRules)                            // ?entity=Y&trigger_type=temporal
    r.Get("/surfaces", a.handleQuerySurfaces)                      // ?contract=Codec&facing=Admin
    r.Get("/contracts", a.handleQueryContracts)
    r.Get("/cross-refs", a.handleQueryCrossRefs)                   // ?target=User
    r.Get("/dependencies", a.handleQueryDependencies)              // ?project=X&direction=reverse

    // Evolution
    r.Get("/events", a.handleQueryEvents)                          // ?module={id}&since=...&construct=User

    // Traces
    r.Post("/traces", a.handleCreateTrace)
    r.Post("/traces/{id}/events", a.handleAppendTraceEvents)
    r.Get("/traces/{id}/validate", a.handleValidateTrace)
    r.Get("/traces/violations", a.handleQueryViolations)
})
```

### Ingestion Pipeline

The ingestion of an `.allium` file into CockroachDB follows this pipeline:

```
.allium file (source text)
    |
    v
Rust parser (allium-cli --format json)
    |
    v
Typed AST (JSON)
    |
    v
hive-server ingestion handler:
    1. Compute source hash (SHA-256)
    2. Check if module exists and hash differs (skip if unchanged)
    3. Begin CockroachDB transaction
    4. Upsert allium_modules (source_text, parsed_ast, source_hash)
    5. Decompose AST into construct tables
    6. Diff old vs new constructs, record events
    7. Resolve cross-references (populate allium_cross_refs)
    8. Commit transaction
    9. Return updated module with construct counts
```

The Rust parser is invoked as a subprocess. If `allium-cli` is not available, hive-server falls back to storing the raw source without decomposition (the `parsed_ast` column would be empty, and construct tables would not be populated). This graceful degradation means the system works without the Rust toolchain installed, just with reduced query capability.

### Store Interface Extension

Following hive-server's existing pattern of a `Store` interface in handlers:

```go
// handlers/specs.go

type SpecStore interface {
    // Projects
    CreateProject(ctx context.Context, name, description string) (*SpecProject, error)
    ListProjects(ctx context.Context) ([]*SpecProject, error)

    // Modules
    IngestModule(ctx context.Context, projectID uuid.UUID, filePath, source string, ast json.RawMessage) (*SpecModule, error)
    GetModule(ctx context.Context, id uuid.UUID) (*SpecModule, error)
    ListModules(ctx context.Context, projectID uuid.UUID) ([]*SpecModule, error)

    // Queries
    QueryEntities(ctx context.Context, filter EntityFilter) ([]*SpecEntity, error)
    QueryRules(ctx context.Context, filter RuleFilter) ([]*SpecRule, error)
    QueryCrossRefs(ctx context.Context, target string) ([]*CrossRef, error)

    // Events
    QueryEvents(ctx context.Context, filter EventFilter) ([]*SpecEvent, error)

    // Traces
    CreateTrace(ctx context.Context, trace *Trace) (*Trace, error)
    AppendTraceEvents(ctx context.Context, traceID uuid.UUID, events []TraceEvent) error
    ValidateTrace(ctx context.Context, traceID uuid.UUID) (*ValidationResult, error)
}
```

This interface can have both a CockroachDB implementation (for production) and a mock (for testing), consistent with hive-server's existing test patterns.

### Sync Between Files and Database

The database is a derived view over `.allium` files, not the source of truth. The sync mechanism:

1. **Push-based**: CI/CD pipelines or git hooks POST updated `.allium` files to hive-server after each commit.
2. **Pull-based**: A periodic job scans repositories for `.allium` files and ingests any that have changed (based on source hash comparison).
3. **Agent-initiated**: Tend and Weed agents POST specs to hive-server as part of their workflow.

The `source_hash` column in `allium_modules` enables idempotent ingestion. Re-posting an unchanged file is a no-op.

---

## 12. Tradeoffs

### What Is Gained

| Capability                     | Without CockroachDB                     | With CockroachDB                                        |
| ------------------------------ | --------------------------------------- | ------------------------------------------------------- |
| **Cross-spec queries**         | Parse every file, build in-memory index | SQL query with indexed lookups                          |
| **Impact analysis**            | Manual grep across repositories         | `SELECT FROM allium_cross_refs WHERE target = 'User'`   |
| **Change history**             | `git log` (file-level)                  | Construct-level event log with agent attribution        |
| **Trace validation**           | Planned but no storage solution         | Full trace storage with violation tracking              |
| **Spec discovery**             | Browse file trees                       | Search by entity, contract, tag, or pattern             |
| **Multi-team sharing**         | Git remote access + parsing             | Query API with access control                           |
| **Consistency during updates** | File write (all-or-nothing on one file) | Multi-table ACID transaction                            |
| **Dead construct detection**   | Manual review                           | SQL queries for unreferenced entities, unused contracts |

### What Is Lost

| Concern                     | Detail                                                                                                                           |
| --------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| **Simplicity**              | `.allium` files are self-contained. Adding a database introduces a synchronization problem and a new failure mode.               |
| **Offline access**          | Files work without network connectivity. A database requires the server to be running.                                           |
| **Source of truth clarity** | Today, the `.allium` file is definitively the spec. With a database, there are two representations that can diverge.             |
| **Tooling independence**    | Allium's strength is that it requires no infrastructure. Adding CockroachDB creates an infrastructure dependency.                |
| **LLM context cost**        | Agents that currently read a single `.allium` file now need to understand an API, make HTTP calls, and interpret JSON responses. |

### Complexity Added

| Component                        | Effort                                             | Risk                                                 |
| -------------------------------- | -------------------------------------------------- | ---------------------------------------------------- |
| **Schema design and migrations** | Medium: ~15 tables, standard SQL                   | Low: schema is straightforward                       |
| **Ingestion pipeline**           | Medium: Rust parser integration, AST decomposition | Medium: parser subprocess management, error handling |
| **Sync mechanism**               | Medium: git hooks or periodic jobs                 | Medium: drift between files and database             |
| **API endpoints**                | Medium: ~20 new REST endpoints                     | Low: follows existing hive-server patterns           |
| **Event sourcing**               | Medium: diff logic, event generation               | Low: append-only, no complex state machines          |
| **Trace validation**             | High: spec-to-trace comparison logic               | High: semantic interpretation of spec constructs     |
| **CockroachDB operations**       | Medium: cluster setup, monitoring, backups         | Medium: new infrastructure component                 |

### Decision Framework

**Use the database when:**

- The organization has more than ~10 `.allium` files across multiple projects
- Cross-team spec sharing is needed
- Runtime trace validation is desired
- Spec change auditing is required
- Agents need to answer cross-spec questions frequently

**Skip the database when:**

- A single project with a handful of specs
- One developer or one team
- LLM agents handle all spec queries via direct file reading
- The organization is not ready for additional infrastructure

### The Middle Ground

A pragmatic path: start with the `allium_modules` table alone, storing source text and parsed ASTs without decomposing into per-construct tables. This gives:

- Basic spec storage and version tracking
- Full AST queries via JSONB path expressions
- A foundation to add construct tables incrementally as query needs emerge

This avoids the upfront cost of designing and maintaining 15 tables while still providing queryable spec storage.

---

## 13. Implementation Roadmap

### Phase 0: Foundation (No Allium-Specific Changes)

Complete the CockroachDB store implementation for hive-server's existing tables (memory, tasks, agents). This is already planned in the CockroachDB integration plan. It establishes the pgx/pgxpool/crdbpgx patterns that the Allium tables will follow.

### Phase 1: Minimal Spec Storage

- Add `allium_projects` and `allium_modules` tables
- Implement ingestion endpoint (POST source text, store with hash)
- Implement retrieval endpoints (GET source, GET AST)
- No construct decomposition yet; queries use JSONB path expressions on `parsed_ast`
- Integrate `allium-cli` as optional subprocess for parsing

### Phase 2: Construct Decomposition

- Add entity, rule, surface, contract, actor, invariant, config tables
- Build AST decomposition logic in Go
- Add cross-reference table and population logic
- Implement query endpoints for each construct type
- Add inverted indexes on JSONB columns

### Phase 3: Event Sourcing

- Add event log table
- Implement diff-and-record logic during module ingestion
- Add event query endpoints
- Build evolution timeline views

### Phase 4: Trace Validation

- Add trace, trace_events, and trace_violations tables
- Design trace event format based on Allium surface definitions
- Implement trace ingestion and validation logic
- Add violation query endpoints
- Integrate with application middleware (language-specific)

### Phase 5: Cross-Project Composition

- Add dependency tracking table
- Implement dependency resolution logic
- Add library spec tagging and search
- Build dependency graph queries
- Consider multi-region data placement if teams are geographically distributed

Each phase is independently valuable and can be deployed without completing subsequent phases. The ingestion pipeline in Phase 1 is the critical foundation; everything else builds on having specs in the database.
