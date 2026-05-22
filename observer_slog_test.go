package configkit_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	configkit "github.com/jaredjakacky/configkit"
)

func TestSlogObserverLogsInfoForNonFailureEvents(t *testing.T) {
	handler := &captureSlogHandler{}
	observer := configkit.SlogObserver(slog.New(handler))

	observer(context.Background(), configkit.Event{Kind: configkit.EventKindLoadStarted, AttemptID: 10})

	record := handler.singleRecord(t)
	if record.Level != slog.LevelInfo {
		t.Fatalf("level = %v, want %v", record.Level, slog.LevelInfo)
	}
	if record.Message != "configkit event" {
		t.Fatalf("message = %q, want %q", record.Message, "configkit event")
	}
	attrs := slogRecordAttrs(record)
	if got := attrs["event"].String(); got != string(configkit.EventKindLoadStarted) {
		t.Fatalf("event attr = %q, want %q", got, configkit.EventKindLoadStarted)
	}
	if got := attrs["attempt_id"].Uint64(); got != 10 {
		t.Fatalf("attempt_id attr = %d, want 10", got)
	}
}

func TestSlogObserverLogsErrorForLoadFailed(t *testing.T) {
	handler := &captureSlogHandler{}
	observer := configkit.SlogObserver(slog.New(handler))

	observer(context.Background(), configkit.Event{Kind: configkit.EventKindLoadFailed})

	record := handler.singleRecord(t)
	if record.Level != slog.LevelError {
		t.Fatalf("level = %v, want %v", record.Level, slog.LevelError)
	}
}

func TestSlogEventAttrsIncludesOperationalMetadata(t *testing.T) {
	handler := &captureSlogHandler{}
	observer := configkit.SlogObserver(slog.New(handler))
	startedAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(250 * time.Millisecond)
	event := configkit.Event{
		Kind:        configkit.EventKindSnapshotApplied,
		AttemptID:   10,
		AttemptKind: configkit.AttemptKindReload,
		Source:      configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		Revision:    "rev-1",
		Attempt: &configkit.AttemptRecord{
			Status:    configkit.AttemptStatusSucceeded,
			Stage:     configkit.AttemptStageChecksum,
			Checksum:  "attempt-sum",
			StartedAt: startedAt,
			EndedAt:   endedAt,
		},
		Snapshot: &configkit.SnapshotMetadata{Checksum: "snapshot-sum"},
		Apply: &configkit.ApplyResult{
			Published: true,
			Changed:   true,
		},
	}

	observer(context.Background(), event)
	attrs := slogRecordAttrs(handler.singleRecord(t))
	if got := attrs["event"].String(); got != string(configkit.EventKindSnapshotApplied) {
		t.Fatalf("event attr = %q, want %q", got, configkit.EventKindSnapshotApplied)
	}
	if got := attrs["attempt_id"].Uint64(); got != 10 {
		t.Fatalf("attempt_id attr = %d, want 10", got)
	}
	if got := attrs["attempt_kind"].String(); got != string(configkit.AttemptKindReload) {
		t.Fatalf("attempt_kind attr = %q, want %q", got, configkit.AttemptKindReload)
	}
	if got := attrs["source_name"].String(); got != "memory" {
		t.Fatalf("source_name attr = %q, want memory", got)
	}
	if got := attrs["source_kind"].String(); got != "memory" {
		t.Fatalf("source_kind attr = %q, want memory", got)
	}
	if got := attrs["revision"].String(); got != "rev-1" {
		t.Fatalf("revision attr = %q, want rev-1", got)
	}
	if got := attrs["attempt_checksum"].String(); got != "attempt-sum" {
		t.Fatalf("attempt_checksum attr = %q, want attempt-sum", got)
	}
	if got := attrs["attempt_stage"].String(); got != string(configkit.AttemptStageChecksum) {
		t.Fatalf("attempt_stage attr = %q, want %q", got, configkit.AttemptStageChecksum)
	}
	if got := attrs["attempt_started_at"].Time(); !got.Equal(startedAt) {
		t.Fatalf("attempt_started_at attr = %v, want %v", got, startedAt)
	}
	if got := attrs["attempt_ended_at"].Time(); !got.Equal(endedAt) {
		t.Fatalf("attempt_ended_at attr = %v, want %v", got, endedAt)
	}
	if got := attrs["attempt_duration"].Duration(); got != 250*time.Millisecond {
		t.Fatalf("attempt_duration attr = %v, want %v", got, 250*time.Millisecond)
	}
	if got := attrs["snapshot_checksum"].String(); got != "snapshot-sum" {
		t.Fatalf("snapshot_checksum attr = %q, want snapshot-sum", got)
	}
	if got := attrs["published"].Bool(); !got {
		t.Fatal("published attr = false, want true")
	}
	if got := attrs["changed"].Bool(); !got {
		t.Fatal("changed attr = false, want true")
	}
}

func TestSlogEventAttrsFallsBackToAttemptID(t *testing.T) {
	handler := &captureSlogHandler{}
	observer := configkit.SlogObserver(slog.New(handler))

	observer(context.Background(), configkit.Event{
		Kind: configkit.EventKindLoadFailed,
		Attempt: &configkit.AttemptRecord{
			ID:     42,
			Status: configkit.AttemptStatusFailed,
			Error:  "decode failed",
		},
	})
	attrs := slogRecordAttrs(handler.singleRecord(t))

	if got := attrs["attempt_id"].Uint64(); got != 42 {
		t.Fatalf("attempt_id attr = %d, want 42", got)
	}
	if got := attrs["attempt_error"].String(); got != "decode failed" {
		t.Fatalf("attempt_error attr = %q, want decode failed", got)
	}
}

func TestSlogEventAttrsOmitRawConfigAndRedactedFields(t *testing.T) {
	handler := &captureSlogHandler{}
	observer := configkit.SlogObserver(slog.New(handler))

	observer(context.Background(), configkit.Event{
		Kind:     configkit.EventKindSnapshotApplied,
		Snapshot: &configkit.SnapshotMetadata{Checksum: "sum-1"},
		Apply:    &configkit.ApplyResult{Published: true},
	})
	attrs := slogRecordAttrs(handler.singleRecord(t))

	for _, key := range []string{"value", "config", "redacted"} {
		if _, ok := attrs[key]; ok {
			t.Fatalf("attrs contain %q, want omitted", key)
		}
	}
}

func TestSlogObserverAcceptsNilLogger(t *testing.T) {
	observer := configkit.SlogObserver(nil)

	observer(context.Background(), configkit.Event{Kind: configkit.EventKindLoadStarted})
}

type captureSlogHandler struct {
	records []slog.Record
}

func (h *captureSlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *captureSlogHandler) Handle(ctx context.Context, record slog.Record) error {
	h.records = append(h.records, record.Clone())
	return nil
}

func (h *captureSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *captureSlogHandler) WithGroup(name string) slog.Handler {
	return h
}

func (h *captureSlogHandler) singleRecord(t *testing.T) slog.Record {
	t.Helper()

	if len(h.records) != 1 {
		t.Fatalf("record count = %d, want 1", len(h.records))
	}
	return h.records[0]
}

func slogRecordAttrs(record slog.Record) map[string]slog.Value {
	attrs := make(map[string]slog.Value)
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value
		return true
	})
	return attrs
}
