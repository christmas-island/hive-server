package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

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

// ErrInvalidTransition is returned when a state transition is not allowed.
var ErrInvalidTransition = errors.New("invalid status transition")

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
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      TaskStatus `json:"status"`
	Creator     string     `json:"creator"`
	Assignee    string     `json:"assignee"`
	Priority    int        `json:"priority"`
	Tags        []string   `json:"tags"`
	Notes       []string   `json:"notes"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// TaskFilter holds optional filter parameters for listing tasks.
type TaskFilter struct {
	Status   string
	Assignee string
	Creator  string
	Limit    int
	Offset   int
}

// TaskUpdate carries the fields that can be changed via PATCH.
type TaskUpdate struct {
	Status   *TaskStatus
	Assignee *string
	Note     *string // appended if non-nil
	AgentID  string  // who is making the change (for note attribution)
}

// CreateTask inserts a new task and returns it.
// Uses RetryTx to handle CockroachDB serialization conflicts.
func (s *Store) CreateTask(ctx context.Context, t *Task) (*Task, error) {
	t.ID = uuid.New().String()
	t.Status = TaskStatusOpen
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now

	tagsJSON, err := json.Marshal(t.Tags)
	if err != nil {
		return nil, fmt.Errorf("marshal tags: %w", err)
	}
	if t.Tags == nil {
		tagsJSON = []byte(`[]`)
	}

	err = s.RetryTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO tasks (id, title, description, status, creator, assignee, priority, tags, created_at, updated_at)
             VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			t.ID, t.Title, t.Description, string(t.Status), t.Creator,
			t.Assignee, t.Priority, string(tagsJSON),
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("insert task: %w", err)
	}

	t.Notes = []string{}
	return t, nil
}

// GetTask retrieves a task by ID, including its notes.
func (s *Store) GetTask(ctx context.Context, id string) (*Task, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
         FROM tasks WHERE id = $1`,
		id,
	)
	t, err := scanTaskRow(row)
	if err != nil {
		return nil, err
	}
	if err := s.loadTaskNotes(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// ListTasks returns tasks matching the filter.
func (s *Store) ListTasks(ctx context.Context, f TaskFilter) ([]*Task, error) {
	q := `SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
          FROM tasks WHERE 1=1`
	args := []any{}
	argIdx := 1

	if f.Status != "" {
		q += fmt.Sprintf(` AND status = $%d`, argIdx)
		args = append(args, f.Status)
		argIdx++
	}
	if f.Assignee != "" {
		q += fmt.Sprintf(` AND assignee = $%d`, argIdx)
		args = append(args, f.Assignee)
		argIdx++
	}
	if f.Creator != "" {
		q += fmt.Sprintf(` AND creator = $%d`, argIdx)
		args = append(args, f.Creator)
		argIdx++
	}
	q += ` ORDER BY created_at DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	} else {
		q += ` LIMIT 50`
	}
	if f.Offset > 0 {
		q += fmt.Sprintf(` OFFSET %d`, f.Offset)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}

	var tasks []*Task
	for rows.Next() {
		t, err := scanTaskRows(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close() // Free the connection before loading notes.

	for _, t := range tasks {
		if err := s.loadTaskNotes(ctx, t); err != nil {
			return nil, err
		}
	}
	return tasks, nil
}

// UpdateTask applies a TaskUpdate to a task, enforcing state machine rules.
// Uses RetryTx to handle CockroachDB serialization conflicts.
func (s *Store) UpdateTask(ctx context.Context, id string, upd TaskUpdate) (*Task, error) {
	err := s.RetryTx(ctx, func(tx *sql.Tx) error {
		// Fetch current task.
		var t Task
		var tagsRaw, createdStr, updatedStr string
		err := tx.QueryRowContext(ctx,
			`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
             FROM tasks WHERE id = $1`, id,
		).Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.Creator, &t.Assignee,
			&t.Priority, &tagsRaw, &createdStr, &updatedStr)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("fetch task: %w", err)
		}

		now := time.Now().UTC()

		// Apply status transition.
		if upd.Status != nil && *upd.Status != t.Status {
			if !IsValidTransition(t.Status, *upd.Status) {
				return ErrInvalidTransition
			}
			t.Status = *upd.Status
		}

		// Apply assignee change.
		if upd.Assignee != nil {
			t.Assignee = *upd.Assignee
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE tasks SET status = $1, assignee = $2, updated_at = $3 WHERE id = $4`,
			string(t.Status), t.Assignee, now.Format(time.RFC3339Nano), id,
		); err != nil {
			return fmt.Errorf("update task: %w", err)
		}

		// Append note if provided.
		if upd.Note != nil && *upd.Note != "" {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO task_notes (task_id, note, agent_id, created_at) VALUES ($1, $2, $3, $4)`,
				id, *upd.Note, upd.AgentID, now.Format(time.RFC3339Nano),
			); err != nil {
				return fmt.Errorf("insert task note: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return s.GetTask(ctx, id)
}

// DeleteTask removes a task (and its notes via cascade) by ID.
// Uses RetryTx to handle CockroachDB serialization conflicts.
func (s *Store) DeleteTask(ctx context.Context, id string) error {
	return s.RetryTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id = $1`, id)
		if err != nil {
			return fmt.Errorf("delete task: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// loadTaskNotes fetches all notes for a task and attaches them.
func (s *Store) loadTaskNotes(ctx context.Context, t *Task) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT note FROM task_notes WHERE task_id = $1 ORDER BY created_at ASC, id ASC`,
		t.ID,
	)
	if err != nil {
		return fmt.Errorf("load notes: %w", err)
	}
	defer rows.Close()

	t.Notes = []string{}
	for rows.Next() {
		var note string
		if err := rows.Scan(&note); err != nil {
			return fmt.Errorf("scan note: %w", err)
		}
		t.Notes = append(t.Notes, note)
	}
	return rows.Err()
}

// scanTaskRow scans a *sql.Row into a Task.
func scanTaskRow(row *sql.Row) (*Task, error) {
	var t Task
	var tagsRaw, createdStr, updatedStr string
	err := row.Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.Creator, &t.Assignee,
		&t.Priority, &tagsRaw, &createdStr, &updatedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}
	return finishTaskScan(&t, tagsRaw, createdStr, updatedStr)
}

// scanTaskRows scans a *sql.Rows into a Task.
func scanTaskRows(rows *sql.Rows) (*Task, error) {
	var t Task
	var tagsRaw, createdStr, updatedStr string
	if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.Creator, &t.Assignee,
		&t.Priority, &tagsRaw, &createdStr, &updatedStr); err != nil {
		return nil, fmt.Errorf("scan task row: %w", err)
	}
	return finishTaskScan(&t, tagsRaw, createdStr, updatedStr)
}

func finishTaskScan(t *Task, tagsRaw, createdStr, updatedStr string) (*Task, error) {
	if err := json.Unmarshal([]byte(tagsRaw), &t.Tags); err != nil {
		t.Tags = []string{}
	}
	if t.Tags == nil {
		t.Tags = []string{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, createdStr); err == nil {
		t.CreatedAt = ts
	} else if ts, err := time.Parse(time.RFC3339, createdStr); err == nil {
		t.CreatedAt = ts
	}
	if ts, err := time.Parse(time.RFC3339Nano, updatedStr); err == nil {
		t.UpdatedAt = ts
	} else if ts, err := time.Parse(time.RFC3339, updatedStr); err == nil {
		t.UpdatedAt = ts
	}
	return t, nil
}
