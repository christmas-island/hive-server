# Skill Replacement Feasibility Analysis

**Date:** 2026-03-09
**Purpose:** Determine exactly what hive-server + CockroachDB + Meilisearch + Gel DB must provide to make each skill (GSD, Superpowers, Allium) unnecessary. Brutally honest about what is server-side vs inherently prompt engineering.

---

## Executive Summary

None of the three skills can be fully replaced by server-side infrastructure. Each skill is 40-60% prompt engineering and LLM reasoning that no API can replicate. What the server can replace is the fragile state management, lack of search, and absence of cross-session memory. The skills would not become "unnecessary" -- they would become thinner clients that delegate state and coordination to hive-server while retaining their prompt templates, workflow orchestration logic, and agent persona definitions.

The honest framing is not "replace these skills" but "extract their backend into hive-server so the skills become stateless prompt layers."

---

## 1. GSD (Get Shit Done)

### 1.1 Capability Inventory

| #   | Capability                           | Description                                                                  |
| --- | ------------------------------------ | ---------------------------------------------------------------------------- |
| G1  | Project initialization questionnaire | Interactive Q&A to establish project scope, constraints, and goals           |
| G2  | Parallel research dispatch           | Spawns 4 research agents simultaneously for domain investigation             |
| G3  | Research synthesis                   | Combines parallel research outputs into a single coherent document           |
| G4  | Requirements extraction              | Derives structured requirements (AUTH-01, etc.) from research and discussion |
| G5  | Roadmap creation                     | Creates phased project plan from requirements with dependency ordering       |
| G6  | Phase discussion / context capture   | Interactive session to capture user preferences (UI, API patterns)           |
| G7  | Plan creation                        | Decomposes phases into 2-3 atomic task plans in XML format                   |
| G8  | Plan validation (plan-checker)       | Iterative review of plans against requirements (up to 3 iterations)          |
| G9  | Wave-based dependency analysis       | Groups plans into dependency waves for parallel execution                    |
| G10 | Parallel plan execution              | Spawns executor sub-agents with fresh 200k contexts per plan                 |
| G11 | Context rot mitigation               | Fresh context windows per sub-agent to maintain output quality               |
| G12 | Atomic git commits per task          | One commit per completed task with conventional commit messages              |
| G13 | Post-execution verification          | Verifier agent checks task outputs against acceptance criteria               |
| G14 | Nyquist verification layer           | Ensures automated tests exist for every task before implementation           |
| G15 | TDD wave-0                           | Creates test stubs before implementation tasks execute                       |
| G16 | Debugging agent                      | Systematic root-cause debugging when tasks fail                              |
| G17 | Integration checking                 | Verifies cross-component integrations after execution                        |
| G18 | Codebase mapping (brownfield)        | Analyzes existing codebases for structure, patterns, and integration points  |
| G19 | State tracking (STATE.md)            | Tracks current position, metrics, session continuity in a 100-line file      |
| G20 | Session pause/resume                 | Creates .continue-here.md files, reads state on resume                       |
| G21 | Requirement traceability             | Maps requirements to phases to plans to tasks to commits                     |
| G22 | Progress tracking                    | Percentage completion, velocity metrics (last 5 plans)                       |
| G23 | Model profile system                 | Assigns different model tiers (opus/sonnet/haiku) to different agent roles   |
| G24 | Configuration management             | config.json for mode, granularity, workflow toggles, concurrency             |
| G25 | Milestone management                 | Archive, tag release, transition to next milestone                           |
| G26 | Quick mode                           | Fast path for small changes bypassing full planning cycle                    |
| G27 | Phase insertion                      | Insert urgent phases between existing ones (decimal numbering)               |
| G28 | Audit milestone                      | Nyquist auditor validates test coverage maps to all requirements             |
| G29 | 12 specialized agent personas        | Markdown-defined agent roles with specific instructions for each             |
| G30 | 32 slash commands                    | Command interface for every workflow step                                    |
| G31 | XML task format                      | Structured task definitions with name, files, action, verify, done criteria  |
| G32 | Commit-only-outcomes pattern         | Commits completed work, not intermediate planning artifacts                  |

### 1.2 Classification

