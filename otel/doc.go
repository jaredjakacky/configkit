// Package otel provides OpenTelemetry observers for Configkit.
//
// This is a first-party optional integration. The root configkit package does
// not import or compile against OpenTelemetry, and applications that do not
// import this package do not compile or link it. Because this adapter lives in
// the same Go module as the root package, OpenTelemetry remains in the module
// dependency graph and can participate in version selection. Keeping it in the
// same module makes telemetry discoverable and versioned with Configkit while
// preserving a backend-neutral core package.
//
// The observer records these metrics:
//
//	configkit.load.started
//	configkit.load.completed
//	configkit.load.failed
//	configkit.load.duration
//	configkit.apply.published
//	configkit.apply.changed
//
// It creates these spans:
//
//	configkit.load
//	configkit.apply
//
// These are retrospective lifecycle spans created from emitted Configkit events.
// The observer does not wrap pipeline execution and does not create parent spans
// around source reads, decoders, default appliers, validators, copiers,
// redactors, or checksum functions. Application-provided source and pipeline
// functions may create their own spans when they need execution-level tracing.
//
// The default attributes are low-cardinality and Kubernetes-friendly:
//
//	configkit.event
//	configkit.component.name
//	configkit.manager.state
//	configkit.attempt.kind
//	configkit.attempt.status
//	configkit.attempt.stage
//	configkit.failure.code (failed attempts only)
//	configkit.source.kind
//	configkit.apply.published
//	configkit.apply.changed
//
// Spans also include configkit.attempt.id for manager-local correlation. Attempt
// IDs are excluded from metric attributes because they are unbounded.
//
// Source names are excluded by default because they can increase metric
// cardinality. Use WithSourceName when source names are stable enough for your
// telemetry backend and safe for telemetry. Revision, checksum, raw config
// data, redacted config data, and typed config values are never recorded by
// this observer. Source kind, optional source name, attempt stage/status, and
// public failure code may be recorded as attributes. Public failure messages
// may be recorded as span status/error data. Arbitrary internal error strings
// are not recorded.
package otel
