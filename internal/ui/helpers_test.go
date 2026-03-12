package ui

import (
	"testing"
	"time"
)

func TestTimeAgo(t *testing.T) {
	tests := []struct {
		name     string
		offset   time.Duration
		expected string
	}{
		{"just now", 10 * time.Second, "just now"},
		{"minutes", 5 * time.Minute, "5m ago"},
		{"hours", 3 * time.Hour, "3h ago"},
		{"one day", 25 * time.Hour, "1 day ago"},
		{"days", 72 * time.Hour, "3 days ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := timeAgo(time.Now().Add(-tt.offset))
			if result != tt.expected {
				t.Errorf("timeAgo(%v ago) = %q, want %q", tt.offset, result, tt.expected)
			}
		})
	}
}

func TestFormatTime(t *testing.T) {
	ts := time.Date(2026, 3, 12, 10, 30, 45, 0, time.UTC)
	got := formatTime(ts)
	want := "2026-03-12 10:30:45"
	if got != want {
		t.Errorf("formatTime() = %q, want %q", got, want)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		n        int
		expected string
	}{
		{"short", 10, "short"},
		{"a long string that exceeds", 10, "a long str…"},
		{"exactly10!", 10, "exactly10!"},
		{"", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := truncate(tt.input, tt.n)
			if result != tt.expected {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.n, result, tt.expected)
			}
		})
	}
}

func TestStatusClass(t *testing.T) {
	tests := []struct {
		status   string
		expected string
	}{
		{"online", "text-green-400"},
		{"offline", "text-red-400"},
		{"idle", "text-yellow-400"},
		{"open", "text-blue-400"},
		{"in_progress", "text-orange-400"},
		{"done", "text-green-400"},
		{"failed", "text-red-400"},
		{"active", "text-green-400"},
		{"expired", "text-red-400"},
		{"unknown", "text-gray-400"},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			result := statusClass(tt.status)
			if result != tt.expected {
				t.Errorf("statusClass(%q) = %q, want %q", tt.status, result, tt.expected)
			}
		})
	}
}