| #   | Capability                  | Classification  | Reasoning                                                                                                                                                                                                                                                      |
| --- | --------------------------- | --------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| G1  | Project init questionnaire  | **Client-side** | Interactive Q&A with a human requires LLM reasoning to ask good follow-up questions, understand domain context, and probe for missing information. No API replaces this.                                                                                       |
| G2  | Parallel research dispatch  | **Hybrid**      | Server coordinates agent spawning and tracks their status. Client defines what each researcher investigates. The orchestration logic (which agents, what prompts) is prompt engineering. The coordination state (who is running, who finished) is server-side. |
| G3  | Research synthesis          | **Client-side** | Combining 4 research documents into one coherent synthesis is pure LLM reasoning. No database query produces a synthesis.                                                                                                                                      |
| G4  | Requirements extraction     | **Client-side** | Deriving structured requirements from unstructured research and conversation is LLM reasoning. The extracted requirements themselves are server-side data.                                                                                                     |
| G5  | Roadmap creation            | **Client-side** | Deciding how to decompose requirements into phases, ordering them by dependency, and setting success criteria requires domain understanding. The roadmap data structure is server-side.                                                                        |
| G6  | Phase discussion            | **Client-side** | Interactive conversation to capture preferences is entirely LLM reasoning.                                                                                                                                                                                     |
| G7  | Plan creation               | **Client-side** | Decomposing a phase into atomic tasks with specific file paths, action instructions, and verification commands requires understanding the codebase and the goal. Pure prompt engineering.                                                                      |
| G8  | Plan validation             | **Client-side** | Reviewing plans against requirements and iterating is LLM judgment. Server can store the plan and requirements for the checker to read, but the checking is LLM reasoning.                                                                                     |
| G9  | Wave dependency analysis    | **Hybrid**      | The dependency graph is server-side data (Gel or CRDB). Analyzing which tasks depend on which is a graph query. But the initial dependency declaration (task A depends on task B) requires LLM understanding of the work.                                      |
| G10 | Parallel plan execution     | **Hybrid**      | Server manages execution state (which plans are running, which completed, which failed). Client provides the executor prompt and fresh context. The sub-agent invocation itself is a client-side operation (Task tool).                                        |
| G11 | Context rot mitigation      | **Client-side** | This is the core architectural decision: spawn sub-agents with fresh contexts. It is a prompt engineering strategy, not a server feature. The server cannot "give" an LLM fresh context.                                                                       |
| G12 | Atomic git commits          | **Client-side** | Git operations are local filesystem operations. The commit message format and timing (one per task) is prompt engineering.                                                                                                                                     |
| G13 | Post-execution verification | **Client-side** | Running verification commands and interpreting their output is LLM reasoning. The verification criteria can be stored server-side.                                                                                                                             |
| G14 | Nyquist verification layer  | **Client-side** | Ensuring tests exist before implementation is a prompt engineering discipline rule. The requirement that plans include verification commands is enforced by the plan-checker agent, not by a database.                                                         |
| G15 | TDD wave-0                  | **Client-side** | Writing test stubs is code generation by an LLM.                                                                                                                                                                                                               |
| G16 | Debugging agent             | **Client-side** | Root-cause analysis and fix generation is LLM reasoning.                                                                                                                                                                                                       |
| G17 | Integration checking        | **Client-side** | Verifying cross-component connections is LLM + test execution.                                                                                                                                                                                                 |
| G18 | Codebase mapping            | **Client-side** | Analyzing an existing codebase for patterns and structure is pure LLM capability.                                                                                                                                                                              |
| G19 | State tracking              | **Server-side** | Current position, phase, plan, status -- this is exactly what a database does. STATE.md is a fragile markdown database. Replace it with actual state in CockroachDB or SQLite.                                                                                 |
| G20 | Session pause/resume        | **Server-side** | Persisting state between sessions and reconstructing context on resume. Server stores the state. Client reads it and reconstructs.                                                                                                                             |
| G21 | Requirement traceability    | **Server-side** | The mapping of requirements to phases to plans to tasks is a graph. Gel handles this natively. CRDB handles it with join tables.                                                                                                                               |
| G22 | Progress tracking           | **Server-side** | Completion percentages, velocity metrics, duration tracking -- pure database queries over structured data.                                                                                                                                                     |
| G23 | Model profile system        | **Client-side** | Which model to use for which agent is a configuration decision enforced at the prompt/orchestration layer. The server does not control which model the client uses.                                                                                            |
| G24 | Configuration management    | **Server-side** | config.json is structured data. Store it in the database. But the interpretation of config (what "granularity: fine" means for plan creation) is client-side.                                                                                                  |
| G25 | Milestone management        | **Hybrid**      | Archiving milestone data is server-side. Deciding when a milestone is complete and what to do next is client-side reasoning.                                                                                                                                   |
| G26 | Quick mode                  | **Client-side** | Deciding that a task is small enough for quick mode and executing it without full planning is LLM judgment.                                                                                                                                                    |
| G27 | Phase insertion             | **Server-side** | Renumbering phases and inserting new ones is data manipulation.                                                                                                                                                                                                |
| G28 | Audit milestone             | **Client-side** | The Nyquist auditor validates test coverage against requirements. The validation logic is LLM reasoning. The requirements and test mapping data is server-side.                                                                                                |
| G29 | Agent personas              | **Client-side** | Agent persona definitions (researcher, planner, executor, etc.) are prompt templates. They are the core intellectual property of GSD. No server replaces them.                                                                                                 |
| G30 | Slash commands              | **Client-side** | Commands are prompt entry points. They orchestrate which agent to invoke with which context. This is the skill's user interface.                                                                                                                               |
| G31 | XML task format             | **Client-side** | The task format is a prompt engineering convention. The server stores tasks, but the XML structure with action/verify/done fields is a prompting pattern.                                                                                                      |
| G32 | Commit-only-outcomes        | **Client-side** | The decision about what to commit and when is an orchestration rule.                                                                                                                                                                                           |

### 1.3 Database Mapping (Server-Side Capabilities)

