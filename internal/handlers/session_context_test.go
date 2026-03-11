package handlers_test

import (
	"net/http"
	"testing"

	"github.com/christmas-island/hive-server/internal/model"
)

// sessionHeaders returns a standard set of session context headers for testing.
func sessionHeaders() map[string]string {
	return map[string]string{
		"X-Session-Key":    "sk_test123",
		"X-Session-ID":     "sess_abc",
		"X-Channel":        "general",
		"X-Sender-ID":      "user42",
		"X-Sender-Is-Owner": "true",
		"X-Sandboxed":      "false",
	}
}

func TestMemoryUpsert_SessionContext_RoundTrip(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)
	hdrs := sessionHeaders()

	// Create with session headers.
	resp := requestWithHeaders(t, srv, http.MethodPost, "/api/v1/memory", map[string]any{
		"key":   "session.test",
		"value": "hello",
	}, testToken, testAgent, hdrs)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var entry model.MemoryEntry
	decodeJSON(t, resp, &entry)

	if entry.SessionKey != "sk_test123" {
		t.Errorf("SessionKey = %q, want sk_test123", entry.SessionKey)
	}
	if entry.SessionID != "sess_abc" {
		t.Errorf("SessionID = %q, want sess_abc", entry.SessionID)
	}
	if entry.Channel != "general" {
		t.Errorf("Channel = %q, want general", entry.Channel)
	}
	if entry.SenderID != "user42" {
		t.Errorf("SenderID = %q, want user42", entry.SenderID)
	}
	if !entry.SenderIsOwner {
		t.Error("SenderIsOwner = false, want true")
	}
	if entry.Sandboxed {
		t.Error("Sandboxed = true, want false")
	}

	// Read back via GET and verify session context persists.
	resp2 := request(t, srv, http.MethodGet, "/api/v1/memory/session.test", nil, testToken, testAgent)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", resp2.StatusCode)
	}
	var entry2 model.MemoryEntry
	decodeJSON(t, resp2, &entry2)

	if entry2.SessionKey != "sk_test123" {
		t.Errorf("GET SessionKey = %q, want sk_test123", entry2.SessionKey)
	}
	if entry2.SenderID != "user42" {
		t.Errorf("GET SenderID = %q, want user42", entry2.SenderID)
	}
}

func TestTaskCreate_SessionContext_RoundTrip(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)
	hdrs := sessionHeaders()

	resp := requestWithHeaders(t, srv, http.MethodPost, "/api/v1/tasks", map[string]any{
		"title": "session task",
	}, testToken, testAgent, hdrs)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var task model.Task
	decodeJSON(t, resp, &task)

	if task.SessionKey != "sk_test123" {
		t.Errorf("SessionKey = %q, want sk_test123", task.SessionKey)
	}
	if task.Channel != "general" {
		t.Errorf("Channel = %q, want general", task.Channel)
	}
	if !task.SenderIsOwner {
		t.Error("SenderIsOwner = false, want true")
	}

	// Read back via GET.
	resp2 := request(t, srv, http.MethodGet, "/api/v1/tasks/"+task.ID, nil, testToken, testAgent)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", resp2.StatusCode)
	}
	var task2 model.Task
	decodeJSON(t, resp2, &task2)

	if task2.SessionKey != "sk_test123" {
		t.Errorf("GET SessionKey = %q, want sk_test123", task2.SessionKey)
	}
}

func TestTaskUpdate_SessionContext_RoundTrip(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	// Create a task without session headers.
	resp := request(t, srv, http.MethodPost, "/api/v1/tasks", map[string]any{
		"title": "update session task",
	}, testToken, testAgent)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	var task model.Task
	decodeJSON(t, resp, &task)

	if task.SessionKey != "" {
		t.Errorf("initial SessionKey = %q, want empty", task.SessionKey)
	}

	// Update with session headers.
	hdrs := sessionHeaders()
	claimed := model.TaskStatusClaimed
	resp2 := requestWithHeaders(t, srv, http.MethodPatch, "/api/v1/tasks/"+task.ID, map[string]any{
		"status": claimed,
	}, testToken, testAgent, hdrs)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d, want 200", resp2.StatusCode)
	}
	var updated model.Task
	decodeJSON(t, resp2, &updated)

	if updated.SessionKey != "sk_test123" {
		t.Errorf("updated SessionKey = %q, want sk_test123", updated.SessionKey)
	}
	if updated.SenderID != "user42" {
		t.Errorf("updated SenderID = %q, want user42", updated.SenderID)
	}
}

