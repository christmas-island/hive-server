# CLAUDE.md

hive-server: REST API for cross-agent memory and task coordination. CockroachDB-backed, single binary.

## Structure

- `cmd/app/` — CLI entrypoint (cobra), HTTP server setup
- `internal/handlers/` — HTTP handlers, chi router, auth middleware
- `internal/store/` — CockroachDB (PostgreSQL) persistence, migrations
- `internal/log/` — Logging
- `script/` — Lifecycle scripts (bootstrap, setup, test, server)
- `.github/` — CI/CD workflows

## Key Patterns

- Auth: Bearer token via `HIVE_TOKEN` env var. If empty, auth is disabled (local dev).
- Agent ID: `X-Agent-ID` header, injected into context by middleware.
- API prefix: `/api/v1/`
- Health probes: `/health`, `/ready` (outside API prefix, no auth)
- Router: chi v5
- Store interface allows mocking in tests
- Database driver: `github.com/jackc/pgx/v5/stdlib` via `database/sql`

## Database Configuration

| Variable       | Description                              | Default                                                  |
| -------------- | ---------------------------------------- | -------------------------------------------------------- |
| `DATABASE_URL` | PostgreSQL/CockroachDB connection string | `postgresql://root@localhost:26257/hive?sslmode=disable` |

CLI flag `--database-url` overrides the env var.

Store tests and handler integration tests require `DATABASE_URL` to be set. They are skipped automatically when no live DB is available.

## Conventions

- Idiomatic Go, `golangci-lint` for linting
- Tests alongside code (`_test.go`)
- Multi-stage Docker builds
- k8s manifests managed externally (christmas-island/k8s repo)
- SQL placeholders: `$1`, `$2`, … (PostgreSQL style — never `?`)
- JSON columns use `JSONB` type for indexing support