| Capability                    | CockroachDB                                                                                      | Meilisearch                                                                               | Gel DB                                                                                                                                                             |
| ----------------------------- | ------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| G19: State tracking           | Project/phase/plan status rows with ACID updates                                                 | --                                                                                        | Project state as graph with computed progress                                                                                                                      |
| G20: Session pause/resume     | Session table with agent_id, state snapshot, resume context                                      | --                                                                                        | Session linked to project, phase, plan for instant context reconstruction                                                                                          |
| G21: Requirement traceability | Join tables: requirements <-> phases <-> plans <-> tasks                                         | --                                                                                        | **Best fit.** Bidirectional links: `Requirement.phases`, `Phase.plans`, `Plan.tasks`. EdgeQL path traversal: `SELECT Requirement { phases: { plans: { tasks } } }` |
| G22: Progress tracking        | COUNT/SUM queries over task status. Velocity from timestamp diffs.                               | --                                                                                        | Computed properties: `property completion := count(.tasks filter .status = 'done') / count(.tasks)`                                                                |
| G24: Configuration            | JSON column or config table                                                                      | --                                                                                        | Config type with constrained fields                                                                                                                                |
| G27: Phase insertion          | UPDATE ordering column                                                                           | --                                                                                        | Ordered links with insert-at semantics                                                                                                                             |
| G2: Agent coordination state  | Agent execution table with status, timestamps. Serializable transactions for concurrent updates. | --                                                                                        | Agent linked to session, plan, with real-time status                                                                                                               |
| G9: Dependency graph          | Adjacency table with recursive CTE for wave computation                                          | --                                                                                        | **Best fit.** `Plan.depends_on` multi-link. Wave computation via path traversal.                                                                                   |
| G10: Execution state          | Task status with CLAIMED/IN_PROGRESS/DONE transitions                                            | --                                                                                        | Same, with schema-enforced enum                                                                                                                                    |
| Cross-project search          | --                                                                                               | **Best fit.** Index all research, plans, summaries. Typo-tolerant search across projects. | --                                                                                                                                                                 |
| Historical analytics          | Time-series queries over events table                                                            | Faceted search over historical outcomes                                                   | Computed aggregate properties                                                                                                                                      |

### 1.4 Gap Analysis: What Cannot Be Replicated

**Irreplaceable client-side capabilities (no server API can provide these):**

1. **The 12 agent personas** (G29): The researcher, planner, executor, verifier, debugger, codebase-mapper, etc. are prompt templates that encode how to do each job. They are GSD's core value. A database cannot "be a researcher."

2. **Context rot mitigation** (G11): This is GSD's key architectural insight -- spawn fresh sub-agents with clean 200k contexts. This is a client-side orchestration strategy. The server cannot control the client's context window.

3. **Plan creation and validation** (G7, G8): Decomposing "implement authentication" into specific tasks with file paths, action instructions, and verification commands requires understanding the codebase, the framework, and the goal. This is LLM reasoning at its core.

4. **Research and synthesis** (G2, G3, G4): Investigating a domain, synthesizing findings, and extracting requirements is pure LLM capability.

5. **The Nyquist verification philosophy** (G14, G28): The rule "every task must have automated verification" is a prompt engineering discipline. The server cannot enforce that an LLM writes good tests.

6. **Slash command orchestration** (G30): The 32 commands are workflow entry points that decide which agents to invoke, in what order, with what context. This is the skill's control flow.

### 1.5 The Honest Answer: Can GSD Be Made Unnecessary?

**No.** GSD is approximately:

- **40% prompt engineering** (agent personas, task format, verification philosophy, orchestration logic)
- **25% workflow orchestration** (slash commands, wave execution, parallel dispatch)
- **20% state management** (STATE.md, ROADMAP.md, REQUIREMENTS.md tracking)
- **15% data persistence** (file-based storage, git integration)

hive-server can replace the 35% that is state management + data persistence. This eliminates GSD's fragile markdown state, adds search across accumulated artifacts, and enables cross-project visibility. But the 65% that is prompt engineering + workflow orchestration must remain client-side.

**What "replacing GSD" actually means:** A GSD-compatible MCP plugin that uses hive-server APIs for state instead of markdown files, while keeping GSD's agent personas, task format, and orchestration logic as prompt templates. GSD becomes a thinner client, not unnecessary.

---

## 2. Superpowers

### 2.1 Capability Inventory

| #   | Capability                               | Description                                                                                     |
| --- | ---------------------------------------- | ----------------------------------------------------------------------------------------------- |
| S1  | Skill discovery engine                   | JavaScript engine that scans directories, parses YAML frontmatter, builds skill catalog         |
| S2  | Skill shadowing                          | Personal skills override built-in skills by name; `superpowers:` prefix forces built-in         |
| S3  | Session-start hook                       | Synchronous hook that loads skill context on every session start/resume/clear/compact           |
| S4  | EnterPlanMode intercept                  | Hook that routes plan-mode requests through brainstorming first                                 |
| S5  | Brainstorming skill                      | Structured creative exploration before any planning. Generates options, evaluates tradeoffs.    |
| S6  | Writing-plans skill                      | Creates structured plans with phases, tasks, acceptance criteria in docs/plans/                 |
| S7  | Executing-plans skill                    | Batch execution (3 tasks, checkpoint, repeat) with subagent dispatch                            |
| S8  | Test-driven development skill            | Enforces red-green-refactor cycle with anti-pattern examples                                    |
| S9  | Verification-before-completion skill     | Requires fresh test runs with output inspection before any completion claim                     |
| S10 | Systematic-debugging skill               | Mandates root cause investigation before fix attempts. Structured debugging pipeline.           |
| S11 | Subagent-driven development skill        | Protocol for dispatching single-session subagents with focused task prompts                     |
| S12 | Dispatching-parallel-agents skill        | Protocol for multi-session parallel investigation (3+ independent failures)                     |
| S13 | Requesting-code-review skill             | Protocol for requesting structured code review against plan and standards                       |
| S14 | Receiving-code-review skill              | Protocol for processing review feedback, categorizing issues, creating fix plans                |
| S15 | Using-git-worktrees skill                | Branch isolation pattern using git worktrees                                                    |
| S16 | Finishing-a-development-branch skill     | Four exit strategies: merge locally, create PR, keep branch, discard                            |
| S17 | Using-superpowers meta-skill             | Activation mandate: "1% chance a skill applies -> MUST invoke." Anti-rationalization rules.     |
| S18 | Writing-skills meta-skill                | Guide for creating new skills. Skill type taxonomy (discipline, technique, pattern, reference). |
| S19 | Code-reviewer agent                      | Markdown persona for code review: plans, SOLID, critical/important/suggestion categories        |
| S20 | Skill activation by description matching | "Use when..." patterns in frontmatter trigger contextual skill loading                          |
| S21 | Platform-agnostic integration            | Works across Claude Code, Cursor, Codex, OpenCode via platform-specific config                  |
| S22 | Workflow pipeline                        | brainstorm -> plan -> execute -> (subagent OR parallel) -> finish branch                        |
| S23 | Plan-as-progress-tracker                 | Task checklists in plan files serve as progress tracking                                        |
| S24 | Anti-rationalization design              | Skills written to preempt common agent excuses for skipping steps                               |
| S25 | Update detection                         | Git-based check for remote skill updates with 3-second timeout                                  |

