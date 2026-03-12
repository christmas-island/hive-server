package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/christmas-island/hive-server/internal/handlers"
	"github.com/christmas-island/hive-server/internal/log"
	"github.com/christmas-island/hive-server/internal/relay"
	"github.com/christmas-island/hive-server/internal/store"
	"github.com/christmas-island/hive-server/internal/ui"
	"github.com/christmas-island/hive-server/internal/webhook"
)

// Server manages the HTTP server lifecycle.
type Server struct {
	config Config
	store  *store.Store
	srv    *http.Server
}

// New creates a new Server with the given configuration.
func New(cfg Config) *Server {
	return &Server{
		config: cfg,
	}
}

// claimExpirer is satisfied by any type that can expire old claims.
type claimExpirer interface {
	ExpireOldClaims(ctx context.Context) (int64, error)
}

// claimExpiryInterval is how often the background goroutine sweeps for expired claims.
// It is a variable (not const) so tests can override it for fast-tick scenarios.
var claimExpiryInterval = time.Minute

// todoPruner is satisfied by any type that can prune completed todos.
type todoPruner interface {
	PruneDoneTodos(ctx context.Context, agentID string, olderThan time.Duration) (int64, error)
}

// todoPruneInterval is how often the background goroutine prunes completed todos.
var todoPruneInterval = time.Hour

// runClaimExpiry sweeps for and expires stale active claims on a fixed interval.
// It runs until ctx is cancelled.
func runClaimExpiry(ctx context.Context, ce claimExpirer) {
	ticker := time.NewTicker(claimExpiryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := ce.ExpireOldClaims(ctx)
			if err != nil {
				log.Error("expire claims: ", err)
			} else if n > 0 {
				log.Info(fmt.Sprintf("expired %d stale claim(s)", n))
			}
		}
	}
}

// runTodoPrune periodically prunes completed todos older than 24 hours.
// It prunes across all agents by passing an empty agent ID with a 24h threshold.
func runTodoPrune(ctx context.Context, tp todoPruner) {
	ticker := time.NewTicker(todoPruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := tp.PruneDoneTodos(ctx, "", 24*time.Hour)
			if err != nil {
				log.Error("prune todos: ", err)
			} else if n > 0 {
				log.Info(fmt.Sprintf("pruned %d completed todo(s)", n))
			}
		}
	}
}

// buildMux creates the top-level HTTP mux with health probes, version endpoint,
// the API handler, and the GitHub webhook endpoint.
func buildMux(st *store.Store, token string, rc *relay.Client, webhookSecret string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /ready", handleReady)
	mux.HandleFunc("GET /version", handleVersion)
	mux.Handle("GET /healthz", healthzHandler(st))

	// GitHub webhook endpoint — bypasses Bearer auth (uses HMAC signature validation).
	wh := webhook.New(webhookSecret, st)
	mux.Handle("POST /api/v1/webhooks/github", wh)

	// Web UI — with same auth middleware as API
	uiHandler := ui.New(st, token)
	mux.Handle("/ui/", http.StripPrefix("/ui", uiHandler.Routes()))

	mux.Handle("/", handlers.New(st, token, rc))

	// Wrap the entire mux with version header middleware
	return versionHeaderMiddleware(mux)
}

// logVersionInfo logs the build-time version metadata at startup.
func logVersionInfo() {
	vi := GetVersionInfo()
	log.Info(fmt.Sprintf("hive-server version=%s commit=%s date=%s", vi.Version, vi.Commit, vi.Date))
}

// Run starts the HTTP server and blocks until the context is cancelled.
// It handles store initialization, server lifecycle, and graceful shutdown.
func (s *Server) Run(ctx context.Context) error {
	// Open CockroachDB/PostgreSQL store.
	log.Info("connecting to database: ", s.config.DatabaseURL)
	store, err := store.New(s.config.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	s.store = store
	defer func() {
		if err := s.store.Close(); err != nil {
			log.Error("store close: ", err)
		}
	}()

	// Start background claim expiry sweep.
	go runClaimExpiry(ctx, s.store)

	// Start background todo pruner.
	go runTodoPrune(ctx, s.store)

	// Create relay client (no-op if URL is empty).
	rc := relay.New(s.config.OnlyClawsURL, s.config.OnlyClawsToken)

	// Build top-level mux: health probes bypass auth, everything else goes to
	// the API handler (which owns auth middleware and Huma routing).
	handler := buildMux(s.store, s.config.Token, rc, s.config.GitHubWebhookSecret)

	s.srv = &http.Server{
		Addr:         s.config.BindAddr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown on context cancellation.
	go func() {
		<-ctx.Done()
		log.Info("shutting down HTTP server")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(shutCtx); err != nil {
			log.Error("shutdown error: ", err)
		}
	}()

	logVersionInfo()
	log.Info("HTTP server starting on ", s.config.BindAddr)
	if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen: %w", err)
	}
	return nil
}
