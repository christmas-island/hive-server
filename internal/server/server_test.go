package server

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	cfg := Config{
		BindAddr:    ":0",
		DatabaseURL: "postgresql://localhost:26257/test",
		Token:       "test-token",
	}
	s := New(cfg)
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.config.BindAddr != ":0" {
		t.Errorf("BindAddr = %q, want %q", s.config.BindAddr, ":0")
	}
	if s.config.Token != "test-token" {
		t.Errorf("Token = %q, want %q", s.config.Token, "test-token")
	}
	if s.store != nil {
		t.Error("store should be nil before Run")
	}
	if s.srv != nil {
		t.Error("srv should be nil before Run")
	}
}

func TestNew_AllFields(t *testing.T) {
	cfg := Config{
		BindAddr:       ":9090",
		DatabaseURL:    "postgresql://db:5432/hive",
		Token:          "secret",
		OnlyClawsURL:   "https://only-claws.net",
		OnlyClawsToken: "relay-token",
	}
	s := New(cfg)
	if s.config.OnlyClawsURL != "https://only-claws.net" {
		t.Errorf("OnlyClawsURL = %q", s.config.OnlyClawsURL)
	}
	if s.config.OnlyClawsToken != "relay-token" {
		t.Errorf("OnlyClawsToken = %q", s.config.OnlyClawsToken)
	}
}

// mockClaimExpirer implements claimExpirer for testing.
type mockClaimExpirer struct {
	calls   atomic.Int64
	expired int64
	err     error
}

func (m *mockClaimExpirer) ExpireOldClaims(_ context.Context) (int64, error) {
	m.calls.Add(1)
	return m.expired, m.err
}

func TestRunClaimExpiry_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		runClaimExpiry(ctx, &mockClaimExpirer{})
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// exited cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("runClaimExpiry did not exit within 2s after context cancellation")
	}
}

func TestRunClaimExpiry_ErrorHandling(t *testing.T) {
	// Verify runClaimExpiry doesn't panic on errors from ExpireOldClaims.
	ctx, cancel := context.WithCancel(context.Background())
	mock := &mockClaimExpirer{err: errors.New("db gone")}

	done := make(chan struct{})
	go func() {
		runClaimExpiry(ctx, mock)
		close(done)
	}()

	// Cancel immediately — the error handling is exercised if the ticker fires,
	// but we primarily verify it doesn't panic and exits cleanly.
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runClaimExpiry did not exit within 2s")
	}
}

func TestConfig_ZeroValue(t *testing.T) {
	cfg := Config{}
	if cfg.BindAddr != "" {
		t.Errorf("default BindAddr = %q, want empty", cfg.BindAddr)
	}
	if cfg.Token != "" {
		t.Errorf("default Token = %q, want empty", cfg.Token)
	}
	if cfg.DatabaseURL != "" {
		t.Errorf("default DatabaseURL = %q, want empty", cfg.DatabaseURL)
	}
}
