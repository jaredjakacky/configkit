package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	configkit "github.com/jaredjakacky/configkit"
	ckworker "github.com/jaredjakacky/configkit/worker"
	workerkit "github.com/jaredjakacky/workerkit"
)

type reloadTestConfig struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port"`
}

func TestReloadCommandDefaults(t *testing.T) {
	spec := ckworker.ReloadCommand(
		configkit.NewManager[reloadTestConfig](),
		configkit.NewBytesSource([]byte(`{"name":"api","enabled":true,"port":8080}`), configkit.SourceMetadata{}, "rev-1"),
		reloadTestPipeline(),
	)

	if spec.Name != "config/reload" {
		t.Fatalf("command name = %q, want %q", spec.Name, "config/reload")
	}
	if spec.Description != "reloads Configkit configuration from source" {
		t.Fatalf("description = %q, want %q", spec.Description, "reloads Configkit configuration from source")
	}
	if spec.Handler == nil {
		t.Fatal("handler = nil, want handler")
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("validate command spec: %v", err)
	}
}

func TestReloadCommandOptions(t *testing.T) {
	spec := ckworker.ReloadCommand(
		configkit.NewManager[reloadTestConfig](),
		configkit.NewBytesSource([]byte(`{}`), configkit.SourceMetadata{}, "rev-1"),
		reloadTestPipeline(),
		ckworker.WithCommandName("admin/config/reload"),
		ckworker.WithDescription("reload config"),
		nil,
	)

	if spec.Name != "admin/config/reload" {
		t.Fatalf("command name = %q, want admin/config/reload", spec.Name)
	}
	if spec.Description != "reload config" {
		t.Fatalf("description = %q, want reload config", spec.Description)
	}
}

func TestReloadCommandEmptyNameOptionPreservesDefault(t *testing.T) {
	spec := ckworker.ReloadCommand(
		configkit.NewManager[reloadTestConfig](),
		configkit.NewBytesSource([]byte(`{}`), configkit.SourceMetadata{}, "rev-1"),
		reloadTestPipeline(),
		ckworker.WithCommandName(""),
	)

	if spec.Name != "config/reload" {
		t.Fatalf("command name = %q, want default", spec.Name)
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("validate command spec: %v", err)
	}
}

func TestReloadCommandReturnsErrorForMissingManager(t *testing.T) {
	spec := ckworker.ReloadCommand[reloadTestConfig](nil, nil, reloadTestPipeline())

	result, err := spec.Handler.HandleCommand(context.Background(), workerkit.CommandRequest{Name: spec.Name})
	if err == nil {
		t.Fatal("command error = nil, want missing manager error")
	}
	if !strings.Contains(err.Error(), "missing manager") {
		t.Fatalf("command error = %q, want missing manager", err.Error())
	}
	if result.Message != "" || result.Payload != nil {
		t.Fatalf("command result = %+v, want zero", result)
	}
}

