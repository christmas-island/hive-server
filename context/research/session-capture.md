# Session Capture Research Report

**Date:** 2026-03-10
**Context:** Research for hive-server issue #35 - Native session capture: ship agent context directly to hive
**Status:** Research findings and implementation architecture

## Executive Summary

This report analyzes implementing native session capture for OpenClaw/ACP agents, directly shipping session data to hive-server as first-class memory. The approach builds on battle-tested session replay patterns from entire.io and web analytics, but adapts them for AI agent workflows. This capability would provide the "write side" of hive's behavioral knowledge engine, creating a feedback loop from agent experience back to directive generation.

## Session Capture Landscape Analysis

### entire.io Architecture & Lessons

**How entire.io Works:**

1. **Git hook integration**: Captures agent sessions on `git commit`
2. **JSONL parsing**: Reads Claude Code CLI session logs (HAIL format)
3. **Branch storage**: Ships to `entire/checkpoints/v1` git branch
4. **AI summarization**: Generates intent/outcome summaries at commit time
5. **Searchable index**: Makes sessions queryable by repo, file, timestamp

**Key Insights from entire.io:**

- **Git-centric workflow**: Natural integration point for code-focused agents
- **Separate branch strategy**: Keeps code history clean while preserving context
- **JSONL as interchange format**: Battle-tested for agent session data
- **Checkpoint concept**: Sessions tied to specific code state (commits)
- **AI-generated summaries**: Raw transcripts too verbose, need synthesis

**entire.io Data Fields (HAIL Format):**

```json
{"v":"hail/0.1","tool":"claude-code","model":"opus-4","ts":"2026-03-10T00:00:00Z"}
{"role":"human","content":"Fix the auth bug","files":["src/auth.go"]}
{"role":"agent","content":"I'll analyze the auth code...","tools":[{"name":"read","args":{"file":"src/auth.go"}}]}
{"role":"tool","content":"package auth\n\n// Current implementation..."}
{"role":"agent","content":"Found the issue - missing null check...","edits":[{"file":"src/auth.go","changes":[...]}]}
```

### Session Replay Tools (Web Analytics)

**Common Patterns from FullStory, LogRocket, PostHog:**

- **Automatic capture**: No manual instrumentation required
- **Event streams**: User actions as time-ordered events
- **Privacy controls**: Configurable data masking and retention
- **Contextual metadata**: User ID, session ID, environment info
- **Search & filtering**: Query by user properties, actions, timeframe
- **Replay reconstruction**: Recreate user experience from event stream

**Privacy & Data Minimization Principles:**

- **Opt-in collection**: Users must consent to session recording
- **Sensitive data masking**: Automatically redact PII, passwords, secrets
- **Retention policies**: Automatic data expiration (30-90 days typical)
- **Access controls**: Audit who views session data
- **Anonymization**: Strip identifying information for analytics

## OpenClaw/ACP Session Context

### Current Session Structure

**OpenClaw Session Components:**

- **Session metadata**: Agent ID, channel, start/end timestamps, model used
- **Conversation transcript**: Full prompt/response history with tool calls
- **Tool execution logs**: Commands run, files modified, outputs captured
- **Token usage tracking**: Input/output/cache tokens per turn
- **Sub-agent hierarchy**: Nested sessions spawned by main agent
- **File change tracking**: Git-style diffs of modifications made

**ACP Harness Context:**

- **Agent identity**: Which claw, which channel, session scope
- **Runtime state**: Working directory, environment variables
- **Error handling**: Failed tool calls, retry attempts, timeouts

### Session Data Model for AI Agents

**Proposed Schema (extends hive-server directive model):**

