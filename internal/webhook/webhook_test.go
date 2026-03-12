package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/christmas-island/hive-server/internal/model"
)

// --- mock store ---

type mockStore struct {
	mu             sync.Mutex
	tasks          []*model.Task
	claims         []*model.Claim
	createTaskErr  error
	listTasksErr   error
	updateTaskErr  error
	listClaimsErr  error
	releaseClaimErr error
}

func (m *mockStore) CreateTask(_ context.Context, t *model.Task) (*model.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createTaskErr != nil {
		return nil, m.createTaskErr
	}
	t.ID = "task-" + t.Title
	t.Status = model.TaskStatusOpen
	t.CreatedAt = time.Now().UTC()
	t.UpdatedAt = t.CreatedAt
	if t.Tags == nil {
		t.Tags = []string{}
	}
	if t.Notes == nil {
		t.Notes = []string{}
	}
	m.tasks = append(m.tasks, t)
	return t, nil
}

func (m *mockStore) ListTasks(_ context.Context, _ model.TaskFilter) ([]*model.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listTasksErr != nil {
		return nil, m.listTasksErr
	}
	result := make([]*model.Task, len(m.tasks))
	copy(result, m.tasks)
	return result, nil
}

func (m *mockStore) UpdateTask(_ context.Context, id string, upd model.TaskUpdate) (*model.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateTaskErr != nil {
		return nil, m.updateTaskErr
	}
	for _, t := range m.tasks {
		if t.ID == id {
			if upd.Status != nil {
				t.Status = *upd.Status
			}
			t.UpdatedAt = time.Now().UTC()
			return t, nil
		}
	}
	return nil, model.ErrNotFound
}

func (m *mockStore) ListClaims(_ context.Context, f model.ClaimFilter) ([]*model.Claim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listClaimsErr != nil {
		return nil, m.listClaimsErr
	}
	var result []*model.Claim
	for _, c := range m.claims {
		if f.Resource != "" && c.Resource != f.Resource {
			continue
		}
		if f.Status != "" && string(c.Status) != f.Status {
			continue
		}
		result = append(result, c)
	}
	return result, nil
}

func (m *mockStore) ReleaseClaim(_ context.Context, id string) (*model.ClaimReleaseResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.releaseClaimErr != nil {
		return nil, m.releaseClaimErr
	}
	for _, c := range m.claims {
		if c.ID == id {
			c.Status = model.ClaimStatusReleased
			return &model.ClaimReleaseResult{Released: true, Claim: c}, nil
		}
	}
	return nil, model.ErrNotFound
}

// --- helpers ---

const testSecret = "webhook-secret-potate"

