package main

import (
	"testing"

	"github.com/christmas-island/hive-server/internal/server"
)

func TestApp(t *testing.T) {
	cmd := App()
	if cmd == nil {
		t.Fatal("App returned nil")
	}
	if cmd.Use != "hive-server" {
		t.Errorf("Use = %q, want hive-server", cmd.Use)
	}
	if cmd.Version != version {
		t.Errorf("Version = %q, want %q", cmd.Version, version)
	}
}

func TestVersionDefaults(t *testing.T) {
	if version != "dev" {
		t.Errorf("version = %q, want dev", version)
	}
	if commit != "none" {
		t.Errorf("commit = %q, want none", commit)
	}
	if date != "unknown" {
		t.Errorf("date = %q, want unknown", date)
	}
}

func TestApp_SetsVersionInfo(t *testing.T) {
	// App() calls SetVersionInfo — verify it propagated.
	_ = App()
	vi := server.GetVersionInfo()
	if vi.Version != version {
		t.Errorf("VersionInfo.Version = %q, want %q", vi.Version, version)
	}
	if vi.Commit != commit {
		t.Errorf("VersionInfo.Commit = %q, want %q", vi.Commit, commit)
	}
	if vi.Date != date {
		t.Errorf("VersionInfo.Date = %q, want %q", vi.Date, date)
	}
}
