# Superpowers Technology Brief

**Repository:** <https://github.com/obra/superpowers>
**Version:** 4.3.1 (as of 2026-02-21)
**Author:** Jesse Vincent (<jesse@fsck.com>)
**License:** MIT
**Stars:** ~74.6k | **Language:** Shell (76%), JavaScript (12%), Python (6%)

## What It Is

Superpowers is an agentic skills framework that provides structured development workflows for AI coding agents. It ships as a plugin for Claude Code, Cursor, Codex, and OpenCode. Rather than letting agents jump straight to coding, Superpowers enforces a disciplined pipeline: brainstorm, plan, implement (via TDD), review, and verify.

The core premise is that LLM coding agents produce better results when guided by structured, composable "skills" -- reusable instruction sets that automatically activate based on context. Skills are essentially prompt-engineering documents with YAML frontmatter metadata, organized into a discoverable catalog.

## Architecture and Key Components

### Component Overview

```
superpowers/
  .claude-plugin/      Plugin manifest (plugin.json, marketplace.json)
  .cursor-plugin/      Cursor IDE plugin config
  .codex/              Codex integration
  .opencode/           OpenCode integration
  hooks/               Session lifecycle hooks
    hooks.json         Hook definitions (SessionStart trigger)
    session-start/     Bootstrap scripts
    run-hook.cmd       Cross-platform hook runner
  skills/              Individual skill definitions (14 skills)
    <skill-name>/
      SKILL.md         Skill definition with YAML frontmatter
      *.md             Optional supporting reference docs
  agents/              Subagent role definitions
    code-reviewer.md   Code review agent persona
  commands/            Slash command definitions
    brainstorm.md      /brainstorm command
    write-plan.md      /write-plan command
    execute-plan.md    /execute-plan command
  lib/
    skills-core.js     Skill discovery, resolution, and loading engine
```

### Skill Discovery Engine (skills-core.js)

The JavaScript core provides:

- **Frontmatter extraction:** Parses YAML metadata (`name`, `description`) from SKILL.md files.
- **Recursive directory scanning:** Searches up to 3 levels deep for SKILL.md files. Returns a catalog of skills with path, metadata, and source type (personal vs. built-in).
- **Skill shadowing:** Personal skills (user-defined) override built-in superpowers skills by name. Users can force a built-in skill using the `superpowers:` prefix.
- **Update detection:** Git-based check for remote updates with 3-second timeout and graceful degradation.
- **Content processing:** Strips frontmatter before injecting skill content into context.

### Hook System

Hooks are defined in `hooks.json` and trigger on session lifecycle events:

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup|resume|clear|compact",
        "hooks": [
          {
            "type": "command",
            "command": "'${CLAUDE_PLUGIN_ROOT}/hooks/run-hook.cmd' session-start",
            "async": false
          }
        ]
      }
    ]
  }
}
```

The session-start hook runs synchronously (changed from async in v4.3.0) to ensure skills context is loaded before the first user message. v4.3.0 also added an `EnterPlanMode` intercept hook to route plan-mode requests through brainstorming first.

### Plugin Manifest

```json
{
  "name": "superpowers",
  "description": "Core skills library for Claude Code: TDD, debugging, collaboration patterns, and proven techniques",
  "version": "4.3.1",
  "keywords": [
    "skills",
    "tdd",
    "debugging",
    "collaboration",
    "best-practices",
    "workflows"
  ]
}
```

## How It Enhances LLM Agent Capabilities

Superpowers addresses several failure modes of unguided LLM agents:

1. **Premature implementation:** Agents skip design and jump to code. Superpowers enforces brainstorming and planning gates.
2. **Shallow debugging:** Agents guess at fixes. The systematic-debugging skill mandates root cause investigation before any fix attempt.
3. **Missing verification:** Agents claim success without evidence. The verification-before-completion skill requires fresh test runs with output inspection.
4. **Context pollution:** Long sessions degrade quality. Subagent-driven development spawns fresh agents per task.
5. **Skip testing:** Agents write implementation first. TDD skill enforces red-green-refactor with examples of good/bad patterns.

### Activation Model

The using-superpowers meta-skill defines a mandatory threshold: "If you think there is even a 1% chance a skill might apply, you ABSOLUTELY MUST invoke the skill." This aggressive activation ensures skills are not bypassed through agent rationalization.

Skills are loaded on-demand when the agent determines they are relevant, based on the `description` field in frontmatter. Descriptions use "Use when..." patterns to optimize for contextual matching.

## Skill Definitions and Formats

### SKILL.md Structure

```markdown
---
name: skill-name-in-kebab-case
description: Use when [trigger condition] - [what it does]
---

