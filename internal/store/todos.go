package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/christmas-island/hive-server/internal/model"
	"github.com/christmas-island/hive-server/internal/timing"
	"github.com/google/uuid"
)

// CreateTodo inserts a new todo and returns it.
func (s *Store) CreateTodo(ctx context.Context, t *model.Todo) (*model.Todo, error) {
	defer timing.TrackDB(ctx, time.Now())
	t.ID = uuid.New().String()
	t.Status = model.TodoStatusPending
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now

	err := s.RetryTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO todos (id, agent_id, title, status, sort_order, parent_task, context, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			t.ID, t.AgentID, t.Title, string(t.Status), t.SortOrder, t.ParentTask, t.Context,
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("insert todo: %w", err)
	}
	return t, nil
}

// GetTodo retrieves a todo by ID.
func (s *Store) GetTodo(ctx context.Context, id string) (*model.Todo, error) {
	defer timing.TrackDB(ctx, time.Now())
	row := s.db.QueryRowContext(ctx,
		`SELECT id, agent_id, title, status, sort_order, parent_task, context, created_at, updated_at
		 FROM todos WHERE id = $1`, id,
	)
	return scanTodoRow(row)
}

// ListTodos returns todos filtered by agent_id (always required) and optional status.
func (s *Store) ListTodos(ctx context.Context, f model.TodoFilter) ([]*model.Todo, error) {
	defer timing.TrackDB(ctx, time.Now())
	q := `SELECT id, agent_id, title, status, sort_order, parent_task, context, created_at, updated_at
	      FROM todos WHERE agent_id = $1`
	args := []any{f.AgentID}
	argIdx := 2

	if f.Status != "" {
		q += fmt.Sprintf(` AND status = $%d`, argIdx)
		args = append(args, f.Status)
		argIdx++
	}
	q += ` ORDER BY sort_order ASC, created_at ASC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	} else {
		q += ` LIMIT 100`
	}
	if f.Offset > 0 {
		q += fmt.Sprintf(` OFFSET %d`, f.Offset)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list todos: %w", err)
	}
	defer rows.Close()

	var todos []*model.Todo
	for rows.Next() {
		t, err := scanTodoRows(rows)
		if err != nil {
			return nil, err
		}
		todos = append(todos, t)
	}
	return todos, rows.Err()
}

// UpdateTodo applies a TodoUpdate to a todo. Returns model.ErrNotFound if missing.
func (s *Store) UpdateTodo(ctx context.Context, id string, upd model.TodoUpdate) (*model.Todo, error) {
	defer timing.TrackDB(ctx, time.Now())
	err := s.RetryTx(ctx, func(tx *sql.Tx) error {
		var exists bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM todos WHERE id = $1)`, id).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check todo: %w", err)
		}
		if !exists {
			return model.ErrNotFound
		}

		now := time.Now().UTC()
		if upd.Status != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE todos SET status = $1, updated_at = $2 WHERE id = $3`,
				string(*upd.Status), now.Format(time.RFC3339Nano), id); err != nil {
				return fmt.Errorf("update todo status: %w", err)
			}
		}
		if upd.Title != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE todos SET title = $1, updated_at = $2 WHERE id = $3`,
				*upd.Title, now.Format(time.RFC3339Nano), id); err != nil {
				return fmt.Errorf("update todo title: %w", err)
			}
		}
		if upd.Context != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE todos SET context = $1, updated_at = $2 WHERE id = $3`,
				*upd.Context, now.Format(time.RFC3339Nano), id); err != nil {
				return fmt.Errorf("update todo context: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetTodo(ctx, id)
}

// DeleteTodo removes a todo by ID.
func (s *Store) DeleteTodo(ctx context.Context, id string) error {
	defer timing.TrackDB(ctx, time.Now())
	return s.RetryTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM todos WHERE id = $1`, id)
		if err != nil {
			return fmt.Errorf("delete todo: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return model.ErrNotFound
		}
		return nil
	})
}

// PruneDoneTodos deletes completed/skipped/cancelled todos older than maxAge for a given agent.
// If agentID is empty, prunes across all agents.
func (s *Store) PruneDoneTodos(ctx context.Context, agentID string, maxAge time.Duration) (int64, error) {
	defer timing.TrackDB(ctx, time.Now())
	cutoff := time.Now().UTC().Add(-maxAge).Format(time.RFC3339Nano)
	var count int64
	err := s.RetryTx(ctx, func(tx *sql.Tx) error {
		q := `DELETE FROM todos WHERE status != 'pending' AND updated_at < $1`
		args := []any{cutoff}
		if agentID != "" {
			q += ` AND agent_id = $2`
			args = append(args, agentID)
		}
		res, err := tx.ExecContext(ctx, q, args...)
		if err != nil {
			return fmt.Errorf("prune done todos: %w", err)
		}
		count, _ = res.RowsAffected()
		return nil
	})
	return count, err
}

// ReorderTodos sets the sort_order for the given todo IDs in sequence.
// All IDs must belong to the specified agent.
func (s *Store) ReorderTodos(ctx context.Context, agentID string, ids []string) error {
	defer timing.TrackDB(ctx, time.Now())
	return s.RetryTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		for i, id := range ids {
			res, err := tx.ExecContext(ctx,
				`UPDATE todos SET sort_order = $1, updated_at = $2 WHERE id = $3 AND agent_id = $4`,
				i, now, id, agentID,
			)
			if err != nil {
				return fmt.Errorf("reorder todo %s: %w", id, err)
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				return model.ErrNotFound
			}
		}
		return nil
	})
}

// --- scan helpers ---

func scanTodoRow(row *sql.Row) (*model.Todo, error) {
	var t model.Todo
	var createdStr, updatedStr string
	err := row.Scan(&t.ID, &t.AgentID, &t.Title, &t.Status, &t.SortOrder,
		&t.ParentTask, &t.Context, &createdStr, &updatedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan todo: %w", err)
	}
	return finishTodoScan(&t, createdStr, updatedStr)
}

func scanTodoRows(rows *sql.Rows) (*model.Todo, error) {
	var t model.Todo
	var createdStr, updatedStr string
	if err := rows.Scan(&t.ID, &t.AgentID, &t.Title, &t.Status, &t.SortOrder,
		&t.ParentTask, &t.Context, &createdStr, &updatedStr); err != nil {
		return nil, fmt.Errorf("scan todo row: %w", err)
	}
	return finishTodoScan(&t, createdStr, updatedStr)
}

func finishTodoScan(t *model.Todo, createdStr, updatedStr string) (*model.Todo, error) {
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
