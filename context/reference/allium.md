# Allium: Technology Brief

## What Allium Is

Allium is a **behavioral specification language** created by JUXT (MIT license, 2026). It captures _what_ a system should do and _why_, deliberately excluding _how_. It has no compiler and no runtime -- it is purely descriptive, interpreted by LLMs and humans directly from `.allium` files.

The name echoes "LLM" phonetically and continues the botanical naming tradition from BDD tools (Cucumber, Gherkin). The tagline is "Velocity through clarity."

## Problem Space

Allium addresses three failure modes in LLM-assisted development:

1. **Within-session drift**: LLMs pattern-match on their own outputs rather than original intent, causing meaning to shift during long sessions.
2. **Cross-session knowledge loss**: Assumptions and decisions evaporate when conversations end.
3. **Spec-code divergence**: Code captures implementation including bugs and expedient decisions. LLMs treat all of it as intended behavior. There is no way to distinguish intent from accident.

Allium provides a durable, structured artifact that persists across sessions and makes contradictions formally visible.

## Language Design and Syntax

### File Structure

Files start with a version marker (`-- allium: 2`) and organize into ordered sections:

```
use declarations -> given -> external entities -> value types ->
contracts -> enumerations -> entities/variants -> config -> defaults ->
rules -> invariants -> actor declarations -> surfaces -> deferred specs ->
open questions
```

Empty sections are omitted. Comments use `--`. Indentation is significant for blocks opened by colons.

### Naming Conventions

- **PascalCase**: entities, variants, rules, actors, surfaces, contracts, invariants
- **snake_case**: fields, config parameters, enum literals, relationships

### Core Constructs

**Entities** have identity and lifecycle. They declare fields, relationships, projections, and derived values:

```
entity CircuitBreaker {
    service: ExternalService
    status: closed | open | half_open
    failure_rate: derived from recent failures
    is_tripped: derived boolean
}
```

**Value types** are immutable data without identity, compared structurally.

**Sum types** (discriminated unions) use a discriminator field with pipe-separated variant names:

```
entity Node { kind: Branch | Leaf }
variant Branch : Node { children: List<Node?> }
```

**Rules** describe event-driven behavior with preconditions and outcomes:

```
rule RequestPasswordReset {
    when: UserRequestsPasswordReset(email)
    let user = User{email}
    requires: exists user
    requires: user.status in {active, locked}
    ensures: existing reset tokens invalidated
    ensures: new ResetToken created
    ensures: email sent to user
}
```

Trigger types: external stimulus, state transition (`transitions_to`), state becomes (`becomes`), temporal conditions, entity creation (`.created`), derived conditions, and chained triggers.

**Surfaces** define boundary contracts between system and external parties:

```
surface Dashboard {
    facing: Admin
    context: Workspace
    exposes: workspace.name, workspace.members
    provides:
        invite_member(email) when workspace.seats_available > 0
    contracts:
        demands Codec
    @guarantee DataFreshness
        -- data reflects state within 30 seconds
    timeout: session_expiry
}
```

**Contracts** are named, direction-agnostic obligations with typed signatures:

```
contract Codec {
    serialize: (value: Any) -> ByteArray
    @invariant Roundtrip
        -- deserialize(serialize(x)) = x
}
```

**Actors** are entity types that interact with surfaces, declared with identity mapping and optional scoping (`within` clause).

**Invariants** come in two forms: prose annotations (`@invariant`) in contracts, and expression-bearing (`invariant Name { expr }`) at module and entity scopes.

**Config** declares parameterized values referenced as `config.field`. Supports arithmetic defaults and cross-module qualified references.

**Defaults** create named seed entity instances available to all rules.

### Expression Language

- Navigation: field access, optional chaining (`?.`), null coalescing (`??`), `this`
- Collections: `in`, `any()`, `all()`, `where`, `for`, `.add()`, `.remove()`
- Boolean: `and`, `or`, `not`, `implies` (lowest precedence)
- Existence: `exists entity`, `not exists entity`
- Black box functions: pure, deterministic domain logic referenced but not defined (e.g., `hash()`, `verify()`)

