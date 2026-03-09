# Gel DB (formerly EdgeDB) Technology Brief

## What Is Gel

Gel is a **graph-relational database** built on top of PostgreSQL. Renamed from EdgeDB in February 2025. Apache 2.0 licensed. It compiles its own query language (EdgeQL) and schema system down to efficient PostgreSQL operations.

- **Not an ORM** — full database server with its own protocol and query language
- **Not a PostgreSQL fork** — separate server process using PostgreSQL as storage backend
- **Safety net**: data always accessible via standard PostgreSQL tools

---

## EdgeQL Query Language

Designed as a spiritual successor to SQL:

- `{ curly braces }` for scopes, `:=` for assignment
- Statically typed, composable, no JOINs needed
- Path expressions traverse links directly

```edgeql
SELECT Movie {
    title,
    release_year,
    director: { name },
    actors: { name }
}
FILTER .release_year > 2020
ORDER BY .title;
```

**SQL Support (Gel 6.0+)**: Full native SQL support alongside EdgeQL. ORMs like Drizzle supported, Prisma/SQLAlchemy in progress.

---

## Architecture

```
Client Application
    | (Gel binary protocol / PostgreSQL wire protocol)
Gel Server (query compilation, schema management, auth, extensions)
    | (Internal PostgreSQL protocol)
PostgreSQL (storage engine, query execution, MVCC, indexing)
```

- Default port: 5656
- One EdgeQL query = one SQL query (no N+1)
- **Minimum 1 GB RAM** for Gel server process
- Can use bundled PostgreSQL or external via `GEL_SERVER_BACKEND_DSN`

---

## Go Client: gel-go

**Package**: `github.com/geldata/gel-go` (v1.4.3, Apache 2.0)

```go
client, err := gel.CreateClient(gelcfg.Options{})
defer client.Close()

var users []User
err := client.Query(ctx, `SELECT User { id, name }`, &users)
```

### Key Methods

| Method                                 | Description                 |
| -------------------------------------- | --------------------------- |
| `Query(ctx, cmd, &out, args...)`       | Multiple results into slice |
| `QuerySingle(ctx, cmd, &out, args...)` | Single result               |
| `QueryJSON(ctx, cmd, &out, args...)`   | Results as JSON bytes       |
| `Execute(ctx, cmd, args...)`           | No return value             |
| `QuerySQL(ctx, cmd, &out, args...)`    | SQL query (not EdgeQL)      |

### Transactions

```go
err := client.Tx(ctx, func(ctx context.Context, tx geltypes.Tx) error {
    return tx.Execute(ctx, `INSERT User { name := 'Alice' }`)
})
// Automatic retry on transient failures
```

### Code Generator: edgeql-go

Creates type-safe Go functions from `.edgeql` files:

```bash
go install github.com/edgedb/edgedb-go/cmd/edgeql-go@latest
```

---

## Schema Definition (SDL)

Declarative `.esdl` files as single source of truth:

```sdl
type User extending HasTimestamps {
    required name: str;
    required email: str { constraint exclusive; };
    multi tasks: Task;
}

type Task extending HasTimestamps {
    required title: str;
    description: str;
    required status: Status;
    required creator: User;
    assignee: User;
    multi tags: str;
    multi notes: TaskNote;
}

scalar type Status extending enum<'open', 'claimed', 'in_progress', 'done', 'failed', 'cancelled'>;

abstract type HasTimestamps {
    required created_at: datetime { default := datetime_current(); };
    required updated_at: datetime { default := datetime_current(); };
}
```

### Key Schema Features

- **Links**: first-class relationships (no foreign keys/JOINs)
- **Abstract types**: mixins/inheritance
- **Constraints**: exclusive (unique), min/max, custom
- **Computed properties**: derived values
- **Indexes**: simple, composite, expression-based
- **Enums**: scalar type extending enum
- **Access policies**: row-level security

---

## Migration System

Database-native, diff-based migrations:

1. Edit `.esdl` schema files
2. `gel migration create` — server compares schema vs current state, generates migration
3. `gel migrate` — idempotent, applies only unapplied migrations
4. Self-tracking: no separate migration table needed

---

## Key Features

- **Built-in Auth Extension**: Email/password, OAuth, WebAuthn, Magic Links, PKCE
- **AI Extension (ext::ai)**: Vector similarity search via pgvector, RAG support
- **GraphQL Extension**: Auto-generated from schema at `/graphql`
- **Prometheus Metrics**: `/metrics` endpoint
- **Health Probes**: `/server/status/alive`, `/server/status/ready`

---

## Deployment

### Docker (recommended)

```yaml
services:
  gel:
    image: geldata/gel
    environment:
      GEL_SERVER_SECURITY: insecure_dev_mode # dev only
    volumes:
      - "./dbschema:/dbschema"
      - "gel-data:/var/lib/gel/data"
    ports:
      - "5656:5656"
```

- **Gel Cloud**: Managed service on AWS, free tier available
- **K8s**: Container deployment alongside hive-server
- **Production**: Use external managed PostgreSQL via `GEL_SERVER_BACKEND_DSN`

---

## Comparison to SQL

| Aspect         | Traditional SQL       | Gel                           |
| -------------- | --------------------- | ----------------------------- |
| Schema         | Imperative migrations | Declarative `.esdl` files     |
| Relationships  | Foreign keys + JOINs  | Links with path syntax        |
| Nested queries | Complex JOINs/CTEs    | Natural `{ }` nesting         |
| Results        | Flat rows             | Structured objects            |
| Type safety    | Runtime errors        | Static type inference         |
| Migrations     | External tools        | Built-in, diff-based          |
| N+1 queries    | Common ORM problem    | Impossible (1 EdgeQL = 1 SQL) |

### Trade-offs

- 1 GB RAM minimum for Gel server
- Learning curve for EdgeQL
- Smaller ecosystem than PostgreSQL
- Additional server process between app and PostgreSQL
- Go client less mature than TypeScript client

---

## Integration with hive-server

### Approach

The existing `Store` interface pattern in hive-server is the ideal integration point. Implement a `GelStore` backend using `gel-go`.

### Key Considerations

1. **RAM**: Gel needs 1 GB minimum — adjust k8s resource requests
2. **Auth**: Keep existing `HIVE_TOKEN` bearer auth (Gel's auth extension is for end-user OAuth)
3. **Store interface**: Support both SQLite (local dev) and Gel (production) backends
4. **Schema**: Graph-relational model maps naturally to agents, tasks, memory relationships
5. **External PostgreSQL**: For production, run Gel against managed PostgreSQL (RDS, Cloud SQL)
6. **CI**: Need `geldata/gel-cli` Docker image in CI pipeline for migrations
