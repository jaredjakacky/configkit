package configkit_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	configkit "github.com/jaredjakacky/configkit"
)

func TestManagerApplyRejectsSucceededResultWithoutSnapshot(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()
	_, err := manager.Apply(context.Background(), configkit.LoadResult[stepsTestConfig]{
		Attempt: configkit.AttemptRecord{Status: configkit.AttemptStatusSucceeded},
	})
	if !errors.Is(err, configkit.ErrInvalidLoadResult) {
		t.Fatalf("validate result error = %v, want configkit.ErrInvalidLoadResult", err)
	}
	if !strings.Contains(err.Error(), "succeeded attempt missing snapshot") {
		t.Fatalf("validate result error = %q, want missing snapshot detail", err.Error())
	}
}

func TestManagerApplyRejectsSucceededResultWithEmptySnapshotChecksum(t *testing.T) {
	var events []configkit.Event
	manager := configkit.NewManager[stepsTestConfig](configkit.WithObservers(func(_ context.Context, event configkit.Event) {
		events = append(events, event)
	}))
	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("seed manager: %v", err)
	}
	events = nil

	_, err := manager.Apply(context.Background(), succeededStatusTestResult("v2", ""))
	if !errors.Is(err, configkit.ErrInvalidLoadResult) {
		t.Fatalf("apply empty-checksum result error = %v, want configkit.ErrInvalidLoadResult", err)
	}
	if !strings.Contains(err.Error(), "succeeded snapshot missing checksum") {
		t.Fatalf("apply error = %q, want empty snapshot checksum detail", err.Error())
	}

	status := manager.LifecycleStatus()
	if status.State != configkit.LifecycleStateLoaded || status.Current == nil || status.Current.Revision != "v1" || status.Current.Checksum != "sum-1" {
		t.Fatalf("manager status = %+v, want unchanged loaded v1 snapshot", status)
	}
	if status.LastAttempt == nil || status.LastAttempt.ID != 1 || status.LastAttempt.Revision != "v1" {
		t.Fatalf("last attempt = %+v, want unchanged attempt 1 for v1", status.LastAttempt)
	}
	if attempts := manager.Attempts(); len(attempts) != 1 {
		t.Fatalf("attempt count = %d, want 1", len(attempts))
	}
	if len(events) != 0 {
		t.Fatalf("observer event count = %d, want 0", len(events))
	}
}

func TestManagerApplyRejectsContradictorySucceededResults(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*configkit.LoadResult[stepsTestConfig])
		detail string
	}{
		{
			name: "failure stage",
			mutate: func(result *configkit.LoadResult[stepsTestConfig]) {
				result.Attempt.Stage = configkit.AttemptStageDecode
			},
			detail: "succeeded attempt includes failure stage",
		},
		{
			name: "failure detail",
			mutate: func(result *configkit.LoadResult[stepsTestConfig]) {
				result.Attempt.Failure = testFailure("must remain private")
			},
			detail: "succeeded attempt includes failure detail",
		},
		{
			name: "missing attempt checksum",
			mutate: func(result *configkit.LoadResult[stepsTestConfig]) {
				result.Attempt.Checksum = ""
			},
			detail: "succeeded attempt missing checksum",
		},
		{
			name: "checksum mismatch",
			mutate: func(result *configkit.LoadResult[stepsTestConfig]) {
				result.Attempt.Checksum = "secret-attempt-checksum"
			},
			detail: "succeeded attempt checksum does not match snapshot",
		},
		{
			name: "source mismatch",
			mutate: func(result *configkit.LoadResult[stepsTestConfig]) {
				result.Attempt.Source = configkit.SourceMetadata{Name: "secret-source", Kind: "remote"}
			},
			detail: "succeeded attempt source does not match snapshot",
		},
		{
			name: "revision mismatch",
			mutate: func(result *configkit.LoadResult[stepsTestConfig]) {
				result.Attempt.Revision = "secret-revision"
			},
			detail: "succeeded attempt revision does not match snapshot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := succeededStatusTestResult("v1", "sum-1")
			tt.mutate(&result)
			assertInvalidLoadResultWithoutMutation(t, result, tt.detail)
		})
	}
}

func TestManagerApplyRejectsContradictoryFailedResults(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*configkit.LoadResult[stepsTestConfig])
		detail string
	}{
		{
			name: "missing stage",
			mutate: func(result *configkit.LoadResult[stepsTestConfig]) {
				result.Attempt.Stage = ""
			},
			detail: "failed attempt missing failure stage",
		},
		{
			name: "nil failure",
			mutate: func(result *configkit.LoadResult[stepsTestConfig]) {
				result.Attempt.Failure = nil
			},
			detail: "failed attempt missing public failure detail",
		},
		{
			name: "zero failure",
			mutate: func(result *configkit.LoadResult[stepsTestConfig]) {
				failure := *result.Attempt.Failure
				failure.Code = ""
				failure.Message = ""
				result.Attempt.Failure = &failure
			},
			detail: "failed attempt missing public failure detail",
		},
		{
			name: "checksum",
			mutate: func(result *configkit.LoadResult[stepsTestConfig]) {
				result.Attempt.Checksum = "secret-failed-checksum"
			},
			detail: "failed attempt includes checksum",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := failedStatusTestResult("safe failure")
			tt.mutate(&result)
			assertInvalidLoadResultWithoutMutation(t, result, tt.detail)
		})
	}
}

