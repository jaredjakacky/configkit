package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"

	configkit "github.com/jaredjakacky/configkit"
)

type AppConfig struct {
	ServiceName string `json:"service_name"`
	Port        int    `json:"port"`
}

func main() {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// SlogObserver logs lifecycle metadata such as component identity, event
	// kind, manager state, source, attempt status, stage, checksum, apply result,
	// and public failure detail. It does not log raw typed config or arbitrary
	// returned validation errors.
	manager := configkit.NewManager[AppConfig](
		configkit.WithIdentity("search-config"),
		configkit.WithObservers(configkit.SlogObserver(logger)),
	)
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

	validSource := configkit.NewBytesSource(
		[]byte(`{"service_name":"search","port":8080}`),
		configkit.SourceMetadata{Name: "initial-json", Kind: "memory"},
		"initial-v1",
	)
	if _, err := manager.LoadFromSource(ctx, configkit.AttemptKindInitialLoad, validSource, pipeline); err != nil {
		log.Fatalf("initial load: %v", err)
	}

	invalidSource := configkit.NewBytesSource(
		[]byte(`{"service_name":"search","port":0}`),
		configkit.SourceMetadata{Name: "reload-json", Kind: "memory"},
		"reload-v2",
	)
	if _, err := manager.LoadFromSource(ctx, configkit.AttemptKindReload, invalidSource, pipeline); err != nil {
		fmt.Println("reload failed as expected; see the safe observer failure fields")
	}
	fmt.Printf("final manager status: %s\n", manager.LifecycleStatus().State)
}