```sql
CREATE TABLE agent_sessions (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL,           -- Multi-tenancy support

    -- Session identity
    agent_id        TEXT        NOT NULL,           -- jakeclaw, pinchyclaw, etc.
    session_type    TEXT        NOT NULL,           -- main, subagent, cron
    parent_session  UUID        NULL,               -- For sub-agent hierarchy
    channel         TEXT        NOT NULL,           -- discord, telegram, local

    -- Temporal bounds
    started_at      TIMESTAMPTZ NOT NULL,
    ended_at        TIMESTAMPTZ NULL,               -- NULL = still active
    duration_ms     BIGINT      NULL,               -- Calculated on end

    -- Runtime context
    working_dir     TEXT        NOT NULL,           -- Agent workspace
    model           TEXT        NOT NULL,           -- claude-sonnet-4, opus-4
    runtime_version TEXT        NOT NULL,           -- openclaw version

    -- Outcome tracking
    task_completed  BOOLEAN     DEFAULT false,
    exit_reason     TEXT        NULL,               -- timeout, error, completion, manual

    -- Repository context (when applicable)
    repo_url        TEXT        NULL,               -- GitHub repo URL
    repo_branch     TEXT        NULL,               -- Current branch
    commit_before   TEXT        NULL,               -- Git SHA before session
    commit_after    TEXT        NULL,               -- Git SHA after session

    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE session_turns (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID        NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    turn_index      INTEGER     NOT NULL,           -- 0, 1, 2... within session

    -- Turn content
    role            TEXT        NOT NULL,           -- human, agent, tool, system
    content         TEXT        NOT NULL,           -- Message content
    content_type    TEXT        DEFAULT 'text',     -- text, image, file

    -- Tool execution (when role=tool)
    tool_name       TEXT        NULL,               -- exec, read, write, web_search
    tool_args       JSONB       NULL,               -- Tool parameters as JSON
    tool_output     TEXT        NULL,               -- Tool execution result
    tool_success    BOOLEAN     NULL,               -- Success/failure flag

    -- Token tracking
    input_tokens    INTEGER     NULL,
    output_tokens   INTEGER     NULL,
    cache_tokens    INTEGER     NULL,

    -- Timing
    started_at      TIMESTAMPTZ NOT NULL,
    completed_at    TIMESTAMPTZ NULL,
    duration_ms     BIGINT      NULL,

    created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE session_files (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID        NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,

    -- File identification
    file_path       TEXT        NOT NULL,           -- Relative to working_dir
    operation       TEXT        NOT NULL,           -- read, write, edit, delete
    turn_id         UUID        NOT NULL REFERENCES session_turns(id),

    -- Change tracking
    content_before  TEXT        NULL,               -- File content before change
    content_after   TEXT        NULL,               -- File content after change
    diff_unified    TEXT        NULL,               -- Unified diff format

    -- Metadata
    file_size       BIGINT      NULL,               -- File size in bytes
    mime_type       TEXT        NULL,               -- text/plain, application/json

    created_at      TIMESTAMPTZ DEFAULT NOW()
);
```

## Integration with OpenClaw Session Model

### Session Hook Points

**1. Session Start Hook**

```javascript
// ACP harness session initialization
function onSessionStart(sessionMetadata) {
  const sessionData = {
    agent_id: sessionMetadata.agentId,
    session_type: sessionMetadata.isSubagent ? "subagent" : "main",
    parent_session: sessionMetadata.parentSessionId,
    channel: sessionMetadata.channel,
    working_dir: process.cwd(),
    model: sessionMetadata.model,
    runtime_version: getOpenClawVersion(),
  };

  hiveClient.createSession(sessionData);
}
```

**2. Turn Completion Hook**

```javascript
// After each agent response or tool call
function onTurnComplete(sessionId, turn) {
  const turnData = {
    session_id: sessionId,
    turn_index: turn.index,
    role: turn.role,
    content: turn.content,
    tool_name: turn.tool?.name,
    tool_args: turn.tool?.args,
    tool_output: turn.tool?.output,
    input_tokens: turn.tokens?.input,
    output_tokens: turn.tokens?.output,
    started_at: turn.startTime,
    completed_at: turn.endTime,
  };

  hiveClient.addTurn(turnData);
}
```

**3. File Change Hook**

