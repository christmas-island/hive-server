# Deep Analysis: Gel DB as a Backend for Allium Behavioral Specifications

## Premise

Allium is a behavioral specification language -- `.allium` files that describe what a system does and why. Gel is a graph-relational database with a rich type system, link-based relationships, and path-traversal queries. This analysis explores what happens when you store and query Allium's constructs in Gel rather than leaving them as flat files parsed ad hoc by LLMs and CLI tools.

The central question: **Can Gel turn Allium from a passive document format into an actively queryable behavioral knowledge graph, and is the complexity worth it?**

---

## 1. Mapping Allium Constructs to Gel's Type System

Allium's constructs have natural analogs in Gel's schema definition language. The correspondence is remarkably close because both systems think in terms of typed entities with named relationships.

### 1.1 Core Entity Mapping

| Allium Construct     | Gel Type Pattern                                                  | Notes                                          |
| -------------------- | ----------------------------------------------------------------- | ---------------------------------------------- |
| `entity`             | `type` with properties and links                                  | Direct mapping                                 |
| `value type`         | `type` without identity semantics (or scalar)                     | Gel types always have `id`; needs convention   |
| `variant` (sum type) | Abstract type + concrete subtypes                                 | Gel's inheritance models discriminated unions  |
| `rule`               | `type Rule` with links to triggers, preconditions, postconditions | Rules become first-class queryable objects     |
| `surface`            | `type Surface` with links to actors, contracts, exposed fields    | Boundary contracts become graph nodes          |
| `contract`           | `type Contract` with multi links to obligations                   | Direction-agnostic obligations                 |
| `actor`              | `type Actor` with links to accessible surfaces                    | Actor-surface relationships become traversable |
| `invariant`          | `type Invariant` with link to owning scope                        | Both prose and expression forms                |
| `config`             | `type ConfigParam` with default values                            | Config parameters as queryable entities        |
| `enumeration`        | `scalar type extending enum<...>`                                 | Direct mapping                                 |

### 1.2 Proposed Gel Schema for Allium's AST

