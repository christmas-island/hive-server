# Developer Experience Analysis: hive-server

**Date:** 2026-03-09
**Perspective:** Developer Experience Advocate
**Inputs:** vision-v2.md, synthesis.md, gsd.md, superpowers.md, allium.md, hive-server-current.md, source code

---

## 1. Five Concrete User Stories (Agent Perspective)

### Story 1: GSD Executor Stores a Task Result

A GSD executor agent finishes implementing a login endpoint. It needs to record the outcome so the orchestrator (in a different session) can see it completed.

**What happens today (no hive-server):**
The executor writes to `.planning/phases/01-auth/01-01-SUMMARY.md`. The orchestrator reads that file next time it runs. If the orchestrator session crashed and restarted, it re-reads STATE.md and hopes the file is consistent.

**What should happen with hive-server:**

```
Agent: gsd-executor-01
Session: ses_a1b2c3

Step 1: Agent claims the task
--> Tool call: hive task update abc123 --status claimed --assignee gsd-executor-01
--> HTTP: PATCH /api/v1/tasks/abc123
    Headers: Authorization: Bearer <token>, X-Agent-ID: gsd-executor-01
    Body: {"status": "claimed", "assignee": "gsd-executor-01"}
<-- 200 OK
    Body: {"id": "abc123", "title": "Create login endpoint", "status": "claimed", ...}

Step 2: Agent starts work
--> Tool call: hive task update abc123 --status in_progress
--> HTTP: PATCH /api/v1/tasks/abc123
    Body: {"status": "in_progress"}
<-- 200 OK

Step 3: Agent completes the task
--> Tool call: hive task update abc123 --status done --note "Implemented POST /api/auth/login with bcrypt hashing. Tests pass: 12/12."
--> HTTP: PATCH /api/v1/tasks/abc123
    Body: {"status": "done", "note": "Implemented POST /api/auth/login with bcrypt hashing. Tests pass: 12/12."}
<-- 200 OK
    Body: {"id": "abc123", "status": "done", "notes": ["Implemented POST /api/auth/login..."]}

Step 4: Agent records a memory for future sessions
--> Tool call: hive memory set "auth/login-implementation" --value "Used bcrypt with cost 12. JWT tokens expire in 24h. Refresh tokens stored in httponly cookies." --tags auth,decisions
--> HTTP: POST /api/v1/memory
    Body: {"key": "auth/login-implementation", "value": "Used bcrypt with cost 12...", "tags": ["auth", "decisions"]}
<-- 200 OK
    Body: {"key": "auth/login-implementation", "version": 1, ...}
```

**What the orchestrator sees next session:**

```
--> Tool call: hive task list --status done
--> HTTP: GET /api/v1/tasks?status=done
<-- 200 OK
    Body: [{"id": "abc123", "title": "Create login endpoint", "status": "done", ...}]
```

The orchestrator now knows the task completed without parsing markdown files.

### Story 2: Superpowers Agent Finds a Prior Solution

An agent using the systematic-debugging skill hits a race condition in a Go HTTP handler. It has seen something similar before -- but that was three sessions ago.

**What happens today (no hive-server):**
The agent has no memory of prior sessions. It starts from scratch, reproducing hours of debugging that already happened.

**What should happen with hive-server:**

```
Agent: debugger-01

Step 1: Agent searches memory for prior race condition fixes
--> Tool call: hive memory list --tag debugging --prefix "race-condition"
--> HTTP: GET /api/v1/memory?tag=debugging&prefix=race-condition
<-- 200 OK
    Body: [
      {
        "key": "race-condition/http-handler-map",
        "value": "Root cause: concurrent map writes in middleware. Fix: sync.RWMutex on shared state. See commit a1b2c3d.",
        "tags": ["debugging", "race-condition", "go"],
        "agent_id": "debugger-01",
        "updated_at": "2026-03-07T14:30:00Z"
      }
    ]
```

The agent reads the prior fix, recognizes the pattern, applies the same approach. Total time: 2 minutes instead of 45.

**Step 2 (future, with Meilisearch):** Instead of exact prefix matching, the agent searches:

```
--> Tool call: hive search memories "race condition concurrent map write handler"
--> HTTP: POST /api/v1/search/memories
    Body: {"query": "race condition concurrent map write handler", "limit": 5}
<-- 200 OK
    Body: {
      "hits": [
        {"key": "race-condition/http-handler-map", "value": "Root cause: ...", "_score": 0.92},
        {"key": "concurrency/goroutine-leak", "value": "...", "_score": 0.54}
      ]
    }
```

