package model

import "time"

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskStatusOpen       TaskStatus = "open"
	TaskStatusClaimed    TaskStatus = "claimed"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusDone       TaskStatus = "done"
	TaskStatusFailed     TaskStatus = "failed"
	TaskStatusCancelled  TaskStatus = "cancelled"
)

// validTransitions defines the allowed state machine moves.
var validTransitions = map[TaskStatus]map[TaskStatus]bool{
	TaskStatusOpen: {
		TaskStatusClaimed:   true,
		TaskStatusCancelled: true,
	},
	TaskStatusClaimed: {
		TaskStatusOpen:       true, // unclaim
		TaskStatusInProgress: true,
		TaskStatusCancelled:  true,
	},
	TaskStatusInProgress: {
		TaskStatusDone:   true,
		TaskStatusFailed: true,
		TaskStatusOpen:   true, // unblock/reassign
	},
}

// IsValidTransition reports whether moving from src to dst is allowed.
func IsValidTransition(src, dst TaskStatus) bool {
	if src == dst {
		return true
	}
	allowed, ok := validTransitions[src]
	if !ok {
		return false
	}
	return allowed[dst]
}

// Task is the full task record including appended notes.
type Task struct {
	ID             string     `json:"id"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	Status         TaskStatus `json:"status"`
	Creator        string     `json:"creator"`
	Assignee       string     `json:"assignee"`
	Priority       int        `json:"priority"`
	Tags           []string   `json:"tags"`
	Notes          []string   `json:"notes"`
	SessionContext `json:",inline"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TaskFilter holds optional filter parameters for listing tasks.
type TaskFilter struct {
	Status     string
	Assignee   string
	Creator    string
	SessionKey string
	Limit      int
	Offset     int
}

// TaskUpdate carries the fields that can be changed via PATCH.
type TaskUpdate struct {
	Status         *TaskStatus
	Assignee       *string
	Note           *string // appended if non-nil
	AgentID        string  // who is making the change (for note attribution)
	SessionContext         // session context of the caller
}