```sdl
# ─── Module / File Level ───

module allium {

    scalar type ConstructKind extending enum<
        'entity', 'value_type', 'rule', 'surface', 'contract',
        'actor', 'invariant', 'config', 'enumeration', 'default'
    >;

    scalar type TriggerKind extending enum<
        'external_stimulus', 'state_transition', 'state_becomes',
        'temporal', 'entity_created', 'derived_condition', 'chained'
    >;

    scalar type FieldKind extending enum<
        'required', 'optional', 'derived', 'computed', 'multi'
    >;

    scalar type InvariantForm extending enum<
        'prose', 'expression'
    >;

    scalar type MismatchClassification extending enum<
        'spec_bug', 'code_bug', 'aspirational', 'intentional_gap'
    >;

    # ─── Base Types ───

    abstract type HasTimestamps {
        required created_at: datetime { default := datetime_current(); };
        required updated_at: datetime { default := datetime_current(); };
    }

    abstract type HasSourceLocation {
        required file_path: str;
        required line_start: int32;
        required line_end: int32;
        source_text: str;  # raw Allium source for this construct
    }

    # ─── Specification (one per .allium file) ───

    type Spec extending HasTimestamps {
        required name: str { constraint exclusive; };
        required version: int16 { default := 2; };
        required file_path: str { constraint exclusive; };
        content_hash: str;  # SHA-256 of file contents
        git_sha: str;       # commit that introduced this version

        # Relationships to all constructs in this spec
        multi entities: Entity;
        multi value_types: ValueType;
        multi rules: Rule;
        multi surfaces: Surface;
        multi contracts: Contract;
        multi actors: Actor;
        multi invariants: Invariant;
        multi config_params: ConfigParam;
        multi enumerations: Enumeration;
        multi imports: SpecImport;
        multi open_questions: OpenQuestion;
        multi deferred: DeferredSpec;
    }

    type SpecImport {
        required source_spec: Spec;
        required target_spec: Spec;
        alias_name: str;
    }

    # ─── Entity and Fields ───

    type Entity extending HasTimestamps, HasSourceLocation {
        required name: str;
        description: str;
        multi fields: Field;
        multi relationships: Relationship;
        multi projections: Projection;
        multi derived_values: DerivedValue;
        multi local_invariants: Invariant;

        # Sum type support
        is_sum_type: bool { default := false; };
        discriminator_field: str;
        multi variants: Variant;

        # Back-link: which rules reference this entity
        multi referencing_rules := .<affected_entities[is Rule];

        # Back-link: which surfaces expose this entity
        multi exposed_by := .<exposed_entities[is Surface];
    }

    type Variant extending HasSourceLocation {
        required name: str;
        required parent_entity: Entity;
        multi fields: Field;
    }

    type Field extending HasSourceLocation {
        required name: str;
        required field_type: str;  # type expression as string
        required kind: FieldKind;
        is_optional: bool { default := false; };
        default_expr: str;
    }

    type Relationship extending HasSourceLocation {
        required name: str;
        required target_entity: Entity;
        cardinality: str;  # 'one', 'many', 'optional'
        backreference: str;  # 'with X = this' clause
    }

    type Projection extending HasSourceLocation {
        required name: str;
        required expression: str;
    }

    type DerivedValue extending HasSourceLocation {
        required name: str;
        required description: str;
        result_type: str;
    }

    type ValueType extending HasTimestamps, HasSourceLocation {
        required name: str;
        multi fields: Field;
    }

    # ─── Rules ───

    type Rule extending HasTimestamps, HasSourceLocation {
        required name: str;
        multi triggers: Trigger;
        multi preconditions: Precondition;
        multi postconditions: Postcondition;
        multi let_bindings: LetBinding;
        multi affected_entities: Entity;
    }

    type Trigger extending HasSourceLocation {
        required kind: TriggerKind;
        required expression: str;
        source_entity: Entity;
    }

    type Precondition extending HasSourceLocation {
        required expression: str;  # 'requires:' clause
    }

    type Postcondition extending HasSourceLocation {
        required expression: str;  # 'ensures:' clause
    }

    type LetBinding extending HasSourceLocation {
        required variable_name: str;
        required expression: str;
    }

    # ─── Surfaces ───

    type Surface extending HasTimestamps, HasSourceLocation {
        required name: str;
        required facing: Actor;
        context_entity: Entity;
        multi exposed_entities: Entity;
        multi exposed_fields: str;  # dot-path expressions
        multi provided_actions: ProvidedAction;
        multi demanded_contracts: Contract;
        multi guarantees: Invariant;
        timeout: str;
    }

    type ProvidedAction extending HasSourceLocation {
        required name: str;
        required signature: str;
        guard_expression: str;  # 'when' clause
    }

    # ─── Contracts ───

    type Contract extending HasTimestamps, HasSourceLocation {
        required name: str;
        multi obligations: ContractObligation;
        multi invariants: Invariant;
    }

    type ContractObligation extending HasSourceLocation {
        required name: str;
        required signature: str;  # typed function signature
    }

    # ─── Actors ───

    type Actor extending HasTimestamps, HasSourceLocation {
        required name: str;
        identity_mapping: str;
        scope_entity: Entity;  # 'within' clause

        # Which surfaces does this actor face?
        multi accessible_surfaces := .<facing[is Surface];
    }

    # ─── Invariants ───

    type Invariant extending HasTimestamps, HasSourceLocation {
        required name: str;
        required form: InvariantForm;
        prose: str;           # for @invariant prose annotations
        expression: str;      # for expression-bearing invariants
        owning_entity: Entity;
        owning_contract: Contract;
    }

    # ─── Config and Defaults ───

    type ConfigParam extending HasSourceLocation {
        required name: str;
        required param_type: str;
        default_value: str;
        arithmetic_default: str;
    }

    type Enumeration extending HasSourceLocation {
        required name: str;
        multi literals: str;
    }

    # ─── Deferred and Open ───

    type DeferredSpec extending HasSourceLocation {
        required name: str;
        required reason: str;
    }

    type OpenQuestion extending HasSourceLocation {
        required text: str;
        resolved: bool { default := false; };
        resolution: str;
    }

    # ─── Versioning / History ───

    type SpecVersion extending HasTimestamps {
        required spec: Spec;
        required version_number: int32;
        required git_sha: str;
        required content_hash: str;
        required snapshot: json;  # full AST at this version
        change_summary: str;
        author: str;
    }

    # ─── Drift Detection ───

    type DriftReport extending HasTimestamps {
        required spec: Spec;
        required git_sha: str;
        multi mismatches: DriftMismatch;
        status: str;  # 'clean', 'drifted', 'reconciled'
    }

    type DriftMismatch extending HasTimestamps {
        required construct_name: str;
        required construct_kind: ConstructKind;
        required classification: MismatchClassification;
        required description: str;
        spec_text: str;
        code_text: str;
        resolved: bool { default := false; };
    }
}
```

