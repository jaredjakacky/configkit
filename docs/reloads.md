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
snapshot.

A reload can publish with `Changed=false` when the input was valid but the
effective config checksum did not change.

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
- error string on failure

Manager-owned attempts receive fresh manager-local IDs. Package-level `Load`
and `LoadFromSource` may leave IDs zero.

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
malformed results, such as a successful attempt with no snapshot or a failed
attempt with a snapshot.

## Reload Triggers Are Application-Owned

Configkit core does not poll, watch files, schedule reloads, expose HTTP reload
routes, or rebuild clients.

Common trigger owners:

- application startup code for initial load
- Workerkit commands for operator-triggered reload
- application-specific signal handlers
- deployment hooks
- custom source watchers outside Configkit core

Use `configkit/worker` when Workerkit should expose reload as an operational
command.

## Examples

- [`examples/04-failed-reload`](../examples/04-failed-reload)
- [`examples/05-changed-detection`](../examples/05-changed-detection)
- [`examples/09-workerkit-reload-command`](../examples/09-workerkit-reload-command)
