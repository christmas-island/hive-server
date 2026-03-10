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
	mu              sync.Mutex
	memory          map[string]*model.MemoryEntry
	tasks           map[string]*model.Task
	notes           map[string][]string // task_id -> notes (ordered)
	agents          map[string]*model.Agent
	notesMeta       map[string][]noteMeta // task_id -> note metadata
	discoveryAgents map[string]*model.DiscoveryAgent
	channels        map[string]*model.DiscoveryChannel
	roles           map[string]*model.DiscoveryRole
}

type noteMeta struct {
	note    string
	agentID string
}

func newMockStore() *mockStore {
	return &mockStore{
		memory:          make(map[string]*model.MemoryEntry),
		tasks:           make(map[string]*model.Task),
		notes:           make(map[string][]string),
		agents:          make(map[string]*model.Agent),
		notesMeta:       make(map[string][]noteMeta),
		discoveryAgents: make(map[string]*model.DiscoveryAgent),
		channels:        make(map[string]*model.DiscoveryChannel),
		roles:           make(map[string]*model.DiscoveryRole),
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

// --- Discovery ---

func (m *mockStore) UpsertChannel(_ context.Context, ch *model.DiscoveryChannel) (*model.DiscoveryChannel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	c := *ch
	if c.Members == nil {
		c.Members = []string{}
	}
	if existing, ok := m.channels[ch.ID]; ok {
		c.CreatedAt = existing.CreatedAt
	} else {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	m.channels[ch.ID] = &c
	result := c
	return &result, nil
}

func (m *mockStore) GetChannel(_ context.Context, id string) (*model.DiscoveryChannel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch, ok := m.channels[id]
	if !ok {
		return nil, model.ErrNotFound
	}
	result := *ch
	return &result, nil
}

func (m *mockStore) ListChannels(_ context.Context) ([]*model.DiscoveryChannel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*model.DiscoveryChannel
	for _, ch := range m.channels {
		c := *ch
		result = append(result, &c)
	}
	return result, nil
}

func (m *mockStore) DeleteChannel(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.channels[id]; !ok {
		return model.ErrNotFound
	}
	delete(m.channels, id)
	return nil
}

func (m *mockStore) UpsertRole(_ context.Context, role *model.DiscoveryRole) (*model.DiscoveryRole, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	r := *role
	if r.Members == nil {
		r.Members = []string{}
	}
	if existing, ok := m.roles[role.ID]; ok {
		r.CreatedAt = existing.CreatedAt
	} else {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	m.roles[role.ID] = &r
	result := r
	return &result, nil
}

func (m *mockStore) GetRole(_ context.Context, id string) (*model.DiscoveryRole, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.roles[id]
	if !ok {
		return nil, model.ErrNotFound
	}
	result := *r
	return &result, nil
}

func (m *mockStore) ListRoles(_ context.Context) ([]*model.DiscoveryRole, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*model.DiscoveryRole
	for _, r := range m.roles {
		role := *r
		result = append(result, &role)
	}
	return result, nil
}

func (m *mockStore) DeleteRole(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.roles[id]; !ok {
		return model.ErrNotFound
	}
	delete(m.roles, id)
	return nil
}

func (m *mockStore) UpsertAgentMeta(_ context.Context, id string, meta *model.DiscoveryAgentMeta) (*model.DiscoveryAgent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	agent, ok := m.agents[id]
	if !ok {
		return nil, model.ErrNotFound
	}
	metaCopy := *meta
	if metaCopy.Channels == nil {
		metaCopy.Channels = []string{}
	}
	da := &model.DiscoveryAgent{
		Agent:              copyAgent(agent),
		DiscoveryAgentMeta: &metaCopy,
	}
	m.discoveryAgents[id] = da
	result := *da
	metaResult := *da.DiscoveryAgentMeta
	result.DiscoveryAgentMeta = &metaResult
	return &result, nil
}

func (m *mockStore) GetDiscoveryAgent(_ context.Context, id string) (*model.DiscoveryAgent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	agent, ok := m.agents[id]
	if !ok {
		return nil, model.ErrNotFound
	}
	da, ok := m.discoveryAgents[id]
	if !ok {
		// Agent exists but no meta set yet — return with empty meta.
		return &model.DiscoveryAgent{
			Agent:              copyAgent(agent),
			DiscoveryAgentMeta: &model.DiscoveryAgentMeta{Channels: []string{}},
		}, nil
	}
	result := *da
	metaResult := *da.DiscoveryAgentMeta
	result.DiscoveryAgentMeta = &metaResult
	return &result, nil
}

func (m *mockStore) ListDiscoveryAgents(_ context.Context) ([]*model.DiscoveryAgent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*model.DiscoveryAgent
	for id, agent := range m.agents {
		if da, ok := m.discoveryAgents[id]; ok {
			r := *da
			meta := *da.DiscoveryAgentMeta
			r.DiscoveryAgentMeta = &meta
			result = append(result, &r)
		} else {
			result = append(result, &model.DiscoveryAgent{
				Agent:              copyAgent(agent),
				DiscoveryAgentMeta: &model.DiscoveryAgentMeta{Channels: []string{}},
			})
		}
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
