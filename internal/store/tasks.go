package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/christmas-island/hive-server/internal/model"
	"github.com/google/uuid"
)



// CreateTask inserts a new task and returns it.
// Uses RetryTx to handle CockroachDB serialization conflicts.
func (s *Store) CreateTask(ctx context.Context, t *model.Task) (*model.Task, error) {
	t.ID = uuid.New().String()
	t.Status = model.TaskStatusOpen
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
func (s *Store) GetTask(ctx context.Context, id string) (*model.Task, error) {
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
func (s *Store) ListTasks(ctx context.Context, f model.TaskFilter) ([]*model.Task, error) {
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

	var tasks []*model.Task
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
func (s *Store) UpdateTask(ctx context.Context, id string, upd model.TaskUpdate) (*model.Task, error) {
	err := s.RetryTx(ctx, func(tx *sql.Tx) error {
		// Fetch current task.
		var t model.Task
		var tagsRaw, createdStr, updatedStr string
		err := tx.QueryRowContext(ctx,
			`SELECT id, title, description, status, creator, assignee, priority, tags, created_at, updated_at
             FROM tasks WHERE id = $1`, id,
		).Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.Creator, &t.Assignee,
			&t.Priority, &tagsRaw, &createdStr, &updatedStr)
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("fetch task: %w", err)
		}

		now := time.Now().UTC()

		// Apply status transition.
		if upd.Status != nil && *upd.Status != t.Status {
			if !model.IsValidTransition(t.Status, *upd.Status) {
				return model.ErrInvalidTransition
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
			return model.ErrNotFound
		}
		return nil
	})
}

// loadTaskNotes fetches all notes for a task and attaches them.
func (s *Store) loadTaskNotes(ctx context.Context, t *model.Task) error {
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
