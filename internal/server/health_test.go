package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockPinger implements the pinger interface for testing.
type mockPinger struct {
	err error
}

func (m *mockPinger) Ping(_ context.Context) error {
	return m.err
}

func TestHandleHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want ok", resp["status"])
	}
}

func TestHandleReady(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	handleReady(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "ready" {
		t.Errorf("status = %q, want ready", resp["status"])
	}
}

func TestHealthzHandler_Healthy(t *testing.T) {
	p := &mockPinger{}
	h := healthzHandler(p)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "healthy" {
		t.Errorf("status = %q, want healthy", resp["status"])
	}
}

func TestHandleVersion(t *testing.T) {
	// Set known version info for the test.
	SetVersionInfo("1.2.3", "abc1234", "2026-03-11")

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	w := httptest.NewRecorder()

	handleVersion(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var vi VersionInfo
	if err := json.NewDecoder(w.Body).Decode(&vi); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if vi.Version != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3", vi.Version)
	}
	if vi.Commit != "abc1234" {
		t.Errorf("Commit = %q, want abc1234", vi.Commit)
	}
	if vi.Date != "2026-03-11" {
		t.Errorf("Date = %q, want 2026-03-11", vi.Date)
	}
}

func TestSetVersionInfo(t *testing.T) {
	SetVersionInfo("v2.0.0", "deadbeef", "2026-01-01")
	vi := GetVersionInfo()

	if vi.Version != "v2.0.0" {
		t.Errorf("Version = %q, want v2.0.0", vi.Version)
	}
	if vi.Commit != "deadbeef" {
		t.Errorf("Commit = %q, want deadbeef", vi.Commit)
	}
	if vi.Date != "2026-01-01" {
		t.Errorf("Date = %q, want 2026-01-01", vi.Date)
	}
}

func TestGetVersionInfo_Defaults(t *testing.T) {
	// Reset to defaults.
	SetVersionInfo("dev", "none", "unknown")
	vi := GetVersionInfo()

	if vi.Version != "dev" {
		t.Errorf("Version = %q, want dev", vi.Version)
	}
	if vi.Commit != "none" {
		t.Errorf("Commit = %q, want none", vi.Commit)
	}
	if vi.Date != "unknown" {
		t.Errorf("Date = %q, want unknown", vi.Date)
	}
}

func TestHealthzHandler_Unhealthy(t *testing.T) {
	p := &mockPinger{err: errors.New("connection refused")}
	h := healthzHandler(p)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "unavailable" {
		t.Errorf("status = %q, want unavailable", resp["status"])
	}
	if resp["error"] == "" {
		t.Error("expected error message in response")
	}
}
