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

// claimExpiryInterval is how often the background goroutine sweeps for expired claims.
const claimExpiryInterval = time.Minute

// runClaimExpiry sweeps for and expires stale active claims on a fixed interval.
// It runs until ctx is cancelled.
func runClaimExpiry(ctx context.Context, st *store.Store) {
	ticker := time.NewTicker(claimExpiryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := st.ExpireOldClaims(ctx)
			if err != nil {
				log.Error("expire claims: ", err)
			} else if n > 0 {
				log.Info(fmt.Sprintf("expired %d stale claim(s)", n))
			}
		}
	}
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

	// Create relay client (no-op if URL is empty).
	rc := relay.New(s.config.OnlyClawsURL, s.config.OnlyClawsToken)

	// Build top-level mux: health probes bypass auth, everything else goes to
	// the API handler (which owns auth middleware and Huma routing).
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /ready", handleReady)
	mux.Handle("GET /healthz", healthzHandler(s.store))
	mux.Handle("/", handlers.New(s.store, s.config.Token, rc))

	s.srv = &http.Server{
		Addr:         s.config.BindAddr,
		Handler:      mux,
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

	log.Info("HTTP server starting on ", s.config.BindAddr)
	if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen: %w", err)
	}
	return nil
}