func TestReloadCommandSuccessfulReloadPayload(t *testing.T) {
	manager := configkit.NewManager[reloadTestConfig]()
	source := configkit.NewBytesSource(
		[]byte(`{"name":"api","enabled":true,"port":8080}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)
	spec := ckworker.ReloadCommand(manager, source, reloadTestPipeline())

	result, err := spec.Handler.HandleCommand(context.Background(), workerkit.CommandRequest{Name: spec.Name})
	if err != nil {
		t.Fatalf("command error: %v", err)
	}
	if result.Message != "config reload succeeded" {
		t.Fatalf("message = %q, want success message", result.Message)
	}

	payload := decodeReloadPayload(t, result.Payload)
	if payload.AttemptID == 0 {
		t.Fatal("attempt id = 0, want manager-assigned id")
	}
	if payload.AttemptStatus != configkit.AttemptStatusSucceeded {
		t.Fatalf("attempt status = %q, want succeeded", payload.AttemptStatus)
	}
	if payload.ManagerState != configkit.StatusStateLoaded {
		t.Fatalf("manager state = %q, want loaded", payload.ManagerState)
	}
	if !payload.Published {
		t.Fatal("published = false, want true")
	}
	if !payload.Changed {
		t.Fatal("changed = false, want true")
	}
	if payload.CurrentChecksum == "" {
		t.Fatal("current checksum = empty, want checksum")
	}
	if payload.CurrentRevision != "rev-1" {
		t.Fatalf("current revision = %q, want rev-1", payload.CurrentRevision)
	}
	if payload.Error != "" {
		t.Fatalf("error = %q, want empty", payload.Error)
	}
}

func TestReloadCommandFailedReloadReturnsResultWithoutCommandError(t *testing.T) {
	manager := configkit.NewManager[reloadTestConfig]()
	initialSource := configkit.NewBytesSource(
		[]byte(`{"name":"api","enabled":true,"port":8080}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)
	if _, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindInitialLoad, initialSource, reloadTestPipeline()); err != nil {
		t.Fatalf("initial load: %v", err)
	}
	failingSource := configkit.NewBytesSource(
		[]byte(`{"name":`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-2",
	)
	spec := ckworker.ReloadCommand(manager, failingSource, reloadTestPipeline())

	result, err := spec.Handler.HandleCommand(context.Background(), workerkit.CommandRequest{Name: spec.Name})
	if err != nil {
		t.Fatalf("command error = %v, want nil", err)
	}
	if result.Message != "config reload failed" {
		t.Fatalf("message = %q, want failure message", result.Message)
	}

	payload := decodeReloadPayload(t, result.Payload)
	if payload.AttemptStatus != configkit.AttemptStatusFailed {
		t.Fatalf("attempt status = %q, want failed", payload.AttemptStatus)
	}
	if payload.ManagerState != configkit.StatusStateDegraded {
		t.Fatalf("manager state = %q, want degraded", payload.ManagerState)
	}
	if payload.Published {
		t.Fatal("published = true, want false")
	}
	if payload.Changed {
		t.Fatal("changed = true, want false")
	}
	if payload.CurrentChecksum == "" {
		t.Fatal("current checksum = empty, want last-known-good checksum")
	}
	if payload.CurrentRevision != "rev-1" {
		t.Fatalf("current revision = %q, want rev-1", payload.CurrentRevision)
	}
	if payload.Error == "" {
		t.Fatal("error = empty, want load failure details")
	}
}

func TestReloadCommandValidationFailureReturnsResultWithoutCommandError(t *testing.T) {
	manager := configkit.NewManager[reloadTestConfig]()
	initialSource := configkit.NewBytesSource(
		[]byte(`{"name":"api","enabled":true,"port":8080}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)
	if _, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindInitialLoad, initialSource, reloadTestPipeline()); err != nil {
		t.Fatalf("initial load: %v", err)
	}
	failingSource := configkit.NewBytesSource(
		[]byte(`{"name":"","enabled":true,"port":8080}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-2",
	)
	spec := ckworker.ReloadCommand(manager, failingSource, reloadTestPipeline())

	result, err := spec.Handler.HandleCommand(context.Background(), workerkit.CommandRequest{Name: spec.Name})
	if err != nil {
		t.Fatalf("command error = %v, want nil", err)
	}

	payload := decodeReloadPayload(t, result.Payload)
	if payload.AttemptStatus != configkit.AttemptStatusFailed {
		t.Fatalf("attempt status = %q, want failed", payload.AttemptStatus)
	}
	if payload.ManagerState != configkit.StatusStateDegraded {
		t.Fatalf("manager state = %q, want degraded", payload.ManagerState)
	}
	if payload.Published {
		t.Fatal("published = true, want false")
	}
	if payload.Changed {
		t.Fatal("changed = true, want false")
	}
	if payload.CurrentChecksum == "" {
		t.Fatal("current checksum = empty, want last-known-good checksum")
	}
	if payload.CurrentRevision != "rev-1" {
		t.Fatalf("current revision = %q, want rev-1", payload.CurrentRevision)
	}
	if !strings.Contains(payload.Error, "name is required") {
		t.Fatalf("error = %q, want validation failure details", payload.Error)
	}
}

func TestReloadCommandMissingSourceReturnsFailurePayload(t *testing.T) {
	manager := configkit.NewManager[reloadTestConfig]()
	spec := ckworker.ReloadCommand(manager, nil, reloadTestPipeline())

	result, err := spec.Handler.HandleCommand(context.Background(), workerkit.CommandRequest{Name: spec.Name})
	if err != nil {
		t.Fatalf("command error = %v, want nil", err)
	}
	payload := decodeReloadPayload(t, result.Payload)
	if payload.AttemptStatus != configkit.AttemptStatusFailed {
		t.Fatalf("attempt status = %q, want failed", payload.AttemptStatus)
	}
	if payload.ManagerState != configkit.StatusStateFailed {
		t.Fatalf("manager state = %q, want failed", payload.ManagerState)
	}
	if !strings.Contains(payload.Error, configkit.ErrMissingSource.Error()) {
		t.Fatalf("payload error = %q, want missing source", payload.Error)
	}
}

func TestReloadCommandReturnsCommandErrorOnContextCanceled(t *testing.T) {
	manager := configkit.NewManager[reloadTestConfig]()
	source := configkit.NewBytesSource(
		[]byte(`{"name":"api","enabled":true,"port":8080}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)
	spec := ckworker.ReloadCommand(manager, source, reloadTestPipeline())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := spec.Handler.HandleCommand(ctx, workerkit.CommandRequest{Name: spec.Name})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("command error = %v, want context.Canceled", err)
	}
	if result.Message != "" || result.Payload != nil {
		t.Fatalf("command result = %+v, want zero", result)
	}
}

func TestReloadCommandReturnsCommandErrorOnContextDeadlineExceeded(t *testing.T) {
	manager := configkit.NewManager[reloadTestConfig]()
	source := configkit.NewBytesSource(
		[]byte(`{"name":"api","enabled":true,"port":8080}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)
	spec := ckworker.ReloadCommand(manager, source, reloadTestPipeline())

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	result, err := spec.Handler.HandleCommand(ctx, workerkit.CommandRequest{Name: spec.Name})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("command error = %v, want context.DeadlineExceeded", err)
	}
	if result.Message != "" || result.Payload != nil {
		t.Fatalf("command result = %+v, want zero", result)
	}
}

func reloadTestPipeline() configkit.Pipeline[reloadTestConfig] {
	return configkit.Pipeline[reloadTestConfig]{
		Decode: configkit.JSONDecoder[reloadTestConfig](),
		ValidateConfig: func(ctx context.Context, cfg reloadTestConfig) error {
			if cfg.Name == "" {
				return errors.New("name is required")
			}
			return nil
		},
		Redact:   configkit.EmptyRedactor[reloadTestConfig](),
		Checksum: configkit.SHA256JSONChecksum[reloadTestConfig](),
	}
}

type reloadResultPayload struct {
	AttemptID       uint64                  `json:"attempt_id,omitempty"`
	AttemptStatus   configkit.AttemptStatus `json:"attempt_status"`
	ManagerState    configkit.StatusState   `json:"manager_state"`
	Published       bool                    `json:"published"`
	Changed         bool                    `json:"changed"`
	CurrentChecksum string                  `json:"current_checksum,omitempty"`
	CurrentRevision string                  `json:"current_revision,omitempty"`
	Error           string                  `json:"error,omitempty"`
}

func decodeReloadPayload(t *testing.T, data []byte) reloadResultPayload {
	t.Helper()

	var payload reloadResultPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode reload payload: %v", err)
	}
	return payload
}
