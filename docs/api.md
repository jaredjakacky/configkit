# API Map

This is the fast way to orient yourself in Configkit's public API.

It covers the exported surface of the root `configkit` package and the optional
`opshttp` and `otel` adapter packages.

Go doc comments remain the canonical symbol-level reference. This file is the
companion view that groups the exported surface by the decisions you make when
using the package.

If you only remember the common path, remember this:

- `NewBytesSource(...)` or `NewFileSource(...)` provides raw configuration bytes
- `Pipeline[T]` describes decode, defaults, validation, redaction, and checksum
- `NewManager[T](...)` owns the current last-known-good snapshot
- `Manager.LoadFromSource(...)` reads, validates, publishes, records status, and notifies observers

Everything else in this file exists to customize that path without turning
configuration into a framework.

## Package `configkit`

### Start here

- `Manager[T]`

  The main lifecycle object. It owns the current published snapshot, status,
  recent attempts, last-known-good preservation, and observer notifications.

- `NewManager[T](...)`

  Creates an unloaded manager. The zero value of `Manager[T]` is also valid,
  but `NewManager` is the normal path when using options.

- `Pipeline[T]`

  Describes how raw `SourceData` becomes a publishable typed snapshot.

- `Source`

  Interface for reading raw configuration data. Sources do not decode,
  validate, redact, checksum, publish, reload, or decide application meaning.

### Common load path

- `NewBytesSource(data []byte, metadata SourceMetadata, revision string)`

  Creates an in-memory source. The input bytes are copied. Use this for tests,
  examples, embedded config, or callers that already have raw config bytes.

- `NewFileSource(path string, metadata SourceMetadata)`

  Creates a local file source. If `metadata.Name` is empty, the base file name
  is used. If `metadata.Kind` is empty, it defaults to `"file"`. File revisions
  are SHA-256 fingerprints of the file bytes.

- `Pipeline[T].Decode`

  Required decoder step.

- `Pipeline[T].Redact`

  Required redaction step. Use `EmptyRedactor[T]()` until fields have been
  explicitly reviewed as safe for operational output.

- `Pipeline[T].Checksum`

  Required checksum step.

- `Pipeline[T].ApplyDefaults`

  Optional defaults step. Runs after decode and before validation.

- `Pipeline[T].ValidateConfig`

  Optional typed validation step. Its error is returned privately to the load
  caller; operational records use the stable `validate_config_failed` failure
  code and bounded public text.

- `Pipeline[T].Copy`

  Optional copy step. Use it when `T` contains maps, slices, pointers, or other
  mutable references that should be detached before snapshot publication.

- `Pipeline[T].Validate()`

  Checks that required pipeline steps are present.

- `Manager.LoadFromSource(ctx, kind, source, pipeline)`

  Reads from a source, runs the load lifecycle, applies the result, records the
  attempt, preserves the last-known-good snapshot on failure, and emits observer
  events. Manager lifecycle mutation is serialized. Cancellation before
  admission returns the context error without reading the source or starting or
  recording an attempt.

- `Manager.Load(ctx, kind, data, pipeline)`

  Runs the load lifecycle against already-read source data and applies the
  result to the manager. Cancellation before serialized lifecycle admission
  returns the context error without starting or recording an attempt.

### Stateless load path

- `LoadFromSource[T](ctx, kind, source, pipeline)`

  Reads from a source and runs one load lifecycle without storing or publishing
  the produced snapshot.

- `Load[T](ctx, kind, data, pipeline)`

  Runs one load lifecycle against already-read `SourceData` without reading
  from a source or publishing state.

- `LoadResult[T]`

  Result of one stateless load attempt. Successful results contain a snapshot
  and a succeeded attempt record. Failed results contain no snapshot and a
  failed attempt record.

- `LoadResult[T].Snapshot`

  Snapshot produced by a successful load. It is nil on failed attempts.

- `LoadResult[T].Attempt`

  Attempt record for the load lifecycle.

- `ManagedLoadResult[T]`

  Result returned by manager-owned load methods. `Load` is the stateless
  lifecycle result. `Apply` describes how that result affected manager state.

- `ManagedLoadResult[T].Load`

  Stateless load result.

- `ManagedLoadResult[T].Apply`

  Manager apply result.

### Source model

- `Source`

  Interface implemented by configuration sources.

  Shape:

  ```go
  Metadata() SourceMetadata
  Read(context.Context) (SourceData, error)
  ```

