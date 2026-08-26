package jira

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Error is an HTTP-level failure with the status code and error bodies intact.
// Preserving these is the reason this package speaks HTTP directly instead of
// shelling out to acli, which surfaces failures only as stderr text.
type Error struct {
	StatusCode int
	// Messages are Jira's errorMessages array.
	Messages []string
	// Fields are Jira's per-field errors, keyed by field ID.
	Fields map[string]string
	// RetryAfter is the server's stated delay, zero when not supplied.
	RetryAfter time.Duration
	Op         string
}

func (e *Error) Error() string {
	if len(e.Messages) > 0 {
		return fmt.Sprintf("%s: %d: %s", e.Op, e.StatusCode, e.Messages[0])
	}
	return fmt.Sprintf("%s: %d", e.Op, e.StatusCode)
}

// Retryable reports whether a read may be retried. Writes are never retried
// regardless of what this returns: a double transition or duplicate comment is
// a worse outcome than an error message.
func (e *Error) Retryable() bool {
	return e.StatusCode == 429 || e.StatusCode >= 500
}

// FieldsRequiredError reports a transition rejected because its screen demands
// fields we do not collect. Collecting them is out of scope for v1; the UI
// names the missing fields and offers the acli escape hatch.
type FieldsRequiredError struct {
	TransitionID string
	// Fields maps field ID to Jira's explanation.
	Fields map[string]string
}

func (e *FieldsRequiredError) Error() string {
	return fmt.Sprintf("transition %s requires fields: %s", e.TransitionID, strings.Join(e.FieldNames(), ", "))
}

// FieldNames are the rejected fields, sorted. Order is part of the message a
// user reads back from a failed write, and map order would make the same
// rejection read differently each time.
func (e *FieldsRequiredError) FieldNames() []string {
	out := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// OfflineError marks a failure to reach Jira at all, as distinct from a
// rejection by it. The UI keeps cached data browsable and shows an offline
// marker rather than blanking the screen.
type OfflineError struct{ Err error }

func (e *OfflineError) Error() string { return "offline: " + e.Err.Error() }
func (e *OfflineError) Unwrap() error { return e.Err }