### 2.2 Classification

| #   | Capability                    | Classification  | Reasoning                                                                                                                                                                                                                                                                                            |
| --- | ----------------------------- | --------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| S1  | Skill discovery engine        | **Hybrid**      | Discovery can be server-side (Meilisearch index of skills). But resolution logic (scan directories, parse frontmatter, build catalog) requires either a client-side script or a server endpoint. The engine itself is 150 lines of JavaScript -- not complex, but tightly coupled to the filesystem. |
| S2  | Skill shadowing               | **Server-side** | A skill registry with priority ordering (personal > built-in) is a database query. `SELECT * FROM skills WHERE name = $1 ORDER BY source_priority DESC LIMIT 1`.                                                                                                                                     |
| S3  | Session-start hook            | **Client-side** | Hooks are platform-specific lifecycle events. The server cannot inject code into a Claude Code session start. The hook mechanism is inherently client-side.                                                                                                                                          |
| S4  | EnterPlanMode intercept       | **Client-side** | Intercepting a host agent's plan mode and rerouting it is a platform-specific hook. Pure client-side.                                                                                                                                                                                                |
| S5  | Brainstorming skill           | **Client-side** | Structured creative exploration is LLM reasoning. The brainstorming methodology (generate options, evaluate tradeoffs, rank by feasibility) is a prompt template. No API produces brainstorming output.                                                                                              |
| S6  | Writing-plans skill           | **Hybrid**      | The plan creation logic (structured phases, tasks, acceptance criteria) is LLM reasoning. The resulting plan data structure can be stored server-side. But the instructions for how to write a good plan are prompt engineering.                                                                     |
| S7  | Executing-plans skill         | **Hybrid**      | Batch execution with checkpoints is orchestration logic (client-side). Task status tracking is server-side. Subagent dispatch is client-side (Task tool).                                                                                                                                            |
| S8  | TDD skill                     | **Client-side** | The red-green-refactor discipline, anti-pattern examples, and enforcement rules are prompt engineering. The server cannot make an LLM write tests first.                                                                                                                                             |
| S9  | Verification skill            | **Client-side** | "Run the command freshly, read full output, verify it supports the claim" is a behavioral instruction for the LLM. Pure prompt engineering.                                                                                                                                                          |
| S10 | Systematic-debugging skill    | **Client-side** | The debugging methodology (reproduce, isolate, hypothesize, test, fix) is prompt engineering. The server can store debug session artifacts, but the debugging itself is LLM reasoning.                                                                                                               |
| S11 | Subagent dispatch protocol    | **Hybrid**      | The dispatch protocol (create prompt, dispatch, wait for questions, review) is prompt engineering. The agent lifecycle tracking (dispatched, running, completed) is server-side.                                                                                                                     |
| S12 | Parallel agents skill         | **Hybrid**      | The conditions for when to use parallel agents (3+ independent failures, no shared dependencies) are prompt engineering. The coordination state is server-side.                                                                                                                                      |
| S13 | Code review request           | **Client-side** | Structuring a review request against plan and standards is prompt engineering.                                                                                                                                                                                                                       |
| S14 | Code review processing        | **Client-side** | Categorizing issues and creating fix plans is LLM reasoning.                                                                                                                                                                                                                                         |
| S15 | Git worktrees                 | **Client-side** | Git operations are local filesystem operations. The pattern of using worktrees for isolation is a technique, not a server feature.                                                                                                                                                                   |
| S16 | Branch finishing              | **Client-side** | The four exit strategies and the decision of which to use are prompt engineering / user interaction.                                                                                                                                                                                                 |
| S17 | Activation mandate            | **Client-side** | "1% chance -> MUST invoke" is a meta-prompt instruction. This is the most purely prompt-engineering capability in the entire skill.                                                                                                                                                                  |
| S18 | Writing-skills guide          | **Client-side** | A guide for writing skills is documentation / prompt engineering.                                                                                                                                                                                                                                    |
| S19 | Code-reviewer agent           | **Client-side** | Agent persona definition is a prompt template.                                                                                                                                                                                                                                                       |
| S20 | Description-based activation  | **Hybrid**      | Matching user context to skill descriptions could be server-side search (Meilisearch). But the decision to activate is still made by the LLM reading the description.                                                                                                                                |
| S21 | Platform-agnostic integration | **Client-side** | Platform config directories (.claude-plugin/, .cursor-plugin/) are client-side infrastructure.                                                                                                                                                                                                       |
| S22 | Workflow pipeline             | **Client-side** | The brainstorm -> plan -> execute -> finish pipeline is orchestration logic encoded in prompt templates.                                                                                                                                                                                             |
| S23 | Plan-as-progress-tracker      | **Server-side** | Task completion tracking is a database concern.                                                                                                                                                                                                                                                      |
| S24 | Anti-rationalization design   | **Client-side** | Writing prompts that preempt agent excuses is pure prompt engineering. This is arguably the most valuable and irreplaceable aspect of Superpowers.                                                                                                                                                   |
| S25 | Update detection              | **Server-side** | Checking for skill updates can be a server endpoint: `GET /api/v1/superpowers/skills/updates`.                                                                                                                                                                                                       |

