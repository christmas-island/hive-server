package model

import "testing"

func TestIsValidTransition(t *testing.T) {
	tests := []struct {
		name string
		src  TaskStatus
		dst  TaskStatus
		want bool
	}{
		// Same-state transitions are always valid.
		{"open→open (self)", TaskStatusOpen, TaskStatusOpen, true},
		{"done→done (self)", TaskStatusDone, TaskStatusDone, true},
		{"failed→failed (self)", TaskStatusFailed, TaskStatusFailed, true},

		// Valid transitions from open.
		{"open→claimed", TaskStatusOpen, TaskStatusClaimed, true},
		{"open→cancelled", TaskStatusOpen, TaskStatusCancelled, true},

		// Invalid transitions from open.
		{"open→in_progress", TaskStatusOpen, TaskStatusInProgress, false},
		{"open→done", TaskStatusOpen, TaskStatusDone, false},
		{"open→failed", TaskStatusOpen, TaskStatusFailed, false},

		// Valid transitions from claimed.
		{"claimed→open (unclaim)", TaskStatusClaimed, TaskStatusOpen, true},
		{"claimed→in_progress", TaskStatusClaimed, TaskStatusInProgress, true},
		{"claimed→cancelled", TaskStatusClaimed, TaskStatusCancelled, true},

		// Invalid transitions from claimed.
		{"claimed→done", TaskStatusClaimed, TaskStatusDone, false},
		{"claimed→failed", TaskStatusClaimed, TaskStatusFailed, false},

		// Valid transitions from in_progress.
		{"in_progress→done", TaskStatusInProgress, TaskStatusDone, true},
		{"in_progress→failed", TaskStatusInProgress, TaskStatusFailed, true},
		{"in_progress→open (unblock)", TaskStatusInProgress, TaskStatusOpen, true},

		// Invalid transitions from in_progress.
		{"in_progress→claimed", TaskStatusInProgress, TaskStatusClaimed, false},
		{"in_progress→cancelled", TaskStatusInProgress, TaskStatusCancelled, false},

		// Terminal states: done, failed, cancelled have no outgoing transitions.
		{"done→open", TaskStatusDone, TaskStatusOpen, false},
		{"done→claimed", TaskStatusDone, TaskStatusClaimed, false},
		{"done→in_progress", TaskStatusDone, TaskStatusInProgress, false},
		{"done→failed", TaskStatusDone, TaskStatusFailed, false},
		{"done→cancelled", TaskStatusDone, TaskStatusCancelled, false},
		{"failed→open", TaskStatusFailed, TaskStatusOpen, false},
		{"failed→claimed", TaskStatusFailed, TaskStatusClaimed, false},
		{"failed→done", TaskStatusFailed, TaskStatusDone, false},
		{"cancelled→open", TaskStatusCancelled, TaskStatusOpen, false},
		{"cancelled→done", TaskStatusCancelled, TaskStatusDone, false},

		// Unknown source status — no entry in validTransitions map.
		{"unknown→open", TaskStatus("unknown"), TaskStatusOpen, false},
		{"unknown→unknown (self)", TaskStatus("unknown"), TaskStatus("unknown"), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsValidTransition(tc.src, tc.dst)
			if got != tc.want {
				t.Errorf("IsValidTransition(%q, %q) = %v, want %v", tc.src, tc.dst, got, tc.want)
			}
		})
	}
}
