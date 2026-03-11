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
	ID             string            `json:"id"`
	Type           ClaimType         `json:"type"`
	Resource       string            `json:"resource"`
	AgentID        string            `json:"agent_id"`
	Status         ClaimStatus       `json:"status"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	SessionContext `json:",inline"`
	ClaimedAt      time.Time `json:"claimed_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ClaimFilter holds optional filters for listing claims.
type ClaimFilter struct {
	Type       string
	AgentID    string
	Resource   string
	Status     string
	SessionKey string
	Limit      int
	Offset     int
}

// ClaimWaiter represents an agent waiting in the claim queue for a resource.
type ClaimWaiter struct {
	ID           string            `json:"id"`
	Resource     string            `json:"resource"`
	AgentID      string            `json:"agent_id"`
	Type         ClaimType         `json:"type"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	SessionContext `json:",inline"`
	ExpiresInSec int               `json:"expires_in_sec"`
	QueuedAt     time.Time         `json:"queued_at"`
}

// ClaimQueueResult is returned when a claim request is queued instead of
// immediately granted (resource already held by another agent).
type ClaimQueueResult struct {
	Queued   bool   `json:"queued"`
	Position int    `json:"position"`
	WaiterID string `json:"waiter_id"`
	Resource string `json:"resource"`
}

// ClaimReleaseResult is returned on a successful release. Next is non-nil
// when there was a waiter in the queue — they have been promoted to holder.
type ClaimReleaseResult struct {
	Released bool         `json:"released"`
	Claim    *Claim       `json:"claim"`
	Next     *ClaimWaiter `json:"next,omitempty"`
}