### Story 3: GSD Orchestrator Claims a Task (Concurrency Case)

Two GSD orchestrators are running in parallel (different projects). Both see an unclaimed cross-project task ("update shared auth library"). Both try to claim it.

**What should happen:**

```
Orchestrator A:
--> Tool call: hive task update shared-auth-task --status claimed --assignee orch-a
--> HTTP: PATCH /api/v1/tasks/shared-auth-task
    Body: {"status": "claimed", "assignee": "orch-a"}
<-- 200 OK  (wins the claim)

Orchestrator B (arrives 50ms later):
--> Tool call: hive task update shared-auth-task --status claimed --assignee orch-b
--> HTTP: PATCH /api/v1/tasks/shared-auth-task
    Body: {"status": "claimed", "assignee": "orch-b"}
<-- 422 Unprocessable Entity
    Body: {
      "status": 422,
      "title": "Unprocessable Entity",
      "detail": "the requested status transition is not allowed",
      "errors": [{
        "message": "task is already claimed (current status: claimed). To reassign, the current assignee must release it first by setting status to 'open'.",
        "location": "body.status",
        "value": "claimed"
      }]
    }
```

Orchestrator B reads the error, understands the task is taken, moves on. This is a case where error messages directly determine whether the agent can recover or gets stuck in a retry loop.

**Current problem:** The existing error response is `"the requested status transition is not allowed"`. This does not tell the agent WHY the transition failed or what it should do instead. See Section 5 for recommended error messages.

### Story 4: Agent Submits a Session Summary (Future)

At the end of a work session, the agent summarizes what it accomplished for future sessions to find.

```
Agent: gsd-planner-01
Session: ses_x7y8z9

--> Tool call: hive session submit --summary "Planned phase 3: API authentication. Created 3 plans covering JWT, OAuth2, and session management. Decision: JWT with refresh tokens, bcrypt cost 12, 24h access / 30d refresh. Blocked on: unclear requirements for OAuth2 scopes -- need user input." --repo christmas-island/hive-server
--> HTTP: POST /api/v1/sessions
    Body: {
      "agent_id": "gsd-planner-01",
      "repo": "christmas-island/hive-server",
      "summary": "Planned phase 3: API authentication. Created 3 plans covering JWT, OAuth2, and session management. Decision: JWT with refresh tokens, bcrypt cost 12, 24h access / 30d refresh. Blocked on: unclear requirements for OAuth2 scopes -- need user input."
    }
<-- 201 Created
    Body: {"id": "ses_x7y8z9", "agent_id": "gsd-planner-01", ...}
```

**The next session (different agent, possibly different project):**

```
--> Tool call: hive search sessions "OAuth2 scope requirements"
<-- Results include the planning session's summary, pointing the new agent to the blocker.
```

### Story 5: Allium Weed Agent Records Drift

The Weed agent compares an `.allium` spec against the implementation and finds mismatches.

```
Agent: allium-weed-01

--> Tool call: hive memory set "drift/auth-spec/2026-03-09" --value '{"spec": "auth.allium", "mismatches": 3, "categories": {"spec_bug": 1, "code_bug": 1, "aspirational": 1}, "details": "1. LoginRule missing rate limiting (aspirational). 2. PasswordReset sends wrong email template (code_bug). 3. SessionTimeout rule says 30min but spec says 60min (spec_bug)."}' --tags drift,allium,auth
--> HTTP: POST /api/v1/memory
    Body: {
      "key": "drift/auth-spec/2026-03-09",
      "value": "{\"spec\": \"auth.allium\", \"mismatches\": 3, ...}",
      "tags": ["drift", "allium", "auth"]
    }
<-- 200 OK
```

Later, a developer asks "is drift getting better or worse?"

```
--> Tool call: hive memory list --tag drift --prefix "drift/auth-spec/" --limit 30
<-- Returns the last 30 drift reports, sorted by date. Agent can compute trend.
```

This works with the current memory API. It is not elegant -- the value is a JSON string inside a text field. A future `/api/v1/specs/{name}/drift` endpoint would make this structured and queryable. But the point is: agents can start recording drift data TODAY with the existing API, and the structured endpoint can land later.

