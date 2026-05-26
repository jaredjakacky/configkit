// Package worker adapts Configkit reloads into Workerkit commands.
//
// The package is an optional adapter for Kit Series services that use both
// Configkit and Workerkit. It exposes configuration reload as a Workerkit
// command while leaving Configkit lifecycle semantics and Workerkit command
// dispatch policy in their respective packages.
//
// The root configkit package does not import or compile against Workerkit.
// Applications only compile this adapter when they import configkit/worker.
// Because this adapter lives in the same Go module as the root package,
// Workerkit may still appear in this repository's go.mod.
//
// The adapter does not poll, watch files, rebuild clients, or expose HTTP. It
// returns Workerkit command specs that applications can attach to whichever
// worker owns the operational reload command.
package worker
