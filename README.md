# hive-server

REST API for cross-agent memory and task coordination. SQLite-backed, single binary.

## Run

```bash
go run ./cmd/app serve
```

Default: `0.0.0.0:8080`. Override with `--bind` or `PORT` env var.

## Environment Variables

| Variable       | Description                                 | Default         |
| -------------- | ------------------------------------------- | --------------- |
| `PORT`         | Listen port                                 | `8080`          |
| `HIVE_TOKEN`   | Bearer token for API auth (empty = no auth) | _(none)_        |
| `HIVE_DB_PATH` | SQLite database path                        | `/data/hive.db` |

## API

All endpoints are under `/api/v1`. Auth via `Authorization: Bearer <token>` header. Agent identification via `X-Agent-ID` header.

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
| `GET`    | `/tasks`      | List tasks (query: `status`, `assignee`, `limit`)                 |
| `GET`    | `/tasks/{id}` | Get task by ID                                                    |
| `PATCH`  | `/tasks/{id}` | Update task (`status`, `note`)                                    |
| `DELETE` | `/tasks/{id}` | Delete task                                                       |

Priority: 0=unset, 1=low, 2=medium, 3=high, 4=critical.
Status: `open`, `claimed`, `in_progress`, `done`, `failed`, `cancelled`.

### Agents

| Method | Path                     | Description                                             |
| ------ | ------------------------ | ------------------------------------------------------- |
| `POST` | `/agents/{id}/heartbeat` | Register/refresh agent (body: `status`, `capabilities`) |
| `GET`  | `/agents`                | List all agents                                         |
| `GET`  | `/agents/{id}`           | Get agent by ID                                         |

### Health

| Path      | Description     |
| --------- | --------------- |
| `/health` | Liveness probe  |
| `/ready`  | Readiness probe |

## Build

```bash
go build -o hive-server ./cmd/app
```

## Docker

```bash
docker build -t hive-server .
docker run -p 8080:8080 -v hive-data:/data hive-server serve
```

## Structure

```text
cmd/app/          CLI entrypoint (cobra)
internal/
  handlers/       HTTP handlers (chi router)
  store/          SQLite persistence
  log/            Logging
script/           Lifecycle scripts (bootstrap, setup, test, server)
```
