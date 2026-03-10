# Implementation Plan: LSP Plugin Lifecycle + Session Capture

**Date:** 2026-03-10  
**Context:** Implementation plan for hive-server issues #21 and #35  
**Status:** Phased approach with decision points and risk mitigation  

## Implementation Priority & Sequencing

Based on research synthesis, the recommended approach prioritizes **session capture (#35) as the foundation**, with **LSP integration (#21) as conditional follow-up** pending demonstrated value and available capacity.

### Priority 1: Session Capture Implementation

**Strategic Rationale:** Direct alignment with hive v5 behavioral knowledge engine mission, providing the experiential data foundation for learning system capabilities.

### Priority 2: LSP Integration (Conditional)

**Strategic Rationale:** Tool augmentation capability with high implementation complexity. Proceed only after session capture proves effective and clear agent demand is demonstrated.

---

## Phase 1: Session Capture Foundation (8-10 weeks)

**Target Completion:** Q2 2026  
**Key Dependencies:** Issue #22 (stateful store research), hive v5 core architecture

### 1.1 Schema Design & Storage Backend (2 weeks)

**Deliverables:**
```sql
-- Core session tracking tables
CREATE TABLE agent_sessions (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL,
    agent_id        TEXT        NOT NULL,           -- jakeclaw, pinchyclaw
    session_type    TEXT        NOT NULL,           -- main, subagent, cron
    parent_session  UUID        NULL,               -- Sub-agent hierarchy
    channel         TEXT        NOT NULL,           -- discord, telegram, local
    started_at      TIMESTAMPTZ NOT NULL,
    ended_at        TIMESTAMPTZ NULL,
    working_dir     TEXT        NOT NULL,
    model           TEXT        NOT NULL,           -- claude-sonnet-4
    task_completed  BOOLEAN     DEFAULT false,
    exit_reason     TEXT        NULL,               -- timeout, error, completion
    repo_url        TEXT        NULL,               -- When applicable
    commit_before   TEXT        NULL,               -- Git context
    commit_after    TEXT        NULL,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE session_turns (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID        NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    turn_index      INTEGER     NOT NULL,
    role            TEXT        NOT NULL,           -- human, agent, tool, system
    content         TEXT        NOT NULL,
    tool_name       TEXT        NULL,
    tool_args       JSONB       NULL,
    tool_output     TEXT        NULL,
    tool_success    BOOLEAN     NULL,
    input_tokens    INTEGER     NULL,
    output_tokens   INTEGER     NULL,
    cache_tokens    INTEGER     NULL,
    started_at      TIMESTAMPTZ NOT NULL,
    completed_at    TIMESTAMPTZ NULL,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE session_files (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID        NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    turn_id         UUID        NOT NULL REFERENCES session_turns(id),
    file_path       TEXT        NOT NULL,
    operation       TEXT        NOT NULL,           -- read, write, edit, delete
    content_before  TEXT        NULL,
    content_after   TEXT        NULL,
    diff_unified    TEXT        NULL,
    file_size       BIGINT      NULL,
    mime_type       TEXT        NULL,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);
```

**Tasks:**
- Implement schema in chosen storage backend (pending #22)
- Add necessary indexes for query performance
- Set up tenant isolation and access controls
- Implement data retention/cleanup procedures

### 1.2 Privacy & Sanitization Framework (1 week)

**Deliverables:**
```go
// Privacy sanitization pipeline
type SessionSanitizer struct {
    config SanitizationConfig
}

type SanitizationConfig struct {
    RedactSecrets       bool     `yaml:"redact_secrets"`
    RedactPersonalInfo  bool     `yaml:"redact_personal_info"`
    RedactInternalInfo  bool     `yaml:"redact_internal_info"`
    CustomPatterns      []string `yaml:"custom_patterns"`
    RetentionDays       int      `yaml:"retention_days"`
}

func (s *SessionSanitizer) SanitizeContent(content string) string {
    content = s.redactAPIKeys(content)      // API_KEY=xxx, Bearer xxx
    content = s.redactPasswords(content)    // password=xxx
    content = s.redactTokens(content)       // JWT tokens, OAuth
    content = s.redactEmails(content)       // email@domain.com → e***@d***.com
    content = s.redactIPs(content)          // 192.168.x.x → [IP]
    content = s.redactCustom(content)       // User-configured patterns
    return content
}
```

**Tasks:**
- Implement comprehensive sanitization patterns
- Add configurable sensitivity levels (minimal, standard, verbose)  
- Test against common secret/PII patterns
- Document privacy guarantees and limitations

### 1.3 OpenClaw Integration Hooks (2 weeks)

**Deliverables:**
```javascript
// ACP harness integration points
class SessionCapture {
    constructor(hiveClient, config) {
        this.hive = hiveClient;
        this.config = config;
        this.currentSession = null;
    }
    
    // Session lifecycle hooks
    async onSessionStart(metadata) {
        this.currentSession = await this.hive.createSession({
            agent_id: metadata.agentId,
            session_type: metadata.isSubagent ? 'subagent' : 'main',
            parent_session: metadata.parentSessionId,
            channel: metadata.channel,
            working_dir: process.cwd(),
            model: metadata.model,
            runtime_version: getOpenClawVersion()
        });
    }
    
    async onTurn(turn) {
        if (!this.currentSession) return;
        
        const sanitizedContent = this.sanitizeContent(turn.content);
        await this.hive.addTurn(this.currentSession.id, {
            turn_index: turn.index,
            role: turn.role,
            content: sanitizedContent,
            tool_name: turn.tool?.name,
            tool_args: turn.tool?.args,
            tool_output: this.sanitizeContent(turn.tool?.output),
            input_tokens: turn.tokens?.input,
            output_tokens: turn.tokens?.output,
            started_at: turn.startTime,
            completed_at: turn.endTime
        });
    }
    
    async onFileChange(turnId, fileChange) {
        if (!this.currentSession) return;
        
        await this.hive.recordFileChange(this.currentSession.id, {
            turn_id: turnId,
            file_path: fileChange.path,
            operation: fileChange.operation,
            content_before: this.sanitizeFileContent(fileChange.before),
            content_after: this.sanitizeFileContent(fileChange.after),
            diff_unified: generateDiff(fileChange.before, fileChange.after)
        });
    }
    
    async onSessionEnd(exitReason) {
        if (!this.currentSession) return;
        
        await this.hive.endSession(this.currentSession.id, {
            ended_at: new Date(),
            exit_reason: exitReason,
            task_completed: this.determineTaskCompletion(),
            commit_after: getCurrentGitCommit()
        });
        
        // Trigger background analysis
        this.hive.scheduleSessionAnalysis(this.currentSession.id);
        this.currentSession = null;
    }
}
```

**Tasks:**
- Integrate hooks into ACP harness session lifecycle
- Implement file change detection and diff generation
- Add configuration for capture granularity levels
- Test with multiple claws and session types

### 1.4 Hive API Endpoints (2 weeks)

**Deliverables:**
```go
// Hive-server session endpoints
type SessionAPI struct {
    store SessionStore
    analyzer SessionAnalyzer
}

// POST /sessions - Create new session
func (api *SessionAPI) CreateSession(w http.ResponseWriter, r *http.Request) {
    var req CreateSessionRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "Invalid request body")
        return
    }
    
    session, err := api.store.CreateSession(r.Context(), req)
    if err != nil {
        writeError(w, http.StatusInternalServerError, "Failed to create session")
        return
    }
    
    writeJSON(w, session)
}

// GET /sessions - Query sessions with filtering
func (api *SessionAPI) QuerySessions(w http.ResponseWriter, r *http.Request) {
    filter := parseQueryFilter(r.URL.Query())
    sessions, err := api.store.QuerySessions(r.Context(), filter)
    if err != nil {
        writeError(w, http.StatusInternalServerError, "Query failed")
        return
    }
    
    writeJSON(w, sessions)
}

// PATCH /sessions/{id} - Update session (end, metadata)
func (api *SessionAPI) UpdateSession(w http.ResponseWriter, r *http.Request) {
    sessionID := mux.Vars(r)["id"]
    var update UpdateSessionRequest
    if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
        writeError(w, http.StatusBadRequest, "Invalid request body")
        return
    }
    
    session, err := api.store.UpdateSession(r.Context(), sessionID, update)
    if err != nil {
        writeError(w, http.StatusInternalServerError, "Update failed")
        return
    }
    
    writeJSON(w, session)
}
```

**Tasks:**
- Implement session CRUD endpoints with filtering
- Add session turn and file change endpoints
- Implement pagination for large result sets
- Add proper authentication and tenant isolation

### 1.5 Testing & Validation (1 week)

**Tasks:**
- End-to-end testing with real claw sessions
- Validate privacy sanitization effectiveness
- Performance testing with large sessions
- Integration testing with multiple concurrent sessions

---

## Phase 2: Session Analysis Pipeline (4-6 weeks)

**Target Completion:** Q3 2026  
**Dependencies:** Phase 1 completion, directive extraction research

### 2.1 Pattern Detection Algorithms (2 weeks)

**Deliverables:**
```go
type PatternDetector interface {
    DetectPatterns(session *Session) []Pattern
}

type Pattern struct {
    Type        string      `json:"type"`        // repeated_action, error_pattern, success_sequence
    Confidence  float64     `json:"confidence"`  // 0.0-1.0
    Description string      `json:"description"`
    Context     interface{} `json:"context"`
    Evidence    []TurnID    `json:"evidence"`    // Supporting turn IDs
}

// Example pattern detectors
type RepeatedActionDetector struct{}
func (d *RepeatedActionDetector) DetectPatterns(session *Session) []Pattern {
    // Detect when agent repeatedly reads same file
    // Detect when agent tries same failed approach multiple times
    // Detect when agent asks for same information repeatedly
}

type SuccessPatternDetector struct{}
func (d *SuccessPatternDetector) DetectPatterns(session *Session) []Pattern {
    // Detect successful debugging approaches
    // Detect effective problem-solving sequences
    // Detect successful collaboration patterns
}

type FrictionDetector struct{}
func (d *FrictionDetector) DetectPatterns(session *Session) []Pattern {
    // Detect when agent gets stuck or confused
    // Detect when tools fail repeatedly
    // Detect when agent backtracks frequently
}
```

**Tasks:**
- Implement core pattern detection algorithms
- Define pattern taxonomy and confidence scoring
- Test pattern detection on historical session data
- Tune detection sensitivity and accuracy

### 2.2 Directive Generation Pipeline (2 weeks)

**Deliverables:**
```go
type DirectiveGenerator struct {
    patterns []Pattern
    context  SessionContext
}

func (g *DirectiveGenerator) GenerateDirectives(session *Session, patterns []Pattern) []DirectiveCandidate {
    var directives []DirectiveCandidate
    
    for _, pattern := range patterns {
        switch pattern.Type {
        case "repeated_file_reads":
            directives = append(directives, DirectiveCandidate{
                Content: fmt.Sprintf("In %s, consider caching %s contents instead of repeated reads", 
                    session.RepoURL, pattern.Context.(string)),
                Kind: "pattern",
                SourceType: "experience", 
                SourceID: session.ID.String(),
                Weight: calculateWeight(pattern),
                Context: pattern.Context,
            })
            
        case "successful_debugging":
            directives = append(directives, DirectiveCandidate{
                Content: fmt.Sprintf("When debugging similar issues, try: %s", 
                    pattern.Description),
                Kind: "behavioral",
                SourceType: "experience",
                SourceID: session.ID.String(),
                Weight: calculateWeight(pattern),
            })
        }
    }
    
    return directives
}
```

**Tasks:**
- Implement pattern → directive transformation rules
- Design directive candidate ranking and selection
- Integration with existing directive system
- Testing with sample session patterns

### 2.3 Effectiveness Tracking (1 week)

**Deliverables:**
```sql
-- Track directive effectiveness via session outcomes
CREATE TABLE directive_injections (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    directive_id    UUID        NOT NULL REFERENCES directives(id),
    session_id      UUID        NOT NULL REFERENCES agent_sessions(id),
    turn_id         UUID        NULL REFERENCES session_turns(id),
    injection_rank  INTEGER     NOT NULL,       -- Order in injection list
    relevance_score FLOAT8      NOT NULL,       -- Why this directive was selected
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE directive_outcomes (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    injection_id    UUID        NOT NULL REFERENCES directive_injections(id),
    outcome         TEXT        NOT NULL,       -- followed, ignored, negative
    evidence        TEXT        NULL,           -- How outcome was determined
    confidence      FLOAT8      NULL,           -- 0.0-1.0 confidence in outcome
    recorded_by     TEXT        NULL,           -- agent, human, automatic
    created_at      TIMESTAMPTZ DEFAULT NOW()
);
```

**Tasks:**
- Implement feedback collection mechanisms
- Design automatic outcome detection algorithms  
- Add effectiveness metrics to directive system
- Create feedback loops for directive refinement

### 2.4 Integration & Testing (1 week)

**Tasks:**
- End-to-end testing of analysis pipeline
- Integration with directive injection system
- Performance optimization for batch processing
- Monitoring and alerting for pipeline health

---

## Phase 3: LSP Integration (Conditional, 9-12 weeks)

**Target Completion:** Q4 2026 or later  
**Prerequisites:** Phases 1-2 successful, demonstrated agent demand for LSP capabilities

### 3.1 Decision Point: Proceed with LSP?

**Required Evidence for Proceeding:**
- [ ] Session capture system stable and providing value
- [ ] Agent feedback indicating need for language intelligence  
- [ ] No suitable external LSP-MCP bridge solutions
- [ ] Available engineering capacity after core v5 features
- [ ] Clear integration plan with behavioral knowledge engine

**Alternative Paths if Not Proceeding:**
1. **Support external bridge development**: Contribute to LSP-MCP bridge projects
2. **Enhance existing tools**: Improve file/code tools instead of LSP integration  
3. **Future consideration**: Revisit when ecosystem more mature

### 3.2 LSP Process Management (3 weeks)

**Deliverables (if proceeding):**
```sql
-- LSP server tracking schema per issue #21
CREATE TABLE lsp_servers (
    id              TEXT        PRIMARY KEY,    -- "workspace:/path:language:rust"
    tenant_id       UUID        NOT NULL,
    workspace_path  TEXT        NOT NULL,
    language        TEXT        NOT NULL,       -- rust, go, python, typescript
    server_binary   TEXT        NOT NULL,       -- rust-analyzer, gopls, pyright
    pid             INTEGER     NULL,           -- Process ID (NULL if not running)
    status          TEXT        NOT NULL,       -- spawning, initializing, ready, idle, dead
    last_accessed   INTEGER     NOT NULL,       -- Unix timestamp  
    capabilities    JSONB       NULL,           -- Server capabilities JSON
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);
```

**Tasks:**
- Implement LSP server lifecycle management
- Add process monitoring and health checks
- Implement graceful shutdown and cleanup  
- Add resource limits and monitoring

### 3.3 Language Detection & Configuration (1 week)

**Tasks:**
- Implement workspace language detection
- Add server binary configuration and discovery
- Support per-workspace LSP settings
- Handle multi-language workspaces

### 3.4 JSON-RPC Protocol Integration (2 weeks)

**Tasks:**
- Implement LSP protocol client
- Add capability negotiation and initialization
- Support core LSP methods (hover, definition, references)
- Error handling and timeout management

### 3.5 Agent Tool Integration (1 week)

**Tasks:**
- Implement `cmd=lsp` tool interface per issue #21
- Add request routing and response formatting
- Integration with existing agent tool system
- Documentation and examples for agents

### 3.6 Reconciliation & Cleanup (2 weeks)

**Tasks:**
- Implement background reconciliation logic
- Add automatic server cleanup (>55min idle)
- Dead process detection and restart
- Multi-agent coordination and conflict resolution

---

## Open Questions & Decisions Needed

### Strategic Questions

**Q1: Storage Backend Selection (Critical for Phase 1)**
- **Context:** Issue #22 research pending, affects all session storage
- **Options:** CockroachDB, PostgreSQL, hybrid approach
- **Decision needed by:** Start of Phase 1 implementation
- **Impact:** Entire session capture architecture depends on this

**Q2: Session Data Retention Policy**
- **Context:** Balance between memory value and privacy/cost concerns
- **Options:** 30 days, 90 days, configurable by tenant
- **Decision needed by:** Phase 1.2 privacy framework
- **Impact:** Storage costs, compliance requirements, directive generation data availability

**Q3: Token Cost Budget for Session Shipping**
- **Context:** Full session transcripts vs. summaries vs. hybrid approach
- **Options:** Summary-only ($0.01-0.08/session), full transcripts ($0.15-2.50/session), hybrid
- **Decision needed by:** Phase 1.3 OpenClaw integration  
- **Impact:** Operating costs, data fidelity, debugging capabilities

### Technical Questions

**Q4: Session Capture Granularity**
- **Context:** How much detail to capture vs. privacy/cost concerns
- **Options:** Minimal (metadata only), standard (sanitized content), verbose (full content)
- **Decision needed by:** Phase 1.3 OpenClaw integration
- **Impact:** Data utility, privacy risk, storage costs

**Q5: Directive Effectiveness Measurement**
- **Context:** How to determine if experience-based directives are helpful
- **Options:** Agent feedback, session outcome correlation, manual review
- **Decision needed by:** Phase 2.3 effectiveness tracking
- **Impact:** Learning loop effectiveness, directive quality

**Q6: LSP Integration Architecture**
- **Context:** Native vs. bridge approach for LSP capabilities  
- **Options:** Native hive integration, external LSP-MCP bridge, hybrid
- **Decision needed by:** Phase 3 decision point
- **Impact:** Architectural complexity, maintenance burden, feature capabilities

### Resource Questions

**Q7: Engineering Allocation Priority**
- **Context:** Limited engineering capacity, multiple competing priorities  
- **Options:** Session capture only, parallel development, LSP first
- **Decision needed by:** Phase planning
- **Impact:** Delivery timeline, quality, scope

**Q8: Operational Support Model**
- **Context:** Multi-process LSP system requires specialized operational knowledge
- **Options:** Full operational support, limited support, external bridge
- **Decision needed by:** Phase 3 decision point  
- **Impact:** Long-term maintenance costs, system reliability

## Risk Mitigation Strategies

### Session Capture Risks

**Privacy Risk: Accidental Secret Capture**
- **Mitigation:** Multi-layer sanitization, configurable sensitivity, regular pattern updates
- **Monitoring:** Automated secret detection in stored data, alerts for potential leaks
- **Response:** Immediate data purging, notification protocols, pattern refinement

**Performance Risk: Large Session Storage**
- **Mitigation:** Aggressive data compression, tiered storage, automatic cleanup
- **Monitoring:** Storage growth rates, query performance metrics, cost tracking
- **Response:** Data archival, retention policy adjustment, schema optimization

**Cost Risk: Token Usage Explosion**
- **Mitigation:** Summary-based shipping, configurable capture levels, cost budgets
- **Monitoring:** Per-agent token usage, cost per session metrics, budget alerts
- **Response:** Capture level adjustment, agent education, billing optimization

### LSP Integration Risks

**Complexity Risk: Architectural Overreach**  
- **Mitigation:** External bridge option, phased implementation, clear scope limits
- **Monitoring:** Development velocity, bug rates, operational incidents
- **Response:** Scope reduction, external bridge migration, feature deprecation

**Resource Risk: Memory/CPU Exhaustion**
- **Mitigation:** Resource limits, monitoring, automatic server termination
- **Monitoring:** Server resource usage, system performance impact
- **Response:** Limit adjustment, server restart, workload balancing

**Operational Risk: Complex System Debugging**
- **Mitigation:** Comprehensive logging, operational runbooks, escalation procedures
- **Monitoring:** Error rates, response times, process health
- **Response:** Automated recovery, expert escalation, graceful degradation

## Success Metrics & KPIs

### Phase 1: Session Capture Foundation

**Technical Metrics:**
- [ ] 95%+ session capture success rate across all active claws
- [ ] <500ms p95 latency for session API endpoints
- [ ] 99.9%+ data sanitization effectiveness (no secrets in storage)
- [ ] <1GB storage growth per agent per week

**Business Metrics:**  
- [ ] 100% of active claws shipping session data to hive
- [ ] 50%+ reduction in "agent forgetting previous work" incidents
- [ ] Session query API used >10 times per week by agents

### Phase 2: Session Analysis Pipeline

**Technical Metrics:**
- [ ] 80%+ pattern detection accuracy on test session data
- [ ] 25+ experience-derived directives generated per week
- [ ] <5 minute p95 session analysis completion time
- [ ] 70%+ directive relevance score (agent feedback)

**Business Metrics:**
- [ ] 10%+ improvement in agent task completion rates
- [ ] 5+ agents actively using experience-based directive context
- [ ] 15%+ reduction in repeated agent errors on similar tasks

### Phase 3: LSP Integration (if implemented)

**Technical Metrics:**
- [ ] 95%+ LSP server uptime across all supported languages
- [ ] <200ms p95 latency for LSP queries (hover, definition)
- [ ] 90%+ agent satisfaction with LSP capability quality
- [ ] 50%+ resource efficiency vs. independent LSP servers

**Business Metrics:**
- [ ] 20%+ reduction in code-related agent errors
- [ ] 3+ agents regularly using LSP features for code tasks
- [ ] 15%+ improvement in agent code understanding accuracy

This implementation plan provides a structured approach to delivering both capabilities while maintaining focus on hive's core behavioral intelligence mission and managing complexity appropriately.