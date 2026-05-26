package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	configkit "github.com/jaredjakacky/configkit"
)

type AppConfig struct {
	ServiceName string `json:"service_name"`
	Port        int    `json:"port"`
	Environment string `json:"environment"`
}

func main() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "configkit-file-source-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "app-config.json")
	if err := os.WriteFile(path, []byte(`{"service_name":"billing","port":9090}`), 0o600); err != nil {
		log.Fatal(err)
	}

	source := configkit.NewFileSource(path, configkit.SourceMetadata{Name: "app-config.json", Kind: "file"})
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
	status := manager.Status()
	if status.State != configkit.StatusStateLoaded {
		log.Fatalf("manager state = %s, want %s", status.State, configkit.StatusStateLoaded)
	}
	current, ok := manager.Value()
	if !ok {
		log.Fatal("manager has no current config")
	}
	metadata := result.Load.Snapshot.Metadata()

	fmt.Printf("loaded config: %+v\n", current)
	fmt.Printf("source: %s/%s\n", metadata.Source.Kind, metadata.Source.Name)
	fmt.Printf("source revision: %s\n", metadata.Revision)
	fmt.Printf("snapshot checksum: %s\n", metadata.Checksum)
	fmt.Printf("loaded_at: %s\n", metadata.LoadedAt.Format("2006-01-02T15:04:05Z07:00"))
	fmt.Println("file source revisions are SHA-256 fingerprints of the file bytes")
}