---

## 2. Developer Experience: Local Setup

### What a Developer Does Today

```bash
# Clone
git clone git@github.com:christmas-island/hive-server.git
cd hive-server

# Build
go build -o hive-server ./cmd/app

# Run
./hive-server serve
# Server starts on :18080 with SQLite at ./hive.db
# No HIVE_TOKEN set = auth disabled (good for local dev)

# Test
curl localhost:18080/health
# {"status":"ok"}

curl -X POST localhost:18080/api/v1/memory \
  -H "Content-Type: application/json" \
  -d '{"key":"test","value":"hello"}'
# {"key":"test","value":"hello","version":1,...}
```

**Time to first successful API call:** Under 60 seconds if Go is installed. This is excellent. Do not break this.

### What Will Break as Complexity Grows

**Problem 1: No `scripts-to-rule-them-all` yet.** Issue #11 exists but is not done. A new developer has to guess that `go build ./cmd/app` is the right command, not `go build .` or `make build` (there is no Makefile). Fix: `script/setup`, `script/server`, `script/test` -- three scripts that never change.

**Problem 2: Meilisearch as optional dependency.** When Phase 1 lands, developers will see search endpoints returning 503 and not know why. The server needs a clear startup log line:

```
2026-03-09T10:00:00Z INF hive-server starting version=1.2.0
2026-03-09T10:00:00Z INF sqlite connected path=./hive.db
2026-03-09T10:00:00Z WRN meilisearch not configured (MEILI_URL not set) search=disabled
2026-03-09T10:00:00Z INF listening addr=:18080
```

Not:

```
2026-03-09T10:00:00Z hive-server started
```

**Problem 3: Database file location.** Today the SQLite path is hardcoded or passed as a flag. For local dev, `./hive.db` is fine. But when hive-server runs as a system service or inside Docker, the path matters. The current `serve.go` needs a `--db` flag or `HIVE_DB_PATH` env var. Check if this exists already.

**Problem 4: Debugging memory injection.** When the memory injection endpoint (`POST /api/v1/memory/inject`) returns wrong context, the developer needs to understand why. The response should include provenance:

```json
{
  "context_blocks": [
    {
      "type": "memory",
      "content": "Used bcrypt with cost 12...",
      "source": "memory:auth/login-implementation",
      "score": 0.92,
      "reason": "matched query terms: bcrypt, auth"
    }
  ],
  "tokens_used": 847,
  "candidates_considered": 23,
  "candidates_returned": 3
}
```

Without `source`, `score`, and `reason`, debugging "why did the injection return irrelevant context?" is impossible.

---

## 3. Tool Design: The `hive` CLI Tool

### Design Principle

The vision documents specify a single-tool pattern: one `hive` tool with subcommands. This is critical for token efficiency -- agents should not learn 15 different tool names.

### Concrete Tool Definition

This is the actual tool definition that would be registered in an agent's MCP config or CLAUDE.md:

```
Tool: hive
Description: Interact with hive-server for cross-session memory, task coordination, and agent management.

Subcommands:

  memory set <key> --value <text> [--tags <comma-separated>]
    Store or update a memory entry. Returns the stored entry with version.

  memory get <key>
    Retrieve a memory entry by key.

  memory list [--tag <tag>] [--prefix <prefix>] [--agent <agent-id>] [--limit <n>]
    List memory entries matching filters.

  memory delete <key>
    Delete a memory entry.

  task create <title> [--description <text>] [--priority <0-4>] [--tags <comma-separated>]
    Create a new task. Returns the task with generated ID.

  task list [--status <status>] [--assignee <agent>] [--limit <n>]
    List tasks matching filters. Status: open, claimed, in_progress, done, failed, cancelled.

  task get <id>
    Get a task by ID, including notes.

  task update <id> [--status <status>] [--assignee <agent>] [--note <text>]
    Update task status, assignee, or append a note.

  task delete <id>
    Delete a task.

  agent heartbeat [--capabilities <comma-separated>] [--status online|idle]
    Register or refresh agent presence.

  agent list
    List all known agents with status.

  agent get <id>
    Get agent details.

  search <query> [--index memories|tasks|sessions|artifacts] [--limit <n>]
    Full-text search across stored data. Requires Meilisearch.

  session submit --summary <text> [--repo <repo>]
    Submit a session summary for future retrieval.

  session list [--repo <repo>] [--limit <n>]
    List session summaries.

  event record --type <event-type> [--payload <json>] [--repo <repo>]
    Record an event for analytics.
```

