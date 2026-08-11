package configkit_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	configkit "github.com/jaredjakacky/configkit"
)

func TestManagerSnapshotAndValueBeforeLoad(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()

	if snapshot, ok := manager.Snapshot(); ok {
		t.Fatalf("snapshot = %+v, ok = true, want false", snapshot)
	}
	if value, ok := manager.Value(); ok {
		t.Fatalf("value = %+v, ok = true, want false", value)
	}
}

func TestManagerApplyPublishesSnapshotAndValue(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()

	apply, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1"))
	if err != nil {
		t.Fatalf("apply succeeded result: %v", err)
	}
	if !apply.Published {
		t.Fatal("published = false, want true")
	}
	if !apply.Changed {
		t.Fatal("changed = false, want true")
	}
	if apply.Previous != nil {
		t.Fatalf("previous = %+v, want nil", apply.Previous)
	}
	if apply.Current == nil || apply.Current.Checksum != "sum-1" {
		t.Fatalf("current = %+v, want checksum sum-1", apply.Current)
	}
	if apply.ManagerState != configkit.LifecycleStateLoaded {
		t.Fatalf("manager state = %q, want %q", apply.ManagerState, configkit.LifecycleStateLoaded)
	}

	snapshot, ok := manager.Snapshot()
	if !ok {
		t.Fatal("snapshot ok = false, want true")
	}
	if got := snapshot.Metadata().Checksum; got != "sum-1" {
		t.Fatalf("snapshot checksum = %q, want sum-1", got)
	}
	value, ok := manager.Value()
	if !ok {
		t.Fatal("value ok = false, want true")
	}
	if value.Name != "api" || !value.Enabled || value.Port != 8080 {
		t.Fatalf("value = %+v, want published config", value)
	}
}

func TestManagerApplyChangedFalseWhenChecksumUnchanged(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()

	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	apply, err := manager.Apply(context.Background(), succeededStatusTestResult("v2", "sum-1"))
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}

	if !apply.Published {
		t.Fatal("published = false, want true")
	}
	if apply.Changed {
		t.Fatal("changed = true, want false")
	}
	if apply.Previous == nil || apply.Previous.Revision != "v1" {
		t.Fatalf("previous = %+v, want v1 metadata", apply.Previous)
	}
	if apply.Current == nil || apply.Current.Revision != "v2" {
		t.Fatalf("current = %+v, want v2 metadata", apply.Current)
	}
}

func TestManagerApplyFailedPreservesCurrentSnapshot(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()

	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("apply success: %v", err)
	}
	apply, err := manager.Apply(context.Background(), failedStatusTestResult("reload failed"))
	if err != nil {
		t.Fatalf("apply failure result: %v", err)
	}

	if apply.Published {
		t.Fatal("published = true, want false")
	}
	if apply.Changed {
		t.Fatal("changed = true, want false")
	}
	if apply.Previous == nil || apply.Previous.Checksum != "sum-1" {
		t.Fatalf("previous = %+v, want existing snapshot metadata", apply.Previous)
	}
	if apply.Current == nil || apply.Current.Checksum != "sum-1" {
		t.Fatalf("current = %+v, want preserved snapshot metadata", apply.Current)
	}
	if apply.ManagerState != configkit.LifecycleStateDegraded {
		t.Fatalf("manager state = %q, want %q", apply.ManagerState, configkit.LifecycleStateDegraded)
	}
}

func TestManagerApplyResultRetainsResultingStateAfterLaterMutation(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()

	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("initial apply: %v", err)
	}
	degraded, err := manager.Apply(context.Background(), failedStatusTestResult("reload failed"))
	if err != nil {
		t.Fatalf("failed apply: %v", err)
	}
	if degraded.ManagerState != configkit.LifecycleStateDegraded {
		t.Fatalf("failed apply manager state = %q, want %q", degraded.ManagerState, configkit.LifecycleStateDegraded)
	}

	recovered, err := manager.Apply(context.Background(), succeededStatusTestResult("v2", "sum-2"))
	if err != nil {
		t.Fatalf("recovery apply: %v", err)
	}
	if recovered.ManagerState != configkit.LifecycleStateLoaded {
		t.Fatalf("recovery apply manager state = %q, want %q", recovered.ManagerState, configkit.LifecycleStateLoaded)
	}
	if degraded.ManagerState != configkit.LifecycleStateDegraded {
		t.Fatalf("earlier apply manager state = %q after recovery, want immutable %q", degraded.ManagerState, configkit.LifecycleStateDegraded)
	}
}

func TestManagerApplyRecordsAppliedAtForSuccessfulAndFailedResults(t *testing.T) {
	tests := []struct {
		name       string
		result     func(time.Time) configkit.LoadResult[stepsTestConfig]
		wantState  configkit.LifecycleState
		wantLoaded bool
	}{
		{
			name: "successful",
			result: func(endedAt time.Time) configkit.LoadResult[stepsTestConfig] {
				return succeededStatusTestResultAt("v1", "sum-1", endedAt)
			},
			wantState:  configkit.LifecycleStateLoaded,
			wantLoaded: true,
		},
		{
			name: "failed",
			result: func(endedAt time.Time) configkit.LoadResult[stepsTestConfig] {
				return failedStatusTestResultAt("safe failure", endedAt)
			},
			wantState: configkit.LifecycleStateFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := configkit.NewManager[stepsTestConfig]()
			endedAt := time.Now().UTC().Add(-2 * time.Hour)
			result := tt.result(endedAt)

			before := time.Now().UTC()
			apply, err := manager.Apply(context.Background(), result)
			after := time.Now().UTC()
			if err != nil {
				t.Fatalf("apply result: %v", err)
			}
			if apply.AppliedAt.Before(before) || apply.AppliedAt.After(after) {
				t.Fatalf("applied at = %v, want between %v and %v", apply.AppliedAt, before, after)
			}
			if apply.AppliedAt.Location() != time.UTC {
				t.Fatalf("applied at location = %v, want UTC", apply.AppliedAt.Location())
			}
			if apply.ManagerState != tt.wantState {
				t.Fatalf("apply manager state = %q, want %q", apply.ManagerState, tt.wantState)
			}

			status := manager.LifecycleStatus()
			if status.State != tt.wantState {
				t.Fatalf("state = %q, want %q", status.State, tt.wantState)
			}
			if status.LastApply == nil || !status.LastApply.AppliedAt.Equal(apply.AppliedAt) || status.LastApply.ManagerState != tt.wantState {
				t.Fatalf("last apply = %+v, want applied_at %v and state %q", status.LastApply, apply.AppliedAt, tt.wantState)
			}
			if status.LastAttempt == nil || !status.LastAttempt.EndedAt.Equal(endedAt) {
				t.Fatalf("last attempt = %+v, want ended_at %v preserved", status.LastAttempt, endedAt)
			}
			if tt.wantLoaded {
				if status.Current == nil || !status.Current.LoadedAt.Equal(endedAt) {
					t.Fatalf("current = %+v, want loaded_at %v preserved", status.Current, endedAt)
				}
			}
		})
	}
}

