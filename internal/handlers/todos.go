package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/christmas-island/hive-server/internal/model"
)

// --- Todo input/output types ---

type todoCreateInput struct {
	XAgentID string `header:"X-Agent-ID" doc:"Calling agent identifier"`
	Body     struct {
		Title      string `json:"title" minLength:"1" doc:"Todo title"`
		ParentTask string `json:"parent_task,omitempty" doc:"Optional parent task ID"`
		Context    string `json:"context,omitempty" doc:"Freeform resumption context"`
		SortOrder  int    `json:"sort_order,omitempty" doc:"Sort order (default 0)"`
	}
}

type todoOutput struct {
	Body *model.Todo
}

type todoGetInput struct {
	ID string `path:"id" doc:"Todo ID"`
}

type todoListInput struct {
	XAgentID string `header:"X-Agent-ID" doc:"Calling agent identifier"`
	Status   string `query:"status" doc:"Filter by status (pending, done, skipped, cancelled)"`
	Limit    int    `query:"limit" doc:"Maximum results (default 100)" minimum:"0"`
	Offset   int    `query:"offset" doc:"Pagination offset" minimum:"0"`
}

type todoListOutput struct {
	Body []*model.Todo
}

type todoUpdateInput struct {
	ID       string `path:"id" doc:"Todo ID"`
	XAgentID string `header:"X-Agent-ID" doc:"Calling agent identifier"`
	Body     struct {
		Title   *string           `json:"title,omitempty" doc:"New title"`
		Status  *model.TodoStatus `json:"status,omitempty" doc:"New status"`
		Context *string           `json:"context,omitempty" doc:"Updated context"`
	}
}

type todoDeleteInput struct {
	ID       string `path:"id" doc:"Todo ID"`
	XAgentID string `header:"X-Agent-ID" doc:"Calling agent identifier"`
}

type todoDeleteOutput struct{}

type todoPruneDoneInput struct {
	XAgentID string `header:"X-Agent-ID" doc:"Calling agent identifier"`
}

type todoPruneOutput struct {
	Body struct {
		Pruned int64 `json:"pruned" doc:"Number of todos pruned"`
	}
}

type todoReorderInput struct {
	XAgentID string `header:"X-Agent-ID" doc:"Calling agent identifier"`
	Body     struct {
		IDs []string `json:"ids" minItems:"1" doc:"Ordered list of todo IDs"`
	}
}

type todoReorderOutput struct{}

// --- Handlers ---

func (a *API) todoCreate(ctx context.Context, input *todoCreateInput) (*todoOutput, error) {
	if input.XAgentID == "" {
		return nil, huma.Error422UnprocessableEntity("X-Agent-ID header is required")
	}
	t := &model.Todo{
		AgentID:    input.XAgentID,
		Title:      input.Body.Title,
		ParentTask: input.Body.ParentTask,
		Context:    input.Body.Context,
		SortOrder:  input.Body.SortOrder,
	}
	result, err := a.store.CreateTodo(ctx, t)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to create todo")
	}
	return &todoOutput{Body: result}, nil
}

func (a *API) todoList(ctx context.Context, input *todoListInput) (*todoListOutput, error) {
	if input.XAgentID == "" {
		return nil, huma.Error422UnprocessableEntity("X-Agent-ID header is required")
	}
	f := model.TodoFilter{
		AgentID: input.XAgentID,
		Status:  input.Status,
		Limit:   input.Limit,
		Offset:  input.Offset,
	}
	todos, err := a.store.ListTodos(ctx, f)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list todos")
	}
	if todos == nil {
		todos = []*model.Todo{}
	}
	return &todoListOutput{Body: todos}, nil
}

func (a *API) todoGet(ctx context.Context, input *todoGetInput) (*todoOutput, error) {
	t, err := a.store.GetTodo(ctx, input.ID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("todo not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get todo")
	}
	return &todoOutput{Body: t}, nil
}

func (a *API) todoUpdate(ctx context.Context, input *todoUpdateInput) (*todoOutput, error) {
	if input.XAgentID == "" {
		return nil, huma.Error422UnprocessableEntity("X-Agent-ID header is required")
	}
	// Ownership check.
	existing, err := a.store.GetTodo(ctx, input.ID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("todo not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get todo")
	}
	if existing.AgentID != input.XAgentID {
		return nil, huma.Error403Forbidden("only the todo owner can update this todo")
	}

	upd := model.TodoUpdate{
		Title:   input.Body.Title,
		Status:  input.Body.Status,
		Context: input.Body.Context,
	}
	result, err := a.store.UpdateTodo(ctx, input.ID, upd)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("todo not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to update todo")
	}
	return &todoOutput{Body: result}, nil
}