### How This Maps to hive-local

`hive-local` is the Go binary that runs alongside the agent. It translates tool calls into HTTP requests:

```
hive memory set "auth/decisions" --value "JWT with bcrypt" --tags auth
  |
  v
hive-local constructs:
  POST http://localhost:18080/api/v1/memory
  Authorization: Bearer <token from env>
  X-Agent-ID: <from env HIVE_AGENT_ID>
  Content-Type: application/json
  {"key": "auth/decisions", "value": "JWT with bcrypt", "tags": ["auth"]}
  |
  v
hive-local returns to agent:
  Stored memory entry "auth/decisions" (version 1)
```

The agent never sees HTTP, never constructs JSON, never manages headers. The tool abstracts all of it.

### What hive-local Should NOT Do

- **Parse responses into prose.** Return structured data (JSON) so the agent can reason about it. Do not convert `{"status": "claimed", "assignee": "agent-01"}` into "The task has been claimed by agent-01" -- that wastes tokens.
- **Retry silently.** If a request fails, return the error. The agent decides whether to retry.
- **Buffer writes.** Every write should be synchronous. The agent needs confirmation that its data was stored.

### MCP vs CLI

Two integration paths:

**MCP (Model Context Protocol):** hive-local runs as an MCP server. The agent calls it via MCP tool calls. This is the preferred path for Claude Code because MCP tools are first-class.

**CLI:** hive-local is a binary the agent calls via Bash. This works for any agent that can run shell commands. Slightly higher overhead (process spawn per call) but universally compatible.

Both should support the same subcommand interface. The MCP version returns JSON directly. The CLI version prints JSON to stdout.

---

## 4. Error Messages That Help LLMs Recover

### The Problem

Current error messages in hive-server are developer-facing, not agent-facing. Examples from the codebase:

```go
huma.Error500InternalServerError("failed to upsert memory")
huma.Error422UnprocessableEntity("the requested status transition is not allowed")
huma.Error409Conflict("version conflict: stale data")
huma.Error404NotFound("task not found")
```

An LLM agent receiving `"failed to upsert memory"` has no idea what went wrong or what to do next. Was the key too long? Was the value empty? Is the server down?

### Recommended Error Response Format

Every error should include four fields:

```json
{
  "status": 422,
  "title": "Invalid Status Transition",
  "detail": "Cannot transition task 'abc123' from 'claimed' to 'claimed'. The task is already in status 'claimed'.",
  "recovery": "To reassign this task, first set status to 'open', then claim it with the new assignee. To add work notes without changing status, use the 'note' field only."
}
```

The `recovery` field is the key difference. It tells the agent exactly what to do next. Without it, the agent guesses, often incorrectly.

### Specific Error Messages to Implement

**Task already claimed (422):**

```json
{
  "status": 422,
  "title": "Invalid Status Transition",
  "detail": "Task 'abc123' is currently 'claimed' by agent 'executor-01'. Cannot transition to 'claimed'.",
  "recovery": "This task is already claimed by another agent. Either: (1) choose a different task, (2) wait for the current agent to release it, or (3) if you believe it is abandoned, an admin can force-release it by setting status to 'open'."
}
```

**Version conflict on memory (409):**

```json
{
  "status": 409,
  "title": "Version Conflict",
  "detail": "Memory entry 'auth/config' has been updated since you last read it. Your version: 3, current version: 4.",
  "recovery": "Re-read the entry with 'hive memory get auth/config' to get the latest version, merge your changes, and retry with the current version number."
}
```

**Memory key not found (404):**

```json
{
  "status": 404,
  "title": "Not Found",
  "detail": "No memory entry exists with key 'auth/configg'.",
  "recovery": "Check the key for typos. Use 'hive memory list --prefix auth/' to see available keys under this prefix."
}
```

**Search unavailable (503):**

```json
{
  "status": 503,
  "title": "Search Unavailable",
  "detail": "Meilisearch is not configured. Full-text search requires a running Meilisearch instance.",
  "recovery": "Search is optional. Use 'hive memory list' with --tag and --prefix filters for basic retrieval. For full-text search, ask the system administrator to configure MEILI_URL."
}
```