[Skill content: instructions, rules, examples, flowcharts, checklists]
```

**Frontmatter fields:**

- `name` (required): Kebab-case identifier, used for shadowing and resolution.
- `description` (required): "Use when..." trigger clause. Must describe conditions/symptoms, never summarize the workflow. This is critical because agents may follow the description instead of reading the full skill.

### Skill Taxonomy

| Category          | Skills                                                   | Purpose                         |
| ----------------- | -------------------------------------------------------- | ------------------------------- |
| **Process**       | brainstorming, writing-plans, executing-plans            | Structured development pipeline |
| **Quality**       | test-driven-development, verification-before-completion  | Enforce testing and evidence    |
| **Debugging**     | systematic-debugging                                     | Root cause investigation        |
| **Collaboration** | subagent-driven-development, dispatching-parallel-agents | Multi-agent coordination        |
| **Review**        | requesting-code-review, receiving-code-review            | Two-stage review process        |
| **Git**           | using-git-worktrees, finishing-a-development-branch      | Branch isolation and cleanup    |
| **Meta**          | using-superpowers, writing-skills                        | Skill system itself             |

### Skill Types (from writing-skills)

- **Discipline skills:** Rules and requirements. Tested under maximum pressure.
- **Technique skills:** How-to guides. Tested on novel scenarios.
- **Pattern skills:** Mental models. Tested on recognition boundaries.
- **Reference skills:** APIs and docs. Tested on retrieval accuracy.

### Supporting Files

Skills can include additional reference documents (e.g., `testing-anti-patterns.md` in the TDD skill). These are linked via `@filename.md` syntax and loaded when referenced.

## Integration Patterns

### Platform Integration

Superpowers integrates with four platforms via platform-specific config directories:

| Platform    | Config            | Installation                                       |
| ----------- | ----------------- | -------------------------------------------------- |
| Claude Code | `.claude-plugin/` | Plugin marketplace                                 |
| Cursor      | `.cursor-plugin/` | Plugin marketplace                                 |
| Codex       | `.codex/`         | Clone + symlink to `~/.agents/skills/superpowers/` |
| OpenCode    | `.opencode/`      | Manual setup                                       |

v4.2.0 removed the Node.js bootstrap CLI dependency. Installation for Codex/OpenCode is now "clone + symlink" only.

### Agent Role Definitions

Subagents are defined as markdown personas in `agents/`. The code-reviewer agent reviews against plans, coding standards, SOLID principles, and categorizes issues as Critical/Important/Suggestion.

### Commands (Slash Commands)

Commands are thin wrappers that invoke skills:

```markdown
---
description: "You MUST use this before any creative work..."
disable-model-invocation: true
---

Invoke the superpowers:brainstorming skill and follow it exactly.
```

The `disable-model-invocation: true` flag prevents the agent from responding before loading the skill.

### Workflow Pipeline

```
/brainstorm --> brainstorming skill
                  |
                  v
              writing-plans skill --> docs/plans/YYYY-MM-DD-<feature>.md
                  |
                  v
              executing-plans skill (batch of 3 tasks, checkpoint, repeat)
                  |
                  +--> subagent-driven-development (single session)
                  |    or
                  +--> dispatching-parallel-agents (multi-session)
                  |
                  v
              finishing-a-development-branch skill
                  |
                  +--> Merge locally
                  +--> Create PR via `gh`
                  +--> Keep branch
                  +--> Discard (requires typed confirmation)
