# Hive-Server Current State

## Project Overview

**hive-server** is a REST API service for cross-agent memory and task coordination. It's a single-binary Go application backed by SQLite for persistence. The project is designed to facilitate communication between distributed agents, providing shared memory, task management, and agent heartbeat tracking.

- **Language**: Go 1.25.1
- **Database**: SQLite (pure Go via modernc.org/sqlite, no CGO)
- **Web Framework**: chi v5 (lightweight router)
- **Location**: `/Users/shakefu/git/christmas-island/hive-server/`

---

## 1. Project Structure

```
hive-server/
├── cmd/app/                  # CLI entrypoint
│   ├── main.go              # CLI bootstrap with signal handling
│   └── serve.go             # HTTP server setup command
├── internal/
│   ├── handlers/            # HTTP API handlers (chi routes)
│   │   ├── handlers.go      # Route definitions, auth middleware
│   │   ├── memory.go        # Memory CRUD handlers
│   │   ├── memory_test.go   # Memory handler tests
│   │   ├── tasks.go         # Task CRUD handlers
│   │   ├── tasks_test.go    # Task handler tests
│   │   ├── agents.go        # Agent heartbeat handlers
│   │   ├── agents_test.go   # Agent handler tests
│   │   └── handlers_test.go # Auth & common test utilities
│   ├── store/               # SQLite persistence layer
│   │   ├── store.go         # Store initialization, schema, migrations
│   │   ├── store_test.go    # Store setup tests
│   │   ├── memory.go        # Memory CRUD operations
│   │   ├── memory_test.go   # Memory store tests
│   │   ├── tasks.go         # Task CRUD & state machine
│   │   ├── tasks_test.go    # Task store tests
│   │   ├── agents.go        # Agent registration & heartbeat
│   │   └── agents_test.go   # Agent store tests
│   └── log/                 # Logging utilities
│       └── log.go           # Custom logging with level/writer management
├── k8s/                     # Kubernetes manifests
│   ├── deployment.yaml      # Pod spec with env, volumes, probes
│   ├── service.yaml         # ClusterIP + LoadBalancer services
│   ├── ingress.yaml         # Nginx ingress + DigitalOcean LB options
│   ├── hpa.yaml             # Horizontal Pod Autoscaler config
│   ├── pdb.yaml             # Pod Disruption Budget
│   ├── serviceaccount.yaml  # ServiceAccount + RBAC Role/RoleBinding
│   ├── kustomization.yaml   # Kustomize base configuration
│   └── README.md            # K8s deployment guide
├── .github/
│   └── workflows/
│       ├── ci.yaml          # Lint & test on PRs
│       ├── release.yaml     # Semantic release + Docker publish
│       └── common-repo-check.yaml  # Daily scaffold sync check
├── Dockerfile               # Multi-stage Alpine build
├── .goreleaser.yaml         # Binary & Docker build config
├── .releaserc.yaml          # Semantic Release configuration
├── .pre-commit-config.yaml  # Pre-commit hooks (linting, security)
├── .golangci.yaml           # Go linter config (minimal setup)
├── .common-repo.yaml        # Scaffold inheritance config
└── README.md                # API documentation & quick start
```

---

## 2. Go Module Dependencies

**Direct Dependencies**:

- `github.com/go-chi/chi/v5 v5.2.5` - HTTP router with middleware
- `github.com/google/uuid v1.6.0` - UUID generation for task IDs
- `github.com/spf13/cobra v1.10.1` - CLI framework
- `modernc.org/sqlite v1.46.1` - Pure Go SQLite driver (no CGO required)

**Key Characteristics**:

- Minimal dependencies (4 direct)
- No ORM (direct database/sql)
- No external logging framework (custom implementation)
- Single-binary deployable

---

## 3. API Routes and Endpoints

All endpoints under `/api/v1` prefix require authentication (Bearer token) unless `HIVE_TOKEN` is empty (dev mode).

### Authentication

- Header: `Authorization: Bearer <token>`
- Header: `X-Agent-ID` (optional, injected into context for attribution)
- If `HIVE_TOKEN` env var is empty, auth is disabled (local development)

