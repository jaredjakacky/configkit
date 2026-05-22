package configkit_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	configkit "github.com/jaredjakacky/configkit"
)

func TestEventKindValues(t *testing.T) {
	tests := []struct {
		name string
		kind configkit.EventKind
		want string
	}{
		{name: "load started", kind: configkit.EventKindLoadStarted, want: "load_started"},
		{name: "load succeeded", kind: configkit.EventKindLoadSucceeded, want: "load_succeeded"},
		{name: "load failed", kind: configkit.EventKindLoadFailed, want: "load_failed"},
		{name: "snapshot applied", kind: configkit.EventKindSnapshotApplied, want: "snapshot_applied"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tt.kind); got != tt.want {
				t.Fatalf("event kind = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEventJSONUsesOperationalFieldNames(t *testing.T) {
	event := configkit.Event{
		Kind:        configkit.EventKindLoadFailed,
		AttemptID:   7,
		AttemptKind: configkit.AttemptKindReload,
		Source:      configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		Revision:    "rev-1",
		Attempt: &configkit.AttemptRecord{
			ID:     7,
			Kind:   configkit.AttemptKindReload,
			Status: configkit.AttemptStatusFailed,
			Stage:  configkit.AttemptStageDecode,
			Error:  "decode failed",
		},
		OccurredAt: snapshotTestMetadata().LoadedAt,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal event JSON: %v", err)
	}

	for _, key := range []string{"kind", "attempt_id", "attempt_kind", "source", "revision", "attempt", "occurred_at"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("event JSON missing key %q in %s", key, data)
		}
	}
	if _, ok := got["value"]; ok {
		t.Fatalf("event JSON contains raw value key: %s", data)
	}
	if _, ok := got["redacted"]; ok {
		t.Fatalf("event JSON contains redacted view key: %s", data)
	}
}

func TestManagerObserverRecoversPanics(t *testing.T) {
	observer := configkit.Observer(func(ctx context.Context, event configkit.Event) {
		panic("observer failed")
	})
	manager := configkit.NewManager[stepsTestConfig](configkit.WithObservers(observer))

	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("apply succeeded result with panicking observer: %v", err)
	}
}

func TestManagerSkipsNilObserver(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig](configkit.WithObservers(nil))

	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("apply succeeded result: %v", err)
	}
}

