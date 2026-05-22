package configkit

import (
	"context"
	"sync"
	"time"
)

const defaultAttemptHistoryLimit = 20

// Manager owns the currently published configuration snapshot and lifecycle status.
//
// A Manager does not decide what configuration means. It stores the last known
// good snapshot, records load/reload attempts, and provides safe access to
// current configuration state. Manager-owned load attempts are serialized while
// status and snapshot reads remain concurrent.
type Manager[T any] struct {
	attemptMu sync.Mutex
	mu        sync.RWMutex

	nextAttemptID uint64

	current     *Snapshot[T]
	lastAttempt *AttemptRecord
	lastSuccess *AttemptRecord
	lastFailure *AttemptRecord
	lastApply   *ApplyResult

	attemptHistory      []AttemptRecord
	attemptHistoryLimit int

	observers []Observer
}

// ManagerOption configures a Manager.
type ManagerOption func(*managerOptions)

type managerOptions struct {
	observers              []Observer
	attemptHistoryLimit    int
	attemptHistoryLimitSet bool
}

// WithObservers registers configuration lifecycle observers for the manager.
//
// Observers run synchronously by default. They should return quickly and must
// not call Load, LoadFromSource, or Apply on the same manager. Read-only calls
// such as Status, Inspect, Snapshot, and Value are acceptable. Use AsyncObserver
// or another goroutine for follow-up work that may block or trigger lifecycle
// operations.
func WithObservers(observers ...Observer) ManagerOption {
	return func(options *managerOptions) {
		options.observers = append(options.observers, observers...)
	}
}

// WithAttemptHistoryLimit configures how many recent attempts a Manager keeps.
//
// The default is 20. A limit less than or equal to zero disables attempt
// history while preserving LastAttempt, LastSuccess, and LastFailure status.
func WithAttemptHistoryLimit(limit int) ManagerOption {
	return func(options *managerOptions) {
		options.attemptHistoryLimit = limit
		options.attemptHistoryLimitSet = true
	}
}

// NewManager creates an unloaded configuration manager.
//
// The zero value of Manager is also valid. NewManager exists as a convenience
// for callers that prefer constructor-style initialization or manager options.
func NewManager[T any](opts ...ManagerOption) *Manager[T] {
	var options managerOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}

	attemptHistoryLimit := defaultAttemptHistoryLimit
	if options.attemptHistoryLimitSet {
		attemptHistoryLimit = options.attemptHistoryLimit
		if attemptHistoryLimit <= 0 {
			attemptHistoryLimit = -1
		}
	}

	return &Manager[T]{
		attemptHistoryLimit: attemptHistoryLimit,
		observers:           append([]Observer(nil), options.observers...),
	}
}

// Status returns the current observable configuration lifecycle state.
func (m *Manager[T]) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.statusLocked()
}

// Attempts returns the recent load attempts retained by the manager.
//
// The returned slice is ordered from oldest to newest. It is a copy; modifying
// it does not mutate manager state.
func (m *Manager[T]) Attempts() []AttemptRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return cloneAttemptRecords(m.attemptHistory)
}

// Inspect returns a safe operational view of current configuration state.
//
// Inspect does not expose the typed configuration value.
func (m *Manager[T]) Inspect() Inspection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	inspection := Inspection{
		Status: m.statusLocked(),
	}

	if m.current != nil {
		inspection.Redacted = m.current.Redacted()
	}

	return inspection
}

func (m *Manager[T]) statusLocked() Status {
	status := Status{
		LastAttempt: cloneAttemptRecordPtr(m.lastAttempt),
		LastSuccess: cloneAttemptRecordPtr(m.lastSuccess),
		LastFailure: cloneAttemptRecordPtr(m.lastFailure),
		LastApply:   cloneApplyResultPtr(m.lastApply),
	}

	if m.current == nil {
		if m.lastAttempt != nil && m.lastAttempt.Status == AttemptStatusFailed {
			status.State = StatusStateFailed
			return status
		}

		status.State = StatusStateUnloaded
		return status
	}

	metadata := m.current.Metadata()
	status.Current = &metadata

	if m.lastAttempt != nil && m.lastAttempt.Status == AttemptStatusFailed {
		status.State = StatusStateDegraded
		return status
	}

	status.State = StatusStateLoaded
	return status
}

// Snapshot returns the currently published configuration snapshot.
//
// The boolean is false when no valid configuration snapshot has been published.
func (m *Manager[T]) Snapshot() (Snapshot[T], bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.current == nil {
		var zero Snapshot[T]
		return zero, false
	}

	return *m.current, true
}