```javascript
// After file modifications
function onFileChange(sessionId, turnId, fileChange) {
  const fileData = {
    session_id: sessionId,
    turn_id: turnId,
    file_path: fileChange.path,
    operation: fileChange.operation,
    content_before: fileChange.before,
    content_after: fileChange.after,
    diff_unified: generateDiff(fileChange.before, fileChange.after),
  };

  hiveClient.recordFileChange(fileData);
}
```

**4. Session End Hook**

```javascript
// On session termination
function onSessionEnd(sessionId, exitReason) {
  const endData = {
    ended_at: new Date(),
    exit_reason: exitReason,
    task_completed: determineTaskCompletion(),
    commit_after: getCurrentGitCommit(),
  };

  hiveClient.endSession(sessionId, endData);

  // Trigger directive extraction from session
  scheduleSessionAnalysis(sessionId);
}
```

### Git Integration Points

**Option 1: Git Hook Trigger (entire.io pattern)**

- Capture sessions on `git commit` like entire.io
- Sessions tied to specific code states
- Natural integration with development workflow
- May miss sessions that don't result in commits

**Option 2: Session End Trigger (recommended)**

- Capture on agent session termination
- More complete coverage of agent activity
- Independent of git workflow
- Can correlate with commits post-hoc

## Privacy & Data Minimization

### Sensitive Data Concerns

**High-Risk Data Types:**

- **API keys & secrets**: Database passwords, OAuth tokens, private keys
- **Personal information**: Email addresses, phone numbers, real names
- **Business secrets**: Proprietary algorithms, customer data, financial info
- **System internals**: Internal URLs, server hostnames, network topology

**Mitigation Strategies:**

**1. Pre-flight Sanitization**

```go
func sanitizeContent(content string) string {
    // Redact common secret patterns
    content = redactAPIKeys(content)       // Remove API_KEY=xyz patterns
    content = redactPasswords(content)     // Remove password fields
    content = redactTokens(content)        // Remove JWT tokens, OAuth tokens
    content = redactEmails(content)        // Mask email addresses
    content = redactIPs(content)           // Mask IP addresses
    return content
}
```

**2. Configurable Collection Levels**

```yaml
# hive session capture config
session_capture:
  enabled: true
  level: "minimal" # minimal, standard, verbose
  retention_days: 30

  collection:
    tool_outputs: true
    file_contents: false # Skip file content capture
    environment_vars: false # Skip env var capture
    network_requests: false # Skip network request bodies

  sanitization:
    redact_secrets: true
    redact_personal_info: true
    custom_patterns:
      - "INTERNAL_.*" # Custom regex patterns
      - "SECRET_.*"
```

**3. Data Retention Policies**

```sql
-- Automatic session cleanup
CREATE OR REPLACE FUNCTION cleanup_old_sessions() RETURNS void AS $$
BEGIN
    DELETE FROM agent_sessions
    WHERE created_at < NOW() - INTERVAL '30 days';
END;
$$ LANGUAGE plpgsql;

-- Scheduled cleanup job
SELECT cron.schedule('session-cleanup', '0 2 * * *', 'SELECT cleanup_old_sessions();');
```

## Token Cost Analysis

### Cost Considerations

**Raw Session Data Volume:**

- **Typical session**: 20-50 turns, 500-2000 tokens per turn
- **Large session**: 100+ turns, 5000+ tokens per turn
- **Sub-agent overhead**: Nested sessions multiply token costs

**Shipping Strategies:**

**1. Full Transcript Shipping**

- **Pros**: Complete context preservation, exact replay capability
- **Cons**: High token costs (10K-50K+ tokens per session)
- **Cost**: ~$0.15-$2.50 per session (Claude Sonnet rates)

**2. Summarized Shipping (Recommended)**

```javascript
// Generate session summary before shipping
function summarizeSession(session) {
  const summary = {
    intent: extractIntent(session.turns),
    outcome: determineOutcome(session.turns),
    key_decisions: extractDecisions(session.turns),
    friction_points: identifyFriction(session.turns),
    files_touched: session.fileChanges.map((f) => f.path),
    tools_used: unique(session.turns.map((t) => t.tool_name)),
    token_usage: summarizeTokens(session.turns),
  };
  return summary; // ~200-500 tokens vs 10K+ raw
}
```

