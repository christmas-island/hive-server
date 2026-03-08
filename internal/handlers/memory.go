package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/christmas-island/hive-server/internal/store"
)

// handleMemoryUpsert handles POST /api/v1/memory
func (a *API) handleMemoryUpsert(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key     string   `json:"key"`
		Value   string   `json:"value"`
		Tags    []string `json:"tags"`
		Version int64    `json:"version"` // 0 means no version check
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", nil)
		return
	}
	if req.Key == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "key is required", nil)
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "value is required", nil)
		return
	}

	aid := agentID(r)
	entry := &store.MemoryEntry{
		Key:     req.Key,
		Value:   req.Value,
		AgentID: aid,
		Tags:    req.Tags,
		Version: req.Version,
	}

	result, err := a.store.UpsertMemory(r.Context(), entry)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "conflict", "version conflict: stale data", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to upsert memory", nil)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleMemoryGet handles GET /api/v1/memory/{key}
func (a *API) handleMemoryGet(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	entry, err := a.store.GetMemory(r.Context(), key)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "memory entry not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to get memory", nil)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

// handleMemoryList handles GET /api/v1/memory
func (a *API) handleMemoryList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.MemoryFilter{
		Tag:    q.Get("tag"),
		Agent:  q.Get("agent"),
		Prefix: q.Get("prefix"),
	}
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "limit must be a non-negative integer", nil)
			return
		}
		f.Limit = n
	}
	if o := q.Get("offset"); o != "" {
		n, err := strconv.Atoi(o)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "offset must be a non-negative integer", nil)
			return
		}
		f.Offset = n
	}

	entries, err := a.store.ListMemory(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list memory", nil)
		return
	}
	if entries == nil {
		entries = []*store.MemoryEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// handleMemoryDelete handles DELETE /api/v1/memory/{key}
func (a *API) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	err := a.store.DeleteMemory(r.Context(), key)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "memory entry not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete memory", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
