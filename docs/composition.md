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

The root `configkit` package imports Opskit because `Manager[T]` directly
implements Opskit component, readiness, and inspection contracts. It does not
import or compile against Servekit, Workerkit, or OpenTelemetry.
Configkit-specific adapter packages remain available for specialized HTTP and
telemetry behavior. Generic cross-kit composition uses Opskit contracts. See
the README's [dependency model](../README.md#dependency-model) for why optional
adapter and example dependencies remain in Configkit's module graph without
entering the root package's build.

## Opskit-First Servekit Composition

For Kit Series services, register the Configkit manager with Opskit and let
Servekit consume the registry:

```go
ops := opskit.NewRegistry()

manager := configkit.NewManager[AppConfig](
	configkit.WithIdentity("config"),
)

ops.MustRegister(manager, opskit.Required())

server := servekit.New(
	servekit.WithOps(ops, servekit.WithOpsAdmin()),
)
```

Servekit readiness now includes Configkit readiness through Opskit. When admin
routes are enabled, `/admin/components/config` exposes the manager's Opskit
component snapshot. The registry preserves the Configkit parent identity and
registration policy; the manager reports only its authoritative aggregate
readiness because it has no child readiness domain.

Default Configkit readiness policy:

- `unloaded`: not ready
- `failed`: not ready
- `loaded`: ready
- `degraded`: ready

`degraded` is ready by default because a valid last-known-good snapshot remains
active. Use `configkit.WithDegradedReady(false)` when constructing the manager
for stricter services.

## Specialized Configkit HTTP Inspection

Use `configkit/opshttp` when operators need Configkit-specific HTTP inspection
in addition to, or instead of, generic Opskit component snapshots:

```go
err := opshttp.Mount(server, manager,
	opshttp.WithEndpointOptions(
		servekit.WithAuthGate(requireAdmin),
	),
)
```

Default specialized routes:

- `GET /admin/config`
- `GET /admin/config/attempts`

The routes are read-only. They do not expose typed config values and they do not
trigger reloads.

Read-only does not mean public. LifecycleInspection and attempts can include metadata,
revisions, checksums, redacted values, and public failure detail. Protect them
with Servekit endpoint policy when needed.

`opshttp.ReadinessCheck` remains available as standalone Servekit support for
services that are not using an Opskit registry:

```go
server := servekit.New(
	servekit.WithReadinessChecks(opshttp.ReadinessCheck(manager)),
)
```

## Reload Commands

Root Configkit exposes reload as an Opskit command handler:

```go
reload := configkit.ReloadCommand(manager, source, pipeline)
ops.MustRegister(reload, opskit.Informational())
```

Use Workerkit's generic Opskit command adapter when that reload command should
be dispatched through a Workerkit runtime:

```go
descriptors := reload.Commands(ctx)
if len(descriptors) != 1 {
	return fmt.Errorf("reload command descriptors = %d, want 1", len(descriptors))
}

err := runtime.Register(workerkit.WorkerSpec{
	Name:   "config",
	Worker: configWorker{},
}, workerkit.WithCommandSpec(
	workerkit.CommandFromOpskit(descriptors[0], reload),
))
```

Workerkit runtimes implement Opskit component, readiness, and inspection
contracts directly. Register the runtime in the same Opskit registry as the
Configkit manager and reload command handler. Workerkit remains responsible for
dispatch, admission, timeouts, retries, concurrency, and observation.

Default command name:

```text
config/reload
```

The command calls:

```go
manager.LoadFromSource(ctx, configkit.AttemptKindReload, source, pipeline)
```

Successful reloads return completed command results. Admitted reload failures
return failed Opskit commands with safe public failure detail. Workerkit maps
those results into its normal command failure, observation, and configured retry
path without automatically failing the worker lifecycle. Configkit independently
preserves the last-known-good snapshot, reports degraded manager state, and
remains ready by default while that snapshot is active.

## Full Production Shape

A typical composed service has:

- Configkit manager for typed config
- Opskit registry containing the Configkit manager and Workerkit runtime
- Servekit server for app routes, admin routes, auth, response encoding, and readiness
- Workerkit runtime for operational commands
- `servekit.WithOps` for shared readiness and generic component inspection
- optional `opshttp.Mount` for read-only Configkit-specific inspection
- `configkit.ReloadCommand` for the Opskit reload command
- `workerkit.CommandFromOpskit` for Workerkit dispatch
- app routes that read current config through `manager.Value()`

This keeps the ownership clear:

- application code owns the config type and business behavior
- Configkit owns config lifecycle mechanics
- Opskit owns shared operational registry shape
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