---

## 2. EdgeQL Path Traversal for Spec Relationships

This is where Gel's design shines brightest. Allium's constructs form a directed graph of dependencies: entities are referenced by rules, rules trigger postconditions that affect other entities, surfaces expose entities and demand contracts, contracts carry invariants. In flat files, tracing these chains requires parsing. In Gel, it is a path expression.

### 2.1 Entity Impact Analysis

"Which rules affect the `User` entity, and what surfaces expose it?"

```edgeql
SELECT allium::Entity {
    name,
    referencing_rules: {
        name,
        triggers: { expression },
        postconditions: { expression }
    },
    exposed_by: {
        name,
        facing: { name },
        demanded_contracts: {
            name,
            invariants: { name, prose }
        }
    }
}
FILTER .name = 'User';
```

A single query traverses entity -> rules -> triggers/postconditions AND entity -> surfaces -> actors -> contracts -> invariants. This would require multiple file parses and cross-referencing in the flat-file model.

### 2.2 Full Dependency Chain: Entity -> Rules -> Surfaces -> Contracts

"For a given entity, trace the full behavioral chain."

```edgeql
WITH target := (SELECT allium::Entity FILTER .name = 'CircuitBreaker')
SELECT {
    entity := target { name, fields: { name, field_type } },

    rules := (
        SELECT allium::Rule
        FILTER target IN .affected_entities
    ) {
        name,
        triggers: { kind, expression },
        preconditions: { expression },
        postconditions: { expression }
    },

    surfaces := (
        SELECT allium::Surface
        FILTER target IN .exposed_entities
    ) {
        name,
        facing: { name },
        demanded_contracts: {
            name,
            obligations: { name, signature },
            invariants: { name, prose, expression }
        },
        guarantees: { name, prose }
    }
};
```

### 2.3 Cross-Spec Reference Tracing

"Which specs import the `Auth` spec, and which of their rules reference entities defined in Auth?"

```edgeql
WITH auth_spec := (SELECT allium::Spec FILTER .name = 'Auth'),
     auth_entities := auth_spec.entities
SELECT allium::SpecImport {
    source_spec: { name },
    rules_using_auth := (
        SELECT .source_spec.rules
        FILTER count(.affected_entities INTERSECT auth_entities) > 0
    ) { name }
}
FILTER .target_spec = auth_spec;
```

### 2.4 Invariant Coverage Map

"Which entities lack invariants, either directly or via contract?"

```edgeql
SELECT allium::Entity {
    name,
    has_local_invariants := count(.local_invariants) > 0,
    has_contract_invariants := count(
        .exposed_by.demanded_contracts.invariants
    ) > 0
}
FILTER
    count(.local_invariants) = 0
    AND count(.exposed_by.demanded_contracts.invariants) = 0;
```

