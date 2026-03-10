# Decision 003: Directive Content Format

**Date:** 2026-03-10
**Status:** Decided
**Decided by:** Jake + JakeClaw

## Context

What format should directive `content` fields use? Options: plain text, markdown, structured JSON.

## Decision

**Plain text.** Natural language behavioral instructions.

## Rationale

The storage stack determines what works best:

- **CockroachDB** — `content TEXT`. No format constraints.
- **Meilisearch** — Indexes for keyword/BM25 search. Markdown formatting (headers, bullets, code blocks) adds noise to search relevance. Plain text is optimal.
- **Gel DB** — String property. No format preference.

All three favor plain text. Markdown pollutes search with formatting tokens. Structured JSON complicates indexing and requires parsing on every read.

Directive content is the _input_ to LLM recomposition, not the _output_ delivered to agents. The recomposition LLM handles contextual formatting.

## Implications

- Decomposition strips markdown formatting when extracting directives from skill docs
- Meilisearch search quality improves (no formatting noise)
- Recomposition LLM receives clean natural language input
