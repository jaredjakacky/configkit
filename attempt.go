package configkit

import (
	"context"
	"errors"
	"time"

	opskit "github.com/jaredjakacky/opskit"
)

// Stable public operational failure codes emitted by Configkit.
const (
	FailureCodeCanceled               = "canceled"
	FailureCodeDeadlineExceeded       = "deadline_exceeded"
	FailureCodeMissingSource          = "missing_source"
	FailureCodeContextFailed          = "context_failed"
	FailureCodeSourceReadFailed       = "source_read_failed"
	FailureCodePipelineValidateFailed = "pipeline_validate_failed"
	FailureCodeDecodeFailed           = "decode_failed"
	FailureCodeDefaultsFailed         = "defaults_failed"
	FailureCodeValidateConfigFailed   = "validate_config_failed"
	FailureCodeCopyFailed             = "copy_failed"
	FailureCodeRedactFailed           = "redact_failed"
	FailureCodeChecksumFailed         = "checksum_failed"
	FailureCodeLoadFailed             = "load_failed"
	FailureCodeReloadFailed           = "reload_failed"
)

func attemptFailure(stage AttemptStage, err error) *opskit.Failure {
	if errors.Is(err, context.Canceled) {
		return newFailure(FailureCodeCanceled, "config load canceled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return newFailure(FailureCodeDeadlineExceeded, "config load deadline exceeded")
	}

	code := FailureCodeLoadFailed
	message := "config load failed"
	switch stage {
	case AttemptStageContext:
		code = FailureCodeContextFailed
		message = "config load context failed"
	case AttemptStageSourceRead:
		code = FailureCodeSourceReadFailed
		message = "config source read failed"
	case AttemptStagePipelineValidate:
		code = FailureCodePipelineValidateFailed
		message = "config pipeline validation failed"
	case AttemptStageDecode:
		code = FailureCodeDecodeFailed
		message = "config decode failed"
	case AttemptStageDefaults:
		code = FailureCodeDefaultsFailed
		message = "config default application failed"
	case AttemptStageValidateConfig:
		code = FailureCodeValidateConfigFailed
		message = "config validation failed"
	case AttemptStageCopy:
		code = FailureCodeCopyFailed
		message = "config copy failed"
	case AttemptStageRedact:
		code = FailureCodeRedactFailed
		message = "config redaction failed"
	case AttemptStageChecksum:
		code = FailureCodeChecksumFailed
		message = "config checksum failed"
	}
	return newFailure(code, message)
}

func newFailure(code, message string) *opskit.Failure {
	return &opskit.Failure{Code: code, Message: message}
}

func cloneFailure(failure *opskit.Failure) *opskit.Failure {
	if failure == nil {
		return nil
	}
	copy := *failure
	return &copy
}

func cloneAttemptRecord(in AttemptRecord) AttemptRecord {
	out := in
	out.Failure = cloneFailure(in.Failure)
	return out
}

// AttemptKind describes why configuration loading was attempted.
type AttemptKind string

const (
	AttemptKindInitialLoad AttemptKind = "initial_load"
	AttemptKindReload      AttemptKind = "reload"
)

// AttemptStatus describes the outcome of a configuration load attempt.
type AttemptStatus string

const (
	AttemptStatusSucceeded AttemptStatus = "succeeded"
	AttemptStatusFailed    AttemptStatus = "failed"
)

// AttemptStage describes the lifecycle stage associated with a failed attempt.
//
// Successful attempts leave Stage empty because the full lifecycle completed.
type AttemptStage string

const (
	AttemptStageContext          AttemptStage = "context"
	AttemptStageSourceRead       AttemptStage = "source_read"
	AttemptStagePipelineValidate AttemptStage = "pipeline_validate"
	AttemptStageDecode           AttemptStage = "decode"
	AttemptStageDefaults         AttemptStage = "defaults"
	AttemptStageValidateConfig   AttemptStage = "validate_config"
	AttemptStageCopy             AttemptStage = "copy"
	AttemptStageRedact           AttemptStage = "redact"
	AttemptStageChecksum         AttemptStage = "checksum"
)

// AttemptRecord describes one configuration load or reload attempt.
//
// Successful attempts have no Stage or Failure and carry the non-empty checksum
// of their Snapshot. Failed attempts have a Stage and safe public Failure, do
// not carry a checksum, and do not produce a new Snapshot. A manager can keep
// the last known good Snapshot active while recording the failed attempt
// separately. ID is assigned by a Manager when the attempt is applied;
// package-level Load and LoadFromSource may leave it zero.
type AttemptRecord struct {
	ID     uint64        `json:"id,omitempty"`
	Kind   AttemptKind   `json:"kind,omitempty"`
	Status AttemptStatus `json:"status"`
	Stage  AttemptStage  `json:"stage,omitempty"`

	Source   SourceMetadata `json:"source"`
	Revision string         `json:"revision,omitempty"`
	Checksum string         `json:"checksum,omitempty"`

	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`

	// Failure is explicit safe public detail for a failed attempt. The original
	// error remains only on the load call's return path.
	Failure *opskit.Failure `json:"failure,omitempty"`
}
