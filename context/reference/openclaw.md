# OpenClaw Technology Brief

## What Is OpenClaw

OpenClaw is a **free, open-source, autonomous AI agent platform** (TypeScript/Node.js, requires v22+). Formerly known as Clawdbot/Moltbot/Molty, rebranded in 2024. MIT License. **287k+ GitHub stars** as of March 2026.

Runs a single always-on **Gateway** process that connects to 15+ messaging platforms (WhatsApp, Telegram, Slack, Discord, Signal, iMessage, Teams, Matrix, IRC, etc.) and routes messages through LLM-powered agents.

---

## Core Architecture

### Gateway

- Single source of truth for sessions, routing, channel connections
- Single port: HTTP + WebSocket simultaneously
- WebSocket protocol v3 for real-time bidirectional communication
- HTTP endpoints for Control UI, webhooks, health checks, REST
- 50+ RPC methods across config, sessions, agents, channels, cron, tools
- Optional OpenAI-compatible Chat Completions API

### Agent Runtime

Each agent is fully isolated with:

- **Workspace**: personality/instruction files (`SOUL.md`, `AGENTS.md`, `USER.md`)
- **State directory**: `~/.openclaw/agents/<agentId>/agent`
- **Session store**: `~/.openclaw/agents/<agentId>/sessions`
- Independent tool policies, sandbox config, model configuration

### Channel Architecture

Each messaging platform is a separate adapter plugin normalizing to common internal format.

### Memory System (built-in)

- **Vector search** via SQLite with embedding extensions
- **FTS5** for exact keyword matches
- Configurable embedding providers (OpenAI, local models, Gemini, Voyage)

---

## Multi-Agent Orchestration

### Agent Types

1. **Persistent Agents**: Long-lived, bound to channels/chats, run continuously
2. **Sub-agents**: Background workers spawned from conversations, post results back to parent

### Agent Routing

Deterministic, specificity-based hierarchy through "bindings":

1. Peer ID (exact DM/group/channel)
2. Parent peer (thread inheritance)
3. Guild ID + roles (Discord)
4. Guild ID alone
5. Team ID (Slack)
6. Account ID match
7. Channel-level match
8. Fallback to default agent

### Per-Agent Isolation

- Independent tool allow/deny lists
- Independent model configuration (fast vs strong)
- Sandbox mode (container-per-agent)
- Separate authentication credentials per agent

---

## Skills & Tools System

- **Tools** = capabilities (file ops, shell commands, web browsing, API calls)
- **Skills** = markdown playbooks (`SKILL.md`) teaching agents how to use tools

Skill loading precedence:

1. `<workspace>/skills/` (highest)
2. `~/.openclaw/skills/`
3. Bundled skills (lowest)

**ClawHub** = official skill directory (10,700+ skills, but 820+ malicious ones identified)

---

## MCP Support

Native MCP server support via `@modelcontextprotocol/sdk@1.25.3`. Config in `openclaw.json`:

```json
{
  "mcp": {
    "servers": {
      "my-server": {
        "command": "node",
        "args": ["path/to/server.js"]
      }
    }
  }
}
```

---

## Webhook & External API

- `POST /hooks/wake` — enqueue system event
- `POST /hooks/agent` — run isolated agent turn (message, agentId, sessionKey, model, timeout)
- `POST /hooks/<name>` — custom mapped webhooks with JS/TS payload transforms
- Auth: `Authorization: Bearer <token>` or `x-openclaw-token: <token>` header

---

## LLM Provider Support

Built primarily for **Anthropic Claude** but also supports OpenAI, Google Gemini, DeepSeek, and any OpenAI-compatible endpoint (Ollama, vLLM, LM Studio).

---

## Integration Patterns with hive-server

### Pattern A: hive-server as MCP Server (most idiomatic)

Wrap hive-server REST API as MCP tools. OpenClaw agents call hive endpoints natively.

### Pattern B: Webhook-Driven Orchestration

hive-server sends webhook POSTs to OpenClaw `/hooks/agent` to trigger agent actions. Agents call back to hive-server REST API.

### Pattern C: OpenClaw as Sidecar

Run Gateway alongside hive-server in same k8s pod. hive-server manages persistent state, OpenClaw handles agent reasoning.

### Pattern D: OpenAI-Compatible API Bridge

hive-server calls OpenClaw's OpenAI-compatible endpoint for LLM completions.

### Key Considerations

- OpenClaw is TS/Node.js; hive-server is Go — integration over HTTP
- OpenClaw's memory system overlaps with hive-server's storage — need clear ownership boundaries
- OpenClaw's skill system can reference hive-server API endpoints
- Gateway is single-machine design (no HA/clustering documented)

---

## Security Considerations

- **CVE-2026-25253**: Critical RCE via WebSocket hijacking
- **CVE-2026-25157**: Critical vulnerability affecting exposed instances
- **42,000+ exposed instances** found, 63% vulnerable
- **CrowdStrike** published detection guidance
- Never expose Gateway to public internet without hardening
- Use dedicated hook tokens separate from gateway auth
