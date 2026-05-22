package configkit

import (
	"errors"
	"fmt"
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
	case AttemptStatusFailed:
		if result.Snapshot != nil {
			return fmt.Errorf("%w: failed attempt includes snapshot", ErrInvalidLoadResult)
		}
	default:
		return fmt.Errorf("%w: unknown attempt status %q", ErrInvalidLoadResult, result.Attempt.Status)
	}

	return nil
}
