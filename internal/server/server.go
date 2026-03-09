package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/christmas-island/hive-server/internal/handlers"
	"github.com/christmas-island/hive-server/internal/log"
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

	// Build HTTP handler: chi router handles health probes, OpenAPI docs, and
	// all API endpoints. Health and ready probes are registered without auth.
	h := handlers.New(s.store, s.config.Token)

	s.srv = &http.Server{
		Addr:         s.config.BindAddr,
		Handler:      h,
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
