package configkit

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrLifecyclePanicked is returned when Configkit recovers a panic from a
// source or pipeline lifecycle step.
//
// The recovered panic value is intentionally not exposed through Error or
// Unwrap because it may contain secrets, raw config, or arbitrary application
// data.
var ErrLifecyclePanicked = errors.New("configkit: lifecycle panicked")

var errEmptyChecksum = errors.New("checksummer returned an empty checksum")

// LoadResult describes the result of one configuration load attempt.
//
// A successful load produces a Snapshot and a successful AttemptRecord with
// matching source, revision, and non-empty checksum metadata. It has no failure
// stage or detail. A failed load produces no Snapshot or checksum and a failed
// AttemptRecord with a stage and safe public failure detail.
type LoadResult[T any] struct {
	Snapshot *Snapshot[T]
	Attempt  AttemptRecord
}

// ManagedLoadResult describes a manager-owned load attempt.
//
// Load is the stateless lifecycle result. Apply describes how that result
// affected the manager's published state.
type ManagedLoadResult[T any] struct {
	Load  LoadResult[T]
	Apply ApplyResult
}

// LoadFromSource reads configuration data from source and runs one stateless
// load lifecycle.
//
// If the source read fails, LoadFromSource returns a failed LoadResult with no
// snapshot. If the source read succeeds, it runs the load lifecycle. It returns
// a LoadResult but does not store or publish the produced snapshot.
// Nil and typed-nil Source values return ErrMissingSource.
//
// Callers must pass a non-nil context. Passing nil is invalid and may panic.
func LoadFromSource[T any](ctx context.Context, kind AttemptKind, source Source, pipeline Pipeline[T]) (LoadResult[T], error) {
	startedAt := time.Now().UTC()
	var sourceMetadata SourceMetadata
	var metadataErr error
	if !isNilSource(source) {
		sourceMetadata, metadataErr = loadSourceMetadata(source)
	}

	return loadFromSourceWithMetadata(ctx, kind, source, pipeline, startedAt, sourceMetadata, metadataErr)
}

func loadFromSourceWithMetadata[T any](ctx context.Context, kind AttemptKind, source Source, pipeline Pipeline[T], startedAt time.Time, sourceMetadata SourceMetadata, metadataErr error) (LoadResult[T], error) {
	if isNilSource(source) {
		return LoadResult[T]{
			Attempt: AttemptRecord{
				Kind:      kind,
				Status:    AttemptStatusFailed,
				Stage:     AttemptStageSourceRead,
				StartedAt: startedAt,
				EndedAt:   time.Now().UTC(),
				Failure:   newFailure(FailureCodeMissingSource, "config source is missing"),
			},
		}, ErrMissingSource
	}

	if metadataErr != nil {
		return LoadResult[T]{
			Attempt: AttemptRecord{
				Kind:      kind,
				Status:    AttemptStatusFailed,
				Stage:     AttemptStageSourceRead,
				StartedAt: startedAt,
				EndedAt:   time.Now().UTC(),
				Failure:   attemptFailure(AttemptStageSourceRead, metadataErr),
			},
		}, metadataErr
	}

	data, err := readSourceData(ctx, source)
	if err != nil {
		readErr := sourceReadError(err)

		return LoadResult[T]{
			Attempt: AttemptRecord{
				Kind:      kind,
				Status:    AttemptStatusFailed,
				Stage:     AttemptStageSourceRead,
				Source:    sourceMetadata,
				StartedAt: startedAt,
				EndedAt:   time.Now().UTC(),
				Failure:   attemptFailure(AttemptStageSourceRead, readErr),
			},
		}, readErr
	}

	if data.Metadata == (SourceMetadata{}) {
		data.Metadata = sourceMetadata
	}

	return load(ctx, kind, data, pipeline, startedAt)
}

func loadSourceMetadata(source Source) (metadata SourceMetadata, err error) {
	defer recoverSourcePanic("read config source metadata", &err)

	return source.Metadata(), nil
}

func readSourceData(ctx context.Context, source Source) (data SourceData, err error) {
	defer recoverSourcePanic("read config source", &err)

	return source.Read(ctx)
}

func recoverSourcePanic(prefix string, err *error) {
	if recovered := recover(); recovered != nil {
		*err = newLifecyclePanicError(prefix, recovered)
	}
}

func sourceReadError(err error) error {
	var panicErr *lifecyclePanicError
	if errors.As(err, &panicErr) {
		return err
	}

	return fmt.Errorf("read config source: %w", err)
}

// Load runs one stateless load lifecycle against already-read source data.
//
// Load does not read from a source. It returns a LoadResult but does not store
// or publish the produced snapshot.
//
// Callers must pass a non-nil context. Passing nil is invalid and may panic.
func Load[T any](ctx context.Context, kind AttemptKind, data SourceData, pipeline Pipeline[T]) (LoadResult[T], error) {
	return load(ctx, kind, data, pipeline, time.Now().UTC())
}

