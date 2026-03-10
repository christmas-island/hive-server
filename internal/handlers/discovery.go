package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/christmas-island/hive-server/internal/model"
)

// --- Input/output types ---

type discoveryAgentListOutput struct {
	Body []*model.DiscoveryAgent
}

type discoveryAgentGetInput struct {
	ID string `path:"id" doc:"Agent ID"`
}

type discoveryAgentOutput struct {
	Body *model.DiscoveryAgent
}

type discoveryAgentPutInput struct {
	ID   string `path:"id" doc:"Agent ID"`
	Body model.DiscoveryAgentMeta
}

type discoveryChannelListOutput struct {
	Body []*model.DiscoveryChannel
}

type discoveryChannelGetInput struct {
	ID string `path:"id" doc:"Channel ID slug"`
}

type discoveryChannelOutput struct {
	Body *model.DiscoveryChannel
}

// discoveryChannelBody is the PUT request body for channels (no auto-managed timestamps).
type discoveryChannelBody struct {
	Name      string   `json:"name" doc:"Display name"`
	DiscordID string   `json:"discord_id,omitempty" doc:"Discord channel snowflake"`
	Purpose   string   `json:"purpose,omitempty" doc:"Channel purpose"`
	Category  string   `json:"category,omitempty" doc:"Parent category name"`
	Members   []string `json:"members,omitempty" doc:"Agent IDs with access"`
}

type discoveryChannelPutInput struct {
	ID   string `path:"id" doc:"Channel ID slug"`
	Body discoveryChannelBody
}

type discoveryRoleListOutput struct {
	Body []*model.DiscoveryRole
}

type discoveryRoleGetInput struct {
	ID string `path:"id" doc:"Role ID slug"`
}

type discoveryRoleOutput struct {
	Body *model.DiscoveryRole
}

// discoveryRoleBody is the PUT request body for roles (no auto-managed timestamps).
type discoveryRoleBody struct {
	Name      string   `json:"name" doc:"Display name"`
	DiscordID string   `json:"discord_id,omitempty" doc:"Discord role snowflake"`
	Members   []string `json:"members,omitempty" doc:"Agent IDs"`
}

type discoveryRolePutInput struct {
	ID   string `path:"id" doc:"Role ID slug"`
	Body discoveryRoleBody
}

type discoveryRoutingInput struct {
	AgentID string `path:"agent_id" doc:"Agent ID"`
}

type discoveryRoutingOutput struct {
	Body *model.DiscoveryRouting
}

// --- Handlers ---

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
	da, err := a.store.GetDiscoveryAgent(ctx, input.ID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("agent not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get discovery agent")
	}
	return &discoveryAgentOutput{Body: da}, nil
}

func (a *API) discoveryPutAgentMeta(ctx context.Context, input *discoveryAgentPutInput) (*discoveryAgentOutput, error) {
	da, err := a.store.UpsertAgentMeta(ctx, input.ID, &input.Body)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("agent not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to upsert agent meta")
	}
	return &discoveryAgentOutput{Body: da}, nil
}

func (a *API) discoveryListChannels(ctx context.Context, _ *struct{}) (*discoveryChannelListOutput, error) {
	channels, err := a.store.ListChannels(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list channels")
	}
	if channels == nil {
		channels = []*model.DiscoveryChannel{}
	}
	return &discoveryChannelListOutput{Body: channels}, nil
}

func (a *API) discoveryGetChannel(ctx context.Context, input *discoveryChannelGetInput) (*discoveryChannelOutput, error) {
	ch, err := a.store.GetChannel(ctx, input.ID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("channel not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get channel")
	}
	return &discoveryChannelOutput{Body: ch}, nil
}

func (a *API) discoveryPutChannel(ctx context.Context, input *discoveryChannelPutInput) (*discoveryChannelOutput, error) {
	ch := &model.DiscoveryChannel{
		ID:        input.ID,
		Name:      input.Body.Name,
		DiscordID: input.Body.DiscordID,
		Purpose:   input.Body.Purpose,
		Category:  input.Body.Category,
		Members:   input.Body.Members,
	}
	result, err := a.store.UpsertChannel(ctx, ch)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to upsert channel")
	}
	return &discoveryChannelOutput{Body: result}, nil
}