// Value returns the currently published configuration value.
//
// The boolean is false when no valid configuration snapshot has been published.
func (m *Manager[T]) Value() (T, bool) {
	snapshot, ok := m.Snapshot()
	if !ok {
		var zero T
		return zero, false
	}

	return snapshot.Value(), true
}

// Apply records a LoadResult and publishes its snapshot when the attempt succeeded.
//
// Successful results replace the current snapshot and update last success.
// Failed results preserve the current snapshot and update last failure. Invalid
// results return an error wrapping ErrInvalidLoadResult and do not mutate
// manager state. Apply assigns a fresh manager-local attempt ID regardless of
// any ID on the input result. Apply is serialized with manager-owned load
// attempts.
//
// Apply emits snapshot_applied when a successful snapshot is published. It does
// not emit load_started, load_succeeded, or load_failed because the load
// lifecycle occurred outside the manager-owned load methods.
//
// Callers must pass a non-nil context. Passing nil is invalid and may panic.
func (m *Manager[T]) Apply(ctx context.Context, result LoadResult[T]) (ApplyResult, error) {
	m.attemptMu.Lock()
	defer m.attemptMu.Unlock()

	return m.applyValidated(ctx, result, true)
}

func (m *Manager[T]) applyValidated(ctx context.Context, result LoadResult[T], assignAttemptID bool) (ApplyResult, error) {
	if err := validateLoadResult(result); err != nil {
		return ApplyResult{}, err
	}
	if assignAttemptID {
		m.assignAttemptID(&result)
	}

	return m.apply(ctx, result), nil
}

func (m *Manager[T]) apply(ctx context.Context, result LoadResult[T]) ApplyResult {
	var applied *SnapshotMetadata
	var applyForEvent *ApplyResult
	var attemptForEvent *AttemptRecord
	var applyResult ApplyResult

	m.mu.Lock()

	attempt := result.Attempt
	m.lastAttempt = &attempt
	m.recordAttemptLocked(attempt)
	current := currentSnapshotMetadata(m.current)
	applyResult.Previous = cloneSnapshotMetadataPtr(current)
	applyResult.Current = cloneSnapshotMetadataPtr(current)

	if attempt.Status == AttemptStatusSucceeded && result.Snapshot != nil {
		applyResult.Published = true

		snapshot := *result.Snapshot
		m.current = &snapshot

		success := attempt
		m.lastSuccess = &success

		metadata := snapshot.Metadata()
		applied = &metadata
		applyResult.Current = &metadata
		applyResult.Changed = applyResult.Previous == nil || applyResult.Previous.Checksum != metadata.Checksum

		attemptCopy := attempt
		attemptForEvent = &attemptCopy

		applyCopy := cloneApplyResult(applyResult)
		applyForEvent = &applyCopy
	} else if attempt.Status == AttemptStatusFailed {
		failure := attempt
		m.lastFailure = &failure
	}

	applyStored := cloneApplyResult(applyResult)
	m.lastApply = &applyStored

	m.mu.Unlock()

	if applied != nil {
		m.notify(ctx, Event{
			Kind:        EventKindSnapshotApplied,
			AttemptID:   attempt.ID,
			AttemptKind: attempt.Kind,
			Source:      attempt.Source,
			Revision:    attempt.Revision,
			Attempt:     attemptForEvent,
			Snapshot:    applied,
			Apply:       applyForEvent,
			OccurredAt:  time.Now().UTC(),
		})
	}

	return applyResult
}

// Load runs one load lifecycle and applies the result to the manager.
//
// On success, the produced snapshot becomes current. On failure, the current
// snapshot is left unchanged and the failed attempt is recorded. The returned
// ManagedLoadResult includes both the stateless load result and the manager
// apply result.
//
// Callers must pass a non-nil context. Passing nil is invalid and may panic.
func (m *Manager[T]) Load(ctx context.Context, kind AttemptKind, data SourceData, pipeline Pipeline[T]) (ManagedLoadResult[T], error) {
	m.attemptMu.Lock()
	defer m.attemptMu.Unlock()

	attemptID := m.nextManagedAttemptID()
	m.notifyLoadStarted(ctx, attemptID, kind, data.Metadata, data.Revision)

	loadResult, err := Load(ctx, kind, data, pipeline)
	loadResult.Attempt.ID = attemptID

	m.notifyLoadFinished(ctx, attemptID, kind, loadResult, err)
	applyResult, applyErr := m.applyValidated(ctx, loadResult, false)
	if err == nil {
		err = applyErr
	}
	return ManagedLoadResult[T]{
		Load:  loadResult,
		Apply: applyResult,
	}, err
}