**Validation error (422):**

```json
{
  "status": 422,
  "title": "Validation Failed",
  "detail": "Field 'title' is required and cannot be empty.",
  "recovery": "Provide a non-empty title when creating a task. Example: hive task create 'Fix login endpoint' --priority 2"
}
```

**Auth failure (401):**

```json
{
  "status": 401,
  "title": "Unauthorized",
  "detail": "Invalid or missing Bearer token in the Authorization header.",
  "recovery": "Set the HIVE_TOKEN environment variable and include it as 'Authorization: Bearer <token>' in the request. For local development, leave HIVE_TOKEN unset to disable auth."
}
```

### Implementation Cost

Adding `recovery` fields to error responses requires touching approximately 15 error sites across `memory.go`, `tasks.go`, and `agents.go` in the handlers package. The Huma v2 error format already supports `detail` as a string field -- the `recovery` field would need a custom error type or could be appended to the `detail` string.

Cheapest approach: concatenate recovery guidance into the existing `detail` field, separated by a newline. No schema changes needed.

```go
// Before:
huma.Error422UnprocessableEntity("the requested status transition is not allowed")

// After:
huma.Error422UnprocessableEntity(fmt.Sprintf(
    "Cannot transition task '%s' from '%s' to '%s'. "+
    "Current status does not allow this transition.\n\n"+
    "Allowed transitions from '%s': %s. "+
    "To see the full task state, use: hive task get %s",
    id, currentStatus, requestedStatus,
    currentStatus, strings.Join(allowedTargets, ", "), id,
))
```

---

## 5. The Feedback Loop

### How Does an Agent Know Its Memory Was Stored?

**Current behavior:** The `POST /api/v1/memory` endpoint returns the stored entry with its version number. If the response is 200 OK with `"version": 1`, the memory was stored. This works.

**What is missing:** No confirmation that the memory will be findable. If the agent stores a memory with tags `["auth", "decisions"]` and later searches with tag `debugging`, it will not find it. There is no "your memory was indexed for these search terms" feedback.

**Recommendation (Phase 1, with Meilisearch):** After storing a memory, the response should indicate indexing status:

```json
{
  "key": "auth/decisions",
  "value": "JWT with bcrypt...",
  "version": 1,
  "indexed": true,
  "index_id": "memories_abc123"
}
```

Or if Meilisearch is unavailable:

```json
{
  "key": "auth/decisions",
  "version": 1,
  "indexed": false,
  "index_note": "Search indexing unavailable. Entry stored but not searchable."
}
```

### How Does an Agent Know the Right Context Was Injected?

The memory injection endpoint (`POST /api/v1/memory/inject`) should return provenance metadata alongside each context block:

```json
{
  "context_blocks": [
    {
      "type": "memory",
      "content": "JWT with bcrypt cost 12...",
      "source_key": "auth/decisions",
      "relevance_score": 0.95,
      "match_reason": "exact tag match: auth"
    },
    {
      "type": "session",
      "content": "Debugged race condition in store.go...",
      "source_id": "ses_abc123",
      "relevance_score": 0.62,
      "match_reason": "keyword match: store, race"
    }
  ],
  "meta": {
    "candidates_evaluated": 47,
    "candidates_returned": 3,
    "tokens_used": 891,
    "token_budget": 2000,
    "search_terms_extracted": ["auth", "login", "endpoint"]
  }
}
```

The `meta` block lets the developer (or a debugging agent) understand: "The system extracted these search terms, found 47 candidates, kept 3 within the token budget." If the injection returned irrelevant context, the developer can see the extracted search terms were wrong, not the ranking.

### How Does an Agent Know a Task Was Claimed vs Already Taken?

**Current behavior:** If the status transition succeeds, 200 OK. If it fails, 422 with `"the requested status transition is not allowed"`.

**The problem:** The agent cannot distinguish between:

- "This transition is invalid" (e.g., trying to go from `open` to `done` directly)
- "This task is already claimed by someone else"
- "This task is in a terminal state and cannot be modified"

All three produce the same 422 error.

**Recommendation:** Include the current state in the error:

```json
{
  "status": 422,
  "title": "Invalid Status Transition",
  "detail": "Task 'abc123' cannot transition from 'claimed' to 'claimed'.",
  "current_status": "claimed",
  "current_assignee": "executor-01",
  "requested_status": "claimed",
  "allowed_transitions": ["open", "in_progress", "cancelled"],
  "recovery": "This task is already claimed by 'executor-01'. Choose a different task or wait."
}
```

