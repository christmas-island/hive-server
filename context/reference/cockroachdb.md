# CockroachDB Technology Brief for hive-server

**Date:** 2026-03-09
**Purpose:** Evaluate CockroachDB as a relational/transactional backend for hive-server, replacing the current SQLite store for production multi-node deployments.

---

## Table of Contents

1. [What is CockroachDB?](#1-what-is-cockroachdb)
2. [Distributed SQL Architecture](#2-distributed-sql-architecture)
3. [SQL Dialect and PostgreSQL Compatibility](#3-sql-dialect-and-postgresql-compatibility)
4. [Go Client Libraries and SDKs](#4-go-client-libraries-and-sdks)
5. [Schema Definition and Migrations](#5-schema-definition-and-migrations)
6. [Key Features](#6-key-features)
7. [Deployment Options](#7-deployment-options)
8. [Comparison with Other Databases](#8-comparison-with-other-databases)
9. [Limitations and Gotchas](#9-limitations-and-gotchas)
10. [Go Server Integration](#10-go-server-integration)
11. [Multi-Tenancy](#11-multi-tenancy)
12. [JSON/Document Storage](#12-jsondocument-storage)
13. [Licensing Considerations](#13-licensing-considerations)
14. [Integration Plan for hive-server](#14-integration-plan-for-hive-server)

---

## 1. What is CockroachDB?

CockroachDB is a cloud-native, distributed SQL database built for applications that require strong consistency, horizontal scalability, and high availability without manual sharding. It was created by Cockroach Labs (founded by ex-Google engineers who worked on Spanner and F1) and is designed to survive datacenter, zone, and regional failures with zero data loss (RPO = 0 seconds) and near-instant recovery (RTO < 9 seconds).

### The Problem It Solves

Traditional relational databases (PostgreSQL, MySQL) are single-node systems. Scaling them horizontally requires manual sharding, read replicas, and complex failover logic, all of which push distributed systems complexity into the application layer. CockroachDB eliminates this by providing:

- **Automatic sharding**: Data is split into 64MB ranges and distributed across nodes
- **Automatic replication**: At least 3 replicas of every range via Raft consensus
- **Serializable transactions**: Full ACID compliance across distributed nodes
- **Transparent failover**: Node failures are handled without application changes
- **Geo-distribution**: Data placement can be controlled at the row level for latency and compliance

In the context of hive-server, CockroachDB solves the limitation of SQLite as a single-node, single-writer datastore. When multiple hive-server instances need to serve agents concurrently across regions, CockroachDB provides the shared transactional backend that SQLite cannot.

---

## 2. Distributed SQL Architecture

CockroachDB implements a layered architecture where a SQL engine sits on top of a distributed, transactional key-value store.

### Architecture Layers

**SQL Gateway Layer**: Every node in a CockroachDB cluster can act as a SQL gateway. Client connections land on any node, and that node parses, plans, and distributes the query. There is no single-master bottleneck.

**Distribution Layer**: Data is organized into key-value pairs, split into ranges (~64MB each), and distributed across nodes. Each range has a _leaseholder_ node responsible for serving reads and coordinating writes.

**Replication Layer**: Ranges are replicated (default 3 replicas) using the Raft consensus protocol. Writes are committed only when a majority of replicas acknowledge, guaranteeing consistency even during node failures.

**Storage Layer**: Each node uses Pebble (a RocksDB-derived LSM-tree storage engine written in Go) for local persistence.

### Distributed SQL (DistSQL) Execution

For queries that touch data on multiple nodes, CockroachDB uses DistSQL: each node performs computation on the data it holds locally, then sends aggregated results to a coordinating node. This dramatically reduces network traffic compared to fetching all rows to a single node.

### Consensus and Consistency

- **Raft consensus**: All writes go through Raft, ensuring linearizable consistency
- **Serializable isolation**: The default (and strongest) isolation level; CockroachDB also supports READ COMMITTED as of recent versions
- **Hybrid-logical clocks (HLC)**: Used for global ordering of events without requiring perfectly synchronized clocks

---

## 3. SQL Dialect and PostgreSQL Compatibility

### Wire Protocol Compatibility

CockroachDB implements the PostgreSQL wire protocol ("pgwire"). Any PostgreSQL client driver can connect to CockroachDB using standard `postgresql://` connection strings.

### SQL Syntax

CockroachDB reuses PostgreSQL's SQL parser and supports the vast majority of PostgreSQL SQL syntax, including:

- Standard DML: `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `UPSERT`
- Joins: `INNER`, `LEFT`, `RIGHT`, `FULL OUTER`, `CROSS`
- Window functions
- CTEs (Common Table Expressions) and recursive CTEs
- `JSONB` type and operators (`->`, `->>`, `@>`, `?`)
- `ON CONFLICT` (upsert) syntax
- `EXPLAIN` / `EXPLAIN ANALYZE`
- User-defined functions (UDFs) and stored procedures (with limitations)
- Triggers (partial support)
- Row-Level Security (RLS)

### Compatibility Gaps

CockroachDB scores approximately 40% on third-party PostgreSQL compatibility indices. The important differences for application developers:

| Feature                                 | Status in CockroachDB                                                                        |
| --------------------------------------- | -------------------------------------------------------------------------------------------- |
| Extensions (PostGIS, pg_trgm, etc.)     | Not supported (limited spatial support built-in)                                             |
| `CREATE DOMAIN`                         | Not supported                                                                                |
| PostgreSQL range types                  | Not supported                                                                                |
| `LISTEN`/`NOTIFY`                       | Not supported                                                                                |
| Advisory locks                          | Not supported                                                                                |
| `%TYPE` / `%ROWTYPE` in PL/pgSQL        | Not supported                                                                                |
| Full-text search (`tsvector`/`tsquery`) | Not supported (use inverted indexes on JSONB or computed columns)                            |
| System catalogs                         | Partially compatible; some behave differently                                                |
| Sequences                               | Supported but behavior differs (distributed sequences use larger increments for performance) |
| `SERIAL` / `AUTOINCREMENT`              | Supported but UUIDs are strongly recommended to avoid hot-spot ranges                        |
| `SELECT FOR UPDATE`                     | Supported but does not prevent phantom reads                                                 |

### Practical Implication for hive-server

The current hive-server SQLite schema uses `?` placeholders, `AUTOINCREMENT`, `json_each()`, and `ON CONFLICT`. These all need adjustment:

- **Placeholders**: Change from `?` (SQLite/MySQL) to `$1, $2, ...` (PostgreSQL/CockroachDB)
- **`AUTOINCREMENT`**: Replace with `SERIAL` or preferably `UUID` primary keys
- **`json_each()`**: Replace with CockroachDB's `jsonb_array_elements()` or JSONB containment operators
- **`ON CONFLICT`**: Supported as-is (PostgreSQL-compatible syntax)
- **`TEXT` timestamps**: Replace with native `TIMESTAMPTZ` columns
- **`INTEGER PRIMARY KEY AUTOINCREMENT`**: Replace with `UUID` or `INT DEFAULT unique_rowid()`

---

## 4. Go Client Libraries and SDKs

### Recommended: pgx (Standalone Mode)

The `jackc/pgx` driver is the recommended Go client for CockroachDB. It is the most performant PostgreSQL driver for Go and is explicitly tested against CockroachDB.

```go
import (
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/cockroachdb/cockroach-go/v2/crdb/crdbpgx"
)
```

**Key packages:**

- `github.com/jackc/pgx/v5` -- PostgreSQL driver (standalone mode, not `database/sql`)
- `github.com/jackc/pgx/v5/pgxpool` -- Connection pool with health checking
- `github.com/cockroachdb/cockroach-go/v2/crdb/crdbpgx` -- CockroachDB transaction retry wrapper for pgx

### Alternative: database/sql Interface

If you prefer the standard library interface:

```go
import (
    "database/sql"
    _ "github.com/jackc/pgx/v5/stdlib"
    "github.com/cockroachdb/cockroach-go/v2/crdb"
)
```

**Key packages:**

- `github.com/jackc/pgx/v5/stdlib` -- Registers pgx as a `database/sql` driver
- `github.com/cockroachdb/cockroach-go/v2/crdb` -- Transaction retry wrapper for `database/sql`
- `github.com/lib/pq` -- Also supported, but pgx is preferred for new projects

### Alternative: GORM

CockroachDB works with GORM using the standard PostgreSQL dialect:

```go
import (
    "gorm.io/gorm"
    "gorm.io/driver/postgres"
    "github.com/cockroachdb/cockroach-go/v2/crdb/crdbgorm"
)
```

GORM adds ORM overhead. For hive-server's relatively simple CRUD patterns, raw pgx or `database/sql` is more appropriate and gives direct control over query construction and transaction retry behavior.

### Node-Aware Connection Pooling

For production deployments, consider `github.com/authzed/crdbpool`, which distributes connections evenly across CockroachDB nodes and retries failed queries against different nodes.

### Recommendation for hive-server

Use **pgx in standalone mode** with **pgxpool** for connection pooling and **crdbpgx** for transaction retries. This provides:

- Best performance (avoids `database/sql` reflection overhead)
- Built-in connection pool with health checking
- CockroachDB-aware transaction retry logic
- Direct compatibility with the PostgreSQL wire protocol

---

## 5. Schema Definition and Migrations

### Migration Tool Options

| Tool               | CockroachDB Support              | Notes                                                                                      |
| ------------------ | -------------------------------- | ------------------------------------------------------------------------------------------ |
| **goose**          | Yes (via PostgreSQL driver)      | Simple, supports Go and SQL migrations, widely used in Go projects                         |
| **golang-migrate** | Yes (via PostgreSQL driver)      | CLI + library, supports embed for bundled migrations                                       |
| **Atlas**          | Yes (native support)             | Declarative schema management, generates migrations from desired state, most sophisticated |
| **Flyway**         | Yes (native CockroachDB dialect) | Enterprise-grade, Java-based, heavier                                                      |
| **dbmate**         | Yes (via PostgreSQL driver)      | Lightweight, language-agnostic                                                             |

### Recommendation for hive-server

**goose** is the best fit. It is:

- Pure Go, integrates as a library or CLI
- Supports both SQL migration files and Go-code migrations
- Uses standard PostgreSQL connection strings
- Widely adopted in the Go ecosystem
- Supports embed.FS for bundling migrations in the binary

### Schema Translation Example

Current hive-server SQLite schema translated to CockroachDB-compatible PostgreSQL:

```sql
-- 001_initial_schema.sql

CREATE TABLE IF NOT EXISTS memory (
    key         TEXT        NOT NULL PRIMARY KEY,
    value       TEXT        NOT NULL DEFAULT '',
    agent_id    TEXT        NOT NULL DEFAULT '',
    tags        JSONB       NOT NULL DEFAULT '[]'::JSONB,
    version     INT8        NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tasks (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    title       TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    status      TEXT        NOT NULL DEFAULT 'open',
    creator     TEXT        NOT NULL,
    assignee    TEXT        NOT NULL DEFAULT '',
    priority    INT4        NOT NULL DEFAULT 0,
    tags        JSONB       NOT NULL DEFAULT '[]'::JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS task_notes (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     UUID        NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    note        TEXT        NOT NULL,
    agent_id    TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agents (
    id              TEXT        NOT NULL PRIMARY KEY,
    name            TEXT        NOT NULL DEFAULT '',
    status          TEXT        NOT NULL DEFAULT 'offline',
    capabilities    JSONB       NOT NULL DEFAULT '[]'::JSONB,
    last_heartbeat  TIMESTAMPTZ NOT NULL DEFAULT now(),
    registered_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_memory_agent     ON memory(agent_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status     ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_assignee   ON tasks(assignee);
CREATE INDEX IF NOT EXISTS idx_tasks_creator    ON tasks(creator);
CREATE INDEX IF NOT EXISTS idx_task_notes_task  ON task_notes(task_id);
```

Key changes from SQLite:

- `TEXT` date columns become `TIMESTAMPTZ` (native timestamp with timezone)
- `INTEGER PRIMARY KEY AUTOINCREMENT` becomes `UUID DEFAULT gen_random_uuid()`
- `TEXT` JSON columns become `JSONB` (enables indexing and operators)
- `?` placeholders become `$1, $2, ...` in queries
- `json_each()` becomes `jsonb_array_elements_text()` or `@>` containment

### Online Schema Changes

CockroachDB performs schema changes online without locking the table. Adding a column, creating an index, or altering constraints all happen in the background while the table remains readable and writable. However, there are constraints:

- DDL statements inside multi-statement transactions can fail at COMMIT time
- Schema changes that require index backfills pause if the node runs out of disk space
- It is safest to run DDL statements as individual, implicit transactions (one statement per transaction)

---

## 6. Key Features

### Distributed Transactions

CockroachDB provides fully serializable ACID transactions across any number of nodes and ranges. The transaction protocol uses:

- **Write intents**: Provisional writes that are resolved at commit time
- **Transaction records**: Track the state of distributed transactions
- **Parallel commits**: An optimization that reduces commit latency by writing intents and the transaction record in parallel
- **Automatic retries**: Server-side retries for read-refresh errors; client-side retries for serialization conflicts (SQLSTATE 40001)

### Geo-Partitioning

CockroachDB supports fine-grained control over data placement:

- **`REGIONAL BY ROW`** tables: Each row specifies its home region; leaseholders are pinned to that region for fast local reads
- **`GLOBAL`** tables: Replicated to all regions for low-latency reads of infrequently-updated reference data
- **Zone configurations**: Pin partitions to specific nodes or regions

### Survivability

- **Zone survival**: Survives the loss of any single availability zone (default)
- **Region survival**: Survives the loss of an entire region (requires 3+ regions, higher write latency)
- **RPO = 0**: No committed data is ever lost
- **RTO < 9 seconds**: Automatic failover without manual intervention

### Change Data Capture (CDC)

CockroachDB provides changefeeds for streaming row-level changes to external systems:

- Sinks: Kafka, cloud storage (S3/GCS), webhooks, or direct SQL connection
- CDC Queries: SQL-like filtering and transformation of the change stream
- Enriched changefeeds: Debezium-compatible event format
- Fault-tolerant: Changefeed jobs survive node failures

This is relevant for hive-server if agents need to subscribe to changes in memory entries or task state.

### Row-Level Security (RLS)

CockroachDB supports PostgreSQL-style RLS policies, enabling tenant isolation at the database level without separate schemas or databases per tenant.

---

## 7. Deployment Options

### Local Development

```bash
# Single-node, no persistence (for testing)
cockroach demo

# Single-node with persistent storage
cockroach start-single-node --insecure --store=path=/tmp/cockroach-data

# Docker single-node
docker run -d --name cockroach -p 26257:26257 -p 8080:8080 \
  cockroachdb/cockroach:latest start-single-node --insecure
```

No license key is required for single-node clusters.

### Docker Compose (Multi-Node Dev)

```yaml
services:
  crdb1:
    image: cockroachdb/cockroach:latest
    command: start --insecure --join=crdb1,crdb2,crdb3
    ports: ["26257:26257", "8080:8080"]
  crdb2:
    image: cockroachdb/cockroach:latest
    command: start --insecure --join=crdb1,crdb2,crdb3
  crdb3:
    image: cockroachdb/cockroach:latest
    command: start --insecure --join=crdb1,crdb2,crdb3
```

Initialize with: `cockroach init --insecure --host=crdb1`

### Kubernetes

CockroachDB provides a Kubernetes Operator (Public Preview as of August 2025, GA planned):

- Custom resources: `CrdbCluster`, `CrdbNode`
- Eliminates StatefulSet pod ordering constraints
- Handles upgrades, scaling, and node replacement
- Production-proven, recommended over raw StatefulSets

For hive-server's existing `k8s/` directory, adding CockroachDB as a StatefulSet or Operator-managed cluster is straightforward.

### CockroachDB Cloud (Managed)

Three tiers:

- **Basic**: Serverless, pay-per-use, scales to zero, multi-tenant
- **Standard**: Dedicated compute, serverless scaling, better isolation
- **Advanced**: Single-tenant, enterprise features, SLAs

Available on AWS, GCP, and Azure. The Basic tier is suitable for early-stage hive-server deployments.

### Recommendation for hive-server

- **Local dev**: `cockroach start-single-node --insecure` or Docker
- **CI/CD**: Docker single-node in test workflows
- **Production**: CockroachDB Cloud Basic/Standard for managed, or Kubernetes Operator for self-hosted
- **SQLite fallback**: Keep the existing SQLite store for single-instance/embedded deployments where no external database is needed

---

## 8. Comparison with Other Databases

### CockroachDB vs. PostgreSQL

| Dimension               | CockroachDB                                | PostgreSQL                               |
| ----------------------- | ------------------------------------------ | ---------------------------------------- |
| Architecture            | Distributed, multi-node                    | Single-node (with read replicas)         |
| Horizontal scaling      | Automatic, add nodes                       | Manual sharding (Citus) or read replicas |
| High availability       | Built-in, multi-active                     | Primary-replica, manual failover         |
| Isolation level         | SERIALIZABLE (default)                     | READ COMMITTED (default)                 |
| Extension ecosystem     | Very limited                               | Very rich (PostGIS, pg_trgm, etc.)       |
| Single-node performance | Slower (distributed overhead)              | Faster for complex queries               |
| Operational complexity  | Lower at scale                             | Lower for single-node                    |
| License                 | Proprietary (CockroachDB Software License) | Truly open source (PostgreSQL License)   |
| Compatibility           | ~40% of PostgreSQL features                | 100% (it is PostgreSQL)                  |

### CockroachDB vs. MySQL

| Dimension          | CockroachDB                 | MySQL                                  |
| ------------------ | --------------------------- | -------------------------------------- |
| Wire protocol      | PostgreSQL                  | MySQL                                  |
| Horizontal scaling | Automatic                   | Vitess, ProxySQL, or application-level |
| Transactions       | Serializable by default     | REPEATABLE READ by default             |
| JSON support       | JSONB with inverted indexes | JSON with generated columns            |

### CockroachDB vs. Other Distributed Databases

| Dimension         | CockroachDB | TiDB             | YugabyteDB           | Cloud Spanner            |
| ----------------- | ----------- | ---------------- | -------------------- | ------------------------ |
| SQL dialect       | PostgreSQL  | MySQL            | PostgreSQL           | GoogleSQL/PostgreSQL     |
| License           | Proprietary | Apache 2.0       | Apache 2.0 (partial) | Proprietary (cloud-only) |
| Self-hosted       | Yes         | Yes              | Yes                  | No                       |
| Geo-distribution  | Yes         | Limited          | Yes                  | Yes                      |
| Serverless option | Yes         | Yes (TiDB Cloud) | Yes                  | Yes                      |

### When CockroachDB Wins

- Multi-region deployments requiring low-latency reads in each region
- Applications that must survive zone or region failures without data loss
- Systems that need horizontal scaling without application-level sharding
- Strong consistency requirements across distributed writes

### When PostgreSQL Wins

- Single-region deployments
- Complex analytical queries (OLAP-like workloads)
- Need for extensions (PostGIS, full-text search, etc.)
- Cost sensitivity (truly free, no licensing concerns)
- Maximum PostgreSQL compatibility

---

## 9. Limitations and Gotchas

### Transaction Retries (Critical)

CockroachDB uses SERIALIZABLE isolation by default. Under contention, transactions will receive `SQLSTATE 40001` (serialization failure) errors and **must be retried by the client**. This is the single most important difference from PostgreSQL for application developers.

```go
// WRONG: no retry handling
tx, _ := db.Begin()
tx.Exec("UPDATE ...")
tx.Commit() // may fail with serialization error, data is lost

// RIGHT: use cockroach-go retry wrapper
err := crdbpgx.ExecuteTx(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
    _, err := tx.Exec(ctx, "UPDATE ...")
    return err
})
```

The `cockroach-go` library provides `crdbpgx.ExecuteTx()` which handles retry logic automatically.

**Key rules for retry-safe transactions:**

- The transaction function must be idempotent (it will be called multiple times on retry)
- Wrap errors with `%w` (not `%s`) to preserve the retryable error chain
- Keep transactions short to minimize contention
- Avoid side effects inside the transaction function (HTTP calls, file writes, etc.)

### Performance Gotchas

- **Sequential IDs cause hot spots**: Never use auto-incrementing integers as primary keys. Use `UUID` (`gen_random_uuid()`) or `unique_rowid()` to distribute writes across ranges.
- **Single-node overhead**: CockroachDB on a single node is slower than SQLite or PostgreSQL because it still runs the full distributed protocol stack.
- **Cross-range transactions**: Transactions touching many ranges are slower than single-range transactions. Design schemas to co-locate related data.
- **OLAP workloads**: CockroachDB is optimized for OLTP. Complex analytical queries with large scans are significantly slower than in PostgreSQL.

### SQL Feature Gaps

- No `LISTEN`/`NOTIFY` (use changefeeds instead)
- No advisory locks
- No PostgreSQL extensions
- No `CREATE DOMAIN`
- No full-text search via `tsvector`/`tsquery`
- Sequence behavior differs (larger increments for distribution)
- Triggers have limited support
- UDFs cannot be used in partial index predicates

### DDL Gotchas

- Run DDL statements as individual implicit transactions (not inside `BEGIN`/`COMMIT` blocks with DML)
- Schema changes are applied asynchronously; the `SHOW JOBS` command tracks progress
- Adding an index to a large table triggers a backfill that consumes I/O
- Some schema changes on the same table cannot run concurrently

### Operational Gotchas

- Minimum 3 nodes for any production cluster (Raft requires a majority)
- SSDs are strongly recommended; spinning disks cause severe performance issues
- Avoid "burstable" or "shared-core" VMs
- Connection distribution matters: if one node gets disproportionate connections, it becomes a bottleneck
- Telemetry cannot be disabled on the free Enterprise tier

---

## 10. Go Server Integration

### Connection Setup

```go
package store

import (
    "context"
    "fmt"

    "github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
    pool *pgxpool.Pool
}

func New(ctx context.Context, connString string) (*Store, error) {
    config, err := pgxpool.ParseConfig(connString)
    if err != nil {
        return nil, fmt.Errorf("parse config: %w", err)
    }

    // Production pool settings
    config.MaxConns = 25
    config.MinConns = 5
    config.MaxConnLifetime = time.Hour
    config.MaxConnIdleTime = 30 * time.Minute
    config.HealthCheckPeriod = time.Minute

    pool, err := pgxpool.NewWithConfig(ctx, config)
    if err != nil {
        return nil, fmt.Errorf("create pool: %w", err)
    }

    return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }
```

Connection string format:

```
postgresql://root@localhost:26257/hive?sslmode=disable
```

### Transaction Retry Pattern

```go
import "github.com/cockroachdb/cockroach-go/v2/crdb/crdbpgx"

func (s *Store) UpsertMemory(ctx context.Context, entry *MemoryEntry) (*MemoryEntry, error) {
    var result *MemoryEntry

    err := crdbpgx.ExecuteTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
        // Check for existing entry
        var existing MemoryEntry
        err := tx.QueryRow(ctx,
            `SELECT key, value, agent_id, tags, version, created_at, updated_at
             FROM memory WHERE key = $1`, entry.Key,
        ).Scan(&existing.Key, &existing.Value, &existing.AgentID,
            &existing.Tags, &existing.Version,
            &existing.CreatedAt, &existing.UpdatedAt)

        if errors.Is(err, pgx.ErrNoRows) {
            // Insert
            err = tx.QueryRow(ctx,
                `INSERT INTO memory (key, value, agent_id, tags, version)
                 VALUES ($1, $2, $3, $4, 1)
                 RETURNING key, value, agent_id, tags, version, created_at, updated_at`,
                entry.Key, entry.Value, entry.AgentID, entry.Tags,
            ).Scan(&result.Key, /* ... */)
            return err
        }
        if err != nil {
            return fmt.Errorf("query memory: %w", err)
        }

        // Optimistic concurrency check
        if entry.Version > 0 && existing.Version != entry.Version {
            return ErrConflict
        }

        // Update
        err = tx.QueryRow(ctx,
            `UPDATE memory SET value = $1, agent_id = $2, tags = $3,
                    version = version + 1, updated_at = now()
             WHERE key = $4
             RETURNING key, value, agent_id, tags, version, created_at, updated_at`,
            entry.Value, entry.AgentID, entry.Tags, entry.Key,
        ).Scan(/* ... */)
        return err
    })

    return result, err
}
```

### Key Differences from SQLite Store

| Aspect       | Current (SQLite)         | CockroachDB                                   |
| ------------ | ------------------------ | --------------------------------------------- |
| Driver       | `modernc.org/sqlite`     | `github.com/jackc/pgx/v5`                     |
| Connection   | File path                | PostgreSQL connection string                  |
| Pool         | MaxOpenConns=1           | pgxpool with 25 connections                   |
| Placeholders | `?`                      | `$1, $2, ...`                                 |
| Timestamps   | `TEXT` (manual parse)    | `TIMESTAMPTZ` (native)                        |
| JSON columns | `TEXT` + `json_each()`   | `JSONB` + `@>`, `jsonb_array_elements_text()` |
| Auto-IDs     | `AUTOINCREMENT`          | `gen_random_uuid()`                           |
| Transactions | `database/sql` tx        | `crdbpgx.ExecuteTx()` with retries            |
| Upsert       | `ON CONFLICT`            | `ON CONFLICT` (same syntax)                   |
| Foreign keys | `PRAGMA foreign_keys=ON` | Always on                                     |

---

## 11. Multi-Tenancy

### CockroachDB's Multi-Tenancy Approaches

CockroachDB supports multiple isolation models for multi-tenant applications:

**1. Row-Level Security (RLS) -- Shared Tables**

All tenant data lives in shared tables. Access is controlled at the row level using RLS policies. This is the most scalable approach and the most relevant for hive-server.

```sql
-- Add tenant_id to all tables
ALTER TABLE memory ADD COLUMN tenant_id UUID NOT NULL;
ALTER TABLE tasks ADD COLUMN tenant_id UUID NOT NULL;

-- Create RLS policy
CREATE POLICY tenant_isolation ON memory
    USING (tenant_id = current_setting('app.tenant_id')::UUID);

ALTER TABLE memory ENABLE ROW LEVEL SECURITY;
ALTER TABLE memory FORCE ROW LEVEL SECURITY;
```

In Go, set the tenant context per-request:

```go
tx.Exec(ctx, "SET app.tenant_id = $1", tenantID)
```

**2. Separate Databases per Tenant**

Strong isolation but operationally expensive. Each tenant gets their own database within the same cluster. Not recommended for hive-server unless the number of tenants is very small.

**3. Schema-per-Tenant**

Middle ground between shared tables and separate databases. Each tenant gets their own schema. Becomes unwieldy at scale.

### Recommendation for hive-server

Use the **shared-table model with a `tenant_id` column** on all tables. RLS policies can be added later if database-level enforcement is needed. For the initial implementation, enforce tenant isolation in the Go application layer (middleware sets tenant_id in context, store layer filters by it). This is simpler and avoids the operational complexity of RLS while maintaining the same data model.

---

## 12. JSON/Document Storage

### JSONB Support

CockroachDB supports the `JSONB` data type with the same operator set as PostgreSQL:

| Operator | Purpose                         | Example                  |
| -------- | ------------------------------- | ------------------------ | ----------- | --------------- | --- | ---------------- |
| `->`     | Get JSON object field (as JSON) | `tags->'name'`           |
| `->>`    | Get JSON object field (as text) | `tags->>'name'`          |
| `@>`     | Contains (left contains right)  | `tags @> '["urgent"]'`   |
| `<@`     | Contained by                    | `'["urgent"]' <@ tags`   |
| `?`      | Key exists                      | `tags ? 'name'`          |
| `?       | `                               | Any key exists           | `tags ?     | array['a','b']` |
| `?&`     | All keys exist                  | `tags ?& array['a','b']` |
| `        |                                 | `                        | Concatenate | `tags           |     | '{"new": true}'` |

### Inverted Indexes (GIN Indexes)

CockroachDB supports inverted indexes on JSONB columns, similar to PostgreSQL's GIN indexes:

```sql
CREATE INVERTED INDEX idx_memory_tags ON memory(tags);
CREATE INVERTED INDEX idx_tasks_tags ON tasks(tags);
```

This enables fast queries like:

```sql
-- Find memories tagged with "important"
SELECT * FROM memory WHERE tags @> '["important"]';

-- Find tasks with specific metadata
SELECT * FROM tasks WHERE metadata @> '{"priority": "high"}';
```

### Forward Indexes on JSON Paths

For frequently-queried JSON paths, computed columns with standard indexes are faster:

```sql
ALTER TABLE tasks ADD COLUMN priority_label TEXT
    AS (tags->>'priority') STORED;
CREATE INDEX idx_tasks_priority_label ON tasks(priority_label);
```

### Performance Considerations

- Inverted indexes are slow on writes (each JSON path creates index entries)
- For bulk loads, create the inverted index after loading data
- Forward indexes on specific JSON paths are faster for known query patterns
- JSONB storage is less space-efficient than normalized relational columns

### Relevance to hive-server

The current hive-server stores tags as `TEXT` JSON and uses SQLite's `json_each()` for searching. Migrating to `JSONB` with inverted indexes will:

- Eliminate manual JSON parsing in Go code
- Enable direct containment queries (`@>`) instead of `json_each()` joins
- Support richer metadata patterns as tool/memory schemas evolve
- Allow the database to optimize JSON queries using inverted index scans

---

## 13. Licensing Considerations

### Current License (as of v24.3+, November 2024)

CockroachDB is no longer open source. The licensing model was consolidated:

**CockroachDB Software License (CSL)**

- Source-available (you can read the code)
- Not OSI-approved open source
- Free for internal use in your own applications
- Cannot be used to offer a competing managed database service

**Enterprise License Tiers:**

| Tier             | Cost                  | Requirements                                |
| ---------------- | --------------------- | ------------------------------------------- |
| Enterprise Free  | Free (annual renewal) | Revenue < $10M, mandatory telemetry, no SLA |
| Enterprise Trial | Free (30 days)        | Evaluation only                             |
| Enterprise       | Paid                  | Revenue >= $10M, full features, support     |

**Key restrictions:**

- No license key required for single-node development clusters
- Enterprise Free requires annual renewal and mandatory telemetry reporting
- Companies with >$10M annual revenue must purchase an Enterprise license for production use
- The license prohibits using CockroachDB to build a competing database-as-a-service offering

### Implication for hive-server

- **Development and testing**: Free, no restrictions, no license key needed
- **Production (sub-$10M revenue)**: Enterprise Free license; requires annual renewal and telemetry
- **Production (over $10M revenue)**: Requires paid Enterprise license
- **CockroachDB Cloud**: Separate pricing; Basic tier is pay-per-usage and avoids self-hosting license concerns entirely

### Open-Source Alternatives

If licensing is a concern:

- **PostgreSQL**: Truly open source, but not distributed
- **YugabyteDB**: Apache 2.0 core, PostgreSQL-compatible, distributed
- **TiDB**: Apache 2.0, MySQL-compatible, distributed

---

## 14. Integration Plan for hive-server

### Architecture: Store Interface Abstraction

hive-server already has a clean separation between handlers and the store layer. The integration strategy is to introduce a store interface and provide both SQLite and CockroachDB implementations:

```
internal/store/
    store.go          -- Store interface definition
    sqlite.go         -- SQLite implementation (current code, renamed)
    cockroach.go      -- CockroachDB/pgx implementation (new)
    migrations/       -- goose migration files
        001_initial.sql
```

### Key Changes Required

1. **Define a Store interface** extracting the current method signatures:

   - `UpsertMemory`, `GetMemory`, `ListMemory`, `DeleteMemory`
   - `CreateTask`, `GetTask`, `ListTasks`, `UpdateTask`, `DeleteTask`
   - `Heartbeat`, `GetAgent`, `ListAgents`
   - `Close`

2. **Refactor SQLite store** to implement the interface (preserve current behavior)

3. **Implement CockroachDB store** using pgx:

   - Use `pgxpool` for connection management
   - Use `crdbpgx.ExecuteTx()` for all transactions
   - Use `$1, $2, ...` placeholders
   - Use native `TIMESTAMPTZ` (no manual timestamp parsing)
   - Use `JSONB` columns with `@>` for tag queries
   - Use `gen_random_uuid()` for ID generation

4. **Add migration support** using goose:

   - Embed migration SQL files in the binary
   - Run migrations on startup (same pattern as current `migrate()`)

5. **Configuration**: Select backend via environment variable:

   ```
   HIVE_DB_DRIVER=sqlite    HIVE_DB_DSN=data/hive.db
   HIVE_DB_DRIVER=cockroach HIVE_DB_DSN=postgresql://root@crdb:26257/hive?sslmode=disable
   ```

### Migration Priorities

**Phase 1: Interface + SQLite refactor**

- Define store interface
- Refactor current code to implement it
- No functional changes, all tests pass

**Phase 2: CockroachDB implementation**

- Add pgx/pgxpool dependency
- Implement CockroachDB store
- Add goose migrations
- Add integration tests (against real CockroachDB in Docker)

**Phase 3: Production hardening**

- Connection pool tuning
- Transaction retry monitoring/metrics
- Health check integration (pool stats)
- Graceful shutdown (drain connections)

### Query Translation Reference

| Operation       | SQLite (current)                              | CockroachDB (target)                           |
| --------------- | --------------------------------------------- | ---------------------------------------------- |
| Tag search      | `json_each(m.tags) WHERE json_each.value = ?` | `WHERE tags @> $1::JSONB` (e.g., `'["tag"]'`)  |
| Upsert          | `ON CONFLICT(id) DO UPDATE SET ...`           | Same syntax                                    |
| Auto-ID         | `uuid.New().String()` in Go                   | `DEFAULT gen_random_uuid()` in DDL, or Go-side |
| Timestamp write | `now.Format(time.RFC3339Nano)`                | `DEFAULT now()` or `$1::TIMESTAMPTZ`           |
| Timestamp read  | Manual `time.Parse()`                         | Native `time.Time` scan via pgx                |
| LIKE prefix     | `key LIKE ?` with `prefix + "%"`              | `key LIKE $1` with `prefix + "%"` (same)       |

---

## Sources

- [CockroachDB Official Site](https://www.cockroachlabs.com/)
- [CockroachDB GitHub Repository](https://github.com/cockroachdb/cockroach)
- [CockroachDB Architecture Overview (System Design Space)](https://system-design.space/en/chapter/cockroachdb-overview/)
- [Life of a Distributed Transaction](https://www.cockroachlabs.com/docs/stable/architecture/life-of-a-distributed-transaction)
- [SQL Layer Architecture](https://www.cockroachlabs.com/docs/stable/architecture/sql-layer)
- [PostgreSQL Compatibility](https://www.cockroachlabs.com/docs/stable/postgresql-compatibility)
- [Why CockroachDB and PostgreSQL Are Compatible](https://www.cockroachlabs.com/blog/why-postgres/)
- [CockroachDB vs. Postgres: Complete Comparison (Bytebase)](https://www.bytebase.com/blog/cockroachdb-vs-postgres/)
- [Build a Go App with CockroachDB and pgx](https://www.cockroachlabs.com/docs/stable/build-a-go-app-with-cockroachdb)
- [Build a Go App with CockroachDB and GORM](https://www.cockroachlabs.com/docs/stable/build-a-go-app-with-cockroachdb-gorm)
- [cockroach-go/crdbpgx Package](https://pkg.go.dev/github.com/cockroachdb/cockroach-go/v2/crdb/crdbpgx)
- [crdbpool: Node-Aware Connection Pooling](https://github.com/authzed/crdbpool)
- [How to Use CockroachDB with Go (OneUptime)](https://oneuptime.com/blog/post/2026-02-02-cockroachdb-go/view)
- [How to Maximize CockroachDB Performance (AuthZed)](https://authzed.com/blog/maximizing-cockroachdb-performance)
- [Install Client Drivers](https://www.cockroachlabs.com/docs/stable/install-client-drivers)
- [Transaction Retry Error Reference](https://www.cockroachlabs.com/docs/stable/transaction-retry-error-reference)
- [Advanced Client-Side Transaction Retries](https://www.cockroachlabs.com/docs/stable/advanced-client-side-transaction-retries)
- [Online Schema Changes](https://www.cockroachlabs.com/docs/stable/online-schema-changes)
- [Known Limitations in CockroachDB v25.4](https://www.cockroachlabs.com/docs/stable/known-limitations)
- [Geo-Partitioning: What Global Data Looks Like](https://www.cockroachlabs.com/blog/geo-partitioning-one/)
- [CockroachDB: The Resilient Geo-Distributed SQL Database (SIGMOD 2020)](https://dl.acm.org/doi/10.1145/3318464.3386134)
- [Row-Level Security Overview](https://www.cockroachlabs.com/docs/stable/row-level-security)
- [Fine Grained Access Control with Row Level Security](https://www.cockroachlabs.com/blog/fine-grained-access-control-row-level-security/)
- [Multi-Tenant Architecture Glossary](https://www.cockroachlabs.com/glossary/serverless/multi-tenant-architecture/)
- [Tenant Isolation with CockroachDB (Andrew Deally)](https://andrewdeally.medium.com/tenant-isolation-with-cockroachdb-85303250ed72)
- [JSONB and Inverted Indexes](https://www.cockroachlabs.com/docs/stable/inverted-indexes)
- [Forward Indexes on JSON Columns](https://www.cockroachlabs.com/blog/forward-indexes-on-json-columns/)
- [JSON in CockroachDB](https://www.cockroachlabs.com/blog/json-coming-to-cockroach/)
- [Change Data Capture Overview](https://www.cockroachlabs.com/docs/stable/change-data-capture-overview)
- [Licensing FAQs](https://www.cockroachlabs.com/docs/stable/licensing-faqs)
- [CockroachDB License Change Coverage (InfoQ)](https://www.infoq.com/news/2024/09/cockroachdb-license-concerns/)
- [CockroachDB Licensing Evolution (DB News)](https://db-news.com/beyond-open-source-analyzing-cockroachdbs-licensing-evolution-and-its-impact-on-the-database-ecosystem)
- [CockroachDB Retires Core Offering (SD Times)](https://sdtimes.com/os/cockroachdb-retires-self-hosted-core-offering-makes-enterprise-version-free-for-companies-under-10m-in-annual-revenue/)
- [CockroachDB on Kubernetes](https://www.cockroachlabs.com/product/kubernetes/)
- [CockroachDB Kubernetes Operator](https://www.cockroachlabs.com/blog/kubernetes-cockroachdb-operator/)
- [Serverless Databases 2026 (DEV Community)](https://dev.to/dataformathub/serverless-databases-2026-why-cockroachdb-is-the-new-standard-390k)
- [CockroachDB 300-Node Cluster Support (2026)](https://www.prnewswire.com/news-releases/cockroach-labs-pushes-distributed-sql-to-new-limits-with-300-node-1pb-cluster-support-302685942.html)
- [Cockroach Labs 2026 Momentum](https://www.prnewswire.com/news-releases/cockroach-labs-accelerates-momentum-into-2026-as-enterprises-rebuild-for-ai-scale-resilience-302660764.html)
