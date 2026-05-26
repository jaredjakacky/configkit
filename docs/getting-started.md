# Getting Started

This guide builds the smallest useful Configkit program: raw JSON bytes become
a typed, validated, checksummed snapshot owned by a manager.

For the runnable version, see [`examples/01-basic-json`](../examples/01-basic-json).

## Install

```bash
go get github.com/jaredjakacky/configkit
```

## Build a First Config Manager

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

	fmt.Printf("config: %+v\n", cfg)
	fmt.Printf("state: %s\n", manager.Status().State)
	fmt.Printf("checksum: %s\n", result.Apply.Current.Checksum)
}
```

## What This Creates

`NewBytesSource` creates a source for raw configuration bytes. It does not
decode, validate, redact, checksum, publish, or reload configuration.

`Pipeline[T]` describes the lifecycle for one load attempt. It turns raw bytes
into a typed value, applies defaults, validates the result, produces a safe
redacted view, and computes a checksum.

`NewManager[T]` creates the state owner. The manager stores the current
last-known-good snapshot, records attempts, exposes status and inspection, and
notifies observers.

`Manager.LoadFromSource` is the normal path. It reads from the source, runs the
pipeline, publishes the snapshot on success, records the attempt, and returns
both the load result and apply result.

## What You Get Without Adapters

The root `configkit` package does not import or compile against Servekit,
Workerkit, OpenTelemetry, HTTP, Kubernetes, or a remote configuration backend.

You can directly:

- load typed config from bytes or files
- provide custom sources
- apply defaults and validation
- publish immutable-enough snapshots
- read the current typed value
- inspect safe operational state
- record reload attempts
- preserve last-known-good config on failed reloads
- attach observers

Servekit, Workerkit, and OpenTelemetry integrations are optional adapter
packages in this same Go module. Their dependencies may appear in `go.mod`
because they are part of this repository, but applications only compile adapter
packages they import.

## Next Steps

- Read [`usage.md`](usage.md) for the normal Configkit path.
- Read [`lifecycle.md`](lifecycle.md) for the full load and apply lifecycle.
- Read [`reloads.md`](reloads.md) for last-known-good behavior.
- Read [`examples.md`](examples.md) for the guided example sequence.
