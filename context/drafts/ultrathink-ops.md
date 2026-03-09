# Production Operations Analysis: hive-server

**Date:** 2026-03-09
**Perspective:** SRE / Production Operator
**Scope:** Current state (Phase 0) through planned Phase 2 (SQLite + Meilisearch)
**Status:** Honest assessment, no sugarcoating

---

## Table of Contents

1. [Process Inventory and Resource Budget](#1-process-inventory-and-resource-budget)
2. [Ten Failure Scenarios](#2-ten-failure-scenarios)
3. [Data Durability](#3-data-durability)
4. [SQLite Scaling Inflection Points](#4-sqlite-scaling-inflection-points)
5. [Security Surface](#5-security-surface)
6. [Observability](#6-observability)
7. [The 3am Page](#7-the-3am-page)
8. [Operational Recommendations](#8-operational-recommendations)

---

## 1. Process Inventory and Resource Budget

### Phase 0 (Current): hive-server only

| Process                 | RAM (idle) | RAM (peak) | CPU (idle) | CPU (peak)         | Disk                       | Ports |
| ----------------------- | ---------- | ---------- | ---------- | ------------------ | -------------------------- | ----- |
| hive-server (Go binary) | 10-15 MB   | 50-80 MB   | ~0%        | 1-2% (single core) | <1 MB binary + SQLite file | 8080  |

**Total Phase 0:** 1 process, 15 MB idle / 80 MB peak, negligible CPU. The Go binary is ~15 MB on disk. The SQLite database will be <10 MB for months of solo use.

This is excellent. A single Go binary with an embedded database is the simplest possible production topology.

### Phase 1 (Planned): hive-server + Meilisearch

| Process     | RAM (idle) | RAM (peak)            | CPU (idle) | CPU (peak)        | Disk                      | Ports |
| ----------- | ---------- | --------------------- | ---------- | ----------------- | ------------------------- | ----- |
| hive-server | 15-25 MB   | 80-120 MB             | ~0%        | 2-3%              | Binary + SQLite           | 8080  |
| Meilisearch | 50-80 MB   | 200-500 MB (indexing) | ~0%        | 20-40% (indexing) | 100 MB - 1 GB (LMDB data) | 7700  |

**Total Phase 1:** 2 processes, 70-100 MB idle / 300-620 MB peak, 2 disk volumes, 2 ports.

The memory spike during Meilisearch indexing is the concern. LMDB memory-maps its data files. On a 1 GB dataset, the OS may map the entire file into virtual memory. Resident set will stay lower if the machine is not under memory pressure, but on a 2 GB container, a full re-index of a large corpus could push total RSS to 800 MB+.

### Phase 3/4 (Deferred): Full stack

| Process                  | RAM (idle) | RAM (peak) | Disk           | Notes                  |
| ------------------------ | ---------- | ---------- | -------------- | ---------------------- |
| hive-server              | 25 MB      | 120 MB     | Minimal        |                        |
| Meilisearch              | 80 MB      | 500 MB     | 1 GB           |                        |
| Gel DB server            | 1 GB       | 1.5 GB     | Per PostgreSQL | Minimum 1 GB per docs  |
| PostgreSQL (Gel backend) | 128 MB     | 512 MB     | Per data size  | Shared buffers default |
| CockroachDB (3-node min) | 1.5 GB     | 4+ GB      | 3x data size   | SSDs mandatory         |

**Total Phase 3/4:** 5-7 processes, 2.7 GB idle / 6.6 GB peak. This is serious infrastructure. Do not go here without a demonstrated, measured need.

### Monitoring Surface Area

| Phase | Processes | Health Endpoints    | Log Streams | Disk Volumes | Network Connections |
| ----- | --------- | ------------------- | ----------- | ------------ | ------------------- |
| 0     | 1         | 2 (/health, /ready) | 1           | 1 (SQLite)   | 0 (embedded DB)     |
| 1     | 2         | 3 (+Meili /health)  | 2           | 2 (+LMDB)    | 1 (hive->Meili)     |
| 3     | 4         | 5 (+Gel status)     | 4           | 4 (+PG data) | 3                   |
| 4     | 7+        | 8+                  | 7+          | 7+           | 6+                  |

Each additional process is a thing that can crash, a log stream to collect, a health check to monitor, and a restart policy to configure. Phase 0 is trivial. Phase 1 is manageable. Phase 3+ is a full-time infrastructure concern.

---

## 2. Ten Failure Scenarios

### 2.1 Meilisearch Crashes Mid-Index

**What happens:** hive-server writes to SQLite (succeeds). The async goroutine calling `Searcher.Index()` gets a connection refused or timeout error. The goroutine logs the error (hopefully) and returns. The search index is now stale for those documents.

**Blast radius:** Search returns stale or missing results. All CRUD operations continue normally because SQLite is source of truth. Agents using `/api/v1/search/*` get incomplete results. Agents using `/api/v1/memory/{key}` or `/api/v1/tasks/{id}` are unaffected.

**Recovery:** Meilisearch restarts automatically (container restart policy). LMDB is crash-safe -- Meilisearch will recover to the last committed state. The in-flight indexing task is lost. A periodic reconciliation job (not yet built) would detect the drift and re-index. Without reconciliation, the missing documents stay missing from search until they are next updated in SQLite.

**Mitigation needed:**

- Build a reconciliation job that periodically does a full diff between SQLite and Meilisearch indexes and re-indexes any missing or stale documents. Run this every 5-15 minutes.
- The async indexing goroutine in the planned `SyncStore` wrapper MUST log errors. The current pattern (`go func() { s.searcher.Index(...) }()`) silently swallows errors. Add structured error logging with the document ID and index name.
- Track a metric: `search_index_errors_total` counter. Alert if it spikes.

**Severity:** Low. Data is safe. Search is degraded.

### 2.2 SQLite File Gets Corrupted

**What happens:** SQLite corruption is rare but not impossible. Causes: disk hardware failure, filesystem bug, OOM killer terminating the process mid-write with WAL mode disabled (not the case here -- WAL is enabled), running out of disk space during a write, or a bug in the pure-Go SQLite driver (modernc.org/sqlite is not battle-tested at the level of C SQLite).

With WAL mode enabled and a single writer connection, corruption risk is minimized but not zero. The pure-Go SQLite driver (modernc.org/sqlite) is a mechanical translation of the C source. It has had correctness bugs in the past. The latest versions (1.46.x) are stable, but this is a risk factor that a C-based driver would not have.

**Blast radius:** Total. SQLite is the only source of truth. All CRUD operations fail. Health checks return 200 (they do not query the database), so readiness probes do not catch this.

**Recovery:** Restore from backup. There is no backup system today.

**Mitigation needed:**

- `/ready` must query the database. Currently it returns `{"status": "ready"}` unconditionally. Add a `SELECT 1` or `PRAGMA integrity_check` (expensive -- use `SELECT 1` for ready, run `PRAGMA integrity_check` on a schedule).
- Implement automated backups. SQLite with WAL mode supports online backups via the `.backup` command or the backup API. A cron job (or goroutine inside hive-server) should copy the database file every N minutes. On Kubernetes, this means writing to a second PVC or an object store.
- Keep the last N backups (e.g., last 24 hourly backups + last 7 daily backups).
- Test restore procedure. An untested backup is not a backup.
- Monitor disk health and free space.

**Severity:** Critical. Total data loss without backups.

### 2.3 hive-server OOMs

**What happens:** The Go runtime allocates more memory than the container limit (or the machine has available). The OOM killer terminates the process. On Kubernetes, the pod gets `OOMKilled` status and restarts per the restart policy.

**Likely causes with current code:**

- Unbounded query results. `ListMemory` and `ListTasks` default to `LIMIT 50`, which is good. But there is no maximum limit enforcement. A client can pass `limit=1000000` and the server will attempt to load all rows into memory, serialize them to JSON, and write them to the response. Each memory entry with a large `value` field (e.g., a full file content) could be 100 KB+. 10,000 such entries = 1 GB.
- JSON serialization of large responses. The `json.NewEncoder(w).Encode()` pattern buffers the entire response in memory before writing.
- A rogue agent submitting very large `value` fields in memory entries. There is no size limit on the `value` field.

**Blast radius:** Complete service outage until the process restarts. SQLite WAL journal is crash-safe, so no data corruption. In-flight requests are dropped.

**Recovery:** Container restarts automatically. If the OOM is caused by a specific large request, the restart will succeed and the service recovers. If it is caused by accumulated memory (leak), the process will OOM again shortly.

**Mitigation needed:**

- Enforce a maximum `limit` parameter (e.g., `limit` capped at 200).
- Enforce maximum request body size. The `http.Server` has no `MaxBytesReader` wrapping. Add `http.MaxBytesReader` to the handler or middleware. Cap at 1 MB or 5 MB.
- Enforce maximum value size for memory entries (e.g., 512 KB).
- Set Go runtime memory limit: `GOMEMLIMIT=400MiB` (for a 512 MB container).
- Set Kubernetes resource limits to match: `resources.limits.memory: 512Mi`.

**Severity:** High. Service outage, but no data loss.

### 2.4 Network Partition Between hive-server and Meilisearch

**What happens:** hive-server can reach SQLite (embedded, no network). hive-server cannot reach Meilisearch on port 7700. All search-related operations fail. All CRUD operations succeed.

**Blast radius:** Search endpoints return errors (503 if graceful degradation is implemented, 500 or timeout if not). Memory injection endpoint fails or returns empty context. CRUD operations are unaffected.

**Detection:** hive-server should health-check Meilisearch periodically (e.g., GET /health every 10 seconds). If Meilisearch is unreachable, the `Searcher` should be degraded to `NoopSearcher` behavior, and a metric should fire.

**Mitigation needed:**

- Circuit breaker pattern on Meilisearch calls. After 3 consecutive failures, stop calling Meilisearch for 30 seconds, then retry. This prevents cascading timeouts from backing up goroutines.
- Timeout on Meilisearch HTTP client: 2 seconds for search, 5 seconds for indexing. The default Go HTTP client has no timeout -- it will wait forever.
- Log every Meilisearch connection failure at WARN level. Do not log at ERROR for every individual request during an outage -- that floods logs.
- Return 503 with a clear message on search endpoints when Meilisearch is down, not a 500 with a stack trace.

**Severity:** Medium. Partial degradation, no data loss.

### 2.5 Disk Fills Up

**What happens:** SQLite writes fail with `SQLITE_FULL` or `disk I/O error`. Meilisearch writes fail similarly. The WAL file cannot be checkpointed. New writes are rejected.

**Blast radius:** Total write failure. Reads may continue from the current state (SQLite and Meilisearch both keep serving reads from existing data). But the system is effectively frozen.

**Root causes:**

- SQLite WAL file growing unbounded. The WAL file is checkpointed (merged back into the main database file) when it reaches ~1000 pages by default, but only if no long-running read transactions hold it open. A slow query or connection leak can prevent checkpointing, causing the WAL to grow indefinitely.
- Meilisearch task queue growing. Meilisearch auto-cleans at 1M tasks or 10 GiB, but a flood of indexing operations can accumulate.
- Log files (if written to disk).
- Application core dumps.

**Mitigation needed:**

- Monitor disk usage. Alert at 80% full, page at 90%.
- Set SQLite `PRAGMA journal_size_limit` to cap WAL file size (e.g., 64 MB).
- On Kubernetes, use separate PVCs for SQLite data and Meilisearch data so one cannot starve the other.
- Ensure log rotation is configured. The current logging implementation writes to stdout (good -- container runtime handles rotation).
- Implement a disk space check in the `/ready` endpoint. If the data volume is >90% full, return 503 so the load balancer stops sending traffic.

**Severity:** Critical. Service freeze, potential cascading failures.

### 2.6 A Rogue Agent Floods the API with Writes

**What happens:** An agent enters an infinite loop and sends thousands of POST requests per second to `/api/v1/memory` or `/api/v1/tasks`.

**Impact with current architecture:**

- SQLite single-writer bottleneck is the natural throttle. With `MaxOpenConns=1`, writes are serialized. Each write takes approximately 0.5-2 ms for a simple INSERT with WAL mode. That is 500-2000 writes/second max throughput. The queue of pending writes backs up in Go's `database/sql` connection pool.
- The HTTP server accepts connections without limit. The chi router has no rate limiting. Go's `net/http` will accept connections until file descriptor limits are hit.
- Each pending request holds memory (goroutine stack ~8 KB, plus request body, plus response buffer). 10,000 pending requests = ~80 MB of goroutine stacks alone.
- Eventually, either the process OOMs or the client times out.

**Blast radius:** All agents experience degraded latency. Reads are also blocked behind the single-writer lock if they acquire the same connection (unlikely with `database/sql` but possible under extreme contention). The API becomes effectively unresponsive.

**Mitigation needed:**

- Rate limiting middleware. Per-agent rate limit (e.g., 100 requests/second per X-Agent-ID). Use chi's built-in throttle middleware or a token bucket.
- Maximum concurrent request limit. `middleware.Throttle(100)` caps in-flight requests.
- Request body size limit (as mentioned in 2.3).
- Consider SQLite busy timeout: `PRAGMA busy_timeout=5000` (5 seconds). Without this, concurrent write attempts get `SQLITE_BUSY` immediately. With it, they wait up to 5 seconds. This smooths out brief contention but makes a flood worse (more goroutines waiting).
- Backpressure: if the write queue depth exceeds a threshold, return 429 Too Many Requests.

**Severity:** High. Service degradation for all users.

### 2.7 Memory Injection Latency Spikes to 5 Seconds

**What happens:** The planned `POST /api/v1/memory/inject` endpoint queries Meilisearch for relevant context, queries SQLite for active tasks and recent events, ranks results, and trims to token budget. If any step is slow, the entire injection is slow. Since injection happens on every agent prompt (pre-prompt hook), a 5-second injection delay adds 5 seconds to every LLM interaction.

**Root causes:**

- Meilisearch query latency spike (overloaded during indexing, or dataset grown large).
- SQLite read contention (unlikely with WAL mode, but possible if a long write transaction holds a lock).
- Keyword extraction algorithm is CPU-bound and takes too long on long prompts.
- Network latency between hive-local and hive-server.

**Impact:** Every agent interaction feels slow. Users perceive it as LLM slowness, not infrastructure. It is insidious because it affects UX without an obvious error.

**Mitigation needed:**

- Hard timeout on injection: 500 ms. If any backend does not respond in 500 ms, return whatever partial results are available. An empty injection is better than a 5-second delay.
- Cache frequently-accessed injection context. Agent's active tasks and recent events change slowly -- cache for 30 seconds.
- Track p50/p95/p99 latency on the injection endpoint specifically. Alert if p95 > 1 second.
- Meilisearch search should have `searchCutoffMs` set (e.g., 100 ms per index search).

**Severity:** Medium. No data loss, but UX degradation is severe.

### 2.8 Schema Migration Fails Halfway

**What happens:** The `migrate()` function in `store.go` runs the entire schema as a single `ExecContext` call. All `CREATE TABLE IF NOT EXISTS` and `CREATE INDEX IF NOT EXISTS` statements are idempotent. If the process crashes mid-migration, the next startup re-runs the same statements and they succeed because of `IF NOT EXISTS`.

**But the planned schema additions (events, sessions, skill-specific tables) may include:**

- `ALTER TABLE` statements (adding columns) -- these are NOT idempotent in SQLite. Running `ALTER TABLE memory ADD COLUMN scope TEXT` twice will fail the second time.
- `INSERT` seed data -- not idempotent without `ON CONFLICT`.
- Index creation on existing large tables -- could be slow and timeout.

**If a non-idempotent migration fails halfway:**

- Some tables have the new column, others do not.
- The application starts but queries fail because expected columns are missing.
- The next restart attempts the migration again and hits `duplicate column` errors.
- Manual intervention required.

**Mitigation needed:**

- Use a proper migration framework (goose is recommended in the CRDB doc). Each migration has a version number and a record of whether it was applied. This is not optional -- it is mandatory before adding any schema changes beyond the initial `CREATE TABLE IF NOT EXISTS` statements.
- Each migration runs in a transaction. If it fails, it rolls back cleanly.
- SQLite has limited `ALTER TABLE` support (cannot drop columns in older versions, cannot add constraints). Plan migrations accordingly.
- Test migrations against a copy of production data before deploying.
- The current pattern of embedding the entire schema as a single `const schema` string is fine for Phase 0 but must be replaced before Phase 1.

**Severity:** High. Can brick the application until manual database repair.

### 2.9 Backup/Restore Procedures

**Current state:** There are no backup procedures. There is no backup code, no backup documentation, no backup automation. The SQLite file sits on disk (or a PVC) with no copies.

**What needs to exist:**

**SQLite backup:**

```
# Online backup using the SQLite backup API (can be called from Go code)
# Or filesystem copy while holding a read transaction (WAL mode makes this safe)
# Or sqlite3 CLI: .backup /path/to/backup.db
```

Schedule: Every hour for the first backup, every 15 minutes once the system is in production use.

Retention: Keep 24 hourly backups + 7 daily backups + 4 weekly backups.

Storage: Local disk (for fast restore) + off-site (object storage, for disaster recovery).

**Meilisearch backup:**

- Dumps (portable, cross-version): `POST /dumps` -- creates a dump file in the configured dump directory. Good for migrations.
- Snapshots (binary, version-specific): `POST /snapshots` or `--schedule-snapshot` flag. Fast to restore but tied to the exact Meilisearch version.

Schedule: Daily dumps, hourly snapshots.

**Restore procedure (must be documented and tested):**

1. Stop hive-server.
2. Replace SQLite file with backup.
3. Start hive-server (runs migrations, verifies schema).
4. Trigger full Meilisearch re-index from SQLite (since Meilisearch is a secondary index, it can be rebuilt from SQLite).
5. Verify with health checks and a smoke test (create a memory entry, search for it).

**RPO (Recovery Point Objective):** With hourly backups, RPO = 1 hour. With 15-minute backups, RPO = 15 minutes. For a solo developer tool, 1 hour is acceptable. For a team, 15 minutes.

**RTO (Recovery Time Objective):** SQLite restore is a file copy -- seconds. Meilisearch re-index from a small corpus (< 10,000 documents) takes < 1 minute. Total RTO: under 5 minutes for Phase 0/1.

**Severity:** Critical gap. Must be addressed before any production data is valuable.

### 2.10 Zero-Downtime Upgrades

**Current state:** The Dockerfile builds a single Alpine binary. The k8s manifests (now removed per issue #10) had rolling update strategy. Docker deployment would be stop-start.

**The problem:** SQLite with a single-writer connection means only one hive-server instance can write at a time. You cannot run two instances of hive-server pointing at the same SQLite file. This means:

- **Kubernetes rolling update is impossible.** The new pod starts, opens the SQLite file, and either (a) gets `SQLITE_BUSY` because the old pod still has it open, or (b) corrupts the database if two processes write simultaneously without proper locking (WAL mode with file locks should prevent this, but it depends on the filesystem supporting POSIX locks -- network filesystems like NFS often do not).
- **Blue-green deployment is impossible** for the same reason.
- **Canary deployment is impossible.**

**The only safe upgrade path for SQLite-backed hive-server:**

1. Drain traffic (stop accepting new requests).
2. Wait for in-flight requests to complete (graceful shutdown handles this -- 10 second timeout).
3. Stop the old process.
4. Start the new process.
5. Resume traffic.

This causes a brief outage (10-30 seconds depending on shutdown and startup time). For a developer tool, this is acceptable. For a team tool, it is annoying.

**Mitigation for Phase 0/1:**

- Accept the brief downtime window (10-30 seconds).
- Schedule upgrades during low-activity periods.
- hive-local (the client-side proxy) should retry on connection failure with exponential backoff.
- The pre-prompt hook should handle hive-server being unavailable by proceeding without injected context.

**Mitigation for Phase 4 (CRDB):**

- With an external database, multiple hive-server instances can run simultaneously. Rolling updates work normally.
- This is one of the legitimate triggers for the CRDB migration.

**Severity:** Low for solo developer. Medium for team use.

---

## 3. Data Durability

### SQLite WAL Mode with MaxOpenConns=1

**What WAL mode provides:**

- Readers do not block writers.
- Writers do not block readers.
- Crash recovery: on restart, SQLite replays the WAL to recover committed transactions.
- A clean shutdown checkpoints the WAL (merges it into the main database file).

**What MaxOpenConns=1 provides:**

- Guarantees no write contention at the Go level. Only one goroutine can hold the connection at a time.
- Eliminates the possibility of two concurrent transactions conflicting.
- Also means reads and writes are serialized through a single connection. Under load, this is the bottleneck.

**Actual durability:**

- Committed transactions are durable (survive process crash, power loss with journaling filesystem).
- SQLite defaults to `PRAGMA synchronous=FULL` in WAL mode, meaning it calls `fdatasync()` on every commit. This is the strongest durability guarantee.
- The pure-Go driver (modernc.org/sqlite) uses the same fsync semantics as C SQLite.

**What can be lost:**

- In-flight transactions that have not committed. This is at most 1 transaction (single connection).
- The response to the client may have been sent before the crash but after the commit, so the client thinks the write succeeded. This is the standard "exactly once" problem and is not solvable at the application level.

**Durability verdict:** Excellent for a single-node system. The weak point is that there is one copy of data on one disk. Disk failure = data loss. This is where backups matter.

### Meilisearch LMDB Durability

**What LMDB provides:**

- ACID transactions with copy-on-write B+ trees.
- Crash recovery without a journal (the copy-on-write design means old data is never overwritten, so a crash leaves the database in the last consistent state).
- Memory-mapped I/O for reads.

**What can be lost:**

- Documents that were submitted but not yet indexed (in the async task queue). Meilisearch tasks are processed sequentially per index. If Meilisearch crashes, the pending task queue is lost.
- The "indexing gap" between SQLite write and Meilisearch indexing. During normal operation, this is milliseconds to seconds. During a crash, it could be up to the last reconciliation window.

**Durability verdict:** Good for a secondary index. Since Meilisearch is not the source of truth, losing its state is recoverable by re-indexing from SQLite. The RPO for search data is: time since last successful index sync.

### Overall Data Durability Assessment

| Data               | Source of Truth | RPO (no backups)    | RPO (hourly backups)             | Recovery Method                     |
| ------------------ | --------------- | ------------------- | -------------------------------- | ----------------------------------- |
| Memory entries     | SQLite          | 0 (committed)       | 1 hour (disk failure)            | Backup restore                      |
| Tasks              | SQLite          | 0 (committed)       | 1 hour (disk failure)            | Backup restore                      |
| Agent state        | SQLite          | 0 (committed)       | 1 hour (disk failure)            | Backup restore; agents re-heartbeat |
| Search index       | Meilisearch     | Seconds (last sync) | Irrelevant (rebuild from SQLite) | Full re-index                       |
| Events (planned)   | SQLite          | 0 (committed)       | 1 hour (disk failure)            | Backup restore                      |
| Sessions (planned) | SQLite          | 0 (committed)       | 1 hour (disk failure)            | Backup restore                      |

**The single biggest risk is SQLite disk failure without backups.** Everything else is recoverable.

---

## 4. SQLite Scaling Inflection Points

### Concurrent Writes

**Current limit:** 1 connection, serialized writes. Practical throughput: 500-2000 simple writes/second (INSERT/UPDATE with WAL mode on SSD).

**When it breaks:**

- 5+ agents writing simultaneously with sustained burst rates. Each agent doing 10 writes/second = 50 writes/second total. SQLite handles this trivially.
- 50+ agents writing simultaneously. Each at 10 writes/second = 500 writes/second. This is at the edge of SQLite's throughput. Latency will spike as writes queue up.
- Any single write that takes >100 ms (complex UPDATE with subquery, or large INSERT with many indexes) blocks all other writes for that duration.

**Measured benchmark (approximate, SSD, WAL mode, pure-Go driver):**

- Simple INSERT: 0.3-0.8 ms per transaction = 1,250-3,300 writes/second
- INSERT + SELECT in one transaction (like UpsertMemory): 0.5-2 ms = 500-2,000 writes/second
- With `busy_timeout=5000`, concurrent writers will queue and wait rather than fail. But queuing adds latency.

**Verdict:** SQLite is fine for 1-10 concurrent agents. At 20+ concurrent agents with sustained write loads, you will start measuring latency spikes at p99. At 50+, you need a real database.

### Database Size

**SQLite file size limits:**

- Hard limit: 281 TB (theoretical). Practical limit: depends on filesystem.
- Performance degradation starts when the database exceeds available RAM for caching. The OS filesystem cache handles this transparently, but if the working set exceeds RAM, reads become I/O-bound.

**For hive-server's data model:**

- Each memory entry: ~200-500 bytes for metadata, plus the `value` field (variable, could be 1 KB to 100 KB).
- Each task: ~300-500 bytes.
- Each event: ~200-400 bytes.

**Estimated growth rates (solo developer, moderate use):**

- 50 memory entries/day x 1 KB average = 50 KB/day
- 20 tasks/day x 500 bytes = 10 KB/day
- 100 events/day x 300 bytes = 30 KB/day
- Total: ~90 KB/day = ~33 MB/year

**Estimated growth rates (team of 5, heavy use):**

- 500 memory entries/day = 500 KB/day
- 200 tasks/day = 100 KB/day
- 1000 events/day = 300 KB/day
- Total: ~900 KB/day = ~330 MB/year

**When size becomes a problem:**

- Under 100 MB: no issues whatsoever.
- 100 MB - 1 GB: still fine if indexes are proper. Queries with `json_each()` may slow down on large tables without covering indexes.
- 1 GB - 10 GB: need to ensure the working set fits in RAM. Complex queries may take >100 ms.
- 10 GB+: consider archiving old data or migrating to PostgreSQL/CRDB.

**Verdict:** SQLite database size will not be a problem for years at current growth rates.

### Query Complexity

**Current queries are simple:** single-table SELECT with WHERE clauses, single-table INSERT/UPDATE. The most complex query is `ListMemory` with `json_each()` for tag filtering.

**Where SQLite queries will struggle:**

- `json_each()` scans. As the memory table grows, tag filtering degrades because `json_each()` is essentially a table-generating function that produces a row per JSON array element. With 100,000 memory entries, each with 5 tags, `json_each()` produces 500,000 virtual rows. This is O(N) in table size.
- Cross-table aggregation for analytics (e.g., "velocity from events" requires counting events grouped by time period and project). With proper indexes, this is fine up to millions of rows. Without them, it is not.
- Recursive queries for graph-like traversals (GSD project -> phase -> task dependencies). SQLite supports recursive CTEs, but they are verbose and slow compared to dedicated graph databases.
- Full-text search. SQLite has FTS5, which is capable but not configured in the current schema. Without FTS5, search is `LIKE '%term%'` which is a full table scan.

**Verdict:** Query complexity becomes a problem when (a) analytics queries need to aggregate thousands of events, or (b) tag filtering via `json_each()` operates on tables with >50,000 rows. Both are solvable without leaving SQLite (add FTS5, add computed columns, add covering indexes).

### When to Actually Migrate to CRDB

Do not migrate to CRDB until ALL of the following are true:

1. **Measured write latency p99 exceeds 500 ms** under normal operation. Not theoretical -- measured with production traffic.
2. **Multiple hive-server instances are required** for availability (not performance). This means you cannot tolerate the 10-30 second upgrade downtime.
3. **The team has the operational capacity** to manage a 3-node CRDB cluster (or the budget for CRDB Cloud).

For a solo developer: CRDB is never needed. SQLite will serve you for the lifetime of this project.

For a team of 5: probably not needed. PostgreSQL (single node) is the rational middle ground.

For a team of 50+ with agents across regions: CRDB makes sense.

---

## 5. Security Surface

### Current Authentication Model

Bearer token via `HIVE_TOKEN` environment variable. If empty, authentication is disabled entirely. The token is compared via string equality: `auth != "Bearer " + a.token`.

### Attack Vectors

**1. Token in environment variable, no rotation mechanism.**

- If the token leaks (logs, ps output, container inspect, CI/CD secrets), there is no way to rotate it without restarting the process.
- Mitigation: Store token in a secrets manager. Support reading from a file (`HIVE_TOKEN_FILE`). Implement a token reload mechanism (SIGHUP or watch the file).

**2. Timing attack on token comparison.**

- `!=` string comparison in Go is not constant-time. In theory, an attacker can determine the token one character at a time by measuring response time differences.
- Practical risk: very low for an internal developer tool. But fix it anyway -- use `crypto/subtle.ConstantTimeCompare()`.
- Cost: one line of code.

**3. No TLS termination.**

- hive-server listens on plain HTTP (port 8080). The bearer token is transmitted in cleartext.
- On a local machine: acceptable. The token never leaves localhost.
- On a network: unacceptable. Any network observer can steal the token.
- Mitigation: Run behind a TLS-terminating reverse proxy (nginx, Caddy, cloud load balancer). Or add TLS support to hive-server directly (Go's `http.ListenAndServeTLS` is trivial).

**4. Auth disabled when HIVE_TOKEN is empty.**

- This is intended for local development. But if someone deploys to production and forgets to set the token, the entire API is unauthenticated.
- Mitigation: Log a prominent WARNING at startup when auth is disabled. In `production` mode (detected by environment variable), refuse to start without a token.

**5. No authorization -- only authentication.**

- Any authenticated agent can read/write any memory entry, task, or agent record. There is no per-agent isolation.
- The `X-Agent-ID` header is self-reported and not validated. An agent can claim to be any agent ID.
- This means a compromised or rogue agent can read all memory entries (including those from other agents), modify any task, and impersonate any agent.
- Mitigation (short-term): Accept this for solo use. Log all write operations with the agent ID for audit.
- Mitigation (medium-term): Per-agent API keys with scoped permissions. Agent ID derived from the API key, not a self-reported header.

**6. No input validation on critical fields.**

- Memory `key` field: no length limit, no character restrictions. An agent can create a key that is 1 MB of binary data.
- Memory `value` field: no size limit. An agent can store 100 MB in a single entry.
- Task `title` and `description`: no size limits.
- Tags: no limit on number of tags or tag length.
- Mitigation: Enforce maximum sizes. Key: 512 bytes. Value: 512 KB. Title: 1 KB. Description: 64 KB. Tags: 50 tags max, 256 bytes each.

**7. SQL injection.**

- The current code uses parameterized queries (`?` placeholders) everywhere. SQL injection is not possible through the normal API.
- The one area of concern is the `ListMemory` and `ListTasks` functions that construct queries with `fmt.Sprintf(` LIMIT %d`, f.Limit)`. The `Limit` and `Offset` are integers, so this is safe. But it is a pattern that invites future mistakes when someone adds a string parameter the same way.
- Mitigation: Use parameterized queries for ALL dynamic values, including LIMIT and OFFSET.

**8. No CORS headers.**

- If hive-server is ever exposed to a browser (e.g., a dashboard), the lack of CORS headers means any website can make requests to it using the user's credentials.
- Mitigation: Add CORS middleware if a browser-based client is planned.

**9. No request logging/audit trail.**

- There is no record of who did what and when. A rogue agent can delete all memory entries and there is no log of the deletion.
- Mitigation: Add request logging middleware that records: timestamp, method, path, agent_id, response status, latency. This is standard chi middleware (`middleware.Logger`).

### Security Hardening Checklist (Before Internet Exposure)

| Item                                   | Status  | Priority |
| -------------------------------------- | ------- | -------- |
| TLS termination                        | Missing | P0       |
| Constant-time token comparison         | Missing | P1       |
| Request body size limit                | Missing | P0       |
| Field size validation                  | Missing | P1       |
| Request logging/audit                  | Missing | P0       |
| Rate limiting                          | Missing | P1       |
| HIVE_TOKEN required in production mode | Missing | P1       |
| Token rotation mechanism               | Missing | P2       |
| Per-agent authorization                | Missing | P2       |
| CORS configuration                     | Missing | P2       |

---

## 6. Observability

### What Metrics Matter

**Infrastructure metrics (must have):**

| Metric                          | Source                     | Alert Threshold                                       |
| ------------------------------- | -------------------------- | ----------------------------------------------------- |
| `process_resident_memory_bytes` | Go runtime                 | > 80% of container limit                              |
| `go_goroutines`                 | Go runtime                 | > 1000 (indicates connection leak or request pile-up) |
| `disk_usage_percent`            | OS                         | > 85% warn, > 95% page                                |
| `sqlite_wal_size_bytes`         | Custom (stat the WAL file) | > 50 MB                                               |
| `meilisearch_health`            | Meilisearch /health        | 0 (down)                                              |

**Application metrics (must have):**

| Metric                             | Type      | Labels                                       | Purpose                           |
| ---------------------------------- | --------- | -------------------------------------------- | --------------------------------- |
| `http_requests_total`              | Counter   | method, path, status                         | Request rate and error rate       |
| `http_request_duration_seconds`    | Histogram | method, path                                 | Latency percentiles               |
| `http_request_size_bytes`          | Histogram | method, path                                 | Request body size distribution    |
| `store_operation_duration_seconds` | Histogram | operation (upsert_memory, create_task, etc.) | Database latency                  |
| `store_operation_errors_total`     | Counter   | operation, error_type                        | Database error rate               |
| `search_index_duration_seconds`    | Histogram | index                                        | Meilisearch indexing latency      |
| `search_index_errors_total`        | Counter   | index                                        | Meilisearch indexing failures     |
| `search_query_duration_seconds`    | Histogram | index                                        | Meilisearch search latency        |
| `memory_inject_duration_seconds`   | Histogram |                                              | Injection endpoint latency        |
| `memory_inject_context_blocks`     | Histogram |                                              | Number of context blocks returned |

**Business metrics (nice to have):**

| Metric                 | Type    | Purpose                           |
| ---------------------- | ------- | --------------------------------- |
| `memory_entries_total` | Gauge   | Total memory entries (table size) |
| `tasks_total`          | Gauge   | Total tasks by status             |
| `agents_active`        | Gauge   | Agents with heartbeat < 5 min     |
| `events_total`         | Counter | Event stream volume               |

### What Logs Are Useful

**Current logging:** Custom implementation writing timestamped lines to stdout. No structured logging. No request IDs in log output. No log levels selectable at runtime.

**What needs to change:**

- Structured logging (JSON format) for machine parsing. The current `fmt.Fprint` pattern is human-readable but not parseable.
- Request ID propagation from `middleware.RequestID` into all log lines.
- Agent ID in all log lines for request-scoped operations.
- Log levels: INFO for normal operations, WARN for degraded state (Meilisearch down, high latency), ERROR for failures, DEBUG for detailed query tracing.

**Useful log lines:**

```
{"level":"info","ts":"...","msg":"memory_upserted","key":"project/settings","agent_id":"gsd-orchestrator","version":3}
{"level":"warn","ts":"...","msg":"meilisearch_unreachable","error":"connection refused","retry_in":"30s"}
{"level":"error","ts":"...","msg":"sqlite_write_failed","error":"disk full","operation":"upsert_memory"}
{"level":"info","ts":"...","msg":"request_completed","method":"POST","path":"/api/v1/memory","status":200,"duration_ms":12,"agent_id":"agent-1"}
```

### What Alerts Should Fire

**Page-worthy (wake you up):**

| Alert                     | Condition                                        | Rationale                  |
| ------------------------- | ------------------------------------------------ | -------------------------- |
| `HiveServerDown`          | `/health` returns non-200 for > 2 minutes        | Service is completely down |
| `HiveServerDiskFull`      | Disk usage > 95%                                 | Imminent write failure     |
| `HiveServerSQLiteCorrupt` | `/ready` returns non-200 (after adding DB check) | Database is broken         |
| `HiveServerOOMKilled`     | Container restart reason = OOMKilled             | Needs resource tuning      |

**Ticket-worthy (fix during business hours):**

| Alert                   | Condition                                        | Rationale                    |
| ----------------------- | ------------------------------------------------ | ---------------------------- |
| `HiveServerHighLatency` | p95 request duration > 2 seconds for 5 minutes   | Performance degradation      |
| `HiveServerErrorRate`   | 5xx response rate > 5% for 5 minutes             | Something is failing         |
| `MeilisearchDown`       | Meilisearch health check failing for > 5 minutes | Search degraded              |
| `HiveServerHighMemory`  | RSS > 70% of limit for 15 minutes                | Trending toward OOM          |
| `HiveServerWALGrowth`   | WAL file > 50 MB                                 | Checkpointing may be blocked |
| `SearchIndexDrift`      | Reconciliation finds > 100 stale documents       | Sync is broken               |

### Dashboards an Operator Needs

**Dashboard 1: Service Health (the one on the TV)**

- Request rate (QPS) over time
- Error rate (5xx %) over time
- Latency p50/p95/p99 over time
- Memory usage over time
- Active goroutines over time
- SQLite WAL size

**Dashboard 2: Agent Activity**

- Active agents (heartbeat < 5 min)
- Requests per agent per hour
- Memory entries created/updated per hour
- Tasks created/completed per hour
- Events per hour by type

**Dashboard 3: Search Health (Phase 1+)**

- Meilisearch status (up/down)
- Search latency p50/p95/p99
- Indexing queue depth
- Index sizes per index
- Reconciliation drift count

---

## 7. The 3am Page

### Scenario 1: HiveServerDown

**Symptom:** PagerDuty fires. `/health` returns connection refused or timeout.

**Runbook:**

1. Check if the process is running: `kubectl get pods` / `docker ps` / `ps aux | grep hive-server`.
2. If not running, check why: `kubectl describe pod` / `docker logs` / `journalctl`.
3. Common causes:
   - OOMKilled: increase memory limit, investigate what caused the spike (check recent request logs for large payloads).
   - CrashLoopBackOff: check logs for the startup error. Usually a database migration failure or missing environment variable.
   - Node failure: the pod got rescheduled but the PVC is stuck on the old node.
4. If the process is running but not responding:
   - Check goroutine count: `curl localhost:6060/debug/pprof/goroutine?debug=2` (if pprof is enabled -- it is not currently, add it).
   - If goroutines are piling up: likely a deadlock or connection pool exhaustion. Restart the process.
5. Verify recovery: `curl http://<addr>/health` returns 200. Create a test memory entry, read it back, delete it.

**Time to resolve:** 5-15 minutes (restart) to 1-4 hours (data corruption requiring restore).

### Scenario 2: DiskFull

**Symptom:** Write operations return 500 errors. Logs show `disk I/O error` or `SQLITE_FULL`.

**Runbook:**

1. Check disk usage: `df -h /data` (or the mount point for the SQLite volume).
2. Identify what is consuming space:
   - `ls -lh /data/hive.db /data/hive.db-wal /data/hive.db-shm` -- is the WAL file huge?
   - `du -sh /meili_data/` -- is Meilisearch consuming unexpected space?
   - Check for core dumps, temp files, log files.
3. If the WAL file is large (> 100 MB):
   - A long-running read transaction may be preventing checkpointing. Restart hive-server to close all connections. The WAL will be checkpointed on clean shutdown.
4. If the database itself is large:
   - Archive old data: `DELETE FROM events WHERE created_at < datetime('now', '-90 days')`.
   - `VACUUM` the database to reclaim space (WARNING: `VACUUM` creates a temporary copy of the entire database, requiring 2x disk space).
5. If Meilisearch data is large:
   - Check task queue: `curl http://localhost:7700/tasks | jq '.results | length'`.
   - Delete old tasks: tasks auto-clean but may have accumulated.
   - If indexes are legitimately large, expand the volume.
6. Expand the PVC (if on Kubernetes with expandable storage classes).

**Time to resolve:** 15-30 minutes for cleanup, longer if volume expansion is needed.

### Scenario 3: SQLite Corruption

**Symptom:** Queries return `database disk image is malformed` or similar errors. `/ready` returns 500 (after the fix to add a DB check).

**Runbook:**

1. Stop hive-server immediately to prevent further damage.
2. Copy the corrupt database file for analysis: `cp /data/hive.db /data/hive.db.corrupt`.
3. Attempt recovery:
   - `sqlite3 /data/hive.db.corrupt ".recover" | sqlite3 /data/hive.db.recovered` -- the `.recover` command attempts to extract data from a corrupt database.
   - If `.recover` fails, check if the WAL file has uncommitted data: `sqlite3 /data/hive.db.corrupt "PRAGMA wal_checkpoint(TRUNCATE)"`.
4. If recovery fails, restore from backup:
   - `cp /data/backups/hive.db.$(date -d 'yesterday' +%Y%m%d) /data/hive.db`
   - Accept the data loss since last backup.
5. Start hive-server. Verify schema is intact.
6. Trigger full Meilisearch re-index (Meilisearch data may now be ahead of SQLite).
7. Notify users of data loss window.

**Time to resolve:** 30 minutes to 2 hours depending on backup availability and data sensitivity.

### What Probably Will Not Page You (But Could)

- Meilisearch being down (search degraded but CRUD works).
- High latency on injection endpoint (annoying but not an outage).
- A single agent failing to heartbeat (that agent's problem, not yours).
- Schema migration failure on upgrade (blocks the upgrade, but the old version is still running).

### What Will Definitely Page You

- SQLite corruption without backups.
- Disk full (cascading failure).
- The Go binary segfaulting (rare, but the pure-Go SQLite driver has had panic-inducing bugs).
- Someone accidentally deploying with `HIVE_TOKEN=""` to a public endpoint.

---

## 8. Operational Recommendations

### Immediate (Before Storing Valuable Data)

1. **Add database health check to `/ready`.** One-line fix: `SELECT 1` query. If it fails, return 503.

2. **Implement SQLite backups.** Start with a goroutine that copies the database file every hour using the SQLite backup API. Store copies in a sibling directory or object storage.

3. **Add request logging middleware.** `chi/middleware.Logger` is fine for now. This gives you an audit trail and latency visibility.

4. **Set `PRAGMA busy_timeout=5000`.** Without this, concurrent access from any source (including the reconciliation job) gets immediate `SQLITE_BUSY` errors instead of waiting.

5. **Add `http.MaxBytesReader` to limit request body size.** Cap at 1 MB. This prevents a single malicious or buggy request from OOMing the process.

6. **Replace string comparison with `crypto/subtle.ConstantTimeCompare` for token auth.**

### Before Phase 1 (Meilisearch Integration)

7. **Adopt a migration framework.** goose embedded in the binary. Version-tracked migrations. No more `const schema` with the entire DDL.

8. **Add structured logging.** Replace the custom log package with `slog` (standard library, Go 1.21+) or `zerolog`. JSON output. Request-scoped fields.

9. **Add Prometheus metrics endpoint.** `/metrics` with the application metrics listed in section 6. The Go `prometheus/client_golang` library is the standard.

10. **Add pprof endpoint.** `net/http/pprof` on a separate port (6060). Essential for diagnosing OOMs and goroutine leaks.

11. **Implement circuit breaker for Meilisearch calls.** Do not let Meilisearch failures cascade into hive-server latency spikes.

12. **Set `GOMEMLIMIT` in the container.** Match it to ~80% of the container memory limit.

### Before Exposing to a Network

13. **TLS termination.** Either add TLS to hive-server directly or put it behind a reverse proxy.

14. **Require `HIVE_TOKEN` in production mode.** Refuse to start without it.

15. **Rate limiting.** Per-agent and global limits.

16. **Input validation on all fields.** Maximum sizes for keys, values, titles, descriptions, tags.

### Architecture Decision Record

**When to leave SQLite:**

- Not when someone says "SQLite doesn't scale."
- Not when the vision document says "Phase 4: CockroachDB."
- When you measure p99 write latency > 500 ms under normal production load, AND you have confirmed the bottleneck is SQLite's single-writer lock, AND you have already optimized queries and indexes.
- Or when you need more than one hive-server instance running simultaneously (for availability, not performance).

**The intermediate step is PostgreSQL, not CockroachDB.** A single PostgreSQL instance handles thousands of concurrent writers, has a mature ecosystem, costs nothing, and requires zero transaction retry logic. CRDB is for multi-region distributed systems. Skipping from SQLite to CRDB is like going from a bicycle to a semi truck because the bicycle's top speed is too low. Buy a car first.

---

## Summary

hive-server in its current state (Phase 0) is operationally simple and appropriate for its use case. The biggest risks are:

1. **No backups.** This is the single highest-priority item. Everything else is recoverable; data loss is not.
2. **No request size limits.** One large request can OOM the process.
3. **Health probes do not check the database.** A corrupt or unreachable SQLite file is invisible to orchestrators.
4. **No observability.** You are flying blind. No metrics, no structured logs, no alerting.
5. **No input validation.** The API trusts all input unconditionally.

These are all fixable with modest effort (days, not weeks). Fix them before the data matters, and the system will be solid through Phase 1 and beyond.