### 2.5 Rule Conflict Detection

"Find rules with overlapping triggers on the same entity that might conflict."

```edgeql
WITH
    entity_rules := (
        SELECT allium::Entity {
            name,
            rules := .referencing_rules {
                name,
                triggers: { kind, expression }
            }
        }
        FILTER count(.referencing_rules) > 1
    )
SELECT entity_rules {
    name,
    rule_pairs := (
        FOR r1 IN .rules
        UNION (
            FOR r2 IN .rules
            FILTER r1 != r2
                AND count(r1.triggers.kind INTERSECT r2.triggers.kind) > 0
            UNION {
                rule_a := r1.name,
                rule_b := r2.name,
                shared_trigger_kinds := r1.triggers.kind INTERSECT r2.triggers.kind
            }
        )
    )
};
```

---

## 3. How Gel's Links Model Allium's Behavioral Relationships

Allium describes several kinds of relationships that translate directly to Gel links:

### 3.1 Relationship Types

**Structural ownership** (entity owns fields, spec owns entities):

```sdl
type Spec {
    multi entities: Entity {
        on source delete delete target;  # cascade: spec deletion removes entities
    };
}
```

**Behavioral reference** (rule references entity it affects):

```sdl
type Rule {
    multi affected_entities: Entity;  # no cascade: deleting a rule doesn't delete entities
}
```

**Boundary facing** (surface faces an actor):

```sdl
type Surface {
    required facing: Actor;  # single link: a surface faces exactly one actor
}
```

**Contractual demand** (surface demands contract):

```sdl
type Surface {
    multi demanded_contracts: Contract;  # many-to-many through link
}
```

**Computed back-links** (which rules reference this entity):

```sdl
type Entity {
    multi referencing_rules := .<affected_entities[is Rule];
}
```

These back-links are the key advantage. In flat files, finding "what rules affect entity X" requires scanning every rule in every file. In Gel, it is a pre-computed reverse traversal.

### 3.2 The Graph Shape

The behavioral graph has a clear topology:

```
Spec
 ├── Entity ←──── Rule (affects)
 │    ├── Field        ├── Trigger
 │    ├── Relationship ├── Precondition
 │    ├── Variant      └── Postcondition
 │    └── Invariant
 ├── Surface ──→ Actor
 │    ├── exposed: Entity
 │    ├── demands: Contract
 │    │    ├── Obligation
 │    │    └── Invariant
 │    └── guarantees: Invariant
 └── Config
```

Every edge in this graph is a Gel link. Every node is a Gel type. Path expressions traverse the graph. This is exactly the data model Gel was designed for.

---

## 4. Programmatic Querying for Tend and Weed Agents

### 4.1 Current Problem

Today, Tend and Weed work by:

1. Reading `.allium` files as text
2. Passing them to an LLM for interpretation
3. Relying on LLM comprehension to extract structure

This has three problems: it consumes tokens, it is nondeterministic, and it cannot perform cross-file structural queries.

### 4.2 Tend (Spec Steward) with Gel

Tend creates, modifies, and restructures specs. With Gel as a backend, Tend can:

**Check for naming conflicts before creating an entity:**

```edgeql
SELECT EXISTS (
    SELECT allium::Entity FILTER .name = 'PaymentIntent'
);
```

**Find all constructs that reference an entity before renaming it:**

```edgeql
WITH target := (SELECT allium::Entity FILTER .name = 'OldName')
SELECT {
    rules := target.referencing_rules { name },
    surfaces := target.exposed_by { name },
    relationships := (
        SELECT allium::Relationship FILTER .target_entity = target
    ) { name },
    variants := (
        SELECT allium::Variant FILTER .parent_entity = target
    ) { name }
};
```

**Validate structural completeness after modification:**

