package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/christmas-island/hive-server/internal/log"
	"github.com/spf13/cobra"
)

// Serve starts the HTTP server.
func Serve() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP server.",
		RunE:  runServe,
	}

	flags := cmd.Flags()
	flags.StringP("bind", "b", "0.0.0.0:8080", "The bind address for the HTTP server.")

	return cmd
}

func runServe(cmd *cobra.Command, args []string) error {
	flags := cmd.Flags()
	bind, err := flags.GetString("bind")
	if err != nil {
		return err
	}

	mux := http.NewServeMux()

	// Health and readiness probes
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/ready", handleReady)

	log.Info("HTTP server starting on ", bind)
	return http.ListenAndServe(bind, mux) //nolint:gosec
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

func init() {
	_ = fmt.Sprint // keep fmt import for future use
}
