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
}

func main() {
	ctx := context.Background()
	manager := configkit.NewManager[AppConfig]()
	pipeline := configkit.Pipeline[AppConfig]{
		Decode: configkit.JSONDecoder[AppConfig](),
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

	initial := load(ctx, manager, configkit.AttemptKindInitialLoad, "initial", []byte(`{"service_name":"catalog","port":8080}`), pipeline)
	same := load(ctx, manager, configkit.AttemptKindReload, "same", []byte(`{"service_name":"catalog","port":8080}`), pipeline)
	changed := load(ctx, manager, configkit.AttemptKindReload, "changed", []byte(`{"service_name":"catalog","port":9090}`), pipeline)

	requireApply("initial", initial, true, true)
	requireApply("same reload", same, true, false)
	requireApply("changed reload", changed, true, true)

	fmt.Println("successful reloads publish fresh snapshots")
	fmt.Println("changed=false means the effective config checksum did not change")
	fmt.Println("changed=true means the effective config checksum changed")
	printResult("initial", initial)
	printResult("same reload", same)
	printResult("changed reload", changed)
}

func load(ctx context.Context, manager *configkit.Manager[AppConfig], kind configkit.AttemptKind, revision string, data []byte, pipeline configkit.Pipeline[AppConfig]) configkit.ManagedLoadResult[AppConfig] {
	source := configkit.NewBytesSource(data, configkit.SourceMetadata{Name: revision, Kind: "memory"}, revision)
	result, err := manager.LoadFromSource(ctx, kind, source, pipeline)
	if err != nil {
		log.Fatalf("load %s: %v", revision, err)
	}
	return result
}

func requireApply(label string, result configkit.ManagedLoadResult[AppConfig], published bool, changed bool) {
	if result.Apply.Published != published {
		log.Fatalf("%s published = %t, want %t", label, result.Apply.Published, published)
	}
	if result.Apply.Changed != changed {
		log.Fatalf("%s changed = %t, want %t", label, result.Apply.Changed, changed)
	}
}

func printResult(label string, result configkit.ManagedLoadResult[AppConfig]) {
	checksum := ""
	if result.Apply.Current != nil {
		checksum = result.Apply.Current.Checksum
	}
	fmt.Printf("%s: attempt_id=%d published=%t changed=%t checksum=%s\n",
		label,
		result.Load.Attempt.ID,
		result.Apply.Published,
		result.Apply.Changed,
		checksum,
	)
}
