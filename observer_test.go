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
		Kind:          configkit.EventKindLoadFailed,
		ComponentName: "payments-config",
		ManagerState:  configkit.LifecycleStateDegraded,
		AttemptID:     7,
		AttemptKind:   configkit.AttemptKindReload,
		Source:        configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		Revision:      "rev-1",
		Attempt: &configkit.AttemptRecord{
			ID:      7,
			Kind:    configkit.AttemptKindReload,
			Status:  configkit.AttemptStatusFailed,
			Stage:   configkit.AttemptStageDecode,
			Failure: testFailure("decode failed"),
		},
		Apply:      &configkit.ApplyResult{Published: false},
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

	for _, key := range []string{"kind", "component_name", "manager_state", "attempt_id", "attempt_kind", "source", "revision", "attempt", "apply", "occurred_at"} {
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

	apply, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1"))
	if err != nil {
		t.Fatalf("apply succeeded result: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	event := events[0]
	if event.Kind != configkit.EventKindSnapshotApplied {
		t.Fatalf("event kind = %q, want %q", event.Kind, configkit.EventKindSnapshotApplied)
	}
	if event.ComponentName != "config" {
		t.Fatalf("component name = %q, want config", event.ComponentName)
	}
	if event.ManagerState != configkit.LifecycleStateLoaded {
		t.Fatalf("manager state = %q, want %q", event.ManagerState, configkit.LifecycleStateLoaded)
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
	if !event.Apply.AppliedAt.Equal(apply.AppliedAt) {
		t.Fatalf("event applied_at = %v, want returned applied_at %v", event.Apply.AppliedAt, apply.AppliedAt)
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
		if event.ComponentName != "config" {
			t.Fatalf("event %d component name = %q, want config", i, event.ComponentName)
		}
		if event.AttemptID == 0 {
			t.Fatalf("event %d attempt id = 0, want manager-assigned id", i)
		}
		if event.AttemptID != events[0].AttemptID {
			t.Fatalf("event %d attempt id = %d, want %d", i, event.AttemptID, events[0].AttemptID)
		}
	}
	if events[0].ManagerState != configkit.LifecycleStateUnloaded {
		t.Fatalf("started manager state = %q, want %q", events[0].ManagerState, configkit.LifecycleStateUnloaded)
	}
	for i := 1; i < len(events); i++ {
		if events[i].ManagerState != configkit.LifecycleStateLoaded {
			t.Fatalf("event %d manager state = %q, want %q", i, events[i].ManagerState, configkit.LifecycleStateLoaded)
		}
		if events[i].Apply == nil || !events[i].Apply.Published {
			t.Fatalf("event %d apply = %+v, want published result", i, events[i].Apply)
		}
		if events[i].Apply.ManagerState != events[i].ManagerState {
			t.Fatalf("event %d apply state = %q, event state = %q; want equal", i, events[i].Apply.ManagerState, events[i].ManagerState)
		}
	}
}

func TestManagerEventsDistinguishManagerLocalAttemptIDs(t *testing.T) {
	var events []configkit.Event
	observer := configkit.Observer(func(_ context.Context, event configkit.Event) {
		events = append(events, event)
	})
	first := configkit.NewManager[stepsTestConfig](
		configkit.WithIdentity("primary-config"),
		configkit.WithObservers(observer),
	)
	second := configkit.NewManager[stepsTestConfig](
		configkit.WithIdentity("secondary-config"),
		configkit.WithObservers(observer),
	)

	if _, err := first.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("apply first manager: %v", err)
	}
	if _, err := second.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("apply second manager: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if events[0].AttemptID != 1 || events[1].AttemptID != 1 {
		t.Fatalf("attempt ids = %d, %d; want manager-local id 1 for both", events[0].AttemptID, events[1].AttemptID)
	}
	if events[0].ComponentName != "primary-config" || events[1].ComponentName != "secondary-config" {
		t.Fatalf("component names = %q, %q; want distinct configured identities", events[0].ComponentName, events[1].ComponentName)
	}
	for i, event := range events {
		if event.ManagerState != configkit.LifecycleStateLoaded || event.Apply == nil || event.Apply.ManagerState != event.ManagerState {
			t.Fatalf("event %d state/apply = %q/%+v, want matching loaded state", i, event.ManagerState, event.Apply)
		}
	}
}

func TestLoadSucceededObserverSeesResultingManagerState(t *testing.T) {
	var manager *configkit.Manager[stepsTestConfig]
	var observed bool
	observer := configkit.Observer(func(_ context.Context, event configkit.Event) {
		if event.Kind != configkit.EventKindLoadSucceeded {
			return
		}
		observed = true
		status := manager.LifecycleStatus()
		if status.State != configkit.LifecycleStateLoaded || event.ManagerState != status.State {
			t.Fatalf("event state = %q, manager state = %q; want loaded", event.ManagerState, status.State)
		}
		value, ok := manager.Value()
		if !ok || value.Name != "api" {
			t.Fatalf("manager value = %+v, ok = %t; want newly published config", value, ok)
		}
		if event.Apply == nil || !event.Apply.Published || event.Apply.Current == nil {
			t.Fatalf("completion apply = %+v, want published current snapshot", event.Apply)
		}
		if event.Apply.ManagerState != event.ManagerState {
			t.Fatalf("apply state = %q, event state = %q; want equal", event.Apply.ManagerState, event.ManagerState)
		}
	})
	manager = configkit.NewManager[stepsTestConfig](configkit.WithObservers(observer))

	if _, err := manager.Load(context.Background(), configkit.AttemptKindInitialLoad, configkit.SourceData{
		Data: []byte(`{"name":"api","enabled":true,"port":8080}`),
	}, testPipeline()); err != nil {
		t.Fatalf("manager load: %v", err)
	}
	if !observed {
		t.Fatal("load succeeded event not observed")
	}
}

func TestLoadFromSourceSucceededObserverSeesResultingManagerState(t *testing.T) {
	var manager *configkit.Manager[stepsTestConfig]
	var observed bool
	observer := configkit.Observer(func(_ context.Context, event configkit.Event) {
		if event.Kind != configkit.EventKindLoadSucceeded {
			return
		}
		observed = true
		status := manager.LifecycleStatus()
		if status.State != configkit.LifecycleStateLoaded || event.ManagerState != status.State {
			t.Fatalf("event state = %q, manager state = %q; want loaded", event.ManagerState, status.State)
		}
		if event.Apply == nil || !event.Apply.Published {
			t.Fatalf("completion apply = %+v, want published result", event.Apply)
		}
		if event.Apply.ManagerState != event.ManagerState {
			t.Fatalf("apply state = %q, event state = %q; want equal", event.Apply.ManagerState, event.ManagerState)
		}
	})
	manager = configkit.NewManager[stepsTestConfig](configkit.WithObservers(observer))
	source := configkit.NewBytesSource(
		[]byte(`{"name":"api","enabled":true,"port":8080}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)

	if _, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindInitialLoad, source, testPipeline()); err != nil {
		t.Fatalf("manager load from source: %v", err)
	}
	if !observed {
		t.Fatal("load succeeded event not observed")
	}
}

func TestInitialLoadFailedObserverSeesFailedManagerState(t *testing.T) {
	var manager *configkit.Manager[stepsTestConfig]
	var observed bool
	observer := configkit.Observer(func(_ context.Context, event configkit.Event) {
		if event.Kind != configkit.EventKindLoadFailed {
			return
		}
		observed = true
		status := manager.LifecycleStatus()
		if status.State != configkit.LifecycleStateFailed || event.ManagerState != status.State {
			t.Fatalf("event state = %q, manager state = %q; want failed", event.ManagerState, status.State)
		}
		if status.LastFailure == nil || event.Apply == nil || event.Apply.Published {
			t.Fatalf("status = %+v, apply = %+v; want recorded unpublished failure", status, event.Apply)
		}
		if event.Apply.ManagerState != event.ManagerState {
			t.Fatalf("apply state = %q, event state = %q; want equal", event.Apply.ManagerState, event.ManagerState)
		}
		if _, ok := manager.Snapshot(); ok {
			t.Fatal("snapshot available during initial failure completion")
		}
	})
	manager = configkit.NewManager[stepsTestConfig](configkit.WithObservers(observer))

	if _, err := manager.Load(context.Background(), configkit.AttemptKindInitialLoad, configkit.SourceData{
		Data: []byte(`{"name":`),
	}, testPipeline()); err == nil {
		t.Fatal("manager load error = nil, want decode failure")
	}
	if !observed {
		t.Fatal("load failed event not observed")
	}
}

func TestReloadFailedObserverSeesDegradedLastKnownGoodState(t *testing.T) {
	var manager *configkit.Manager[stepsTestConfig]
	var observed bool
	observer := configkit.Observer(func(_ context.Context, event configkit.Event) {
		if event.Kind != configkit.EventKindLoadFailed {
			return
		}
		observed = true
		status := manager.LifecycleStatus()
		if status.State != configkit.LifecycleStateDegraded || event.ManagerState != status.State {
			t.Fatalf("event state = %q, manager state = %q; want degraded", event.ManagerState, status.State)
		}
		value, ok := manager.Value()
		if !ok || value.Name != "api" {
			t.Fatalf("manager value = %+v, ok = %t; want last-known-good config", value, ok)
		}
		if event.Apply == nil || event.Apply.Published || event.Apply.Previous == nil || event.Apply.Current == nil {
			t.Fatalf("completion apply = %+v, want unpublished retained snapshot", event.Apply)
		}
		if event.Apply.Previous.Checksum != event.Apply.Current.Checksum {
			t.Fatalf("apply previous checksum = %q, current = %q; want retained snapshot", event.Apply.Previous.Checksum, event.Apply.Current.Checksum)
		}
		if event.Apply.ManagerState != event.ManagerState {
			t.Fatalf("apply state = %q, event state = %q; want equal", event.Apply.ManagerState, event.ManagerState)
		}
	})
	manager = configkit.NewManager[stepsTestConfig](configkit.WithObservers(observer))
	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("seed manager: %v", err)
	}

	if _, err := manager.Load(context.Background(), configkit.AttemptKindReload, configkit.SourceData{
		Data: []byte(`{"name":`),
	}, testPipeline()); err == nil {
		t.Fatal("manager reload error = nil, want decode failure")
	}
	if !observed {
		t.Fatal("load failed event not observed")
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