### 2.3 Database Mapping (Server-Side Capabilities)

| Capability                       | CockroachDB                                                     | Meilisearch                                                                                                                      | Gel DB                                              |
| -------------------------------- | --------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------- |
| S2: Skill shadowing/registry     | Skills table with source_priority column                        | --                                                                                                                               | Skill type with priority ordering                   |
| S6: Plan storage                 | Plans table with tasks as JSON or child rows                    | --                                                                                                                               | Plan -> Task links with status enum                 |
| S7: Execution state              | Task status table with transitions                              | --                                                                                                                               | Plan execution graph with computed progress         |
| S11: Agent lifecycle             | Agent session table with status, dispatch_time, completion_time | --                                                                                                                               | Agent -> Session -> Plan graph                      |
| S12: Parallel agent coordination | Agent table with SELECT FOR UPDATE for claiming                 | --                                                                                                                               | Agent coordination graph                            |
| S20: Skill search by description | --                                                              | **Best fit.** Index skill descriptions, searchable by natural language. "Use when debugging..." matched by typo-tolerant search. | --                                                  |
| S23: Progress tracking           | Task completion counts, velocity queries                        | --                                                                                                                               | Computed completion properties                      |
| S25: Update detection            | Version column on skills table                                  | --                                                                                                                               | --                                                  |
| Cross-session memory             | Session summaries table, searchable                             | **Best fit.** Index session summaries. "What debugging approach worked for race conditions?" across all prior sessions.          | Session -> Outcome links for pattern analysis       |
| Skill effectiveness              | Invocations table with success/failure, duration                | Faceted analytics over invocation history                                                                                        | Computed effectiveness scores via aggregate queries |

### 2.4 Gap Analysis: What Cannot Be Replicated

**Irreplaceable client-side capabilities:**

1. **The 14 skill definitions** (S5-S16): Each skill is a carefully crafted behavioral instruction set. The brainstorming methodology, TDD enforcement rules, systematic debugging pipeline, and verification requirements are prompt engineering documents. They encode how experienced developers think. No API produces "a brainstorming session."

2. **Anti-rationalization design** (S24): Skills are written to preempt agent excuses. "You might think this test is unnecessary because... but you MUST write it because..." This adversarial prompt design is Superpowers' deepest value. It requires understanding how LLMs fail and writing prompts that prevent those failures.

3. **Activation mandate** (S17): The "1% rule" is a meta-prompt that overrides the LLM's tendency to skip steps. This is a prompt engineering technique that works precisely because it is injected into the LLM's context, not because of any external enforcement.

4. **Session-start hooks and platform integration** (S3, S4, S21): These are host-agent-specific integration mechanisms. The server cannot hook into Claude Code's session lifecycle.

5. **Workflow pipeline orchestration** (S22): The brainstorm -> plan -> execute -> finish pipeline with its gates and checkpoints is control flow logic that lives in the client. The server tracks state, but the "what comes next" decisions are client-side.

### 2.5 The Honest Answer: Can Superpowers Be Made Unnecessary?

**No.** Superpowers is approximately:

- **55% prompt engineering** (skill definitions, anti-rationalization design, activation rules, agent personas)
- **15% workflow orchestration** (pipeline sequencing, subagent dispatch protocol, checkpoint logic)
- **15% platform integration** (hooks, plugin manifests, cross-platform config)
- **10% state management** (plan progress tracking, task checklists)
- **5% search/discovery** (skill catalog, skill resolution)

hive-server can replace the 15% that is state management + search/discovery. This adds cross-session memory (the biggest gap), skill effectiveness analytics, and better skill discovery via Meilisearch. But the 85% that is prompt engineering + orchestration + platform integration must remain client-side.

**What "replacing Superpowers" actually means:** Superpowers keeps all its skill definitions and platform hooks. It gains a server-side skill registry (searchable via Meilisearch), cross-session memory (session summaries indexed and retrievable), and progress tracking (task state in hive-server instead of plan file checklists). The skill becomes a thinner client for state, not unnecessary.

**Important caveat:** Superpowers' value proposition is partly that it requires zero infrastructure. Adding a server dependency fundamentally changes its nature. Many users choose Superpowers specifically because it is "just markdown files." A server-backed Superpowers is a different product.

---

## 3. Allium

### 3.1 Capability Inventory

