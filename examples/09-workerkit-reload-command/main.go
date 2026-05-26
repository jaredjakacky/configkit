package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	configkit "github.com/jaredjakacky/configkit"
	ckworker "github.com/jaredjakacky/configkit/worker"
	workerkit "github.com/jaredjakacky/workerkit"
)

type AppConfig struct {
	ServiceName string `json:"service_name"`
	Port        int    `json:"port"`
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
	manager := configkit.NewManager[AppConfig]()
	source := newMutableSource(
		[]byte(`{"service_name":"worker-demo","port":8080}`),
		"valid-v1",
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
	if _, err := manager.LoadFromSource(ctx, configkit.AttemptKindInitialLoad, source, pipeline); err != nil {
		log.Fatalf("initial load: %v", err)
	}
	fmt.Printf("initial config loaded: state=%s\n", manager.Status().State)

	runtime, err := workerkit.New(workerkit.Identity{Name: "config-runtime"})
	if err != nil {
		log.Fatalf("create runtime: %v", err)
	}
	if err := runtime.Register(workerkit.WorkerSpec{
		Name:        "config",
		Description: "Owns operational config commands.",
		Worker:      noopWorker{},
	}, workerkit.WithCommandSpec(ckworker.ReloadCommand(manager, source, pipeline))); err != nil {
		log.Fatalf("register worker: %v", err)
	}
	if err := runtime.StartAll(ctx); err != nil {
		log.Fatalf("start runtime: %v", err)
	}
	defer shutdownRuntime(ctx, runtime)

	source.Set([]byte(`{"service_name":"worker-demo","port":9090}`), "valid-v2")
	success := dispatchReload(ctx, runtime)
	printPayload("successful reload command", success)

	source.Set([]byte(`{"service_name":"worker-demo","port":0}`), "invalid-v2")
	failure := dispatchReload(ctx, runtime)
	printPayload("failed reload command", failure)

	current, _ := manager.Value()
	fmt.Printf("current config after failed reload: %+v\n", current)
	fmt.Println("failed reloads return failure details but preserve the last-known-good config")
}

func dispatchReload(ctx context.Context, runtime *workerkit.Runtime) reloadPayload {
	result, err := runtime.Dispatch(ctx, workerkit.CommandRequest{
		Worker: "config",
		Name:   "config/reload",
	})
	if err != nil {
		log.Fatalf("dispatch reload command: %v", err)
	}

	var payload reloadPayload
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		log.Fatalf("decode reload payload: %v", err)
	}
	return payload
}

func printPayload(label string, payload reloadPayload) {
	fmt.Printf("%s:\n", label)
	fmt.Printf("  attempt_id=%d\n", payload.AttemptID)
	fmt.Printf("  attempt_status=%s\n", payload.AttemptStatus)
	fmt.Printf("  manager_state=%s\n", payload.ManagerState)
	fmt.Printf("  published=%t\n", payload.Published)
	fmt.Printf("  changed=%t\n", payload.Changed)
	fmt.Printf("  current_checksum=%s\n", payload.CurrentChecksum)
	fmt.Printf("  current_revision=%s\n", payload.CurrentRevision)
	if payload.Error != "" {
		fmt.Printf("  error=%s\n", payload.Error)
	}
}

func shutdownRuntime(ctx context.Context, runtime *workerkit.Runtime) {
	shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := runtime.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown runtime: %v", err)
	}
}

type noopWorker struct{}

func (noopWorker) Start(ctx context.Context) error {
	return nil
}

func (noopWorker) Stop(ctx context.Context) error {
	return nil
}

type mutableSource struct {
	mu       sync.RWMutex
	data     []byte
	revision string
}

func newMutableSource(data []byte, revision string) *mutableSource {
	source := &mutableSource{}
	source.Set(data, revision)
	return source
}

func (s *mutableSource) Metadata() configkit.SourceMetadata {
	return configkit.SourceMetadata{Name: "runtime-json", Kind: "memory"}
}

func (s *mutableSource) Read(ctx context.Context) (configkit.SourceData, error) {
	if err := ctx.Err(); err != nil {
		return configkit.SourceData{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return configkit.SourceData{
		Data:     append([]byte(nil), s.data...),
		Metadata: s.Metadata(),
		Revision: s.revision,
	}, nil
}

func (s *mutableSource) Set(data []byte, revision string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data = append([]byte(nil), data...)
	s.revision = revision
}
