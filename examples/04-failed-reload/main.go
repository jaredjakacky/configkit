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
			if cfg.Port <= 0 {
				return fmt.Errorf("port must be positive: %d", cfg.Port)
			}
			return nil
		},
		Redact:   configkit.EmptyRedactor[AppConfig](),
		Checksum: configkit.SHA256JSONChecksum[AppConfig](),
	}

	validSource := configkit.NewBytesSource(
		[]byte(`{"service_name":"orders","port":8080}`),
		configkit.SourceMetadata{Name: "valid-json", Kind: "memory"},
		"valid-v1",
	)
	if _, err := manager.LoadFromSource(ctx, configkit.AttemptKindInitialLoad, validSource, pipeline); err != nil {
		log.Fatal(err)
	}
	if status := manager.Status(); status.State != configkit.StatusStateLoaded {
		log.Fatalf("initial manager state = %s, want %s", status.State, configkit.StatusStateLoaded)
	}

	invalidSource := configkit.NewBytesSource(
		[]byte(`{"service_name":"orders","port":0}`),
		configkit.SourceMetadata{Name: "invalid-json", Kind: "memory"},
		"invalid-v2",
	)
	_, reloadErr := manager.LoadFromSource(ctx, configkit.AttemptKindReload, invalidSource, pipeline)
	if reloadErr == nil {
		log.Fatal("reload error = nil, want validation error")
	}

	status := manager.Status()
	if status.State != configkit.StatusStateDegraded {
		log.Fatalf("manager state = %s, want %s", status.State, configkit.StatusStateDegraded)
	}
	current, ok := manager.Value()
	if !ok {
		log.Fatal("manager has no current config")
	}
	if current != (AppConfig{ServiceName: "orders", Port: 8080}) {
		log.Fatalf("current config = %+v, want original valid config", current)
	}

	fmt.Printf("reload error: %v\n", reloadErr)
	fmt.Printf("manager status: %s\n", status.State)
	if status.LastFailure != nil {
		fmt.Printf("last failure: stage=%s error=%s\n", status.LastFailure.Stage, status.LastFailure.Error)
	}
	fmt.Printf("current config: %+v\n", current)
	fmt.Println("current config remains the original valid snapshot")
}
