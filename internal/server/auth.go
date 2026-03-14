package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/christmas-island/hive-server/internal/model"
)

// AgentLookup is the minimal interface needed by auth middleware to validate
// per-agent tokens.
type AgentLookup interface {
	GetAgent(ctx context.Context, id string) (*model.Agent, error)
}

// AuthMiddleware validates the Bearer token and extracts X-Agent-ID and session
// context headers into the request context.
// Supports both a global token (HIVE_TOKEN) and per-agent tokens via AgentLookup.
func AuthMiddleware(token string, agents AgentLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Parse bearer token from Authorization header.
			authHeader := r.Header.Get("Authorization")
			bearerToken := ""
			if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
				bearerToken = authHeader[7:]
			}

			agentID := r.Header.Get("X-Agent-ID")

			// Check auth: either global token or per-agent token.
			authValid := false
			if token != "" && bearerToken == token {
				authValid = true
			} else if bearerToken != "" && agentID != "" {
				agent, err := agents.GetAgent(r.Context(), agentID)
				if err == nil && agent.Token != "" && bearerToken == agent.Token {
					authValid = true
				}
			}

			// If auth is required and not valid, reject.
			if (token != "" || bearerToken != "") && !authValid {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":   "unauthorized",
					"message": "invalid or missing bearer token",
				})
				return
			}

			// Inject agent ID and session context into context.
			ctx := r.Context()
			if agentID != "" {
				ctx = model.ContextWithAgentID(ctx, agentID)
			}

			sc := model.SessionContext{
				SessionKey:    r.Header.Get("X-Session-Key"),
				SessionID:     r.Header.Get("X-Session-ID"),
				Channel:       r.Header.Get("X-Channel"),
				SenderID:      r.Header.Get("X-Sender-ID"),
				SenderIsOwner: r.Header.Get("X-Sender-Is-Owner") == "true",
				Sandboxed:     r.Header.Get("X-Sandboxed") == "true",
			}
			ctx = model.ContextWithSession(ctx, sc)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