```

## State Management Approach

Superpowers has **no persistent state layer**. State management is handled entirely through:

1. **Filesystem artifacts:** Plans are saved to `docs/plans/YYYY-MM-DD-<feature>.md`. Design documents go to `docs/plans/YYYY-MM-DD-<topic>-design.md`.
2. **Git branches:** Worktrees provide isolation. Branch state tracks progress.
3. **Agent context window:** Skills are injected into the agent's context. Task completion is tracked within the conversation.
4. **Task checklists in plans:** Each plan task is marked complete as it is executed. The plan file serves as a progress tracker.

There is no database, no session store, no cross-session memory, and no shared state between agents. Each subagent starts fresh with only the context explicitly passed to it.

### Implications for Database Integration

This is the most significant architectural gap. Superpowers currently cannot:

- **Remember across sessions:** No cross-session learning or accumulated knowledge.
- **Share state between agents:** Parallel agents cannot coordinate beyond their dispatch instructions.
- **Track historical outcomes:** No record of what worked, what failed, or how long tasks took.
- **Search prior work:** No semantic or full-text search over past plans, designs, or implementations.
- **Manage skill effectiveness:** No metrics on which skills are invoked, how often they help, or where they fail.

## Key APIs and Protocols

### Skill Resolution Protocol

1. Check personal skills directory (`~/.claude/skills/` or equivalent).
2. Check superpowers skills directory.
3. Personal skills shadow (override) superpowers skills by name.
4. `superpowers:` prefix forces the built-in version.

### Subagent Dispatch Protocol

1. Create focused task prompt with: specific scope, clear goal, work constraints, expected deliverables.
2. Dispatch subagent with full task context.
3. Wait for clarifying questions.
4. Subagent implements, tests, self-reviews, commits.
5. Spec compliance review (does it match the plan?).
6. Code quality review (is it well-written?).
7. Fix loop if issues found.
8. Mark task complete.

### Parallel Agent Conditions

Deploy parallel agents only when:

- 3+ independent failures across different domains.
- No shared dependencies between investigations.
- Problems can be understood in isolation.
- Agents will not interfere with each other.

### Verification Protocol

Before any completion claim:

1. Identify the verification command.
2. Run the command freshly (no cached results).
3. Read full output including exit codes.
4. Verify output supports the claim.
5. State claim with evidence attached.

## Strengths

- **Zero infrastructure:** Pure markdown and shell scripts. No runtime dependencies, no server, no database.
- **Platform-agnostic:** Works across Claude Code, Cursor, Codex, and OpenCode.
- **Composable:** Skills reference each other and chain naturally (brainstorm -> plan -> execute -> finish).
- **Battle-tested workflow:** Enforces TDD, root-cause debugging, and evidence-based verification.
- **Extensible:** Users create personal skills that shadow built-in ones. The writing-skills meta-skill documents how.
- **Low barrier to entry:** Install is clone + symlink. Skills are just markdown files.
- **Anti-rationalization design:** Skills are written to preempt common agent excuses for skipping steps.
- **Large community:** 74k+ stars, 5.8k forks, active development.

## Limitations

- **No persistent memory:** Cannot learn from past sessions, accumulate project knowledge, or share context across agent instances. Every session starts from zero.
- **No shared state:** Parallel agents and subagents cannot coordinate beyond their initial dispatch context. No real-time state synchronization.
- **No search or retrieval:** Cannot search past plans, designs, or implementation history. No semantic similarity matching for relevant prior work.
- **No metrics or analytics:** Cannot track skill effectiveness, task completion rates, or workflow performance. No feedback loop for skill improvement.
- **No structured data:** Plans and designs are unstructured markdown. No schema validation, no queryable format, no relational data model.
- **Context window dependent:** All skill content must fit in the agent's context window. No external memory augmentation or RAG pipeline.
- **Single-repo scoped:** Skills are loaded from the plugin directory or personal directory. No multi-repo skill sharing or organization-wide skill management.
- **No real-time coordination:** Parallel agents are fire-and-forget. No message passing, no event bus, no coordination protocol during execution.
- **Markdown-as-code fragility:** Skill behavior depends on LLM interpretation of natural language instructions. No formal specification or type system for skill contracts.

## Relevance for Database Technology Evaluation

The following Superpowers limitations map directly to capabilities that database technologies could address:

| Limitation                | Gel DB Potential                              | CockroachDB Potential                        | Meilisearch Potential                                 |
| ------------------------- | --------------------------------------------- | -------------------------------------------- | ----------------------------------------------------- |
| No persistent memory      | Graph-based agent memory with relationships   | Distributed session/task state               | Not applicable                                        |
| No shared state           | Object-graph for real-time agent coordination | ACID transactions for concurrent agent state | Not applicable                                        |
| No search/retrieval       | Graph traversal for related plans/designs     | SQL queries over structured task data        | Full-text + semantic search over plans, code, designs |
| No metrics                | Computed properties for skill analytics       | Time-series task metrics, aggregation        | Faceted search over historical metrics                |
| No structured data        | Schema-enforced skill/plan objects            | Relational schema for plans, tasks, outcomes | Indexed document store for plan content               |
| Context window limits     | Selective graph queries for relevant context  | Paginated query results                      | Relevance-ranked retrieval for RAG                    |
| Single-repo scope         | Cross-repo object references                  | Multi-tenant distributed storage             | Federated index across repos                          |
| No real-time coordination | Reactive graph subscriptions                  | Change feeds / CDC for agent sync            | Not applicable                                        |
