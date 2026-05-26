# Advanced Guide

Most Configkit users should start with [`getting-started.md`](getting-started.md),
[`usage.md`](usage.md), [`lifecycle.md`](lifecycle.md), and
[`reloads.md`](reloads.md). This guide covers customization points that are
useful after the normal path is clear.

## When You Are in Advanced Territory

You are probably in advanced Configkit territory when:

- one service has several independently reloaded config values
- source data is merged from multiple backends
- a config value contains mutable references that need defensive copying
- checksums need custom canonicalization
- redacted views need to match existing operational contracts
- loads and publication need to be separated with `Load` and `Manager.Apply`
- observers feed several backends
- Servekit and Workerkit adapters are composed into a production service

## Custom Sources

Implement `Source` when configuration bytes come from somewhere other than
`BytesSource` or `FileSource`:

```go
type envSource struct{}

func (envSource) Metadata() configkit.SourceMetadata {
	return configkit.SourceMetadata{Name: "env", Kind: "env"}
}

func (envSource) Read(ctx context.Context) (configkit.SourceData, error) {
	return configkit.SourceData{
		Data:     []byte(os.Getenv("APP_CONFIG_JSON")),
		Metadata: configkit.SourceMetadata{Name: "env", Kind: "env"},
		Revision: "process-env",
	}, nil
}
```

Keep source names and revisions safe for operational output.

## Custom Decoders

Use a custom `Decoder[T]` for non-JSON formats, merged sources, strict parsing,
decryption, or source-aware decode behavior.

Decoders receive `SourceData`, so they can inspect source metadata or revision
when that is part of the application contract.

## Custom Checksums

Use a custom `Checksummer[T]` when `SHA256JSONChecksum` is not the right
fingerprint.

Common reasons:

- exclude operationally sensitive fields
- canonicalize maps or unordered data
- reuse a backend-provided version
- match an existing deployment fingerprint
- avoid exposing fingerprints for low-entropy secret-bearing config

## Custom Redaction

Redactors should be conservative. Prefer fields that answer operational
questions without exposing raw values:

- configured/not configured
- count
- mode
- safe enum
- safe endpoint host
- safe feature summary

Avoid returning masked secret values unless that masked form has been reviewed
as safe for the target audience.

## Copy Before Publication

Use `Pipeline.Copy` when the typed config contains mutable references:

```go
type AppConfig struct {
	ServiceName string            `json:"service_name"`
	Headers     map[string]string `json:"headers"`
}

Copy: func(ctx context.Context, cfg AppConfig) (AppConfig, error) {
	cfg.Headers = maps.Clone(cfg.Headers)
	return cfg, nil
}
```

`Pipeline.Copy` detaches the value before publication. That protects the
published snapshot from mutable references shared with decode, defaults,
validation, or other earlier pipeline stages.

This is still a Go value, not a frozen object. `Snapshot.Value` and
`Manager.Value` return `T` by normal Go assignment rules. If `T` contains maps,
slices, pointers, or other mutable references, callers can mutate reachable data
after reading the value. `Pipeline.Copy` does not prevent that later mutation.

Prefer scalar or naturally immutable config shapes where possible. If mutable
reference fields are necessary, provide `Pipeline.Copy` and treat values
returned by `Value` or `Snapshot.Value` as read-only. Configkit protects
snapshot metadata and the top-level redacted map, but it cannot enforce deep
immutability for arbitrary application values.

## Separating Load from Apply

Use package-level `Load` or `LoadFromSource` when you need to run the lifecycle
without immediately mutating manager state:

```go
loadResult, err := configkit.LoadFromSource(ctx, configkit.AttemptKindReload, source, pipeline)
if err != nil {
	// inspect or decide before applying
}

applyResult, applyErr := manager.Apply(ctx, loadResult)
```

`Manager.Apply` validates the `LoadResult`, assigns a fresh manager-local
attempt ID, and mutates manager state only when the result is internally
consistent.

For observability, external apply is intentionally narrower than
manager-owned load. `Manager.Apply` emits `snapshot_applied` when a successful
snapshot is published, but it does not emit `load_started`, `load_succeeded`,
or `load_failed` because it did not perform the load lifecycle.

## Multiple Managers

Use multiple managers only when the config values have separate lifecycle
ownership.

Good reasons:

- separate reload triggers
- separate readiness semantics
- separate operational inspection surfaces
- separate ownership by different service components

Avoid multiple managers for fields that are really one atomic application
configuration value. A single snapshot gives readers a coherent view.

## Async Observers

Observers run synchronously unless wrapped.

Use `AsyncObserver` for observers that may block:

```go
async := configkit.NewAsyncObserver(observer, configkit.WithAsyncObserverBuffer(128))
defer async.Close(context.Background())
```

Pick buffer sizes deliberately. A larger buffer absorbs short bursts but uses
more memory and can delay visibility. A full or closed observer drops new
events and increments `Dropped`.

## Strict Readiness

`opshttp.ReadinessCheck` treats degraded as ready by default because the
last-known-good snapshot remains active.

Use strict readiness when any failed reload should remove the service from
traffic:

```go
servekit.WithReadinessChecks(
	opshttp.ReadinessCheck(manager, opshttp.WithDegradedReady(false)),
)
```

## Adapter Boundaries

Use adapters where they remove repeated operational glue:

- `configkit/opshttp` for Servekit inspection and readiness
- `configkit/worker` for Workerkit reload commands
- `configkit/otel` for OpenTelemetry observers

The root `configkit` package does not import or compile against Servekit,
Workerkit, or OpenTelemetry. These adapter packages live in the same Go module,
so their dependencies may appear in `go.mod`, but applications only compile
adapter packages they import.

Do not push application policy into Configkit core. HTTP auth belongs to
Servekit. Command dispatch belongs to Workerkit. Backend-specific source
ownership belongs to the application or a dedicated adapter.

## Backend-Specific Sources

Custom `Source` implementations can connect Configkit to SSM, Vault,
Kubernetes, Consul, etcd, databases, remote APIs, or other configuration
backends. These sources should usually live in application code or dedicated
adapter packages so backend behavior can follow the deployment's policy.

The root `configkit` package should not own backend auth policy, leases,
polling, watching, rollout behavior, client lifecycle, or backend-specific
operational policy. First-party source adapters, if ever added, should preserve
the `Source` boundary and remain policy-light: read raw configuration bytes,
return safe source metadata, and leave backend-specific decisions to the
application or adapter.

## Recommended Advanced Sequence

1. Build the service with `Manager.LoadFromSource`.
2. Add explicit validation and conservative redaction.
3. Add reload behavior and inspect degraded state.
4. Add `SlogObserver`.
5. Add custom copy or checksum only when the config shape requires it.
6. Add `opshttp` for protected read-only inspection.
7. Add `worker.ReloadCommand` if operators need command-driven reload.
8. Add OpenTelemetry when the service telemetry backend is ready.

## Related Material

- [`operational-safety.md`](operational-safety.md)
- [`observability.md`](observability.md)
- [`composition.md`](composition.md)
- [`examples/10-production-composition`](../examples/10-production-composition)
