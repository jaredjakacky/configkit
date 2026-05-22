package configkit_test

import (
	"context"
	"testing"
	"time"

	configkit "github.com/jaredjakacky/configkit"
)

func TestStatusZeroManagerIsUnloaded(t *testing.T) {
	var manager configkit.Manager[stepsTestConfig]

	status := manager.Status()
	if status.State != configkit.StatusStateUnloaded {
		t.Fatalf("status state = %q, want %q", status.State, configkit.StatusStateUnloaded)
	}
	if status.Current != nil {
		t.Fatalf("current = %+v, want nil", status.Current)
	}
	if status.LastAttempt != nil {
		t.Fatalf("last attempt = %+v, want nil", status.LastAttempt)
	}
	if status.LastSuccess != nil {
		t.Fatalf("last success = %+v, want nil", status.LastSuccess)
	}
	if status.LastFailure != nil {
		t.Fatalf("last failure = %+v, want nil", status.LastFailure)
	}
	if status.LastApply != nil {
		t.Fatalf("last apply = %+v, want nil", status.LastApply)
	}
}

func TestStatusFailedAfterFailedApplyWithoutCurrentSnapshot(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()

	if _, err := manager.Apply(context.Background(), failedStatusTestResult("decode failed")); err != nil {
		t.Fatalf("apply failed result: %v", err)
	}

	status := manager.Status()
	if status.State != configkit.StatusStateFailed {
		t.Fatalf("status state = %q, want %q", status.State, configkit.StatusStateFailed)
	}
	if status.Current != nil {
		t.Fatalf("current = %+v, want nil", status.Current)
	}
	if status.LastAttempt == nil || status.LastAttempt.Status != configkit.AttemptStatusFailed {
		t.Fatalf("last attempt = %+v, want failed attempt", status.LastAttempt)
	}
	if status.LastFailure == nil || status.LastFailure.Error != "decode failed" {
		t.Fatalf("last failure = %+v, want recorded failure", status.LastFailure)
	}
	if status.LastSuccess != nil {
		t.Fatalf("last success = %+v, want nil", status.LastSuccess)
	}
}

func TestStatusLoadedAfterSuccessfulApply(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()

	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("apply succeeded result: %v", err)
	}

	status := manager.Status()
	if status.State != configkit.StatusStateLoaded {
		t.Fatalf("status state = %q, want %q", status.State, configkit.StatusStateLoaded)
	}
	if status.Current == nil {
		t.Fatal("current = nil, want snapshot metadata")
	}
	if status.Current.Revision != "v1" {
		t.Fatalf("current revision = %q, want %q", status.Current.Revision, "v1")
	}
	if status.Current.Checksum != "sum-1" {
		t.Fatalf("current checksum = %q, want %q", status.Current.Checksum, "sum-1")
	}
	if status.LastAttempt == nil || status.LastAttempt.Status != configkit.AttemptStatusSucceeded {
		t.Fatalf("last attempt = %+v, want succeeded attempt", status.LastAttempt)
	}
	if status.LastSuccess == nil || status.LastSuccess.Checksum != "sum-1" {
		t.Fatalf("last success = %+v, want successful attempt", status.LastSuccess)
	}
	if status.LastFailure != nil {
		t.Fatalf("last failure = %+v, want nil", status.LastFailure)
	}
	if status.LastApply == nil || !status.LastApply.Published || !status.LastApply.Changed {
		t.Fatalf("last apply = %+v, want published changed apply", status.LastApply)
	}
}