func TestManagerOwnedLoadsRecordAppliedAt(t *testing.T) {
	tests := []struct {
		name string
		load func(*configkit.Manager[stepsTestConfig]) (configkit.ManagedLoadResult[stepsTestConfig], error)
	}{
		{
			name: "load",
			load: func(manager *configkit.Manager[stepsTestConfig]) (configkit.ManagedLoadResult[stepsTestConfig], error) {
				return manager.Load(context.Background(), configkit.AttemptKindInitialLoad, configkit.SourceData{
					Data:     []byte(`{"name":"api","enabled":true,"port":8080}`),
					Metadata: configkit.SourceMetadata{Name: "memory", Kind: "memory"},
					Revision: "v1",
				}, testPipeline())
			},
		},
		{
			name: "load from source",
			load: func(manager *configkit.Manager[stepsTestConfig]) (configkit.ManagedLoadResult[stepsTestConfig], error) {
				source := configkit.NewBytesSource(
					[]byte(`{"name":"api","enabled":true,"port":8080}`),
					configkit.SourceMetadata{Name: "memory", Kind: "memory"},
					"v1",
				)
				return manager.LoadFromSource(context.Background(), configkit.AttemptKindInitialLoad, source, testPipeline())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := configkit.NewManager[stepsTestConfig]()
			result, err := tt.load(manager)
			if err != nil {
				t.Fatalf("manager load: %v", err)
			}
			if result.Apply.AppliedAt.IsZero() {
				t.Fatal("apply applied_at is zero")
			}
			if result.Apply.AppliedAt.Before(result.Load.Attempt.EndedAt) {
				t.Fatalf("applied at = %v, want at or after attempt ended_at %v", result.Apply.AppliedAt, result.Load.Attempt.EndedAt)
			}
			if result.Apply.ManagerState != configkit.LifecycleStateLoaded {
				t.Fatalf("apply manager state = %q, want %q", result.Apply.ManagerState, configkit.LifecycleStateLoaded)
			}
			status := manager.LifecycleStatus()
			if status.LastApply == nil || !status.LastApply.AppliedAt.Equal(result.Apply.AppliedAt) {
				t.Fatalf("last apply = %+v, want applied_at %v", status.LastApply, result.Apply.AppliedAt)
			}
		})
	}
}

func TestManagerLoadEmptyChecksumPreservesLastKnownGood(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()
	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("seed manager: %v", err)
	}
	pipeline := testPipeline()
	pipeline.Checksum = func(context.Context, stepsTestConfig) (string, error) {
		return "", nil
	}

	result, err := manager.Load(context.Background(), configkit.AttemptKindReload, configkit.SourceData{
		Data:     []byte(`{"name":"worker","enabled":false,"port":9090}`),
		Revision: "v2",
	}, pipeline)
	if err == nil || !strings.Contains(err.Error(), "checksummer returned an empty checksum") {
		t.Fatalf("manager reload error = %v, want private empty-checksum contract failure", err)
	}
	if result.Load.Attempt.Status != configkit.AttemptStatusFailed || result.Load.Attempt.Stage != configkit.AttemptStageChecksum {
		t.Fatalf("load attempt = %+v, want failed checksum attempt", result.Load.Attempt)
	}
	if result.Load.Attempt.Failure == nil || result.Load.Attempt.Failure.Code != configkit.FailureCodeChecksumFailed || result.Load.Attempt.Failure.Message != "config checksum failed" {
		t.Fatalf("load failure = %+v, want safe checksum failure", result.Load.Attempt.Failure)
	}
	if result.Apply.Published || result.Apply.Changed {
		t.Fatalf("apply published/changed = %t/%t, want false/false", result.Apply.Published, result.Apply.Changed)
	}
	if result.Apply.AppliedAt.IsZero() {
		t.Fatal("failed apply applied_at is zero")
	}
	if result.Apply.Previous == nil || result.Apply.Current == nil || result.Apply.Previous.Checksum != "sum-1" || result.Apply.Current.Checksum != "sum-1" {
		t.Fatalf("apply = %+v, want retained sum-1 snapshot", result.Apply)
	}
	if result.Apply.ManagerState != configkit.LifecycleStateDegraded {
		t.Fatalf("apply manager state = %q, want %q", result.Apply.ManagerState, configkit.LifecycleStateDegraded)
	}

	value, ok := manager.Value()
	if !ok || value.Name != "api" || !value.Enabled || value.Port != 8080 {
		t.Fatalf("manager value = %+v, ok = %t; want last-known-good api config", value, ok)
	}
	status := manager.LifecycleStatus()
	if status.State != configkit.LifecycleStateDegraded || status.Current == nil || status.Current.Revision != "v1" {
		t.Fatalf("manager status = %+v, want degraded with v1 snapshot", status)
	}
	if status.LastFailure == nil || status.LastFailure.Stage != configkit.AttemptStageChecksum {
		t.Fatalf("last failure = %+v, want checksum-stage failure", status.LastFailure)
	}
}

