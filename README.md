# Configkit

[![Release](https://img.shields.io/github/v/release/jaredjakacky/configkit?sort=semver)](https://github.com/jaredjakacky/configkit/releases)
[![CI](https://github.com/jaredjakacky/configkit/actions/workflows/ci.yaml/badge.svg)](https://github.com/jaredjakacky/configkit/actions/workflows/ci.yaml)
[![Go Support](https://img.shields.io/badge/go%20support-1.25.x%20%7C%201.26.x-00ADD8)](https://github.com/jaredjakacky/configkit/actions/workflows/ci.yaml)
[![License](https://img.shields.io/github/license/jaredjakacky/configkit)](https://github.com/jaredjakacky/configkit/blob/main/LICENSE)

## Overview

Configkit is a small Go package for managing typed application configuration with a production-oriented lifecycle.

Decoding configuration is the easy part. The hard part is everything that comes after it becomes runtime state: applying defaults, validating, redacting, checksumming, tracking reload attempts, preserving last-known-good behavior, and exposing status for operational inspection.

Configkit gives ordinary Go services a consistent shell around that lifecycle. A source produces raw bytes. A pipeline decodes, validates, redacts, and checksums them into a typed snapshot. A manager publishes the current snapshot, records load and reload attempts, exposes status, and preserves the last-known-good configuration when a reload fails.

The application owns the config struct, source choices, validation rules, defaults, redaction policy, and business meaning. Configkit owns the reusable mechanics around that value — nothing more.

It is not a framework, secrets manager, feature flag system, policy engine, or distributed configuration control plane. It keeps configuration ordinary Go and gives it a clear operational boundary.

## Why Configkit exists

Configuration starts simple, then quietly becomes production state.

A service begins with a few environment variables or a small config file. That works until operations asks real questions: What configuration is active? Where did it come from? When was it loaded? Did validation pass? What changed on the last reload? What was the last failed attempt? What is safe to show in logs or an admin endpoint? Is the service still ready if a reload fails?

Those concerns are not business logic, but they appear in every serious service. Without a defined lifecycle, they spread: package globals, ad hoc validation, one-off reload code, disconnected log statements, unsafe debug output.

Configkit pulls that operational layer into one reusable package. Your config stays ordinary Go. Configkit gives it a predictable runtime envelope.

## What you get

With one manager and one pipeline, Configkit gives a Go service the configuration lifecycle it would otherwise rebuild from scratch:

- typed snapshots
- source metadata and revisions
- defaults and validation
- optional copy-before-publication
- safe redacted inspection
- checksums and change detection
- load and reload attempt records
- last-known-good preservation
- lifecycle status states
- observer hooks, structured logging, and optional OpenTelemetry
- optional Servekit operations routes
- generic Workerkit execution of the Opskit reload command

That is the lifecycle teams usually rebuild around production services. Configkit makes it the baseline instead of the afterthought.

## What Configkit is not

Configkit is not a secrets manager. It does not store secrets, rotate credentials, manage access policy, or replace systems such as Vault, AWS Secrets Manager, Kubernetes Secrets, or SOPS.

Configkit is not a feature flag system. It does not evaluate rollout rules, tenant targeting, experiments, or gradual release policy.

Configkit is not a distributed configuration control plane. It does not push changes across a fleet, coordinate rollout, manage leases, or decide which instance should apply which configuration.

Configkit is not tied to a specific backend. The core package does not require Kubernetes, Consul, Vault, SSM, etcd, a database, or a remote API. Those systems can be connected through sources or adapters, but they are not part of the core package.

Configkit does not own your application model. It does not decide what a setting means, which values are safe for a tenant, which features should be enabled, or how production should differ from staging. The application owns those rules.

Its focus is narrower: load configuration, apply defaults, validate, copy, redact, checksum, snapshot, inspect, record reload attempts, preserve last-known-good state, and notify observers.

## Backend-specific sources

Custom `Source` implementations can connect Configkit to SSM, Vault,
Kubernetes, Consul, etcd, databases, remote APIs, or other configuration
backends. Those sources should usually live in application code or dedicated
adapter packages, not in the root `configkit` package.

Root Configkit should not own backend auth policy, leases, polling, watching,
rollout behavior, client lifecycle, or backend-specific operational policy.
First-party source adapters, if ever added, should preserve the `Source`
boundary and stay policy-light: read raw configuration bytes and report safe
metadata, while the application or adapter owns backend details. This follows
the scope described in [What Configkit is not](#what-configkit-is-not).

## Good fit / not a fit

Configkit is a good fit when:

- your service has typed configuration that needs validation, defaults, redaction, status, and operational inspection
- you want configuration to produce snapshots instead of scattered reads from environment variables, files, or package-level globals
- you need to track where configuration came from, when it was loaded, what checksum or revision is active, and whether validation succeeded
- you want reload attempts to have clear behavior: successful reloads publish a new snapshot, failed reloads preserve the last-known-good configuration, and both outcomes are observable
- you need safe redacted views for logs, diagnostics, admin endpoints, or support workflows
- you want source-neutral configuration mechanics that can work with files, environment-derived data, remote stores, or platform adapters without making any one backend part of the core package
- you want production-oriented configuration lifecycle behavior without adopting a framework or giving up ordinary Go structs and validation code

Configkit is probably not a fit when:

- you only need to read one or two environment variables once at process startup
- you want any of the systems described in [What Configkit is not](#what-configkit-is-not) above: secrets management, feature flags, or a distributed configuration control plane
- you want the library to decide environment policy, deployment rules, or which settings belong in development, staging, or production
- you want configuration validation to encode your business rules instead of keeping that meaning in application code
- your service already has a mature configuration lifecycle with snapshots, redaction, reload behavior, status, and observability that Configkit would mostly duplicate

## Installation

```bash
go get github.com/jaredjakacky/configkit
```

```go
import configkit "github.com/jaredjakacky/configkit"
```

Optional adapters are imported separately:

```go
import opshttp "github.com/jaredjakacky/configkit/opshttp"
import configotel "github.com/jaredjakacky/configkit/otel"
```

### Dependency model

Configkit intentionally uses one Go module for the root package, optional
first-party adapters, tests, and runnable examples. This keeps the integrations
discoverable and versioned with Configkit, so `go.mod` includes dependencies
used outside the root package, including Servekit, Workerkit, and OpenTelemetry.

Go compiles the packages an application imports, not every package in a module.
An application that imports only `github.com/jaredjakacky/configkit` compiles
Configkit, Opskit, and the standard library. It does not compile or link
Servekit, Workerkit, `configkit/opshttp`, `configkit/otel`, or OpenTelemetry.
Those modules may still appear in the application's module graph and
participate in version selection.

Import `configkit/opshttp` or `configkit/otel` explicitly when those
integrations are needed. Configkit does not provide a Workerkit-specific
adapter package; Workerkit executes `configkit.ReloadCommand` through the
shared Opskit command contract.

This one-module policy is deliberate before v1. A future integration with a
substantially heavier dependency graph or an independent release cadence may
be moved to a separate module, but package-level adapters remain the default.

## Quick Start

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	configkit "github.com/jaredjakacky/configkit"
)

type AppConfig struct {
	ServiceName string `json:"service_name"`
	Port        int    `json:"port"`
	Environment string `json:"environment"`
}

func main() {
	ctx := context.Background()

	source := configkit.NewBytesSource(
		[]byte(`{"service_name":"checkout","port":8080}`),
		configkit.SourceMetadata{Name: "embedded-json", Kind: "memory"},
		"example-v1",
	)

	pipeline := configkit.Pipeline[AppConfig]{
		Decode: configkit.JSONDecoder[AppConfig](),

		ApplyDefaults: func(ctx context.Context, cfg AppConfig) (AppConfig, error) {
			if cfg.Environment == "" {
				cfg.Environment = "development"
			}
			return cfg, nil
		},

		ValidateConfig: func(ctx context.Context, cfg AppConfig) error {
			if cfg.ServiceName == "" {
				return errors.New("service_name is required")
			}
			if cfg.Port <= 0 || cfg.Port > 65535 {
				return fmt.Errorf("port %d is invalid", cfg.Port)
			}
			return nil
		},

		Redact:   configkit.EmptyRedactor[AppConfig](),
		Checksum: configkit.SHA256JSONChecksum[AppConfig](),
	}

	manager := configkit.NewManager[AppConfig]()

	result, err := manager.LoadFromSource(ctx, configkit.AttemptKindInitialLoad, source, pipeline)
	if err != nil {
		log.Fatal(err)
	}

	cfg, ok := manager.Value()
	if !ok {
		log.Fatal("config not loaded")
	}

	fmt.Printf("service=%s port=%d env=%s changed=%t\n",
		cfg.ServiceName,
		cfg.Port,
		cfg.Environment,
		result.Apply.Changed,
	)

	fmt.Printf("status=%s checksum=%s\n",
		manager.LifecycleStatus().State,
		result.Apply.Current.Checksum,
	)
}
```

That one manager gives the service a complete configuration lifecycle: typed snapshot, source metadata, revision, checksum, status, inspection, attempt records, observer hooks, and last-known-good behavior, without hand-building any of it yourself.

## The Core Model

Configkit is deliberately built around a small set of ordinary Go concepts.

`Source` reads raw configuration bytes and safe source metadata. Built-in
sources cover in-memory bytes and local files; custom sources can connect to
remote APIs, platform stores, or application-owned backends without making those
systems part of the root package.

`Pipeline[T]` turns raw source data into a publishable typed snapshot. `Decode`,
`Redact`, and `Checksum` are required. Defaults, validation, and copy are
optional. The application owns those functions because it owns the config type
and its meaning. A successful Checksummer must return a non-empty fingerprint;
Configkit does not otherwise prescribe its format.

`JSONDecoder[T]` is the production-oriented JSON default. It rejects unknown
fields at ordinary Go struct boundaries and requires exactly one JSON value,
while accepting leading and trailing whitespace. It otherwise follows
`encoding/json`, including case-insensitive field matching and duplicate-key
behavior. Use `LenientJSONDecoder[T]` when ignoring unknown fields is an
intentional compatibility policy.

`Snapshot[T]` is one successfully loaded, validated, and published value. It
contains the typed config, source/checksum metadata, and a safe redacted
inspection view. Failed loads and reloads do not produce snapshots.

`Manager[T]` owns the current last-known-good snapshot, records attempts,
exposes lifecycle and Opskit operational views, and preserves the current
snapshot when a later reload fails. Most services should start with
`Manager.LoadFromSource`.

`LifecycleStatus` and `LifecycleInspection` are operational views. They do not expose the typed
config value. `LifecycleProvider[T]` and `LifecycleInspector` are read-only
composition seams for application code and operational adapters.

Observers receive `load_started`, `load_succeeded`, `load_failed`, and
`snapshot_applied` events for logs, telemetry, diagnostics, and lightweight
operational hooks. Manager-emitted events include the normalized Opskit
component name and manager state at the event boundary. Together,
`ComponentName` and the manager-local `AttemptID` correlate attempts when
multiple managers share an observer.

## Mutability Contract

Snapshots are immutable by convention, not by deep-copy enforcement. `Value`
methods return `T` by normal Go assignment rules. Prefer scalar or naturally
immutable config shapes. If `T` contains maps, slices, pointers, or other
mutable references, use `Pipeline.Copy` to detach them before publication and
treat returned values as read-only.

```go
type AppConfig struct {
	ServiceName string            `json:"service_name"`
	Headers     map[string]string `json:"headers"`
}

pipeline := configkit.Pipeline[AppConfig]{
	Decode: configkit.JSONDecoder[AppConfig](),
	Copy: func(ctx context.Context, cfg AppConfig) (AppConfig, error) {
		cfg.Headers = maps.Clone(cfg.Headers)
		return cfg, nil
	},
	Redact:   configkit.EmptyRedactor[AppConfig](),
	Checksum: configkit.SHA256JSONChecksum[AppConfig](),
}
```

`Pipeline.Copy` protects the published snapshot from references shared with
earlier pipeline stages. It does not prevent later callers from mutating maps,
slices, or pointers they receive through `Manager.Value` or `Snapshot.Value`.
Configkit cannot enforce deep immutability for arbitrary Go values.

## Reload behavior and last-known-good state

Configkit treats failed reloads differently from failed initial loads.

If the first load fails, there is no valid configuration to serve from. The manager enters `failed` state and has no current snapshot.

If a later reload fails after a valid snapshot already exists, Configkit preserves the last-known-good snapshot. The manager enters `degraded` state, records the failed attempt, and continues serving the previous valid config.

```go
if _, err := manager.LoadFromSource(ctx, configkit.AttemptKindInitialLoad, validSource, pipeline); err != nil {
	log.Fatal(err)
}

if _, err := manager.LoadFromSource(ctx, configkit.AttemptKindReload, invalidSource, pipeline); err != nil {
	fmt.Println("reload failed")
}

status := manager.LifecycleStatus()
fmt.Println(status.State) // degraded
if status.LastFailure != nil {
	fmt.Println(status.LastFailure.Failure) // safe public code and message
}

cfg, _ := manager.Value()
fmt.Printf("still serving from last-known-good config: %+v\n", cfg)
```

This is the main reason Configkit is a lifecycle package, not just a parser.

## Safe inspection and redaction

Configkit does not expose typed config values through `LifecycleStatus`, `LifecycleInspection`, `SlogObserver`, `opshttp`, or the OpenTelemetry observer.

LifecycleInspection uses the application-provided `Redactor[T]`.

```go
Redact: func(ctx context.Context, cfg AppConfig) (configkit.RedactedView, error) {
	return configkit.RedactedView{
		"service_name":       cfg.ServiceName,
		"port":               cfg.Port,
		"api_key_configured": cfg.APIKey != "",
	}, nil
}
```

The safest built-in redactor is `EmptyRedactor[T]`, which exposes no config fields.

Redaction is application-owned. Configkit cannot know which values are safe for logs, support tools, dashboards, or admin endpoints. Keep redactors conservative and expose only fields that are explicitly safe.

Operational output may still include source metadata, revisions, checksums,
attempt stages, stage-specific public failure detail, and redacted values chosen
by the application. Arbitrary validation and source-read error strings remain
on private return channels. Do not put secrets in public fields. Checksums are
operational fingerprints, not secrecy mechanisms, and can leak information for
low-entropy or known config sets.

## Opskit and Servekit composition

Configkit core is transport-neutral, but `Manager[T]` implements Opskit's
component, readiness, and inspection contracts. For Kit Series services, the
primary Servekit composition path is to register the manager with an Opskit
registry and give that registry to Servekit.

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

Servekit's `/readyz` includes the registered Configkit readiness, and
`/admin/components/config` exposes the manager's Opskit component snapshot when
`servekit.WithOpsAdmin()` is enabled.

The optional `configkit/opshttp` package remains useful when operators need a
Configkit-specific HTTP view:

```go
err := opshttp.Mount(server, manager,
	opshttp.WithEndpointOptions(servekit.WithAuthGate(requireAdmin)),
)
if err != nil {
	log.Fatal(err)
}
```

By default, `opshttp.Mount` exposes `GET /admin/config` for `configkit.LifecycleInspection` and `GET /admin/config/attempts` for recent attempts when available.

`opshttp.ReadinessCheck` is standalone Servekit support for services that are
not using an Opskit registry. It follows the manager's core readiness policy.

Operational routes can expose metadata, revisions, checksums, redacted values,
and public failure detail. Protect them with Servekit endpoint options
appropriate for the deployment.

## Opskit reload command

Configkit exposes reload as an Opskit command handler in the root package:

```go
reload := configkit.ReloadCommand(manager, source, pipeline,
	configkit.WithReloadCommandName("config/reload"),
	configkit.WithReloadCommandDescription("reloads Configkit configuration from source"),
)

ops.MustRegister(reload, opskit.Informational())
```

The command descriptor defaults to `config/reload`. Successful reloads return
completed Opskit command results. A configured handler that admits a reload but
cannot read, decode, default, validate, copy, redact, checksum, or publish it
returns a failed command with explicit safe public failure detail and no result
payload. Context cancellation and deadline expiration are also failed command
results.

A handler missing its manager, source, or required pipeline steps reports
not-ready status and rejects commands without recording a Configkit load
attempt. Nil and typed-nil `Source` values are both missing. Handler usability,
command outcome, and manager readiness are separate: a failed command can leave
the manager degraded but ready while it serves a last-known-good snapshot.

The successful result payload is `configkit.ReloadCommandResult`. It does not
include typed config values or redacted inspection output. It may include
attempt status, manager state, revision, and checksum. Failed commands expose a
stage-specific `opskit.Failure` instead. The internal returned error is not
serialized, and recovered panic payloads are replaced with safe stage-specific
failures.

## Workerkit reload command execution

Configkit core does not poll, watch files, schedule reloads, or expose HTTP reload routes.

For services that use [Workerkit](https://github.com/jaredjakacky/workerkit),
adapt the root Opskit reload handler through Workerkit's generic command
support:

```go
reload := configkit.ReloadCommand(manager, source, pipeline)
descriptors := reload.Commands(ctx)
if len(descriptors) != 1 {
	return fmt.Errorf("reload command descriptors = %d, want 1", len(descriptors))
}

if err := runtime.Register(workerkit.WorkerSpec{
	Name:        "config",
	Description: "Owns configuration reload commands.",
	Worker:      configWorker{},
}, workerkit.WithCommandSpec(
	workerkit.CommandFromOpskit(descriptors[0], reload),
)); err != nil {
	log.Fatal(err)
}
```

The default command name is `config/reload`.

The command calls `manager.LoadFromSource(ctx, configkit.AttemptKindReload,
source, pipeline)`. Workerkit maps failed Opskit reload results into
`ErrOpsCommandFailed`, records command failure status and telemetry, and applies
the configured command retry policy. Returned command errors do not
automatically fail the worker lifecycle or runtime readiness. Configkit still
preserves any last-known-good snapshot and reports its manager state separately.

Successful payloads do not include typed config values or redacted inspection
output. Failed commands retain the safe public failure code and message through
Workerkit's `OpskitCommandError`; internal returned errors and recovered panic
payloads are not exposed.

## Observability

Configkit emits lifecycle events through `Observer`.

```go
manager := configkit.NewManager[AppConfig](
	configkit.WithObservers(configkit.SlogObserver(logger)),
)
```

`SlogObserver` logs lifecycle metadata such as event kind, component name,
manager state, attempt ID, source metadata, attempt status, failure stage,
checksum, duration, and apply result. It does not log typed config values or
redacted fields.

Observers run synchronously by default and should return quickly. A synchronous
observer must not call `Load`, `LoadFromSource`, or `Apply` on the same manager
that emitted the event, because that creates reentrant lifecycle behavior and
can deadlock. Read-only calls such as `LifecycleStatus`, `LifecycleInspection`, `Snapshot`, and
`Value` are acceptable.

Manager state is updated before `load_succeeded` or `load_failed` is delivered,
so synchronous completion observers see state consistent with the event. A
completion event includes the resulting `ApplyResult`; failed reloads therefore
show the retained last-known-good snapshot without exposing its typed value.

Use `AsyncObserver` when an observer may block:

```go
async := configkit.NewAsyncObserver(configkit.SlogObserver(logger))
defer async.Close(context.Background())

manager := configkit.NewManager[AppConfig](
	configkit.WithObservers(async.Observer()),
)
```

`AsyncObserver` delivers events on a background goroutine. It does not block
loads or applies. If its queue is full or closed, it drops events and counts
them with `Dropped()`. Use it, or hand work off to another goroutine, for
follow-up work that may block or trigger more lifecycle operations. Event
fields are the authoritative event-time view; a live manager read during later
async delivery may observe a newer attempt.

The optional OpenTelemetry package provides metrics and retrospective lifecycle
spans from emitted Configkit events. It does not wrap source reads or pipeline
steps; application-provided sources, decoders, validators, and other functions
can create their own spans when execution-level tracing is needed.

This is optional at build time: applications only compile it when they import
`configkit/otel`. See [Dependency model](#dependency-model) for the distinction
between Configkit's package build graph and module dependency graph.

```go
observer, err := configotel.NewObserver(meter, tracer)
if err != nil {
	log.Fatal(err)
}

manager := configkit.NewManager[AppConfig](
	configkit.WithObservers(observer),
)
```

The OpenTelemetry observer records load, failure, duration, publish, and
changed metrics. It creates `configkit.load` and `configkit.apply` lifecycle
spans. It does not record revision, checksum, raw config data, redacted config
data, or typed config values.

## Kit Series boundaries

Configkit is the typed configuration lifecycle shell in the Kit Series.

Servekit owns inbound HTTP routing, request policy, auth gates, response encoding, readiness endpoints, and HTTP lifecycle. Opskit owns shared operational registry views. Workerkit owns background runtime, commands, reload triggers, scheduling, retries, concurrency, and worker lifecycle. Configkit owns source reads, decoding, defaults, validation, redaction, checksums, snapshots, status, inspection, reload bookkeeping, and observer events.

The primary composition path is Configkit manager to Opskit registry to
Servekit. In services with Workerkit v0.6.0 or newer, register the Workerkit
runtime in that same Opskit registry. The
optional adapters preserve narrower boundaries: `configkit/opshttp` exposes
Configkit-specific inspection routes. Workerkit executes Configkit's root
Opskit reload command through its generic command adapter.

Core packages stay independently useful. Shared Opskit contracts snap them
together, while specialized adapters remain limited to behavior the generic
surface does not cover.

## Examples

Runnable programs live in [`examples/`](examples), which includes a guided tour of the example set.

Recommended reading order:

1. [`examples/01-basic-json`](examples/01-basic-json)
2. [`examples/02-file-source`](examples/02-file-source)
3. [`examples/03-redaction-inspection`](examples/03-redaction-inspection)
4. [`examples/04-failed-reload`](examples/04-failed-reload)
5. [`examples/05-changed-detection`](examples/05-changed-detection)
6. [`examples/06-observability-slog`](examples/06-observability-slog)
7. [`examples/07-servekit-opshttp`](examples/07-servekit-opshttp)
8. [`examples/08-servekit-readiness`](examples/08-servekit-readiness)
9. [`examples/09-workerkit-reload-command`](examples/09-workerkit-reload-command)
10. [`examples/10-production-composition`](examples/10-production-composition)

The examples build from the smallest typed JSON load to full Kit Series composition with Configkit, Servekit, and Workerkit.

## Documentation

- [Getting Started](docs/getting-started.md): smallest useful Configkit program
- [Usage Guide](docs/usage.md): normal source, pipeline, manager, load, and reload path
- [Lifecycle](docs/lifecycle.md): load stages, apply behavior, status transitions, and context contract
- [Reloads](docs/reloads.md): last-known-good behavior, attempts, apply results, and change detection
- [Commands](docs/commands.md): Opskit reload commands and generic Workerkit execution
- [Operational Safety](docs/operational-safety.md): metadata, revisions, checksums, redaction, errors, logs, telemetry, and ops exposure
- [Observability](docs/observability.md): observer events, slog, async delivery, and OpenTelemetry
- [Composition](docs/composition.md): Configkit, Servekit, and Workerkit boundaries
- [Advanced Guide](docs/advanced.md): custom sources, decoders, checksums, redactors, copy, external apply, and adapter posture
- [Examples Guide](docs/examples.md): guided walkthrough of the runnable examples
- [API Map](docs/api.md): human-friendly map of the exported surface
- [Examples Directory](examples/README.md): quick index of the runnable example programs

## API Reference

The canonical symbol-level API documentation lives in Go doc comments so it stays accurate in editors and Go tooling. The repository-level companion is [docs/api.md](docs/api.md), which groups the exported surface into a human-oriented map.

## Development

Configkit is a small Go module. The main local verification command is:

```bash
make verify
```

`make verify` checks formatting, runs `go vet`, runs tests, builds examples, and verifies that `go.mod` and `go.sum` are tidy.

For changes that affect concurrency, manager state, observers, reload behavior, or adapter code, also run:

```bash
make test-race
```

For dependency or security-sensitive changes, run:

```bash
make govulncheck
```

CI runs verification and race tests on the supported Go versions. Release tags are gated by those jobs plus `govulncheck` before publishing.

### Releases

Prepare releases from a committed `main` revision that has been pushed to the
remote. Before creating the tag, run the complete pre-release suite with one
command:

```bash
make release-check
```

`release-check` requires network access and fails unless:

- the checkout is attached to `main`
- the working tree has no staged, unstaged, or non-ignored untracked files
- `origin/main` can be refreshed
- `HEAD` exactly matches the refreshed `origin/main`

The command validates that commit in a temporary detached Git worktree with Go
workspace discovery disabled. Local ignored files, caches, and parent
`go.work` files therefore cannot alter the tree under test. It runs `verify`,
`test-race`, and `govulncheck`, checks that validation did not modify the
detached worktree, and then repeats the branch, cleanliness, and remote-tip
checks in the original checkout.

After the command succeeds, create and push a stable `v0.x.y` or `v1.x.y` tag
without changing the checkout. If `main` advances or local state changes before
tagging, rerun `make release-check`. The release workflow validates that the tag
has the expected Go module version form and points to a commit on the `main`
release line. It then independently reruns `verify`, `test-race`, and
`govulncheck` on every supported Go version before publishing the GitHub
Release.

## Issues and Scope

Bug reports, documentation fixes, small API ergonomics improvements, and compatibility issues are welcome.

Configkit is intentionally scoped as a small typed configuration lifecycle library. Large feature additions are evaluated conservatively because Configkit is not a secrets manager, feature flag system, policy engine, dynamic configuration control plane, distributed configuration system, or deployment abstraction.

Likely out of scope for core: secret storage or rotation, feature flag evaluation, distributed rollout policy, fleet-wide config coordination, polling loops, file watching, HTTP reload routes, client construction or rebuilding, durable state persistence, and backend-specific source packages in the root package.

For security issues, please follow [`SECURITY.md`](SECURITY.md) instead of opening a public issue.

## Maintenance

Configkit is a small open source library maintained on a best-effort basis.

The active development line lives on `main`, and that is the only line actively maintained unless explicitly noted otherwise. The minimum supported Go version is declared in [`go.mod`](go.mod), and the Go versions currently verified in CI are listed in [`.github/workflows/ci.yaml`](.github/workflows/ci.yaml).

Compatibility-impacting changes should be called out explicitly in release notes or release descriptions. Long-lived maintenance branches and backports are not planned unless explicitly noted.

## License

[MIT](LICENSE)
