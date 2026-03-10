package store

import (
	"testing"
	"time"

	"github.com/christmas-island/hive-server/internal/model"
)

// --- finishAgentScan ---

func TestFinishAgentScan_ValidInput(t *testing.T) {
	a := &model.Agent{ID: "test-agent", Status: model.AgentStatusOnline}
	ts := time.Now().UTC().Truncate(time.Second)
	hbStr := ts.Format(time.RFC3339Nano)
	regStr := ts.Add(-time.Hour).Format(time.RFC3339Nano)
	capsRaw := `["memory","tasks"]`

	got, err := finishAgentScan(a, capsRaw, hbStr, regStr)
	if err != nil {
		t.Fatalf("finishAgentScan: %v", err)
	}
	if len(got.Capabilities) != 2 {
		t.Errorf("capabilities len = %d, want 2", len(got.Capabilities))
	}
	if got.Capabilities[0] != "memory" {
		t.Errorf("capabilities[0] = %q, want memory", got.Capabilities[0])
	}
}

func TestFinishAgentScan_InvalidJSON_DefaultsToEmptySlice(t *testing.T) {
	a := &model.Agent{ID: "a", Status: model.AgentStatusOnline}
	got, err := finishAgentScan(a, "not-json", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Capabilities == nil || len(got.Capabilities) != 0 {
		t.Errorf("expected empty capabilities, got %v", got.Capabilities)
	}
}

func TestFinishAgentScan_NullCapabilities(t *testing.T) {
	a := &model.Agent{ID: "a", Status: model.AgentStatusOnline}
	got, err := finishAgentScan(a, "null", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Capabilities == nil {
		t.Error("capabilities should not be nil after null JSON")
	}
}

func TestFinishAgentScan_RFC3339Fallback(t *testing.T) {
	a := &model.Agent{ID: "a", Status: model.AgentStatusOnline}
	ts := time.Now().UTC().Truncate(time.Second)
	hbStr := ts.Format(time.RFC3339) // non-nano format
	regStr := ts.Format(time.RFC3339)
	got, err := finishAgentScan(a, "[]", hbStr, regStr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.LastHeartbeat.IsZero() {
		t.Error("LastHeartbeat should not be zero with valid RFC3339 string")
	}
}

func TestFinishAgentScan_StaleHeartbeatMarkedOffline(t *testing.T) {
	a := &model.Agent{ID: "a", Status: model.AgentStatusOnline}
	// Heartbeat from 2 days ago — well past offlineThreshold.
	stale := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339Nano)
	got, err := finishAgentScan(a, "[]", stale, stale)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != model.AgentStatusOffline {
		t.Errorf("status = %q, want offline", got.Status)
	}
}

// --- finishMemoryScan ---

func TestFinishMemoryScan_ValidInput(t *testing.T) {
	e := &model.MemoryEntry{Key: "k", Value: "v"}
	ts := time.Now().UTC().Truncate(time.Second)
	timeStr := ts.Format(time.RFC3339Nano)

	got, err := finishMemoryScan(e, `["tag1","tag2"]`, timeStr, timeStr)
	if err != nil {
		t.Fatalf("finishMemoryScan: %v", err)
	}
	if len(got.Tags) != 2 {
		t.Errorf("tags len = %d, want 2", len(got.Tags))
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}

func TestFinishMemoryScan_InvalidJSON_DefaultsToEmpty(t *testing.T) {
	e := &model.MemoryEntry{Key: "k"}
	got, err := finishMemoryScan(e, "bad-json", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Tags == nil || len(got.Tags) != 0 {
		t.Errorf("expected empty tags, got %v", got.Tags)
	}
}

func TestFinishMemoryScan_NullTags(t *testing.T) {
	e := &model.MemoryEntry{Key: "k"}
	got, err := finishMemoryScan(e, "null", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Tags == nil {
		t.Error("tags should not be nil after null JSON")
	}
}

func TestFinishMemoryScan_RFC3339Fallback(t *testing.T) {
	e := &model.MemoryEntry{Key: "k"}
	ts := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	got, err := finishMemoryScan(e, "[]", ts, ts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero with valid RFC3339 string")
	}
}

// --- finishTaskScan ---

func TestFinishTaskScan_ValidInput(t *testing.T) {
	task := &model.Task{ID: "t1", Title: "Test task"}
	ts := time.Now().UTC().Truncate(time.Second)
	timeStr := ts.Format(time.RFC3339Nano)

	got, err := finishTaskScan(task, `["bug","high"]`, timeStr, timeStr)
	if err != nil {
		t.Fatalf("finishTaskScan: %v", err)
	}
	if len(got.Tags) != 2 {
		t.Errorf("tags len = %d, want 2", len(got.Tags))
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestFinishTaskScan_InvalidJSON_DefaultsToEmpty(t *testing.T) {
	task := &model.Task{ID: "t1"}
	got, err := finishTaskScan(task, "not-valid-json", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Tags == nil || len(got.Tags) != 0 {
		t.Errorf("expected empty tags, got %v", got.Tags)
	}
}

func TestFinishTaskScan_NullTags(t *testing.T) {
	task := &model.Task{ID: "t1"}
	got, err := finishTaskScan(task, "null", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Tags == nil {
		t.Error("tags should not be nil after null JSON")
	}
}

func TestFinishTaskScan_RFC3339Fallback(t *testing.T) {
	task := &model.Task{ID: "t1"}
	ts := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	got, err := finishTaskScan(task, "[]", ts, ts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero with valid RFC3339 string")
	}
}

// --- finishClaimScan ---

func TestFinishClaimScan_ValidInput(t *testing.T) {
	c := &model.Claim{ID: "c1", Type: "resource", Resource: "my-resource", AgentID: "agent1"}
	ts := time.Now().UTC().Truncate(time.Second)
	timeStr := ts.Format(time.RFC3339Nano)

	got, err := finishClaimScan(c, `{"key":"value"}`, timeStr, timeStr, timeStr)
	if err != nil {
		t.Fatalf("finishClaimScan: %v", err)
	}
	if got.ClaimedAt.IsZero() {
		t.Error("ClaimedAt should not be zero")
	}
	if got.ExpiresAt.IsZero() {
		t.Error("ExpiresAt should not be zero")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}

func TestFinishClaimScan_InvalidMetadataJSON(t *testing.T) {
	c := &model.Claim{ID: "c1"}
	// Invalid JSON for metadata — should not error, just use empty/nil
	got, err := finishClaimScan(c, "bad-json", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = got // just ensure no panic
}


// --- finishChannelScan ---

func TestFinishChannelScan_ValidInput(t *testing.T) {
	ch := &model.DiscoveryChannel{ID: "general", Name: "General"}
	ts := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339Nano)

	got, err := finishChannelScan(ch, `["member1","member2"]`, ts, ts)
	if err != nil {
		t.Fatalf("finishChannelScan: %v", err)
	}
	if len(got.Members) != 2 {
		t.Errorf("members len = %d, want 2", len(got.Members))
	}
}

func TestFinishChannelScan_InvalidJSON(t *testing.T) {
	ch := &model.DiscoveryChannel{ID: "c"}
	got, err := finishChannelScan(ch, "not-json", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Members == nil || len(got.Members) != 0 {
		t.Errorf("expected empty members, got %v", got.Members)
	}
}

func TestFinishChannelScan_NullMembers(t *testing.T) {
	ch := &model.DiscoveryChannel{ID: "c"}
	got, err := finishChannelScan(ch, "null", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Members == nil {
		t.Error("members should not be nil after null JSON")
	}
}

func TestFinishChannelScan_RFC3339Fallback(t *testing.T) {
	ch := &model.DiscoveryChannel{ID: "c"}
	ts := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	got, err := finishChannelScan(ch, "[]", ts, ts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

// --- finishRoleScan ---

func TestFinishRoleScan_ValidInput(t *testing.T) {
	r := &model.DiscoveryRole{ID: "admin", Name: "Admin"}
	ts := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339Nano)

	got, err := finishRoleScan(r, `["user1","user2","user3"]`, ts, ts)
	if err != nil {
		t.Fatalf("finishRoleScan: %v", err)
	}
	if len(got.Members) != 3 {
		t.Errorf("members len = %d, want 3", len(got.Members))
	}
}

func TestFinishRoleScan_InvalidJSON(t *testing.T) {
	r := &model.DiscoveryRole{ID: "r"}
	got, err := finishRoleScan(r, "bad", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Members == nil || len(got.Members) != 0 {
		t.Errorf("expected empty members, got %v", got.Members)
	}
}

func TestFinishRoleScan_NullMembers(t *testing.T) {
	r := &model.DiscoveryRole{ID: "r"}
	got, err := finishRoleScan(r, "null", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Members == nil {
		t.Error("members should not be nil after null JSON")
	}
}

func TestFinishRoleScan_RFC3339Fallback(t *testing.T) {
	r := &model.DiscoveryRole{ID: "r"}
	ts := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	got, err := finishRoleScan(r, "[]", ts, ts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

// --- finishClaimScan edge cases ---

func TestFinishClaimScan_NullMetadata(t *testing.T) {
	c := &model.Claim{ID: "c1"}
	got, err := finishClaimScan(c, "null", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Metadata == nil {
		t.Error("metadata should not be nil after null JSON")
	}
}


// --- tagsContain ---

func TestTagsContain(t *testing.T) {
	tags := []string{"bug", "high", "backend"}

	tests := []struct {
		tag  string
		want bool
	}{
		{"bug", true},
		{"BUG", true},   // case-insensitive
		{"Bug", true},
		{"missing", false},
		{"", false},
	}

	for _, tt := range tests {
		got := tagsContain(tags, tt.tag)
		if got != tt.want {
			t.Errorf("tagsContain(%v, %q) = %v, want %v", tags, tt.tag, got, tt.want)
		}
	}
}

func TestTagsContain_EmptySlice(t *testing.T) {
	if tagsContain(nil, "anything") {
		t.Error("tagsContain(nil, ...) should return false")
	}
	if tagsContain([]string{}, "anything") {
		t.Error("tagsContain([], ...) should return false")
	}
}
