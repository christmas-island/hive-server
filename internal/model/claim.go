package model

import "time"

// ClaimType represents the kind of resource being claimed.
type ClaimType string

const (
	ClaimTypeIssue  ClaimType = "issue"
	ClaimTypeReview ClaimType = "review"
	ClaimTypeConch  ClaimType = "conch"
)

// ClaimStatus represents the lifecycle state of a claim.
type ClaimStatus string

const (
	ClaimStatusActive   ClaimStatus = "active"
	ClaimStatusExpired  ClaimStatus = "expired"
	ClaimStatusReleased ClaimStatus = "released"
)

// Claim represents an agent's exclusive hold on a resource.
type Claim struct {
	ID        string            `json:"id"`
	Type      ClaimType         `json:"type"`
	Resource  string            `json:"resource"`
	AgentID   string            `json:"agent_id"`
	Status    ClaimStatus       `json:"status"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	ClaimedAt time.Time         `json:"claimed_at"`
	ExpiresAt time.Time         `json:"expires_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// ClaimFilter holds optional filters for listing claims.
type ClaimFilter struct {
	Type     string
	AgentID  string
	Resource string
	Status   string
	Limit    int
	Offset   int
}
