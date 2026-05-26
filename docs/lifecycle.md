# Lifecycle

Configkit separates reading raw configuration, transforming it into a typed
snapshot, and publishing that snapshot into manager state.

## Stateless Load Lifecycle

The stateless lifecycle is:

```text
Source read -> Decode -> Apply defaults -> Validate -> Copy -> Redact -> Checksum -> Snapshot
```

`LoadFromSource` includes the source read. `Load` starts with already-read
`SourceData`.

## Source Read

`Source.Metadata` provides safe operational context before reading. `Source.Read`
returns `SourceData`: raw bytes, metadata, and an optional revision.

Source read failures produce a failed `LoadResult` with:

- `AttemptStatusFailed`
- `AttemptStageSourceRead`
- no snapshot
- an error string recorded on the attempt

Source errors may appear in operational output. They should not include secrets.

## Decode

`Decode` turns raw `SourceData` into the application config type.

`JSONDecoder[T]` is the built-in JSON decoder. Applications can provide their
own decoder for YAML, TOML, environment-derived config, merged sources,
decryption, stricter parsing, or backend-specific formats.

## Apply Defaults

`ApplyDefaults` is optional. It should fill mechanical defaults before
validation runs.

Defaults should not hide invalid deployment policy. Keep environment policy and
business meaning in application code.

## Validate

`ValidateConfig` is optional but recommended for production config.

Validation errors are recorded in attempts, status, logs, telemetry, and
adapter output. Keep them safe for operational audiences. Do not include secret
values in error strings.

## Copy

`Copy` is optional. Use it when `T` contains mutable references and the value
published in the snapshot should be detached from values used during earlier
pipeline steps.

Configkit returns typed config values by normal Go assignment rules. It cannot
make arbitrary application structs deeply immutable.

## Redact

`Redact` is required. It returns the operational inspection view for the
snapshot.

Use `EmptyRedactor[T]()` until a field is explicitly safe to expose. Custom
redactors should be conservative and should prefer boolean or summary fields
over masked secret values.

## Checksum

`Checksum` is required. It computes a stable fingerprint for the effective
configuration.

`SHA256JSONChecksum[T]()` hashes the JSON representation of the typed value.
Checksums are operational fingerprints, not secrecy mechanisms. Avoid exposing
checksums for low-entropy or secret-bearing config when the fingerprint itself
would be sensitive.

## Snapshot

A successful load creates a `Snapshot[T]` containing:

- the typed config value
- source metadata
- revision
- checksum
- load time
- redacted inspection view

Failed loads do not produce snapshots.

## Manager Apply

Manager load methods run the stateless lifecycle and then apply the result:

```text
Manager.Load           -> Load           -> Apply
Manager.LoadFromSource -> LoadFromSource -> Apply
```

Successful attempts publish the snapshot and update last success. Failed
attempts preserve the current snapshot and update last failure.

`Manager.Apply` can apply externally produced `LoadResult` values. It validates
the result before publication and returns `ErrInvalidLoadResult` for malformed
input.

`Manager.Apply` records status and attempts. When it publishes a successful
snapshot, it emits `snapshot_applied`. It does not emit `load_started`,
`load_succeeded`, or `load_failed` because the load lifecycle happened outside
the manager-owned `Load` or `LoadFromSource` method.

## Status Transitions

Initial manager state is `unloaded`.

Failed initial load:

```text
unloaded -> failed
```

Successful initial load:

```text
unloaded -> loaded
```

Successful reload:

```text
loaded -> loaded
degraded -> loaded
```

Failed reload with a current snapshot:

```text
loaded -> degraded
degraded -> degraded
```

The `degraded` state means the most recent attempt failed, but a valid
last-known-good snapshot remains active.

## Panic Recovery

Configkit recovers panics from source metadata reads, source reads, and pipeline
steps. Recovered panics become load errors with the appropriate failed stage.

Observer panics are also recovered by the manager. Observers should still return
quickly because they run synchronously unless wrapped by `AsyncObserver`.
Synchronous observers must not call `Load`, `LoadFromSource`, or `Apply` on the
same manager that emitted the event. Read-only calls such as `Status`,
`Inspect`, `Snapshot`, and `Value` are acceptable. Use `AsyncObserver` or
another goroutine for follow-up work that may block or trigger lifecycle
operations.

## Context Contract

Lifecycle APIs require non-nil contexts. Passing nil is invalid and may panic.

This applies to:

- `Load`
- `LoadFromSource`
- `Manager.Load`
- `Manager.LoadFromSource`
- `Manager.Apply`
- source `Read` methods
- pipeline step functions

## Examples

- [`examples/01-basic-json`](../examples/01-basic-json)
- [`examples/02-file-source`](../examples/02-file-source)
- [`examples/04-failed-reload`](../examples/04-failed-reload)
- [`examples/05-changed-detection`](../examples/05-changed-detection)
