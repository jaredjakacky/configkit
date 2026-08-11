# Reload Commands

Configkit exposes configuration reload through Opskit's transport-neutral
command contracts. Configkit owns reload behavior and result data. An executor
such as Workerkit owns dispatch, admission, timeout, retry, concurrency, and
observation policy.

## Create the command

```go
reload := configkit.ReloadCommand(manager, source, pipeline)
```

`ReloadCommand` implements `opskit.Component`, `opskit.CommandDescriber`, and
`opskit.CommandHandler`. Its default component name is `config-reload`, and its
single command descriptor is named `config/reload`.

The handler reports ready status when its manager and source are present and
`Pipeline.Validate` accepts its required steps. Nil and typed-nil `Source`
values are both missing static configuration: status is not ready and commands
are rejected without recording a manager load attempt. Status does not read the
source or mirror manager lifecycle state.

Register the handler with Opskit when the service needs command discovery or
component inspection:

```go
ops.MustRegister(reload, opskit.Informational())
```

Opskit defines and discovers the command capability. It does not execute the
command or add runtime policy.

## Execute through Workerkit

Workerkit generically adapts Opskit command descriptors and handlers:

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

The generic adapter preserves the Opskit command name, description, payload
kind, dangerous and idempotent hints, and safe attributes. It translates
Workerkit requests into Opskit requests and JSON-encodes the Opskit result
payload for Workerkit callers.

## Result semantics

A successful reload returns a completed command containing
`ReloadCommandResult` and publishes the new snapshot.

A configured handler that admits a reload but fails to read, decode, apply
defaults, validate, copy, redact, checksum, or publish configuration returns a
failed Opskit command with explicit safe public `Failure` detail and no reload
payload. Configkit records the attempt and preserves the last-known-good
snapshot when one exists.

Cancellation and deadline expiration are also failed Opskit command results.
Workerkit maps failed results to `ErrOpsCommandFailed`, records
`LastCommandFailure`, emits command failure and completion observations, and
applies configured retry policy. A returned command error does not
automatically fail the Workerkit worker lifecycle or runtime readiness.

Handler and manager state remain separate. A failed reload commonly leaves the
manager `degraded` but ready by default because its last-known-good snapshot is
still active. `WithDegradedReady(false)` remains the explicit strict-readiness
option.

Workerkit retries are opt-in. The reload descriptor's `Idempotent` field remains
advisory, so applications should use predicate-gated retry and inspect
`*workerkit.OpskitCommandError`. Broad retry policies can repeat permanent
failures; predicates should normally exclude `ErrOpsCommandRejected` and retry
only selected `ErrOpsCommandFailed` failure codes.

## Operational safety

Successful reload results never contain the typed configuration value or
redacted inspection view. They can contain revisions and checksums. Failed
commands expose only stage-specific public failure detail through Opskit and
Workerkit command error, status, and telemetry surfaces. Treat command discovery
and outcomes as operational data and authorize every dispatch surface
accordingly.

Recovered lifecycle panic payloads are replaced with safe stage-specific text.
Normal errors returned by sources and pipeline functions remain on the direct
internal return path and are not serialized.

Avoid exposing the same handler through multiple command routes with different
authorization policies. In the normal Kit Series composition, Workerkit owns
execution and Servekit exposes Workerkit dispatch only through an explicitly
protected administrative route.

## Related material

- [Reloads](reloads.md)
- [Composition](composition.md)
- [Operational Safety](operational-safety.md)
- [Examples](examples.md)
