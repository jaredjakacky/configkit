# Observability

Configkit emits backend-neutral lifecycle events from the root package. HTTP,
Servekit, Workerkit, and OpenTelemetry are not imported or compiled by the root
`configkit` package for configuration observability. Optional adapter packages
live in this same Go module, so adapter dependencies may appear in `go.mod`,
but applications only compile adapter packages they import.

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
behavior and can deadlock. Read-only calls such as `Status`, `Inspect`,
`Snapshot`, and `Value` are acceptable because they do not start another
lifecycle operation.

Use `AsyncObserver` or hand work off to another goroutine for follow-up work
that may block, call external systems, or trigger another load/apply operation.

## Events

`Event` is operational data. It does not expose typed config values.

Events can include:

- event kind
- attempt ID
- attempt kind
- source metadata
- revision
- attempt record
- snapshot metadata
- apply result
- event time

Source metadata, revisions, checksums, and error strings should be safe for the
observer audience.

## Structured Logs

`SlogObserver` maps Configkit events to `log/slog` records:

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

manager := configkit.NewManager[AppConfig](
	configkit.WithObservers(configkit.SlogObserver(logger)),
)
```

It logs lifecycle metadata only. It does not log typed config values or redacted
fields.

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
- `configkit.attempt.kind`
- `configkit.attempt.status`
- `configkit.attempt.stage`
- `configkit.source.kind`
- `configkit.apply.changed`

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

When the kits are composed, their telemetry should complement each other
without mixing responsibilities.

## Examples

- [`examples/06-observability-slog`](../examples/06-observability-slog)
- [`examples/10-production-composition`](../examples/10-production-composition)
