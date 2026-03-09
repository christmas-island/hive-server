package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/christmas-island/hive-server/internal/model"
)

// --- Agent input/output types ---

type agentHeartbeatInput struct {
	ID   string `path:"id" doc:"Agent ID"`
	Body struct {
		Capabilities []string `json:"capabilities,omitempty" doc:"Agent capability list"`
		Status       string   `json:"status,omitempty" doc:"Agent status: online or idle (defaults to online)"`
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

// --- Handlers ---

func (a *API) agentHeartbeat(ctx context.Context, input *agentHeartbeatInput) (*agentOutput, error) {
	status := model.AgentStatus(input.Body.Status)
	switch status {
	case model.AgentStatusOnline, model.AgentStatusIdle:
		// valid
	default:
		status = model.AgentStatusOnline
	}

	agent, err := a.store.Heartbeat(ctx, input.ID, input.Body.Capabilities, status)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to record heartbeat")
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
}