### Primitive Types

`String`, `Integer`, `Decimal`, `Boolean`, `Timestamp`, `Duration`, `Set<T>`, `List<T>`, `T?` (optional)

### Modularity

Specifications import others via `use` with aliases. External entities act as type placeholders. Configuration from imported specs uses qualified names and can be overridden.

## Architecture and Key Components

### The Language Itself

Allium is a specification-only language. It produces `.allium` files that LLMs and humans read. There is no AST execution, no code generation (yet), and no runtime enforcement. The language reference document _is_ the specification.

### Tooling (allium-tools)

A separate repo (`juxt/allium-tools`) provides:

- **Rust parser** producing a typed AST and JSON output
- **Tree-sitter grammar** for editor syntax highlighting
- **CLI validator** (`allium-cli`) checking `.allium` files for parser errors and cross-entity reference issues
- Install via: `brew tap juxt/allium && brew install allium` or `cargo install allium-cli`

### LLM Integration Layer

Allium integrates directly into LLM coding assistants:

- **Claude Code**: via JUXT plugin marketplace (`/plugin install allium`)
- **40+ tools** (Cursor, Windsurf, Copilot, Aider, Continue): `npx skills add juxt/allium`

Three interaction modes:

- `/allium` -- interactive specification work
- `/allium:elicit` -- build specs through structured stakeholder conversation
- `/allium:distill` -- extract specs from existing code

### Delegated Agents

**Tend** (`.claude/agents/tend.md`): A specification steward that creates, modifies, and restructures `.allium` files. Pushes back on ambiguity rather than filling gaps with assumptions. Works only on spec files, not implementation code.

**Weed** (`.claude/agents/weed.md`): A divergence detector that compares `.allium` specs against implementation code. Three modes: check (report only), update spec (align spec to code), update code (align code to spec). Classifies mismatches as spec bug, code bug, aspirational design, or intentional gap.

## How It Relates to LLM Agents and AI Systems

Allium is fundamentally **LLM-native** -- designed to be consumed and produced by language models:

1. **Persistent intent layer**: Specs survive across sessions, preventing the knowledge loss that plagues multi-session LLM workflows.
2. **Disambiguation**: Formal structure forces contradictions to surface. When two rules have incompatible preconditions, the syntax exposes the conflict.
3. **Behavioral authority**: Specs distinguish intended behavior from accidental implementation, giving LLMs an authoritative reference for what code _should_ do.
4. **Agent specialization**: Tend and Weed agents demonstrate how spec-aware agents can maintain alignment between intent and implementation.
5. **Integration test generation**: The language is designed to generate integration and end-to-end tests (not unit tests) from rule preconditions and postconditions.

## Data Formats and Schemas

### File Format

Plain text `.allium` files with structured sections. Version-marked (`-- allium: 2`).

### Tooling Output

The Rust parser produces a **typed AST serialized as JSON**, suitable for downstream tool consumption.

### No Database Schema

Allium deliberately excludes database schemas, API designs, wire formats, and architectural choices. It operates at the domain behavior level only.

### Validation Schema

The CLI enforces structural rules:

- All referenced entities/fields must exist
- Relationships require backreferences (`with X = this`)
- Rules need at least one trigger and one `ensures` clause
- Sum type variants must all be declared
- Config references must be acyclic
- Temporal triggers on optional fields are flagged

## Integration Patterns

### Specification-First Development

Write `.allium` specs before or alongside code. LLMs reference specs during implementation to maintain alignment.

### Distillation (Code to Spec)

Extract specs from existing codebases. The methodology involves: mapping territory, extracting entity states, extracting transitions, finding temporal triggers, identifying external boundaries, abstracting implementation, and validating with stakeholders. Uses the "Would we rebuild this?" test to determine spec inclusion.

### Elicitation (Conversation to Spec)

Build specs through structured stakeholder interviews. Phases: scope definition, happy path flow, edge cases and errors, refinement.

### Drift Detection

Continuous comparison of specs against implementation via the Weed agent. Can run in check-only mode or actively reconcile in either direction.

### Library Specs

