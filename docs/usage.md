# Usage Guide

This guide covers the normal Configkit path: choose a source, build a pipeline,
create a manager, load initial configuration, read the current value, inspect
status, and reload when needed.

For symbol-level details, see [`api.md`](api.md).

## The Normal Path

Most Configkit programs follow this shape:

1. Define an application config type.
2. Choose or implement a `Source`.
3. Build a `Pipeline[T]`.
4. Create one `Manager[T]`.
5. Load initial config with `Manager.LoadFromSource`.
6. Read current config with `Value` or `Snapshot`.
7. Use status, inspection, attempts, and observers for operations.
8. Reload through application-owned triggers.

```go
manager := configkit.NewManager[AppConfig]()

result, err := manager.LoadFromSource(ctx, configkit.AttemptKindInitialLoad, source, pipeline)
if err != nil {
	return err
}

cfg, ok := manager.Value()
if !ok {
	return errors.New("config not loaded")
}

fmt.Println(cfg.ServiceName, result.Apply.Changed)
```

## Manager

A manager represents one configuration lifecycle boundary. It owns the current
snapshot, attempt history, lifecycle status, inspection output, and observer
notifications.

Use one manager for one logical application configuration value. Use multiple
managers only when the values have separate lifecycle, reload, inspection, or
readiness semantics.

## Sources

A source reads raw configuration data:

```go
type Source interface {
	Metadata() SourceMetadata
	Read(context.Context) (SourceData, error)
}
```

Built-in sources:

- `BytesSource` for already-available bytes
- `FileSource` for local files

Custom sources can read from environment-derived data, remote APIs, databases,
Kubernetes, Consul, SSM, Vault, or any other backend. Keep source metadata safe:
it can appear in logs, status, inspection, telemetry, and adapter output.

## Pipeline

A pipeline turns raw source data into a publishable snapshot.

Required steps:

- `Decode`
- `Redact`
- `Checksum`

Optional steps:

- `ApplyDefaults`
- `ValidateConfig`
- `Copy`

`ApplyDefaults` should fill mechanical defaults. `ValidateConfig` should reject
invalid configuration. `Copy` should detach mutable references before
publication when `T` contains maps, slices, pointers, or other shared state.

## Reading Current Config

Use `Value` when application code needs the typed value:

```go
cfg, ok := manager.Value()
if !ok {
	return errors.New("config unavailable")
}
```

Use `Snapshot` when code also needs metadata:

```go
snapshot, ok := manager.Snapshot()
if ok {
	metadata := snapshot.Metadata()
	fmt.Println(metadata.Checksum)
}
```

Treat returned config values as immutable. If `T` contains mutable references,
use `Pipeline.Copy` or application-level discipline to avoid mutating snapshot
state after publication.

## Mutability Contract

Configkit treats snapshots as immutable by convention. It does not deep-freeze
arbitrary Go values.

`Manager.Value` and `Snapshot.Value` return `T` by normal Go assignment rules.
For config structs made of strings, numbers, booleans, durations, and other
scalar values, that is usually exactly what you want. If `T` contains maps,
slices, pointers, or other mutable references, a caller that mutates the
returned value can mutate data reachable from the current snapshot.

Prefer scalar or naturally immutable config shapes where practical. When a
config type needs mutable reference fields, use `Pipeline.Copy` to detach them
before publication and treat returned config values as read-only.

```go
type AppConfig struct {
	ServiceName string            `json:"service_name"`
	Headers     map[string]string `json:"headers"`
}

pipeline := configkit.Pipeline[AppConfig]{
	Decode: configkit.JSONDecoder[AppConfig](),
	Copy: func(ctx context.Context, cfg AppConfig) (AppConfig, error) {
		cfg.Headers = maps.Clone(cfg.Headers)
		return cfg, nil
	},
	Redact:   configkit.EmptyRedactor[AppConfig](),
	Checksum: configkit.SHA256JSONChecksum[AppConfig](),
}
```

`Pipeline.Copy` detaches the value before it is published. It does not stop
later callers from mutating maps, slices, or pointers returned by
`Manager.Value` or `Snapshot.Value`, so application code should treat those
returned values as read-only.

## Status and Inspection

`Status` answers lifecycle questions:

```go
status := manager.Status()
fmt.Println(status.State)
```

States:

- `unloaded`
- `failed`
- `loaded`
- `degraded`

`Inspect` returns status plus the current redacted view:

```go
inspection := manager.Inspect()
```

Inspection does not expose the typed config value. Its redacted data is only as
safe as the application redactor.

## Reloads

Use the same manager and pipeline for reloads:

```go
result, err := manager.LoadFromSource(ctx, configkit.AttemptKindReload, source, pipeline)
```

Successful reloads publish fresh snapshots. `ApplyResult.Changed` reports
whether the effective checksum changed. Failed reloads preserve the
last-known-good snapshot when one exists.

Read [`reloads.md`](reloads.md) for the detailed model.

## Observers

Observers receive lifecycle events:

- `load_started`
- `load_succeeded`
- `load_failed`
- `snapshot_applied`

Use `SlogObserver` for structured logs, `AsyncObserver` when delivery should
not block loads, and `otel.NewObserver` for OpenTelemetry.

## Adapters

Configkit core stays transport-neutral.
The root `configkit` package does not import or compile against Servekit,
Workerkit, or OpenTelemetry. The adapter packages live in this same Go module,
so their dependencies may appear in `go.mod`; applications only compile adapter
packages they import.

Use `configkit/opshttp` when Servekit should expose read-only inspection,
recent attempts, or readiness.

Use `configkit/worker` when Workerkit should expose a reload command.

## Common Options

Manager options:

- `WithObservers`
- `WithAttemptHistoryLimit`

Ops HTTP options:

- `opshttp.WithPathPrefix`
- `opshttp.WithEndpointOptions`
- `opshttp.WithDegradedReady`

Worker reload command options:

- `worker.WithCommandName`
- `worker.WithDescription`

OTel observer options:

- `otel.WithSourceName`

## Related Material

- [`lifecycle.md`](lifecycle.md)
- [`reloads.md`](reloads.md)
- [`operational-safety.md`](operational-safety.md)
- [`composition.md`](composition.md)
