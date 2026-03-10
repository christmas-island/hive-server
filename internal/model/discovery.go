package model

import (
	"encoding/json"
	"time"
)

// DiscoveryAgent is an agent entry in the discovery registry.
type DiscoveryAgent struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	DiscordUserID string          `json:"discord_user_id,omitempty"`
	HomeChannel   string          `json:"home_channel,omitempty"`
	Capabilities  json.RawMessage `json:"capabilities,omitempty"`
	Status        string          `json:"status,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// DiscoveryChannel is a channel entry in the discovery registry.
type DiscoveryChannel struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	DiscordChannelID string          `json:"discord_channel_id,omitempty"`
	Purpose          string          `json:"purpose,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// DiscoveryRole is a role entry in the discovery registry.
type DiscoveryRole struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	DiscordRoleID string          `json:"discord_role_id,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// RoutingInfo describes how to reach an agent.
type RoutingInfo struct {
	Mention          string `json:"mention"`
	HomeChannel      string `json:"home_channel"`
	SessionKeyFormat string `json:"session_key_format"`
}
