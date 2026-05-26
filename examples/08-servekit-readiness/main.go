package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"

	configkit "github.com/jaredjakacky/configkit"
	opshttp "github.com/jaredjakacky/configkit/opshttp"
	servekit "github.com/jaredjakacky/servekit"
)

type AppConfig struct {
	ServiceName string `json:"service_name"`
	Port        int    `json:"port"`
}

func main() {
	ctx := context.Background()
	manager := configkit.NewManager[AppConfig]()
	server := servekit.New(servekit.WithReadinessChecks(opshttp.ReadinessCheck(manager)))
	server.SetReady(true)

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

	fmt.Printf("before initial load: %s\n", readyzStatus(server))

	validSource := configkit.NewBytesSource(
		[]byte(`{"service_name":"frontend","port":8080}`),
		configkit.SourceMetadata{Name: "initial-json", Kind: "memory"},
		"initial-v1",
	)
	if _, err := manager.LoadFromSource(ctx, configkit.AttemptKindInitialLoad, validSource, pipeline); err != nil {
		log.Fatalf("initial load: %v", err)
	}
	fmt.Printf("after successful load: %s\n", readyzStatus(server))

	invalidSource := configkit.NewBytesSource(
		[]byte(`{"service_name":"frontend","port":0}`),
		configkit.SourceMetadata{Name: "reload-json", Kind: "memory"},
		"reload-v2",
	)
	if _, err := manager.LoadFromSource(ctx, configkit.AttemptKindReload, invalidSource, pipeline); err != nil {
		fmt.Printf("failed reload: %v\n", err)
	} else {
		log.Fatal("reload error = nil, want validation error")
	}
	fmt.Printf("after failed reload with last-known-good: %s\n", readyzStatus(server))

	// Degraded is ready by default because a valid last-known-good snapshot is
	// still active. Use WithDegradedReady(false) for stricter services.
	strictCheck := opshttp.ReadinessCheck(manager, opshttp.WithDegradedReady(false))
	if err := strictCheck(ctx); err != nil {
		fmt.Printf("strict degraded readiness: not ready (%v)\n", err)
	}
}

func readyzStatus(server *servekit.Server) string {
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		return "ready"
	}
	return "not ready"
}
