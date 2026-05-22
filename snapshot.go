package configkit

import "time"

// Snapshot is one successfully loaded, validated, and published configuration value.
//
// A snapshot represents last-known-good configuration state. Failed load or reload
// attempts should be recorded outside the snapshot, because a Snapshot should only
// represent configuration that was valid enough to publish.
//
// Snapshot protects its metadata and redacted map container from mutation.
// Pipeline.Copy can protect the stored value from references shared with earlier
// pipeline stages before publication.
//
// Value returns T by normal Go assignment rules. Callers must treat returned
// values as immutable unless T is naturally immutable or caller code performs
// its own defensive copying.
type Snapshot[T any] struct {
	value    T
	metadata SnapshotMetadata
	redacted RedactedView
}

// SnapshotMetadata describes where a configuration snapshot came from and when it
// became active.
type SnapshotMetadata struct {
	// Source describes the logical source that produced the snapshot.
	Source SourceMetadata `json:"source"`

	// Revision is an optional source-provided version, generation, ETag, resource
	// version, commit SHA, or similar identifier. It may be exposed through
	// operational output and should not contain secrets or sensitive tenant,
	// path, or environment details.
	Revision string `json:"revision,omitempty"`

	// Checksum is a stable fingerprint of the effective configuration value. It
	// is operational metadata, not a redaction or secrecy mechanism. Exposed
	// checksums can leak information for low-entropy values or known config sets.
	Checksum string `json:"checksum"`

	// LoadedAt is when this snapshot was successfully loaded and published.
	LoadedAt time.Time `json:"loaded_at"`
}

// RedactedView is the safe inspection shape for a snapshot.
//
// It should contain only values that are safe for logs, diagnostics, admin
// endpoints, and support workflows. Snapshot copies the top-level map, but not
// arbitrary values stored inside it. Its safety depends entirely on the
// application's Redactor.
type RedactedView map[string]any

// NewSnapshot creates a configuration snapshot from an already-loaded
// configuration value, its metadata, and a safe redacted inspection view.
//
// NewSnapshot does not load, decode, validate, redact, or compute metadata.
// Those steps happen before a snapshot is created. A Snapshot represents the
// published result of that work.
func NewSnapshot[T any](value T, metadata SnapshotMetadata, redacted RedactedView) Snapshot[T] {
	return Snapshot[T]{
		value:    value,
		metadata: metadata,
		redacted: cloneRedactedView(redacted),
	}
}

// Value returns the typed configuration value stored in the snapshot.
//
// The returned value follows normal Go assignment rules. If T contains maps,
// slices, pointers, or other mutable references, mutating the returned value may
// mutate data reachable from the snapshot.
func (s Snapshot[T]) Value() T {
	return s.value
}

// Metadata returns metadata describing where the snapshot came from and when it
// was loaded.
func (s Snapshot[T]) Metadata() SnapshotMetadata {
	return s.metadata
}

// Redacted returns the snapshot's safe inspection view.
//
// The returned map is a copy. Changing it does not mutate the snapshot.
func (s Snapshot[T]) Redacted() RedactedView {
	return cloneRedactedView(s.redacted)
}

func cloneRedactedView(in RedactedView) RedactedView {
	if in == nil {
		return nil
	}

	out := make(RedactedView, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