func TestManagerApplyRejectsInvalidLoadResultWithoutMutation(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()

	apply, err := manager.Apply(context.Background(), configkit.LoadResult[stepsTestConfig]{
		Attempt: configkit.AttemptRecord{Status: configkit.AttemptStatusSucceeded},
	})
	if !errors.Is(err, configkit.ErrInvalidLoadResult) {
		t.Fatalf("apply invalid result error = %v, want configkit.ErrInvalidLoadResult", err)
	}
	if apply != (configkit.ApplyResult{}) {
		t.Fatalf("apply result = %+v, want zero result", apply)
	}

	status := manager.LifecycleStatus()
	if status.State != configkit.LifecycleStateUnloaded {
		t.Fatalf("state = %q, want %q", status.State, configkit.LifecycleStateUnloaded)
	}
	if len(manager.Attempts()) != 0 {
		t.Fatalf("attempt history len = %d, want 0", len(manager.Attempts()))
	}
}

func TestManagerApplyAssignsFreshAttemptID(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()
	result := succeededStatusTestResult("v1", "sum-1")
	result.Attempt.ID = 99

	if _, err := manager.Apply(context.Background(), result); err != nil {
		t.Fatalf("apply succeeded result: %v", err)
	}

	status := manager.LifecycleStatus()
	if status.LastAttempt == nil || status.LastAttempt.ID != 1 {
		t.Fatalf("last attempt = %+v, want manager-assigned id 1", status.LastAttempt)
	}
	if result.Attempt.ID != 99 {
		t.Fatalf("input result attempt id = %d, want unchanged 99", result.Attempt.ID)
	}
}

func TestManagerAttemptsHonorsHistoryLimit(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig](configkit.WithAttemptHistoryLimit(2))

	for _, revision := range []string{"v1", "v2", "v3"} {
		if _, err := manager.Apply(context.Background(), succeededStatusTestResult(revision, revision+"-sum")); err != nil {
			t.Fatalf("apply %s: %v", revision, err)
		}
	}

	attempts := manager.Attempts()
	if len(attempts) != 2 {
		t.Fatalf("attempt history len = %d, want 2", len(attempts))
	}
	if attempts[0].Revision != "v2" || attempts[1].Revision != "v3" {
		t.Fatalf("attempt revisions = %q, %q; want v2, v3", attempts[0].Revision, attempts[1].Revision)
	}
	if attempts[0].ID != 2 || attempts[1].ID != 3 {
		t.Fatalf("attempt ids = %d, %d; want 2, 3", attempts[0].ID, attempts[1].ID)
	}
}

func TestManagerAttemptsDisabledPreservesStatusPointers(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig](configkit.WithAttemptHistoryLimit(0))

	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("apply succeeded result: %v", err)
	}

	if attempts := manager.Attempts(); len(attempts) != 0 {
		t.Fatalf("attempt history len = %d, want 0", len(attempts))
	}
	status := manager.LifecycleStatus()
	if status.LastAttempt == nil || status.LastSuccess == nil {
		t.Fatalf("status = %+v, want last attempt and last success preserved", status)
	}
}

func TestManagerAttemptsReturnsCopy(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()

	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("apply succeeded result: %v", err)
	}
	attempts := manager.Attempts()
	attempts[0].Checksum = "mutated"

	next := manager.Attempts()
	if next[0].Checksum != "sum-1" {
		t.Fatalf("attempt checksum after external mutation = %q, want sum-1", next[0].Checksum)
	}
}

func TestManagerApplyDetachesFailureFromCaller(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()
	result := failedStatusTestResult("safe failure")

	if _, err := manager.Apply(context.Background(), result); err != nil {
		t.Fatalf("apply failed result: %v", err)
	}
	result.Attempt.Failure.Message = "caller mutation"

	status := manager.LifecycleStatus()
	if status.LastFailure == nil || failureMessage(status.LastFailure.Failure) != "safe failure" {
		t.Fatalf("last failure = %+v, want detached safe failure", status.LastFailure)
	}
	attempts := manager.Attempts()
	if len(attempts) != 1 || failureMessage(attempts[0].Failure) != "safe failure" {
		t.Fatalf("attempts = %+v, want detached safe failure", attempts)
	}
}

func TestManagerLoadDetachesFailureFromObserverMutation(t *testing.T) {
	var secondObserverMessage string
	observer := configkit.Observer(func(_ context.Context, event configkit.Event) {
		if event.Kind == configkit.EventKindLoadFailed && event.Attempt != nil && event.Attempt.Failure != nil {
			event.Attempt.Failure.Message = "observer mutation"
		}
	})
	secondObserver := configkit.Observer(func(_ context.Context, event configkit.Event) {
		if event.Kind == configkit.EventKindLoadFailed && event.Attempt != nil {
			secondObserverMessage = failureMessage(event.Attempt.Failure)
		}
	})
	pipeline := testPipeline()
	pipeline.ValidateConfig = func(context.Context, stepsTestConfig) error {
		return errors.New("private validation detail")
	}
	manager := configkit.NewManager[stepsTestConfig](configkit.WithObservers(observer, secondObserver))

	result, err := manager.Load(context.Background(), configkit.AttemptKindReload, configkit.SourceData{
		Data: []byte(`{"name":"api","enabled":true,"port":8080}`),
	}, pipeline)
	if err == nil {
		t.Fatal("load error = nil, want validation failure")
	}
	if got := failureMessage(result.Load.Attempt.Failure); got != "config validation failed" {
		t.Fatalf("returned failure = %q, want config validation failed", got)
	}
	status := manager.LifecycleStatus()
	if status.LastFailure == nil || failureMessage(status.LastFailure.Failure) != "config validation failed" {
		t.Fatalf("last failure = %+v, want detached config validation failure", status.LastFailure)
	}
	if secondObserverMessage != "config validation failed" {
		t.Fatalf("second observer failure = %q, want detached config validation failure", secondObserverMessage)
	}
}

