package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	configkit "github.com/jaredjakacky/configkit"
	ckops "github.com/jaredjakacky/configkit/opshttp"
	ckworker "github.com/jaredjakacky/configkit/worker"
	servekit "github.com/jaredjakacky/servekit"
	workerkit "github.com/jaredjakacky/workerkit"
	wkops "github.com/jaredjakacky/workerkit/opshttp"
)

type AppConfig struct {
	ServiceName string `json:"service_name"`
	Port        int    `json:"port"`
	Message     string `json:"message"`
	APIKey      string `json:"api_key"`
}

type reloadPayload struct {
	AttemptID       uint64                  `json:"attempt_id,omitempty"`
	AttemptStatus   configkit.AttemptStatus `json:"attempt_status"`
	ManagerState    configkit.StatusState   `json:"manager_state"`
	Published       bool                    `json:"published"`
	Changed         bool                    `json:"changed"`
	CurrentChecksum string                  `json:"current_checksum,omitempty"`
	CurrentRevision string                  `json:"current_revision,omitempty"`
	Error           string                  `json:"error,omitempty"`
}

func main() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "configkit-production-*")
	if err != nil {
		log.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	configPath := filepath.Join(dir, "app-config.json")
	writeConfig(configPath, AppConfig{
		ServiceName: "production-demo",
		Port:        8080,
		Message:     "hello from initial config",
		APIKey:      "initial-secret",
	})

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	manager := configkit.NewManager[AppConfig](configkit.WithObservers(configkit.SlogObserver(logger)))
	source := configkit.NewFileSource(configPath, configkit.SourceMetadata{Name: "app-config.json", Kind: "file"})
	pipeline := appPipeline()

	if _, err := manager.LoadFromSource(ctx, configkit.AttemptKindInitialLoad, source, pipeline); err != nil {
		log.Fatalf("initial config load: %v", err)
	}
	fmt.Printf("1. service starts with valid config: status=%s\n", manager.Status().State)

	runtime := newWorkerRuntime(ctx, manager, source, pipeline)
	defer shutdownRuntime(ctx, runtime)

	auth := servekit.WithAuthGate(requireAdminToken)
	server := servekit.New(
		servekit.WithReadinessChecks(ckops.ReadinessCheck(manager), wkops.ReadinessCheck(runtime)),
	)
	server.SetReady(true)

	server.Handle(http.MethodGet, "/message", func(r *http.Request) (any, error) {
		cfg, ok := manager.Value()
		if !ok {
			return nil, servekit.Error(http.StatusServiceUnavailable, "config not loaded", nil)
		}
		return map[string]any{
			"service": cfg.ServiceName,
			"message": cfg.Message,
		}, nil
	})
	if err := ckops.Mount(server, manager, ckops.WithEndpointOptions(auth)); err != nil {
		log.Fatalf("mount config ops routes: %v", err)
	}
	if err := wkops.Mount(server, runtime, wkops.WithPrefix("/admin/workers"), wkops.WithEndpointOptions(auth), wkops.WithCommandDispatchEnabled()); err != nil {
		log.Fatalf("mount worker ops routes: %v", err)
	}

	fmt.Printf("2. /message uses typed config: %s\n", get(server, "/message", ""))
	fmt.Printf("3. /admin/config exposes safe inspection: %s\n", get(server, "/admin/config", "demo"))
	fmt.Printf("   worker command discovery: %s\n", get(server, "/admin/workers/commands?worker=config", "demo"))

	writeConfig(configPath, AppConfig{
		ServiceName: "production-demo",
		Port:        8080,
		Message:     "hello from changed config",
		APIKey:      "changed-secret",
	})
	changed := dispatchReload(ctx, runtime)
	fmt.Printf("4. reload command applies changed config: %+v\n", changed)
	fmt.Printf("   /message after reload: %s\n", get(server, "/message", ""))

	writeConfig(configPath, AppConfig{
		ServiceName: "production-demo",
		Port:        0,
		Message:     "this invalid config is not published",
		APIKey:      "invalid-secret",
	})
	failed := dispatchReload(ctx, runtime)
	current, _ := manager.Value()
	fmt.Printf("5. failed reload preserves last-known-good: %+v\n", failed)
	fmt.Printf("   current typed config: service=%s port=%d message=%q\n", current.ServiceName, current.Port, current.Message)
	fmt.Printf("6. status becomes degraded: %s\n", manager.Status().State)
	fmt.Printf("   /readyz remains ready by default: %s\n", get(server, "/readyz", ""))
}