```edgeql
# Rules without postconditions (violates Allium's "at least one ensures" rule)
SELECT allium::Rule { name, file_path }
FILTER count(.postconditions) = 0;

# Relationships without backreferences
SELECT allium::Relationship { name, target_entity: { name } }
FILTER .backreference = '' OR NOT EXISTS .backreference;

# Sum types with missing variants
SELECT allium::Entity {
    name,
    discriminator_field,
    variant_count := count(.variants)
}
FILTER .is_sum_type = true AND count(.variants) = 0;
```

### 4.3 Weed (Drift Detector) with Gel

Weed compares specs against code. With Gel, it can:

**Store and query drift history:**

```edgeql
# Insert a drift report
INSERT allium::DriftReport {
    spec := (SELECT allium::Spec FILTER .name = 'Auth'),
    git_sha := 'abc123f',
    status := 'drifted',
    mismatches := {
        (INSERT allium::DriftMismatch {
            construct_name := 'RequestPasswordReset',
            construct_kind := allium::ConstructKind.rule,
            classification := allium::MismatchClassification.code_bug,
            description := 'Rule requires email sent, but implementation silently drops on SMTP failure',
            spec_text := 'ensures: email sent to user',
            code_text := 'smtp.Send(email) // error swallowed'
        })
    }
};
```

**Track drift trends over time:**

```edgeql
SELECT allium::DriftReport {
    created_at,
    spec: { name },
    git_sha,
    status,
    mismatch_count := count(.mismatches),
    by_classification := (
        GROUP .mismatches
        USING classification := .classification
        BY classification
    ) { key := .classification, count := count(.elements) }
}
ORDER BY .created_at DESC
LIMIT 20;
```

**Find chronically drifting constructs:**

```edgeql
SELECT allium::DriftMismatch {
    construct_name,
    construct_kind,
    drift_count := count(allium::DriftMismatch
        FILTER .construct_name = allium::DriftMismatch.construct_name
    )
}
FILTER NOT .resolved
ORDER BY .drift_count DESC;
```

### 4.4 Token Economics

A typical Allium spec file might be 200-500 lines. An LLM reading 10 spec files to answer "which rules affect the User entity" consumes 2,000-5,000 lines of context. The equivalent EdgeQL query returns a structured JSON response of perhaps 50 lines. For agents that run repeatedly, the token savings are substantial -- and the answers are deterministic.

---

## 5. Addressing Allium's Lack of Runtime Enforcement

Allium explicitly has no runtime. But Gel's constraint system can enforce structural invariants that the Allium CLI currently validates only syntactically.

### 5.1 Gel Constraints as Spec Invariants

```sdl
type Rule extending HasTimestamps, HasSourceLocation {
    required name: str;
    multi triggers: Trigger;
    multi postconditions: Postcondition;

    # Enforce Allium's structural requirement:
    # every rule needs at least one trigger and one ensures clause
    constraint expression on (count(.triggers) >= 1);
    constraint expression on (count(.postconditions) >= 1);
}

type Relationship extending HasSourceLocation {
    required name: str;
    required target_entity: Entity;
    # Allium requires backreferences on relationships
    required backreference: str {
        constraint min_len_value(1);
    };
}

type Entity {
    required name: str;
    # Names must be PascalCase
    constraint expression on (
        re_test(r'^[A-Z][a-zA-Z0-9]*$', .name)
    );
}

type Field {
    required name: str;
    # Names must be snake_case
    constraint expression on (
        re_test(r'^[a-z][a-z0-9_]*$', .name)
    );
}
```

### 5.2 What This Does and Does Not Solve

**Enforced at write time:**

- Naming conventions (PascalCase entities, snake_case fields)
- Structural completeness (rules have triggers and postconditions)
- Referential integrity (relationships point to existing entities)
- Uniqueness (no duplicate entity names within a spec)
- Required fields present

**Still not enforced (and probably cannot be):**

