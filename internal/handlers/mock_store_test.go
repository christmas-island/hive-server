package handlers_test

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/christmas-island/hive-server/internal/model"
)

// mockStore is an in-memory implementation of handlers.Store for unit tests.
// All methods are safe for concurrent use.
type mockStore struct {
	mu       sync.Mutex
	memory   map[string]*model.MemoryEntry
	tasks    map[string]*model.Task
	notes    map[string][]string // task_id -> notes (ordered)
	agents   map[string]*model.Agent
	notesMeta map[string][]noteMeta // task_id -> note metadata
}

type noteMeta struct {
	note    string
	agentID string
}

func newMockStore() *mockStore {
	return &mockStore{
		memory:    make(map[string]*model.MemoryEntry),
		tasks:     make(map[string]*model.Task),
		notes:     make(map[string][]string),
		agents:    make(map[string]*model.Agent),
		notesMeta: make(map[string][]noteMeta),
	}
}

// --- Memory ---

func (m *mockStore) UpsertMemory(_ context.Context, entry *model.MemoryEntry) (*model.MemoryEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	if entry.Tags == nil {
		entry.Tags = []string{}
	}

	existing, exists := m.memory[entry.Key]
	if !exists {
		e := &model.MemoryEntry{
			Key:       entry.Key,
			Value:     entry.Value,
			AgentID:   entry.AgentID,
			Tags:      entry.Tags,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		}
		m.memory[entry.Key] = e
		return copyMemoryEntry(e), nil
	}

	// Optimistic concurrency check.
	if entry.Version > 0 && existing.Version != entry.Version {
		return nil, model.ErrConflict
	}

	existing.Value = entry.Value
	existing.AgentID = entry.AgentID
	existing.Tags = entry.Tags
	existing.Version++
	existing.UpdatedAt = now
	return copyMemoryEntry(existing), nil
}

func (m *mockStore) GetMemory(_ context.Context, key string) (*model.MemoryEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.memory[key]
	if !ok {
		return nil, model.ErrNotFound
	}
	return copyMemoryEntry(e), nil
}

func (m *mockStore) ListMemory(_ context.Context, f model.MemoryFilter) ([]*model.MemoryEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []*model.MemoryEntry
	for _, e := range m.memory {
		if f.Agent != "" && e.AgentID != f.Agent {
			continue
		}
		if f.Prefix != "" && !strings.HasPrefix(e.Key, f.Prefix) {
			continue
		}
		if f.Tag != "" && !sliceContains(e.Tags, f.Tag) {
			continue
		}
		result = append(result, copyMemoryEntry(e))
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockStore) DeleteMemory(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.memory[key]; !ok {
		return model.ErrNotFound
	}
	delete(m.memory, key)
	return nil
}

// --- Tasks ---

func (m *mockStore) CreateTask(_ context.Context, t *model.Task) (*model.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	task := &model.Task{
		ID:          uuid.New().String(),
		Title:       t.Title,
		Description: t.Description,
		Status:      model.TaskStatusOpen,
		Creator:     t.Creator,
		Assignee:    t.Assignee,
		Priority:    t.Priority,
		Tags:        t.Tags,
		Notes:       []string{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if task.Tags == nil {
		task.Tags = []string{}
	}
	m.tasks[task.ID] = task
	m.notes[task.ID] = []string{}
	return copyTask(task), nil
}

func (m *mockStore) GetTask(_ context.Context, id string) (*model.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.tasks[id]
	if !ok {
		return nil, model.ErrNotFound
	}
	result := copyTask(t)
	result.Notes = make([]string, len(m.notes[id]))
	copy(result.Notes, m.notes[id])
	return result, nil
}

func (m *mockStore) ListTasks(_ context.Context, f model.TaskFilter) ([]*model.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []*model.Task
	for _, t := range m.tasks {
		if f.Status != "" && string(t.Status) != f.Status {
			continue
		}
		if f.Assignee != "" && t.Assignee != f.Assignee {
			continue
		}
		if f.Creator != "" && t.Creator != f.Creator {
			continue
		}
		task := copyTask(t)
		task.Notes = make([]string, len(m.notes[t.ID]))
		copy(task.Notes, m.notes[t.ID])
		result = append(result, task)
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockStore) UpdateTask(_ context.Context, id string, upd model.TaskUpdate) (*model.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.tasks[id]
	if !ok {
		return nil, model.ErrNotFound
	}

	// Apply status transition.
	if upd.Status != nil && *upd.Status != t.Status {
		if !model.IsValidTransition(t.Status, *upd.Status) {
			return nil, model.ErrInvalidTransition
		}
		t.Status = *upd.Status
	}

	// Apply assignee change.
	if upd.Assignee != nil {
		t.Assignee = *upd.Assignee
	}

	// Append note.
	if upd.Note != nil && *upd.Note != "" {
		m.notes[id] = append(m.notes[id], *upd.Note)
	}

	t.UpdatedAt = time.Now().UTC()

	result := copyTask(t)
	result.Notes = make([]string, len(m.notes[id]))
	copy(result.Notes, m.notes[id])
	return result, nil
}

func (m *mockStore) DeleteTask(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.tasks[id]; !ok {
		return model.ErrNotFound
	}
	delete(m.tasks, id)
	delete(m.notes, id)
	return nil
}

// --- Agents ---

func (m *mockStore) Heartbeat(_ context.Context, id string, capabilities []string, status model.AgentStatus) (*model.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	if capabilities == nil {
		capabilities = []string{}
	}

	existing, ok := m.agents[id]
	if !ok {
		agent := &model.Agent{
			ID:            id,
			Name:          id,
			Status:        status,
			Capabilities:  capabilities,
			LastHeartbeat: now,
			RegisteredAt:  now,
		}
		m.agents[id] = agent
		return copyAgent(agent), nil
	}

	existing.Status = status
	existing.Capabilities = capabilities
	existing.LastHeartbeat = now
	return copyAgent(existing), nil
}

func (m *mockStore) GetAgent(_ context.Context, id string) (*model.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	a, ok := m.agents[id]
	if !ok {
		return nil, model.ErrNotFound
	}
	return copyAgent(a), nil
}

func (m *mockStore) ListAgents(_ context.Context) ([]*model.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []*model.Agent
	for _, a := range m.agents {
		result = append(result, copyAgent(a))
	}
	return result, nil
}

// --- Helpers ---

func copyMemoryEntry(e *model.MemoryEntry) *model.MemoryEntry {
	tags := make([]string, len(e.Tags))
	copy(tags, e.Tags)
	return &model.MemoryEntry{
		Key:       e.Key,
		Value:     e.Value,
		AgentID:   e.AgentID,
		Tags:      tags,
		Version:   e.Version,
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}
}

func copyTask(t *model.Task) *model.Task {
	tags := make([]string, len(t.Tags))
	copy(tags, t.Tags)
	notes := make([]string, len(t.Notes))
	copy(notes, t.Notes)
	return &model.Task{
		ID:          t.ID,
		Title:       t.Title,
		Description: t.Description,
		Status:      t.Status,
		Creator:     t.Creator,
		Assignee:    t.Assignee,
		Priority:    t.Priority,
		Tags:        tags,
		Notes:       notes,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

func copyAgent(a *model.Agent) *model.Agent {
	caps := make([]string, len(a.Capabilities))
	copy(caps, a.Capabilities)
	return &model.Agent{
		ID:            a.ID,
		Name:          a.Name,
		Status:        a.Status,
		Capabilities:  caps,
		LastHeartbeat: a.LastHeartbeat,
		RegisteredAt:  a.RegisteredAt,
	}
}

func sliceContains(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}