- **Pros**: 95% cost reduction while preserving key insights
- **Cons**: Loss of fine-grained detail, potential information loss
- **Cost**: ~$0.01-$0.08 per session

**3. Hybrid Approach**

- Ship summaries by default
- Store full transcripts locally with configurable retention
- Upload full session on-demand for debugging/analysis
- Best of both worlds: cost efficiency + detail when needed

## Storage & Retrieval Patterns

### Query Patterns

**Common Session Queries:**

```sql
-- Recent sessions by agent
SELECT * FROM agent_sessions
WHERE agent_id = 'jakeclaw' AND started_at > NOW() - INTERVAL '7 days';

-- Sessions touching specific files
SELECT DISTINCT s.* FROM agent_sessions s
JOIN session_files sf ON s.id = sf.session_id
WHERE sf.file_path LIKE '%auth.go%';

-- Failed sessions for debugging
SELECT * FROM agent_sessions
WHERE exit_reason IN ('error', 'timeout') AND started_at > NOW() - INTERVAL '1 day';

-- Token usage aggregation
SELECT agent_id, DATE(started_at),
       SUM(st.input_tokens) as total_input,
       SUM(st.output_tokens) as total_output
FROM agent_sessions s
JOIN session_turns st ON s.id = st.session_id
GROUP BY agent_id, DATE(started_at);

-- Tool usage patterns
SELECT tool_name, COUNT(*), AVG(duration_ms)
FROM session_turns
WHERE tool_name IS NOT NULL
GROUP BY tool_name;
```

### Integration with Hive Directive System

**Session → Directive Extraction Pipeline:**

1. **Session completion** triggers analysis job
2. **Pattern detection** identifies recurring problems/solutions
3. **Directive generation** creates behavioral nudges from experience
4. **Effectiveness tracking** links directives back to session outcomes

**Example Directive Extraction:**

```javascript
// Analyze completed session for directive opportunities
function extractDirectives(sessionId) {
  const session = getSessionWithTurns(sessionId);

  // Pattern: Agent repeatedly reading same file
  if (countFileReads(session) > 5) {
    generateDirective({
      content: `In ${session.repo_url}, consider caching file contents instead of repeated reads`,
      kind: "pattern",
      source_type: "experience",
      source_id: sessionId,
      context: { file_path: getMostReadFile(session) },
    });
  }

  // Pattern: Successful debugging approach
  if (session.task_completed && containsDebuggingSession(session)) {
    const approach = extractDebuggingApproach(session);
    generateDirective({
      content: `When debugging similar issues, try: ${approach}`,
      kind: "behavioral",
      source_type: "experience",
      source_id: sessionId,
    });
  }
}
```

## Implementation Roadmap

### Phase 1: Basic Session Capture

- Implement session hooks in ACP harness
- Basic schema with sessions, turns, files
- Local storage before hive integration
- Privacy sanitization for secrets/PII

### Phase 2: Hive Integration

- `POST /sessions` endpoint in hive-server
- Session data shipping on completion
- Query interface for session retrieval
- Token usage aggregation

### Phase 3: Directive Extraction

- Session analysis pipeline
- Pattern recognition for directive generation
- Integration with existing directive system
- Effectiveness feedback loop

### Phase 4: Advanced Features

- Real-time session streaming (optional)
- Cross-agent session correlation
- Performance analytics dashboard
- Advanced privacy controls

## Recommendation

**Proceed with implementation.** Session capture provides critical missing piece of hive's behavioral knowledge engine - the experiential data needed to generate effective directives.

**Key Success Factors:**

1. **Privacy-first design**: Extensive sanitization, configurable collection levels
2. **Cost optimization**: Summary-based shipping with full transcript fallback
3. **Incremental rollout**: Start with basic capture, add analysis capabilities over time
4. **Integration focus**: Sessions as input to directive generation, not standalone feature

This capability transforms hive from a static knowledge store into a learning system that improves based on actual agent experience.