func (a *API) discoveryListRoles(ctx context.Context, _ *struct{}) (*discoveryRoleListOutput, error) {
	roles, err := a.store.ListRoles(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list roles")
	}
	if roles == nil {
		roles = []*model.DiscoveryRole{}
	}
	return &discoveryRoleListOutput{Body: roles}, nil
}

func (a *API) discoveryGetRole(ctx context.Context, input *discoveryRoleGetInput) (*discoveryRoleOutput, error) {
	role, err := a.store.GetRole(ctx, input.ID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("role not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get role")
	}
	return &discoveryRoleOutput{Body: role}, nil
}

func (a *API) discoveryPutRole(ctx context.Context, input *discoveryRolePutInput) (*discoveryRoleOutput, error) {
	role := &model.DiscoveryRole{
		ID:        input.ID,
		Name:      input.Body.Name,
		DiscordID: input.Body.DiscordID,
		Members:   input.Body.Members,
	}
	result, err := a.store.UpsertRole(ctx, role)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to upsert role")
	}
	return &discoveryRoleOutput{Body: result}, nil
}

func (a *API) discoveryGetRouting(ctx context.Context, input *discoveryRoutingInput) (*discoveryRoutingOutput, error) {
	da, err := a.store.GetDiscoveryAgent(ctx, input.AgentID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("agent not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get routing info")
	}
	routing := &model.DiscoveryRouting{
		AgentID: da.Agent.ID,
	}
	if da.DiscoveryAgentMeta != nil {
		routing.MentionFormat = da.DiscoveryAgentMeta.MentionFormat
		routing.HomeChannel = da.DiscoveryAgentMeta.HomeChannel
		routing.Channels = da.DiscoveryAgentMeta.Channels
	}
	return &discoveryRoutingOutput{Body: routing}, nil
}

// --- Registration ---

func registerDiscovery(a *API, api huma.API) {
	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/discovery/agents",
		Summary:     "List discovery agents",
		Description: "Return all agents with their discovery metadata.",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-list-agents",
	}, a.discoveryListAgents)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/discovery/agents/{id}",
		Summary:     "Get discovery agent",
		Description: "Retrieve a single agent with its discovery metadata by ID.",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-get-agent",
	}, a.discoveryGetAgent)

	huma.Register(api, huma.Operation{
		Method:      http.MethodPut,
		Path:        "/api/v1/discovery/agents/{id}",
		Summary:     "Upsert agent discovery metadata",
		Description: "Update the discovery metadata for an existing agent.",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-put-agent-meta",
	}, a.discoveryPutAgentMeta)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/discovery/channels",
		Summary:     "List discovery channels",
		Description: "Return all registered Discord channels.",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-list-channels",
	}, a.discoveryListChannels)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/discovery/channels/{id}",
		Summary:     "Get discovery channel",
		Description: "Retrieve a single channel by ID slug.",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-get-channel",
	}, a.discoveryGetChannel)

	huma.Register(api, huma.Operation{
		Method:      http.MethodPut,
		Path:        "/api/v1/discovery/channels/{id}",
		Summary:     "Upsert discovery channel",
		Description: "Create or update a channel record.",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-put-channel",
	}, a.discoveryPutChannel)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/discovery/roles",
		Summary:     "List discovery roles",
		Description: "Return all registered Discord roles.",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-list-roles",
	}, a.discoveryListRoles)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/discovery/roles/{id}",
		Summary:     "Get discovery role",
		Description: "Retrieve a single role by ID slug.",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-get-role",
	}, a.discoveryGetRole)

	huma.Register(api, huma.Operation{
		Method:      http.MethodPut,
		Path:        "/api/v1/discovery/roles/{id}",
		Summary:     "Upsert discovery role",
		Description: "Create or update a role record.",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-put-role",
	}, a.discoveryPutRole)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/discovery/routing/{agent_id}",
		Summary:     "Get agent routing",
		Description: "Return routing information (mention format, home channel, channels) for an agent.",
		Tags:        []string{"Discovery"},
		OperationID: "discovery-get-routing",
	}, a.discoveryGetRouting)
}
