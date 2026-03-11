package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/christmas-island/hive-server/internal/model"
)

// --- Agent scan helpers ---

func scanAgentRow(row *sql.Row) (*model.Agent, error) {
	var a model.Agent
	var capsRaw, hbStr, regStr string
	err := row.Scan(&a.ID, &a.Name, &a.Status, &a.Activity, &capsRaw, &hbStr, &regStr, &a.HiveLocalVersion, &a.HivePluginVersion, &a.Token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan agent: %w", err)
	}
	return finishAgentScan(&a, capsRaw, hbStr, regStr)
}

func scanAgentRows(rows *sql.Rows) (*model.Agent, error) {
	var a model.Agent
	var capsRaw, hbStr, regStr string
	if err := rows.Scan(&a.ID, &a.Name, &a.Status, &a.Activity, &capsRaw, &hbStr, &regStr, &a.HiveLocalVersion, &a.HivePluginVersion, &a.Token); err != nil {
		return nil, fmt.Errorf("scan agent row: %w", err)
	}
	return finishAgentScan(&a, capsRaw, hbStr, regStr)
}

func finishAgentScan(a *model.Agent, capsRaw, hbStr, regStr string) (*model.Agent, error) {
	if err := json.Unmarshal([]byte(capsRaw), &a.Capabilities); err != nil {
		a.Capabilities = []string{}
	}
	if a.Capabilities == nil {
		a.Capabilities = []string{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, hbStr); err == nil {
		a.LastHeartbeat = ts
	} else if ts, err := time.Parse(time.RFC3339, hbStr); err == nil {
		a.LastHeartbeat = ts
	}
	if ts, err := time.Parse(time.RFC3339Nano, regStr); err == nil {
		a.RegisteredAt = ts
	} else if ts, err := time.Parse(time.RFC3339, regStr); err == nil {
		a.RegisteredAt = ts
	}

	// Apply offline threshold override.
	if time.Since(a.LastHeartbeat) > offlineThreshold && a.Status != model.AgentStatusOffline {
		a.Status = model.AgentStatusOffline
	}
	return a, nil
}

// --- Memory scan helpers ---

func scanMemoryRow(row *sql.Row) (*model.MemoryEntry, error) {
	var e model.MemoryEntry
	var tagsRaw string
	var createdStr, updatedStr string
	err := row.Scan(&e.Key, &e.Value, &e.AgentID, &tagsRaw, &e.Version,
		&e.SessionKey, &e.SessionID, &e.Channel, &e.SenderID, &e.SenderIsOwner, &e.Sandboxed,
		&createdStr, &updatedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan memory: %w", err)
	}
	return finishMemoryScan(&e, tagsRaw, createdStr, updatedStr)
}

func scanMemoryRows(rows *sql.Rows) (*model.MemoryEntry, error) {
	var e model.MemoryEntry
	var tagsRaw string
	var createdStr, updatedStr string
	if err := rows.Scan(&e.Key, &e.Value, &e.AgentID, &tagsRaw, &e.Version,
		&e.SessionKey, &e.SessionID, &e.Channel, &e.SenderID, &e.SenderIsOwner, &e.Sandboxed,
		&createdStr, &updatedStr); err != nil {
		return nil, fmt.Errorf("scan memory row: %w", err)
	}
	return finishMemoryScan(&e, tagsRaw, createdStr, updatedStr)
}

func finishMemoryScan(e *model.MemoryEntry, tagsRaw, createdStr, updatedStr string) (*model.MemoryEntry, error) {
	if err := json.Unmarshal([]byte(tagsRaw), &e.Tags); err != nil {
		// Fall back to empty slice on bad JSON.
		e.Tags = []string{}
	}
	if e.Tags == nil {
		e.Tags = []string{}
	}
	t, err := time.Parse(time.RFC3339Nano, createdStr)
	if err != nil {
		t, err = time.Parse(time.RFC3339, createdStr)
	}
	if err == nil {
		e.CreatedAt = t
	}
	t, err = time.Parse(time.RFC3339Nano, updatedStr)
	if err != nil {
		t, err = time.Parse(time.RFC3339, updatedStr)
	}
	if err == nil {
		e.UpdatedAt = t
	}
	return e, nil
}

// --- Task scan helpers ---

func scanTaskRow(row *sql.Row) (*model.Task, error) {
	var t model.Task
	var tagsRaw, createdStr, updatedStr string
	err := row.Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.Creator, &t.Assignee,
		&t.Priority, &tagsRaw,
		&t.SessionKey, &t.SessionID, &t.Channel, &t.SenderID, &t.SenderIsOwner, &t.Sandboxed,
		&createdStr, &updatedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}
	return finishTaskScan(&t, tagsRaw, createdStr, updatedStr)
}

