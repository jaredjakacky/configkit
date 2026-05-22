package configkit_test

import (
	"context"
	"errors"
	"strings"
	"testing"

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
