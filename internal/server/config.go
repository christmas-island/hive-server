package server

// Config holds all server configuration.
type Config struct {
	BindAddr             string // HTTP server bind address
	DatabaseURL          string // PostgreSQL/CockroachDB connection URL
	Token                string // HIVE_TOKEN for Bearer auth
	OnlyClawsURL         string // ONLY_CLAWS_URL for relay
	OnlyClawsToken       string // ONLY_CLAWS_TOKEN for relay auth
	GitHubWebhookSecret  string // GITHUB_WEBHOOK_SECRET for webhook HMAC validation
}

// VersionInfo holds build-time version metadata.
type VersionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// versionInfo is the package-level version info set at startup.
var versionInfo = VersionInfo{Version: "dev", Commit: "none", Date: "unknown"}

// SetVersionInfo stores build-time version metadata for use by the server.
func SetVersionInfo(version, commit, date string) {
	versionInfo = VersionInfo{Version: version, Commit: commit, Date: date}
}

// GetVersionInfo returns the current version info.
func GetVersionInfo() VersionInfo {
	return versionInfo
}
