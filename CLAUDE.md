# CLAUDE.md

hive-server: REST API for cross-agent memory, task coordination, and distributed claims. CockroachDB-backed, single binary.

## Quick Reference

```bash
script/build          # Build binary
script/test           # Run tests
script/server         # Start dev server
go build ./cmd/...    # Build directly
go test ./...         # Test directly
```

## Structure

```text
cmd/hive-server/  CLI entrypoint (cobra)
internal/
  handlers/       Huma v2 handler registrations + handler tests (mock store)
  model/          Domain types, filters, session context
  server/         HTTP server setup, mux building, middleware
  store/          CockroachDB persistence, migrations, unit tests (go-sqlmock)
  relay/          Relay client for status/token proxying
  timing/         Request timing instrumentation (X-Total-Ms, X-Processing-Ms, X-DB-Ms headers)
  webhook/        GitHub webhook handling (HMAC validation)
  log/            Logging
  testutil/       Shared test helpers
script/           Lifecycle scripts (bootstrap, setup, test, build, server)
context/          v5 design documents (future architecture — see GUIDANCE.md)
```

## Key Patterns

- **API framework:** Huma v2 with chi v5 adapter. Handlers registered via `huma.Register()`.
- **Auth:** Bearer token via `HIVE_TOKEN` env var. If empty, auth is disabled (local dev). Per-agent tokens via `/agents/{id}/onboard`.
- **Agent ID:** `X-Agent-ID` header, extracted by middleware into context.
- **Session context:** Headers `X-Session-Key`, `X-Session-ID`, `X-Channel`, `X-Sender-ID`, `X-Sender-Is-Owner`, `X-Sandboxed` — persisted on memory/task/claim writes.
- **API prefix:** `/api/v1/`
- **Health probes:** `/health`, `/ready`, `/healthz` (with DB check) — outside API prefix, no auth.
- **Version:** `/version` — returns build-time version, commit, date. Injected via ldflags.
- **OpenAPI:** `/docs` — auto-generated from Huma registrations.
- **Timing:** Response headers `X-Total-Ms`, `X-Processing-Ms`, `X-DB-Ms` on every request.
- **Store interface:** All persistence methods on `store.Store` are mockable via the `handlers.Storer` interface.
- **Database driver:** `github.com/jackc/pgx/v5/stdlib` via `database/sql`.
- **SQL placeholders:** `$1`, `$2`, … (PostgreSQL style — never `?`).
- **JSON columns:** `JSONB` type for indexing support.
- **Migrations:** Auto-run on startup in two phases (tables/columns first, indexes second) for CockroachDB compatibility.
- **Background goroutines:** Claim expiry runs periodically to release expired claims.

## Conventions

- Idiomatic Go, `golangci-lint` for linting
- Tests alongside code (`_test.go`)
- Handler tests use mock store; store tests use `go-sqlmock`
- Integration tests require `DATABASE_URL` (auto-skipped without it)
- Multi-stage Docker builds (golang:1.25-alpine → alpine:3.21)
- k8s manifests in separate repo (`christmas-island/k8s`)
- **Merge strategy:** Rebase only (`gh pr merge --rebase`, no squash)
- **CI:** Lint + Test + Integration + diff coverage (80% threshold on new code)
- **Releases:** semantic-release on main → version tag → goreleaser + Docker build → GHCR

## v5 Future Architecture

See [GUIDANCE.md](GUIDANCE.md) for the v5 behavioral knowledge engine design principles.
Design documents in `context/` — these describe future architecture, not current implementation.
