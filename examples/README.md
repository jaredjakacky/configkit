# Configkit Examples

Configkit is a typed configuration lifecycle shell for Go services. These examples are the fastest way to understand what that means in practice: starting from raw JSON bytes becoming a validated snapshot, ending with a production-style composition that includes safe inspection, last-known-good reload behavior, structured observability, Servekit ops routes, Servekit readiness, and a Workerkit reload command.

Every example is runnable, and every example is part of the public documentation. They are meant to teach the model, not just prove the code compiles.

If you want the fuller narrative walkthrough, start with the [Examples Guide](../docs/examples.md).

## Recommended Order

Read the examples in the order listed below. Each one builds on the last. By the end you will have a complete mental model of how typed configuration becomes production runtime state: source, pipeline, snapshot, manager, status, inspection, observability, HTTP ops, reload commands, and full Kit Series composition.

### Core Lifecycle

**[01-basic-json](01-basic-json)** — The smallest useful Configkit program. Raw JSON bytes become a typed, validated, checksummed snapshot owned by a manager. This is Configkit before files, reloads, HTTP, workers, or telemetry enter the picture.

**[02-file-source](02-file-source)** — Loading configuration from a local JSON file. This example focuses on `FileSource`, safe source metadata, source revisions, snapshot checksums, and `loaded_at` metadata.

**[03-redaction-inspection](03-redaction-inspection)** — Typed config can contain sensitive fields, but operational inspection should not. This example shows an application-owned `Redactor` and `Manager.Inspect()` returning only a safe `RedactedView`.

### Reload Semantics

**[04-failed-reload](04-failed-reload)** — Failed reloads preserve the last-known-good snapshot. A valid initial load succeeds, an invalid reload fails validation, manager state becomes `degraded`, and the current typed value remains the original valid config.

**[05-changed-detection](05-changed-detection)** — Publishing is not the same as changing. Successful reloads publish fresh snapshots, while `ApplyResult.Changed` reports whether the effective checksum actually changed.

### Observability

**[06-observability-slog](06-observability-slog)** — Configkit emits lifecycle observer events. This example maps them into production-friendly `slog` records for successful loads and failed reloads without logging raw typed config values.

### Servekit Integration

**[07-servekit-opshttp](07-servekit-opshttp)** — Configkit snaps into Servekit through `configkit/opshttp`. Protected read-only routes expose inspection and attempts at `/admin/config` and `/admin/config/attempts`, while the application route reads typed config through `Manager.Value()`.

**[08-servekit-readiness](08-servekit-readiness)** — Configkit status contributes to Servekit readiness. This example shows unloaded and failed states as not ready, loaded as ready, degraded as ready by default, and the stricter degraded-not-ready option.

### Workerkit Integration

**[09-workerkit-reload-command](09-workerkit-reload-command)** — Configkit reload snaps into Workerkit as an operational command. The example performs an initial Configkit load directly, then uses the `config/reload` command for successful and failed reloads, including command payload metadata and last-known-good preservation.

### The Full Picture

**[10-production-composition](10-production-composition)** — The full Kit Series composition. Configkit owns typed configuration lifecycle, Servekit owns HTTP service policy, and Workerkit owns command dispatch. The example demonstrates typed config reads, protected inspection, command-driven reload, failed reload preservation, degraded status, and readiness staying ready by default because last-known-good config remains active.

---

## Why This Order

The examples move from the core lifecycle outward deliberately. HTTP and worker commands are not where Configkit starts. Configkit starts with typed config becoming a validated, redacted, checksummed snapshot, then layers on operational concerns only where they belong.

This order answers five questions:

- "What is the shortest useful Configkit load?"
- "How do source metadata, redaction, snapshots, status, and inspection work before reloads?"
- "What happens when reloads succeed, fail, or publish the same effective config?"
- "How do I observe and expose Configkit safely through Servekit and Workerkit?"
- "What does the full production composition look like?"

---

## Run Them

Run examples from the repository root:

```bash
go run ./examples/01-basic-json
go run ./examples/02-file-source
go run ./examples/03-redaction-inspection
go run ./examples/04-failed-reload
go run ./examples/05-changed-detection
go run ./examples/06-observability-slog
go run ./examples/07-servekit-opshttp
go run ./examples/08-servekit-readiness
go run ./examples/09-workerkit-reload-command
go run ./examples/10-production-composition
```

Each example prints its own output and startup notes. The source comments in each `main.go` explain what to look for while it runs.