| #   | Capability                        | Description                                                                                                         |
| --- | --------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| A1  | Behavioral specification language | Custom language for capturing what a system should do and why, excluding how                                        |
| A2  | Entity modeling                   | Entities with identity, lifecycle, fields, relationships, projections, derived values                               |
| A3  | Value type definitions            | Immutable data without identity, compared structurally                                                              |
| A4  | Sum types / discriminated unions  | Discriminator field with pipe-separated variant names                                                               |
| A5  | Rule definitions                  | Event-driven behavior with preconditions (requires), postconditions (ensures), triggers                             |
| A6  | Surface definitions               | Boundary contracts between system and external parties (facing, context, exposes, provides)                         |
| A7  | Contract definitions              | Named, direction-agnostic obligations with typed signatures and invariants                                          |
| A8  | Actor declarations                | Entity types that interact with surfaces, with identity mapping and scoping                                         |
| A9  | Invariant declarations            | Prose annotations (@invariant) and expression-bearing invariants                                                    |
| A10 | Config declarations               | Parameterized values with arithmetic defaults and cross-module references                                           |
| A11 | Default entity instances          | Named seed instances available to all rules                                                                         |
| A12 | Expression language               | Field access, optional chaining, collections, boolean logic, existence checks                                       |
| A13 | Module system                     | use declarations, external entities, qualified references, config overrides                                         |
| A14 | Rust parser / typed AST           | Produces JSON AST from .allium files                                                                                |
| A15 | Tree-sitter grammar               | Editor syntax highlighting                                                                                          |
| A16 | CLI validator                     | Checks structural rules (referenced entities exist, relationships have backreferences, etc.)                        |
| A17 | /allium interactive mode          | Interactive specification work with LLM                                                                             |
| A18 | /allium:elicit mode               | Build specs through structured stakeholder conversation                                                             |
| A19 | /allium:distill mode              | Extract specs from existing code                                                                                    |
| A20 | Tend agent                        | Specification steward: creates, modifies, restructures .allium files. Pushes back on ambiguity.                     |
| A21 | Weed agent                        | Divergence detector: compares specs against implementation. Three modes: check, update spec, update code.           |
| A22 | Drift classification              | Categorizes mismatches: spec bug, code bug, aspirational design, intentional gap                                    |
| A23 | Library specs                     | Reusable contracts (OAuth, payment, email) importable via use declarations                                          |
| A24 | Modular composition               | Specs reference others via git SHAs, content hashes, relative paths                                                 |
| A25 | Specification-first development   | Write specs before code; LLMs reference specs during implementation                                                 |
| A26 | Distillation methodology          | Map territory, extract states, extract transitions, find temporal triggers, identify boundaries, abstract, validate |
| A27 | Elicitation methodology           | Scope definition, happy path, edge cases, refinement phases                                                         |
| A28 | Language governance               | 9-member review panel for language changes, two tracks (reviews and proposals)                                      |

### 3.2 Classification

| #   | Capability                      | Classification  | Reasoning                                                                                                                                                                                                |
| --- | ------------------------------- | --------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A1  | Specification language          | **Client-side** | The language itself is a notation consumed by humans and LLMs. It is text. No server "is" a language. The language reference is a document, not a service.                                               |
| A2  | Entity modeling                 | **Client-side** | Defining entities with their fields and relationships is an authoring activity done by a human or LLM. The resulting entity definitions can be stored server-side.                                       |
| A3  | Value type definitions          | **Client-side** | Same as A2. Authoring.                                                                                                                                                                                   |
| A4  | Sum types                       | **Client-side** | Same.                                                                                                                                                                                                    |
| A5  | Rule definitions                | **Client-side** | Writing rules with preconditions and postconditions is specification authoring. LLM reasoning about what the rules should be is irreplaceable.                                                           |
| A6  | Surface definitions             | **Client-side** | Same.                                                                                                                                                                                                    |
| A7  | Contract definitions            | **Client-side** | Same.                                                                                                                                                                                                    |
| A8  | Actor declarations              | **Client-side** | Same.                                                                                                                                                                                                    |
| A9  | Invariants                      | **Client-side** | Same.                                                                                                                                                                                                    |
| A10 | Config                          | **Client-side** | Authoring. But config storage and cross-spec config resolution could be server-side.                                                                                                                     |
| A11 | Defaults                        | **Client-side** | Authoring.                                                                                                                                                                                               |
| A12 | Expression language             | **Client-side** | The expression language is part of the specification notation.                                                                                                                                           |
| A13 | Module system                   | **Hybrid**      | Use declarations and qualified references are authoring. But resolving imports across repositories and managing dependency versions is a server concern.                                                 |
| A14 | Rust parser / AST               | **Server-side** | Parsing .allium files into typed ASTs is computation. The server could store ASTs and provide structural queries over them. The parser itself is a CLI tool (allium-cli).                                |
| A15 | Tree-sitter grammar             | **Client-side** | Editor integration is purely client-side.                                                                                                                                                                |
| A16 | CLI validator                   | **Hybrid**      | Validation rules (all references exist, relationships have backreferences) could be server-side checks. But the validator currently runs as a local CLI tool.                                            |
| A17 | Interactive specification mode  | **Client-side** | Interactive conversation to create specs is LLM reasoning with human interaction.                                                                                                                        |
| A18 | Elicitation mode                | **Client-side** | Structured stakeholder interviews with phased questioning is LLM reasoning.                                                                                                                              |
| A19 | Distillation mode               | **Client-side** | Extracting specs from code requires reading code, understanding intent, and abstracting implementation. Pure LLM capability.                                                                             |
| A20 | Tend agent                      | **Client-side** | Specification steward persona. Creates, modifies, restructures specs. Pushes back on ambiguity. This is a prompt template encoding how a good specification author works.                                |
| A21 | Weed agent                      | **Hybrid**      | Comparing specs against code requires LLM reasoning (client-side). But the comparison results (drift reports with classifications) are server-side data. Tracking drift over time is a database concern. |
| A22 | Drift classification            | **Client-side** | Categorizing a mismatch as "spec bug" vs "code bug" vs "aspirational design" vs "intentional gap" requires understanding intent. Pure LLM judgment.                                                      |
| A23 | Library specs                   | **Server-side** | A registry of reusable spec libraries (OAuth, payment, email) with versioning and discovery is a server concern. Meilisearch indexes them. Gel models their dependency graph.                            |
| A24 | Modular composition             | **Hybrid**      | Import resolution (which version of which spec) is server-side. The actual composition (how imported contracts affect local rules) is specification authoring.                                           |
| A25 | Specification-first development | **Client-side** | The methodology of writing specs before code is a workflow discipline. Prompt engineering.                                                                                                               |
| A26 | Distillation methodology        | **Client-side** | The 7-step process for extracting specs from code is prompt engineering.                                                                                                                                 |
| A27 | Elicitation methodology         | **Client-side** | The phased interview structure is prompt engineering.                                                                                                                                                    |
| A28 | Language governance             | **Neither**     | This is an organizational process, not a technical capability.                                                                                                                                           |

