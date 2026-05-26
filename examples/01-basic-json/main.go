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

	if _, err := manager.LoadFromSource(ctx, configkit.AttemptKindInitialLoad, source, pipeline); err != nil {
		log.Fatal(err)
	}

	status := manager.Status()
	if status.State != configkit.StatusStateLoaded {
		log.Fatalf("manager state = %s, want %s", status.State, configkit.StatusStateLoaded)
	}

	current, ok := manager.Value()
	if !ok {
		log.Fatal("manager has no current config")
	}

	fmt.Printf("current config: %+v\n", current)
	fmt.Printf("status state: %s\n", status.State)
	if status.Current != nil {
		fmt.Printf("current checksum: %s\n", status.Current.Checksum)
	}
}
