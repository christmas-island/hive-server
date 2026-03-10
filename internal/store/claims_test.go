//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/christmas-island/hive-server/internal/model"
	"github.com/christmas-island/hive-server/internal/store"
)

func makeClaim(s *store.Store, t *testing.T, resource string) *model.Claim {
	t.Helper()
	claim, err := s.CreateClaim(context.Background(), &model.Claim{
		Type:      model.ClaimTypeIssue,
		Resource:  resource,
		AgentID:   "agent-test",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateClaim(%q): %v", resource, err)
	}
	return claim
}

func TestCreateClaim(t *testing.T) {
	s := newTestStore(t)
	claim := makeClaim(s, t, "ops#79")

	if claim.ID == "" {
		t.Error("expected non-empty ID")
	}
	if claim.Status != model.ClaimStatusActive {
		t.Errorf("Status = %q, want %q", claim.Status, model.ClaimStatusActive)
	}
	if claim.ClaimedAt.IsZero() {
		t.Error("ClaimedAt is zero")
	}
	if claim.ExpiresAt.IsZero() {
		t.Error("ExpiresAt is zero")
	}
}

func TestGetClaim(t *testing.T) {
	s := newTestStore(t)
	created := makeClaim(s, t, "get-me#1")

	got, err := s.GetClaim(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
	if got.Resource != "get-me#1" {
		t.Errorf("Resource = %q, want %q", got.Resource, "get-me#1")
	}
}

func TestGetClaim_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetClaim(context.Background(), "no-such-id")
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListClaims_Filters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	makeClaim(s, t, "list#1")
	makeClaim(s, t, "list#2")
	makeClaim(s, t, "list#3")

	claims, err := s.ListClaims(ctx, model.ClaimFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListClaims: %v", err)
	}
	if len(claims) != 3 {
		t.Errorf("len = %d, want 3", len(claims))
	}

	// Filter by status.
	active, err := s.ListClaims(ctx, model.ClaimFilter{Status: "active"})
	if err != nil {
		t.Fatalf("ListClaims(active): %v", err)
	}
	if len(active) != 3 {
		t.Errorf("active len = %d, want 3", len(active))
	}

	// Filter by agent.
	byAgent, err := s.ListClaims(ctx, model.ClaimFilter{AgentID: "agent-test"})
	if err != nil {
		t.Fatalf("ListClaims(agent): %v", err)
	}
	if len(byAgent) != 3 {
		t.Errorf("agent len = %d, want 3", len(byAgent))
	}
}

func TestReleaseClaim(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	claim := makeClaim(s, t, "release-me#1")
	released, err := s.ReleaseClaim(ctx, claim.ID)
	if err != nil {
		t.Fatalf("ReleaseClaim: %v", err)
	}
	if released.Status != model.ClaimStatusReleased {
		t.Errorf("Status = %q, want released", released.Status)
	}
}

func TestReleaseClaim_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.ReleaseClaim(context.Background(), "no-such-id")
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRenewClaim(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	claim := makeClaim(s, t, "renew-me#1")
	newExpiry := time.Now().UTC().Add(4 * time.Hour)
	renewed, err := s.RenewClaim(ctx, claim.ID, newExpiry)
	if err != nil {
		t.Fatalf("RenewClaim: %v", err)
	}
	if renewed.Status != model.ClaimStatusActive {
		t.Errorf("Status = %q, want active", renewed.Status)
	}
	if renewed.ExpiresAt.Before(claim.ExpiresAt) {
		t.Error("ExpiresAt should have been extended")
	}
}

func TestRenewClaim_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.RenewClaim(context.Background(), "no-such-id", time.Now().Add(time.Hour))
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRenewClaim_AlreadyReleased(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	claim := makeClaim(s, t, "released-renew#1")
	_, err := s.ReleaseClaim(ctx, claim.ID)
	if err != nil {
		t.Fatalf("ReleaseClaim: %v", err)
	}

	_, err = s.RenewClaim(ctx, claim.ID, time.Now().Add(time.Hour))
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestExpireOldClaims(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create a claim that's already expired.
	c, err := s.CreateClaim(ctx, &model.Claim{
		Type:      model.ClaimTypeIssue,
		Resource:  "expired#1",
		AgentID:   "agent-test",
		ExpiresAt: time.Now().UTC().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateClaim: %v", err)
	}

	// Create a claim that's still valid.
	makeClaim(s, t, "still-valid#1")

	count, err := s.ExpireOldClaims(ctx)
	if err != nil {
		t.Fatalf("ExpireOldClaims: %v", err)
	}
	if count != 1 {
		t.Errorf("expired count = %d, want 1", count)
	}

	// Verify the expired claim.
	got, err := s.GetClaim(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	if got.Status != model.ClaimStatusExpired {
		t.Errorf("Status = %q, want expired", got.Status)
	}
}

func TestCreateClaim_ConflictOnActiveResource(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	makeClaim(s, t, "conflict#1")

	// Second claim on same resource should fail.
	_, err := s.CreateClaim(ctx, &model.Claim{
		Type:      model.ClaimTypeIssue,
		Resource:  "conflict#1",
		AgentID:   "other-agent",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err != model.ErrConflict {
		t.Errorf("expected ErrConflict, got %v", err)
	}

	// Release the first claim, then retry.
	claims, _ := s.ListClaims(ctx, model.ClaimFilter{Resource: "conflict#1", Status: "active"})
	if len(claims) != 1 {
		t.Fatalf("expected 1 active claim, got %d", len(claims))
	}
	_, err = s.ReleaseClaim(ctx, claims[0].ID)
	if err != nil {
		t.Fatalf("ReleaseClaim: %v", err)
	}

	// Now creating a new claim should succeed.
	c, err := s.CreateClaim(ctx, &model.Claim{
		Type:      model.ClaimTypeIssue,
		Resource:  "conflict#1",
		AgentID:   "other-agent",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateClaim after release: %v", err)
	}
	if c.Status != model.ClaimStatusActive {
		t.Errorf("Status = %q, want active", c.Status)
	}
}