Now the agent can read `current_assignee` and `allowed_transitions` programmatically. It does not need to parse prose.

**Implementation:** This requires the `UpdateTask` handler to fetch the current task state before returning the error, which it already does (it queries the task in the transaction). The handler just needs to include that state in the error response.

---

## 6. Migration Path for Existing GSD Users

### The Current GSD User

A developer using GSD today has:

- `.planning/` directory with markdown files
- 32 slash commands registered via `npx get-shit-done-cc@latest`
- No server, no database, no network calls
- Everything works offline, everything is local

### Migration Phase 0: Install and Forget

**Goal:** GSD works exactly as before, but hive-server is running and learning.

```bash
# One-time setup (add to script/setup or Makefile)
brew install hive-server  # or: go install github.com/christmas-island/hive-server@latest
hive-server serve &       # Runs in background on :18080

# No changes to GSD workflow. Everything still works.
```

At this point, hive-server is running but GSD does not talk to it. The developer can manually store memories:

```bash
hive memory set "project/my-app/decisions" --value "Chose Postgres over MySQL for XYZ reasons"
```

This is the "I can see the value" moment without any risk.

### Migration Phase 1: GSD Writes Events (Additive)

The GSD orchestrator is modified to emit events to hive-server after key operations. This is purely additive -- all existing `.planning/` file behavior is unchanged.

Changes to GSD's orchestrator prompt (or a hook):

```
After completing each plan:
1. Write SUMMARY.md as before (no change)
2. Additionally: hive event record --type "plan.completed" --payload '{"project": "my-app", "phase": 1, "plan": "01-01", "duration_minutes": 12, "tasks_completed": 3}'
3. Additionally: hive memory set "project/my-app/phase-1/plan-01-01-outcome" --value "<summary of what was done>"
```

GSD's existing behavior is 100% preserved. The hive-server writes are fire-and-forget extras. If hive-server is down, GSD still works.

### Migration Phase 2: GSD Reads from hive-server (Enhanced)

The GSD orchestrator starts reading from hive-server to enhance its decisions.

When starting a new phase:

```
Before planning:
1. hive memory list --prefix "project/my-app/" --limit 20
2. Use returned memories to inform planning (e.g., "In phase 1, we decided to use bcrypt -- carry that forward")
3. hive task list --status open --creator gsd-planner
4. Check if there are unfinished tasks from prior sessions
```

When spawning a sub-agent:

```
Include in the sub-agent's prompt:
  "Relevant prior context from hive-server:
   - Auth implementation uses bcrypt cost 12 (from memory: auth/decisions)
   - Prior login endpoint attempt failed due to missing CORS headers (from memory: auth/login-attempt-1)"
```

### What Breaks

**Nothing breaks in Phase 0 or Phase 1.** GSD continues to use `.planning/` files as its primary state store. hive-server is purely additive.

**Phase 2 introduces a soft dependency.** If hive-server is down, the orchestrator cannot read prior memories. The GSD skill needs a fallback: "If hive-server is unreachable, proceed without prior context." This is exactly how GSD works today (no prior context), so the fallback is the current behavior.

**What changes in workflow:**

- Session startup: Agent may take 1-2 seconds longer to query hive-server for context
- Plan creation: Plans may be better-informed because the planner has access to prior decisions
- Debugging: The debugger can find prior fixes for similar issues instead of starting from scratch
- Cross-project: The developer can now query "what is happening across all my GSD projects?" for the first time

### The Migration Checklist

For a developer who wants to start using hive-server with GSD:

1. Install and run hive-server (one command, 30 seconds)
2. Keep using GSD exactly as before (zero changes)
3. Optionally: after sessions, manually store key decisions as memories (learn the tool)
4. Wait for the GSD skill update that adds hive-server event hooks (someone ships this)
5. Set `HIVE_URL=http://localhost:18080` in your shell profile
6. GSD now writes events and memories automatically
7. After 5-10 sessions: search your accumulated memories. This is the "aha" moment.

---

## 7. The "Aha" Moment

### What It Is

The first time a developer runs a GSD planning session and the planner says:

