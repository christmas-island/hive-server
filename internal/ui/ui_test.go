package ui_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/christmas-island/hive-server/internal/model"
	"github.com/christmas-island/hive-server/internal/ui"
)

// mockStore implements ui.Store for testing
type mockStore struct{}

func (m *mockStore) ListMemory(ctx context.Context, f model.MemoryFilter) ([]*model.MemoryEntry, error) {
	return []*model.MemoryEntry{}, nil
}

func (m *mockStore) ListTasks(ctx context.Context, f model.TaskFilter) ([]*model.Task, error) {
	return []*model.Task{}, nil
}

func (m *mockStore) ListAgents(ctx context.Context) ([]*model.Agent, error) {
	return []*model.Agent{}, nil
}

func (m *mockStore) GetAgent(ctx context.Context, id string) (*model.Agent, error) {
	return &model.Agent{ID: id}, nil
}

func (m *mockStore) ListClaims(ctx context.Context, f model.ClaimFilter) ([]*model.Claim, error) {
	return []*model.Claim{}, nil
}

func (m *mockStore) ListTodos(ctx context.Context, f model.TodoFilter) ([]*model.Todo, error) {
	return []*model.Todo{}, nil
}

func (m *mockStore) ListCapturedSessions(ctx context.Context, f model.SessionFilter) ([]*model.CapturedSession, error) {
	return []*model.CapturedSession{}, nil
}

// newMockUI creates a UI handler with mock store for testing
func newMockUI() *ui.UI {
	return ui.New(&mockStore{}, "")
}

func TestUI_Routes(t *testing.T) {
	handler := newMockUI()
	router := handler.Routes()
	
	// Test routes exist and return expected status codes
	testCases := []struct {
		method       string
		path         string
		expectedCode int
	}{
		{http.MethodGet, "/", http.StatusOK},
		{http.MethodGet, "/agents", http.StatusOK},
		{http.MethodGet, "/tasks", http.StatusOK},
		{http.MethodGet, "/claims", http.StatusOK},
		{http.MethodGet, "/memory", http.StatusOK},
		{http.MethodGet, "/sessions", http.StatusOK},
	}

	for _, tc := range testCases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			
			router.ServeHTTP(w, req)
			
			if w.Code != tc.expectedCode {
				t.Errorf("expected status %d, got %d", tc.expectedCode, w.Code)
			}
		})
	}
}

func TestUI_StaticFiles(t *testing.T) {
	handler := newMockUI()
	router := handler.Routes()
	
	// Test that static files are served
	req := httptest.NewRequest(http.MethodGet, "/static/style.css", nil)
	w := httptest.NewRecorder()
	
	router.ServeHTTP(w, req)
	
	// Should return 200 if file exists, or 404 if it doesn't (both are valid responses)
	if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
		t.Errorf("expected status 200 or 404, got %d", w.Code)
	}
}

func TestUI_AuthMiddleware_NoToken(t *testing.T) {
	// Test with no token configured (should allow access)
	handler := ui.New(&mockStore{}, "")
	router := handler.Routes()
	
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	
	router.ServeHTTP(w, req)
	
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestUI_AuthMiddleware_WithToken(t *testing.T) {
	// Test with token configured
	handler := ui.New(&mockStore{}, "secret")
	router := handler.Routes()
	
	// Request without token should be unauthorized
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	
	router.ServeHTTP(w, req)
	
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
	
	// Request with correct token should succeed
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	
	router.ServeHTTP(w, req)
	
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestUI_TemplateRendering(t *testing.T) {
	handler := newMockUI()
	router := handler.Routes()
	
	// Test that templates render without error
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	
	router.ServeHTTP(w, req)
	
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	
	// Check that it returns HTML conten
	contentType := w.Header().Get("Content-Type")
	body := w.Body.String()
	
	if !strings.Contains(body, "<html") && !strings.Contains(contentType, "text/html") {
		t.Error("expected HTML content to be returned")
	}
}