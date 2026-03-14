//go:build integration

package testharness_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/christmas-island/hive-server/pkg/testharness"
)

func TestNewTestServer_HealthAndReady(t *testing.T) {
	baseURL := testharness.NewTestServer(t)
	if baseURL == "" {
		t.Fatal("NewTestServer returned empty URL")
	}

	for _, path := range []string{"/health", "/ready"} {
		resp, err := http.Get(baseURL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status %d, want 200", path, resp.StatusCode)
		}
	}
}

func TestNewTestServer_MemoryCRUD(t *testing.T) {
	token := "harness-test-token"
	baseURL := testharness.NewTestServer(t, testharness.WithToken(token))

	client := &http.Client{}

	// POST a memory entry (upsert via POST /api/v1/memory).
	body := `{"key":"greeting","value":"hello world","tags":["test"]}`
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/memory", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Agent-ID", "test-agent")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST memory: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST memory: status %d, want 200", resp.StatusCode)
	}

	// GET it back.
	req, _ = http.NewRequest(http.MethodGet, baseURL+"/api/v1/memory/greeting", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Agent-ID", "test-agent")

	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET memory: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET memory: status %d, want 200", resp.StatusCode)
	}

	data, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if result["key"] != "greeting" {
		t.Fatalf("expected key=greeting, got %v", result["key"])
	}
	if result["value"] != "hello world" {
		t.Fatalf("expected value='hello world', got %v", result["value"])
	}
}