- Semantic consistency (do two rules contradict each other?)
- Logical completeness (are all state transitions covered?)
- Domain correctness (does the spec accurately describe the business?)
- Cross-spec semantic coherence

The constraint system handles the mechanical checks that the Allium CLI already does, but it does them at insert time with database-level guarantees rather than as a separate validation pass. It does not replace the need for LLM-mediated semantic review.

---

## 6. Spec Versioning and History

### 6.1 Version Tracking Schema

The `SpecVersion` type in the schema above captures snapshots:

```edgeql
# Record a new version when a spec changes
INSERT allium::SpecVersion {
    spec := (SELECT allium::Spec FILTER .name = 'Auth'),
    version_number := 3,
    git_sha := 'def456a',
    content_hash := 'sha256:abc...',
    snapshot := <json>$ast_json,
    change_summary := 'Added MFA support to login flow',
    author := 'tend-agent'
};
```

### 6.2 Diff Queries

"What changed between version 2 and version 3 of the Auth spec?"

```edgeql
WITH
    v2 := (SELECT allium::SpecVersion
           FILTER .spec.name = 'Auth' AND .version_number = 2),
    v3 := (SELECT allium::SpecVersion
           FILTER .spec.name = 'Auth' AND .version_number = 3)
SELECT {
    old_version := v2 { version_number, git_sha, change_summary, created_at },
    new_version := v3 { version_number, git_sha, change_summary, created_at },
    # Snapshot diffs would need application-level logic
    # but metadata comparison is immediate
};
```

For structural diffs (added/removed/modified entities, rules, etc.), the snapshots stored as JSON can be compared application-side. Gel stores them; the comparison logic lives in the Tend or Weed agent.

### 6.3 History Timeline

```edgeql
SELECT allium::SpecVersion {
    version_number,
    created_at,
    git_sha,
    change_summary,
    author
}
FILTER .spec.name = 'Auth'
ORDER BY .version_number;
```

### 6.4 Advantage Over Git Alone

Git tracks file-level changes. Gel tracks construct-level changes. "When did the `RequestPasswordReset` rule last change?" is a git log + grep operation on flat files. With Gel, it becomes a direct query against the version history, and the change can be traced through the dependency graph to see downstream impact.

---

## 7. Cross-Project Spec Composition and Reuse

### 7.1 The Multi-Project Graph

Allium's `use` declarations create cross-file dependencies. With Gel, these become first-class graph edges spanning projects:

```sdl
type Project extending HasTimestamps {
    required name: str { constraint exclusive; };
    required repository_url: str;
    multi specs: Spec {
        on source delete delete target;
    };
}
```

```edgeql
# Find all projects that depend on the OAuth library spec
WITH oauth := (SELECT allium::Spec FILTER .name = 'OAuthLibrary')
SELECT allium::Project {
    name,
    dependent_specs := (
        SELECT .specs
        FILTER oauth IN .imports.target_spec
    ) { name }
};
```

### 7.2 Reusable Library Specs

Allium mentions library specs for common patterns (OAuth, payment processing, email delivery). With Gel:

```edgeql
# Find all contracts available as library components
SELECT allium::Contract {
    name,
    obligations: { name, signature },
    used_by := count(.<demanded_contracts[is allium::Surface]),
    specs := .<contracts[is allium::Spec] { name }
}
ORDER BY .used_by DESC;
```

### 7.3 Cross-Project Impact Analysis

"If I change the `Codec` contract, which projects are affected?"

```edgeql
WITH codec := (SELECT allium::Contract FILTER .name = 'Codec')
SELECT DISTINCT (
    SELECT allium::Spec
    FILTER codec IN .surfaces.demanded_contracts
) {
    name,
    file_path,
    affected_surfaces := (
        SELECT .surfaces
        FILTER codec IN .demanded_contracts
    ) { name }
};
```

This kind of cross-project impact analysis is essentially impossible with flat files unless you build custom tooling to parse and cross-reference all specs across all repositories.

