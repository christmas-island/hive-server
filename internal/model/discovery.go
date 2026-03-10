package model

import "time"

// DiscoveryChannel holds metadata about a Discord channel.
type DiscoveryChannel struct {
	ID        string    `json:"id"`                   // slug name, e.g. "allclaws"
	Name      string    `json:"name"`                 // display name
	DiscordID string    `json:"discord_id,omitempty"` // Discord channel snowflake
	Purpose   string    `json:"purpose,omitempty"`
	Category  string    `json:"category,omitempty"` // parent category name
	Members   []string  `json:"members,omitempty"`  // agent IDs with access (JSONB)
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DiscoveryRole holds metadata about a Discord role.
type DiscoveryRole struct {
	ID        string    `json:"id"`                   // slug name
	Name      string    `json:"name"`                 // display name
	DiscordID string    `json:"discord_id,omitempty"` // Discord role snowflake
	Members   []string  `json:"members,omitempty"`    // agent IDs (JSONB)
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DiscoveryAgentMeta is extended metadata stored alongside the agent heartbeat record.
// It's returned as part of the discovery agent response.
type DiscoveryAgentMeta struct {
	DiscordUserID string   `json:"discord_user_id,omitempty"`
	HomeChannel   string   `json:"home_channel,omitempty"`   // channel ID slug
	MentionFormat string   `json:"mention_format,omitempty"` // e.g. "@SmokeyClaw"
	Channels      []string `json:"channels,omitempty"`       // channel IDs the agent uses
}

// DiscoveryAgent is the full discovery view of an agent (presence + metadata).
type DiscoveryAgent struct {
	*Agent
	*DiscoveryAgentMeta
}

// DiscoveryRouting describes how to reach an agent.
type DiscoveryRouting struct {
	AgentID       string   `json:"agent_id"`
	MentionFormat string   `json:"mention_format,omitempty"`
	HomeChannel   string   `json:"home_channel,omitempty"`
	Channels      []string `json:"channels,omitempty"`
}
