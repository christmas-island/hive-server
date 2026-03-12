# hive-server

[![codecov](https://codecov.io/gh/christmas-island/hive-server/branch/main/graph/badge.svg)](https://codecov.io/gh/christmas-island/hive-server)

REST API for cross-agent memory, task coordination, and distributed claims. CockroachDB-backed, single binary.

## Run

```bash
go run ./cmd/hive-server serve
```

Default: `0.0.0.0:8080`. Override with `--bind` or `PORT` env var.

## Environment Variables

| Variable         | Description                                 | Default                                                  |
| ---------------- | ------------------------------------------- | -------------------------------------------------------- |
| `PORT`           | Listen port                                 | `8080`                                                   |
| `HIVE_TOKEN`     | Bearer token for API auth (empty = no auth) | _(none)_                                                 |
| `DATABASE_URL`   | PostgreSQL/CockroachDB connection URL       | `postgresql://root@localhost:26257/hive?sslmode=disable` |
| `WEBHOOK_SECRET` | GitHub webhook HMAC secret                  | _(none)_                                                 |

The `--database-url` CLI flag overrides `DATABASE_URL`.

## Database Setup (CockroachDB)

```bash
# Start a local single-node CockroachDB instance
cockroach start-single-node --insecure --listen-addr=localhost:26257

# Create the database
cockroach sql --insecure -e "CREATE DATABASE IF NOT EXISTS hive;"

# Run the server (schema migrations run automatically on startup)
DATABASE_URL="postgresql://root@localhost:26257/hive?sslmode=disable" go run ./cmd/hive-server serve
```

## API

All endpoints under `/api/v1`. Auth via `Authorization: Bearer <token>` header. Agent identification via `X-Agent-ID` header. OpenAPI docs at `/docs`.

### Memory

| Method   | Path            | Description                                                 |
| -------- | --------------- | ----------------------------------------------------------- |
| `POST`   | `/memory`       | Create or update an entry (`key`, `value`, optional `tags`) |
| `GET`    | `/memory`       | List entries (query: `prefix`, `tag`, `agent`, `limit`)     |
| `GET`    | `/memory/{key}` | Read entry by key                                           |
| `DELETE` | `/memory/{key}` | Delete entry by key                                         |

### Tasks

| Method   | Path          | Description                                                       |
| -------- | ------------- | ----------------------------------------------------------------- |
| `POST`   | `/tasks`      | Create task (`title`, optional `description`, `priority`, `tags`) |
| `GET`    | `/tasks`      | List tasks (query: `status`, `assignee`, `creator`, `limit`)      |
| `GET`    | `/tasks/{id}` | Get task by ID                                                    |
| `PATCH`  | `/tasks/{id}` | Update task (`status`, `note`)                                    |
| `DELETE` | `/tasks/{id}` | Delete task                                                       |

Priority: 0=unset, 1=low, 2=medium, 3=high, 4=critical.
Status: `open`, `claimed`, `in_progress`, `done`, `failed`, `cancelled`.

### Claims

| Method   | Path           | Description                                                   |
| -------- | -------------- | ------------------------------------------------------------- |
| `POST`   | `/claims`      | Create claim (`type`, `resource`, optional `expires_in`)      |
| `GET`    | `/claims`      | List claims (query: `type`, `resource`, `agent_id`, `status`) |
| `GET`    | `/claims/{id}` | Get claim by ID                                               |
| `DELETE` | `/claims/{id}` | Release claim (ownership enforced via `X-Agent-ID`)           |
| `PATCH`  | `/claims/{id}` | Renew claim (ownership enforced via `X-Agent-ID`)             |

Types: `issue`, `review`, `conch`.

### Agents

| Method | Path                     | Description                                             |
| ------ | ------------------------ | ------------------------------------------------------- |
| `POST` | `/agents/{id}/heartbeat` | Register/refresh agent (body: `status`, `capabilities`) |
| `GET`  | `/agents`                | List all agents                                         |
| `GET`  | `/agents/{id}`           | Get agent by ID                                         |
| `POST` | `/agents/{id}/onboard`   | Generate per-agent auth token                           |
| `GET`  | `/agents/{id}/usage`     | Get agent token usage stats                             |

### Sessions

| Method | Path             | Description             |
| ------ | ---------------- | ----------------------- |
| `POST` | `/sessions`      | Create captured session |
| `GET`  | `/sessions`      | List captured sessions  |
| `GET`  | `/sessions/{id}` | Get captured session    |

### Discovery

| Method | Path                            | Description           |
| ------ | ------------------------------- | --------------------- |
| `GET`  | `/discovery/agents`             | List agent metadata   |
| `GET`  | `/discovery/agents/{id}`        | Get agent metadata    |
| `PUT`  | `/discovery/agents/{id}`        | Update agent metadata |
| `GET`  | `/discovery/channels`           | List channels         |
| `GET`  | `/discovery/channels/{id}`      | Get channel           |
| `PUT`  | `/discovery/channels/{id}`      | Update channel        |
| `GET`  | `/discovery/roles`              | List roles            |
| `GET`  | `/discovery/roles/{id}`         | Get role              |
| `PUT`  | `/discovery/roles/{id}`         | Update role           |
| `GET`  | `/discovery/routing/{agent_id}` | Get routing for agent |

### Health & Version

| Path       | Description                                       |
| ---------- | ------------------------------------------------- |
| `/health`  | Liveness probe                                    |
| `/ready`   | Readiness probe                                   |
| `/healthz` | Liveness with DB check (returns `503` if DB down) |
| `/version` | Build version, commit, and date                   |

## Build

```bash
script/build
# or directly:
go build -o hive-server ./cmd/hive-server
```

## Test

```bash
script/test
# or directly:
go test ./...
```

Integration tests require `DATABASE_URL` to be set. They are skipped automatically when no live DB is available.

## Docker

```bash
docker build -t hive-server .
docker run -p 8080:8080 \
  -e DATABASE_URL="postgresql://root@crdb:26257/hive?sslmode=disable" \
  hive-server serve
```

## Structure

```text
cmd/hive-server/  CLI entrypoint (cobra)
internal/
  handlers/       Huma v2 handler registrations
  model/          Domain types and filters
  server/         HTTP server setup, mux, middleware
  store/          CockroachDB persistence and migrations
  relay/          Relay client for status/token proxying
  timing/         Request timing instrumentation
  webhook/        GitHub webhook handling
  log/            Logging
  testutil/       Shared test helpers
script/           Lifecycle scripts (bootstrap, setup, test, build, server)
context/          v5 design documents (future architecture)
docs/             Guides (onboarding)
examples/k8s/     Example Kubernetes manifests
```

## Deployment

- **Live:** `hive.only-claws.net` (DigitalOcean k8s)
- **CI/CD:** GitHub Actions → semantic-release → GHCR → ArgoCD Image Updater auto-sync
- **Image:** `ghcr.io/christmas-island/hive-server:latest`
- **Merge strategy:** Rebase only (`gh pr merge --rebase`)