func TestClaimCreate_SessionContext_RoundTrip(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)
	hdrs := sessionHeaders()

	resp := requestWithHeaders(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type":     "issue",
		"resource": "session#1",
	}, testToken, testAgent, hdrs)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var claim model.Claim
	decodeJSON(t, resp, &claim)

	if claim.SessionKey != "sk_test123" {
		t.Errorf("SessionKey = %q, want sk_test123", claim.SessionKey)
	}
	if claim.Channel != "general" {
		t.Errorf("Channel = %q, want general", claim.Channel)
	}
	if claim.SenderID != "user42" {
		t.Errorf("SenderID = %q, want user42", claim.SenderID)
	}
	if !claim.SenderIsOwner {
		t.Error("SenderIsOwner = false, want true")
	}

	// Read back via GET.
	resp2 := request(t, srv, http.MethodGet, "/api/v1/claims/"+claim.ID, nil, testToken, testAgent)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", resp2.StatusCode)
	}
	var claim2 model.Claim
	decodeJSON(t, resp2, &claim2)

	if claim2.SessionKey != "sk_test123" {
		t.Errorf("GET SessionKey = %q, want sk_test123", claim2.SessionKey)
	}
}

func TestMemoryList_SessionKeyFilter(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	// Create two entries with different session keys.
	hdrs1 := map[string]string{"X-Session-Key": "sk_alpha"}
	hdrs2 := map[string]string{"X-Session-Key": "sk_beta"}

	requestWithHeaders(t, srv, http.MethodPost, "/api/v1/memory", map[string]any{
		"key": "sk.a", "value": "v",
	}, testToken, testAgent, hdrs1).Body.Close()

	requestWithHeaders(t, srv, http.MethodPost, "/api/v1/memory", map[string]any{
		"key": "sk.b", "value": "v",
	}, testToken, testAgent, hdrs2).Body.Close()

	// Filter by session_key.
	resp := request(t, srv, http.MethodGet, "/api/v1/memory?session_key=sk_alpha", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var entries []model.MemoryEntry
	decodeJSON(t, resp, &entries)
	if len(entries) != 1 {
		t.Fatalf("len = %d, want 1", len(entries))
	}
	if entries[0].Key != "sk.a" {
		t.Errorf("Key = %q, want sk.a", entries[0].Key)
	}
}

func TestTaskList_SessionKeyFilter(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	hdrs := map[string]string{"X-Session-Key": "sk_task_filter"}
	requestWithHeaders(t, srv, http.MethodPost, "/api/v1/tasks", map[string]any{
		"title": "filtered task",
	}, testToken, testAgent, hdrs).Body.Close()

	request(t, srv, http.MethodPost, "/api/v1/tasks", map[string]any{
		"title": "unfiltered task",
	}, testToken, testAgent).Body.Close()

	resp := request(t, srv, http.MethodGet, "/api/v1/tasks?session_key=sk_task_filter", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var tasks []model.Task
	decodeJSON(t, resp, &tasks)
	if len(tasks) != 1 {
		t.Fatalf("len = %d, want 1", len(tasks))
	}
	if tasks[0].Title != "filtered task" {
		t.Errorf("Title = %q, want 'filtered task'", tasks[0].Title)
	}
}

func TestClaimList_SessionKeyFilter(t *testing.T) {
	srv := newMockServerWithToken(t, testToken)

	hdrs := map[string]string{"X-Session-Key": "sk_claim_filter"}
	requestWithHeaders(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type": "issue", "resource": "filter#1",
	}, testToken, testAgent, hdrs).Body.Close()

	request(t, srv, http.MethodPost, "/api/v1/claims", map[string]any{
		"type": "issue", "resource": "filter#2",
	}, testToken, testAgent).Body.Close()

	resp := request(t, srv, http.MethodGet, "/api/v1/claims?session_key=sk_claim_filter", nil, testToken, testAgent)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var claims []model.Claim
	decodeJSON(t, resp, &claims)
	if len(claims) != 1 {
		t.Fatalf("len = %d, want 1", len(claims))
	}
	if claims[0].Resource != "filter#1" {
		t.Errorf("Resource = %q, want filter#1", claims[0].Resource)
	}
}
