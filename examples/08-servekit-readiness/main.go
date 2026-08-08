package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"

	configkit "github.com/jaredjakacky/configkit"
	opskit "github.com/jaredjakacky/opskit"
	servekit "github.com/jaredjakacky/servekit"
)

type AppConfig struct {
	ServiceName string `json:"service_name"`
	Port        int    `json:"port"`
}

func main() {
	ctx := context.Background()
	ops := opskit.NewRegistry()
	manager := configkit.NewManager[AppConfig](configkit.WithIdentity("config"))
	ops.MustRegister(manager, opskit.Required())

	server := servekit.New(servekit.WithOps(ops, servekit.WithOpsAdmin()))
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
		fmt.Println("failed reload recorded with safe operational failure detail")
	} else {
		log.Fatal("reload error = nil, want validation error")
	}
	fmt.Printf("after failed reload with last-known-good: %s\n", readyzStatus(server))

	// Degraded is ready by default because a valid last-known-good snapshot is
	// still active. Use configkit.WithDegradedReady(false) for stricter services.
	strictManager := configkit.NewManager[AppConfig](configkit.WithDegradedReady(false))
	strictOps := opskit.NewRegistry()
	strictOps.MustRegister(strictManager, opskit.Required())
	strictServer := servekit.New(servekit.WithOps(strictOps))
	strictServer.SetReady(true)
	if _, err := strictManager.LoadFromSource(ctx, configkit.AttemptKindInitialLoad, validSource, pipeline); err != nil {
		log.Fatalf("strict initial load: %v", err)
	}
	if _, err := strictManager.LoadFromSource(ctx, configkit.AttemptKindReload, invalidSource, pipeline); err == nil {
		log.Fatal("strict reload error = nil, want validation error")
	}
	fmt.Printf("strict degraded readiness: %s\n", readyzStatus(strictServer))
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
