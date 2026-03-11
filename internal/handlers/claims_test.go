package handlers_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/christmas-island/hive-server/internal/model"
)

func TestCreateClaim(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	resp := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":       "issue",
		"resource":   "ops#79",
		"expires_in": "2h",
		"metadata":   map[string]string{"pr": "123"},
	}, testToken, testAgent)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var claim model.Claim
	decodeJSON(t, resp, &claim)

	if claim.ID == "" {
		t.Error("ID is empty")
	}
	if claim.Type != model.ClaimTypeIssue {
		t.Errorf("Type = %q, want issue", claim.Type)
	}
	if claim.Resource != "ops#79" {
		t.Errorf("Resource = %q, want ops#79", claim.Resource)
	}
	if claim.AgentID != testAgent {
		t.Errorf("AgentID = %q, want %q", claim.AgentID, testAgent)
	}
	if claim.Status != model.ClaimStatusActive {
		t.Errorf("Status = %q, want active", claim.Status)
	}
	if claim.Metadata["pr"] != "123" {
		t.Errorf("Metadata[pr] = %q, want 123", claim.Metadata["pr"])
	}
}

func TestCreateClaim_MissingAgentID(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	resp := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "issue",
		"resource": "ops#79",
	}, testToken, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestCreateClaim_Conflict(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	// First claim succeeds.
	r1 := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "issue",
		"resource": "ops#79",
	}, testToken, testAgent)
	if r1.StatusCode != http.StatusCreated {
		t.Fatalf("first claim status = %d, want 201", r1.StatusCode)
	}
	r1.Body.Close()

	// Second claim on same resource should be queued (202 Accepted) instead of 409.
	r2 := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "issue",
		"resource": "ops#79",
	}, testToken, "other-agent")
	defer r2.Body.Close()
	if r2.StatusCode != http.StatusAccepted {
		t.Errorf("second claim status = %d, want 202 (queued)", r2.StatusCode)
	}
	var queuedBody struct {
		Queued   bool   `json:"queued"`
		Position int    `json:"position"`
		WaiterID string `json:"waiter_id"`
	}
	decodeJSON(t, r2, &queuedBody)
	if !queuedBody.Queued {
		t.Errorf("queued = false, want true")
	}
	if queuedBody.Position < 1 {
		t.Errorf("position = %d, want >= 1", queuedBody.Position)
	}
}

func TestGetClaim(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	// Create.
	r1 := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "review",
		"resource": "hive-plugin#3",
	}, testToken, testAgent)
	var created model.Claim
	decodeJSON(t, r1, &created)

	// Get.
	resp := request(t, srv, http.MethodGet, "/api/v1/claims/"+created.ID, nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got model.Claim
	decodeJSON(t, resp, &got)
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
}

func TestGetClaim_NotFound(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)
	resp := request(t, srv, http.MethodGet, "/api/v1/claims/no-such-id", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestListClaims(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	for _, res := range []string{"a#1", "a#2", "a#3"} {
		request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
			"type":     "issue",
			"resource": res,
		}, testToken, testAgent).Body.Close()
	}

	resp := request(t, srv, http.MethodGet, "/api/v1/claims", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var claims []model.Claim
	decodeJSON(t, resp, &claims)
	if len(claims) != 3 {
		t.Errorf("len = %d, want 3", len(claims))
	}
}

func TestListClaims_FilterStatus(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	// Create and release one claim.
	r1 := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "issue",
		"resource": "b#1",
	}, testToken, testAgent)
	var c1 model.Claim
	decodeJSON(t, r1, &c1)
	request(t, srv, http.MethodDelete, "/api/v1/claims/"+c1.ID, nil, testToken, testAgent).Body.Close()

	// Create another active claim.
	request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "issue",
		"resource": "b#2",
	}, testToken, testAgent).Body.Close()

	resp := request(t, srv, http.MethodGet, "/api/v1/claims?status=active", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var claims []model.Claim
	decodeJSON(t, resp, &claims)
	if len(claims) != 1 {
		t.Errorf("active len = %d, want 1", len(claims))
	}
}

