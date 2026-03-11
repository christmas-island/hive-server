// Package handlers implements the hive-server REST API using Huma v2.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/christmas-island/hive-server/internal/model"
	"github.com/christmas-island/hive-server/internal/relay"
)

// Store is the interface used by handlers (allows mocking in tests).
type Store interface {
	// Memory
	UpsertMemory(ctx context.Context, entry *model.MemoryEntry) (*model.MemoryEntry, error)
	GetMemory(ctx context.Context, key string) (*model.MemoryEntry, error)
	ListMemory(ctx context.Context, f model.MemoryFilter) ([]*model.MemoryEntry, error)
	DeleteMemory(ctx context.Context, key string) error
	// Tasks
	CreateTask(ctx context.Context, t *model.Task) (*model.Task, error)
	GetTask(ctx context.Context, id string) (*model.Task, error)
	ListTasks(ctx context.Context, f model.TaskFilter) ([]*model.Task, error)
	UpdateTask(ctx context.Context, id string, upd model.TaskUpdate) (*model.Task, error)
	DeleteTask(ctx context.Context, id string) error
	// Agents
	Heartbeat(ctx context.Context, id string, capabilities []string, status model.AgentStatus) (*model.Agent, error)
	GetAgent(ctx context.Context, id string) (*model.Agent, error)
	ListAgents(ctx context.Context) ([]*model.Agent, error)
	// Claims
	CreateClaim(ctx context.Context, c *model.Claim) (*model.Claim, error)
	EnqueueClaim(ctx context.Context, w *model.ClaimWaiter) (*model.ClaimWaiter, int, error)
	GetClaim(ctx context.Context, id string) (*model.Claim, error)
	ListClaims(ctx context.Context, f model.ClaimFilter) ([]*model.Claim, error)
	ReleaseClaim(ctx context.Context, id string) (*model.ClaimReleaseResult, error)
	RenewClaim(ctx context.Context, id string, expiresAt time.Time) (*model.Claim, error)
	ExpireOldClaims(ctx context.Context) (int64, error)
	QueueDepth(ctx context.Context, resource string) (int, error)
	// Discovery
	UpsertChannel(ctx context.Context, ch *model.DiscoveryChannel) (*model.DiscoveryChannel, error)
	GetChannel(ctx context.Context, id string) (*model.DiscoveryChannel, error)
	ListChannels(ctx context.Context) ([]*model.DiscoveryChannel, error)
	DeleteChannel(ctx context.Context, id string) error
	UpsertRole(ctx context.Context, role *model.DiscoveryRole) (*model.DiscoveryRole, error)
	GetRole(ctx context.Context, id string) (*model.DiscoveryRole, error)
	ListRoles(ctx context.Context) ([]*model.DiscoveryRole, error)
	DeleteRole(ctx context.Context, id string) error
	UpsertAgentMeta(ctx context.Context, id string, meta *model.DiscoveryAgentMeta) (*model.DiscoveryAgent, error)
	GetDiscoveryAgent(ctx context.Context, id string) (*model.DiscoveryAgent, error)
	ListDiscoveryAgents(ctx context.Context) ([]*model.DiscoveryAgent, error)
	// Session capture
	CreateCapturedSession(ctx context.Context, s *model.CapturedSession) (*model.CapturedSession, error)
	GetCapturedSession(ctx context.Context, id string) (*model.CapturedSession, error)
	ListCapturedSessions(ctx context.Context, f model.SessionFilter) ([]*model.CapturedSession, error)
}

// API holds dependencies for all handlers.
type API struct {
	store Store
	token string // HIVE_TOKEN for Bearer auth
	relay *relay.Client
}

// New creates a new API and returns a mounted chi router.
func New(s Store, token string, rc *relay.Client) http.Handler {
	a := &API{store: s, token: token, relay: rc}
	return a.routes()
}

// routes builds and returns the full chi router.
func (a *API) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	// Authenticated API group: auth middleware wraps all Huma operations and
	// the auto-generated OpenAPI docs/schema endpoints.
	r.Group(func(r chi.Router) {
		r.Use(a.authMiddleware)
		r.Use(timingMiddleware)

		config := huma.DefaultConfig("Hive API", "1.0.0")
		config.Info.Description = "Cross-agent memory and task coordination API."

		api := humachi.New(r, config)

		registerMemory(a, api)
		registerTasks(a, api)
		registerAgents(a, api)
		registerDiscovery(a, api)
		registerClaims(a, api)
		registerSessions(a, api)
	})

	return r
}

// ctxKey is the context key type for handler values.
type ctxKey string

const ctxKeyAgentID ctxKey = "agent_id"
const ctxKeySession ctxKey = "session_context"

// authMiddleware validates the Bearer token and extracts X-Agent-ID and session
// context headers into context.
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

		// Extract session context headers.
		sc := model.SessionContext{
			SessionKey:    r.Header.Get("X-Session-Key"),
			SessionID:     r.Header.Get("X-Session-ID"),
			Channel:       r.Header.Get("X-Channel"),
			SenderID:      r.Header.Get("X-Sender-ID"),
			SenderIsOwner: r.Header.Get("X-Sender-Is-Owner") == "true",
			Sandboxed:     r.Header.Get("X-Sandboxed") == "true",
		}
		ctx = context.WithValue(ctx, ctxKeySession, sc)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// sessionFromCtx extracts the SessionContext from the request context.
func sessionFromCtx(ctx context.Context) model.SessionContext {
	sc, _ := ctx.Value(ctxKeySession).(model.SessionContext)
	return sc
}
