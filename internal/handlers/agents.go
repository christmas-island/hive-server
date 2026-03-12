package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/christmas-island/hive-server/internal/model"
	"github.com/christmas-island/hive-server/internal/relay"
)

// --- Agent input/output types ---

type agentHeartbeatInput struct {
	ID   string `path:"id" doc:"Agent ID"`
	Body struct {
		Capabilities      []string `json:"capabilities,omitempty" doc:"Agent capability list"`
		Status            string   `json:"status,omitempty" doc:"Agent status: online or idle (defaults to online)"`
		Activity          string   `json:"activity,omitempty" doc:"Free-text description of current work (e.g. 'reviewing PR #42')"`
		HiveLocalVersion  string   `json:"hive_local_version,omitempty" doc:"Semver string of the hive-local binary (e.g. '2.0.0')"`
		HivePluginVersion string   `json:"hive_plugin_version,omitempty" doc:"Semver string of the hive plugin (e.g. '1.5.0')"`
	}
}

type agentOutput struct {
	Body *model.Agent
}

type agentListOutput struct {
	Body []*model.Agent
}

type agentGetInput struct {
	ID string `path:"id" doc:"Agent ID"`
}

type agentUsageInput struct {
	ID   string `path:"id" doc:"Agent ID"`
	Body struct {
		Model            string  `json:"model" doc:"Model name"`
		InputTokens      int     `json:"inputTokens" doc:"Input token count"`
		OutputTokens     int     `json:"outputTokens" doc:"Output token count"`
		CacheReadTokens  int     `json:"cacheReadTokens" doc:"Cache read token count"`
		CacheWriteTokens int     `json:"cacheWriteTokens" doc:"Cache write token count"`
		TotalTokens      int     `json:"totalTokens" doc:"Total token count"`
		EstimatedCostUsd float64 `json:"estimatedCostUsd" doc:"Estimated cost in USD"`
		SessionID        string  `json:"sessionId" doc:"Session identifier"`
		Timestamp        string  `json:"timestamp" doc:"RFC3339 timestamp"`
	}
}

type agentUsageOutput struct {
	Body struct {
		OK bool `json:"ok"`
	}
}

type agentOnboardInput struct {
	ID string `path:"id" doc:"Agent ID to onboard"`
}

type agentOnboardResponse struct {
	Agent *model.Agent `json:"agent"`
	Token string       `json:"token"`
}

type agentOnboardOutput struct {
	Body *agentOnboardResponse
}

// --- Handlers ---

func (a *API) agentHeartbeat(ctx context.Context, input *agentHeartbeatInput) (*agentOutput, error) {
	status := model.AgentStatus(input.Body.Status)
	switch status {
	case model.AgentStatusOnline, model.AgentStatusIdle:
		// valid
	default:
		status = model.AgentStatusOnline
	}

	agent, err := a.store.Heartbeat(ctx, input.ID, input.Body.Capabilities, status, input.Body.Activity, input.Body.HiveLocalVersion, input.Body.HivePluginVersion)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to record heartbeat")
	}

	// Fire-and-forget relay to only-claws.
	if a.relay != nil {
		go func() {
			_ = a.relay.UpdateStatus(context.Background(), input.ID, string(status), input.Body.Activity)
		}()
	}

	return &agentOutput{Body: agent}, nil
}

func (a *API) agentList(ctx context.Context, _ *struct{}) (*agentListOutput, error) {
	agents, err := a.store.ListAgents(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list agents")
	}
	if agents == nil {
		agents = []*model.Agent{}
	}
	return &agentListOutput{Body: agents}, nil
}

func (a *API) agentGet(ctx context.Context, input *agentGetInput) (*agentOutput, error) {
	agent, err := a.store.GetAgent(ctx, input.ID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("agent not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get agent")
	}
	return &agentOutput{Body: agent}, nil
}

func (a *API) agentUsage(ctx context.Context, input *agentUsageInput) (*agentUsageOutput, error) {
	if a.relay != nil {
		usage := relay.UsageReport{
			Model:            input.Body.Model,
			InputTokens:      input.Body.InputTokens,
			OutputTokens:     input.Body.OutputTokens,
			CacheReadTokens:  input.Body.CacheReadTokens,
			CacheWriteTokens: input.Body.CacheWriteTokens,
			TotalTokens:      input.Body.TotalTokens,
			EstimatedCostUsd: input.Body.EstimatedCostUsd,
			SessionID:        input.Body.SessionID,
			Timestamp:        input.Body.Timestamp,
		}
		go func() {
			_ = a.relay.RecordUsage(context.Background(), input.ID, usage)
		}()
	}
	out := &agentUsageOutput{}
	out.Body.OK = true
	return out, nil
}

func (a *API) agentOnboard(ctx context.Context, input *agentOnboardInput) (*agentOnboardOutput, error) {
	agent, err := a.store.GenerateAgentToken(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to generate agent token")
	}
	// Create a copy without the token exposed in JSON, and return token separately
	agentCopy := *agent
	agentCopy.Token = ""
	return &agentOnboardOutput{
		Body: &agentOnboardResponse{
			Agent: &agentCopy,
			Token: agent.Token,
		},
	}, nil
}

// --- Registration ---

func registerAgents(a *API, api huma.API) {
	huma.Register(api, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/api/v1/agents/{id}/heartbeat",
		Summary:     "Agent heartbeat",
		Description: "Register or update an agent's presence. Creates the agent record if it doesn't exist.",
		Tags:        []string{"Agents"},
		OperationID: "agent-heartbeat",
	}, a.agentHeartbeat)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/agents",
		Summary:     "List agents",
		Description: "Return all known agents with their current presence status.",
		Tags:        []string{"Agents"},
		OperationID: "list-agents",
	}, a.agentList)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/agents/{id}",
		Summary:     "Get an agent",
		Description: "Retrieve a single agent by ID.",
		Tags:        []string{"Agents"},
		OperationID: "get-agent",
	}, a.agentGet)

	huma.Register(api, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/api/v1/agents/{id}/usage",
		Summary:     "Report token usage",
		Description: "Record agent token usage and relay to only-claws.",
		Tags:        []string{"Agents"},
		OperationID: "agent-usage",
	}, a.agentUsage)

	huma.Register(api, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/api/v1/agents/{id}/onboard",
		Summary:     "Onboard a new agent",
		Description: "Generate and return a bearer token for a new agent. This token can be used for future API requests.",
		Tags:        []string{"Agents"},
		OperationID: "onboard-agent",
	}, a.agentOnboard)
}
