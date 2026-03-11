# LSP Plugin Lifecycle Research Report

**Date:** 2026-03-10
**Context:** Research for hive-server issue #21 - LSP plugin: stateful server lifecycle & reconciliation
**Status:** Research findings and architecture comparison

## Executive Summary

This report analyzes implementing Language Server Protocol (LSP) capabilities in hive-server for automated lifecycle management of language-specific development tools (rust-analyzer, gopls, pyright, etc.). The core insight is that LSP provides a mature, standardized protocol for stateful tool lifecycle that could complement MCP's agent-oriented capabilities, but with significant implementation complexity and architectural overlap concerns.

## LSP Fundamentals & Lifecycle

### Standard LSP Lifecycle Sequence

LSP defines a strict initialization and shutdown sequence:

1. **Spawn Process**: Client starts LSP server process (per-workspace)
2. **Initialize Request**: Client sends `initialize` with capabilities and workspace info
3. **Initialize Response**: Server responds with its capabilities
4. **Initialized Notification**: Client confirms initialization complete
5. **Normal Operation**: Request/response cycles for language features
6. **Shutdown Request**: Client requests graceful shutdown
7. **Exit Notification**: Client sends exit, server terminates

### Key LSP Characteristics

- **Client-managed lifecycle**: Clients (editors) are responsible for spawning, monitoring, and terminating servers
- **Process-per-workspace**: Typically one server instance per language per workspace root
- **Stateful operation**: Servers maintain file indexes, parse trees, project metadata
- **JSON-RPC transport**: Bidirectional communication over stdin/stdout or TCP
- **Capability negotiation**: Client and server declare supported features upfront

## Current State of LSP in Agent/AI Tooling

### Existing Integrations

**Direct LSP Usage:**

- **lsp-mcp bridge**: Bridges LSP servers to MCP clients, exposing language features as MCP tools
- **AI editor plugins**: Cursor, Codium, Tabnine integrate with existing LSP infrastructure
- **Language-aware agents**: Some coding agents spawn LSP servers directly for code intelligence

**Patterns & Problems:**

- **Manual lifecycle management**: Most agents spawn/kill servers ad-hoc
- **Workspace confusion**: Multiple agents competing for same LSP server processes
- **Resource waste**: Agents don't share LSP instances, leading to duplication
- **State loss**: Server restarts lose accumulated project knowledge

### Go LSP Ecosystem Maturity

**Production-Ready Libraries:**

- **`go.lsp.dev/protocol`**: Official Go LSP protocol implementation, battle-tested
- **`TobiasYin/go-lsp`**: Simpler server-side framework with lifecycle helpers
- **`golang.org/x/tools/gopls`**: Reference implementation, handles complex workspaces

**Maturity Assessment:** ✅ **MATURE**

- Stable protocol implementations available
- Good documentation and examples
- Active maintenance and community support
- Production usage in major editors (VS Code, Vim/Neovim, Emacs)

## LSP vs Current MCP Approach

| Dimension                | MCP Tools (Current)            | LSP Plugin (Proposed)            |
| ------------------------ | ------------------------------ | -------------------------------- |
| **Lifecycle**            | Stateless function calls       | Stateful server processes        |
| **Agent Coupling**       | Agent spawns tools per request | Shared server pool across agents |
| **Language Support**     | Generic tool interfaces        | Language-specific intelligence   |
| **Resource Usage**       | Low (ephemeral)                | Higher (persistent processes)    |
| **Development Velocity** | Fast (simple tool calls)       | Slower (protocol complexity)     |
| **Code Intelligence**    | Basic file operations          | Rich semantic analysis           |
| **Cross-agent Sharing**  | No state to share              | Shared project understanding     |

### What LSP Solves That MCP Doesn't

1. **Semantic Understanding**: Go to definition, find references, type information
2. **Incremental Updates**: File changes update indexes without full reparse
3. **Project-wide Analysis**: Cross-file refactoring, workspace-wide search
4. **Language Expertise**: Leverages language-specific tooling (rust-analyzer, gopls)
5. **Caching & Performance**: Persistent state avoids repeated expensive analysis

### What MCP Does Better

1. **Simplicity**: Function-style tools vs. complex protocol state machines
2. **Flexibility**: Not constrained to language semantics, can do any operation
3. **Debugging**: Easier to understand and debug stateless interactions
4. **Resource Efficiency**: No persistent processes consuming memory
5. **Agent Independence**: No shared state between agent sessions

## Proposed Architecture: Stateful LSP Manager

Based on issue #21 requirements and existing patterns:

### Core Components

**1. LSP Server Pool (SQLite State Tracking)**

