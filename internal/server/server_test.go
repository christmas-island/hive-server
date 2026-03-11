package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
	// fn overrides the default behaviour when set.
	fn func(context.Context) (int64, error)
}

func (m *mockClaimExpirer) ExpireOldClaims(ctx context.Context) (int64, error) {
	m.calls.Add(1)
	if m.fn != nil {
		return m.fn(ctx)
	}
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

func TestRun_InvalidDatabaseURL(t *testing.T) {
	// Run() should return an error if the database URL is invalid / unreachable.
	// This tests the error path through store.New().
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := New(Config{
		DatabaseURL: "postgresql://invalid:badpass@localhost:1/doesnotexist?sslmode=disable",
		BindAddr:    "127.0.0.1:0",
	})

	err := srv.Run(ctx)
	if err == nil {
		t.Fatal("expected error from Run() with invalid database URL, got nil")
	}
}

func TestRunClaimExpiry_TickerPath_Success(t *testing.T) {
	// Override the interval to a very short duration so we hit the ticker case.
	orig := claimExpiryInterval
	claimExpiryInterval = 5 * time.Millisecond
	defer func() { claimExpiryInterval = orig }()

	ctx, cancel := context.WithCancel(context.Background())

	called := make(chan struct{}, 5)
	ce := &mockClaimExpirer{
		fn: func(ctx context.Context) (int64, error) {
			select {
			case called <- struct{}{}:
			default:
			}
			return 3, nil // >0 triggers the log.Info path
		},
	}

	done := make(chan struct{})
	go func() {
		runClaimExpiry(ctx, ce)
		close(done)
	}()

	// Wait for at least one tick.
	select {
	case <-called:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for claim expiry tick")
	}
	cancel()
	<-done
}

func TestBuildMux(t *testing.T) {
	// buildMux should return a valid mux that routes /version correctly.
	mux := buildMux(nil, "", nil)
	if mux == nil {
		t.Fatal("buildMux returned nil")
	}

	// Verify /version endpoint is wired up.
	SetVersionInfo("test-build", "cafebabe", "2026-03-11")
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /version status = %d, want %d", w.Code, http.StatusOK)
	}
	var vi VersionInfo
	if err := json.NewDecoder(w.Body).Decode(&vi); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if vi.Version != "test-build" {
		t.Errorf("Version = %q, want test-build", vi.Version)
	}
}

func TestLogVersionInfo(t *testing.T) {
	// Set known values and verify logVersionInfo doesn't panic.
	SetVersionInfo("1.0.0", "abc123", "2026-03-11")
	logVersionInfo() // Should log without error.

	// Verify the version info is still correct after logging.
	vi := GetVersionInfo()
	if vi.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", vi.Version)
	}
	if vi.Commit != "abc123" {
		t.Errorf("Commit = %q, want abc123", vi.Commit)
	}
}

func TestRunClaimExpiry_TickerPath_ZeroExpired(t *testing.T) {
	// n == 0 takes a different branch (no log.Info)
	orig := claimExpiryInterval
	claimExpiryInterval = 5 * time.Millisecond
	defer func() { claimExpiryInterval = orig }()

	ctx, cancel := context.WithCancel(context.Background())

	called := make(chan struct{}, 5)
	ce := &mockClaimExpirer{
		fn: func(ctx context.Context) (int64, error) {
			select {
			case called <- struct{}{}:
			default:
			}
			return 0, nil // zero items expired
		},
	}

	done := make(chan struct{})
	go func() {
		runClaimExpiry(ctx, ce)
		close(done)
	}()

	select {
	case <-called:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for tick")
	}
	cancel()
	<-done
}
