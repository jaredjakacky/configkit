package configkit_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"

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
}

func TestManagerApplyRejectsInvalidLoadResultWithoutMutation(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()

	_, err := manager.Apply(context.Background(), configkit.LoadResult[stepsTestConfig]{
		Attempt: configkit.AttemptRecord{Status: configkit.AttemptStatusSucceeded},
	})
	if !errors.Is(err, configkit.ErrInvalidLoadResult) {
		t.Fatalf("apply invalid result error = %v, want configkit.ErrInvalidLoadResult", err)
	}

	status := manager.Status()
	if status.State != configkit.StatusStateUnloaded {
		t.Fatalf("state = %q, want %q", status.State, configkit.StatusStateUnloaded)
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

	status := manager.Status()
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
	status := manager.Status()
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

func TestManagerInspectReturnsStatusAndRedactedCopy(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()

	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("apply succeeded result: %v", err)
	}

	inspection := manager.Inspect()
	if inspection.Status.State != configkit.StatusStateLoaded {
		t.Fatalf("inspection state = %q, want %q", inspection.Status.State, configkit.StatusStateLoaded)
	}
	if got := inspection.Redacted["name"]; got != "api" {
		t.Fatalf("redacted name = %v, want api", got)
	}
	inspection.Redacted["name"] = "mutated"

	next := manager.Inspect()
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
				status := manager.Status()
				if !validManagerState(status.State) {
					errs <- errors.New("invalid manager state: " + string(status.State))
					return
				}

				inspection := manager.Inspect()
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
	if status := manager.Status(); status.State != configkit.StatusStateLoaded {
		t.Fatalf("manager state = %q, want %q", status.State, configkit.StatusStateLoaded)
	}
}

func TestManagerLoadFromSourceMissingSourceRecordsFailure(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()

	result, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindReload, nil, testPipeline())
	if !errors.Is(err, configkit.ErrMissingSource) {
		t.Fatalf("load from missing source error = %v, want configkit.ErrMissingSource", err)
	}
	if result.Load.Attempt.ID == 0 {
		t.Fatal("load attempt id = 0, want manager-assigned id")
	}
	if result.Load.Attempt.Status != configkit.AttemptStatusFailed {
		t.Fatalf("attempt status = %q, want %q", result.Load.Attempt.Status, configkit.AttemptStatusFailed)
	}
	if result.Apply.Published {
		t.Fatal("published = true, want false")
	}
	if status := manager.Status(); status.State != configkit.StatusStateFailed {
		t.Fatalf("manager state = %q, want %q", status.State, configkit.StatusStateFailed)
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

func testPipeline() configkit.Pipeline[stepsTestConfig] {
	return configkit.Pipeline[stepsTestConfig]{
		Decode:   configkit.JSONDecoder[stepsTestConfig](),
		Redact:   configkit.EmptyRedactor[stepsTestConfig](),
		Checksum: configkit.SHA256JSONChecksum[stepsTestConfig](),
	}
}

func validManagerState(state configkit.StatusState) bool {
	switch state {
	case configkit.StatusStateUnloaded,
		configkit.StatusStateLoaded,
		configkit.StatusStateFailed,
		configkit.StatusStateDegraded:
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
