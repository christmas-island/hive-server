package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/christmas-island/hive-server/internal/model"
)

// --- Memory input/output types ---

type memoryUpsertInput struct {
	XAgentID string `header:"X-Agent-ID" doc:"Calling agent identifier"`
	Body     struct {
		Key     string   `json:"key" doc:"Memory key" minLength:"1"`
		Value   string   `json:"value" doc:"Memory value" minLength:"1"`
		Tags    []string `json:"tags,omitempty" doc:"Optional tags"`
		Version int64    `json:"version,omitempty" doc:"Version for optimistic concurrency (0 = no check)"`
	}
}

type memoryOutput struct {
	Body *model.MemoryEntry
}

type memoryGetInput struct {
	Key string `path:"key" doc:"Memory key"`
}

type memoryListInput struct {
	Tag    string `query:"tag" doc:"Filter by tag"`
	Agent  string `query:"agent" doc:"Filter by agent ID"`
	Prefix string `query:"prefix" doc:"Filter by key prefix"`
	Limit  int    `query:"limit" doc:"Maximum results (default 50)" minimum:"0"`
	Offset int    `query:"offset" doc:"Pagination offset" minimum:"0"`
}

type memoryListOutput struct {
	Body []*model.MemoryEntry
}

type memoryDeleteInput struct {
	Key string `path:"key" doc:"Memory key"`
}

type memoryDeleteOutput struct{}

// --- Handlers ---

func (a *API) memoryUpsert(ctx context.Context, input *memoryUpsertInput) (*memoryOutput, error) {
	entry := &model.MemoryEntry{
		Key:     input.Body.Key,
		Value:   input.Body.Value,
		AgentID: input.XAgentID,
		Tags:    input.Body.Tags,
		Version: input.Body.Version,
	}

	result, err := a.store.UpsertMemory(ctx, entry)
	if errors.Is(err, model.ErrConflict) {
		return nil, huma.Error409Conflict("version conflict: stale data")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to upsert memory")
	}
	return &memoryOutput{Body: result}, nil
}

func (a *API) memoryGet(ctx context.Context, input *memoryGetInput) (*memoryOutput, error) {
	entry, err := a.store.GetMemory(ctx, input.Key)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("memory entry not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get memory")
	}
	return &memoryOutput{Body: entry}, nil
}

func (a *API) memoryList(ctx context.Context, input *memoryListInput) (*memoryListOutput, error) {
	f := model.MemoryFilter{
		Tag:    input.Tag,
		Agent:  input.Agent,
		Prefix: input.Prefix,
		Limit:  input.Limit,
		Offset: input.Offset,
	}
	entries, err := a.store.ListMemory(ctx, f)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list memory")
	}
	if entries == nil {
		entries = []*model.MemoryEntry{}
	}
	return &memoryListOutput{Body: entries}, nil
}

func (a *API) memoryDelete(ctx context.Context, input *memoryDeleteInput) (*memoryDeleteOutput, error) {
	err := a.store.DeleteMemory(ctx, input.Key)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("memory entry not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to delete memory")
	}
	return &memoryDeleteOutput{}, nil
}

// --- Registration ---

func registerMemory(a *API, api huma.API) {
	huma.Register(api, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/api/v1/memory",
		Summary:     "Upsert a memory entry",
		Description: "Create or update a memory entry by key. Supports optimistic concurrency via version.",
		Tags:        []string{"Memory"},
		OperationID: "upsert-memory",
	}, a.memoryUpsert)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/memory",
		Summary:     "List memory entries",
		Description: "Return memory entries, optionally filtered by tag, agent, or key prefix.",
		Tags:        []string{"Memory"},
		OperationID: "list-memory",
	}, a.memoryList)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/memory/{key}",
		Summary:     "Get a memory entry",
		Description: "Retrieve a single memory entry by key.",
		Tags:        []string{"Memory"},
		OperationID: "get-memory",
	}, a.memoryGet)

	huma.Register(api, huma.Operation{
		Method:        http.MethodDelete,
		Path:          "/api/v1/memory/{key}",
		Summary:       "Delete a memory entry",
		Description:   "Remove a memory entry by key.",
		Tags:          []string{"Memory"},
		OperationID:   "delete-memory",
		DefaultStatus: http.StatusNoContent,
	}, a.memoryDelete)
}