func TestReleaseClaim(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	r1 := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "conch",
		"resource": "#hive",
	}, testToken, testAgent)
	var created model.Claim
	decodeJSON(t, r1, &created)

	resp := request(t, srv, http.MethodDelete, "/api/v1/claims/"+created.ID, nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var result model.ClaimReleaseResult
	decodeJSON(t, resp, &result)
	if !result.Released {
		t.Errorf("released = false, want true")
	}
	if result.Claim == nil {
		t.Fatal("result.Claim is nil")
	}
	if result.Claim.Status != model.ClaimStatusReleased {
		t.Errorf("Status = %q, want released", result.Claim.Status)
	}
	if result.Next != nil {
		t.Errorf("expected no next waiter (empty queue), got %+v", result.Next)
	}
}

func TestReleaseClaim_NotFound(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)
	resp := request(t, srv, http.MethodDelete, "/api/v1/claims/ghost", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestRenewClaim(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	r1 := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":       "issue",
		"resource":   "c#1",
		"expires_in": "30m",
	}, testToken, testAgent)
	var created model.Claim
	decodeJSON(t, r1, &created)

	resp := request(t, srv, http.MethodPatch, "/api/v1/claims/"+created.ID, map[string]any{
		"expires_in": "4h",
	}, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var renewed model.Claim
	decodeJSON(t, resp, &renewed)
	if renewed.Status != model.ClaimStatusActive {
		t.Errorf("Status = %q, want active", renewed.Status)
	}
	if !renewed.ExpiresAt.After(created.ExpiresAt) {
		t.Error("ExpiresAt should have been extended")
	}
}

func TestRenewClaim_NotFound(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)
	resp := request(t, srv, http.MethodPatch, "/api/v1/claims/ghost", map[string]any{
		"expires_in": "1h",
	}, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestRenewClaim_AlreadyReleased(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	// Create and release.
	r1 := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "issue",
		"resource": "d#1",
	}, testToken, testAgent)
	var created model.Claim
	decodeJSON(t, r1, &created)
	request(t, srv, http.MethodDelete, "/api/v1/claims/"+created.ID, nil, testToken, testAgent).Body.Close()

	// Renew should fail.
	resp := request(t, srv, http.MethodPatch, "/api/v1/claims/"+created.ID, map[string]any{
		"expires_in": "1h",
	}, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestCreateClaim_StoreError(t *testing.T) {
	srv, ms := newMockServerWithStore(t, testToken)
	ms.injectErr("CreateClaim", errTest)
	resp := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type": "issue", "resource": "res", "expires_in": "1h",
	}, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Errorf("expected error status, got %d", resp.StatusCode)
	}
}

func TestGetClaim_StoreError(t *testing.T) {
	srv, ms := newMockServerWithStore(t, testToken)
	ms.injectErr("GetClaim", errTest)
	resp := request(t, srv, http.MethodGet, "/api/v1/claims/no-such", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Errorf("expected error status, got %d", resp.StatusCode)
	}
}

func TestListClaims_StoreError(t *testing.T) {
	srv, ms := newMockServerWithStore(t, testToken)
	ms.injectErr("ListClaims", errTest)
	resp := request(t, srv, http.MethodGet, "/api/v1/claims", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Errorf("expected error status, got %d", resp.StatusCode)
	}
}

func TestReleaseClaim_StoreError(t *testing.T) {
	srv, ms := newMockServerWithStore(t, testToken)
	ms.injectErr("ReleaseClaim", errTest)
	resp := request(t, srv, http.MethodDelete, "/api/v1/claims/no-such", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Errorf("expected error status, got %d", resp.StatusCode)
	}
}

func TestRenewClaim_StoreError(t *testing.T) {
	srv, ms := newMockServerWithStore(t, testToken)
	ms.injectErr("RenewClaim", errTest)
	resp := request(t, srv, http.MethodPost, "/api/v1/claims/no-such/renew", map[string]any{
		"expires_in": "1h",
	}, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Errorf("expected error status, got %d", resp.StatusCode)
	}
}

