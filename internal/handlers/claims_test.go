package handlers_test

import (
	"net/http"
	"testing"

	"github.com/christmas-island/hive-server/internal/model"
)

func TestCreateClaim(t *testing.T) {
	srv := newTestServer(t, testToken)

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
	srv := newTestServer(t, testToken)

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
	srv := newTestServer(t, testToken)

	// First claim succeeds.
	r1 := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "issue",
		"resource": "ops#79",
	}, testToken, testAgent)
	if r1.StatusCode != http.StatusCreated {
		t.Fatalf("first claim status = %d, want 201", r1.StatusCode)
	}
	r1.Body.Close()

	// Second claim on same resource should fail with 409.
	r2 := request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "issue",
		"resource": "ops#79",
	}, testToken, "other-agent")
	defer r2.Body.Close()
	if r2.StatusCode != http.StatusConflict {
		t.Errorf("second claim status = %d, want 409", r2.StatusCode)
	}
}

func TestGetClaim(t *testing.T) {
	srv := newTestServer(t, testToken)

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
	srv := newTestServer(t, testToken)
	resp := request(t, srv, http.MethodGet, "/api/v1/claims/no-such-id", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestListClaims(t *testing.T) {
	srv := newTestServer(t, testToken)

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
	srv := newTestServer(t, testToken)

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
	srv := newTestServer(t, testToken)

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
	var released model.Claim
	decodeJSON(t, resp, &released)
	if released.Status != model.ClaimStatusReleased {
		t.Errorf("Status = %q, want released", released.Status)
	}
}

func TestReleaseClaim_NotFound(t *testing.T) {
	srv := newTestServer(t, testToken)
	resp := request(t, srv, http.MethodDelete, "/api/v1/claims/ghost", nil, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestRenewClaim(t *testing.T) {
	srv := newTestServer(t, testToken)

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
	srv := newTestServer(t, testToken)
	resp := request(t, srv, http.MethodPatch, "/api/v1/claims/ghost", map[string]any{
		"expires_in": "1h",
	}, testToken, testAgent)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestRenewClaim_AlreadyReleased(t *testing.T) {
	srv := newTestServer(t, testToken)

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
