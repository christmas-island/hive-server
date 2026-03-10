package relay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdateStatus_Disabled(t *testing.T) {
	c := New("", "")
	if err := c.UpdateStatus(context.Background(), "agent-1", "online", ""); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestRecordUsage_Disabled(t *testing.T) {
	c := New("", "")
	if err := c.RecordUsage(context.Background(), "agent-1", UsageReport{}); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestUpdateStatus_Request(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	err := c.UpdateStatus(context.Background(), "agent-42", "idle", "thinking")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if gotPath != "/agents/agent-42/status" {
		t.Errorf("path = %q, want /agents/agent-42/status", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("auth = %q, want Bearer test-token", gotAuth)
	}
	if gotBody["status"] != "idle" {
		t.Errorf("body status = %q, want idle", gotBody["status"])
	}
	if gotBody["activity"] != "thinking" {
		t.Errorf("body activity = %q, want thinking", gotBody["activity"])
	}
}

func TestRecordUsage_Request(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody UsageReport

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	usage := UsageReport{
		Model:            "claude-sonnet-4-6",
		InputTokens:      100,
		OutputTokens:     50,
		CacheReadTokens:  10,
		CacheWriteTokens: 5,
		TotalTokens:      165,
		EstimatedCostUsd: 0.01,
		SessionID:        "sess-1",
		Timestamp:        "2026-03-10T00:00:00Z",
	}
	err := c.RecordUsage(context.Background(), "agent-7", usage)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/agents/agent-7/usage" {
		t.Errorf("path = %q, want /agents/agent-7/usage", gotPath)
	}
	if gotBody.Model != "claude-sonnet-4-6" {
		t.Errorf("model = %q, want claude-sonnet-4-6", gotBody.Model)
	}
	if gotBody.TotalTokens != 165 {
		t.Errorf("totalTokens = %d, want 165", gotBody.TotalTokens)
	}
}

func TestUpdateStatus_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	// Should not return an error even on non-2xx (best-effort, logged only).
	err := c.UpdateStatus(context.Background(), "agent-1", "online", "")
	if err != nil {
		t.Fatalf("expected nil error on non-2xx, got %v", err)
	}
}

func TestRecordUsage_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	err := c.RecordUsage(context.Background(), "agent-1", UsageReport{})
	if err != nil {
		t.Fatalf("expected nil error on non-2xx, got %v", err)
	}
}
