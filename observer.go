package configkit

import (
	"context"
	"time"
)

// EventKind describes a configuration lifecycle event.
type EventKind string

const (
	EventKindLoadStarted     EventKind = "load_started"
	EventKindLoadSucceeded   EventKind = "load_succeeded"
	EventKindLoadFailed      EventKind = "load_failed"
	EventKindSnapshotApplied EventKind = "snapshot_applied"
)

// Event describes something that happened during the configuration lifecycle.
//
// Event is operational data. It does not expose the typed configuration value.
// It can include caller-provided metadata, revisions, checksums, and error
// strings that observers may log or export.
// AttemptID is a manager-local correlation identifier and may be zero for
// package-level load results or externally constructed events.
type Event struct {
	Kind EventKind `json:"kind"`

	AttemptID   uint64         `json:"attempt_id,omitempty"`
	AttemptKind AttemptKind    `json:"attempt_kind,omitempty"`
	Source      SourceMetadata `json:"source"`
	Revision    string         `json:"revision,omitempty"`

	Attempt  *AttemptRecord    `json:"attempt,omitempty"`
	Snapshot *SnapshotMetadata `json:"snapshot,omitempty"`
	Apply    *ApplyResult      `json:"apply,omitempty"`

	OccurredAt time.Time `json:"occurred_at"`
}

// Observer receives configuration lifecycle events.
//
// Observers are for telemetry, logs, diagnostics, and operational hooks. They
// should not own configuration state or decide whether a load succeeds. Observer
// panics are recovered by Manager, but observers run synchronously and should
// return quickly.
//
// A synchronous observer must not call Load, LoadFromSource, or Apply on the
// same Manager that emitted the event. That creates reentrant lifecycle behavior
// and can deadlock. Read-only calls such as Status, Inspect, Snapshot, and Value
// are acceptable. Use AsyncObserver or hand work off to another goroutine for
// follow-up lifecycle work.
type Observer func(ctx context.Context, event Event)