func (a *API) todoDelete(ctx context.Context, input *todoDeleteInput) (*todoDeleteOutput, error) {
	if input.XAgentID == "" {
		return nil, huma.Error422UnprocessableEntity("X-Agent-ID header is required")
	}
	// Ownership check.
	existing, err := a.store.GetTodo(ctx, input.ID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("todo not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get todo")
	}
	if existing.AgentID != input.XAgentID {
		return nil, huma.Error403Forbidden("only the todo owner can delete this todo")
	}

	if err := a.store.DeleteTodo(ctx, input.ID); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, huma.Error404NotFound("todo not found")
		}
		return nil, huma.Error500InternalServerError("failed to delete todo")
	}
	return &todoDeleteOutput{}, nil
}

func (a *API) todoPruneDone(ctx context.Context, input *todoPruneDoneInput) (*todoPruneOutput, error) {
	if input.XAgentID == "" {
		return nil, huma.Error422UnprocessableEntity("X-Agent-ID header is required")
	}
	n, err := a.store.PruneDoneTodos(ctx, input.XAgentID, 0)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to prune todos")
	}
	out := &todoPruneOutput{}
	out.Body.Pruned = n
	return out, nil
}

func (a *API) todoReorder(ctx context.Context, input *todoReorderInput) (*todoReorderOutput, error) {
	if input.XAgentID == "" {
		return nil, huma.Error422UnprocessableEntity("X-Agent-ID header is required")
	}
	if err := a.store.ReorderTodos(ctx, input.XAgentID, input.Body.IDs); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, huma.Error404NotFound("one or more todo IDs not found or not owned by agent")
		}
		return nil, huma.Error500InternalServerError("failed to reorder todos")
	}
	return &todoReorderOutput{}, nil
}

// --- Registration ---

func registerTodos(a *API, api huma.API) {
	huma.Register(api, huma.Operation{
		Method:        http.MethodPost,
		Path:          "/api/v1/todos",
		Summary:       "Create a todo",
		Description:   "Create a new agent-scoped todo. Agent ID from X-Agent-ID header.",
		Tags:          []string{"Todos"},
		OperationID:   "create-todo",
		DefaultStatus: http.StatusCreated,
	}, a.todoCreate)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/todos",
		Summary:     "List todos",
		Description: "List todos for the calling agent. Always scoped to X-Agent-ID.",
		Tags:        []string{"Todos"},
		OperationID: "list-todos",
	}, a.todoList)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/todos/{id}",
		Summary:     "Get a todo",
		Description: "Retrieve a single todo by ID.",
		Tags:        []string{"Todos"},
		OperationID: "get-todo",
	}, a.todoGet)

	huma.Register(api, huma.Operation{
		Method:      http.MethodPatch,
		Path:        "/api/v1/todos/{id}",
		Summary:     "Update a todo",
		Description: "Update a todo's title, status, or context. Ownership enforced.",
		Tags:        []string{"Todos"},
		OperationID: "update-todo",
	}, a.todoUpdate)

	huma.Register(api, huma.Operation{
		Method:        http.MethodDelete,
		Path:          "/api/v1/todos/{id}",
		Summary:       "Delete a todo",
		Description:   "Remove a todo. Ownership enforced.",
		Tags:          []string{"Todos"},
		OperationID:   "delete-todo",
		DefaultStatus: http.StatusNoContent,
	}, a.todoDelete)

	huma.Register(api, huma.Operation{
		Method:      http.MethodDelete,
		Path:        "/api/v1/todos/done",
		Summary:     "Prune completed todos",
		Description: "Delete all non-pending todos for the calling agent.",
		Tags:        []string{"Todos"},
		OperationID: "prune-done-todos",
	}, a.todoPruneDone)

	huma.Register(api, huma.Operation{
		Method:        http.MethodPost,
		Path:          "/api/v1/todos/reorder",
		Summary:       "Reorder todos",
		Description:   "Set the sort order of todos by providing an ordered list of IDs.",
		Tags:          []string{"Todos"},
		OperationID:   "reorder-todos",
		DefaultStatus: http.StatusNoContent,
	}, a.todoReorder)
}