func TestCreateClaim_Queue(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	// First claim: should be granted (201).
	r1 := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "conch",
		"resource": "#hive",
	}, testToken, testAgent)
	if r1.StatusCode != http.StatusCreated {
		t.Fatalf("first claim status = %d, want 201", r1.StatusCode)
	}
	r1.Body.Close()

	// Second claim: should be queued (202).
	r2 := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "conch",
		"resource": "#hive",
	}, testToken, "second-agent")
	if r2.StatusCode != http.StatusAccepted {
		t.Fatalf("second claim status = %d, want 202", r2.StatusCode)
	}
	var q struct {
		Queued   bool   `json:"queued"`
		Position int    `json:"position"`
		WaiterID string `json:"waiter_id"`
	}
	decodeJSON(t, r2, &q)
	if !q.Queued {
		t.Error("queued = false, want true")
	}
	if q.WaiterID == "" {
		t.Error("waiter_id is empty")
	}
}

func TestReleaseClaim_WithNextWaiter(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	// Holder claims the resource.
	r1 := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "conch",
		"resource": "#onlyclaws",
	}, testToken, testAgent)
	if r1.StatusCode != http.StatusCreated {
		t.Fatalf("first claim: status = %d", r1.StatusCode)
	}
	var holder struct{ ID string `json:"id"` }
	decodeJSON(t, r1, &holder)

	// Second agent queues.
	r2 := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "conch",
		"resource": "#onlyclaws",
	}, testToken, "next-agent")
	if r2.StatusCode != http.StatusAccepted {
		t.Fatalf("second claim: status = %d, want 202", r2.StatusCode)
	}
	r2.Body.Close()

	// Holder releases — response should include next waiter.
	resp := request(t, srv, http.MethodDelete, "/api/v1/claims/"+holder.ID, nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("release status = %d, want 200", resp.StatusCode)
	}
	var result model.ClaimReleaseResult
	decodeJSON(t, resp, &result)
	if !result.Released {
		t.Error("released = false")
	}
	if result.Next == nil {
		t.Error("expected next waiter, got nil")
	}
	if result.Next != nil && result.Next.AgentID != "next-agent" {
		t.Errorf("next.AgentID = %q, want next-agent", result.Next.AgentID)
	}
}

func TestCreateClaim_QueueError(t *testing.T) {
	// When EnqueueClaim fails, handler should return 500.
	srv, ms := newMockServerWithStore(t, testToken)

	// Seed an existing claim so the next one hits conflict → queue path.
	ms.mu.Lock()
	ms.claims["existing-c"] = &model.Claim{
		ID:        "existing-c",
		Type:      model.ClaimTypeConch,
		Resource:  "conch#err-resource",
		AgentID:   "other-agent",
		Status:    model.ClaimStatusActive,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	ms.mu.Unlock()

	ms.injectErr("EnqueueClaim", errTest)
	// Inject conflict for CreateClaim (the mock returns conflict when resource is active).
	// The mock's CreateClaim checks the claims map — we need CreateClaim to return ErrConflict.
	// Since the mock creates a real ID and doesn't check existing, we inject err directly.
	ms.injectErr("CreateClaim", model.ErrConflict)

	resp := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "conch",
		"resource": "conch#err-resource",
	}, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestReleaseClaim_Forbidden(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	// Create a claim as testAgent.
	r1 := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "conch",
		"resource": "#forbidden-test",
	}, testToken, testAgent)
	var holder struct{ ID string `json:"id"` }
	decodeJSON(t, r1, &holder)

	// Try to release as a different agent.
	resp := request(t, srv, http.MethodDelete, "/api/v1/claims/"+holder.ID, nil, testToken, "other-agent")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestRenewClaim_Forbidden(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	r1 := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "review",
		"resource": "pr#renew-forbidden",
	}, testToken, testAgent)
	var holder struct{ ID string `json:"id"` }
	decodeJSON(t, r1, &holder)

	resp := request(t, srv, http.MethodPatch, "/api/v1/claims/"+holder.ID, map[string]any{
		"expires_in": "1h",
	}, testToken, "other-agent")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}
