//go:build integration

package e2e_test

import (
	"net/http"
	"testing"
)

func TestVersionHeader(t *testing.T) {
	srv := NewTestServer(t)

	// Test version header on health endpoint
	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	resp.Body.Close()

	header := resp.Header.Get("X-Hive-Server-Version")
	if header == "" {
		t.Error("X-Hive-Server-Version header missing")
	}

	t.Logf("X-Hive-Server-Version: %s", header)

	// Test version header on API endpoint
	req, err := http.NewRequest("GET", srv.URL+"/api/agents", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+srv.Token)

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/agents: %v", err)
	}
	resp.Body.Close()

	apiHeader := resp.Header.Get("X-Hive-Server-Version")
	if apiHeader == "" {
		t.Error("X-Hive-Server-Version header missing on API endpoint")
	}

	if apiHeader != header {
		t.Errorf("Version header mismatch: health=%q, api=%q", header, apiHeader)
	}
}