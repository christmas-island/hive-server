package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionHeaderMiddleware(t *testing.T) {
	// Set test version info
	SetVersionInfo("1.2.3", "abc123", "2024-01-01T00:00:00Z")

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	})

	// Wrap with middleware
	wrapped := versionHeaderMiddleware(testHandler)

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	// Execute request
	wrapped.ServeHTTP(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	header := w.Header().Get("X-Hive-Server-Version")
	if header != "1.2.3" {
		t.Errorf("X-Hive-Server-Version = %q, want %q", header, "1.2.3")
	}

	if body := w.Body.String(); body != "test response" {
		t.Errorf("Body = %q, want %q", body, "test response")
	}
}

func TestVersionHeaderMiddleware_DefaultVersion(t *testing.T) {
	// Reset to default version
	SetVersionInfo("dev", "none", "unknown")

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := versionHeaderMiddleware(testHandler)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	header := w.Header().Get("X-Hive-Server-Version")
	if header != "dev" {
		t.Errorf("X-Hive-Server-Version = %q, want %q", header, "dev")
	}
}
