// Package configkit provides typed configuration lifecycle primitives for Go
// services.
//
// Configkit helps services turn raw configuration data into typed, validated,
// redacted, checksummed snapshots. The application owns the configuration
// struct, source choices, validation rules, defaults, redaction policy, and
// business meaning. Configkit owns the lifecycle mechanics around that value.
//
// The stateless load lifecycle is:
//
//	Source read -> Decode -> Apply defaults -> Validate -> Copy -> Redact -> Checksum -> Snapshot
//
// Source implementations read raw configuration data. A Pipeline decodes that
// data into the application's config type, applies optional defaults, runs
// optional validation, optionally copies the value for snapshot publication,
// builds a safe redacted view, and computes a checksum. Load runs that lifecycle
// once without storing state.
//
// Manager stores the current last-known-good snapshot, records recent load
// attempts, exposes status, and preserves the current snapshot when a later
// load fails. Manager load methods return both the stateless load result and
// the apply result describing publication.
//
// Lifecycle APIs require callers to pass a non-nil context. Passing a nil
// context is invalid and may panic.
//
// Provider and Inspector are read-only seams for other packages that need
// current configuration state or safe operational inspection without mutating
// configuration lifecycle state. Operational views do not expose the typed
// configuration value, but they can include caller-provided metadata, revisions,
// redacted values, and error strings.
//
// Treat all operational output as potentially visible to logs, telemetry,
// diagnostics, support tools, or admin endpoints. Do not put secrets in
// SourceMetadata, revisions, checksums, validation errors, or source read
// errors. Redacted views are application-owned; keep Redactor implementations
// conservative and prefer EmptyRedactor until a field is explicitly safe to
// expose. Checksums are operational fingerprints, not secret-safe redaction
// mechanisms, and can leak information for low-entropy values or known config
// sets. HTTP or admin endpoints that expose Status or Inspection should be
// protected by the application's routing, authentication, and policy layer.
//
// Observers provide lifecycle telemetry hooks, with SlogObserver and
// AsyncObserver adapters for standard logging and explicit asynchronous
// delivery.
//
// The package is storage-neutral and transport-neutral. It does not provide a
// secrets manager, feature flag system, policy engine, distributed control
// plane, polling loop, HTTP route, client rebuild mechanism, durable state
// store, or deployment abstraction.
package configkit