func TestManagerNotifiesSnapshotAppliedEvent(t *testing.T) {
	var events []configkit.Event
	observer := configkit.Observer(func(ctx context.Context, event configkit.Event) {
		events = append(events, event)
	})
	manager := configkit.NewManager[stepsTestConfig](configkit.WithObservers(observer))

	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("apply succeeded result: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	event := events[0]
	if event.Kind != configkit.EventKindSnapshotApplied {
		t.Fatalf("event kind = %q, want %q", event.Kind, configkit.EventKindSnapshotApplied)
	}
	if event.AttemptID == 0 {
		t.Fatal("attempt id = 0, want manager-assigned id")
	}
	if event.Attempt == nil || event.Attempt.ID != event.AttemptID {
		t.Fatalf("event attempt = %+v, want matching attempt id %d", event.Attempt, event.AttemptID)
	}
	if event.Snapshot == nil || event.Snapshot.Checksum != "sum-1" {
		t.Fatalf("event snapshot = %+v, want applied snapshot metadata", event.Snapshot)
	}
	if event.Apply == nil || !event.Apply.Published {
		t.Fatalf("event apply = %+v, want published apply result", event.Apply)
	}
	if event.OccurredAt.IsZero() {
		t.Fatal("event occurred_at is zero")
	}
}

func TestManagerApplySuccessfulResultDoesNotEmitLoadLifecycleEvents(t *testing.T) {
	var events []configkit.Event
	observer := configkit.Observer(func(ctx context.Context, event configkit.Event) {
		events = append(events, event)
	})
	manager := configkit.NewManager[stepsTestConfig](configkit.WithObservers(observer))

	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("apply succeeded result: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if events[0].Kind != configkit.EventKindSnapshotApplied {
		t.Fatalf("event kind = %q, want %q", events[0].Kind, configkit.EventKindSnapshotApplied)
	}
	assertEventKindAbsent(t, events, configkit.EventKindLoadStarted)
	assertEventKindAbsent(t, events, configkit.EventKindLoadSucceeded)
}

func TestManagerApplyFailedResultDoesNotEmitLoadFailed(t *testing.T) {
	var events []configkit.Event
	observer := configkit.Observer(func(ctx context.Context, event configkit.Event) {
		events = append(events, event)
	})
	manager := configkit.NewManager[stepsTestConfig](configkit.WithObservers(observer))

	if _, err := manager.Apply(context.Background(), failedStatusTestResult("reload failed")); err != nil {
		t.Fatalf("apply failed result: %v", err)
	}

	if len(events) != 0 {
		t.Fatalf("event count = %d, want 0: %+v", len(events), events)
	}
	assertEventKindAbsent(t, events, configkit.EventKindLoadFailed)
}

func TestManagerApplyInvalidResultEmitsNoObserverEvents(t *testing.T) {
	var events []configkit.Event
	observer := configkit.Observer(func(ctx context.Context, event configkit.Event) {
		events = append(events, event)
	})
	manager := configkit.NewManager[stepsTestConfig](configkit.WithObservers(observer))

	_, err := manager.Apply(context.Background(), configkit.LoadResult[stepsTestConfig]{
		Attempt: configkit.AttemptRecord{Status: configkit.AttemptStatusSucceeded},
	})
	if !errors.Is(err, configkit.ErrInvalidLoadResult) {
		t.Fatalf("apply invalid result error = %v, want configkit.ErrInvalidLoadResult", err)
	}
	if len(events) != 0 {
		t.Fatalf("event count = %d, want 0: %+v", len(events), events)
	}
}

func TestManagerNotifiesLoadLifecycleEvents(t *testing.T) {
	var events []configkit.Event
	observer := configkit.Observer(func(ctx context.Context, event configkit.Event) {
		events = append(events, event)
	})
	manager := configkit.NewManager[stepsTestConfig](configkit.WithObservers(observer))

	_, err := manager.Load(context.Background(), configkit.AttemptKindInitialLoad, configkit.SourceData{
		Data:     []byte(`{"name":"api","enabled":true,"port":8080}`),
		Metadata: configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		Revision: "rev-1",
	}, configkit.Pipeline[stepsTestConfig]{
		Decode:   configkit.JSONDecoder[stepsTestConfig](),
		Redact:   configkit.EmptyRedactor[stepsTestConfig](),
		Checksum: configkit.SHA256JSONChecksum[stepsTestConfig](),
	})
	if err != nil {
		t.Fatalf("manager load: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3", len(events))
	}
	if events[0].Kind != configkit.EventKindLoadStarted {
		t.Fatalf("first event kind = %q, want %q", events[0].Kind, configkit.EventKindLoadStarted)
	}
	if events[1].Kind != configkit.EventKindLoadSucceeded {
		t.Fatalf("second event kind = %q, want %q", events[1].Kind, configkit.EventKindLoadSucceeded)
	}
	if events[2].Kind != configkit.EventKindSnapshotApplied {
		t.Fatalf("third event kind = %q, want %q", events[2].Kind, configkit.EventKindSnapshotApplied)
	}
	for i, event := range events {
		if event.AttemptID == 0 {
			t.Fatalf("event %d attempt id = 0, want manager-assigned id", i)
		}
		if event.AttemptID != events[0].AttemptID {
			t.Fatalf("event %d attempt id = %d, want %d", i, event.AttemptID, events[0].AttemptID)
		}
	}
}

func assertEventKindAbsent(t *testing.T, events []configkit.Event, kind configkit.EventKind) {
	t.Helper()

	for i, event := range events {
		if event.Kind == kind {
			t.Fatalf("event %d kind = %q, want absent", i, kind)
		}
	}
}
