package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/christmas-island/hive-server/internal/model"
)

// --- Task input/output types ---

type taskCreateInput struct {
	XAgentID string `header:"X-Agent-ID" doc:"Calling agent identifier (becomes task creator)"`
	Body     struct {
		Title       string   `json:"title" doc:"Task title" minLength:"1"`
		Description string   `json:"description,omitempty" doc:"Task description"`
		Priority    int      `json:"priority,omitempty" doc:"Task priority"`
		Tags        []string `json:"tags,omitempty" doc:"Task tags"`
	}
}

type taskOutput struct {
	Body *model.Task
}

type taskGetInput struct {
	ID string `path:"id" doc:"Task ID"`
}

type taskListInput struct {
	Status     string `query:"status" doc:"Filter by task status"`
	Assignee   string `query:"assignee" doc:"Filter by assignee"`
	Creator    string `query:"creator" doc:"Filter by creator"`
	SessionKey string `query:"session_key" doc:"Filter by session key"`
	Limit      int    `query:"limit" doc:"Maximum results (default 50)" minimum:"0"`
	Offset     int    `query:"offset" doc:"Pagination offset" minimum:"0"`
}

type taskListOutput struct {
	Body []*model.Task
}

type taskUpdateInput struct {
	ID       string `path:"id" doc:"Task ID"`
	XAgentID string `header:"X-Agent-ID" doc:"Calling agent identifier"`
	Body     struct {
		Status   *model.TaskStatus `json:"status,omitempty" doc:"New task status"`
		Assignee *string           `json:"assignee,omitempty" doc:"New assignee"`
		Note     *string           `json:"note,omitempty" doc:"Append a note"`
	}
}

type taskDeleteInput struct {
	ID string `path:"id" doc:"Task ID"`
}

type taskDeleteOutput struct{}

// --- Handlers ---

func (a *API) taskCreate(ctx context.Context, input *taskCreateInput) (*taskOutput, error) {
	t := &model.Task{
		Title:          input.Body.Title,
		Description:    input.Body.Description,
		Priority:       input.Body.Priority,
		Tags:           input.Body.Tags,
		Creator:        input.XAgentID,
		SessionContext: model.SessionFromCtx(ctx),
	}
	result, err := a.store.CreateTask(ctx, t)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to create task")
	}
	return &taskOutput{Body: result}, nil
}

func (a *API) taskGet(ctx context.Context, input *taskGetInput) (*taskOutput, error) {
	t, err := a.store.GetTask(ctx, input.ID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("task not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get task")
	}
	return &taskOutput{Body: t}, nil
}

func (a *API) taskList(ctx context.Context, input *taskListInput) (*taskListOutput, error) {
	f := model.TaskFilter{
		Status:     input.Status,
		Assignee:   input.Assignee,
		Creator:    input.Creator,
		SessionKey: input.SessionKey,
		Limit:      input.Limit,
		Offset:     input.Offset,
	}
	tasks, err := a.store.ListTasks(ctx, f)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list tasks")
	}
	if tasks == nil {
		tasks = []*model.Task{}
	}
	return &taskListOutput{Body: tasks}, nil
}

func (a *API) taskUpdate(ctx context.Context, input *taskUpdateInput) (*taskOutput, error) {
	upd := model.TaskUpdate{
		Status:         input.Body.Status,
		Assignee:       input.Body.Assignee,
		Note:           input.Body.Note,
		AgentID:        input.XAgentID,
		SessionContext: model.SessionFromCtx(ctx),
	}
	result, err := a.store.UpdateTask(ctx, input.ID, upd)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("task not found")
	}
	if errors.Is(err, model.ErrInvalidTransition) {
		return nil, huma.Error422UnprocessableEntity("the requested status transition is not allowed")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to update task")
	}
	return &taskOutput{Body: result}, nil
}

func (a *API) taskDelete(ctx context.Context, input *taskDeleteInput) (*taskDeleteOutput, error) {
	err := a.store.DeleteTask(ctx, input.ID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, huma.Error404NotFound("task not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to delete task")
	}
	return &taskDeleteOutput{}, nil
}

// --- Registration ---

func registerTasks(a *API, api huma.API) {
	huma.Register(api, huma.Operation{
		Method:        http.MethodPost,
		Path:          "/api/v1/tasks",
		Summary:       "Create a task",
		Description:   "Create a new task. The calling agent becomes the creator.",
		Tags:          []string{"Tasks"},
		OperationID:   "create-task",
		DefaultStatus: http.StatusCreated,
	}, a.taskCreate)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/tasks",
		Summary:     "List tasks",
		Description: "Return tasks, optionally filtered by status, assignee, or creator.",
		Tags:        []string{"Tasks"},
		OperationID: "list-tasks",
	}, a.taskList)

	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/api/v1/tasks/{id}",
		Summary:     "Get a task",
		Description: "Retrieve a single task by ID.",
		Tags:        []string{"Tasks"},
		OperationID: "get-task",
	}, a.taskGet)

	huma.Register(api, huma.Operation{
		Method:      http.MethodPatch,
		Path:        "/api/v1/tasks/{id}",
		Summary:     "Update a task",
		Description: "Update task status, assignee, or append a note. Status changes are validated against the task state machine.",
		Tags:        []string{"Tasks"},
		OperationID: "update-task",
	}, a.taskUpdate)

	huma.Register(api, huma.Operation{
		Method:        http.MethodDelete,
		Path:          "/api/v1/tasks/{id}",
		Summary:       "Delete a task",
		Description:   "Remove a task and all its notes.",
		Tags:          []string{"Tasks"},
		OperationID:   "delete-task",
		DefaultStatus: http.StatusNoContent,
	}, a.taskDelete)
}