func TestManagerInspectReturnsStatusAndRedactedCopy(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()

	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("apply succeeded result: %v", err)
	}

	inspection := manager.LifecycleInspection()
	if inspection.Status.State != configkit.LifecycleStateLoaded {
		t.Fatalf("inspection state = %q, want %q", inspection.Status.State, configkit.LifecycleStateLoaded)
	}
	if got := inspection.Redacted["name"]; got != "api" {
		t.Fatalf("redacted name = %v, want api", got)
	}
	inspection.Redacted["name"] = "mutated"

	next := manager.LifecycleInspection()
	if got := next.Redacted["name"]; got != "api" {
		t.Fatalf("redacted name after external mutation = %v, want api", got)
	}
}

func TestManagerConcurrentReadsDuringApply(t *testing.T) {
	const (
		writerIterations = 300
		readerCount      = 8
		readerIterations = 1000
	)

	ctx := context.Background()
	manager := configkit.NewManager[stepsTestConfig]()
	start := make(chan struct{})
	errs := make(chan error, readerCount+1)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start

		for i := 0; i < writerIterations; i++ {
			revision := "v" + strconv.Itoa(i)
			checksum := "sum-" + strconv.Itoa(i)
			if _, err := manager.Apply(ctx, succeededStatusTestResult(revision, checksum)); err != nil {
				errs <- err
				return
			}
			if _, err := manager.Apply(ctx, failedStatusTestResult("reload failed")); err != nil {
				errs <- err
				return
			}
		}
	}()

	for i := 0; i < readerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			for j := 0; j < readerIterations; j++ {
				status := manager.LifecycleStatus()
				if !validManagerState(status.State) {
					errs <- errors.New("invalid manager state: " + string(status.State))
					return
				}

				inspection := manager.LifecycleInspection()
				if !validManagerState(inspection.Status.State) {
					errs <- errors.New("invalid inspection state: " + string(inspection.Status.State))
					return
				}

				if value, ok := manager.Value(); ok {
					if value.Name != "api" || !value.Enabled || value.Port != 8080 {
						errs <- errors.New("unexpected config value")
						return
					}
				}

				if snapshot, ok := manager.Snapshot(); ok {
					if snapshot.Metadata().Checksum == "" {
						errs <- errors.New("snapshot checksum is empty")
						return
					}
				}

				if attempts := manager.Attempts(); len(attempts) > 20 {
					errs <- errors.New("attempt history exceeded default limit")
					return
				}
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestZeroValueManagerCanceledBeforeAdmissionHasNoEffects(t *testing.T) {
	var manager configkit.Manager[stepsTestConfig]
	source := &admissionTrackingSource{
		data: configkit.SourceData{
			Data:     []byte(`{"name":"api","enabled":true,"port":8080}`),
			Revision: "canceled",
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	loadResult, err := manager.Load(ctx, configkit.AttemptKindReload, source.data, testPipeline())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("load error = %v, want context.Canceled", err)
	}
	if loadResult.Load.Attempt.ID != 0 || loadResult.Load.Attempt.Status != "" || loadResult.Load.Snapshot != nil {
		t.Fatalf("load result = %+v, want zero result", loadResult)
	}
	if loadResult.Apply != (configkit.ApplyResult{}) {
		t.Fatalf("load apply result = %+v, want zero result", loadResult.Apply)
	}

	sourceResult, err := manager.LoadFromSource(ctx, configkit.AttemptKindReload, source, testPipeline())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("load from source error = %v, want context.Canceled", err)
	}
	if sourceResult.Load.Attempt.ID != 0 || sourceResult.Load.Attempt.Status != "" || sourceResult.Load.Snapshot != nil {
		t.Fatalf("load from source result = %+v, want zero result", sourceResult)
	}
	if sourceResult.Apply != (configkit.ApplyResult{}) {
		t.Fatalf("load from source apply result = %+v, want zero result", sourceResult.Apply)
	}
	if source.metadataCalls.Load() != 0 || source.readCalls.Load() != 0 {
		t.Fatalf("source calls = metadata %d, read %d; want zero", source.metadataCalls.Load(), source.readCalls.Load())
	}

	applyResult, err := manager.Apply(ctx, succeededStatusTestResult("canceled", "sum-canceled"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("apply error = %v, want context.Canceled", err)
	}
	if applyResult != (configkit.ApplyResult{}) {
		t.Fatalf("apply result = %+v, want zero result", applyResult)
	}
	assertUnchangedManager(t, &manager)

	succeeded, err := manager.Load(context.Background(), configkit.AttemptKindInitialLoad, configkit.SourceData{
		Data:     []byte(`{"name":"api","enabled":true,"port":8080}`),
		Revision: "v1",
	}, testPipeline())
	if err != nil {
		t.Fatalf("load after canceled calls: %v", err)
	}
	if succeeded.Load.Attempt.ID != 1 {
		t.Fatalf("first admitted attempt id = %d, want 1", succeeded.Load.Attempt.ID)
	}
}

func TestManagerCancellationImmediatelyAfterAcquisitionHasNoEffects(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()
	baseCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &cancelOnSecondErrContext{
		Context: baseCtx,
		cancel:  cancel,
	}

	result, err := manager.Load(ctx, configkit.AttemptKindReload, configkit.SourceData{
		Data:     []byte(`{"name":"api","enabled":true,"port":8080}`),
		Revision: "canceled",
	}, testPipeline())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("load error = %v, want context.Canceled", err)
	}
	if result.Load.Attempt.ID != 0 || result.Load.Attempt.Status != "" || result.Load.Snapshot != nil {
		t.Fatalf("load result = %+v, want zero result", result)
	}
	if result.Apply != (configkit.ApplyResult{}) {
		t.Fatalf("load apply result = %+v, want zero result", result.Apply)
	}
	assertUnchangedManager(t, manager)

	succeeded, err := manager.Load(context.Background(), configkit.AttemptKindInitialLoad, configkit.SourceData{
		Data:     []byte(`{"name":"api","enabled":true,"port":8080}`),
		Revision: "v1",
	}, testPipeline())
	if err != nil {
		t.Fatalf("load after acquisition cancellation: %v", err)
	}
	if succeeded.Load.Attempt.ID != 1 {
		t.Fatalf("first admitted attempt id = %d, want 1", succeeded.Load.Attempt.ID)
	}
}

func TestManagerLifecycleWaiterReturnsOnCancellationWithoutEffects(t *testing.T) {
	manager, events, firstDone, releaseFirst := startBlockedManagerLoad(t)
	source := &admissionTrackingSource{
		data: configkit.SourceData{
			Data:     []byte(`{"name":"api","enabled":true,"port":8080}`),
			Revision: "canceled",
		},
	}
	baseCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := newDoneObservedContext(baseCtx)
	type outcome struct {
		result configkit.ManagedLoadResult[stepsTestConfig]
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := manager.LoadFromSource(ctx, configkit.AttemptKindReload, source, testPipeline())
		done <- outcome{result: result, err: err}
	}()

	receiveManagerTestValue(t, ctx.doneObserved)
	cancel()
	got := receiveManagerTestValue(t, done)
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("waiting load error = %v, want context.Canceled", got.err)
	}
	if got.result.Load.Attempt.ID != 0 || got.result.Load.Attempt.Status != "" || got.result.Load.Snapshot != nil {
		t.Fatalf("waiting load result = %+v, want zero result", got.result)
	}
	if got.result.Apply != (configkit.ApplyResult{}) {
		t.Fatalf("waiting load apply result = %+v, want zero result", got.result.Apply)
	}
	if source.metadataCalls.Load() != 0 || source.readCalls.Load() != 0 {
		t.Fatalf("source calls = metadata %d, read %d; want zero", source.metadataCalls.Load(), source.readCalls.Load())
	}
	assertUnchangedManager(t, manager)

	releaseFirst()
	if err := receiveManagerTestValue(t, firstDone); err != nil {
		t.Fatalf("first load: %v", err)
	}
	if got := len(events); got != 3 {
		t.Fatalf("event count = %d, want only the first load's 3 events", got)
	}
	for i := 0; i < 3; i++ {
		event := <-events
		if event.AttemptID != 1 {
			t.Fatalf("event attempt id = %d, want 1", event.AttemptID)
		}
	}

	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v2", "sum-2")); err != nil {
		t.Fatalf("apply after canceled waiter: %v", err)
	}
	attempts := manager.Attempts()
	if len(attempts) != 2 || attempts[0].ID != 1 || attempts[1].ID != 2 {
		t.Fatalf("attempts = %+v, want admitted attempt ids 1 and 2", attempts)
	}
}