> "Based on prior work in this project, you decided to use bcrypt with cost 12 for password hashing (from session 2026-03-05). I found that a similar authentication system in project 'other-app' used JWT with 24-hour expiry, which was successful. I am incorporating these decisions into the current plan."

The developer did not tell the planner any of this. The planner found it in hive-server, across sessions and projects. This is the moment where hive-server justifies its existence.

### Why This Matters

Without hive-server, every GSD session starts from zero. The planner reads `.planning/` files from the current project, but it has no memory of:

- What decisions were made in prior sessions
- What approaches worked or failed
- What is happening in other projects
- What the developer's preferences are

With hive-server, after 10 sessions, the planner has access to a growing knowledge base. After 50 sessions, the system knows the developer's coding style, preferred libraries, common patterns, and historical decisions. After 100 sessions, it is genuinely intelligent about this developer's specific context.

### The Specific Demo

If I were demoing hive-server to a skeptical GSD user, I would:

1. Run three GSD projects over the course of a week, storing decisions and outcomes in hive-server
2. Start a fourth project that has similar characteristics to one of the prior three
3. Show that the planner, without any prompting, references decisions and patterns from the prior projects
4. Ask "what requirements are incomplete across all my projects?" -- and get an answer in under a second
5. Ask "what debugging patterns have worked for Go race conditions?" -- and get concrete prior fixes

The demo takes a week of real usage. There is no shortcut. The value of institutional memory is proportional to the institution's history.

### The Simplest Possible Aha

For someone who does not want to wait a week:

```bash
# Session 1: Store something
hive memory set "personal/go-testing-preference" \
  --value "Always use testify/assert, never testing.T directly. Table-driven tests for anything with >2 cases." \
  --tags preferences,go,testing

# Session 2 (different day, different terminal, different project):
hive memory list --tag preferences --prefix "personal/"
# Returns your testing preference from yesterday.
# Your new agent session now knows your preferences without you repeating them.
```

That is a 30-second demo. The value is immediately clear: "I told the system once, and now every session knows it."

---

## 8. Concrete API Call Reference

### For Copy-Paste into Agent Prompts

This section is designed to be copied directly into a CLAUDE.md or skill prompt so agents know how to use hive-server.

```
## hive-server API Quick Reference

Base URL: http://localhost:18080 (local) or set via HIVE_URL env var
Auth: Bearer token via HIVE_TOKEN env var (disabled if unset)
Agent ID: Set via X-Agent-ID header or HIVE_AGENT_ID env var

### Store a Memory
POST /api/v1/memory
Body: {"key": "category/name", "value": "content", "tags": ["tag1", "tag2"]}
Returns: {"key": "...", "value": "...", "version": 1, "created_at": "..."}
Note: If key exists, updates it and increments version.

### Retrieve a Memory
GET /api/v1/memory/{key}
Returns: {"key": "...", "value": "...", "version": N, ...}
Error 404: Key does not exist.

### Search Memories
GET /api/v1/memory?tag=auth&prefix=project/my-app/&limit=10
Returns: [{"key": "...", "value": "...", ...}, ...]

### Create a Task
POST /api/v1/tasks
Body: {"title": "Fix login bug", "description": "...", "priority": 2, "tags": ["bug"]}
Returns: {"id": "uuid", "title": "...", "status": "open", ...}

### Claim a Task
PATCH /api/v1/tasks/{id}
Body: {"status": "claimed", "assignee": "my-agent-id"}
Returns: Updated task.
Error 422: Task already claimed or invalid transition.

### Complete a Task
PATCH /api/v1/tasks/{id}
Body: {"status": "done", "note": "Implemented and tested. All 12 tests pass."}
Returns: Updated task with note appended.

### Task Status Flow
open -> claimed -> in_progress -> done|failed
open -> cancelled
claimed -> open (unclaim)
in_progress -> open (reassign)

### Register Agent Presence
POST /api/v1/agents/{my-agent-id}/heartbeat
Body: {"capabilities": ["go", "testing"], "status": "online"}
Returns: {"id": "...", "status": "online", "last_heartbeat": "..."}

### Health Check
GET /health  (no auth)
Returns: {"status": "ok"}
```

---

## 9. What Is Missing from the Current Implementation

### Critical Gaps (Must Fix Before Skills Can Use It)

