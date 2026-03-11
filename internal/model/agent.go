package model

import "time"

// AgentStatus represents an agent's presence state.
type AgentStatus string

const (
	AgentStatusOnline  AgentStatus = "online"
	AgentStatusIdle    AgentStatus = "idle"
	AgentStatusOffline AgentStatus = "offline"
)

// Agent represents a registered agent.
type Agent struct {
	ID               string      `json:"id"`
	Name             string      `json:"name"`
	Status           AgentStatus `json:"status"`
	Capabilities     []string    `json:"capabilities"`
	LastHeartbeat    time.Time   `json:"last_heartbeat"`
	RegisteredAt     time.Time   `json:"registered_at"`
	HiveLocalVersion string      `json:"hive_local_version,omitempty"` // semver reported by hive-local
}