func TestManagerLifecycleWaiterReturnsOnDeadline(t *testing.T) {
	manager, _, firstDone, releaseFirst := startBlockedManagerLoad(t)
	baseCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	ctx := newDoneObservedContext(baseCtx)
	type outcome struct {
		result configkit.ManagedLoadResult[stepsTestConfig]
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := manager.Load(ctx, configkit.AttemptKindReload, configkit.SourceData{
			Data:     []byte(`{"name":"api","enabled":true,"port":8080}`),
			Revision: "timed-out",
		}, testPipeline())
		done <- outcome{result: result, err: err}
	}()

	receiveManagerTestValue(t, ctx.doneObserved)
	got := receiveManagerTestValue(t, done)
	if !errors.Is(got.err, context.DeadlineExceeded) {
		t.Fatalf("waiting load error = %v, want context.DeadlineExceeded", got.err)
	}
	if got.result.Load.Attempt.ID != 0 || got.result.Load.Attempt.Status != "" || got.result.Load.Snapshot != nil {
		t.Fatalf("waiting load result = %+v, want zero result", got.result)
	}
	if got.result.Apply != (configkit.ApplyResult{}) {
		t.Fatalf("waiting load apply result = %+v, want zero result", got.result.Apply)
	}
	assertUnchangedManager(t, manager)

	releaseFirst()
	if err := receiveManagerTestValue(t, firstDone); err != nil {
		t.Fatalf("first load: %v", err)
	}
}

func TestManagerApplyCanceledWhileWaitingDoesNotPublish(t *testing.T) {
	manager, _, firstDone, releaseFirst := startBlockedManagerLoad(t)
	baseCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := newDoneObservedContext(baseCtx)
	type outcome struct {
		result configkit.ApplyResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := manager.Apply(ctx, succeededStatusTestResult("canceled", "sum-canceled"))
		done <- outcome{result: result, err: err}
	}()

	receiveManagerTestValue(t, ctx.doneObserved)
	cancel()
	got := receiveManagerTestValue(t, done)
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("waiting apply error = %v, want context.Canceled", got.err)
	}
	if got.result != (configkit.ApplyResult{}) {
		t.Fatalf("waiting apply result = %+v, want zero result", got.result)
	}
	assertUnchangedManager(t, manager)

	releaseFirst()
	if err := receiveManagerTestValue(t, firstDone); err != nil {
		t.Fatalf("first load: %v", err)
	}
	snapshot, ok := manager.Snapshot()
	if !ok || snapshot.Metadata().Revision != "v1" {
		t.Fatalf("current snapshot = %+v, ok = %t; want first load revision v1", snapshot.Metadata(), ok)
	}
	if status := manager.LifecycleStatus(); status.LastAttempt == nil || status.LastAttempt.ID != 1 {
		t.Fatalf("last attempt = %+v, want first admitted attempt id 1", status.LastAttempt)
	}
}