```sql
CREATE TABLE lsp_servers (
    id              TEXT PRIMARY KEY,  -- "workspace:/path/to/project:language:rust"
    workspace_path  TEXT NOT NULL,
    language        TEXT NOT NULL,     -- rust, go, python, typescript
    server_binary   TEXT NOT NULL,     -- rust-analyzer, gopls, pyright
    pid             INTEGER,           -- Process ID (NULL if not running)
    status          TEXT NOT NULL,     -- spawning, initializing, ready, idle, shutting_down, dead
    last_accessed   INTEGER NOT NULL,  -- Unix timestamp
    capabilities    TEXT,              -- JSON blob of server capabilities
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);

CREATE INDEX idx_lsp_servers_workspace ON lsp_servers(workspace_path);
CREATE INDEX idx_lsp_servers_status ON lsp_servers(status);
CREATE INDEX idx_lsp_servers_last_accessed ON lsp_servers(last_accessed);
```

**2. Plugin Interface**

```bash
# Agent query format (matches issue #21 spec)
hive cmd=lsp json='{
  "workspace": "./foo",
  "lang": "rust",
  "method": "textDocument/hover",
  "params": { "textDocument": { "uri": "file:///path/to/main.rs" }, "position": { "line": 10, "character": 5 } }
}'
```

**3. Reconciliation Logic**

- **On any hive call**: Check for idle servers (>55min since last_accessed), mark for shutdown
- **Dead PID detection**: Verify process still running, mark dead PIDs for respawn
- **Auto-spawn logic**: Detect language from workspace (go.mod, Cargo.toml, package.json)
- **Graceful shutdown**: Send shutdown→exit sequence, wait for process termination

### Language Detection Strategy

**File-based heuristics:**

```go
func detectLanguage(workspace string) string {
    if fileExists(path.Join(workspace, "Cargo.toml")) { return "rust" }
    if fileExists(path.Join(workspace, "go.mod")) { return "go" }
    if fileExists(path.Join(workspace, "package.json")) { return "typescript" }
    if fileExists(path.Join(workspace, "pyproject.toml")) { return "python" }
    // ... more patterns
    return "unknown"
}
```

**Binary mapping:**

```go
var serverBinaries = map[string]string{
    "rust": "rust-analyzer",
    "go": "gopls",
    "python": "pyright",
    "typescript": "typescript-language-server",
}
```

## Benefits & Risks Analysis

### Benefits ✅

1. **Semantic Intelligence**: Agents get rich code understanding without reimplementation
2. **Resource Efficiency**: Shared servers across agents avoid duplicate analysis
3. **Battle-tested Infrastructure**: LSP is mature, well-specified, widely adopted
4. **Language Ecosystem**: Inherits all existing LSP server capabilities
5. **Developer Familiarity**: LSP is known quantity for IDE developers

### Risks & Concerns ⚠️

1. **Architecture Complexity**:

   - Adds stateful server management to otherwise stateless hive
   - Process monitoring, crash recovery, port management
   - Complex shutdown semantics and resource cleanup

2. **Resource Management**:

   - Memory usage: Each server consumes 50-200MB+ per workspace
   - CPU overhead: Continuous background indexing and analysis
   - File handle limits: Large projects can exhaust system limits

3. **State Reconciliation Challenges**:

   - Client reconnection after network/process interruption
   - File synchronization: LSP servers have their own view of file state
   - Version conflicts: Multiple agents editing same files simultaneously

4. **Operational Complexity**:

   - Language server binary distribution and versioning
   - Configuration management per language/workspace
   - Debugging multi-process system interactions

5. **Scope Creep Risk**:
   - LSP capabilities could overlap with existing hive tools
   - May encourage reimplementing editor functionality in agent context
   - Potential for "feature bloat" as more LSP features are exposed

### Alternative: LSP-MCP Bridge

Instead of native LSP support in hive, consider a **separate bridge service**:

- Runs alongside hive as independent process
- Manages LSP lifecycle independently
- Exposes LSP features as MCP tools to hive
- Maintains clean architectural separation

This approach provides LSP benefits while containing complexity outside core hive.

## Implementation Complexity Assessment

**Low Complexity:**

- Basic process spawning and JSON-RPC communication
- Simple language detection heuristics
- SQLite state tracking

**Medium Complexity:**

- Graceful shutdown and process monitoring
- File synchronization between agents and LSP servers
- Error handling and recovery logic

**High Complexity:**

- Multi-client LSP session management (multiple agents per server)
- Workspace-scoped server pooling with proper isolation
- Integration with existing hive directive/tool system

## Recommendation

**Proceed with caution.** LSP integration offers genuine value for code-heavy agent workloads, but introduces significant architectural complexity.

**Suggested approach:**

1. **MVP Phase**: Start with read-only LSP queries (hover, definition, references) for single-agent scenarios
2. **Bridge Pattern**: Implement as separate LSP-MCP bridge service rather than native hive integration
3. **Evaluate Impact**: Measure actual usage patterns and performance before expanding scope

**Blocker Dependencies:**

- Issue #22 (stateful store research) must be resolved first
- Need clear requirements for multi-agent LSP session handling
- Resource limit policies for server memory/CPU consumption

This research provides foundation for implementation decisions, but architectural alignment with hive-server v5 behavioral knowledge engine requires additional design work.
