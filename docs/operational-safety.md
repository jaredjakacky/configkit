# Operational Safety

Configkit is designed to keep raw typed config values out of operational
surfaces. That does not mean every string associated with configuration is safe
to expose.

Operational output can include caller-provided metadata, revisions, checksums,
redacted values, and public failure detail. Treat those fields as visible to
their audience.

## What Configkit Does Not Expose

Configkit does not expose typed config values through:

- `LifecycleStatus`
- `LifecycleInspection`
- observer events
- `SlogObserver`
- the OpenTelemetry observer
- `opshttp` inspection routes
- the Workerkit reload command payload

Application code can still expose typed config values if it prints them, logs
them, returns them from HTTP routes, or places them in redacted views.

## Source Metadata

`SourceMetadata.Name`, `Kind`, and `Description` may appear in logs, telemetry,
status, inspection, ops HTTP responses, and command payloads.

Do not put secrets, tenant identifiers, user identifiers, full local paths,
environment names, or other sensitive details in source metadata unless that
audience is allowed to see them.

`NewFileSource` defaults `Name` to the base file name, not the full path. You
can still override it with an explicit safe name.

## Revisions

`SourceData.Revision` and snapshot revisions may appear in operational output.

Use revisions for safe operational identifiers such as a resource version, ETag,
generation, or sanitized build/config version.

Do not put secrets, full paths, tenant identifiers, or environment-sensitive
details in revisions.

## Checksums

Checksums are operational fingerprints. They are useful for change detection,
status, and support workflows.

Checksums are not redaction or secrecy mechanisms. They can leak information
when config values are low entropy or when the possible config set is known.

Avoid exposing checksums for low-entropy or secret-bearing config unless that
fingerprint is acceptable operational data.

## Redacted Views

`RedactedView` safety is application-owned.

Prefer conservative redaction:

```go
Redact: func(ctx context.Context, cfg AppConfig) (configkit.RedactedView, error) {
	return configkit.RedactedView{
		"service_name":       cfg.ServiceName,
		"port":               cfg.Port,
		"api_key_configured": cfg.APIKey != "",
	}, nil
}
```

Prefer booleans, counts, modes, or safe summaries over masked secret values.
Use `EmptyRedactor[T]()` until a field is explicitly safe to expose.

## Operational Failures

Validation errors and source read errors are returned to their caller for
internal diagnosis, but Configkit does not copy their arbitrary text into
attempts, status, logs, telemetry, ops HTTP responses, or reload command
payloads. Those surfaces receive a stage-specific public `opskit.Failure`.

Recovered panic payloads are not exposed. Direct callers receive a bounded
stage-specific panic error, while operational surfaces receive the same generic
stage failure used for ordinary returned errors.

Application logging of returned errors remains outside Configkit and must apply
the application's private logging policy.

Prefer:

```text
api_key is required
```

Avoid:

```text
api_key "abc123" is invalid
```

## Logs and Observers

`SlogObserver` logs lifecycle metadata, not typed config values or redacted
fields. It can log source metadata, revisions, checksums, attempt stages,
durations, and stage-specific public failure detail.

`AsyncObserver` changes delivery behavior only. It does not sanitize event
data, but it does clone queued event records and nested failure detail so later
caller mutation cannot change the delivered event.

Custom observers should follow the same rule: events are operational data, not
typed config exposure.

## OpenTelemetry

The optional `otel` observer avoids revisions, checksums, raw config data,
redacted config data, and typed config values.

Default attributes are low-cardinality. `WithSourceName` is opt-in because
source names may increase cardinality and may expose caller-provided metadata.

Public failure messages may be recorded on failed spans. Arbitrary returned
error strings are not recorded.

## Opskit and Ops HTTP

`Manager[T]` implements Opskit status, readiness, and inspection. Opskit status
attributes are intentionally low-cardinality and do not include source names,
revisions, checksums, errors, file paths, tenant IDs, or redacted config
values. Opskit inspection can include lifecycle summaries, retained attempts,
last apply results, and redacted config values chosen by the application.

`configkit/opshttp` exposes read-only operational state through Servekit.

Routes may include metadata, revisions, checksums, redacted values, and public
failure detail. Protect these routes with Servekit endpoint options when they
are not safe for the default audience.

Example policy:

```go
opshttp.Mount(server, manager,
	opshttp.WithEndpointOptions(
		servekit.WithAuthGate(requireAdmin),
		servekit.WithEndpointTimeout(5*time.Second),
	),
)
```

## Reload Command

`configkit.ReloadCommand` returns operational reload metadata for successful
commands. Workerkit's generic Opskit command adapter JSON-encodes that payload
when Workerkit executes the command:

- attempt ID
- attempt status
- manager state
- published
- changed
- current checksum
- current revision

The payload does not include typed config values, redacted inspection output, or
internal error causes. Revisions and checksums remain visible to whoever can
dispatch or inspect the successful command result.

Failed reloads return no result payload. Their Opskit `CommandResult.Failure`
contains only Configkit's stable public stage code and bounded message.
Workerkit copies that public detail into `OpskitCommandError`, command failure
status, and built-in telemetry while keeping arbitrary underlying source and
pipeline errors private. Full attempt and last-known-good metadata remains
available through protected Configkit manager inspection surfaces.

## Future Operational Endpoints

Any future HTTP or administrative endpoint exposing `LifecycleStatus`, `LifecycleInspection`,
attempts, or reload results should be protected by the service's routing,
authentication, authorization, and audit policy.

In the Kit Series, that policy belongs at the Servekit boundary.

## Related Material

- [`usage.md`](usage.md)
- [`observability.md`](observability.md)
- [`composition.md`](composition.md)
- [`../SECURITY.md`](../SECURITY.md)
