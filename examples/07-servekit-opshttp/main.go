package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	configkit "github.com/jaredjakacky/configkit"
	opshttp "github.com/jaredjakacky/configkit/opshttp"
	servekit "github.com/jaredjakacky/servekit"
)

type AppConfig struct {
	ServiceName string `json:"service_name"`
	Port        int    `json:"port"`
	Environment string `json:"environment"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	manager := configkit.NewManager[AppConfig]()
	source := configkit.NewBytesSource(
		[]byte(`{"service_name":"frontend","port":8080,"environment":"demo"}`),
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
			return nil
		},
		Redact: func(ctx context.Context, cfg AppConfig) (configkit.RedactedView, error) {
			return configkit.RedactedView{
				"service_name": cfg.ServiceName,
				"port":         cfg.Port,
				"environment":  cfg.Environment,
			}, nil
		},
		Checksum: configkit.SHA256JSONChecksum[AppConfig](),
	}
	if _, err := manager.LoadFromSource(ctx, configkit.AttemptKindInitialLoad, source, pipeline); err != nil {
		log.Fatalf("load config: %v", err)
	}

	server := servekit.New(servekit.WithAddr(":8087"))
	if err := opshttp.Mount(server, manager, opshttp.WithEndpointOptions(servekit.WithAuthGate(requireAdminToken))); err != nil {
		log.Fatalf("mount config ops routes: %v", err)
	}

	server.Handle(http.MethodGet, "/hello", func(r *http.Request) (any, error) {
		cfg, ok := manager.Value()
		if !ok {
			return nil, servekit.Error(http.StatusServiceUnavailable, "config not loaded", nil)
		}
		return map[string]any{
			"message":     "hello from " + cfg.ServiceName,
			"environment": cfg.Environment,
		}, nil
	})

	log.Println("servekit opshttp example listening on :8087")
	log.Println("admin routes require header X-Admin-Token: demo")
	log.Println(`try: curl -i http://127.0.0.1:8087/hello`)
	log.Println(`try: curl -i -H 'X-Admin-Token: demo' http://127.0.0.1:8087/admin/config`)
	log.Println(`try: curl -i -H 'X-Admin-Token: demo' http://127.0.0.1:8087/admin/config/attempts`)
	if err := server.Run(ctx); err != nil {
		log.Printf("serve: %v", err)
	}
}

func requireAdminToken(r *http.Request) error {
	if r.Header.Get("X-Admin-Token") == "demo" {
		return nil
	}
	return servekit.Error(http.StatusUnauthorized, "unauthorized", nil)
}
