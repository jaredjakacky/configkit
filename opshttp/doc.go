// Package opshttp mounts Configkit operational views on Servekit.
//
// The package is an optional adapter for Kit Series services that use both
// Configkit and Servekit. It exposes read-only configuration lifecycle state
// without exposing typed configuration values. Configkit still owns lifecycle
// state, and Servekit still owns HTTP routing, response encoding, endpoint
// policy, auth gates, and readiness endpoints.
//
// The root configkit package does not import this adapter. Applications that
// import only the root package do not compile or link opshttp or Servekit.
// Because this adapter shares Configkit's Go module, Servekit remains in the
// module dependency graph and can participate in version selection.
//
// Operational responses can include caller-provided metadata, revisions,
// checksums, redacted values, and public failure detail. Protect these routes
// with Servekit endpoint options when they are not safe for the default
// audience.
package opshttp