func load[T any](ctx context.Context, kind AttemptKind, data SourceData, pipeline Pipeline[T], startedAt time.Time) (LoadResult[T], error) {
	fail := func(stage AttemptStage, err error) (LoadResult[T], error) {
		return LoadResult[T]{
			Attempt: AttemptRecord{
				Kind:      kind,
				Status:    AttemptStatusFailed,
				Stage:     stage,
				Source:    data.Metadata,
				Revision:  data.Revision,
				StartedAt: startedAt,
				EndedAt:   time.Now().UTC(),
				Failure:   attemptFailure(stage, err),
			},
		}, err
	}

	checkContext := func() (LoadResult[T], error, bool) {
		if err := ctx.Err(); err != nil {
			result, err := fail(AttemptStageContext, err)
			return result, err, true
		}

		var zero LoadResult[T]
		return zero, nil, false
	}

	if result, err, stopped := checkContext(); stopped {
		return result, err
	}

	if err := pipeline.Validate(); err != nil {
		return fail(AttemptStagePipelineValidate, err)
	}
	if result, err, stopped := checkContext(); stopped {
		return result, err
	}

	value, err := decodeConfig(ctx, pipeline.Decode, data)
	if err != nil {
		return fail(AttemptStageDecode, pipelineStageError("decode config", err))
	}
	if result, err, stopped := checkContext(); stopped {
		return result, err
	}

	if pipeline.ApplyDefaults != nil {
		value, err = applyConfigDefaults(ctx, pipeline.ApplyDefaults, value)
		if err != nil {
			return fail(AttemptStageDefaults, pipelineStageError("apply config defaults", err))
		}
		if result, err, stopped := checkContext(); stopped {
			return result, err
		}
	}

	if pipeline.ValidateConfig != nil {
		if err := validateConfig(ctx, pipeline.ValidateConfig, value); err != nil {
			return fail(AttemptStageValidateConfig, pipelineStageError("validate config", err))
		}
		if result, err, stopped := checkContext(); stopped {
			return result, err
		}
	}

	snapshotValue := value
	if pipeline.Copy != nil {
		snapshotValue, err = copyConfig(ctx, pipeline.Copy, value)
		if err != nil {
			return fail(AttemptStageCopy, pipelineStageError("copy config", err))
		}
		if result, err, stopped := checkContext(); stopped {
			return result, err
		}
	}

	redacted, err := redactConfig(ctx, pipeline.Redact, snapshotValue)
	if err != nil {
		return fail(AttemptStageRedact, pipelineStageError("redact config", err))
	}
	if result, err, stopped := checkContext(); stopped {
		return result, err
	}

	checksum, err := checksumConfig(ctx, pipeline.Checksum, snapshotValue)
	if err != nil {
		return fail(AttemptStageChecksum, pipelineStageError("checksum config", err))
	}
	if result, err, stopped := checkContext(); stopped {
		return result, err
	}

	if result, err, stopped := checkContext(); stopped {
		return result, err
	}

	endedAt := time.Now().UTC()

	snapshot := NewSnapshot(snapshotValue, SnapshotMetadata{
		Source:   data.Metadata,
		Revision: data.Revision,
		Checksum: checksum,
		LoadedAt: endedAt,
	}, redacted)

	return LoadResult[T]{
		Snapshot: &snapshot,
		Attempt: AttemptRecord{
			Kind:      kind,
			Status:    AttemptStatusSucceeded,
			Source:    data.Metadata,
			Revision:  data.Revision,
			Checksum:  checksum,
			StartedAt: startedAt,
			EndedAt:   endedAt,
		},
	}, nil
}

func decodeConfig[T any](ctx context.Context, decode Decoder[T], data SourceData) (value T, err error) {
	defer recoverPipelinePanic("decode config", &err)

	return decode(ctx, data)
}

func applyConfigDefaults[T any](ctx context.Context, applyDefaults DefaultApplier[T], value T) (out T, err error) {
	defer recoverPipelinePanic("apply config defaults", &err)

	return applyDefaults(ctx, value)
}

func validateConfig[T any](ctx context.Context, validate Validator[T], value T) (err error) {
	defer recoverPipelinePanic("validate config", &err)

	return validate(ctx, value)
}

func redactConfig[T any](ctx context.Context, redact Redactor[T], value T) (redacted RedactedView, err error) {
	defer recoverPipelinePanic("redact config", &err)

	return redact(ctx, value)
}

func checksumConfig[T any](ctx context.Context, checksum Checksummer[T], value T) (sum string, err error) {
	defer recoverPipelinePanic("checksum config", &err)

	sum, err = checksum(ctx, value)
	if err != nil {
		return "", err
	}
	if sum == "" {
		return "", errEmptyChecksum
	}

	return sum, nil
}

func copyConfig[T any](ctx context.Context, copy Copier[T], value T) (out T, err error) {
	defer recoverPipelinePanic("copy config", &err)

	return copy(ctx, value)
}

func recoverPipelinePanic(prefix string, err *error) {
	if recovered := recover(); recovered != nil {
		*err = newLifecyclePanicError(prefix, recovered)
	}
}

func pipelineStageError(prefix string, err error) error {
	var panicErr *lifecyclePanicError
	if errors.As(err, &panicErr) {
		return err
	}

	return fmt.Errorf("%s: %w", prefix, err)
}

type lifecyclePanicError struct {
	message string
}

func newLifecyclePanicError(prefix string, _ any) error {
	return &lifecyclePanicError{
		message: prefix + " panicked",
	}
}

func (e *lifecyclePanicError) Error() string {
	return e.message
}

func (e *lifecyclePanicError) Unwrap() error {
	return ErrLifecyclePanicked
}