### Health Probes (no auth)

- `GET /health` → `{"status": "ok"}`
- `GET /ready` → `{"status": "ready"}`

### Memory Endpoints

| Method | Path                   | Description                                                                            |
| ------ | ---------------------- | -------------------------------------------------------------------------------------- |
| POST   | `/api/v1/memory`       | Create or update entry (key, value, tags, optional version for optimistic concurrency) |
| GET    | `/api/v1/memory`       | List entries (query: `tag`, `agent`, `prefix`, `limit`, `offset`)                      |
| GET    | `/api/v1/memory/{key}` | Retrieve single entry                                                                  |
| DELETE | `/api/v1/memory/{key}` | Delete entry                                                                           |

### Task Endpoints

| Method | Path                 | Description                                                            |
| ------ | -------------------- | ---------------------------------------------------------------------- |
| POST   | `/api/v1/tasks`      | Create task (title required, description, priority 0-4, tags)          |
| GET    | `/api/v1/tasks`      | List tasks (query: `status`, `assignee`, `creator`, `limit`, `offset`) |
| GET    | `/api/v1/tasks/{id}` | Get task with notes                                                    |
| PATCH  | `/api/v1/tasks/{id}` | Update status/assignee/append note                                     |
| DELETE | `/api/v1/tasks/{id}` | Delete task                                                            |

**Task Status State Machine**: `open` → `claimed` → `in_progress` → `done`/`failed`/`cancelled`

### Agent Endpoints

| Method | Path                            | Description           |
| ------ | ------------------------------- | --------------------- |
| POST   | `/api/v1/agents/{id}/heartbeat` | Register/update agent |
| GET    | `/api/v1/agents`                | List all agents       |
| GET    | `/api/v1/agents/{id}`           | Get agent by ID       |

---

## 4. Data Models

### MemoryEntry

- Key (PK), Value, AgentID, Tags (JSON array), Version (optimistic concurrency), timestamps

### Task

- ID (UUID), Title, Description, Status (state machine), Creator, Assignee, Priority (0-4), Tags, Notes (separate table), timestamps

### Agent

- ID (PK), Name, Status (online/idle/offline), Capabilities (JSON array), LastHeartbeat, RegisteredAt
- Auto-marked offline if heartbeat > 5 minutes old

---

## 5. Database Schema

SQLite with WAL mode, foreign keys enabled, max 1 connection (single writer).

**Tables**: `memory`, `tasks`, `task_notes`, `agents`

- All use TEXT for timestamps (RFC3339Nano)
- JSON arrays stored as TEXT
- Indexes on memory(agent_id), tasks(status/assignee/creator), task_notes(task_id)

---

## 6. Infrastructure

### Docker

- Multi-stage Alpine 3.21 build, single binary, non-root user

### Kubernetes

- 3 replicas, rolling update, DigitalOcean registry
- Security: non-root UID 1000, read-only root fs, drop ALL capabilities
- Resources: 256Mi-512Mi RAM, 100m-500m CPU
- HPA: min 1, max 1 (SQLite limitation)

### CI/CD

- **ci.yaml**: Pre-commit hooks + go test on PRs
- **release.yaml**: Semantic release + GoReleaser + Docker publish to ghcr.io
- **common-repo-check.yaml**: Daily scaffold sync from go-scaffold

---

## 7. Key Architectural Patterns

1. **Interface-Based Store**: Handlers depend on `Store` interface, enabling mocking
2. **Optimistic Concurrency**: Memory entries support version-based conflict detection
3. **State Machine Validation**: Tasks enforce valid status transitions
4. **Signal Handling**: Graceful shutdown on SIGINT/SIGTERM
5. **Middleware Chain**: Auth, request ID, panic recovery (chi middleware)
6. **Error Sentinel Values**: `ErrNotFound`, `ErrConflict`, `ErrInvalidTransition`
7. **Container-Native**: Single binary, Alpine base, non-root, read-only filesystem
8. **Semantic Release**: Automated versioning based on conventional commits