- `SourceData`

  Raw bytes plus source metadata and an optional revision.

- `SourceData.Data`

  Raw configuration payload.

- `SourceData.Metadata`

  Source metadata associated with the payload.

- `SourceData.Revision`

  Optional source-provided version, generation, ETag, resource version, commit
  SHA, or similar identifier.

- `SourceMetadata`

  Safe operational source description.

- `SourceMetadata.Name`

  Human-readable source name. It may appear in logs, telemetry, status,
  inspection, and adapter output, so it should not contain secrets or sensitive
  tenant, user, path, or environment details.

- `SourceMetadata.Kind`

  Broad source category, such as `"env"`, `"file"`, `"memory"`, `"remote"`, or
  `"composite"`.

- `SourceMetadata.Description`

  Optional safe operational context about the source.

- `ErrMissingSource`

  Returned when `LoadFromSource` is called without a source.

- `BytesSource`

  In-memory `Source` implementation.

- `BytesSource.Metadata()`

  Returns the source metadata.

- `BytesSource.Read(...)`

  Returns copied source data.

- `FileSource`

  Local file `Source` implementation.

- `FileSource.Metadata()`

  Returns the source metadata.

- `FileSource.Read(...)`

  Reads the current file contents and returns a SHA-256 source revision.

### Pipeline steps

- `Decoder[T]`

  Turns `SourceData` into a typed configuration value.

  Shape: `func(context.Context, SourceData) (T, error)`

- `JSONDecoder[T]()`

  Production-oriented standard-library JSON decoder for `SourceData.Data`.
  Rejects unknown fields at ordinary Go struct boundaries and requires exactly
  one JSON value. Leading and trailing whitespace are accepted. Field matching,
  duplicate keys, maps, and custom unmarshalers otherwise retain
  `encoding/json` behavior.

- `LenientJSONDecoder[T]()`

  Preserves `json.Unmarshal` behavior, including ignoring unknown struct fields.
  It still requires exactly one JSON value and rejects non-whitespace trailing
  data.

- `DefaultApplier[T]`

  Applies mechanical defaults before validation.

  Shape: `func(context.Context, T) (T, error)`

- `Validator[T]`

  Validates a typed configuration value.

  Shape: `func(context.Context, T) error`

- `Copier[T]`

  Returns the final value to publish in the snapshot.

  Shape: `func(context.Context, T) (T, error)`

- `Redactor[T]`

  Builds the safe operational view for a configuration value.

  Shape: `func(context.Context, T) (RedactedView, error)`

- `EmptyRedactor[T]()`

  Built-in redactor that exposes no configuration fields.

- `Checksummer[T]`

  Computes a stable operational fingerprint for the effective configuration.

  Shape: `func(context.Context, T) (string, error)`

- `SHA256JSONChecksum[T]()`

  Built-in checksummer that hashes the JSON representation of the effective
  config value.

- `ErrMissingDecoder`

  Pipeline validation error for a missing decoder.

- `ErrMissingRedactor`

  Pipeline validation error for a missing redactor.

- `ErrMissingChecksum`

  Pipeline validation error for a missing checksummer.

### Snapshot model

- `Snapshot[T]`

  One successfully loaded, validated, and published configuration value.

- `NewSnapshot[T](value, metadata, redacted)`

  Creates a snapshot from an already-loaded value, metadata, and redacted view.

- `Snapshot.Value()`

  Returns the typed configuration value by normal Go assignment rules.

- `Snapshot.Metadata()`

  Returns snapshot metadata.

- `Snapshot.Redacted()`

  Returns a copy of the top-level redacted map.

- `SnapshotMetadata`

  Operational metadata for a published snapshot.

- `SnapshotMetadata.Source`

  Source metadata associated with the snapshot.

- `SnapshotMetadata.Revision`

  Optional source-provided version, generation, ETag, resource version, commit
  SHA, or similar identifier.

- `SnapshotMetadata.Checksum`

  Stable fingerprint of the effective configuration value.

- `SnapshotMetadata.LoadedAt`

  Time when the snapshot was successfully loaded and published.

- `RedactedView`

  Map shape returned by application redactors and exposed through inspection.
  Its safety depends entirely on the application's `Redactor`.

### Manager state

- `Manager.LifecycleStatus()`

  Returns the current observable lifecycle state.

- `Manager.LifecycleInspection()`

  Returns `LifecycleInspection`, including status and the current snapshot's redacted
  view when a snapshot is active. It does not expose the typed config value.