---

## 8. Tradeoffs

### 8.1 What Is Gained

1. **Deterministic structural queries.** "What rules affect entity X" becomes a database query instead of an LLM interpretation. Answers are exact, reproducible, and fast.

2. **Cross-file and cross-project analysis.** The behavioral graph spans all specs. Impact analysis, dependency tracing, and contradiction detection become graph traversals.

3. **Agent efficiency.** Tend and Weed can make precise queries instead of consuming entire spec files as context. This reduces token usage by an order of magnitude for structural operations.

4. **Constraint enforcement at write time.** Naming conventions, structural requirements, and referential integrity are enforced by the database, not by a separate validation pass.

5. **First-class versioning.** Spec history is queryable at the construct level, not just the file level.

6. **Drift analytics.** Historical drift reports enable trend analysis: which specs drift most, which constructs are chronically misaligned, which classifications dominate.

7. **Composition queries.** Finding reusable contracts, tracing import chains, and assessing cross-project impact become tractable.

### 8.2 What Is Lost

1. **Simplicity.** Allium's current model is a text file and a CLI. Adding Gel means running a database server (1 GB RAM minimum), managing migrations, and maintaining a sync pipeline between `.allium` files and the database.

2. **Self-contained portability.** A `.allium` file can be dropped into any project and read by any LLM. Once specs live in Gel, you need the database to query them structurally. The files remain the source of truth, but the queryable representation depends on infrastructure.

3. **Authoring simplicity.** Developers write `.allium` files in their editor. If the database is the queryable layer, there must be a reliable pipeline from file edits to database state. This is a sync problem that does not currently exist.

4. **LLM-native reading.** LLMs can read `.allium` files directly. They cannot (currently) issue EdgeQL queries. The database serves agents and tooling, not direct LLM consumption. This creates two consumers with different access patterns.

### 8.3 Complexity Added

1. **Sync pipeline.** Every `.allium` file change must be parsed (via the Rust parser), transformed to Gel inserts/updates, and applied. This pipeline must be idempotent and handle deletions (removed constructs, renamed entities).

2. **Schema evolution.** As Allium's language evolves (v2 -> v3), the Gel schema must evolve with it. Two migration systems (Allium's language versions and Gel's schema migrations) must stay coordinated.

3. **Operational overhead.** Gel server process, PostgreSQL backend, migration management, backup strategy. For a specification language that deliberately avoids runtime complexity, this is ironic.

4. **Dual source of truth risk.** If `.allium` files and Gel state diverge, which is authoritative? The files must be canonical, making Gel a derived/materialized view. This must be enforced operationally.

---

## 9. Practical Integration: hive-server as Mediator

### 9.1 Architecture

```
.allium files (source of truth)
    │
    ▼
allium-cli parse --json (Rust parser, produces AST JSON)
    │
    ▼
hive-server /api/v1/specs/sync (receives AST JSON, upserts into Gel)
    │
    ▼
Gel DB (queryable behavioral graph)
    │
    ▼
hive-server /api/v1/specs/query (agents query via REST, EdgeQL underneath)
    │
    ▼
Tend / Weed agents (programmatic spec access)
```

### 9.2 hive-server API Extensions

```
POST   /api/v1/specs/sync          # Ingest parsed AST JSON for a spec
GET    /api/v1/specs                # List all specs
GET    /api/v1/specs/:name          # Get spec with full construct graph
GET    /api/v1/specs/:name/entities # Entities in a spec
GET    /api/v1/specs/:name/rules    # Rules in a spec
GET    /api/v1/specs/:name/impact/:entity  # Impact analysis for an entity

POST   /api/v1/specs/query          # Free-form EdgeQL query (for agents)

GET    /api/v1/specs/:name/history  # Version history
POST   /api/v1/specs/:name/drift    # Submit drift report
GET    /api/v1/specs/:name/drift    # Get drift history

GET    /api/v1/specs/graph/contracts      # Cross-spec contract usage
GET    /api/v1/specs/graph/dependencies   # Import dependency graph
GET    /api/v1/specs/graph/impact/:name   # Cross-project impact analysis
```