Integration patterns (OAuth, payment processing, email delivery, etc.) are extracted as reusable contracts importable via `use` declarations.

### Modular Composition

Specs reference other specs via git SHAs or content hashes for external dependencies, relative paths for local files. Config can be overridden at import sites.

## Runtime / Execution Model

**There is no runtime.** Allium is purely declarative and descriptive.

However, the roadmap includes planned tooling that would create runtime-adjacent capabilities:

1. **Structural validator** (in progress): Enforces language rules against the parsed AST.
2. **Property-based test generation** (planned): Translates rules into PBT specifications for frameworks to execute.
3. **Runtime trace validation** (planned): Derives trace event schemas from surface definitions to validate production behavior against contracts. Two architectures considered: standalone trace file validator, or language-specific middleware.
4. **Model checking bridge** (planned): Translates rule subgraphs into TLA+/P/Alloy for exhaustive state space exploration.
5. **Formal guarantee integration** (planned): Surfaces reference external verification artifacts (other Allium specs, Cedar policies, Kani proofs).

## Governance

Language changes go through a nine-member review panel representing: simplicity, machine reasoning, composability, readability, formal rigor, domain modeling, developer experience, creative ambition, and backward compatibility.

Two tracks: **Reviews** (fix rough edges, default: improve) and **Proposals** (new features, default: preserve stability).

## Strengths

- **Purpose-built for LLM workflows**: Solves real problems with context drift and knowledge loss in AI-assisted development.
- **Clean separation of concerns**: Behavioral intent stays separate from implementation, making both more maintainable.
- **Formal enough to catch contradictions**: Structured syntax surfaces ambiguities that prose specifications hide.
- **Flexible enough for humans**: Not so formal that it becomes inaccessible to non-technical stakeholders.
- **Composable**: Module system with imports, qualified references, and config overrides enables reusable spec libraries.
- **Tooling ecosystem**: Parser, CLI validator, tree-sitter grammar, plus native integration with 40+ LLM coding tools.
- **No lock-in**: Implementation-agnostic. Same spec could drive any language, framework, or architecture.
- **Additive evolution**: v1 to v2 migration required only a version marker change.

## Limitations

- **No runtime enforcement**: Specs are advisory. Nothing prevents code from diverging except manual or LLM-mediated checking.
- **No code generation**: Specs do not produce executable code, database migrations, API stubs, or test harnesses (yet -- PBT generation is on the roadmap).
- **LLM-dependent interpretation**: Without a formal semantics engine, correctness depends on LLM comprehension of the language reference.
- **No query capability**: You cannot query the spec for "which rules affect entity X" programmatically (the AST JSON output partially addresses this).
- **Purely behavioral**: Deliberately excludes performance requirements, data volumes, SLAs, security constraints beyond domain rules, and operational concerns.
- **Young ecosystem**: v2 released in 2026. Tooling roadmap items (test generation, trace validation, model checking) are still planned, not shipped.
- **Validation is syntactic, not semantic**: The CLI catches structural errors but cannot verify that specs are logically consistent or complete.

## Relevance to Database Technology Evaluation

When evaluating how database technologies (Gel DB, CockroachDB, Meilisearch) could enhance Allium's capabilities, key considerations include:

- **Spec storage and querying**: Allium specs are flat files. A database could enable querying across specs (find all rules affecting an entity, detect cross-spec contradictions, track spec evolution).
- **Trace validation**: The planned runtime trace validation feature needs a place to store and query execution traces against surface contracts. Time-series or document storage is a natural fit.
- **AST persistence**: The Rust parser produces JSON ASTs. Storing these in a document or graph database could enable cross-reference analysis, impact assessment, and spec-aware search.
- **Full-text search**: Meilisearch could index spec content for natural-language querying of behavioral definitions.
- **Graph relationships**: Entity relationships, surface contracts, and module dependencies form a graph. A graph-capable database (Gel DB) could model and traverse these relationships.
- **Distributed specs**: CockroachDB's distributed nature could support multi-team, multi-region spec repositories with strong consistency.
- **Version history**: Tracking spec evolution over time (which rules changed, when, why) is a natural database concern currently handled only by git.