- `Manager.ComponentInfo()`

  Returns the manager's Opskit component identity. Defaults to name `config`,
  kind `config`, description `application configuration`, and label
  `kit=configkit`.

- `Manager.Status(ctx)`

  Returns the manager's cached lifecycle state as an `opskit.Status`. Status
  attributes are intentionally low-cardinality and do not include source names,
  revisions, checksums, errors, paths, tenant IDs, or redacted config values.

- `Manager.Readiness(ctx)`

  Returns the manager's configured Opskit readiness. Degraded is ready by
  default because a last-known-good snapshot remains active. Configkit has no
  child readiness domain, so `Items` is empty; the Opskit registry owns the
  parent component identity and required/optional registration policy.

- `Manager.Inspect(ctx)`

  Returns an `opskit.Inspection` with lifecycle summary, redacted config
  details, retained attempts, and last apply result. It does not expose the
  typed config value.

- `Manager.Attempts()`

  Returns retained attempts ordered from oldest to newest.

- `Manager.Snapshot()`

  Returns the current snapshot and a boolean indicating whether one exists.

- `Manager.Value()`

  Returns the current typed config value and a boolean indicating whether one
  exists.

- `Manager.Apply(ctx, result)`

  Applies an externally produced `LoadResult`. Successful results publish the
  snapshot. Failed results preserve the current snapshot. Invalid results return
  an error wrapping `ErrInvalidLoadResult` and do not mutate manager state.
  Apply emits `snapshot_applied` when it publishes a snapshot, but it does not
  emit load lifecycle events because it did not perform the load. Cancellation
  before serialized lifecycle admission returns the context error without
  validating or recording the result, assigning an attempt ID, notifying
  observers, or publishing. Once admitted, later cancellation does not roll
  back the apply.

- `ManagerOption`

  Manager-wide configuration hook.

- `WithObservers(observers ...Observer)`

  Registers lifecycle observers. Observers run synchronously by default, should
  return quickly, and must not call `Load`, `LoadFromSource`, or `Apply` on the
  same manager that emitted the event. Read-only calls such as `LifecycleStatus`,
  `LifecycleInspection`, `Snapshot`, and `Value` are acceptable.

- `WithAttemptHistoryLimit(limit int)`

  Configures retained attempt history. The default is `20`. A value less than
  or equal to zero disables history while preserving last attempt, success, and
  failure status.

- `WithIdentity(name string)`

  Sets the manager's Opskit component name. The default is `config`. Non-empty
  names should satisfy Opskit component-name rules; invalid names may fail when
  the manager is registered with an Opskit registry.

- `WithComponentInfo(info opskit.ComponentInfo)`

  Sets the manager's Opskit component identity. Empty fields fall back to
  Configkit defaults. Non-empty component names should satisfy Opskit
  component-name rules; invalid names may fail when the manager is registered
  with an Opskit registry.

- `WithDegradedReady(ready bool)`

  Configures whether degraded lifecycle state is ready through Opskit
  readiness. The default is `true`.

- `ErrInvalidLoadResult`

  Returned when `Manager.Apply` receives a malformed `LoadResult`.

### Status and inspection

- `LifecycleStatus`

  Observable lifecycle state. It is intentionally not generic and does not
  expose the typed config value.

- `LifecycleStatus.State`

  High-level lifecycle state.

- `LifecycleStatus.Current`

  Current snapshot metadata when a snapshot is active.

- `LifecycleStatus.LastAttempt`

  Most recent load or reload attempt.

- `LifecycleStatus.LastSuccess`

  Most recent successful attempt.

- `LifecycleStatus.LastFailure`

  Most recent failed attempt.

- `LifecycleStatus.LastApply`

  Most recent apply result.

- `LifecycleState`

  String enum for lifecycle state.

- `LifecycleStateUnloaded`

  No valid snapshot has been published and no failed load attempt has been
  recorded.

- `LifecycleStateLoaded`

  A valid snapshot is active and the most recent attempt did not fail.

- `LifecycleStateFailed`

  No valid snapshot is active because the most recent load attempt failed.

- `LifecycleStateDegraded`

  A valid snapshot is active, but the most recent load or reload attempt failed.
  The last-known-good snapshot remains active.

- `LifecycleInspection`

  Safe operational view with `LifecycleStatus` plus application-owned `RedactedView`.

- `LifecycleInspection.Status`

  Current lifecycle status.

- `LifecycleInspection.Redacted`

  Current snapshot redacted view, when a snapshot is active.

