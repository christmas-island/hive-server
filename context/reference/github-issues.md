# Hive GitHub Issues - christmas-island Org

## Organization Repos (Hive-related)

- **christmas-island/hive-server** — Shared cloud backend for cross-agent memory and task coordination
- **christmas-island/hive-local** — Local MCP server for OpenClaw agents; persistent Go process exposing MCP tools, syncs with hive-server
- **christmas-island/hive-plugin** — OpenClaw plugin for Hive; thin TS registration shim proxying to hive-local Go server

---

## Architecture Overview (from ops#82)

```
OpenClaw Gateway -> hive-plugin (TS) -> hive-local (Go server) -> hive-server (k8s, CockroachDB)
```

### Completed Phases

- **Phase 1: Plugin Foundation** — COMPLETE. Full E2E chain confirmed.
- **Phase 2: Credentialed Exec** — COMPLETE. cmd=shell with GitHub App JWT, per-agent tokens.

### 8 Locked Design Decisions

1. Two repos for local stack (hive-local + hive-plugin)
2. Port 18820 for local server
3. Factory registration pattern
4. HTTP headers for context (X-Agent-ID, X-Session-Key, etc.)
5. CockroachDB for production
6. Huma v2 before CRDB migration
7. Token broker in hive-local
8. Node crypto for JWT

---

## Open Issues - hive-server (14)

### Dependency Graph

```
Phase 3: hive-server Upgrades (NEXT):
  #10 (rm k8s/)            — independent, ready
  #11 (scripts)             — independent, ready
  #20 (project layout)      — first structural change
    -> #16 (Huma v2)        — blocked by #20
         -> #12 (CRDB)      — blocked by #16
              +-> #18 (tx retries, ships WITH #12)
              +-> #13 (test updates)
              +-> #14 (ephemeral CRDB)
              +-> #15 (k8s deploy)
  #17 (E2E tests)           — independent, benefits from #16

Phase 4: Advanced Features:
  #9  (Discovery API)
  #19 (auto-report to only-claws)
  #21 (LSP plugin lifecycle)       — blocked by #22
  #22 (stateful store research)    — research phase
```

### Issue Details

#### #9 — Agent Discovery API

Replace static TOOLS.md blob (burns context tokens) with a dynamic metadata registry. Endpoints: `GET /discovery/agents`, `GET /discovery/channels`, `PUT /discovery/agents/{name}`, etc. Non-goals: secrets, real-time presence.

#### #10 — Remove scaffolded k8s/ directory

Dead manifests from go-scaffold template. Deployment managed externally.

#### #11 — Adopt scripts-to-rule-them-all pattern

`script/bootstrap`, `script/setup`, `script/test`, `script/integration/test`. Blocked on CockroachDB migration for integration tests.

#### #12 — Migrate store from SQLite to CockroachDB

Swap to `pgx/v5`, use `JSONB` for tags/capabilities, add `--database-url` / `DATABASE_URL` config. **Blocked by #16.**

#### #13 — Update unit tests for CockroachDB

Store tests need updating for CRDB. Handler tests (mock-based) unaffected.

#### #14 — Ephemeral CockroachDB for integration tests

Use `cockroach-go/v2/testserver` for ephemeral single-node CRDB.

#### #15 — Update k8s deployment for CockroachDB

Point deployment at CRDB, add `/healthz` endpoint that pings database.

#### #16 — Add Huma v2 API framework

OpenAPI generation on top of chi router. Convert all 12 endpoints. **Blocked by #20.**

#### #17 — E2E smoke tests against live server

`test/e2e/` with `//go:build e2e` tag. Per-run namespacing: `__e2e__{run_id}__`.

#### #18 — CockroachDB transaction retry logic

`RetryTx` helper with exponential backoff + jitter (max 5 retries). **Must ship with #12.**

#### #19 — Auto-report agent activity to only-claws API

Side-effect reporting of agent status and token usage.

#### #20 — Refactor project layout to Go standard conventions

`cmd/app/` → `cmd/hive-server/`, extract `internal/model/`, `internal/server/`. **First in dependency chain.**

#### #21 — LSP plugin: stateful server lifecycle & reconciliation

hive-local LSP capabilities. Blocked by #22.

#### #22 — Research: fast deterministic stateful store for plugin state

Evaluate RocksDB, LMDB, Badger, Pebble, etc. Blocks #21.

---

## Open Issues - ops repo (hive-related)

#### ops#82 — Hive Infrastructure: Implementation Plan (master tracker)

Overarching tracker for entire Hive build-out across all phases.

#### ops#81 — hive-plugin: New repo for OpenClaw TS plugin

Plugin responsibilities: tool registration, tool execute, service registration, hook registration, HTTP route registration, slash commands.

#### ops#71 — Carapace: CockroachDB distributed SQL architecture

Phase 1: Hive backend (self-hosted k8s). Phase 2: Actuate evaluation.

---

## Open Issues - k8s repo (hive-related)

#### k8s#58 — Deploy Gel as AI-native knowledge and search layer

Gel (formerly EdgeDB) for knowledge/search. **Revised architecture from Jake**: No Postgres if avoidable, no vector/embedding search (no GPU). Pipeline: Hive → fan out to Gel (graph-relational) + Meilisearch (full-text/fuzzy) → MasterClaw (in-cluster OpenClaw synthesizes results) → back through Hive.

---

## Key Architectural Insights from Issues

1. **Hive is the coordination hub** — all agents route through it
2. **Gel + Meilisearch are search backends** — Hive fans out queries to both
3. **MasterClaw** — an in-cluster OpenClaw instance that synthesizes search results
4. **No vector/embedding search** — explicit decision (no GPU)
5. **CockroachDB replaces SQLite** — for production scalability
6. **Huma v2** — for OpenAPI spec generation, enables client codegen
7. **Discovery API** — replaces static context blobs, saves tokens
8. **Single-tool pattern** — consolidated from 9 tools to 1 (`hive` with subcommands)
