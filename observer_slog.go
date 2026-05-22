package configkit

import (
	"context"
	"log/slog"
)

// SlogObserver returns an Observer that logs lifecycle events with slog.
//
// If logger is nil, slog.Default is used. The observer logs lifecycle metadata
// only; it does not log typed configuration values or redacted fields. It may
// log caller-provided source names, revisions, checksums, and error strings, so
// those values should be safe for the logger's audience.
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
	if attempt.Stage != "" {
		attrs = append(attrs, slog.String("attempt_stage", string(attempt.Stage)))
	}
	if attempt.Error != "" {
		attrs = append(attrs, slog.String("attempt_error", attempt.Error))
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
