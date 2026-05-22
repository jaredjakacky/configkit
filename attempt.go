package configkit

import "time"

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
// Successful attempts usually leave Stage empty because the full lifecycle
// completed.
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
// Failed attempts do not produce a new Snapshot. A manager can keep the last
// known good Snapshot active while recording the failed attempt separately. ID
// is assigned by a Manager when the attempt is applied; package-level Load and
// LoadFromSource may leave it zero.
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

	Error string `json:"error,omitempty"`
}
