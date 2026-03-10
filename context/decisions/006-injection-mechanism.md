# Decision 006: Injection Mechanism — Context Engine Hooks

**Date:** 2026-03-10
**Status:** Decided
**Decided by:** Jake + JakeClaw

## Context

How do directives get into agent conversations? Options:

1. Agent explicitly calls `hive cmd=inject` (agent-initiated)
2. Plugin hooks into OpenClaw context assembly (system-initiated)
3. Speculative pre-fetch (expensive, fires before agent processes message)

## Decision

**Use OpenClaw's ContextEngine plugin hooks.** hive-plugin registers lifecycle hooks, not a standalone tool call.

## Key Hooks (from OpenClaw 2026.3.7 ContextEngine API)

- `assemble` — fires during context assembly for LLM. This is where injection happens: call hive-server, get ranked directives, weave micro-prompts into the assembled context.
- `afterTurn` — fires after agent completes a turn. This is where feedback happens: analyze agent's response against injected directives, report followed/ignored/negative outcomes.
- `prepareSubagentSpawn` / `onSubagentEnded` — potential hooks for subagent-specific directive injection and outcome tracking.

## Rationale

- Agent never needs to know about the injection system — it just gets better context automatically
- Feedback loop is invisible — attribution happens in `afterTurn`, not via explicit agent calls
- No speculative pre-fetch needed — injection is part of context assembly, fires when context is actually being built
- Cost controlled: retrieval + ranking is fast (<100ms), recomposition LLM call only fires when directive set changes (cache on sorted directive IDs)
- hive-local still serves MCP for ACP sessions that can't use plugin hooks directly (dual path)

## Implications

- hive-plugin evolves from thin registration shim to context engine plugin
- `cmd=inject` and `cmd=feedback` still exist as explicit tool commands (agent-initiated fallback, MCP path)
- Frequency control (inject every N-th turn, not every turn) lives in the plugin, configured per-agent
- OpenClaw hook surface needs research — exact `assemble` hook contract (what context is available, what can be injected, return format)
- This is the "MCP plugin contract" answer — the contract is the ContextEngine lifecycle API, not tool parameters