- `LifecycleProvider[T]`

  Read-only typed config access interface.

  Shape:

  ```go
  Snapshot() (Snapshot[T], bool)
  Value() (T, bool)
  LifecycleStatus() LifecycleStatus
  ```

- `LifecycleInspector`

  Safe operational inspection interface.

  Shape: `LifecycleInspection() LifecycleInspection`

### Attempts and apply results

- `AttemptKind`

  Why configuration loading was attempted.

- `AttemptKindInitialLoad`

  Initial configuration load.

- `AttemptKindReload`

  Later reload attempt.

- `AttemptStatus`

  Attempt outcome.

- `AttemptStatusSucceeded`

  Load lifecycle completed successfully and produced a snapshot.

- `AttemptStatusFailed`

  Load lifecycle failed and produced no snapshot.

- `AttemptStage`

  Failed lifecycle stage.

- `AttemptStageContext`

  Context cancellation or deadline stage.

- `AttemptStageSourceRead`

  Source metadata or read stage.

- `AttemptStagePipelineValidate`

  Pipeline validation stage.

- `AttemptStageDecode`

  Decode stage.

- `AttemptStageDefaults`

  Defaults stage.

- `AttemptStageValidateConfig`

  Typed validation stage.

- `AttemptStageCopy`

  Copy stage.

- `AttemptStageRedact`

  Redaction stage.

- `AttemptStageChecksum`

  Checksum stage.

- `AttemptRecord`

  One load or reload attempt. Manager-owned attempts receive manager-local IDs.

- `AttemptRecord.ID`

  Manager-local attempt identifier.

- `AttemptRecord.Kind`

  Why the attempt ran.

- `AttemptRecord.Status`

  Whether the attempt succeeded or failed.

- `AttemptRecord.Stage`

  Failed lifecycle stage. Successful attempts usually leave this empty.

- `AttemptRecord.Source`

  Source metadata associated with the attempt.

- `AttemptRecord.Revision`

  Source revision associated with the attempt.

- `AttemptRecord.Checksum`

  Effective configuration checksum for a successful attempt.

- `AttemptRecord.StartedAt`

  Attempt start time.

- `AttemptRecord.EndedAt`

  Attempt end time.

- `AttemptRecord.Failure`

  Stage-specific public `opskit.Failure` for a failed attempt. The underlying
  error remains on the direct load call's return path and is not serialized.

- `ApplyResult`

  Describes how a `LoadResult` affected manager state.

- `ApplyResult.Published`

  True when a successful snapshot became current.

- `ApplyResult.Changed`

  True when the current effective checksum differs from the previous snapshot
  checksum.

- `ApplyResult.Previous`

  Snapshot metadata that was current before apply, if any.

- `ApplyResult.Current`

  Snapshot metadata that is current after apply, if any.

### Observability

- `EventKind`

  Lifecycle event enum.

- `EventKindLoadStarted`

  Manager-owned load or reload has started.

- `EventKindLoadSucceeded`

  Load lifecycle succeeded.

- `EventKindLoadFailed`

  Load lifecycle failed.

- `EventKindSnapshotApplied`

  A successful snapshot was published.

- `Event`

  Operational event delivered to observers. It does not expose the typed config
  value or internal errors, but it can include metadata, revisions, checksums,
  and public failure detail.

- `Event.Kind`

  Lifecycle event kind.

- `Event.AttemptID`

  Manager-local attempt identifier when available.

- `Event.AttemptKind`

  Initial load or reload kind when available.

- `Event.Source`

  Source metadata associated with the event.

- `Event.Revision`

  Source revision associated with the event.

- `Event.Attempt`

  Attempt record associated with load completion or snapshot application events.

- `Event.Snapshot`

  Snapshot metadata associated with successful load or apply events.

- `Event.Apply`

  Apply result associated with snapshot application events.

- `Event.OccurredAt`

  Event time.

- `Observer`

  Receives lifecycle events. Synchronous observers should return quickly and
  must not call `Load`, `LoadFromSource`, or `Apply` on the same manager. Use
  `AsyncObserver` or another goroutine for follow-up work that may block or
  trigger lifecycle operations.

  Shape: `func(context.Context, Event)`

- `SlogObserver(logger *slog.Logger)`

  Returns an observer that logs lifecycle metadata through `log/slog`. It does
  not log typed config values or redacted fields.

- `AsyncObserver`

  Background adapter for observers that may block.

