// Package handlers implements the hive-server REST API.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/christmas-island/hive-server/internal/store"
)

// Store is the interface used by handlers (allows mocking in tests).
type Store interface {
	// Memory
	UpsertMemory(ctx context.Context, entry *store.MemoryEntry) (*store.MemoryEntry, error)
	GetMemory(ctx context.Context, key string) (*store.MemoryEntry, error)
	ListMemory(ctx context.Context, f store.MemoryFilter) ([]*store.MemoryEntry, error)
	DeleteMemory(ctx context.Context, key string) error
	// Tasks
	CreateTask(ctx context.Context, t *store.Task) (*store.Task, error)
	GetTask(ctx context.Context, id string) (*store.Task, error)
	ListTasks(ctx context.Context, f store.TaskFilter) ([]*store.Task, error)
	UpdateTask(ctx context.Context, id string, upd store.TaskUpdate) (*store.Task, error)
	DeleteTask(ctx context.Context, id string) error
	// Agents
	Heartbeat(ctx context.Context, id string, capabilities []string, status store.AgentStatus) (*store.Agent, error)
	GetAgent(ctx context.Context, id string) (*store.Agent, error)
	ListAgents(ctx context.Context) ([]*store.Agent, error)
}

// API holds dependencies for all handlers.
type API struct {
	store Store
	token string // HIVE_TOKEN for Bearer auth
}

// New creates a new API and returns a mounted chi router.
func New(s Store, token string) http.Handler {
	a := &API{store: s, token: token}
	return a.routes()
}

// routes builds and returns the full chi router.
func (a *API) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(a.authMiddleware)

	r.Route("/api/v1", func(r chi.Router) {
		// Memory
		r.Post("/memory", a.handleMemoryUpsert)
		r.Get("/memory", a.handleMemoryList)
		r.Get("/memory/{key}", a.handleMemoryGet)
		r.Delete("/memory/{key}", a.handleMemoryDelete)

		// Tasks
		r.Post("/tasks", a.handleTaskCreate)
		r.Get("/tasks", a.handleTaskList)
		r.Get("/tasks/{id}", a.handleTaskGet)
		r.Patch("/tasks/{id}", a.handleTaskUpdate)
		r.Delete("/tasks/{id}", a.handleTaskDelete)

		// Agents
		r.Post("/agents/{id}/heartbeat", a.handleAgentHeartbeat)
		r.Get("/agents", a.handleAgentList)
		r.Get("/agents/{id}", a.handleAgentGet)
	})

	return r
}

// ctxKey is the context key type for handler values.
type ctxKey string

const ctxKeyAgentID ctxKey = "agent_id"

// agentID extracts the X-Agent-ID from the request context.
func agentID(r *http.Request) string {
	if v, ok := r.Context().Value(ctxKeyAgentID).(string); ok {
		return v
	}
	return ""
}

// authMiddleware validates the Bearer token and extracts X-Agent-ID.
func (a *API) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If no token configured, skip auth (useful for local dev).
		if a.token != "" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+a.token {
				writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token", nil)
				return
			}
		}

		// Extract and inject agent ID into context.
		aid := r.Header.Get("X-Agent-ID")
		ctx := r.Context()
		if aid != "" {
			ctx = context.WithValue(ctx, ctxKeyAgentID, aid)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// writeJSON writes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errorResponse is the standard error body.
type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// writeError writes a standard JSON error response.
func writeError(w http.ResponseWriter, status int, code, message string, details any) {
	writeJSON(w, status, errorResponse{Error: code, Message: message, Details: details})
}
