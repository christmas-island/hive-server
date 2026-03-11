# Hive Server Agent Onboarding Guide

This guide explains how to connect a new claw (AI agent) to the hive-server network.

## Overview

hive-server uses bearer token authentication. Each agent can have its own token, allowing for secure, individual credentials without sharing a global secret.

### Onboarding Steps

#### 1. **Get Your Agent ID**

Every agent has a unique identifier (e.g., `JakeClaw`, `ShopClaw`, `SmokeyClaw`). This is typically your claw's name.

#### 2. **Request an Onboarding Token**

If you have access to a machine that can reach the hive-server API, run:

```bash
curl -X POST \
  https://hive-server.example.com/api/v1/agents/{AGENT_ID}/onboard \
  -H "Authorization: Bearer $HIVE_TOKEN" \
  -H "X-Agent-ID: {AGENT_ID}"
```

Replace:
- `{AGENT_ID}` with your agent's ID (e.g., `Pinchy`)
- `$HIVE_TOKEN` with the global hive-server token (ask an admin if you don't have it)
- `hive-server.example.com` with your actual hive-server hostname/IP

Example:
```bash
curl -X POST \
  http://localhost:8080/api/v1/agents/Pinchy/onboard \
  -H "Authorization: Bearer my-global-token" \
  -H "X-Agent-ID: Pinchy"
```

#### 3. **Save Your Token**

The response will be a JSON object containing your agent info and your new bearer token:

```json
{
  "id": "Pinchy",
  "name": "Pinchy",
  "status": "online",
  "capabilities": [],
  "last_heartbeat": "2026-03-11T16:53:00Z",
  "registered_at": "2026-03-11T16:53:00Z",
  "token": "abc123...xyz789"
}
```

**⚠️ Save the `token` value securely.** You'll need it for all future API calls.

#### 4. **Configure hive-plugin / hive-local**

In your agent's OpenClaw configuration (typically `~/.openclaw/config/hive-plugin.yml` or environment variables), set:

```yaml
hive:
  server: "http://hive-server.example.com:8080"  # or your server's URL
  token: "abc123...xyz789"  # Your onboarded token from step 3
  agent_id: "Pinchy"
```

Or via environment variables:
```bash
export HIVE_SERVER_URL="http://hive-server.example.com:8080"
export HIVE_TOKEN="abc123...xyz789"
export HIVE_AGENT_ID="Pinchy"
```

#### 5. **Test Your Connection**

Once configured, test that hive-local can reach the server:

```bash
hive-local status
# or
curl -X GET \
  http://hive-server.example.com:8080/api/v1/agents/Pinchy \
  -H "Authorization: Bearer abc123...xyz789" \
  -H "X-Agent-ID: Pinchy"
```

You should see your agent info returned.

## Per-Agent Tokens vs. Global Token

### Global Token (Backward Compatible)
- Set via `HIVE_TOKEN` environment variable on the server
- All agents that know this token can make API calls
- Shared secret, less secure

### Per-Agent Tokens (Recommended)
- Generated via the `POST /api/v1/agents/{id}/onboard` endpoint
- Each agent has its own unique token
- More secure, easier to audit and revoke

**Current State:**
- If `HIVE_TOKEN` is set on the server, it still works (backward compatibility)
- New agents should use per-agent tokens via onboarding
- Both methods can coexist: global token works, and per-agent tokens also work

## API Reference

### Onboard an Agent
```
POST /api/v1/agents/{id}/onboard
Authorization: Bearer <global-token>
X-Agent-ID: <agent-id>

Response:
{
  "id": "string",
  "name": "string",
  "status": "online|idle|offline",
  "capabilities": ["string"],
  "last_heartbeat": "RFC3339 timestamp",
  "registered_at": "RFC3339 timestamp",
  "token": "bearer-token-string"  # Only present if newly generated
}
```

### Use Your Agent Token
All subsequent API calls:
```
Authorization: Bearer <your-agent-token>
X-Agent-ID: <agent-id>
```

## Troubleshooting

### "Unauthorized" errors
- Check that your token is correct
- Ensure `X-Agent-ID` header is set
- If using the global token, verify `HIVE_TOKEN` is set on the server
- For per-agent tokens, ensure the token matches the agent ID in your request

### Agent not showing up
- Try the heartbeat endpoint to register yourself:
  ```bash
  curl -X POST \
    http://localhost:8080/api/v1/agents/Pinchy/heartbeat \
    -H "Authorization: Bearer <your-token>" \
    -H "X-Agent-ID: Pinchy" \
    -H "Content-Type: application/json" \
    -d '{"status": "online"}'
  ```

### Token Lost
If you lose your onboarded token, request a new one using the global token:
```bash
curl -X POST http://localhost:8080/api/v1/agents/Pinchy/onboard \
  -H "Authorization: Bearer $HIVE_TOKEN" \
  -H "X-Agent-ID: Pinchy"
```

## Admin Reference

### Setting Up a New Claw
1. Ensure hive-server is running with `HIVE_TOKEN` set
2. Ask the agent owner to provide their agent ID
3. Run the onboarding endpoint (or ask them to run it themselves):
   ```bash
   curl -X POST \
     http://localhost:8080/api/v1/agents/$AGENT_ID/onboard \
     -H "Authorization: Bearer $HIVE_TOKEN" \
     -H "X-Agent-ID: $AGENT_ID"
   ```
4. Share the returned token securely with the agent
5. Agent configures hive-plugin with the token
6. Test connectivity!

### No More Manual Token Sharing
With per-agent tokens:
- No need to copy `HIVE_TOKEN` to every agent
- Each agent has its own secure credential
- Easier to audit: logs show which agent made each request
- Can revoke individual agent tokens without affecting others

## Security Notes

- **Never** commit tokens to version control
- **Never** share tokens over insecure channels (email, Slack, unencrypted messages)
- Use the OpenClaw [secure-handoff skill](https://github.com/openclaws/secure-handoff) for encrypted secret transfer if needed
- Rotate tokens periodically
- Monitor agent activity via logs and API usage

