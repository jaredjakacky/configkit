package configkit

// Status describes the observable configuration lifecycle state.
//
// Status is intentionally not generic. It does not expose the typed config
// value. It is intended for logs, diagnostics, health checks, admin endpoints,
// and observer payloads, but it can include caller-provided source metadata,
// revisions, checksums, and error strings. Those fields should be treated as
// operationally visible and should not contain secrets.
type Status struct {
	State StatusState `json:"state"`

	Current     *SnapshotMetadata `json:"current,omitempty"`
	LastAttempt *AttemptRecord    `json:"last_attempt,omitempty"`
	LastSuccess *AttemptRecord    `json:"last_success,omitempty"`
	LastFailure *AttemptRecord    `json:"last_failure,omitempty"`
	LastApply   *ApplyResult      `json:"last_apply,omitempty"`
}

// Inspection is a safe operational view of current configuration lifecycle state.
//
// Inspection does not expose the typed configuration value. Its Redacted field
// contains values chosen by the application's Redactor and is only as safe as
// that Redactor's output.
type Inspection struct {
	Status   Status       `json:"status"`
	Redacted RedactedView `json:"redacted,omitempty"`
}

// Provider exposes read-only access to current configuration state.
type Provider[T any] interface {
	Snapshot() (Snapshot[T], bool)
	Value() (T, bool)
	Status() Status
}

// Inspector exposes safe operational configuration inspection.
type Inspector interface {
	Inspect() Inspection
}

// StatusState describes the high-level configuration lifecycle state.
type StatusState string

const (
	// StatusStateUnloaded means no valid configuration snapshot has been
	// published and no failed load attempt has been recorded.
	StatusStateUnloaded StatusState = "unloaded"

	// StatusStateLoaded means a valid configuration snapshot is active and the
	// most recent attempt did not fail.
	StatusStateLoaded StatusState = "loaded"

	// StatusStateFailed means no valid configuration snapshot is active because
	// the most recent load attempt failed.
	StatusStateFailed StatusState = "failed"

	// StatusStateDegraded means a valid configuration snapshot is active, but
	// the most recent load or reload attempt failed. The last known good
	// snapshot remains active.
	StatusStateDegraded StatusState = "degraded"
)
