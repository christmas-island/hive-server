package model_test

import (
	"context"
	"testing"

	"github.com/christmas-island/hive-server/internal/model"
)

func TestContextWithAgentID(t *testing.T) {
	ctx := model.ContextWithAgentID(context.Background(), "test-agent")
	got := model.AgentIDFromCtx(ctx)
	if got != "test-agent" {
		t.Errorf("AgentIDFromCtx() = %q, want %q", got, "test-agent")
	}
}

func TestAgentIDFromCtx_Empty(t *testing.T) {
	got := model.AgentIDFromCtx(context.Background())
	if got != "" {
		t.Errorf("AgentIDFromCtx() on empty ctx = %q, want empty", got)
	}
}

func TestContextWithSession(t *testing.T) {
	sc := model.SessionContext{
		SessionKey:    "key-1",
		SessionID:     "id-1",
		Channel:       "discord",
		SenderID:      "sender-1",
		SenderIsOwner: true,
		Sandboxed:     false,
	}
	ctx := model.ContextWithSession(context.Background(), sc)
	got := model.SessionFromCtx(ctx)
	if got != sc {
		t.Errorf("SessionFromCtx() = %+v, want %+v", got, sc)
	}
}

func TestSessionFromCtx_Empty(t *testing.T) {
	got := model.SessionFromCtx(context.Background())
	if got != (model.SessionContext{}) {
		t.Errorf("SessionFromCtx() on empty ctx = %+v, want zero value", got)
	}
}
