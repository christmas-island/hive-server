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
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	Status            AgentStatus `json:"status"`
	Activity          string      `json:"activity,omitempty"`            // free-text current work description
	Capabilities      []string    `json:"capabilities"`
	LastHeartbeat     time.Time   `json:"last_heartbeat"`
	RegisteredAt      time.Time   `json:"registered_at"`
	HiveLocalVersion  string      `json:"hive_local_version,omitempty"`  // semver reported by hive-local
	HivePluginVersion string      `json:"hive_plugin_version,omitempty"` // semver reported by hive plugin
}