### 3.3 Database Mapping (Server-Side Capabilities)

| Capability                    | CockroachDB                                                                                    | Meilisearch                                                                                                                   | Gel DB                                                                                                                                                                                               |
| ----------------------------- | ---------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A14: AST storage and querying | Spec table with AST as JSONB. Query entity fields via JSONB operators. Verbose but functional. | --                                                                                                                            | **Best fit.** Allium's AST maps almost 1:1 to Gel's schema. Entity -> Rule -> Surface -> Contract as Gel types with links. EdgeQL: `SELECT Entity { rules := .<entity[IS Rule] { name, triggers } }` |
| A16: Validation as a service  | CHECK constraints on spec records                                                              | --                                                                                                                            | Schema constraints enforce structural rules (exclusive constraints on entity names, required fields on rules)                                                                                        |
| A21: Drift tracking           | Drift reports table with timestamps, classifications, resolution status                        | Drift reports indexed for search ("which specs have unresolved drift?")                                                       | DriftReport linked to Spec, Entity, Rule for graph traversal of drift patterns                                                                                                                       |
| A23: Library spec registry    | Specs table with version, author, category                                                     | **Best fit for discovery.** Search library specs by content: "find OAuth-related contracts." Typo-tolerant, natural language. | Library spec dependency graph. "Which specs depend on the Codec contract?"                                                                                                                           |
| A24: Import resolution        | Version table mapping spec dependencies                                                        | --                                                                                                                            | **Best fit.** Spec -> Spec links with version constraints. Dependency cycle detection via graph traversal.                                                                                           |
| Cross-spec impact analysis    | Recursive CTEs over entity-rule-surface join tables                                            | Text search for entity names across specs (approximate)                                                                       | **Best fit.** "Which rules across all specs affect entity User?" is a single back-link traversal.                                                                                                    |
| Spec evolution tracking       | Spec versions table with diffs                                                                 | Search across historical spec versions                                                                                        | Version linked to Spec with computed diff properties                                                                                                                                                 |

### 3.4 Gap Analysis: What Cannot Be Replicated

**Irreplaceable client-side capabilities:**

1. **The specification language itself** (A1-A12): Allium IS the language. The syntax, semantics, section ordering, naming conventions, and expression language are a notation for humans and LLMs. No server "is" a language. The server can store, index, and query specs written in the language, but cannot replace the act of writing them.

2. **Tend agent** (A20): The specification steward persona -- knowing when to push back on ambiguity, when to split entities, when to extract contracts -- is prompt engineering that encodes how experienced specification authors think. No API call produces "a well-structured specification."

3. **Weed agent's judgment** (A21, A22): The Weed agent's ability to compare spec intent against code behavior and classify mismatches requires understanding both the specification and the implementation. The drift report is server-side data. The judgment that produced it is LLM reasoning.

4. **Elicitation and distillation methodologies** (A18, A19, A26, A27): The structured approaches for extracting specifications from stakeholder conversations or existing code are prompt engineering methodologies. They encode domain expertise in interview techniques and code analysis.

5. **Specification-first development methodology** (A25): The philosophy of writing specs before code is a workflow discipline enforced by prompt instructions, not by server enforcement.

### 3.5 The Honest Answer: Can Allium Be Made Unnecessary?

**No.** Allium is approximately:

- **50% specification language** (syntax, semantics, constructs, expression language, module system)
- **25% prompt engineering** (Tend agent, Weed agent, elicitation methodology, distillation methodology)
- **15% tooling** (parser, validator, tree-sitter grammar)
- **10% storage/querying** (AST persistence, cross-spec queries, drift tracking, library registry)

hive-server can replace the 10% that is storage/querying and potentially host the 15% that is tooling (run the parser server-side, expose validation as an API). This enables cross-spec impact analysis, drift trend tracking, and library spec discovery. But the 75% that is the language itself + prompt engineering must remain client-side.

