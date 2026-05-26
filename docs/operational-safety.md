# Operational Safety

Configkit is designed to keep raw typed config values out of operational
surfaces. That does not mean every string associated with configuration is safe
to expose.

Operational output can include caller-provided metadata, revisions, checksums,
redacted values, and error strings. Treat those fields as visible to their
audience.

## What Configkit Does Not Expose

Configkit does not expose typed config values through:

- `Status`
- `Inspection`
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

## Error Strings

Validation errors, source read errors, and recovered panic strings may be
recorded in attempts, status, logs, telemetry, ops HTTP responses, and reload
command payloads.

Do not include raw secret values in errors.

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
durations, and error strings.

`AsyncObserver` changes delivery behavior only. It does not sanitize event data.

Custom observers should follow the same rule: events are operational data, not
typed config exposure.

## OpenTelemetry

The optional `otel` observer avoids revisions, checksums, raw config data,
redacted config data, and typed config values.

Default attributes are low-cardinality. `WithSourceName` is opt-in because
source names may increase cardinality and may expose caller-provided metadata.

Load error strings may be recorded on failed spans.

## Ops HTTP

`configkit/opshttp` exposes read-only operational state through Servekit.

Routes may include metadata, revisions, checksums, redacted values, and error
strings. Protect these routes with Servekit endpoint options when they are not
safe for the default audience.

Example policy:

```go
opshttp.Mount(server, manager,
	opshttp.WithEndpointOptions(
		servekit.WithAuthGate(requireAdmin),
		servekit.WithEndpointTimeout(5*time.Second),
	),
)
```

## Worker Reload Command

`configkit/worker.ReloadCommand` returns operational reload metadata:

- attempt ID
- attempt status
- manager state
- published
- changed
- current checksum
- current revision
- error string

The payload does not include typed config values or redacted inspection output.
Revisions, checksums, and errors are still visible to whoever can dispatch or
inspect the command result.

## Future Operational Endpoints

Any future HTTP or administrative endpoint exposing `Status`, `Inspection`,
attempts, or reload results should be protected by the service's routing,
authentication, authorization, and audit policy.

In the Kit Series, that policy belongs at the Servekit boundary.

## Related Material

- [`usage.md`](usage.md)
- [`observability.md`](observability.md)
- [`composition.md`](composition.md)
- [`../SECURITY.md`](../SECURITY.md)
