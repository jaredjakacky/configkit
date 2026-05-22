// Package opshttp mounts Configkit operational views on Servekit.
//
// The package is an optional adapter for Kit Series services that use both
// Configkit and Servekit. It exposes read-only configuration lifecycle state
// without exposing typed configuration values. Configkit still owns lifecycle
// state, and Servekit still owns HTTP routing, response encoding, endpoint
// policy, auth gates, and readiness endpoints.
//
// The root configkit package does not import or compile against Servekit.
// Applications only compile this adapter when they import configkit/opshttp.
// Because this adapter lives in the same Go module as the root package,
// Servekit may still appear in this repository's go.mod.
//
// Operational responses can include caller-provided metadata, revisions,
// checksums, redacted values, and error strings. Protect these routes with
// Servekit endpoint options when they are not safe for the default audience.
package opshttp
