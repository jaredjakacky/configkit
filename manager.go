package configkit

import (
	"context"
	"sync"
	"time"

	opskit "github.com/jaredjakacky/opskit"
)

const defaultAttemptHistoryLimit = 20

// Manager owns the currently published configuration snapshot and lifecycle status.
//
// A Manager does not decide what configuration means. It stores the last known
// good snapshot, records load/reload attempts, and provides safe access to
// current configuration state. Manager-owned load attempts are serialized while
// status and snapshot reads remain concurrent.
type Manager[T any] struct {
	attemptAdmission lifecycleAdmission
	mu               sync.RWMutex

	nextAttemptID uint64

	current     *Snapshot[T]
	lastAttempt *AttemptRecord
	lastSuccess *AttemptRecord
	lastFailure *AttemptRecord
	lastApply   *ApplyResult

	attemptHistory      []AttemptRecord
	attemptHistoryLimit int

	observers        []Observer
	componentInfo    opskit.ComponentInfo
	degradedReady    bool
	degradedReadySet bool
}

// lifecycleAdmission serializes Manager-owned lifecycle mutations while
// allowing callers to stop waiting when their context is canceled. Its zero
// value is ready for use so Manager's zero value remains valid.
type lifecycleAdmission struct {
	once sync.Once
	held chan struct{}
}

func (a *lifecycleAdmission) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	a.once.Do(func() {
		a.held = make(chan struct{}, 1)
	})

	select {
	case a.held <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Prefer cancellation when admission and cancellation become ready at the
	// same time. Once this check succeeds, the lifecycle attempt is admitted
	// and later cancellation follows the existing cooperative context contract.
	if err := ctx.Err(); err != nil {
		<-a.held
		return err
	}

	return nil
}

func (a *lifecycleAdmission) release() {
	<-a.held
}

// ManagerOption configures a Manager.
type ManagerOption func(*managerOptions)

type managerOptions struct {
	observers              []Observer
	attemptHistoryLimit    int
	attemptHistoryLimitSet bool
	componentInfo          opskit.ComponentInfo
	componentInfoSet       bool
	degradedReady          bool
	degradedReadySet       bool
}

// WithObservers registers configuration lifecycle observers for the manager.
//
// Observers run synchronously by default. They should return quickly and must
// not call Load, LoadFromSource, or Apply on the same manager. Read-only calls
// such as LifecycleStatus, LifecycleInspection, Snapshot, and Value are
// acceptable. Synchronous observers see manager state consistent with the
// event boundary. Use AsyncObserver or another goroutine for follow-up work
// that may block or trigger lifecycle operations.
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

// WithIdentity sets the Opskit component name for the manager.
//
// The default name is "config". Empty names are ignored. Non-empty names should
// satisfy Opskit component-name rules; invalid names may fail when the manager
// is registered with an Opskit registry.
func WithIdentity(name string) ManagerOption {
	return func(options *managerOptions) {
		if name == "" {
			return
		}
		options.componentInfo.Name = name
		options.componentInfoSet = true
	}
}

// WithComponentInfo sets the Opskit component identity for the manager.
//
// Empty fields fall back to Configkit defaults. Labels are appended to stable
// Configkit labels; the kit=configkit label is always preserved. Non-empty
// component names should satisfy Opskit component-name rules; invalid names may
// fail when the manager is registered with an Opskit registry.
func WithComponentInfo(info opskit.ComponentInfo) ManagerOption {
	return func(options *managerOptions) {
		options.componentInfo = info
		options.componentInfoSet = true
	}
}

