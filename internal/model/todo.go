package model

import "time"

// TodoStatus represents the lifecycle state of a todo.
type TodoStatus string

const (
	TodoStatusPending   TodoStatus = "pending"
	TodoStatusDone      TodoStatus = "done"
	TodoStatusSkipped   TodoStatus = "skipped"
	TodoStatusCancelled TodoStatus = "cancelled"
)

// Todo is an agent-scoped ephemeral work item.
type Todo struct {
	ID         string     `json:"id"`
	AgentID    string     `json:"agent_id"`
	Title      string     `json:"title"`
	Status     TodoStatus `json:"status"`
	SortOrder  int        `json:"sort_order"`
	ParentTask string     `json:"parent_task,omitempty"`
	Context    string     `json:"context,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// TodoFilter holds optional filters for listing todos.
type TodoFilter struct {
	AgentID string
	Status  string
	Limit   int
	Offset  int
}

// TodoUpdate carries the fields that can be changed via PATCH.
type TodoUpdate struct {
	Title   *string
	Status  *TodoStatus
	Context *string
}
