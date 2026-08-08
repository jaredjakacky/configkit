package configkit

// LifecycleStatus describes the observable configuration lifecycle state.
//
// LifecycleStatus is intentionally not generic. It does not expose the typed config
// value. It is intended for logs, diagnostics, health checks, admin endpoints,
// and observer payloads, but it can include caller-provided source metadata,
// revisions, checksums, and explicit public failure detail. Those fields should
// be treated as operationally visible and should not contain secrets.
type LifecycleStatus struct {
	State LifecycleState `json:"state"`

	Current     *SnapshotMetadata `json:"current,omitempty"`
	LastAttempt *AttemptRecord    `json:"last_attempt,omitempty"`
	LastSuccess *AttemptRecord    `json:"last_success,omitempty"`
	LastFailure *AttemptRecord    `json:"last_failure,omitempty"`
	LastApply   *ApplyResult      `json:"last_apply,omitempty"`
}

// LifecycleInspection is a safe operational view of current configuration lifecycle state.
//
// LifecycleInspection does not expose the typed configuration value. Its Redacted field
// contains values chosen by the application's Redactor and is only as safe as
// that Redactor's output.
type LifecycleInspection struct {
	Status   LifecycleStatus `json:"status"`
	Redacted RedactedView    `json:"redacted,omitempty"`
}

// LifecycleProvider exposes read-only access to current configuration state.
type LifecycleProvider[T any] interface {
	Snapshot() (Snapshot[T], bool)
	Value() (T, bool)
	LifecycleStatus() LifecycleStatus
}

// LifecycleInspector exposes safe lifecycle configuration inspection.
type LifecycleInspector interface {
	LifecycleInspection() LifecycleInspection
}

// LifecycleState describes the high-level configuration lifecycle state.
type LifecycleState string

const (
	// LifecycleStateUnloaded means no valid configuration snapshot has been
	// published and no failed load attempt has been recorded.
	LifecycleStateUnloaded LifecycleState = "unloaded"

	// LifecycleStateLoaded means a valid configuration snapshot is active and the
	// most recent attempt did not fail.
	LifecycleStateLoaded LifecycleState = "loaded"

	// LifecycleStateFailed means no valid configuration snapshot is active because
	// the most recent load attempt failed.
	LifecycleStateFailed LifecycleState = "failed"

	// LifecycleStateDegraded means a valid configuration snapshot is active, but
	// the most recent load or reload attempt failed. The last known good
	// snapshot remains active.
	LifecycleStateDegraded LifecycleState = "degraded"
)