// LoadFromSource reads configuration data from source, runs one load lifecycle,
// and applies the result to the manager.
//
// On success, the produced snapshot becomes current. On failure, the current
// snapshot is left unchanged and the failed attempt is recorded. The returned
// ManagedLoadResult includes both the stateless load result and the manager
// apply result.
//
// Callers must pass a non-nil context. Passing nil is invalid and may panic.
func (m *Manager[T]) LoadFromSource(ctx context.Context, kind AttemptKind, source Source, pipeline Pipeline[T]) (ManagedLoadResult[T], error) {
	m.attemptMu.Lock()
	defer m.attemptMu.Unlock()

	startedAt := time.Now().UTC()
	var sourceMetadata SourceMetadata
	var metadataErr error
	if source != nil {
		sourceMetadata, metadataErr = loadSourceMetadata(source)
	}
	attemptID := m.nextManagedAttemptID()
	m.notifyLoadStarted(ctx, attemptID, kind, sourceMetadata, "")

	loadResult, err := loadFromSourceWithMetadata(ctx, kind, source, pipeline, startedAt, sourceMetadata, metadataErr)
	loadResult.Attempt.ID = attemptID

	m.notifyLoadFinished(ctx, attemptID, kind, loadResult, err)
	applyResult, applyErr := m.applyValidated(ctx, loadResult, false)
	if err == nil {
		err = applyErr
	}
	return ManagedLoadResult[T]{
		Load:  loadResult,
		Apply: applyResult,
	}, err
}

func (m *Manager[T]) notifyLoadStarted(ctx context.Context, attemptID uint64, kind AttemptKind, source SourceMetadata, revision string) {
	m.notify(ctx, Event{
		Kind:        EventKindLoadStarted,
		AttemptID:   attemptID,
		AttemptKind: kind,
		Source:      source,
		Revision:    revision,
		OccurredAt:  time.Now().UTC(),
	})
}

func (m *Manager[T]) notifyLoadFinished(ctx context.Context, attemptID uint64, kind AttemptKind, result LoadResult[T], err error) {
	eventKind := EventKindLoadSucceeded
	if err != nil {
		eventKind = EventKindLoadFailed
	}

	var snapshot *SnapshotMetadata
	if result.Snapshot != nil {
		metadata := result.Snapshot.Metadata()
		snapshot = &metadata
	}

	attempt := result.Attempt
	m.notify(ctx, Event{
		Kind:        eventKind,
		AttemptID:   attemptID,
		AttemptKind: kind,
		Source:      attempt.Source,
		Revision:    attempt.Revision,
		Attempt:     &attempt,
		Snapshot:    snapshot,
		OccurredAt:  time.Now().UTC(),
	})
}

func cloneAttemptRecordPtr(in *AttemptRecord) *AttemptRecord {
	if in == nil {
		return nil
	}

	out := *in
	return &out
}

func cloneAttemptRecords(in []AttemptRecord) []AttemptRecord {
	return append([]AttemptRecord(nil), in...)
}

func (m *Manager[T]) recordAttemptLocked(attempt AttemptRecord) {
	limit := m.attemptHistoryLimit
	if limit == 0 {
		limit = defaultAttemptHistoryLimit
	}
	if limit < 0 {
		return
	}

	m.attemptHistory = append(m.attemptHistory, attempt)
	if len(m.attemptHistory) <= limit {
		return
	}

	copy(m.attemptHistory, m.attemptHistory[len(m.attemptHistory)-limit:])
	m.attemptHistory = m.attemptHistory[:limit]
}

func currentSnapshotMetadata[T any](snapshot *Snapshot[T]) *SnapshotMetadata {
	if snapshot == nil {
		return nil
	}

	metadata := snapshot.Metadata()
	return &metadata
}

func (m *Manager[T]) assignAttemptID(result *LoadResult[T]) uint64 {
	result.Attempt.ID = m.nextManagedAttemptID()
	return result.Attempt.ID
}

func (m *Manager[T]) nextManagedAttemptID() uint64 {
	m.nextAttemptID++
	return m.nextAttemptID
}

func (m *Manager[T]) notify(ctx context.Context, event Event) {
	observers := append([]Observer(nil), m.observers...)

	for _, observer := range observers {
		if observer != nil {
			notifyObserver(ctx, observer, event)
		}
	}
}

func notifyObserver(ctx context.Context, observer Observer, event Event) {
	defer func() {
		_ = recover()
	}()

	observer(ctx, event)
}
