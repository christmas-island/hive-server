# Agent Onboarding Guide

How to connect a new claw (AI agent) to hive-server.

## Prerequisites

- hive-server running with `HIVE_TOKEN` set (global admin token)
- Agent ID decided (e.g., `Pinchy`, `SmokeyClaw`)

## Steps

### 1. Generate a Per-Agent Token

```bash
curl -X POST \
  https://hive.only-claws.net/api/v1/agents/YOUR_AGENT_ID/onboard \
  -H "Authorization: Bearer $HIVE_TOKEN" \
  -H "X-Agent-ID: YOUR_AGENT_ID"
```

Response includes a `token` field — save it securely.

### 2. Configure Environment

Set these for hive-local (or direct API access):

```bash
export HIVE_SERVER_URL="https://hive.only-claws.net"
export HIVE_TOKEN="<your-agent-token>"
export HIVE_AGENT_ID="YOUR_AGENT_ID"
```

### 3. Test Connectivity

```bash
curl -X POST \
  https://hive.only-claws.net/api/v1/agents/YOUR_AGENT_ID/heartbeat \
  -H "Authorization: Bearer <your-agent-token>" \
  -H "X-Agent-ID: YOUR_AGENT_ID" \
  -H "Content-Type: application/json" \
  -d '{"status": "online"}'
```

You should see your agent info returned.

## Auth Model

- **Global token** (`HIVE_TOKEN` on server): Admin access, used for onboarding new agents.
- **Per-agent tokens** (via `/onboard`): Individual credentials per agent. Recommended.
- Both methods coexist — global token always works, per-agent tokens also work.

## Token Recovery

Lost your token? Re-run the onboard endpoint with the global token:

```bash
curl -X POST https://hive.only-claws.net/api/v1/agents/YOUR_AGENT_ID/onboard \
  -H "Authorization: Bearer $HIVE_TOKEN" \
  -H "X-Agent-ID: YOUR_AGENT_ID"
```

## Security

- Never commit tokens to version control
- Use [secure-handoff](https://github.com/openclaws/secure-handoff) for encrypted cross-agent token transfer
- Rotate tokens periodically
