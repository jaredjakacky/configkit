# Observability

Configkit emits backend-neutral lifecycle events from the root package. HTTP,
Servekit, Workerkit, and OpenTelemetry are not imported or compiled by the root
`configkit` package for configuration observability. `configkit/otel` is an
optional package in the same module. See the README's
[dependency model](../README.md#dependency-model) for the distinction between
the package build graph and the module dependency graph.

## Observer Model

Attach observers with `WithObservers`:

```go
manager := configkit.NewManager[AppConfig](
	configkit.WithObservers(configkit.SlogObserver(logger)),
)
```

Observers receive events for:

- load started
- load succeeded
- load failed
- snapshot applied

Observer panics are recovered by the manager. Observers still run synchronously
by default, so they should return quickly.

A synchronous observer must not call `Load`, `LoadFromSource`, or `Apply` on
the same manager that emitted the event. That creates reentrant lifecycle
behavior and can deadlock. Read-only calls such as `LifecycleStatus`, `LifecycleInspection`,
`Snapshot`, and `Value` are acceptable because they do not start another
lifecycle operation.

Manager state is updated before `load_succeeded` or `load_failed` is delivered.
Synchronous completion observers therefore see `LifecycleStatus`,
`LifecycleInspection`, `Snapshot`, and `Value` consistent with the event they
are handling. Notifications run without holding the manager state lock.

Use `AsyncObserver` or hand work off to another goroutine for follow-up work
that may block, call external systems, or trigger another load/apply operation.

## Events

`Event` is operational data. It does not expose typed config values.

Events can include:

- event kind
- normalized Opskit component name
- manager state at the event boundary
- attempt ID
- attempt kind
- source metadata
- revision
- attempt record
- snapshot metadata
- apply result
- event time

Attempt IDs are manager-local. For manager-owned events, use
`(ComponentName, AttemptID)` as the correlation key. Give managers distinct
component names when they share an observer or telemetry backend. Configkit
does not generate a separate runtime manager UUID.

Manager-owned load events have this ordering and state contract:

| Event | Event state | Apply result | Synchronous manager reads |
| --- | --- | --- | --- |
| `load_started` | State before the attempt | None | Pre-attempt state |
| `load_succeeded` | Resulting state | Present | Published snapshot is current |
| `load_failed` | Resulting `failed` or `degraded` state | Present | Failure is recorded and last-known-good is retained |
| `snapshot_applied` | Post-publication state | Present | Published snapshot is current |

Successful manager-owned loads emit `load_started`, `load_succeeded`, then
`snapshot_applied`. Failed loads emit `load_started`, then `load_failed`.
Completion events are delivered after manager state mutation;
`snapshot_applied` follows the completion event to preserve lifecycle event
ordering.

Source metadata, revisions, checksums, and public failure detail should be safe
for the observer audience. Arbitrary returned error strings are not included.

## Structured Logs

`SlogObserver` maps Configkit events to `log/slog` records:

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

manager := configkit.NewManager[AppConfig](
	configkit.WithObservers(configkit.SlogObserver(logger)),
)
```

It logs lifecycle metadata only. It does not log typed config values or redacted
fields. Depending on the event, attributes include `event`, `component_name`,
`manager_state`, `attempt_id`, `attempt_kind`, `attempt_status`, source and
revision metadata, stage and safe failure detail, checksums, attempt timing,
and `published`/`changed` apply results.

## Async Delivery

Use `AsyncObserver` when an observer may block or when load/apply calls should
not wait for delivery:

```go
async := configkit.NewAsyncObserver(configkit.SlogObserver(logger))
defer async.Close(context.Background())

manager := configkit.NewManager[AppConfig](
	configkit.WithObservers(async.Observer()),
)
```

`Notify` is non-blocking. If the queue is full or closed, events are dropped
and counted by `Dropped`.

Queued events are cloned and are the authoritative event-time view. By the time
the wrapped observer runs, a live manager read may observe the same state or a
newer attempt. Delivery through one `AsyncObserver` preserves enqueue order for
events that are not dropped, but no global ordering is defined across managers.

`Close` stops new events and waits for queued events to drain. It cannot
preempt a wrapped observer that is blocked handling an event.

## OpenTelemetry

The optional `otel` package maps Configkit events into metrics and spans:

```go
observer, err := configotel.NewObserver(meter, tracer)
if err != nil {
	return err
}

manager := configkit.NewManager[AppConfig](
	configkit.WithObservers(observer),
)
```

The observer records:

- `configkit.load.started`
- `configkit.load.completed`
- `configkit.load.failed`
- `configkit.load.duration`
- `configkit.apply.published`
- `configkit.apply.changed`

It creates:

- `configkit.load`
- `configkit.apply`

Load spans are created when completion events arrive and use
`AttemptRecord.StartedAt` and `AttemptRecord.EndedAt` when available. Apply
spans are short-lived and end immediately.

These are retrospective lifecycle spans created from emitted Configkit events.
The observer does not wrap pipeline execution and does not create parent spans
around source reads, decoders, default appliers, validators, copiers,
redactors, or checksum functions. Application-provided source and pipeline
functions may create their own spans when they need execution-level tracing.

## Attribute Policy

Default OTel attributes are intentionally low-cardinality:

- `configkit.event`
- `configkit.component.name`
- `configkit.manager.state`
- `configkit.attempt.kind`
- `configkit.attempt.status`
- `configkit.attempt.stage`
- `configkit.source.kind`
- `configkit.apply.published`
- `configkit.apply.changed`

Spans also include `configkit.attempt.id`. Attempt IDs are excluded from metric
attributes because they are unbounded.

Source names are excluded by default. Use `otel.WithSourceName()` only when
source names are stable enough for telemetry and safe for that audience.

The OTel observer does not record revisions, checksums, raw config data,
redacted config data, or typed config values.

## Multiple Observers

Register more than one observer when one service should send lifecycle data to
several backends:

```go
manager := configkit.NewManager[AppConfig](
	configkit.WithObservers(
		configkit.SlogObserver(logger),
		otelObserver,
	),
)
```

Use `AsyncObserver` around slow observers rather than around observers that are
already cheap and synchronous.

## Servekit and Workerkit Boundaries

Servekit has HTTP observability: request IDs, access logs, middleware, route
timing, panic recovery, and HTTP spans.

Workerkit has runtime observability: worker lifecycle, command dispatch,
readiness, retries, saturation, and failure.

Configkit observability describes configuration lifecycle only: loads, reloads,
publication, status, and last-known-good behavior.

When a Workerkit-dispatched reload fails, Configkit emits its failed load
attempt and Workerkit records the failed command attempt and final dispatch
outcome. Workerkit applies any configured command retry policy. The returned
command error does not automatically fail worker lifecycle or readiness, while
Configkit independently reports degraded-but-ready state when a
last-known-good snapshot remains active.

When the kits are composed, their telemetry complements rather than
contradicts each other: Configkit describes the reload attempt, and Workerkit
describes command execution policy and outcome.

## Examples

- [`examples/06-observability-slog`](../examples/06-observability-slog)
- [`examples/10-production-composition`](../examples/10-production-composition)