func TestStatusDegradedAfterFailedApplyWithCurrentSnapshot(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()

	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("apply succeeded result: %v", err)
	}
	if _, err := manager.Apply(context.Background(), failedStatusTestResult("reload failed")); err != nil {
		t.Fatalf("apply failed result: %v", err)
	}

	status := manager.Status()
	if status.State != configkit.StatusStateDegraded {
		t.Fatalf("status state = %q, want %q", status.State, configkit.StatusStateDegraded)
	}
	if status.Current == nil || status.Current.Checksum != "sum-1" {
		t.Fatalf("current = %+v, want last known good snapshot metadata", status.Current)
	}
	if status.LastAttempt == nil || status.LastAttempt.Status != configkit.AttemptStatusFailed {
		t.Fatalf("last attempt = %+v, want failed attempt", status.LastAttempt)
	}
	if status.LastFailure == nil || status.LastFailure.Error != "reload failed" {
		t.Fatalf("last failure = %+v, want recorded failure", status.LastFailure)
	}
	if status.LastSuccess == nil || status.LastSuccess.Checksum != "sum-1" {
		t.Fatalf("last success = %+v, want previous successful attempt", status.LastSuccess)
	}
	if status.LastApply == nil || status.LastApply.Published {
		t.Fatalf("last apply = %+v, want non-publishing failed apply", status.LastApply)
	}
}

func TestStatusReturnsCopies(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()

	if _, err := manager.Apply(context.Background(), succeededStatusTestResult("v1", "sum-1")); err != nil {
		t.Fatalf("apply succeeded result: %v", err)
	}

	status := manager.Status()
	status.Current.Checksum = "mutated-current"
	status.LastAttempt.Checksum = "mutated-attempt"
	status.LastSuccess.Checksum = "mutated-success"
	status.LastApply.Current.Checksum = "mutated-apply"

	next := manager.Status()
	if next.Current.Checksum != "sum-1" {
		t.Fatalf("current checksum after external mutation = %q, want %q", next.Current.Checksum, "sum-1")
	}
	if next.LastAttempt.Checksum != "sum-1" {
		t.Fatalf("last attempt checksum after external mutation = %q, want %q", next.LastAttempt.Checksum, "sum-1")
	}
	if next.LastSuccess.Checksum != "sum-1" {
		t.Fatalf("last success checksum after external mutation = %q, want %q", next.LastSuccess.Checksum, "sum-1")
	}
	if next.LastApply.Current.Checksum != "sum-1" {
		t.Fatalf("last apply current checksum after external mutation = %q, want %q", next.LastApply.Current.Checksum, "sum-1")
	}
}

func succeededStatusTestResult(revision, checksum string) configkit.LoadResult[stepsTestConfig] {
	loadedAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	metadata := configkit.SnapshotMetadata{
		Source:   configkit.SourceMetadata{Name: "test", Kind: "memory"},
		Revision: revision,
		Checksum: checksum,
		LoadedAt: loadedAt,
	}
	snapshot := configkit.NewSnapshot(stepsTestConfig{Name: "api", Enabled: true, Port: 8080}, metadata, configkit.RedactedView{"name": "api"})

	return configkit.LoadResult[stepsTestConfig]{
		Snapshot: &snapshot,
		Attempt: configkit.AttemptRecord{
			Kind:      configkit.AttemptKindInitialLoad,
			Status:    configkit.AttemptStatusSucceeded,
			Source:    metadata.Source,
			Revision:  revision,
			Checksum:  checksum,
			StartedAt: loadedAt.Add(-time.Second),
			EndedAt:   loadedAt,
		},
	}
}

func failedStatusTestResult(message string) configkit.LoadResult[stepsTestConfig] {
	startedAt := time.Date(2026, 5, 21, 12, 1, 0, 0, time.UTC)

	return configkit.LoadResult[stepsTestConfig]{
		Attempt: configkit.AttemptRecord{
			Kind:      configkit.AttemptKindReload,
			Status:    configkit.AttemptStatusFailed,
			Stage:     configkit.AttemptStageDecode,
			Source:    configkit.SourceMetadata{Name: "test", Kind: "memory"},
			Revision:  "v2",
			StartedAt: startedAt,
			EndedAt:   startedAt.Add(time.Second),
			Error:     message,
		},
	}
}
