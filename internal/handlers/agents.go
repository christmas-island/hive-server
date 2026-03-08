package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/christmas-island/hive-server/internal/store"
)

// handleAgentHeartbeat handles POST /api/v1/agents/{id}/heartbeat
func (a *API) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Capabilities []string `json:"capabilities"`
		Status       string   `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", nil)
		return
	}

	status := store.AgentStatus(req.Status)
	switch status {
	case store.AgentStatusOnline, store.AgentStatusIdle:
		// valid
	default:
		status = store.AgentStatusOnline
	}

	agent, err := a.store.Heartbeat(r.Context(), id, req.Capabilities, status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to record heartbeat", nil)
		return
	}
	writeJSON(w, http.StatusOK, agent)
}

// handleAgentList handles GET /api/v1/agents
func (a *API) handleAgentList(w http.ResponseWriter, r *http.Request) {
	agents, err := a.store.ListAgents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list agents", nil)
		return
	}
	if agents == nil {
		agents = []*store.Agent{}
	}
	writeJSON(w, http.StatusOK, agents)
}

// handleAgentGet handles GET /api/v1/agents/{id}
func (a *API) handleAgentGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, err := a.store.GetAgent(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "agent not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to get agent", nil)
		return
	}
	writeJSON(w, http.StatusOK, agent)
}
