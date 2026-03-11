package handlers

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// okHandler returns 200 OK with a JSON body.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
})

// errorHandler returns 404 Not Found.
var errorHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"error":"not found"}`))
})

// noWriteHeaderHandler writes body without calling WriteHeader (implicit 200).
var noWriteHeaderHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte(`{"implicit":true}`))
})

func TestTimingMiddleware_HeadersPresent(t *testing.T) {
	handler := timingMiddleware(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer result.Body.Close()

	xTotal := result.Header.Get("X-Total-Ms")
	if xTotal == "" {
		t.Fatal("X-Total-Ms header missing")
	}
	ms, err := strconv.ParseInt(xTotal, 10, 64)
	if err != nil {
		t.Fatalf("X-Total-Ms not a valid integer: %q", xTotal)
	}
	if ms < 0 {
		t.Errorf("X-Total-Ms should be >= 0, got %d", ms)
	}

	xProc := result.Header.Get("X-Processing-Ms")
	if xProc == "" {
		t.Fatal("X-Processing-Ms header missing")
	}
	ms2, err := strconv.ParseInt(xProc, 10, 64)
	if err != nil {
		t.Fatalf("X-Processing-Ms not a valid integer: %q", xProc)
	}
	if ms2 < 0 {
		t.Errorf("X-Processing-Ms should be >= 0, got %d", ms2)
	}
}

func TestTimingMiddleware_HeadersPresentOnError(t *testing.T) {
	handler := timingMiddleware(errorHandler)

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer result.Body.Close()

	if result.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", result.StatusCode)
	}
	if result.Header.Get("X-Total-Ms") == "" {
		t.Error("X-Total-Ms header missing on error response")
	}
	if result.Header.Get("X-Processing-Ms") == "" {
		t.Error("X-Processing-Ms header missing on error response")
	}
}

func TestTimingMiddleware_ImplicitStatus200(t *testing.T) {
	// Handler that writes body without calling WriteHeader — should default to 200.
	handler := timingMiddleware(noWriteHeaderHandler)

	req := httptest.NewRequest(http.MethodGet, "/implicit", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer result.Body.Close()

	if result.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", result.StatusCode)
	}
	if result.Header.Get("X-Total-Ms") == "" {
		t.Error("X-Total-Ms header missing on implicit-200 response")
	}
}

func TestTimingMiddleware_BodyPassthrough(t *testing.T) {
	// Ensure the response body is still delivered correctly.
	handler := timingMiddleware(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if body != `{"ok":true}` {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestTimingMiddleware_ValuesMatch(t *testing.T) {
	// X-Total-Ms and X-Processing-Ms should always be equal at this layer.
	handler := timingMiddleware(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer result.Body.Close()

	total := result.Header.Get("X-Total-Ms")
	proc := result.Header.Get("X-Processing-Ms")
	if total != proc {
		t.Errorf("X-Total-Ms (%s) != X-Processing-Ms (%s) — should be equal at hive-server layer", total, proc)
	}
}