- `NewAsyncObserver(observer, opts...)`

  Creates an async adapter and starts one background goroutine.

- `AsyncObserver.Observer()`

  Returns the observer function to register with a manager.

- `AsyncObserver.Notify(...)`

  Queues an event for asynchronous delivery without blocking the caller.

- `AsyncObserver.Dropped()`

  Returns the count of events dropped because the queue was full or closed.

- `AsyncObserver.Close(ctx)`

  Stops new events and waits for queued events to drain.

- `AsyncObserverOption`

  Async observer configuration hook.

- `WithAsyncObserverBuffer(buffer int)`

  Configures the async event buffer. The default is `64`. Values less than or
  equal to zero disable buffering.

### Context and safety contract

Lifecycle APIs require non-nil contexts:

- `Load`
- `LoadFromSource`
- `Manager.Load`
- `Manager.LoadFromSource`
- `Manager.Apply`
- source `Read` methods
- pipeline step functions

Passing nil to lifecycle APIs is invalid and may panic. `AsyncObserver.Notify`
and `AsyncObserver.Close` are defensive and normalize nil contexts to
`context.Background()`.

Manager-owned `Load`, `LoadFromSource`, and `Apply` calls wait for serialized
lifecycle admission using their contexts. Cancellation before admission returns
the context error with a zero result and does not consume an attempt ID, emit
events, enter history, change lifecycle status, read a source, or publish a
snapshot. After admission, cancellation is cooperative through sources,
pipeline steps, and observers. It does not retroactively erase an attempt or
roll back publication.

Operational output does not include raw typed configuration values, but it can
include source metadata, revisions, checksums, redacted values, and error
strings. Treat status, inspection, observers, logs, telemetry, and adapter
responses as visible to their audience.

## Package `opshttp`

