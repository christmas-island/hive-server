package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/christmas-island/hive-server/internal/handlers"
	"github.com/christmas-island/hive-server/internal/log"
	"github.com/christmas-island/hive-server/internal/store"
	"github.com/spf13/cobra"
)

const (
	defaultDBPath = "/data/hive.db"
	defaultPort   = "8080"
)

// Serve starts the HTTP server.
func Serve() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP server.",
		RunE:  runServe,
	}

	flags := cmd.Flags()
	flags.StringP("bind", "b", "", "The bind address for the HTTP server (overrides PORT env var).")

	return cmd
}

func runServe(cmd *cobra.Command, _ []string) error {
	// Resolve bind address: flag > PORT env > default.
	bind, _ := cmd.Flags().GetString("bind")
	if bind == "" {
		port := os.Getenv("PORT")
		if port == "" {
			port = defaultPort
		}
		bind = net.JoinHostPort("0.0.0.0", port)
	}

	// Read config from environment.
	token := os.Getenv("HIVE_TOKEN")
	dbPath := os.Getenv("HIVE_DB_PATH")
	if dbPath == "" {
		dbPath = defaultDBPath
	}

	// Open SQLite store.
	log.Info("opening database at ", dbPath)
	s, err := store.New(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			log.Error("store close: ", err)
		}
	}()

	// Build HTTP mux: health probes + API.
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/ready", handleReady)
	mux.Handle("/api/v1/", handlers.New(s, token))

	srv := &http.Server{
		Addr:         bind,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown on context cancellation.
	ctx := cmd.Context()
	go func() {
		<-ctx.Done()
		log.Info("shutting down HTTP server")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Error("shutdown error: ", err)
		}
	}()

	log.Info("HTTP server starting on ", bind)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen: %w", err)
	}
	return nil
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleReady(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

func init() {
	_ = fmt.Sprint // keep fmt import for future use
}
