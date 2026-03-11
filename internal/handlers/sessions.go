package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/christmas-island/hive-server/internal/model"
)

// --- Input/Output types ---

type sessionCreateInput struct {
	XAgentID string `header:"X-Agent-ID"`
	Body     *model.CapturedSession
}

type sessionCreateOutput struct {
	Status int
	Body   *model.CapturedSession
}

type sessionGetInput struct {
	ID string `path:"id"`
}

type sessionGetOutput struct {
	Body *model.CapturedSession
}

type sessionListInput struct {
	AgentID string `query:"agent_id" doc:"Filter by agent ID"`
	Repo    string `query:"repo" doc:"Filter by repository (e.g. christmas-island/hive-server)"`
	Path    string `query:"path" doc:"Filter by file path (substring match)"`
	Since   string `query:"since" doc:"Filter sessions started after this RFC3339 timestamp"`
	Limit   int    `query:"limit" doc:"Max results (1-100, default 50)"`
}

type sessionListOutput struct {
	Body []*model.CapturedSession
}

// --- Handler functions ---

func (a *API) sessionCreate(ctx context.Context, input *sessionCreateInput) (*sessionCreateOutput, error) {
	cs := input.Body
	if cs == nil {
		cs = &model.CapturedSession{}
	}
	// Fill agent_id from header if not provided in body.
	if cs.AgentID == "" && input.XAgentID != "" {
		cs.AgentID = input.XAgentID
	}

	created, err := a.store.CreateCapturedSession(ctx, cs)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to create session")
	}
	return &sessionCreateOutput{Status: http.StatusCreated, Body: created}, nil
}

func (a *API) sessionGet(ctx context.Context, input *sessionGetInput) (*sessionGetOutput, error) {
	cs, err := a.store.GetCapturedSession(ctx, input.ID)
	if err == model.ErrNotFound {
		return nil, huma.Error404NotFound("session not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get session")
	}
	return &sessionGetOutput{Body: cs}, nil
}

func (a *API) sessionList(ctx context.Context, input *sessionListInput) (*sessionListOutput, error) {
	f := model.SessionFilter{
		AgentID: input.AgentID,
		Repo:    input.Repo,
		Path:    input.Path,
		Limit:   input.Limit,
	}
	if input.Since != "" {
		t, err := time.Parse(time.RFC3339, input.Since)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid since timestamp: must be RFC3339")
		}
		f.Since = t
	}

	sessions, err := a.store.ListCapturedSessions(ctx, f)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list sessions")
	}
	if sessions == nil {
		sessions = []*model.CapturedSession{}
	}
	return &sessionListOutput{Body: sessions}, nil
}

// registerSessions wires session capture endpoints into the Huma API.
func registerSessions(a *API, api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "session-create",
		Method:      http.MethodPost,
		Path:        "/api/v1/sessions",
		Summary:     "Create a captured session",
		Description: "Ship a recorded agent session (transcript, tool calls, token usage) to hive-server.",
		Tags:        []string{"sessions"},
	}, a.sessionCreate)

	huma.Register(api, huma.Operation{
		OperationID: "session-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/sessions/{id}",
		Summary:     "Get a captured session by ID",
		Tags:        []string{"sessions"},
	}, a.sessionGet)

	huma.Register(api, huma.Operation{
		OperationID: "session-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/sessions",
		Summary:     "List captured sessions",
		Description: "Filter by agent, repo, file path, and/or time range.",
		Tags:        []string{"sessions"},
	}, a.sessionList)
}
