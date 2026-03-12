package ui_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/christmas-island/hive-server/internal/model"
	"github.com/christmas-island/hive-server/internal/ui"
)

// mockStore implements ui.Store for testing
type mockStore struct {
	failOn string // set to method name to make it return an error
}

func (m *mockStore) ListMemory(_ context.Context, _ model.MemoryFilter) ([]*model.MemoryEntry, error) {
	if m.failOn == "ListMemory" {
		return nil, errors.New("mock error")
	}
	return []*model.MemoryEntry{
		{Key: "test-key", Value: "test-value", AgentID: "jathyclaw", UpdatedAt: time.Now()},
	}, nil
}

func (m *mockStore) ListTasks(_ context.Context, _ model.TaskFilter) ([]*model.Task, error) {
	if m.failOn == "ListTasks" {
		return nil, errors.New("mock error")
	}
	return []*model.Task{
		{ID: "t1", Title: "Test task", Status: model.TaskStatusOpen, Creator: "jathyclaw"},
	}, nil
}

func (m *mockStore) ListAgents(_ context.Context) ([]*model.Agent, error) {
	if m.failOn == "ListAgents" {
		return nil, errors.New("mock error")
	}
	return []*model.Agent{
		{ID: "jathyclaw", Status: model.AgentStatusOnline, LastHeartbeat: time.Now()},
		{ID: "dragonclaw", Status: model.AgentStatusOffline, LastHeartbeat: time.Now().Add(-1 * time.Hour)},
	}, nil
}

func (m *mockStore) GetAgent(_ context.Context, id string) (*model.Agent, error) {
	return &model.Agent{ID: id}, nil
}

func (m *mockStore) ListClaims(_ context.Context, _ model.ClaimFilter) ([]*model.Claim, error) {
	if m.failOn == "ListClaims" {
		return nil, errors.New("mock error")
	}
	return []*model.Claim{
		{ID: "c1", Resource: "test-resource", AgentID: "jathyclaw", Status: "active"},
	}, nil
}

func (m *mockStore) ListTodos(_ context.Context, _ model.TodoFilter) ([]*model.Todo, error) {
	return []*model.Todo{}, nil
}

func (m *mockStore) ListCapturedSessions(_ context.Context, _ model.SessionFilter) ([]*model.CapturedSession, error) {
	if m.failOn == "ListCapturedSessions" {
		return nil, errors.New("mock error")
	}
	return []*model.CapturedSession{
		{ID: "s1", AgentID: "jathyclaw"},
	}, nil
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
	req := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for static file, got %d", w.Code)
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

	// Check that it returns HTML content
	contentType := w.Header().Get("Content-Type")
	body := w.Body.String()

	if !strings.Contains(body, "<html") && !strings.Contains(contentType, "text/html") {
		t.Error("expected HTML content to be returned")
	}
}

func TestUI_DashboardError(t *testing.T) {
	store := &mockStore{failOn: "ListAgents"}
	handler := ui.New(store, "")
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestUI_DashboardTasksError(t *testing.T) {
	store := &mockStore{failOn: "ListTasks"}
	handler := ui.New(store, "")
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestUI_DashboardClaimsError(t *testing.T) {
	store := &mockStore{failOn: "ListClaims"}
	handler := ui.New(store, "")
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestUI_DashboardMemoryError(t *testing.T) {
	store := &mockStore{failOn: "ListMemory"}
	handler := ui.New(store, "")
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestUI_AgentsError(t *testing.T) {
	store := &mockStore{failOn: "ListAgents"}
	handler := ui.New(store, "")
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestUI_TasksError(t *testing.T) {
	store := &mockStore{failOn: "ListTasks"}
	handler := ui.New(store, "")
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestUI_ClaimsError(t *testing.T) {
	store := &mockStore{failOn: "ListClaims"}
	handler := ui.New(store, "")
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodGet, "/claims", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestUI_MemoryError(t *testing.T) {
	store := &mockStore{failOn: "ListMemory"}
	handler := ui.New(store, "")
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodGet, "/memory", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestUI_SessionsError(t *testing.T) {
	store := &mockStore{failOn: "ListCapturedSessions"}
	handler := ui.New(store, "")
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestUI_TasksWithAllFilters(t *testing.T) {
	handler := newMockUI()
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodGet, "/tasks?status=open&assignee=jathyclaw&creator=jakeclaw&limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestUI_ClaimsWithAllFilters(t *testing.T) {
	handler := newMockUI()
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodGet, "/claims?status=active&agent=jathyclaw&limit=25", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestUI_MemoryWithLimit(t *testing.T) {
	handler := newMockUI()
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodGet, "/memory?limit=100", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestUI_SessionsWithAllFilters(t *testing.T) {
	handler := newMockUI()
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodGet, "/sessions?agent=jathyclaw&repo=hive-server&limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestUI_RenderUnknownPage(t *testing.T) {
	handler := ui.New(&mockStore{}, "")
	w := httptest.NewRecorder()

	// Call Render with a page name that doesn't exist in the pages map
	handler.Render(w, "nonexistent.html", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown page, got %d", w.Code)
	}
}

func TestUI_AuthMiddleware_WrongToken(t *testing.T) {
	handler := ui.New(&mockStore{}, "secret")
	router := handler.Routes()

	// Request with wrong token should be unauthorized
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong token, got %d", w.Code)
	}
}