func TestManagerCancellationAfterAdmissionRecordsFailedAttempt(t *testing.T) {
	enteredDecode := make(chan struct{})
	pipeline := testPipeline()
	pipeline.Decode = func(ctx context.Context, data configkit.SourceData) (stepsTestConfig, error) {
		close(enteredDecode)
		<-ctx.Done()
		return stepsTestConfig{}, ctx.Err()
	}
	manager := configkit.NewManager[stepsTestConfig]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		result configkit.ManagedLoadResult[stepsTestConfig]
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := manager.Load(ctx, configkit.AttemptKindInitialLoad, configkit.SourceData{
			Data: []byte(`{"name":"api","enabled":true,"port":8080}`),
		}, pipeline)
		done <- outcome{result: result, err: err}
	}()

	receiveManagerTestValue(t, enteredDecode)
	cancel()
	got := receiveManagerTestValue(t, done)
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("admitted load error = %v, want context.Canceled", got.err)
	}
	if got.result.Load.Attempt.ID != 1 {
		t.Fatalf("attempt id = %d, want 1", got.result.Load.Attempt.ID)
	}
	if got.result.Load.Attempt.Status != configkit.AttemptStatusFailed || got.result.Load.Attempt.Stage != configkit.AttemptStageDecode {
		t.Fatalf("attempt = %+v, want failed decode attempt", got.result.Load.Attempt)
	}
	if got.result.Apply.ManagerState != configkit.LifecycleStateFailed {
		t.Fatalf("apply manager state = %q, want %q", got.result.Apply.ManagerState, configkit.LifecycleStateFailed)
	}
	status := manager.LifecycleStatus()
	if status.State != configkit.LifecycleStateFailed || status.LastFailure == nil || status.LastFailure.ID != 1 {
		t.Fatalf("status = %+v, want recorded failed attempt 1", status)
	}
	if attempts := manager.Attempts(); len(attempts) != 1 || attempts[0].ID != 1 {
		t.Fatalf("attempts = %+v, want recorded attempt 1", attempts)
	}
}

func TestManagerApplyCancellationAfterAdmissionDoesNotRollbackPublication(t *testing.T) {
	notifying := make(chan struct{})
	observerCanceled := make(chan struct{})
	releaseObserver := make(chan struct{})
	observer := configkit.Observer(func(ctx context.Context, event configkit.Event) {
		if event.Kind != configkit.EventKindSnapshotApplied {
			return
		}
		close(notifying)
		<-ctx.Done()
		close(observerCanceled)
		<-releaseObserver
	})
	manager := configkit.NewManager[stepsTestConfig](configkit.WithObservers(observer))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			close(releaseObserver)
		})
	}
	t.Cleanup(release)
	type outcome struct {
		result configkit.ApplyResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := manager.Apply(ctx, succeededStatusTestResult("v1", "sum-1"))
		done <- outcome{result: result, err: err}
	}()

	receiveManagerTestValue(t, notifying)
	cancel()
	receiveManagerTestValue(t, observerCanceled)
	snapshot, ok := manager.Snapshot()
	if !ok || snapshot.Metadata().Revision != "v1" {
		t.Fatalf("current snapshot = %+v, ok = %t; want published revision v1", snapshot.Metadata(), ok)
	}
	release()
	got := receiveManagerTestValue(t, done)
	if got.err != nil {
		t.Fatalf("admitted apply error = %v, want nil", got.err)
	}
	if !got.result.Published {
		t.Fatalf("apply result = %+v, want published", got.result)
	}
	if got.result.ManagerState != configkit.LifecycleStateLoaded {
		t.Fatalf("apply manager state = %q, want %q", got.result.ManagerState, configkit.LifecycleStateLoaded)
	}
}

func TestManagerLoadAppliesSuccessfulResult(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()

	result, err := manager.Load(context.Background(), configkit.AttemptKindInitialLoad, configkit.SourceData{
		Data:     []byte(`{"name":"api","enabled":true,"port":8080}`),
		Metadata: configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		Revision: "rev-1",
	}, testPipeline())
	if err != nil {
		t.Fatalf("manager load: %v", err)
	}

	if result.Load.Attempt.ID == 0 {
		t.Fatal("load attempt id = 0, want manager-assigned id")
	}
	if result.Load.Snapshot == nil {
		t.Fatal("load snapshot = nil, want snapshot")
	}
	if !result.Apply.Published {
		t.Fatal("apply published = false, want true")
	}
	if result.Apply.ManagerState != configkit.LifecycleStateLoaded {
		t.Fatalf("apply manager state = %q, want %q", result.Apply.ManagerState, configkit.LifecycleStateLoaded)
	}
	if status := manager.LifecycleStatus(); status.State != configkit.LifecycleStateLoaded {
		t.Fatalf("manager state = %q, want %q", status.State, configkit.LifecycleStateLoaded)
	}
}

func TestManagerLoadFromSourceMissingSourceRecordsFailure(t *testing.T) {
	var typedNil *typedNilTestSource
	tests := []struct {
		name   string
		source configkit.Source
	}{
		{name: "nil"},
		{name: "typed nil", source: typedNil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := configkit.NewManager[stepsTestConfig]()

			result, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindReload, tt.source, testPipeline())
			if !errors.Is(err, configkit.ErrMissingSource) {
				t.Fatalf("load from missing source error = %v, want configkit.ErrMissingSource", err)
			}
			if result.Load.Attempt.ID == 0 {
				t.Fatal("load attempt id = 0, want manager-assigned id")
			}
			if result.Load.Attempt.Status != configkit.AttemptStatusFailed {
				t.Fatalf("attempt status = %q, want %q", result.Load.Attempt.Status, configkit.AttemptStatusFailed)
			}
			if result.Load.Attempt.Failure == nil || result.Load.Attempt.Failure.Code != configkit.FailureCodeMissingSource {
				t.Fatalf("attempt failure = %+v, want missing source", result.Load.Attempt.Failure)
			}
			if result.Apply.Published {
				t.Fatal("published = true, want false")
			}
			if result.Apply.ManagerState != configkit.LifecycleStateFailed {
				t.Fatalf("apply manager state = %q, want %q", result.Apply.ManagerState, configkit.LifecycleStateFailed)
			}
			if status := manager.LifecycleStatus(); status.State != configkit.LifecycleStateFailed {
				t.Fatalf("manager state = %q, want %q", status.State, configkit.LifecycleStateFailed)
			}
			if attempts := manager.Attempts(); len(attempts) != 1 || attempts[0].Failure == nil || attempts[0].Failure.Code != configkit.FailureCodeMissingSource {
				t.Fatalf("manager attempts = %+v, want one missing-source failure", attempts)
			}
		})
	}
}