**What "replacing Allium" actually means:** Allium keeps its language, its Tend/Weed agents, and its elicitation/distillation methodologies as prompt templates. It gains a server-side spec registry (searchable via Meilisearch), structural querying over ASTs (via Gel), drift history tracking, and library spec discovery. The spec files remain the source of truth for spec content. The server provides a queryable shadow of the structural metadata.

**Important caveat:** Allium is the most inherently client-side of the three skills. Its core value IS the language and the LLM integration patterns. The server additions are genuinely useful (especially cross-spec impact analysis and drift tracking) but they are enhancements to Allium, not replacements for it.

---

## 4. Comparative Summary

### 4.1 Server-Replaceable Percentage by Skill

| Skill           | Prompt Engineering | Workflow Orchestration | Platform Integration | State Management | Storage/Search    | Server Can Replace |
| --------------- | ------------------ | ---------------------- | -------------------- | ---------------- | ----------------- | ------------------ |
| **GSD**         | 40%                | 25%                    | 0%                   | 20%              | 15%               | **~35%**           |
| **Superpowers** | 55%                | 15%                    | 15%                  | 10%              | 5%                | **~15%**           |
| **Allium**      | 25% + 50% language | 0%                     | 0%                   | 0%               | 10% + 15% tooling | **~10-25%**        |

### 4.2 What Each Database Specifically Enables

**CockroachDB enables:**

- Concurrent multi-agent writes to shared project state (GSD wave execution)
- Distributed hive-server instances for availability
- ACID state transitions (task status machine with serializable isolation)
- Multi-tenant project isolation at scale
- Event sourcing for audit trails

**Meilisearch enables:**

- Cross-session memory retrieval ("what worked last time for X?") -- ALL skills
- Skill discovery by natural language description (Superpowers)
- Spec search by content across projects (Allium)
- Research/plan/summary search across GSD projects
- Typo-tolerant, instant search over all accumulated artifacts

**Gel DB enables:**

- Requirement -> phase -> plan -> task traceability graph (GSD)
- Cross-spec impact analysis: "which rules affect entity X?" (Allium)
- Dependency graph for wave computation (GSD)
- Spec module dependency graph with cycle detection (Allium)
- Computed progress/effectiveness properties (all skills)

### 4.3 The Irreducible Client-Side Core

These capabilities are inherently prompt engineering and can NEVER be replaced by a server API, regardless of how sophisticated:

| Category                   | Examples                                                        | Why It Is Irreducible                                                                                                                                               |
| -------------------------- | --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Agent personas**         | GSD's 12 agents, Superpowers' code-reviewer, Allium's Tend/Weed | These encode "how to think about a problem." They are behavioral instructions for an LLM. A database stores data; it does not think.                                |
| **Workflow discipline**    | TDD enforcement, verification-before-completion, Nyquist rule   | These are rules that override the LLM's tendency to take shortcuts. They work by being in the context window, not by being enforced externally.                     |
| **Anti-rationalization**   | Superpowers' "1% rule," skill preemption of agent excuses       | The most valuable and most irreplaceable capability. Prompt engineering that understands how LLMs fail and prevents it.                                             |
| **Creative reasoning**     | Brainstorming, research synthesis, specification authoring      | Generating novel ideas, synthesizing information, and creating formal specifications are core LLM capabilities that no API call produces.                           |
| **Context rot mitigation** | GSD's fresh-context sub-agent architecture                      | This is a meta-strategy about how to use LLMs effectively. It is advice encoded in orchestration logic, not a server feature.                                       |
| **Domain judgment**        | Plan validation, drift classification, codebase analysis        | Determining if a plan is good, if code matches spec intent, or how a codebase is structured requires understanding the domain. LLM reasoning, not database queries. |

### 4.4 What hive-server Should Actually Build

Instead of "making skills unnecessary," hive-server should build:

1. **A state backend** that skills use instead of markdown files

   - Project/phase/plan/task state (replaces STATE.md, ROADMAP.md)
   - Session state with pause/resume (replaces .continue-here.md)
   - Skill invocation records (new capability)
   - Drift reports with history (replaces ad-hoc .allium comparison)

2. **A search layer** (Meilisearch) that skills query for cross-session memory

   - Index all artifacts: research, plans, summaries, specs, session logs
   - Enable "what worked before for X?" queries
   - Skill discovery by description matching

3. **A graph layer** (Gel) for structural queries that grep cannot answer

   - Requirement traceability chains
   - Cross-spec impact analysis
   - Dependency wave computation

4. **An MCP plugin** that exposes hive-server APIs as tools the skills can call
   - `save_state`, `get_state`, `search`, `record_event`
   - Skills call these tools instead of writing markdown files
   - Skills remain prompt templates that orchestrate LLM reasoning

### 4.5 The Bottom Line

The three skills are not "doing things that a server should do." They are doing things that LLMs must do (reason, plan, verify, debug, specify), and incidentally also managing state in a fragile way because they have no server to delegate to.

hive-server's role is not to replace the skills. It is to be the backend they never had. The skills become thinner, more reliable, and gain capabilities they cannot have today (search, cross-session memory, cross-project visibility, structured querying). But they do not become unnecessary. The prompt engineering and workflow orchestration -- the parts that actually make agents effective -- remain entirely client-side.

**The honest answer for all three skills: No, they cannot be made unnecessary. They can be made better by extracting their fragile state management into a proper backend. The 60-85% that is prompt engineering, workflow orchestration, and platform integration is irreplaceable by any server architecture.**
