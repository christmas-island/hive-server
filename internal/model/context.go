package model

import "context"

// ctxKey is the context key type for request-scoped values.
type ctxKey string

const ctxKeyAgentID ctxKey = "agent_id"
const ctxKeySession ctxKey = "session_context"

// ContextWithAgentID returns a new context with the given agent ID.
func ContextWithAgentID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyAgentID, id)
}

// AgentIDFromCtx extracts the agent ID from the request context.
func AgentIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyAgentID).(string)
	return id
}

// ContextWithSession returns a new context with the given session context.
func ContextWithSession(ctx context.Context, sc SessionContext) context.Context {
	return context.WithValue(ctx, ctxKeySession, sc)
}

// SessionFromCtx extracts the SessionContext from the request context.
func SessionFromCtx(ctx context.Context) SessionContext {
	sc, _ := ctx.Value(ctxKeySession).(SessionContext)
	return sc
}
