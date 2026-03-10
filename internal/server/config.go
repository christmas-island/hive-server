package server

// Config holds all server configuration.
type Config struct {
	BindAddr       string // HTTP server bind address
	DatabaseURL    string // PostgreSQL/CockroachDB connection URL
	Token          string // HIVE_TOKEN for Bearer auth
	OnlyClawsURL   string // ONLY_CLAWS_URL for relay
	OnlyClawsToken string // ONLY_CLAWS_TOKEN for relay auth
}