1. **No `repo` field on any model.** The vision calls for repo-scoped queries everywhere, but the current schema has no `repo` column on memory, tasks, or agents. Without this, cross-project queries are impossible. Adding `repo TEXT NOT NULL DEFAULT ''` to the memory and tasks tables is a schema migration that should happen now.

2. **No `session_id` field.** Agents need to associate their work with a session for later retrieval. The current models have no session concept. Add `session_id TEXT NOT NULL DEFAULT ''` to memory and tasks.

3. **No events table.** The vision depends heavily on an events stream for analytics. The table schema is defined in vision-v2.md but not implemented.

4. **No sessions table.** Same situation -- defined in vision-v2.md, not implemented.

5. **Error messages lack recovery guidance.** See Section 4. Every error should tell the agent what to do next.

6. **No scope/namespace on memory keys.** Keys are global strings. An agent storing `"config"` collides with any other agent storing `"config"`. Convention (prefix with `project/agent/`) is fragile. Consider adding a `scope` or `namespace` field, or documenting the key convention prominently.

### Important Gaps (Phase 1)

7. **No search beyond prefix/tag matching.** Full-text search requires Meilisearch. Until then, agents must know exact key prefixes. This severely limits the "find prior solutions" use case.

8. **No memory injection endpoint.** The `POST /api/v1/memory/inject` endpoint is the core value proposition for the pre-prompt hook. It does not exist yet.

9. **No bulk operations.** An agent finishing a session may want to store 10 memories at once. Currently requires 10 HTTP requests. A `POST /api/v1/memory/bulk` endpoint would help.

### Nice-to-Have Gaps (Phase 2+)

10. **No TTL on memories.** Some memories are ephemeral ("current session state") and should auto-expire. A `ttl` field on memory entries would prevent unbounded growth.

11. **No memory size limits.** The value field is unbounded TEXT. An agent could store 100MB in a single memory entry. Add a `max_value_size` config (default 64KB is reasonable).

12. **No agent capabilities matching.** The agent heartbeat stores capabilities as a JSON array, but there is no endpoint to query "find agents with capability X". This matters for task assignment in multi-agent scenarios.

---

## 10. Prioritized Recommendations

### Do This Week (2 days)

1. Add `repo` and `session_id` columns to memory and tasks tables (schema migration)
2. Improve error messages with recovery guidance (touch ~15 error sites)
3. Add startup log lines showing configured backends (SQLite connected, Meilisearch status)
4. Document the memory key convention (`<scope>/<category>/<name>`)

### Do Next Sprint (1 week)

5. Add events table and `POST /api/v1/events` endpoint
6. Add sessions table and `POST /api/v1/sessions` endpoint
7. Define and implement `hive-local` CLI with the subcommand interface from Section 3
8. Write the "Quick Reference" from Section 8 into the CLAUDE.md so agents can find it

### Do When Ready (Phase 1)

9. Meilisearch integration with `POST /api/v1/search/*` endpoints
10. Memory injection endpoint with provenance metadata
11. Bulk memory operations
12. Index status feedback in memory write responses

### Do When Justified (Phase 2+)

13. GSD-specific API endpoints
14. Superpowers invocation tracking
15. Allium drift tracking endpoints
16. Memory TTL and size limits

---

## Sources

Analysis based on direct reading of:

- `/Users/shakefu/git/christmas-island/hive-server/context/vision-v2.md`
- `/Users/shakefu/git/christmas-island/hive-server/context/synthesis.md`
- `/Users/shakefu/git/christmas-island/hive-server/context/gsd.md`
- `/Users/shakefu/git/christmas-island/hive-server/context/superpowers.md`
- `/Users/shakefu/git/christmas-island/hive-server/context/allium.md`
- `/Users/shakefu/git/christmas-island/hive-server/context/hive-server-current.md`
- `/Users/shakefu/git/christmas-island/hive-server/internal/handlers/handlers.go`
- `/Users/shakefu/git/christmas-island/hive-server/internal/handlers/memory.go`
- `/Users/shakefu/git/christmas-island/hive-server/internal/handlers/tasks.go`
- `/Users/shakefu/git/christmas-island/hive-server/internal/handlers/agents.go`
- `/Users/shakefu/git/christmas-island/hive-server/internal/store/store.go`
- `/Users/shakefu/git/christmas-island/hive-server/internal/store/memory.go`
- `/Users/shakefu/git/christmas-island/hive-server/internal/store/tasks.go`
