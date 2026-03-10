# Decision 004: Chain Ordering Semantics

**Date:** 2026-03-10
**Status:** Decided
**Decided by:** Jake + JakeClaw

## Context

Directives in a `DirectiveChain` have `sequence_in_chain` ordering. When the injection pipeline retrieves one chain member, Gel DB expansion pulls the full chain. Question: must agents follow chain order? Are all members always presented?

## Decision

**Present in chain order, don't enforce sequential execution.** The recomposition LLM decides which chain members are relevant.

## Rationale

- Chain order is the natural reading order (authored as a sequence)
- Some chains are "do all in order" (testing workflow), others are "related principles, take what applies"
- The recomposition LLM already handles relevance filtering — it skips irrelevant directives and explains why in `skipped[]`
- Enforcing strict sequential execution would require tracking chain progress state per agent session, adding complexity for minimal benefit

## Implications

- Gel DB `ExpandChains()` returns members sorted by `sequence_in_chain`
- Pipeline passes chain members to recomposition in order
- Recomposition LLM may skip, merge, or reorder based on context
- No chain progress tracking needed in `agent_sessions`