func sign(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func makeRequest(t *testing.T, handler http.Handler, event string, payload any, secret string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", event)
	if secret != "" {
		req.Header.Set("X-Hub-Signature-256", sign(body, secret))
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

// --- signature validation tests ---

func TestValidateSignature_Valid(t *testing.T) {
	payload := []byte(`{"action":"opened"}`)
	secret := []byte(testSecret)
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !ValidateSignature(payload, sig, secret) {
		t.Error("expected valid signature")
	}
}

func TestValidateSignature_Invalid(t *testing.T) {
	payload := []byte(`{"action":"opened"}`)
	secret := []byte(testSecret)

	if ValidateSignature(payload, "sha256=deadbeef", secret) {
		t.Error("expected invalid signature")
	}
}

func TestValidateSignature_MissingPrefix(t *testing.T) {
	payload := []byte(`{"action":"opened"}`)
	secret := []byte(testSecret)

	if ValidateSignature(payload, "deadbeef", secret) {
		t.Error("expected invalid for missing prefix")
	}
}

func TestValidateSignature_BadHex(t *testing.T) {
	payload := []byte(`{"action":"opened"}`)
	secret := []byte(testSecret)

	if ValidateSignature(payload, "sha256=ZZZZ", secret) {
		t.Error("expected invalid for bad hex")
	}
}

func TestValidateSignature_EmptySignature(t *testing.T) {
	payload := []byte(`{"action":"opened"}`)
	secret := []byte(testSecret)

	if ValidateSignature(payload, "", secret) {
		t.Error("expected invalid for empty signature")
	}
}

// --- HTTP handler tests ---

func TestHandler_InvalidSignature(t *testing.T) {
	h := New(testSecret, &mockStore{})
	body := []byte(`{"action":"opened"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-Hub-Signature-256", "sha256=bad")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	h := New(testSecret, &mockStore{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/github", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestHandler_UnknownEvent(t *testing.T) {
	h := New(testSecret, &mockStore{})
	rr := makeRequest(t, h, "star", map[string]any{"action": "created"}, testSecret)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (no-op)", rr.Code)
	}
}

func TestHandler_NoSecret(t *testing.T) {
	ms := &mockStore{}
	h := New("", ms)
	rr := makeRequest(t, h, "issues", map[string]any{
		"action": "opened",
		"issue": map[string]any{
			"number":   42,
			"title":    "Test issue",
			"body":     "A body",
			"html_url": "https://github.com/org/repo/issues/42",
		},
	}, "")

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if len(ms.tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(ms.tasks))
	}
}

// --- issue event tests ---

func TestHandler_IssueOpened_CreatesTask(t *testing.T) {
	ms := &mockStore{}
	h := New(testSecret, ms)

	rr := makeRequest(t, h, "issues", map[string]any{
		"action": "opened",
		"issue": map[string]any{
			"number":   99,
			"title":    "Add feature X",
			"body":     "Description of feature X",
			"html_url": "https://github.com/org/repo/issues/99",
		},
	}, testSecret)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if len(ms.tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(ms.tasks))
	}
	task := ms.tasks[0]
	if task.Title != "Issue #99: Add feature X" {
		t.Errorf("title = %q", task.Title)
	}
	if task.Description != "Description of feature X" {
		t.Errorf("description = %q", task.Description)
	}
	if task.Creator != "github-webhook" {
		t.Errorf("creator = %q, want github-webhook", task.Creator)
	}
}

func TestHandler_IssueClosed_CompletesTask(t *testing.T) {
	ms := &mockStore{
		tasks: []*model.Task{
			{
				ID:     "task-Issue #42: Bug fix",
				Title:  "Issue #42: Bug fix",
				Status: model.TaskStatusOpen,
			},
		},
	}
	h := New(testSecret, ms)

	rr := makeRequest(t, h, "issues", map[string]any{
		"action": "closed",
		"issue": map[string]any{
			"number":   42,
			"title":    "Bug fix",
			"body":     "",
			"html_url": "https://github.com/org/repo/issues/42",
		},
	}, testSecret)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ms.tasks[0].Status != model.TaskStatusDone {
		t.Errorf("status = %q, want done", ms.tasks[0].Status)
	}
}

func TestHandler_IssueClosed_NoMatchingTask(t *testing.T) {
	ms := &mockStore{}
	h := New(testSecret, ms)

	rr := makeRequest(t, h, "issues", map[string]any{
		"action": "closed",
		"issue": map[string]any{
			"number":   999,
			"title":    "Nothing here",
			"body":     "",
			"html_url": "https://github.com/org/repo/issues/999",
		},
	}, testSecret)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

// --- pull_request event tests ---

func TestHandler_PRMerged_ReleasesClaim(t *testing.T) {
	ms := &mockStore{
		claims: []*model.Claim{
			{
				ID:       "claim-1",
				Type:     model.ClaimTypeIssue,
				Resource: "feat/my-branch",
				AgentID:  "smokeyclaw",
				Status:   model.ClaimStatusActive,
			},
		},
	}
	h := New(testSecret, ms)

	rr := makeRequest(t, h, "pull_request", map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number":   10,
			"title":    "Add thing",
			"body":     "",
			"merged":   true,
			"html_url": "https://github.com/org/repo/pull/10",
			"head":     map[string]any{"ref": "feat/my-branch"},
		},
	}, testSecret)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ms.claims[0].Status != model.ClaimStatusReleased {
		t.Errorf("claim status = %q, want released", ms.claims[0].Status)
	}
}

func TestHandler_PROpened_CreatesTask(t *testing.T) {
	ms := &mockStore{}
	h := New(testSecret, ms)

	rr := makeRequest(t, h, "pull_request", map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number":   5,
			"title":    "New PR",
			"body":     "PR body",
			"merged":   false,
			"html_url": "https://github.com/org/repo/pull/5",
			"head":     map[string]any{"ref": "feat/new-pr"},
		},
	}, testSecret)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if len(ms.tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(ms.tasks))
	}
	if ms.tasks[0].Title != "PR #5: New PR" {
		t.Errorf("title = %q", ms.tasks[0].Title)
	}
}

func TestHandler_PRClosed_NotMerged(t *testing.T) {
	ms := &mockStore{
		claims: []*model.Claim{
			{
				ID:       "claim-stay",
				Type:     model.ClaimTypeIssue,
				Resource: "feat/keep-me",
				AgentID:  "agent",
				Status:   model.ClaimStatusActive,
			},
		},
	}
	h := New(testSecret, ms)

	rr := makeRequest(t, h, "pull_request", map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number":   7,
			"title":    "Abandoned",
			"body":     "",
			"merged":   false,
			"html_url": "https://github.com/org/repo/pull/7",
			"head":     map[string]any{"ref": "feat/keep-me"},
		},
	}, testSecret)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ms.claims[0].Status != model.ClaimStatusActive {
		t.Errorf("claim should stay active when PR closed without merge, got %q", ms.claims[0].Status)
	}
}

// --- delete event tests ---

func TestHandler_BranchDeleted_ReleasesClaim(t *testing.T) {
	ms := &mockStore{
		claims: []*model.Claim{
			{
				ID:       "claim-branch",
				Type:     model.ClaimTypeIssue,
				Resource: "feat/old-branch",
				AgentID:  "jakeclaw",
				Status:   model.ClaimStatusActive,
			},
		},
	}
	h := New(testSecret, ms)

	rr := makeRequest(t, h, "delete", map[string]any{
		"ref_type": "branch",
		"ref":      "feat/old-branch",
	}, testSecret)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ms.claims[0].Status != model.ClaimStatusReleased {
		t.Errorf("claim status = %q, want released", ms.claims[0].Status)
	}
}

func TestHandler_TagDeleted_NoClaims(t *testing.T) {
	ms := &mockStore{
		claims: []*model.Claim{
			{
				ID:       "claim-tag",
				Type:     model.ClaimTypeIssue,
				Resource: "v1.0.0",
				AgentID:  "agent",
				Status:   model.ClaimStatusActive,
			},
		},
	}
	h := New(testSecret, ms)

	rr := makeRequest(t, h, "delete", map[string]any{
		"ref_type": "tag",
		"ref":      "v1.0.0",
	}, testSecret)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ms.claims[0].Status != model.ClaimStatusActive {
		t.Errorf("tag delete should not release claims, got %q", ms.claims[0].Status)
	}
}

// --- error path tests ---

func TestHandler_CreateTaskError_PR(t *testing.T) {
	ms := &mockStore{createTaskErr: fmt.Errorf("db down")}
	h := New(testSecret, ms)

	rr := makeRequest(t, h, "pull_request", map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number":   1,
			"title":    "Err PR",
			"body":     "",
			"merged":   false,
			"html_url": "https://github.com/org/repo/pull/1",
			"head":     map[string]any{"ref": "feat/err"},
		},
	}, testSecret)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (errors are logged, not returned)", rr.Code)
	}
}

func TestHandler_CreateTaskError_Issue(t *testing.T) {
	ms := &mockStore{createTaskErr: fmt.Errorf("db down")}
	h := New(testSecret, ms)

	rr := makeRequest(t, h, "issues", map[string]any{
		"action": "opened",
		"issue": map[string]any{
			"number":   1,
			"title":    "Err Issue",
			"body":     "",
			"html_url": "https://github.com/org/repo/issues/1",
		},
	}, testSecret)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandler_ListClaimsError(t *testing.T) {
	ms := &mockStore{listClaimsErr: fmt.Errorf("db down")}
	h := New(testSecret, ms)

	rr := makeRequest(t, h, "pull_request", map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number":   2,
			"title":    "Merged",
			"body":     "",
			"merged":   true,
			"html_url": "https://github.com/org/repo/pull/2",
			"head":     map[string]any{"ref": "feat/err-claims"},
		},
	}, testSecret)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandler_ReleaseClaimError(t *testing.T) {
	ms := &mockStore{
		releaseClaimErr: fmt.Errorf("db down"),
		claims: []*model.Claim{
			{
				ID:       "claim-err",
				Type:     model.ClaimTypeIssue,
				Resource: "feat/err-release",
				AgentID:  "agent",
				Status:   model.ClaimStatusActive,
			},
		},
	}
	h := New(testSecret, ms)

	rr := makeRequest(t, h, "pull_request", map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number":   3,
			"title":    "Merged with err",
			"body":     "",
			"merged":   true,
			"html_url": "https://github.com/org/repo/pull/3",
			"head":     map[string]any{"ref": "feat/err-release"},
		},
	}, testSecret)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandler_ListTasksError_IssueClosed(t *testing.T) {
	ms := &mockStore{listTasksErr: fmt.Errorf("db down")}
	h := New(testSecret, ms)

	rr := makeRequest(t, h, "issues", map[string]any{
		"action": "closed",
		"issue": map[string]any{
			"number":   50,
			"title":    "Err close",
			"body":     "",
			"html_url": "https://github.com/org/repo/issues/50",
		},
	}, testSecret)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandler_UpdateTaskError_IssueClosed(t *testing.T) {
	ms := &mockStore{
		updateTaskErr: fmt.Errorf("db down"),
		tasks: []*model.Task{
			{
				ID:     "task-Issue #60: Fail update",
				Title:  "Issue #60: Fail update",
				Status: model.TaskStatusOpen,
			},
		},
	}
	h := New(testSecret, ms)

	rr := makeRequest(t, h, "issues", map[string]any{
		"action": "closed",
		"issue": map[string]any{
			"number":   60,
			"title":    "Fail update",
			"body":     "",
			"html_url": "https://github.com/org/repo/issues/60",
		},
	}, testSecret)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandler_BadJSON_PullRequest(t *testing.T) {
	h := New("", &mockStore{})
	body := []byte(`{not valid json`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (bad JSON logged, not 4xx)", rr.Code)
	}
}

func TestHandler_BadJSON_Issues(t *testing.T) {
	h := New("", &mockStore{})
	body := []byte(`{not valid json`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issues")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandler_BadJSON_Delete(t *testing.T) {
	h := New("", &mockStore{})
	body := []byte(`{not valid json`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "delete")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandler_DeleteBranch_EmptyRef(t *testing.T) {
	ms := &mockStore{}
	h := New(testSecret, ms)

	rr := makeRequest(t, h, "delete", map[string]any{
		"ref_type": "branch",
		"ref":      "",
	}, testSecret)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHandler_PRMerged_EmptyBranch(t *testing.T) {
	ms := &mockStore{}
	h := New(testSecret, ms)

	rr := makeRequest(t, h, "pull_request", map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number":   4,
			"title":    "Empty branch",
			"body":     "",
			"merged":   true,
			"html_url": "",
			"head":     map[string]any{"ref": ""},
		},
	}, testSecret)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}