func TestManagerApplyDoesNotOvervalidateOptionalMetadata(t *testing.T) {
	t.Run("successful", func(t *testing.T) {
		metadata := configkit.SnapshotMetadata{Checksum: "opaque"}
		snapshot := configkit.NewSnapshot(stepsTestConfig{Name: "api"}, metadata, nil)
		result := configkit.LoadResult[stepsTestConfig]{
			Snapshot: &snapshot,
			Attempt: configkit.AttemptRecord{
				Kind:     configkit.AttemptKind("application_defined"),
				Status:   configkit.AttemptStatusSucceeded,
				Checksum: metadata.Checksum,
			},
		}

		manager := configkit.NewManager[stepsTestConfig]()
		if _, err := manager.Apply(context.Background(), result); err != nil {
			t.Fatalf("apply successful result with optional metadata omitted: %v", err)
		}
	})

	t.Run("failed", func(t *testing.T) {
		failure := testFailure("safe application failure")
		failure.Code = ""
		result := configkit.LoadResult[stepsTestConfig]{
			Attempt: configkit.AttemptRecord{
				Kind:    configkit.AttemptKind("application_defined"),
				Status:  configkit.AttemptStatusFailed,
				Stage:   configkit.AttemptStage("application_stage"),
				Failure: failure,
			},
		}

		manager := configkit.NewManager[stepsTestConfig]()
		if _, err := manager.Apply(context.Background(), result); err != nil {
			t.Fatalf("apply failed result with application-defined metadata: %v", err)
		}
	})
}

func TestManagerApplyRejectsFailedResultWithSnapshot(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()
	result := succeededStatusTestResult("v1", "sum-1")
	result.Attempt.Status = configkit.AttemptStatusFailed

	_, err := manager.Apply(context.Background(), result)
	if !errors.Is(err, configkit.ErrInvalidLoadResult) {
		t.Fatalf("validate result error = %v, want configkit.ErrInvalidLoadResult", err)
	}
	if !strings.Contains(err.Error(), "failed attempt includes snapshot") {
		t.Fatalf("validate result error = %q, want failed snapshot detail", err.Error())
	}
}

func TestManagerApplyRejectsResultWithUnknownStatus(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()
	_, err := manager.Apply(context.Background(), configkit.LoadResult[stepsTestConfig]{
		Attempt: configkit.AttemptRecord{Status: configkit.AttemptStatus("unknown")},
	})
	if !errors.Is(err, configkit.ErrInvalidLoadResult) {
		t.Fatalf("validate result error = %v, want configkit.ErrInvalidLoadResult", err)
	}
	if !strings.Contains(err.Error(), `unknown attempt status "unknown"`) {
		t.Fatalf("validate result error = %q, want unknown status detail", err.Error())
	}
}

func TestManagerApplyRejectsResultWithEmptyStatus(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()
	_, err := manager.Apply(context.Background(), configkit.LoadResult[stepsTestConfig]{})
	if !errors.Is(err, configkit.ErrInvalidLoadResult) {
		t.Fatalf("validate result error = %v, want configkit.ErrInvalidLoadResult", err)
	}
	if !strings.Contains(err.Error(), `unknown attempt status ""`) {
		t.Fatalf("validate result error = %q, want empty status detail", err.Error())
	}
}

func TestApplyResultJSONIncludesAppliedAt(t *testing.T) {
	appliedAt := time.Date(2026, 8, 10, 15, 4, 5, 123, time.UTC)
	data, err := json.Marshal(configkit.ApplyResult{AppliedAt: appliedAt})
	if err != nil {
		t.Fatalf("marshal apply result: %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("unmarshal apply result fields: %v", err)
	}
	if _, ok := fields["applied_at"]; !ok {
		t.Fatalf("apply result JSON = %s, want applied_at", data)
	}

	var got configkit.ApplyResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal apply result: %v", err)
	}
	if !got.AppliedAt.Equal(appliedAt) {
		t.Fatalf("applied at = %v, want %v", got.AppliedAt, appliedAt)
	}
}

func assertInvalidLoadResultWithoutMutation(t *testing.T, result configkit.LoadResult[stepsTestConfig], detail string) {
	t.Helper()

	var events []configkit.Event
	manager := configkit.NewManager[stepsTestConfig](configkit.WithObservers(func(_ context.Context, event configkit.Event) {
		events = append(events, event)
	}))

	_, err := manager.Apply(context.Background(), result)
	if !errors.Is(err, configkit.ErrInvalidLoadResult) {
		t.Fatalf("apply error = %v, want configkit.ErrInvalidLoadResult", err)
	}
	if !strings.Contains(err.Error(), detail) {
		t.Fatalf("apply error = %q, want detail %q", err.Error(), detail)
	}
	if strings.Contains(err.Error(), "secret-") || strings.Contains(err.Error(), "must remain private") {
		t.Fatalf("apply error exposed contradictory result data: %q", err.Error())
	}

	status := manager.LifecycleStatus()
	if status.State != configkit.LifecycleStateUnloaded || status.LastAttempt != nil || status.LastApply != nil {
		t.Fatalf("manager status = %+v, want unchanged unloaded status", status)
	}
	if attempts := manager.Attempts(); len(attempts) != 0 {
		t.Fatalf("attempt count = %d, want 0", len(attempts))
	}
	if len(events) != 0 {
		t.Fatalf("observer event count = %d, want 0", len(events))
	}

	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("valid", "sum-valid")); err != nil {
		t.Fatalf("apply valid result after rejection: %v", err)
	}
	status = manager.LifecycleStatus()
	if status.LastAttempt == nil || status.LastAttempt.ID != 1 {
		t.Fatalf("last attempt = %+v, want first manager-assigned id 1", status.LastAttempt)
	}
}