// WithDegradedReady configures whether degraded Configkit lifecycle state is
// ready through Opskit readiness.
//
// The default is true because degraded means a valid last-known-good snapshot
// remains active after a failed later attempt.
func WithDegradedReady(ready bool) ManagerOption {
	return func(options *managerOptions) {
		options.degradedReady = ready
		options.degradedReadySet = true
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
	degradedReady := true
	if options.degradedReadySet {
		degradedReady = options.degradedReady
	}

	return &Manager[T]{
		attemptHistoryLimit: attemptHistoryLimit,
		observers:           append([]Observer(nil), options.observers...),
		componentInfo:       managerComponentInfo(options.componentInfo, options.componentInfoSet),
		degradedReady:       degradedReady,
		degradedReadySet:    true,
	}
}

// LifecycleStatus returns the current observable configuration lifecycle state.
func (m *Manager[T]) LifecycleStatus() LifecycleStatus {
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

// LifecycleInspection returns a safe operational view of current configuration state.
//
// LifecycleInspection does not expose the typed configuration value.
func (m *Manager[T]) LifecycleInspection() LifecycleInspection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	inspection := LifecycleInspection{
		Status: m.statusLocked(),
	}

	if m.current != nil {
		inspection.Redacted = m.current.Redacted()
	}

	return inspection
}

func (m *Manager[T]) statusLocked() LifecycleStatus {
	status := LifecycleStatus{
		State:       m.lifecycleStateLocked(),
		LastAttempt: cloneAttemptRecordPtr(m.lastAttempt),
		LastSuccess: cloneAttemptRecordPtr(m.lastSuccess),
		LastFailure: cloneAttemptRecordPtr(m.lastFailure),
		LastApply:   cloneApplyResultPtr(m.lastApply),
	}

	if m.current != nil {
		metadata := m.current.Metadata()
		status.Current = &metadata
	}

	return status
}

func (m *Manager[T]) lifecycleStateLocked() LifecycleState {
	if m.current == nil {
		if m.lastAttempt != nil && m.lastAttempt.Status == AttemptStatusFailed {
			return LifecycleStateFailed
		}
		return LifecycleStateUnloaded
	}
	if m.lastAttempt != nil && m.lastAttempt.Status == AttemptStatusFailed {
		return LifecycleStateDegraded
	}
	return LifecycleStateLoaded
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
// manager state. Successful results must have matching source, revision, and
// non-empty checksum metadata and no failure stage or detail. Failed results
// must have no snapshot or checksum and must have a failure stage and safe
// public failure detail. Apply assigns a fresh manager-local attempt ID
// regardless of any ID on the input result. Apply is serialized with
// manager-owned load attempts. The returned ApplyResult captures the lifecycle
// state produced by this mutation; later manager operations do not change it.
//
// If ctx is canceled before Apply gains lifecycle admission, Apply returns the
// context error without validating or recording the result, assigning an
// attempt ID, notifying observers, or publishing a snapshot. Once admitted,
// Apply is not rolled back by later cancellation.
//
// Apply emits snapshot_applied when a successful snapshot is published. It does
// not emit load_started, load_succeeded, or load_failed because the load
// lifecycle occurred outside the manager-owned load methods.
//
// Callers must pass a non-nil context. Passing nil is invalid and may panic.
func (m *Manager[T]) Apply(ctx context.Context, result LoadResult[T]) (ApplyResult, error) {
	if err := m.attemptAdmission.acquire(ctx); err != nil {
		return ApplyResult{}, err
	}
	defer m.attemptAdmission.release()

	if err := validateLoadResult(result); err != nil {
		return ApplyResult{}, err
	}
	m.assignAttemptID(&result)

	applyResult := m.apply(result)
	m.notifySnapshotApplied(ctx, result.Attempt, applyResult)
	return applyResult, nil
}

func (m *Manager[T]) applyValidated(result LoadResult[T]) (ApplyResult, error) {
	if err := validateLoadResult(result); err != nil {
		return ApplyResult{}, err
	}

	return m.apply(result), nil
}

func (m *Manager[T]) apply(result LoadResult[T]) ApplyResult {
	var applyResult ApplyResult

	m.mu.Lock()
	applyResult.AppliedAt = time.Now().UTC()

	attempt := cloneAttemptRecord(result.Attempt)
	lastAttempt := cloneAttemptRecord(attempt)
	m.lastAttempt = &lastAttempt
	m.recordAttemptLocked(attempt)
	current := currentSnapshotMetadata(m.current)
	applyResult.Previous = cloneSnapshotMetadataPtr(current)
	applyResult.Current = cloneSnapshotMetadataPtr(current)

	if attempt.Status == AttemptStatusSucceeded && result.Snapshot != nil {
		applyResult.Published = true

		snapshot := *result.Snapshot
		m.current = &snapshot

		success := cloneAttemptRecord(attempt)
		m.lastSuccess = &success

		metadata := snapshot.Metadata()
		applyResult.Current = &metadata
		applyResult.Changed = applyResult.Previous == nil || applyResult.Previous.Checksum != metadata.Checksum
	} else if attempt.Status == AttemptStatusFailed {
		failure := cloneAttemptRecord(attempt)
		m.lastFailure = &failure
	}

	applyResult.ManagerState = m.lifecycleStateLocked()
	applyStored := cloneApplyResult(applyResult)
	m.lastApply = &applyStored

	m.mu.Unlock()

	return applyResult
}

// Load runs one load lifecycle and applies the result to the manager.
//
// On success, the produced snapshot becomes current. On failure, the current
// snapshot is left unchanged and the failed attempt is recorded. The returned
// ManagedLoadResult includes both the stateless load result and the manager
// apply result.
//
// Load updates manager state before delivering load_succeeded or load_failed.
// On successful publication, snapshot_applied is delivered after the load
// completion event. Observer notifications run without holding the manager
// state lock.
//
// If ctx is canceled before Load gains lifecycle admission, Load returns the
// context error without starting or recording an attempt. Once admitted,
// cancellation is cooperative through the load lifecycle and observers.
//
// Callers must pass a non-nil context. Passing nil is invalid and may panic.
func (m *Manager[T]) Load(ctx context.Context, kind AttemptKind, data SourceData, pipeline Pipeline[T]) (ManagedLoadResult[T], error) {
	if err := m.attemptAdmission.acquire(ctx); err != nil {
		return ManagedLoadResult[T]{}, err
	}
	defer m.attemptAdmission.release()

	attemptID := m.nextManagedAttemptID()
	m.notifyLoadStarted(ctx, attemptID, kind, data.Metadata, data.Revision)

	loadResult, err := Load(ctx, kind, data, pipeline)
	loadResult.Attempt.ID = attemptID

	applyResult, applyErr := m.applyValidated(loadResult)
	if err == nil {
		err = applyErr
	}
	m.notifyLoadFinished(ctx, attemptID, kind, loadResult, applyResult, err)
	m.notifySnapshotApplied(ctx, loadResult.Attempt, applyResult)
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
// LoadFromSource updates manager state before delivering load_succeeded or
// load_failed. On successful publication, snapshot_applied is delivered after
// the load completion event. Observer notifications run without holding the
// manager state lock.
// An admitted nil or typed-nil Source records the normal missing-source failure.
//
// If ctx is canceled before LoadFromSource gains lifecycle admission,
// LoadFromSource returns the context error without reading source metadata,
// reading the source, or starting or recording an attempt. Once admitted,
// cancellation is cooperative through the load lifecycle and observers.
//
// Callers must pass a non-nil context. Passing nil is invalid and may panic.
func (m *Manager[T]) LoadFromSource(ctx context.Context, kind AttemptKind, source Source, pipeline Pipeline[T]) (ManagedLoadResult[T], error) {
	if err := m.attemptAdmission.acquire(ctx); err != nil {
		return ManagedLoadResult[T]{}, err
	}
	defer m.attemptAdmission.release()

	startedAt := time.Now().UTC()
	var sourceMetadata SourceMetadata
	var metadataErr error
	if !isNilSource(source) {
		sourceMetadata, metadataErr = loadSourceMetadata(source)
	}
	attemptID := m.nextManagedAttemptID()
	m.notifyLoadStarted(ctx, attemptID, kind, sourceMetadata, "")

	loadResult, err := loadFromSourceWithMetadata(ctx, kind, source, pipeline, startedAt, sourceMetadata, metadataErr)
	loadResult.Attempt.ID = attemptID

	applyResult, applyErr := m.applyValidated(loadResult)
	if err == nil {
		err = applyErr
	}
	m.notifyLoadFinished(ctx, attemptID, kind, loadResult, applyResult, err)
	m.notifySnapshotApplied(ctx, loadResult.Attempt, applyResult)
	return ManagedLoadResult[T]{
		Load:  loadResult,
		Apply: applyResult,
	}, err
}

func (m *Manager[T]) notifyLoadStarted(ctx context.Context, attemptID uint64, kind AttemptKind, source SourceMetadata, revision string) {
	managerState := m.LifecycleStatus().State
	m.notify(ctx, Event{
		Kind:          EventKindLoadStarted,
		ComponentName: m.componentName(),
		ManagerState:  managerState,
		AttemptID:     attemptID,
		AttemptKind:   kind,
		Source:        source,
		Revision:      revision,
		OccurredAt:    time.Now().UTC(),
	})
}

func (m *Manager[T]) notifyLoadFinished(ctx context.Context, attemptID uint64, kind AttemptKind, result LoadResult[T], apply ApplyResult, err error) {
	eventKind := EventKindLoadSucceeded
	if err != nil {
		eventKind = EventKindLoadFailed
	}

	var snapshot *SnapshotMetadata
	if result.Snapshot != nil {
		metadata := result.Snapshot.Metadata()
		snapshot = &metadata
	}

	attempt := cloneAttemptRecord(result.Attempt)
	applyForEvent := cloneApplyResult(apply)
	m.notify(ctx, Event{
		Kind:          eventKind,
		ComponentName: m.componentName(),
		ManagerState:  apply.ManagerState,
		AttemptID:     attemptID,
		AttemptKind:   kind,
		Source:        attempt.Source,
		Revision:      attempt.Revision,
		Attempt:       &attempt,
		Snapshot:      snapshot,
		Apply:         &applyForEvent,
		OccurredAt:    time.Now().UTC(),
	})
}

func (m *Manager[T]) notifySnapshotApplied(ctx context.Context, attempt AttemptRecord, apply ApplyResult) {
	if !apply.Published || apply.Current == nil {
		return
	}

	attemptForEvent := cloneAttemptRecord(attempt)
	snapshotForEvent := cloneSnapshotMetadataPtr(apply.Current)
	applyForEvent := cloneApplyResult(apply)
	m.notify(ctx, Event{
		Kind:          EventKindSnapshotApplied,
		ComponentName: m.componentName(),
		ManagerState:  apply.ManagerState,
		AttemptID:     attempt.ID,
		AttemptKind:   attempt.Kind,
		Source:        attempt.Source,
		Revision:      attempt.Revision,
		Attempt:       &attemptForEvent,
		Snapshot:      snapshotForEvent,
		Apply:         &applyForEvent,
		OccurredAt:    time.Now().UTC(),
	})
}

func cloneAttemptRecordPtr(in *AttemptRecord) *AttemptRecord {
	if in == nil {
		return nil
	}

	out := cloneAttemptRecord(*in)
	return &out
}

func cloneAttemptRecords(in []AttemptRecord) []AttemptRecord {
	out := append([]AttemptRecord(nil), in...)
	for i := range out {
		out[i] = cloneAttemptRecord(out[i])
	}
	return out
}

func (m *Manager[T]) recordAttemptLocked(attempt AttemptRecord) {
	limit := m.attemptHistoryLimit
	if limit == 0 {
		limit = defaultAttemptHistoryLimit
	}
	if limit < 0 {
		return
	}

	m.attemptHistory = append(m.attemptHistory, cloneAttemptRecord(attempt))
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
			notifyObserver(ctx, observer, cloneEvent(event))
		}
	}
}

func cloneEvent(event Event) Event {
	out := event
	out.Attempt = cloneAttemptRecordPtr(event.Attempt)
	if event.Snapshot != nil {
		snapshot := *event.Snapshot
		out.Snapshot = &snapshot
	}
	if event.Apply != nil {
		apply := cloneApplyResult(*event.Apply)
		out.Apply = &apply
	}
	return out
}

func notifyObserver(ctx context.Context, observer Observer, event Event) {
	defer func() {
		_ = recover()
	}()

	observer(ctx, event)
}
