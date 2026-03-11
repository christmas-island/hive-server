package model

import "time"

// MemoryEntry represents a shared memory entry.
type MemoryEntry struct {
	Key            string    `json:"key"`
	Value          string    `json:"value"`
	AgentID        string    `json:"agent_id"`
	Tags           []string  `json:"tags"`
	Version        int64     `json:"version"`
	SessionContext `json:",inline"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// MemoryFilter holds optional filter parameters for listing memory entries.
type MemoryFilter struct {
	Tag        string
	Agent      string
	Prefix     string
	SessionKey string
	Limit      int
	Offset     int
}
