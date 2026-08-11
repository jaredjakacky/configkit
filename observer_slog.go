package configkit

import (
	"context"
	"log/slog"
)

// SlogObserver returns an Observer that logs lifecycle events with slog.
//
// If logger is nil, slog.Default is used. The observer logs lifecycle metadata
// only, including component identity and event-time manager state; it does not
// log typed configuration values or redacted fields. It may log caller-provided
// source names, revisions, checksums, and public failure detail, so those values
// should be safe for the logger's audience. Arbitrary internal error strings
// are not copied into events.
func SlogObserver(logger *slog.Logger) Observer {
	if logger == nil {
		logger = slog.Default()
	}

	return func(ctx context.Context, event Event) {
		level := slog.LevelInfo
		if event.Kind == EventKindLoadFailed {
			level = slog.LevelError
		}

		logger.LogAttrs(ctx, level, "configkit event", slogEventAttrs(event)...)
	}
}

func slogEventAttrs(event Event) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("event", string(event.Kind)),
	}
	if event.ComponentName != "" {
		attrs = append(attrs, slog.String("component_name", event.ComponentName))
	}
	if event.ManagerState != "" {
		attrs = append(attrs, slog.String("manager_state", string(event.ManagerState)))
	}

	attemptID := event.AttemptID
	if attemptID == 0 && event.Attempt != nil {
		attemptID = event.Attempt.ID
	}
	if attemptID != 0 {
		attrs = append(attrs, slog.Uint64("attempt_id", attemptID))
	}
	if event.AttemptKind != "" {
		attrs = append(attrs, slog.String("attempt_kind", string(event.AttemptKind)))
	}
	if event.Source.Name != "" {
		attrs = append(attrs, slog.String("source_name", event.Source.Name))
	}
	if event.Source.Kind != "" {
		attrs = append(attrs, slog.String("source_kind", event.Source.Kind))
	}
	if event.Revision != "" {
		attrs = append(attrs, slog.String("revision", event.Revision))
	}

	attrs = appendSlogAttemptAttrs(attrs, event.Attempt)
	attrs = appendSlogSnapshotAttrs(attrs, event.Snapshot)
	attrs = appendSlogApplyAttrs(attrs, event.Apply)

	return attrs
}

func appendSlogAttemptAttrs(attrs []slog.Attr, attempt *AttemptRecord) []slog.Attr {
	if attempt == nil {
		return attrs
	}

	if attempt.Checksum != "" {
		attrs = append(attrs, slog.String("attempt_checksum", attempt.Checksum))
	}
	if attempt.Status != "" {
		attrs = append(attrs, slog.String("attempt_status", string(attempt.Status)))
	}
	if attempt.Stage != "" {
		attrs = append(attrs, slog.String("attempt_stage", string(attempt.Stage)))
	}
	if attempt.Failure != nil {
		if attempt.Failure.Code != "" {
			attrs = append(attrs, slog.String("attempt_failure_code", attempt.Failure.Code))
		}
		if attempt.Failure.Message != "" {
			attrs = append(attrs, slog.String("attempt_failure_message", attempt.Failure.Message))
		}
	}
	if !attempt.StartedAt.IsZero() {
		attrs = append(attrs, slog.Time("attempt_started_at", attempt.StartedAt))
	}
	if !attempt.EndedAt.IsZero() {
		attrs = append(attrs, slog.Time("attempt_ended_at", attempt.EndedAt))
	}
	if !attempt.StartedAt.IsZero() && !attempt.EndedAt.IsZero() {
		attrs = append(attrs, slog.Duration("attempt_duration", attempt.EndedAt.Sub(attempt.StartedAt)))
	}

	return attrs
}

func appendSlogSnapshotAttrs(attrs []slog.Attr, snapshot *SnapshotMetadata) []slog.Attr {
	if snapshot == nil || snapshot.Checksum == "" {
		return attrs
	}

	return append(attrs, slog.String("snapshot_checksum", snapshot.Checksum))
}

func appendSlogApplyAttrs(attrs []slog.Attr, apply *ApplyResult) []slog.Attr {
	if apply == nil {
		return attrs
	}

	attrs = append(attrs,
		slog.Bool("published", apply.Published),
		slog.Bool("changed", apply.Changed),
	)

	return attrs
}
