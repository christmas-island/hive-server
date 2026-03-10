package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/christmas-island/hive-server/internal/model"
)

// --- Input/output types ---

type discoveryAgentUpsertInput struct {
	Name string `path:"name" doc:"Agent name"`
	Body struct {
		DiscordUserID string          `json:"discord_user_id,omitempty"`
		HomeChannel   string          `json:"home_channel,omitempty"`
		Capabilities  json.RawMessage `json:"capabilities,omitempty"`
		Status        string          `json:"status,omitempty"`
		Metadata      json.RawMessage `json:"metadata,omitempty"`
	}
}

type discoveryAgentOutput struct {
	Body *model.DiscoveryAgent
}

type discoveryAgentListOutput struct {
	Body []*model.DiscoveryAgent
}

type discoveryAgentGetInput struct {
	Name string `path:"name" doc:"Agent name"`
}

type discoveryChannelUpsertInput struct {
	Name string `path:"name" doc:"Channel name"`
	Body struct {
		DiscordChannelID string          `json:"discord_channel_id,omitempty"`
		Purpose          string          `json:"purpose,omitempty"`
		Metadata         json.RawMessage `json:"metadata,omitempty"`
	}
}

type discoveryChannelOutput struct {
	Body *model.DiscoveryChannel
}

type discoveryChannelListOutput struct {
	Body []*model.DiscoveryChannel
}

type discoveryChannelGetInput struct {
	Name string `path:"name" doc:"Channel name"`
}

type discoveryRoleListOutput struct {
	Body []*model.DiscoveryRole
}

type discoveryRoutingInput struct {
	Agent string `path:"agent" doc:"Agent name"`
}

type discoveryRoutingOutput struct {
	Body *model.RoutingInfo
}

// --- Handlers ---

func (a *API) discoveryUpsertAgent(ctx context.Context, input *discoveryAgentUpsertInput) (*discoveryAgentOutput, error) {
	agentID, _ := ctx.Value(ctxKeyAgentID).(string)
	if agentID != input.Name {
		return nil, huma.Error403Forbidden("X-Agent-ID must match the agent name")
	}

	agent := &model.DiscoveryAgent{
		Name:          input.Name,
		DiscordUserID: input.Body.DiscordUserID,
		HomeChannel:   input.Body.HomeChannel,
		Capabilities:  input.Body.Capabilities,
		Status:        input.Body.Status,
		Metadata:      input.Body.Metadata,
	}
	got, err := a.store.UpsertDiscoveryAgent(ctx, agent)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to upsert discovery agent")
	}
	return &discoveryAgentOutput{Body: got}, nil
}

func (a *API) discoveryListAgents(ctx context.Context, _ *struct{}) (*discoveryAgentListOutput, error) {
	agents, err := a.store.ListDiscoveryAgents(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list discovery agents")
	}
	if agents == nil {
		agents = []*model.DiscoveryAgent{}
	}
	return &discoveryAgentListOutput{Body: agents}, nil
}

func (a *API) discoveryGetAgent(ctx context.Context, input *discoveryAgentGetInput) (*discoveryAgentOutput, error) {
	agent, err := a.store.GetDiscoveryAgent(ctx, input.Name)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("agent not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get discovery agent")
	}
	return &discoveryAgentOutput{Body: agent}, nil
}

func (a *API) discoveryUpsertChannel(ctx context.Context, input *discoveryChannelUpsertInput) (*discoveryChannelOutput, error) {
	ch := &model.DiscoveryChannel{
		Name:             input.Name,
		DiscordChannelID: input.Body.DiscordChannelID,
		Purpose:          input.Body.Purpose,
		Metadata:         input.Body.Metadata,
	}
	got, err := a.store.UpsertDiscoveryChannel(ctx, ch)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to upsert discovery channel")
	}
	return &discoveryChannelOutput{Body: got}, nil
}

func (a *API) discoveryListChannels(ctx context.Context, _ *struct{}) (*discoveryChannelListOutput, error) {
	channels, err := a.store.ListDiscoveryChannels(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list discovery channels")
	}
	if channels == nil {
		channels = []*model.DiscoveryChannel{}
	}
	return &discoveryChannelListOutput{Body: channels}, nil
}

func (a *API) discoveryGetChannel(ctx context.Context, input *discoveryChannelGetInput) (*discoveryChannelOutput, error) {
	ch, err := a.store.GetDiscoveryChannel(ctx, input.Name)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("channel not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get discovery channel")
	}
	return &discoveryChannelOutput{Body: ch}, nil
}

func (a *API) discoveryListRoles(ctx context.Context, _ *struct{}) (*discoveryRoleListOutput, error) {
	roles, err := a.store.ListDiscoveryRoles(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list discovery roles")
	}
	if roles == nil {
		roles = []*model.DiscoveryRole{}
	}
	return &discoveryRoleListOutput{Body: roles}, nil
}

func (a *API) discoveryRouting(ctx context.Context, input *discoveryRoutingInput) (*discoveryRoutingOutput, error) {
	agent, err := a.store.GetDiscoveryAgent(ctx, input.Agent)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("agent not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get routing info")
	}
	routing := &model.RoutingInfo{
		Mention:          "<@" + agent.DiscordUserID + ">",
		HomeChannel:      agent.HomeChannel,
		SessionKeyFormat: agent.Name + "-ai[bot]",
	}
	return &discoveryRoutingOutput{Body: routing}, nil
}

// --- Registration ---

func registerDiscovery(a *API, api huma.API) {
	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/discovery/agents",
		Summary:     "List discovery agents",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-list-agents",
	}, a.discoveryListAgents)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/discovery/agents/{name}",
		Summary:     "Get discovery agent",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-get-agent",
	}, a.discoveryGetAgent)

	huma.Register(api, huma.Operation{
		Method:      http.MethodPut,
		Path:        "/api/v1/discovery/agents/{name}",
		Summary:     "Self-register or update discovery agent",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-upsert-agent",
	}, a.discoveryUpsertAgent)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/discovery/channels",
		Summary:     "List discovery channels",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-list-channels",
	}, a.discoveryListChannels)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/discovery/channels/{name}",
		Summary:     "Get discovery channel",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-get-channel",
	}, a.discoveryGetChannel)

	huma.Register(api, huma.Operation{
		Method:      http.MethodPut,
		Path:        "/api/v1/discovery/channels/{name}",
		Summary:     "Register or update discovery channel",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-upsert-channel",
	}, a.discoveryUpsertChannel)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/discovery/roles",
		Summary:     "List discovery roles",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-list-roles",
	}, a.discoveryListRoles)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/discovery/routing/{agent}",
		Summary:     "Get routing info for an agent",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-routing",
	}, a.discoveryRouting)
}
