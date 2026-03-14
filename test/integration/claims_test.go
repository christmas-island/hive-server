//go:build integration

package integration

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/christmas-island/hive-server/pkg/testharness"
)

// TestClaimAcquireRelease verifies the claim lifecycle:
// acquire (201) → get → release (204) → verify released.
func TestClaimAcquireRelease(t *testing.T) {
	base := testharness.NewTestServer(t)
	cli := newClient(base, "test-token")
	aid := "integ-agent-claims"
	resource := "integ-test-resource#1"

	// Acquire claim.
	claimBody := map[string]any{
		"type":       "issue",
		"resource":   resource,
		"expires_in": "5m",
		"metadata": map[string]string{
			"purpose": "integration test",
		},
	}
	status, body, err := cli.do("POST", "/api/v1/claims", claimBody, agentID(aid))
	if err != nil {
		t.Fatalf("create claim: %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("create claim: want 201, got %d (%s)", status, body)
	}

	type claimResp struct {
		ID       string            `json:"id"`
		Type     string            `json:"type"`
		Resource string            `json:"resource"`
		AgentID  string            `json:"agent_id"`
		Status   string            `json:"status"`
		Metadata map[string]string `json:"metadata"`
		Queued   *bool             `json:"queued,omitempty"`
	}

	claim, err := decode[claimResp](body)
	if err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if claim.ID == "" {
		t.Fatal("claim ID is empty")
	}
	if claim.Resource != resource {
		t.Errorf("resource: want %q, got %q", resource, claim.Resource)
	}
	if claim.AgentID != aid {
		t.Errorf("agent_id: want %q, got %q", aid, claim.AgentID)
	}
	if claim.Status != "active" {
		t.Errorf("status: want active, got %s", claim.Status)
	}
	claimID := claim.ID
	t.Logf("acquired claim %s on %s", claimID, resource)

	// Get claim.
	status, body, err = cli.do("GET", "/api/v1/claims/"+claimID, nil)
	if err != nil {
		t.Fatalf("get claim: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("get claim: want 200, got %d (%s)", status, body)
	}
	got, err := decode[claimResp](body)
	if err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got.Status != "active" {
		t.Errorf("get status: want active, got %s", got.Status)
	}

	// List claims — should find ours.
	status, body, err = cli.do("GET", "/api/v1/claims?resource="+url.QueryEscape(resource), nil)
	if err != nil {
		t.Fatalf("list claims: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("list claims: want 200, got %d (%s)", status, body)
	}
	claims, err := decode[[]claimResp](body)
	if err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, c := range claims {
		if c.ID == claimID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("claim %s not found in list (%d claims)", claimID, len(claims))
	}

	// Release claim — DELETE returns 200 with release result.
	status, body, err = cli.do("DELETE", "/api/v1/claims/"+claimID, nil)
	if err != nil {
		t.Fatalf("release claim: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("release claim: want 200, got %d", status)
	}
	t.Logf("released claim %s", claimID)

	// Verify claim is released via GET.
	status, body, err = cli.do("GET", "/api/v1/claims/"+claimID, nil)
	if err != nil {
		t.Fatalf("get released: %v", err)
	}
	if status == http.StatusOK {
		released, err := decode[claimResp](body)
		if err != nil {
			t.Fatalf("decode released: %v", err)
		}
		if released.Status != "released" {
			t.Errorf("released status: want released, got %s", released.Status)
		}
	} else if status != http.StatusNotFound {
		// Either 200 (released) or 404 (pruned) is acceptable.
		t.Errorf("get released: want 200 or 404, got %d", status)
	}
}

// TestClaimContention verifies that a second agent trying to claim the same
// resource gets queued (202) rather than granted (201).
func TestClaimContention(t *testing.T) {
	base := testharness.NewTestServer(t)
	cli := newClient(base, "test-token")
	resource := "integ-contention-resource"

	// Agent A acquires.
	claimBody := map[string]any{
		"type":       "conch",
		"resource":   resource,
		"expires_in": "10m",
	}
	status, body, err := cli.do("POST", "/api/v1/claims", claimBody, agentID("agent-a"))
	if err != nil {
		t.Fatalf("agent-a create: %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("agent-a: want 201, got %d (%s)", status, body)
	}

	type claimResp struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Queued   *bool  `json:"queued,omitempty"`
		Position *int   `json:"position,omitempty"`
		WaiterID string `json:"waiter_id,omitempty"`
	}

	claimA, _ := decode[claimResp](body)
	t.Logf("agent-a acquired claim %s", claimA.ID)

	// Agent B tries the same resource — should be queued.
	status, body, err = cli.do("POST", "/api/v1/claims", claimBody, agentID("agent-b"))
	if err != nil {
		t.Fatalf("agent-b create: %v", err)
	}
	if status != http.StatusAccepted {
		t.Fatalf("agent-b: want 202 (queued), got %d (%s)", status, body)
	}
	claimB, _ := decode[claimResp](body)
	if claimB.Queued == nil || !*claimB.Queued {
		t.Error("agent-b: expected queued=true")
	}
	if claimB.WaiterID == "" {
		t.Error("agent-b: expected non-empty waiter_id")
	}
	t.Logf("agent-b queued (waiter %s, position %v)", claimB.WaiterID, claimB.Position)

	// Release agent A's claim — DELETE returns 200 with release result.
	status, _, err = cli.do("DELETE", "/api/v1/claims/"+claimA.ID, nil)
	if err != nil {
		t.Fatalf("release agent-a: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("release agent-a: want 200, got %d", status)
	}
	t.Log("agent-a released, agent-b should be promotable")
}
