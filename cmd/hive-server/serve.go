package main

import (
	"net"
	"os"

	"github.com/christmas-island/hive-server/internal/server"
	"github.com/spf13/cobra"
)

const (
	defaultDatabaseURL = "postgresql://root@localhost:26257/hive?sslmode=disable"
	defaultPort        = "8080"
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
	flags.String("database-url", "", "PostgreSQL/CockroachDB connection URL (overrides DATABASE_URL env var).")

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

	// Resolve database URL: flag > DATABASE_URL env > default.
	dbURL, _ := cmd.Flags().GetString("database-url")
	if dbURL == "" {
		dbURL = os.Getenv("DATABASE_URL")
	}
	if dbURL == "" {
		dbURL = defaultDatabaseURL
	}

	// Read auth token from environment.
	token := os.Getenv("HIVE_TOKEN")

	// Read only-claws relay configuration from environment.
	onlyClawsURL := os.Getenv("ONLY_CLAWS_URL")
	onlyClawsToken := os.Getenv("ONLY_CLAWS_TOKEN")

	// Read GitHub webhook secret from environment.
	githubWebhookSecret := os.Getenv("GITHUB_WEBHOOK_SECRET")

	// Create server configuration.
	cfg := server.Config{
		BindAddr:            bind,
		DatabaseURL:         dbURL,
		Token:               token,
		OnlyClawsURL:        onlyClawsURL,
		OnlyClawsToken:      onlyClawsToken,
		GitHubWebhookSecret: githubWebhookSecret,
	}

	// Create and run server.
	s := server.New(cfg)
	return s.Run(cmd.Context())
}
