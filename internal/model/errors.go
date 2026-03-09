package model

import "errors"

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when an optimistic concurrency check fails.
var ErrConflict = errors.New("version conflict")

// ErrInvalidTransition is returned when a state transition is not allowed.
var ErrInvalidTransition = errors.New("invalid status transition")