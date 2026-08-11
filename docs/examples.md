# Examples Guide

Configkit's examples are a guided product tour, not a random pile of demos.
Start with the smallest useful JSON load, then move outward into file sources,
redaction, reload semantics, observability, Servekit operations, Workerkit
execution of Opskit reload commands, and full Kit Series composition.

If you want the short directory index instead, use
[Configkit Examples](../examples/README.md).

Run examples from the repository root:

```bash
go run ./examples/<name>
```

## Start here

### [`examples/01-basic-json`](../examples/01-basic-json)

**What it demonstrates:** The smallest useful Configkit program: `BytesSource`,
`Pipeline`, `JSONDecoder`, defaults, validation, `EmptyRedactor`,
`SHA256JSONChecksum`, `NewManager`, `LoadFromSource`, `Value`, and `LifecycleStatus`.

**Run it:**

```bash
go run ./examples/01-basic-json
```

**Expected output:** The loaded typed config value, manager state `loaded`, and
the current checksum.

**What to notice:** Configkit is not a framework. It wraps ordinary Go config
with lifecycle mechanics.

**What this example intentionally does not show:** Files, reloads, observers,
Servekit, Workerkit, OpenTelemetry, or custom redaction.

### [`examples/02-file-source`](../examples/02-file-source)

**What it demonstrates:** Loading typed config from a local JSON file with safe
source metadata, file-byte revision, snapshot checksum, and load-completion
timestamp.

**Run it:**

```bash
go run ./examples/02-file-source
```

**Expected output:** The typed config, source kind/name, source revision,
snapshot checksum, and snapshot `loaded_at` timestamp. This is when the load
produced the snapshot, not a separate manager publication timestamp.

**What to notice:** `FileSource` revisions are SHA-256 fingerprints of file
bytes. Source metadata should remain safe for operational output.

**What this example intentionally does not show:** Reloads, observers, Servekit,
Workerkit, or custom redaction.

### [`examples/03-redaction-inspection`](../examples/03-redaction-inspection)

**What it demonstrates:** Typed config can contain sensitive values while
`Inspect` exposes only an application-owned redacted view.

**Run it:**

```bash
go run ./examples/03-redaction-inspection
```

**Expected output:** Manager status and a redacted inspection view that reports
whether the API key is configured without printing the key.

**What to notice:** Redacted view safety is application-owned. Configkit does
not inspect or verify whether redacted values are safe.

**What this example intentionally does not show:** Reloads, observers, HTTP,
Workerkit, or OpenTelemetry.

## Reload behavior

### [`examples/04-failed-reload`](../examples/04-failed-reload)

**What it demonstrates:** Failed reloads preserve the last-known-good snapshot.

**Run it:**

```bash
go run ./examples/04-failed-reload
```

**Expected output:** Initial load succeeds, reload fails validation, manager
state becomes `degraded`, last failure is recorded, and current config remains
the original valid config.

**What to notice:** Failed reloads do not publish snapshots. The active typed
value remains the last-known-good snapshot.

**What this example intentionally does not show:** Observers, Servekit,
Workerkit, files, or OpenTelemetry.

### [`examples/05-changed-detection`](../examples/05-changed-detection)

**What it demonstrates:** `ApplyResult.Published` versus `ApplyResult.Changed`.

**Run it:**

```bash
go run ./examples/05-changed-detection
```

**Expected output:** Initial load publishes and changes, identical reload
publishes without changing, changed reload publishes and changes.

**What to notice:** Successful reloads publish fresh snapshots even when the
effective checksum is unchanged.

**What this example intentionally does not show:** HTTP, Workerkit, observers,
files, or custom source behavior.

## Observability

### [`examples/06-observability-slog`](../examples/06-observability-slog)

**What it demonstrates:** Configkit lifecycle events mapped into structured
`slog` output.

**Run it:**

```bash
go run ./examples/06-observability-slog
```

**Expected output:** Log records with `component_name=search-config` and
event-time manager state for a successful initial load, failed reload, and
final degraded manager state.

**What to notice:** `SlogObserver` logs lifecycle metadata, not raw typed config
values or redacted fields.

**What this example intentionally does not show:** Servekit, Workerkit,
OpenTelemetry, files, or custom async observer behavior.

## Servekit integration

### [`examples/07-servekit-opshttp`](../examples/07-servekit-opshttp)

**What it demonstrates:** Configkit read-only operations routes mounted into
Servekit through `configkit/opshttp`.

**Run it:**

```bash
go run ./examples/07-servekit-opshttp
```

**Expected output:** Startup notes showing `/hello`, `/admin/config`, and
`/admin/config/attempts`. Admin routes require `X-Admin-Token: demo`.

**What to notice:** Servekit owns route policy and auth. Configkit exposes safe
inspection and attempts without exposing typed config values.

**What this example intentionally does not show:** Workerkit reload commands,
readiness-specific behavior, or OpenTelemetry.

### [`examples/08-servekit-readiness`](../examples/08-servekit-readiness)

**What it demonstrates:** Configkit Manager registered with Opskit and exposed
through Servekit readiness.

**Run it:**

```bash
go run ./examples/08-servekit-readiness
```

**Expected output:** Readiness is not ready before initial load, ready after
successful load, ready by default after a failed reload with last-known-good
active, and not ready with strict degraded readiness.

**What to notice:** Servekit reads Configkit readiness through Opskit.
`degraded` is ready by default because the active config is still valid.

**What this example intentionally does not show:** Ops routes, Workerkit,
observers, files, or OpenTelemetry.

## Workerkit integration

### [`examples/09-workerkit-reload-command`](../examples/09-workerkit-reload-command)

**What it demonstrates:** Configkit's root Opskit reload handler executed as a
Workerkit command through `workerkit.CommandFromOpskit`.

**Run it:**

```bash
go run ./examples/09-workerkit-reload-command
```

**Expected output:** Initial load runs directly through Configkit. A successful
Workerkit `config/reload` returns a payload. A failed reload returns a typed
Workerkit command error with safe failure detail while Configkit preserves the
last-known-good snapshot and reports degraded manager state.

**What to notice:** Workerkit's generic Opskit adapter owns command translation
and dispatch, failure observation, and configured retry. Configkit owns reload,
apply, last-known-good, and manager readiness semantics. A command failure does
not automatically fail Workerkit lifecycle or readiness.

**What this example intentionally does not show:** Servekit, HTTP, files,
polling, watching, or OpenTelemetry.

## Full composition

### [`examples/10-production-composition`](../examples/10-production-composition)

**What it demonstrates:** Configkit, Servekit, and Workerkit composed into one
production-style service.

**Run it:**

```bash
go run ./examples/10-production-composition
```

**Expected output:** The service starts with valid config, `/message` uses typed
config, `/admin/config` exposes safe inspection, Workerkit reload applies a
changed config, a failed reload dispatch returns an HTTP command error, the
last-known-good config remains active, manager status becomes degraded, and
readiness remains ready by default.

**What to notice:** The kits snap together without blurring ownership.
Application code still owns domain config and business behavior.

**What this example intentionally does not show:** Secrets management, feature
flags, distributed rollout, polling, file watching, durable state, or client
rebuilding.

## Suggested reading

- [`getting-started.md`](getting-started.md)
- [`usage.md`](usage.md)
- [`lifecycle.md`](lifecycle.md)
- [`reloads.md`](reloads.md)
- [`composition.md`](composition.md)