The `opshttp` package provides optional Configkit-specific HTTP inspection for
Servekit. The primary Kit Series composition path is registering
`Manager[T]` in an Opskit registry and passing that registry to
`servekit.WithOps`. Applications only compile `opshttp` when they import it;
Servekit remains in Configkit's module graph because the adapter shares the
root module. See the README's
[dependency model](../README.md#dependency-model).

### Route mounting

- `Mount(server, inspector, opts...)`

  Mounts read-only Configkit operational routes on a Servekit server.

  Default routes:

  - `GET /admin/config`
  - `GET /admin/config/attempts` when the inspector also implements `AttemptProvider`

- `Option`

  Mount configuration hook.

- `WithPathPrefix(prefix string)`

  Sets the base path. The default is `/admin/config`. Prefixes must be absolute,
  clean, not `/`, and must not end in `/`.

- `WithEndpointOptions(opts ...servekit.EndpointOption)`

  Applies Servekit endpoint options to every mounted route. Use this for auth,
  middleware, timeouts, response policy, body limits, or telemetry controls.

- `AttemptProvider`

  Optional interface for values that expose recent attempts.

  Shape: `Attempts() []configkit.AttemptRecord`

- `ErrMissingServer`

  Returned when `Mount` is called without a Servekit server.

- `ErrMissingInspector`

  Returned when `Mount` is called with a nil or typed-nil Configkit inspector.

### Readiness

- `ReadinessCheck(provider)`

  Adapts Configkit core readiness into a Servekit readiness check for services
  that are not using an Opskit registry. Nil and typed-nil providers produce a
  missing-provider error when the check runs.

  Default policy:

  - `unloaded`: not ready
  - `failed`: not ready
  - `loaded`: ready
  - `degraded`: ready

- `ReadinessProvider`

  Interface for values that expose Configkit readiness.

  Shape: `Readiness(context.Context) opskit.Readiness`

  Use `configkit.WithDegradedReady(false)` when constructing a manager to make
  degraded lifecycle state not ready. The default is `true` because
  degraded means the last-known-good snapshot remains active.

### Opskit reload command

- `ReloadCommand[T](manager, source, pipeline, opts...)`

  Creates an Opskit component that implements `opskit.CommandHandler` and
  `opskit.CommandDescriber` for Configkit reload.

  Default component name: `config-reload`

  Default command name: `config/reload`

  The command calls
  `manager.LoadFromSource(ctx, configkit.AttemptKindReload, source, pipeline)`.
  Successful reloads return completed Opskit command results. Admitted reload
  failures, including context cancellation and deadline expiration, return
  failed command results with safe top-level failure detail and no result
  payload. Missing manager, source, or required pipeline steps make handler
  status not ready and reject commands without recording a load attempt.

  Handler status describes static command usability only. It does not read the
  source or mirror manager lifecycle and readiness state.

- `ReloadCommandResult`

  Safe operational result payload returned by successfully completed reload
  commands.

  Fields:

  - `attempt_id`
  - `attempt_status`
  - `manager_state`
  - `published`
  - `changed`
  - `current_checksum`
  - `current_revision`
  - `failure`

  The payload does not expose typed config values, redacted inspection output,
  or internal error causes. Revisions and checksums are operationally visible.
  Failed commands expose safe public detail through `CommandResult.Failure`
  instead of this payload. The payload's `failure` field is normally omitted
  for successfully completed commands; it remains available to
  `NewReloadCommandResult` callers building a safe view of a failed managed load.

- `ReloadCommandOption`

  Reload command configuration hook.

- `WithReloadCommandName(name string)`

  Sets the Opskit command name. Empty names preserve the default.

- `WithReloadCommandDescription(description string)`

  Sets the command discovery description.

- `WithReloadCommandComponentInfo(info opskit.ComponentInfo)`

  Sets the Opskit component identity for the reload command handler. Empty
  fields fall back to Configkit defaults.

  Workerkit executes this handler through its generic
  `workerkit.CommandFromOpskit(...)` adapter. Failed reloads therefore enter
  Workerkit's command failure, observation, and configured retry path without
  automatically failing worker lifecycle or readiness. Configkit does not
  provide a Workerkit-specific command package.

## Package `otel`

The `otel` package provides a first-party OpenTelemetry observer. It is
optional at the package level: the root `configkit` package does not import or
compile against OpenTelemetry, and applications only compile `otel` when they
import it. OpenTelemetry remains in Configkit's module graph because the
adapter shares the root module. See the README's
[dependency model](../README.md#dependency-model).

### Observer

- `NewObserver(meter, tracer, opts...)`

  Creates a Configkit observer that records OpenTelemetry metrics and spans. If
  `meter` or `tracer` is nil, the corresponding no-op provider is used.

- `Option`

  OpenTelemetry observer configuration hook.

- `WithSourceName()`

  Includes `SourceMetadata.Name` as a metric and span attribute. Source names
  are excluded by default to avoid accidental cardinality growth.

### Metrics

The observer records:

- `configkit.load.started`
- `configkit.load.completed`
- `configkit.load.failed`
- `configkit.load.duration`
- `configkit.apply.published`
- `configkit.apply.changed`

### Spans

The observer creates:

- `configkit.load`
- `configkit.apply`

Load spans are created when completion events arrive and use attempt timestamps
when available. Apply spans are short-lived and end immediately.

These are retrospective lifecycle spans created from emitted events. The
observer does not wrap pipeline execution and does not create parent spans
around source reads, decoders, default appliers, validators, copiers,
redactors, or checksum functions. Application-provided source and pipeline
functions may create their own spans when they need execution-level tracing.

### Attributes and safety

Default attributes are intentionally low-cardinality:

- `configkit.event`
- `configkit.attempt.kind`
- `configkit.attempt.status`
- `configkit.attempt.stage`
- `configkit.failure.code` (failed attempts only)
- `configkit.source.kind`
- `configkit.apply.changed`

The observer does not record revisions, checksums, raw config data, redacted
config data, or typed config values. Source kind, optional source name, attempt
stage/status, and public failure code may be recorded as attributes. A public
failure message may be recorded as failed-span status/error data.

### Stable failure codes

Configkit exports constants for the public codes it emits. The normal stage
codes are `source_read_failed`, `pipeline_validate_failed`, `decode_failed`,
`defaults_failed`, `validate_config_failed`, `copy_failed`, `redact_failed`, and
`checksum_failed`. Context cancellation and deadline codes are `canceled` and
`deadline_exceeded`; context, missing-source, and general fallback codes are
`context_failed`, `missing_source`, `load_failed`, and `reload_failed`.

## Suggested reading order

If you are new to the codebase:

1. [README](../README.md)
2. [Getting Started](getting-started.md)
3. [Usage Guide](usage.md)
4. [Lifecycle](lifecycle.md)
5. [Reloads](reloads.md)
6. [Commands](commands.md)
7. [Operational Safety](operational-safety.md)
8. [Observability](observability.md)
9. [Composition](composition.md)
10. [Examples Guide](examples.md)
11. API Map
12. [Advanced Guide](advanced.md)
