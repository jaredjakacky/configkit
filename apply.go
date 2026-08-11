package configkit

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidLoadResult is returned when a LoadResult cannot be safely applied.
var ErrInvalidLoadResult = errors.New("configkit: invalid load result")

// ApplyResult describes how a LoadResult affected manager state.
type ApplyResult struct {
	// Published is true when a successful snapshot became current.
	Published bool `json:"published"`

	// Changed is true when the current effective configuration checksum differs
	// from the previous snapshot checksum.
	Changed bool `json:"changed"`

	// Previous describes the snapshot that was current before apply, if any.
	Previous *SnapshotMetadata `json:"previous,omitempty"`

	// Current describes the snapshot that is current after apply, if any.
	Current *SnapshotMetadata `json:"current,omitempty"`

	// AppliedAt is when the manager accepted this result and committed its
	// lifecycle-state mutation. It is set for both successful and failed
	// accepted results.
	AppliedAt time.Time `json:"applied_at"`
}

func cloneApplyResult(in ApplyResult) ApplyResult {
	out := in
	out.Previous = cloneSnapshotMetadataPtr(in.Previous)
	out.Current = cloneSnapshotMetadataPtr(in.Current)
	return out
}

func cloneApplyResultPtr(in *ApplyResult) *ApplyResult {
	if in == nil {
		return nil
	}

	out := cloneApplyResult(*in)
	return &out
}

func cloneSnapshotMetadataPtr(in *SnapshotMetadata) *SnapshotMetadata {
	if in == nil {
		return nil
	}

	out := *in
	return &out
}

func validateLoadResult[T any](result LoadResult[T]) error {
	switch result.Attempt.Status {
	case AttemptStatusSucceeded:
		if result.Snapshot == nil {
			return fmt.Errorf("%w: succeeded attempt missing snapshot", ErrInvalidLoadResult)
		}
		if result.Attempt.Stage != "" {
			return fmt.Errorf("%w: succeeded attempt includes failure stage", ErrInvalidLoadResult)
		}
		if result.Attempt.Failure != nil {
			return fmt.Errorf("%w: succeeded attempt includes failure detail", ErrInvalidLoadResult)
		}

		metadata := result.Snapshot.Metadata()
		if metadata.Checksum == "" {
			return fmt.Errorf("%w: succeeded snapshot missing checksum", ErrInvalidLoadResult)
		}
		if result.Attempt.Checksum == "" {
			return fmt.Errorf("%w: succeeded attempt missing checksum", ErrInvalidLoadResult)
		}
		if result.Attempt.Checksum != metadata.Checksum {
			return fmt.Errorf("%w: succeeded attempt checksum does not match snapshot", ErrInvalidLoadResult)
		}
		if result.Attempt.Source != metadata.Source {
			return fmt.Errorf("%w: succeeded attempt source does not match snapshot", ErrInvalidLoadResult)
		}
		if result.Attempt.Revision != metadata.Revision {
			return fmt.Errorf("%w: succeeded attempt revision does not match snapshot", ErrInvalidLoadResult)
		}
	case AttemptStatusFailed:
		if result.Snapshot != nil {
			return fmt.Errorf("%w: failed attempt includes snapshot", ErrInvalidLoadResult)
		}
		if result.Attempt.Stage == "" {
			return fmt.Errorf("%w: failed attempt missing failure stage", ErrInvalidLoadResult)
		}
		if result.Attempt.Failure == nil || (result.Attempt.Failure.Code == "" && result.Attempt.Failure.Message == "") {
			return fmt.Errorf("%w: failed attempt missing public failure detail", ErrInvalidLoadResult)
		}
		if result.Attempt.Checksum != "" {
			return fmt.Errorf("%w: failed attempt includes checksum", ErrInvalidLoadResult)
		}
	default:
		return fmt.Errorf("%w: unknown attempt status %q", ErrInvalidLoadResult, result.Attempt.Status)
	}

	return nil
}
