package configkit_test

import (
	"context"
	"errors"
	"testing"

	configkit "github.com/jaredjakacky/configkit"
)

func TestPipelineValidateAcceptsRequiredSteps(t *testing.T) {
	pipeline := configkit.Pipeline[stepsTestConfig]{
		Decode:   configkit.JSONDecoder[stepsTestConfig](),
		Redact:   configkit.EmptyRedactor[stepsTestConfig](),
		Checksum: configkit.SHA256JSONChecksum[stepsTestConfig](),
	}

	if err := pipeline.Validate(); err != nil {
		t.Fatalf("validate pipeline: %v", err)
	}
}

func TestPipelineValidateAllowsOptionalSteps(t *testing.T) {
	pipeline := configkit.Pipeline[stepsTestConfig]{
		Decode: configkit.JSONDecoder[stepsTestConfig](),
		ApplyDefaults: func(ctx context.Context, value stepsTestConfig) (stepsTestConfig, error) {
			return value, nil
		},
		ValidateConfig: func(ctx context.Context, value stepsTestConfig) error {
			return nil
		},
		Copy: func(ctx context.Context, value stepsTestConfig) (stepsTestConfig, error) {
			return value, nil
		},
		Redact:   configkit.EmptyRedactor[stepsTestConfig](),
		Checksum: configkit.SHA256JSONChecksum[stepsTestConfig](),
	}

	if err := pipeline.Validate(); err != nil {
		t.Fatalf("validate pipeline with optional steps: %v", err)
	}
}

func TestPipelineValidateRequiresDecoder(t *testing.T) {
	pipeline := configkit.Pipeline[stepsTestConfig]{
		Redact:   configkit.EmptyRedactor[stepsTestConfig](),
		Checksum: configkit.SHA256JSONChecksum[stepsTestConfig](),
	}

	if err := pipeline.Validate(); !errors.Is(err, configkit.ErrMissingDecoder) {
		t.Fatalf("validate missing decoder error = %v, want %v", err, configkit.ErrMissingDecoder)
	}
}

func TestPipelineValidateRequiresRedactor(t *testing.T) {
	pipeline := configkit.Pipeline[stepsTestConfig]{
		Decode:   configkit.JSONDecoder[stepsTestConfig](),
		Checksum: configkit.SHA256JSONChecksum[stepsTestConfig](),
	}

	if err := pipeline.Validate(); !errors.Is(err, configkit.ErrMissingRedactor) {
		t.Fatalf("validate missing redactor error = %v, want %v", err, configkit.ErrMissingRedactor)
	}
}

func TestPipelineValidateRequiresChecksum(t *testing.T) {
	pipeline := configkit.Pipeline[stepsTestConfig]{
		Decode: configkit.JSONDecoder[stepsTestConfig](),
		Redact: configkit.EmptyRedactor[stepsTestConfig](),
	}

	if err := pipeline.Validate(); !errors.Is(err, configkit.ErrMissingChecksum) {
		t.Fatalf("validate missing checksum error = %v, want %v", err, configkit.ErrMissingChecksum)
	}
}

func TestPipelineValidateReportsFirstMissingRequiredStep(t *testing.T) {
	var pipeline configkit.Pipeline[stepsTestConfig]

	if err := pipeline.Validate(); !errors.Is(err, configkit.ErrMissingDecoder) {
		t.Fatalf("validate empty pipeline error = %v, want %v", err, configkit.ErrMissingDecoder)
	}
}