func appPipeline() configkit.Pipeline[AppConfig] {
	return configkit.Pipeline[AppConfig]{
		Decode: configkit.JSONDecoder[AppConfig](),
		ApplyDefaults: func(ctx context.Context, cfg AppConfig) (AppConfig, error) {
			if cfg.Message == "" {
				cfg.Message = "hello"
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
			if cfg.APIKey == "" {
				return errors.New("api_key is required")
			}
			return nil
		},
		Redact: func(ctx context.Context, cfg AppConfig) (configkit.RedactedView, error) {
			return configkit.RedactedView{
				"service_name":       cfg.ServiceName,
				"port":               cfg.Port,
				"message":            cfg.Message,
				"api_key_configured": cfg.APIKey != "",
			}, nil
		},
		Checksum: configkit.SHA256JSONChecksum[AppConfig](),
	}
}

func requireAdminToken(r *http.Request) error {
	if r.Header.Get("X-Admin-Token") == "demo" {
		return nil
	}
	return servekit.Error(http.StatusUnauthorized, "unauthorized", nil)
}

func newWorkerRuntime(ctx context.Context, manager *configkit.Manager[AppConfig], source configkit.Source, pipeline configkit.Pipeline[AppConfig]) *workerkit.Runtime {
	runtime, err := workerkit.New(workerkit.Identity{Name: "production"})
	if err != nil {
		log.Fatalf("create worker runtime: %v", err)
	}
	if err := runtime.Register(workerkit.WorkerSpec{
		Name:        "config",
		Description: "Owns configuration reload commands.",
		Worker:      configWorker{},
	}, workerkit.WithCommandSpec(ckworker.ReloadCommand(manager, source, pipeline))); err != nil {
		log.Fatalf("register config worker: %v", err)
	}
	if err := runtime.StartAll(ctx); err != nil {
		log.Fatalf("start worker runtime: %v", err)
	}
	return runtime
}

type configWorker struct{}

func (configWorker) Start(ctx context.Context) error {
	runtime, ok := workerkit.WorkerRuntimeFromContext(ctx)
	if !ok {
		return errors.New("worker runtime handle unavailable")
	}
	return runtime.SetReady(true)
}

func (configWorker) Stop(ctx context.Context) error {
	return nil
}

func dispatchReload(ctx context.Context, runtime *workerkit.Runtime) reloadPayload {
	result, err := runtime.Dispatch(ctx, workerkit.CommandRequest{Worker: "config", Name: "config/reload"})
	if err != nil {
		log.Fatalf("dispatch config reload: %v", err)
	}

	var payload reloadPayload
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		log.Fatalf("decode reload payload: %v", err)
	}
	return payload
}

func get(server *servekit.Server, target string, token string) string {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if token != "" {
		req.Header.Set("X-Admin-Token", token)
	}
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	return compact(rec.Body.Bytes())
}

func writeConfig(path string, cfg AppConfig) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		log.Fatalf("encode config file: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		log.Fatalf("write config file: %v", err)
	}
}

func compact(data []byte) string {
	var out bytes.Buffer
	if err := json.Compact(&out, data); err != nil {
		return string(data)
	}
	return out.String()
}

func shutdownRuntime(ctx context.Context, runtime *workerkit.Runtime) {
	shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := runtime.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown worker runtime: %v", err)
	}
}
