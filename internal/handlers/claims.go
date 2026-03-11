package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/christmas-island/hive-server/internal/model"
)

// --- Claim input/output types ---

type claimCreateInput struct {
	XAgentID string `header:"X-Agent-ID" doc:"Calling agent identifier"`
	Body     struct {
		Type      model.ClaimType   `json:"type" enum:"issue,review,conch" doc:"Claim type"`
		Resource  string            `json:"resource" minLength:"1" doc:"Resource identifier (e.g. ops#79, hive-plugin#3)"`
		ExpiresIn string            `json:"expires_in,omitempty" doc:"Duration (e.g. 1h, 30m). Default 1h."`
		Metadata  map[string]string `json:"metadata,omitempty" doc:"Extensible key-value metadata"`
	}
}

type claimOutput struct {
	Body *model.Claim
}

type claimGetInput struct {
	ID string `path:"id" doc:"Claim ID"`
}

type claimListInput struct {
	Type       string `query:"type" doc:"Filter by claim type"`
	AgentID    string `query:"agent_id" doc:"Filter by agent ID"`
	Resource   string `query:"resource" doc:"Filter by resource"`
	Status     string `query:"status" doc:"Filter by status (active, expired, released)"`
	SessionKey string `query:"session_key" doc:"Filter by session key"`
	Limit      int    `query:"limit" doc:"Maximum results (default 50)" minimum:"0"`
	Offset     int    `query:"offset" doc:"Pagination offset" minimum:"0"`
}

type claimListOutput struct {
	Body []*model.Claim
}

type claimReleaseInput struct {
	XAgentID string `header:"X-Agent-ID" doc:"Calling agent identifier"`
	ID       string `path:"id" doc:"Claim ID"`
}

type claimRenewInput struct {
	XAgentID string `header:"X-Agent-ID" doc:"Calling agent identifier"`
	ID       string `path:"id" doc:"Claim ID"`
	Body     struct {
		ExpiresIn string `json:"expires_in,omitempty" doc:"Duration (e.g. 1h, 30m). Default 1h."`
	}
}

// --- Handlers ---

func (a *API) claimCreate(ctx context.Context, input *claimCreateInput) (*claimOutput, error) {
	if input.XAgentID == "" {
		return nil, huma.Error422UnprocessableEntity("X-Agent-ID header is required to create a claim")
	}

	dur := time.Hour
	if input.Body.ExpiresIn != "" {
		parsed, err := time.ParseDuration(input.Body.ExpiresIn)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid expires_in duration")
		}
		dur = parsed
	}

	c := &model.Claim{
		Type:           input.Body.Type,
		Resource:       input.Body.Resource,
		AgentID:        input.XAgentID,
		Metadata:       input.Body.Metadata,
		SessionContext: sessionFromCtx(ctx),
		ExpiresAt:      time.Now().UTC().Add(dur),
	}
	result, err := a.store.CreateClaim(ctx, c)
	if errors.Is(err, model.ErrConflict) {
		return nil, huma.Error409Conflict("an active claim already exists on this resource")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to create claim")
	}
	return &claimOutput{Body: result}, nil
}

func (a *API) claimGet(ctx context.Context, input *claimGetInput) (*claimOutput, error) {
	c, err := a.store.GetClaim(ctx, input.ID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("claim not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get claim")
	}
	return &claimOutput{Body: c}, nil
}

func (a *API) claimList(ctx context.Context, input *claimListInput) (*claimListOutput, error) {
	f := model.ClaimFilter{
		Type:       input.Type,
		AgentID:    input.AgentID,
		Resource:   input.Resource,
		Status:     input.Status,
		SessionKey: input.SessionKey,
		Limit:      input.Limit,
		Offset:     input.Offset,
	}
	claims, err := a.store.ListClaims(ctx, f)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list claims")
	}
	if claims == nil {
		claims = []*model.Claim{}
	}
	return &claimListOutput{Body: claims}, nil
}

func (a *API) claimRelease(ctx context.Context, input *claimReleaseInput) (*claimOutput, error) {
	// Ownership check: only the claim owner can release it.
	if input.XAgentID != "" {
		existing, err := a.store.GetClaim(ctx, input.ID)
		if errors.Is(err, model.ErrNotFound) {
			return nil, huma.Error404NotFound("claim not found")
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to get claim")
		}
		if existing.AgentID != input.XAgentID {
			return nil, huma.Error403Forbidden("only the claim owner can release this claim")
		}
	}

	c, err := a.store.ReleaseClaim(ctx, input.ID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("claim not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to release claim")
	}
	return &claimOutput{Body: c}, nil
}

func (a *API) claimRenew(ctx context.Context, input *claimRenewInput) (*claimOutput, error) {
	// Ownership check: only the claim owner can renew it.
	if input.XAgentID != "" {
		existing, err := a.store.GetClaim(ctx, input.ID)
		if errors.Is(err, model.ErrNotFound) {
			return nil, huma.Error404NotFound("claim not found")
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to get claim")
		}
		if existing.AgentID != input.XAgentID {
			return nil, huma.Error403Forbidden("only the claim owner can renew this claim")
		}
	}

	dur := time.Hour
	if input.Body.ExpiresIn != "" {
		parsed, err := time.ParseDuration(input.Body.ExpiresIn)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid expires_in duration")
		}
		dur = parsed
	}

	expiresAt := time.Now().UTC().Add(dur)
	c, err := a.store.RenewClaim(ctx, input.ID, expiresAt)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("claim not found or not active")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to renew claim")
	}
	return &claimOutput{Body: c}, nil
}

// --- Registration ---

func registerClaims(a *API, api huma.API) {
	huma.Register(api, huma.Operation{
		Method:        http.MethodPost,
		Path:          "/api/v1/claims",
		Summary:       "Create a claim",
		Description:   "Acquire an exclusive claim on a resource. Returns 409 if the resource is already actively claimed.",
		Tags:          []string{"Claims"},
		OperationID:   "create-claim",
		DefaultStatus: http.StatusCreated,
	}, a.claimCreate)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/claims",
		Summary:     "List claims",
		Description: "Return claims, optionally filtered by type, agent, resource, or status.",
		Tags:        []string{"Claims"},
		OperationID: "list-claims",
	}, a.claimList)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/claims/{id}",
		Summary:     "Get a claim",
		Description: "Retrieve a single claim by ID.",
		Tags:        []string{"Claims"},
		OperationID: "get-claim",
	}, a.claimGet)

	huma.Register(api, huma.Operation{
		Method:      http.MethodDelete,
		Path:        "/api/v1/claims/{id}",
		Summary:     "Release a claim",
		Description: "Release an active claim, freeing the resource for others.",
		Tags:        []string{"Claims"},
		OperationID: "release-claim",
	}, a.claimRelease)

	huma.Register(api, huma.Operation{
		Method:      http.MethodPatch,
		Path:        "/api/v1/claims/{id}",
		Summary:     "Renew a claim",
		Description: "Extend the expiry of an active claim.",
		Tags:        []string{"Claims"},
		OperationID: "renew-claim",
	}, a.claimRenew)
}
