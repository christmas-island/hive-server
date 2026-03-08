package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/christmas-island/hive-server/internal/store"
)

// handleTaskCreate handles POST /api/v1/tasks
func (a *API) handleTaskCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Priority    int      `json:"priority"`
		Tags        []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", nil)
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "title is required", nil)
		return
	}

	t := &store.Task{
		Title:       req.Title,
		Description: req.Description,
		Priority:    req.Priority,
		Tags:        req.Tags,
		Creator:     agentID(r),
	}

	result, err := a.store.CreateTask(r.Context(), t)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create task", nil)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

// handleTaskGet handles GET /api/v1/tasks/{id}
func (a *API) handleTaskGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := a.store.GetTask(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "task not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to get task", nil)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleTaskList handles GET /api/v1/tasks
func (a *API) handleTaskList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.TaskFilter{
		Status:   q.Get("status"),
		Assignee: q.Get("assignee"),
		Creator:  q.Get("creator"),
	}
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "limit must be a non-negative integer", nil)
			return
		}
		f.Limit = n
	}
	if o := q.Get("offset"); o != "" {
		n, err := strconv.Atoi(o)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "offset must be a non-negative integer", nil)
			return
		}
		f.Offset = n
	}

	tasks, err := a.store.ListTasks(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list tasks", nil)
		return
	}
	if tasks == nil {
		tasks = []*store.Task{}
	}
	writeJSON(w, http.StatusOK, tasks)
}

// handleTaskUpdate handles PATCH /api/v1/tasks/{id}
func (a *API) handleTaskUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Status   *string `json:"status"`
		Assignee *string `json:"assignee"`
		Note     *string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", nil)
		return
	}

	upd := store.TaskUpdate{
		Assignee: req.Assignee,
		Note:     req.Note,
		AgentID:  agentID(r),
	}
	if req.Status != nil {
		s := store.TaskStatus(*req.Status)
		upd.Status = &s
	}

	result, err := a.store.UpdateTask(r.Context(), id, upd)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "task not found", nil)
		return
	}
	if errors.Is(err, store.ErrInvalidTransition) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_transition",
			"the requested status transition is not allowed", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update task", nil)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleTaskDelete handles DELETE /api/v1/tasks/{id}
func (a *API) handleTaskDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := a.store.DeleteTask(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "task not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete task", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
