# Reloads

Reloads are where Configkit becomes a lifecycle package instead of only a
decoder wrapper.

Configkit records every manager-owned load attempt, publishes successful
snapshots, and preserves the last-known-good snapshot when a later reload fails.

## Initial Load Versus Reload

Use `AttemptKindInitialLoad` for the first load:

```go
result, err := manager.LoadFromSource(ctx, configkit.AttemptKindInitialLoad, source, pipeline)
```

Use `AttemptKindReload` for later attempts:

```go
result, err := manager.LoadFromSource(ctx, configkit.AttemptKindReload, source, pipeline)
```

The attempt kind is operational metadata. It does not change the pipeline
itself.

## Failed Initial Load

If the first load fails, there is no current snapshot.

The manager records the failure and enters `failed` state:

```text
state: failed
current snapshot: none
last failure: recorded
```

Application code should treat this as configuration unavailable.

## Failed Reload

If a reload fails after a valid snapshot exists, Configkit preserves the current
snapshot.

The manager records the failure and enters `degraded` state:

```text
state: degraded
current snapshot: previous last-known-good snapshot
last failure: recorded
```

Application code can continue reading `Value` while operators inspect the
failed attempt.

## Successful Reload

Successful reloads publish fresh snapshots:

```go
result, err := manager.LoadFromSource(ctx, configkit.AttemptKindReload, source, pipeline)
if err != nil {
	return err
}

fmt.Println(result.Apply.Published)
fmt.Println(result.Apply.Changed)
```

`Published` means a snapshot became current.

`Changed` means the effective checksum differs from the previous current
snapshot. Published snapshots have non-empty checksums, although Configkit does
not prescribe their format or attempt to detect collisions in custom checksum
implementations.

A reload can publish with `Changed=false` when the input was valid but the
effective config checksum did not change.

A Checksummer returning an empty string with no error does not mean
"unchanged." It fails at the checksum stage, records the safe
`checksum_failed` failure, and preserves the last-known-good snapshot.

## Attempt Records

`AttemptRecord` captures operational data for one load or reload:

- manager-local ID
- kind
- status
- failed stage
- source metadata
- revision
- checksum
- start and end timestamps
- public failure code and message

Manager-owned attempts receive fresh manager-local IDs. Package-level `Load`
and `LoadFromSource` may leave IDs zero.

A Manager call canceled before it gains serialized lifecycle admission is not
an attempt. It receives no ID and does not emit events, enter history, or change
last attempt, last failure, degraded state, or the current snapshot.

## Attempt History

`Manager.Attempts()` returns retained attempts ordered oldest to newest.

The default history limit is `20`. Configure it with:

```go
manager := configkit.NewManager[AppConfig](
	configkit.WithAttemptHistoryLimit(50),
)
```

A limit less than or equal to zero disables attempt history while preserving
`LastAttempt`, `LastSuccess`, and `LastFailure` in status.

## ApplyResult

`ApplyResult` describes publication:

- `Published`: a successful snapshot became current
- `Changed`: the current checksum differs from the previous checksum
- `Previous`: previous snapshot metadata, if any
- `Current`: current snapshot metadata, if any
- `AppliedAt`: UTC time when the manager committed this result

`AppliedAt` is set for every accepted apply. A failed result does not publish a
snapshot, but it still updates attempt history and lifecycle state. Snapshot
`LoadedAt` and attempt `EndedAt` remain the historical load times and can be
much earlier for an externally loaded result.

Use it when operators or reload commands need to distinguish "reload succeeded"
from "effective config changed."

## External Load and Apply

Package-level `Load` and `LoadFromSource` are stateless. They return
`LoadResult[T]` and do not mutate manager state.

Use `Manager.Apply` when a caller intentionally separates load execution from
publication:

```go
loadResult, err := configkit.LoadFromSource(ctx, configkit.AttemptKindReload, source, pipeline)
applyResult, applyErr := manager.Apply(ctx, loadResult)
```

`Manager.Apply` validates the `LoadResult` before mutation. It rejects
malformed results. A successful attempt must include a snapshot, matching source
and revision, matching non-empty checksums, and no failure stage or detail. A
failed attempt must include no snapshot or checksum and must include a failure
stage plus non-zero safe public failure detail.

Delayed and out-of-order application remains legal. Configkit does not infer
staleness from timestamps or impose source revision policy. For externally
loaded results, manager-local attempt IDs and retained history reflect apply
order rather than load completion order.

Waiting for `Manager.Apply` admission is context-aware. Cancellation before
admission returns the context error without validation, an attempt ID, status
mutation, observer notification, or publication. Once admitted, Apply is not
rolled back by later cancellation.

## Reload Triggers Are Application-Owned

Configkit core does not poll, watch files, schedule reloads, expose HTTP reload
routes, or rebuild clients.

Common trigger owners:

- application startup code for initial load
- Opskit commands for operator-triggered reload
- Workerkit command dispatch for runtime execution
- application-specific signal handlers
- deployment hooks
- custom source watchers outside Configkit core

Use root `configkit.ReloadCommand` to expose reload through Opskit's command
contracts. Use `workerkit.CommandFromOpskit` when Workerkit should dispatch that
reload as an operational command.

See [`commands.md`](commands.md) for the complete Opskit and Workerkit
composition path.

## Examples

- [`examples/04-failed-reload`](../examples/04-failed-reload)
- [`examples/05-changed-detection`](../examples/05-changed-detection)
- [`examples/09-workerkit-reload-command`](../examples/09-workerkit-reload-command)
