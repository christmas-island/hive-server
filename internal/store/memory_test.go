//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/christmas-island/hive-server/internal/store"
)

func TestMemoryUpsert_Create(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	entry := &store.MemoryEntry{
		Key:     "test.key",
		Value:   "hello",
		AgentID: "agent-1",
		Tags:    []string{"foo", "bar"},
	}

	result, err := s.UpsertMemory(ctx, entry)
	if err != nil {
		t.Fatalf("UpsertMemory: %v", err)
	}
	if result.Key != "test.key" {
		t.Errorf("Key = %q, want %q", result.Key, "test.key")
	}
	if result.Version != 1 {
		t.Errorf("Version = %d, want 1", result.Version)
	}
	if result.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if result.UpdatedAt.IsZero() {
		t.Error("UpdatedAt is zero")
	}
	if len(result.Tags) != 2 {
		t.Errorf("Tags len = %d, want 2", len(result.Tags))
	}
}

func TestMemoryUpsert_Update(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	entry := &store.MemoryEntry{Key: "upd.key", Value: "v1", AgentID: "a1"}
	r1, err := s.UpsertMemory(ctx, entry)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if r1.Version != 1 {
		t.Fatalf("v1 version = %d, want 1", r1.Version)
	}

	entry.Value = "v2"
	r2, err := s.UpsertMemory(ctx, entry)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if r2.Version != 2 {
		t.Errorf("v2 version = %d, want 2", r2.Version)
	}
	if r2.Value != "v2" {
		t.Errorf("v2 value = %q, want %q", r2.Value, "v2")
	}
}

func TestMemoryUpsert_OptimisticConflict(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	entry := &store.MemoryEntry{Key: "oc.key", Value: "v1", AgentID: "a1"}
	_, err := s.UpsertMemory(ctx, entry)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// Simulate stale read by using wrong version.
	stale := &store.MemoryEntry{Key: "oc.key", Value: "stale", AgentID: "a1", Version: 99}
	_, err = s.UpsertMemory(ctx, stale)
	if err == nil {
		t.Fatal("expected ErrConflict, got nil")
	}
	if err != store.ErrConflict {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

func TestMemoryGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.UpsertMemory(ctx, &store.MemoryEntry{Key: "get.key", Value: "data", AgentID: "a1"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	entry, err := s.GetMemory(ctx, "get.key")
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if entry.Value != "data" {
		t.Errorf("Value = %q, want %q", entry.Value, "data")
	}
}

func TestMemoryGet_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetMemory(ctx, "no.such.key")
	if err != store.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMemoryList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, k := range []string{"a.1", "a.2", "b.1"} {
		_, err := s.UpsertMemory(ctx, &store.MemoryEntry{Key: k, Value: "v", AgentID: "agent-x", Tags: []string{"t1"}})
		if err != nil {
			t.Fatalf("upsert %q: %v", k, err)
		}
	}

	// List all.
	all, err := s.ListMemory(ctx, store.MemoryFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListMemory all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("len(all) = %d, want 3", len(all))
	}

	// Filter by agent.
	byAgent, err := s.ListMemory(ctx, store.MemoryFilter{Agent: "agent-x"})
	if err != nil {
		t.Fatalf("ListMemory by agent: %v", err)
	}
	if len(byAgent) != 3 {
		t.Errorf("len(byAgent) = %d, want 3", len(byAgent))
	}

	// Filter by prefix.
	byPrefix, err := s.ListMemory(ctx, store.MemoryFilter{Prefix: "a."})
	if err != nil {
		t.Fatalf("ListMemory by prefix: %v", err)
	}
	if len(byPrefix) != 2 {
		t.Errorf("len(byPrefix) = %d, want 2", len(byPrefix))
	}

	// Filter by tag.
	byTag, err := s.ListMemory(ctx, store.MemoryFilter{Tag: "t1"})
	if err != nil {
		t.Fatalf("ListMemory by tag: %v", err)
	}
	if len(byTag) != 3 {
		t.Errorf("len(byTag) = %d, want 3", len(byTag))
	}
}

func TestMemoryList_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	entries, err := s.ListMemory(ctx, store.MemoryFilter{})
	if err != nil {
		t.Fatalf("ListMemory: %v", err)
	}
	// Should return nil or empty slice, not error.
	_ = entries
}

func TestMemoryDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.UpsertMemory(ctx, &store.MemoryEntry{Key: "del.key", Value: "v", AgentID: "a1"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if err := s.DeleteMemory(ctx, "del.key"); err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}

	_, err = s.GetMemory(ctx, "del.key")
	if err != store.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestMemoryDelete_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	err := s.DeleteMemory(ctx, "ghost.key")
	if err != store.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMemoryNilTags(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	entry, err := s.UpsertMemory(ctx, &store.MemoryEntry{Key: "notags", Value: "v", AgentID: "a1", Tags: nil})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if entry.Tags == nil {
		t.Error("Tags should not be nil")
	}
}