func scanTaskRows(rows *sql.Rows) (*model.Task, error) {
	var t model.Task
	var tagsRaw, createdStr, updatedStr string
	if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.Creator, &t.Assignee,
		&t.Priority, &tagsRaw,
		&t.SessionKey, &t.SessionID, &t.Channel, &t.SenderID, &t.SenderIsOwner, &t.Sandboxed,
		&createdStr, &updatedStr); err != nil {
		return nil, fmt.Errorf("scan task row: %w", err)
	}
	return finishTaskScan(&t, tagsRaw, createdStr, updatedStr)
}

func finishTaskScan(t *model.Task, tagsRaw, createdStr, updatedStr string) (*model.Task, error) {
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

// --- Claim scan helpers ---

func scanClaimRow(row *sql.Row) (*model.Claim, error) {
	var c model.Claim
	var metaRaw, claimedStr, expiresStr, updatedStr string
	err := row.Scan(&c.ID, &c.Type, &c.Resource, &c.AgentID, &c.Status, &metaRaw,
		&c.SessionKey, &c.SessionID, &c.Channel, &c.SenderID, &c.SenderIsOwner, &c.Sandboxed,
		&claimedStr, &expiresStr, &updatedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan claim: %w", err)
	}
	return finishClaimScan(&c, metaRaw, claimedStr, expiresStr, updatedStr)
}

func scanClaimRows(rows *sql.Rows) (*model.Claim, error) {
	var c model.Claim
	var metaRaw, claimedStr, expiresStr, updatedStr string
	if err := rows.Scan(&c.ID, &c.Type, &c.Resource, &c.AgentID, &c.Status, &metaRaw,
		&c.SessionKey, &c.SessionID, &c.Channel, &c.SenderID, &c.SenderIsOwner, &c.Sandboxed,
		&claimedStr, &expiresStr, &updatedStr); err != nil {
		return nil, fmt.Errorf("scan claim row: %w", err)
	}
	return finishClaimScan(&c, metaRaw, claimedStr, expiresStr, updatedStr)
}

func finishClaimScan(c *model.Claim, metaRaw, claimedStr, expiresStr, updatedStr string) (*model.Claim, error) {
	if err := json.Unmarshal([]byte(metaRaw), &c.Metadata); err != nil {
		c.Metadata = map[string]string{}
	}
	if c.Metadata == nil {
		c.Metadata = map[string]string{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, claimedStr); err == nil {
		c.ClaimedAt = ts
	} else if ts, err := time.Parse(time.RFC3339, claimedStr); err == nil {
		c.ClaimedAt = ts
	}
	if ts, err := time.Parse(time.RFC3339Nano, expiresStr); err == nil {
		c.ExpiresAt = ts
	} else if ts, err := time.Parse(time.RFC3339, expiresStr); err == nil {
		c.ExpiresAt = ts
	}
	if ts, err := time.Parse(time.RFC3339Nano, updatedStr); err == nil {
		c.UpdatedAt = ts
	} else if ts, err := time.Parse(time.RFC3339, updatedStr); err == nil {
		c.UpdatedAt = ts
	}
	return c, nil
}
