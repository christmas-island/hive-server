# CLAUDE.md

hive-server: REST API for cross-agent memory and task coordination.

## Structure

- `cmd/app/` — CLI entrypoint (cobra), HTTP server setup
- `internal/handlers/` — HTTP handlers, chi router, auth middleware
- `internal/store/` — SQLite persistence, migrations
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

## Conventions

- Idiomatic Go, `golangci-lint` for linting
- Tests alongside code (`_test.go`)
- Multi-stage Docker builds
- k8s manifests managed externally (christmas-island/k8s repo)
