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

A reload that reaches Configkit but fails to read, decode, validate, redact, or
checksum configuration also returns a completed command. Its payload describes
the failed attempt, and Configkit preserves the last-known-good snapshot when
one exists. This is a domain operation result, not a Workerkit dispatch
failure.

Cancellation and deadline expiration return failed Opskit command results with
no reload payload. Workerkit maps those failures into its normal command error
and lifecycle handling.

## Operational safety

Reload results never contain the typed configuration value or redacted
inspection view. They can contain revisions, checksums, and stage-specific
public failure detail. Treat command discovery and results as operational data
and authorize every dispatch surface accordingly.

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
