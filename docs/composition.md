# Composition with Servekit and Workerkit

Configkit, Servekit, and Workerkit are separate kits with separate
responsibilities.

Configkit owns typed configuration lifecycle. Servekit owns HTTP service
semantics. Workerkit owns background runtime and command dispatch semantics.

## Responsibility Boundary

Configkit owns:

- source reads
- decoding
- defaults
- validation
- copy-before-publication
- redaction
- checksums
- snapshots
- status
- inspection
- attempt records
- last-known-good preservation
- observer events

Servekit owns:

- HTTP server construction
- route registration
- endpoint policy
- auth gates
- request limits
- response encoding
- readiness endpoints
- HTTP lifecycle

Workerkit owns:

- worker lifecycle
- command registration
- command dispatch
- command timeouts
- retries
- concurrency limits
- failure policy
- runtime and worker inspection

The root `configkit` package does not import or compile against Servekit,
Workerkit, or OpenTelemetry. Optional adapter packages connect the kits where
the integration is common operational plumbing. Those adapter packages live in
this same Go module, so their dependencies may appear in `go.mod`; applications
only compile adapter packages they import.

## Read-Only Operations with Servekit

Use `configkit/opshttp` when operators need HTTP inspection:

```go
err := opshttp.Mount(server, manager,
	opshttp.WithEndpointOptions(
		servekit.WithAuthGate(requireAdmin),
	),
)
```

Default routes:

- `GET /admin/config`
- `GET /admin/config/attempts`

The routes are read-only. They do not expose typed config values and they do not
trigger reloads.

Read-only does not mean public. Inspection and attempts can include metadata,
revisions, checksums, redacted values, and error strings. Protect them with
Servekit endpoint policy when needed.

## Readiness with Servekit

Use `opshttp.ReadinessCheck` to connect Configkit status to Servekit readiness:

```go
server := servekit.New(
	servekit.WithReadinessChecks(opshttp.ReadinessCheck(manager)),
)
```

Default policy:

- `unloaded`: not ready
- `failed`: not ready
- `loaded`: ready
- `degraded`: ready

`degraded` is ready by default because a valid last-known-good snapshot remains
active. Use `opshttp.WithDegradedReady(false)` for stricter services.

## Reload Commands with Workerkit

Use `configkit/worker` when reload should be exposed as a Workerkit command:

```go
err := runtime.Register(workerkit.WorkerSpec{
	Name:   "config",
	Worker: configWorker{},
}, workerkit.WithCommandSpec(
	configworker.ReloadCommand(manager, source, pipeline),
))
```

Default command name:

```text
config/reload
```

The command calls:

```go
manager.LoadFromSource(ctx, configkit.AttemptKindReload, source, pipeline)
```

Failed reloads return a successful Workerkit command result containing failure
metadata. That preserves the command payload and lets Configkit report degraded
state without treating every failed reload as a Workerkit dispatch failure.

## Full Production Shape

A typical composed service has:

- Configkit manager for typed config
- Servekit server for app routes, admin routes, auth, response encoding, and readiness
- Workerkit runtime for operational commands
- `opshttp.Mount` for read-only Configkit inspection
- `opshttp.ReadinessCheck` for readiness
- `worker.ReloadCommand` for reload
- app routes that read current config through `manager.Value()`

This keeps the ownership clear:

- application code owns the config type and business behavior
- Configkit owns config lifecycle mechanics
- Servekit owns HTTP policy
- Workerkit owns command dispatch

## What Not to Build in Configkit

Configkit should not own:

- HTTP policy
- polling
- file watching
- client rebuilding
- dependency readiness
- durable state
- secrets management
- feature flags
- deployment policy

Those systems can exist around Configkit. They should connect through sources,
providers, inspectors, observers, or optional adapter packages.

## Examples

- [`examples/07-servekit-opshttp`](../examples/07-servekit-opshttp)
- [`examples/08-servekit-readiness`](../examples/08-servekit-readiness)
- [`examples/09-workerkit-reload-command`](../examples/09-workerkit-reload-command)
- [`examples/10-production-composition`](../examples/10-production-composition)