### 9.3 Sync Pipeline Implementation

The sync endpoint in hive-server would:

1. Accept the JSON AST from `allium-cli parse --json`
2. Compute a content hash for change detection
3. Inside a Gel transaction:
   a. Upsert the `Spec` object
   b. Diff current constructs against incoming AST
   c. Delete removed constructs, update modified ones, insert new ones
   d. Create a `SpecVersion` snapshot
4. Return a sync report (added/modified/deleted counts)

```go
// Sketch of the sync handler
func (h *SpecHandler) SyncSpec(w http.ResponseWriter, r *http.Request) {
    var ast AlliumAST
    if err := json.NewDecoder(r.Body).Decode(&ast); err != nil {
        http.Error(w, "invalid AST JSON", http.StatusBadRequest)
        return
    }

    report, err := h.store.SyncSpec(r.Context(), ast)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    json.NewEncoder(w).Encode(report)
}
```

### 9.4 Store Interface Extension

Following hive-server's existing pattern of a `Store` interface:

```go
type SpecStore interface {
    SyncSpec(ctx context.Context, ast AlliumAST) (*SyncReport, error)
    GetSpec(ctx context.Context, name string) (*Spec, error)
    ListSpecs(ctx context.Context) ([]SpecSummary, error)
    QueryImpact(ctx context.Context, specName, entityName string) (*ImpactReport, error)
    RecordDrift(ctx context.Context, report DriftReport) error
    GetDriftHistory(ctx context.Context, specName string) ([]DriftReport, error)
    QuerySpecs(ctx context.Context, edgeql string) (json.RawMessage, error)
}
```

The `GelStore` implementation would use `gel-go` to execute EdgeQL queries. The existing `SQLiteStore` would not implement `SpecStore` -- this is a Gel-only capability.

### 9.5 Agent Workflow

A Tend agent modifying a spec would:

1. Query hive-server for impact analysis before making changes
2. Edit the `.allium` file
3. Run `allium-cli parse --json` to validate
4. POST the AST to `/api/v1/specs/sync`
5. Query again to verify the graph is consistent

A Weed agent checking for drift would:

1. Query hive-server for the spec's entity/rule graph
2. Compare against implementation code
3. POST drift findings to `/api/v1/specs/:name/drift`
4. Query drift history to identify patterns

---

## 10. Recommendation

The integration is technically sound and the schema maps cleanly. Gel's graph-relational model is a natural fit for Allium's construct relationships, and EdgeQL's path traversal makes cross-spec queries straightforward.

However, the value depends entirely on scale:

**At small scale (1-5 specs, single project):** The overhead is not justified. Reading `.allium` files directly is simpler and sufficient. The sync pipeline adds complexity without proportional benefit.

**At medium scale (10-50 specs, 2-5 projects):** The cross-spec query capability becomes valuable. Impact analysis, contract reuse discovery, and drift trend tracking justify the infrastructure. This is the sweet spot for introducing Gel.

**At large scale (50+ specs, many projects):** This becomes nearly essential. Manual cross-referencing of spec files is intractable. The behavioral graph becomes the primary tool for understanding system-wide invariants and detecting cross-project regressions.

The recommended approach: **Start with the schema and sync pipeline, but defer the full REST API until the spec corpus grows large enough to need it.** The first milestone is `POST /api/v1/specs/sync` and `GET /api/v1/specs/:name` -- enough for agents to ingest and retrieve specs without parsing files. Cross-spec graph queries come next, when the graph has enough nodes to make traversal worthwhile.

The `.allium` files remain the source of truth. Gel is a materialized view of the behavioral graph. The sync pipeline is the critical path -- if it is reliable and fast, the rest follows naturally.