func TestManagerLoadFromTypedNilSourcePreservesLastKnownGood(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()
	validSource := configkit.NewBytesSource(
		[]byte(`{"name":"api","enabled":true,"port":8080}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)
	if _, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindInitialLoad, validSource, testPipeline()); err != nil {
		t.Fatalf("initial load: %v", err)
	}

	var source *typedNilTestSource
	result, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindReload, source, testPipeline())
	if !errors.Is(err, configkit.ErrMissingSource) {
		t.Fatalf("reload missing source error = %v, want configkit.ErrMissingSource", err)
	}
	if result.Load.Attempt.Failure == nil || result.Load.Attempt.Failure.Code != configkit.FailureCodeMissingSource {
		t.Fatalf("reload failure = %+v, want missing source", result.Load.Attempt.Failure)
	}
	if result.Apply.ManagerState != configkit.LifecycleStateDegraded {
		t.Fatalf("apply manager state = %q, want %q", result.Apply.ManagerState, configkit.LifecycleStateDegraded)
	}
	status := manager.LifecycleStatus()
	if status.State != configkit.LifecycleStateDegraded || status.Current == nil || status.Current.Revision != "rev-1" {
		t.Fatalf("manager status = %+v, want degraded with current rev-1", status)
	}
	if attempts := manager.Attempts(); len(attempts) != 2 {
		t.Fatalf("manager attempts = %d, want initial load and failed reload", len(attempts))
	}
}

func TestManagerLoadFromSourceReadsMetadataOnce(t *testing.T) {
	source := &countingMetadataSource{
		data: configkit.SourceData{
			Data:     []byte(`{"name":"api","enabled":true,"port":8080}`),
			Revision: "rev-1",
		},
	}
	manager := configkit.NewManager[stepsTestConfig]()

	if _, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindInitialLoad, source, testPipeline()); err != nil {
		t.Fatalf("manager load from source: %v", err)
	}

	if source.metadataCalls != 1 {
		t.Fatalf("metadata calls = %d, want 1", source.metadataCalls)
	}
}

func TestManagerLoadFromSourceUsesSameMetadataForStartedEventAndAttempt(t *testing.T) {
	var events []configkit.Event
	observer := configkit.Observer(func(ctx context.Context, event configkit.Event) {
		events = append(events, event)
	})
	source := &countingMetadataSource{
		data: configkit.SourceData{
			Data:     []byte(`{"name":"api","enabled":true,"port":8080}`),
			Revision: "rev-1",
		},
	}
	manager := configkit.NewManager[stepsTestConfig](configkit.WithObservers(observer))

	result, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindInitialLoad, source, testPipeline())
	if err != nil {
		t.Fatalf("manager load from source: %v", err)
	}
	if source.metadataCalls != 1 {
		t.Fatalf("metadata calls = %d, want 1", source.metadataCalls)
	}
	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3", len(events))
	}

	startedMetadata := events[0].Source
	if startedMetadata != result.Load.Attempt.Source {
		t.Fatalf("started metadata = %+v, attempt source = %+v; want same", startedMetadata, result.Load.Attempt.Source)
	}
	if events[1].Attempt == nil || events[1].Attempt.Source != startedMetadata {
		t.Fatalf("finished attempt source = %+v, want %+v", events[1].Attempt, startedMetadata)
	}
	if result.Load.Snapshot == nil {
		t.Fatal("snapshot = nil, want snapshot")
	}
	if snapshotMetadata := result.Load.Snapshot.Metadata().Source; snapshotMetadata != startedMetadata {
		t.Fatalf("snapshot source = %+v, want %+v", snapshotMetadata, startedMetadata)
	}
}

func TestManagerLoadFromSourceMetadataPanicRecordsSourceReadFailure(t *testing.T) {
	source := &countingMetadataSource{panicMetadata: true}
	manager := configkit.NewManager[stepsTestConfig]()

	result, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindReload, source, testPipeline())
	if err == nil {
		t.Fatal("manager load source metadata panic error = nil, want error")
	}
	if result.Load.Attempt.Stage != configkit.AttemptStageSourceRead {
		t.Fatalf("attempt stage = %q, want %q", result.Load.Attempt.Stage, configkit.AttemptStageSourceRead)
	}
	if result.Load.Attempt.Status != configkit.AttemptStatusFailed {
		t.Fatalf("attempt status = %q, want %q", result.Load.Attempt.Status, configkit.AttemptStatusFailed)
	}
	if source.metadataCalls != 1 {
		t.Fatalf("metadata calls = %d, want 1", source.metadataCalls)
	}
}

func TestManagerLifecyclePanicPayloadNotExposed(t *testing.T) {
	const secret = "postgres://user:pass@example/db"
	panicErr := errors.New(secret)
	var events []configkit.Event
	observer := configkit.Observer(func(ctx context.Context, event configkit.Event) {
		events = append(events, event)
	})
	pipeline := testPipeline()
	pipeline.ValidateConfig = func(ctx context.Context, value stepsTestConfig) error {
		panic(panicErr)
	}
	manager := configkit.NewManager[stepsTestConfig](configkit.WithObservers(observer))

	result, err := manager.Load(context.Background(), configkit.AttemptKindReload, configkit.SourceData{
		Data: []byte(`{"name":"api","enabled":true,"port":8080}`),
	}, pipeline)
	if err == nil {
		t.Fatal("manager load panic error = nil, want error")
	}
	if !errors.Is(err, configkit.ErrLifecyclePanicked) {
		t.Fatalf("manager load panic error = %v, want configkit.ErrLifecyclePanicked", err)
	}
	if errors.Is(err, panicErr) {
		t.Fatalf("manager load panic error = %v, must not unwrap recovered panic error", err)
	}
	assertStringOmits(t, "returned load error", err.Error(), secret)
	assertStringOmits(t, "load result attempt failure", failureMessage(result.Load.Attempt.Failure), secret)
	if failureMessage(result.Load.Attempt.Failure) != "config validation failed" {
		t.Fatalf("attempt failure = %+v, want safe validation failure", result.Load.Attempt.Failure)
	}

	status := manager.LifecycleStatus()
	if status.LastAttempt == nil {
		t.Fatal("status last attempt = nil, want failed attempt")
	}
	assertStringOmits(t, "manager status", failureMessage(status.LastAttempt.Failure), secret)
	if status.LastFailure == nil {
		t.Fatal("status last failure = nil, want failed attempt")
	}
	assertStringOmits(t, "manager last failure", failureMessage(status.LastFailure.Failure), secret)

	inspection := manager.LifecycleInspection()
	if inspection.Status.LastAttempt == nil {
		t.Fatal("inspection last attempt = nil, want failed attempt")
	}
	assertStringOmits(t, "manager inspection", failureMessage(inspection.Status.LastAttempt.Failure), secret)

	if len(events) != 2 {
		t.Fatalf("event count = %d, want load started and failed", len(events))
	}
	failed := events[1]
	if failed.Kind != configkit.EventKindLoadFailed {
		t.Fatalf("event kind = %q, want %q", failed.Kind, configkit.EventKindLoadFailed)
	}
	if failed.Attempt == nil {
		t.Fatal("failed event attempt = nil, want attempt")
	}
	assertStringOmits(t, "observer event attempt failure", failureMessage(failed.Attempt.Failure), secret)
}

func testPipeline() configkit.Pipeline[stepsTestConfig] {
	return configkit.Pipeline[stepsTestConfig]{
		Decode:   configkit.JSONDecoder[stepsTestConfig](),
		Redact:   configkit.EmptyRedactor[stepsTestConfig](),
		Checksum: configkit.SHA256JSONChecksum[stepsTestConfig](),
	}
}

func validManagerState(state configkit.LifecycleState) bool {
	switch state {
	case configkit.LifecycleStateUnloaded,
		configkit.LifecycleStateLoaded,
		configkit.LifecycleStateFailed,
		configkit.LifecycleStateDegraded:
		return true
	default:
		return false
	}
}

type countingMetadataSource struct {
	metadataCalls int
	panicMetadata bool
	data          configkit.SourceData
}

func (s *countingMetadataSource) Metadata() configkit.SourceMetadata {
	s.metadataCalls++
	if s.panicMetadata {
		panic("metadata boom")
	}

	return configkit.SourceMetadata{
		Name: "source-" + strconv.Itoa(s.metadataCalls),
		Kind: "memory",
	}
}

func (s *countingMetadataSource) Read(ctx context.Context) (configkit.SourceData, error) {
	return s.data, nil
}

type admissionTrackingSource struct {
	metadataCalls atomic.Int64
	readCalls     atomic.Int64
	data          configkit.SourceData
}

func (s *admissionTrackingSource) Metadata() configkit.SourceMetadata {
	s.metadataCalls.Add(1)
	return configkit.SourceMetadata{Name: "tracked", Kind: "memory"}
}

func (s *admissionTrackingSource) Read(context.Context) (configkit.SourceData, error) {
	s.readCalls.Add(1)
	return s.data, nil
}

type doneObservedContext struct {
	context.Context
	doneObserved chan struct{}
	once         sync.Once
}

type cancelOnSecondErrContext struct {
	context.Context
	cancel context.CancelFunc
	calls  atomic.Int64
}

func (c *cancelOnSecondErrContext) Err() error {
	if c.calls.Add(1) == 2 {
		c.cancel()
	}
	return c.Context.Err()
}

func newDoneObservedContext(ctx context.Context) *doneObservedContext {
	return &doneObservedContext{
		Context:      ctx,
		doneObserved: make(chan struct{}),
	}
}

func (c *doneObservedContext) Done() <-chan struct{} {
	c.once.Do(func() {
		close(c.doneObserved)
	})
	return c.Context.Done()
}

func startBlockedManagerLoad(t *testing.T) (*configkit.Manager[stepsTestConfig], chan configkit.Event, <-chan error, func()) {
	t.Helper()

	started := make(chan struct{})
	release := make(chan struct{})
	events := make(chan configkit.Event, 8)
	var blockOnce sync.Once
	observer := configkit.Observer(func(_ context.Context, event configkit.Event) {
		events <- event
		if event.Kind == configkit.EventKindLoadStarted {
			blockOnce.Do(func() {
				close(started)
				<-release
			})
		}
	})
	manager := configkit.NewManager[stepsTestConfig](configkit.WithObservers(observer))
	firstDone := make(chan error, 1)
	go func() {
		_, err := manager.Load(context.Background(), configkit.AttemptKindInitialLoad, configkit.SourceData{
			Data:     []byte(`{"name":"api","enabled":true,"port":8080}`),
			Revision: "v1",
		}, testPipeline())
		firstDone <- err
	}()

	var releaseOnce sync.Once
	releaseFirst := func() {
		releaseOnce.Do(func() {
			close(release)
		})
	}
	t.Cleanup(releaseFirst)
	receiveManagerTestValue(t, started)
	return manager, events, firstDone, releaseFirst
}

func assertUnchangedManager(t *testing.T, manager *configkit.Manager[stepsTestConfig]) {
	t.Helper()

	status := manager.LifecycleStatus()
	if status.State != configkit.LifecycleStateUnloaded || status.LastAttempt != nil || status.LastFailure != nil || status.LastApply != nil {
		t.Fatalf("manager status = %+v, want unchanged unloaded status", status)
	}
	if attempts := manager.Attempts(); len(attempts) != 0 {
		t.Fatalf("attempts = %+v, want none", attempts)
	}
	if snapshot, ok := manager.Snapshot(); ok {
		t.Fatalf("snapshot = %+v, ok = true; want none", snapshot)
	}
}

func receiveManagerTestValue[T any](t *testing.T, ch <-chan T) T {
	t.Helper()

	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		var zero T
		t.Fatal("timed out waiting for manager test operation")
		return zero
	}
}

func assertStringOmits(t *testing.T, label string, value string, secret string) {
	t.Helper()

	if strings.Contains(value, secret) {
		t.Fatalf("%s = %q, must not contain %q", label, value, secret)
	}
}
