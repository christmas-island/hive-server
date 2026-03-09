// Package handlers implements the hive-server REST API using Huma v2.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
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
	// Health
	Ping(ctx context.Context) error
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

	// Health endpoints — no auth required, intentionally outside the API group.
	r.Get("/health", handleHealth)
	r.Get("/ready", handleReady)
	r.Get("/healthz", a.handleHealthz)

	// Authenticated API group: auth middleware wraps all Huma operations and
	// the auto-generated OpenAPI docs/schema endpoints.
	r.Group(func(r chi.Router) {
		r.Use(a.authMiddleware)

		config := huma.DefaultConfig("Hive API", "1.0.0")
		config.Info.Description = "Cross-agent memory and task coordination API."

		api := humachi.New(r, config)

		registerMemory(a, api)
		registerTasks(a, api)
		registerAgents(a, api)
	})

	return r
}

// ctxKey is the context key type for handler values.
type ctxKey string

const ctxKeyAgentID ctxKey = "agent_id"

// authMiddleware validates the Bearer token and extracts X-Agent-ID into context.
func (a *API) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If no token configured, skip auth (local dev).
		if a.token != "" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+a.token {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":   "unauthorized",
					"message": "invalid or missing bearer token",
				})
				return
			}
		}

		// Inject agent ID into context for downstream use.
		aid := r.Header.Get("X-Agent-ID")
		ctx := r.Context()
		if aid != "" {
			ctx = context.WithValue(ctx, ctxKeyAgentID, aid)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// handleHealth handles GET /health.
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleReady handles GET /ready.
func handleReady(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// handleHealthz handles GET /healthz with database connectivity check.
func (a *API) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx := r.Context()
	if err := a.store.Ping(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "unavailable",
			"error":  err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}
