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
	APIKey      string `json:"api_key"`
}

func main() {
	ctx := context.Background()
	source := configkit.NewBytesSource(
		[]byte(`{"service_name":"payments","port":7070,"api_key":"secret-value"}`),
		configkit.SourceMetadata{Name: "embedded-json", Kind: "memory"},
		"example-v1",
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
			if cfg.APIKey == "" {
				return errors.New("api_key is required")
			}
			return nil
		},
		Redact: func(ctx context.Context, cfg AppConfig) (configkit.RedactedView, error) {
			// RedactedView safety is application-owned: only return fields that
			// are safe for logs, status pages, and operational endpoints.
			return configkit.RedactedView{
				"service_name":       cfg.ServiceName,
				"port":               cfg.Port,
				"api_key_configured": cfg.APIKey != "",
			}, nil
		},
		Checksum: configkit.SHA256JSONChecksum[AppConfig](),
	}
	manager := configkit.NewManager[AppConfig]()

	if _, err := manager.LoadFromSource(ctx, configkit.AttemptKindInitialLoad, source, pipeline); err != nil {
		log.Fatal(err)
	}
	if status := manager.Status(); status.State != configkit.StatusStateLoaded {
		log.Fatalf("manager state = %s, want %s", status.State, configkit.StatusStateLoaded)
	}

	inspection := manager.Inspect()
	fmt.Printf("manager status: %s\n", inspection.Status.State)
	fmt.Printf("redacted inspection: %+v\n", inspection.Redacted)
}
