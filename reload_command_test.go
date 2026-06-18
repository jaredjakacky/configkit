package configkit_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	configkit "github.com/jaredjakacky/configkit"
	opskit "github.com/jaredjakacky/opskit"
)

type reloadCommandTestConfig struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port"`
}

func TestReloadCommandDefaults(t *testing.T) {
	command := configkit.ReloadCommand(
		configkit.NewManager[reloadCommandTestConfig](),
		configkit.NewBytesSource([]byte(`{"name":"api","enabled":true,"port":8080}`), configkit.SourceMetadata{}, "rev-1"),
		reloadCommandTestPipeline(),
	)

	info := command.ComponentInfo()
	if info.Name != "config-reload" {
		t.Fatalf("component name = %q, want config-reload", info.Name)
	}
	if info.Kind != "command_handler" {
		t.Fatalf("component kind = %q, want command_handler", info.Kind)
	}
	if command.Status(context.Background()).State != opskit.StateReady {
		t.Fatalf("status = %+v, want ready", command.Status(context.Background()))
	}

	commands := command.Commands(context.Background())
	if len(commands) != 1 {
		t.Fatalf("command count = %d, want 1", len(commands))
	}
	if commands[0].Name != "config/reload" {
		t.Fatalf("command name = %q, want config/reload", commands[0].Name)
	}
	if commands[0].Description != "reloads Configkit configuration from source" {
		t.Fatalf("description = %q, want default", commands[0].Description)
	}
	if !commands[0].Dangerous {
		t.Fatal("dangerous = false, want true")
	}
	if commands[0].Idempotent {
		t.Fatal("idempotent = true, want false")
	}
}

func TestReloadCommandOptions(t *testing.T) {
	command := configkit.ReloadCommand(
		configkit.NewManager[reloadCommandTestConfig](),
		configkit.NewBytesSource([]byte(`{}`), configkit.SourceMetadata{}, "rev-1"),
		reloadCommandTestPipeline(),
		configkit.WithReloadCommandName("admin/config/reload"),
		configkit.WithReloadCommandDescription("reload config"),
		configkit.WithReloadCommandComponentInfo(opskit.ComponentInfo{
			Name:        "admin-config-reload",
			Kind:        "admin_command",
			Description: "admin reload command",
			Labels:      []opskit.Attribute{opskit.Attr("owner", "platform")},
		}),
	)

	if got := command.ComponentInfo().Name; got != "admin-config-reload" {
		t.Fatalf("component name = %q, want admin-config-reload", got)
	}
	commands := command.Commands(context.Background())
	if commands[0].Name != "admin/config/reload" {
		t.Fatalf("command name = %q, want admin/config/reload", commands[0].Name)
	}
	if commands[0].Description != "reload config" {
		t.Fatalf("description = %q, want reload config", commands[0].Description)
	}
}

func TestReloadCommandUnsupportedNameRejected(t *testing.T) {
	command := configkit.ReloadCommand(
		configkit.NewManager[reloadCommandTestConfig](),
		configkit.NewBytesSource([]byte(`{"name":"api","enabled":true,"port":8080}`), configkit.SourceMetadata{}, "rev-1"),
		reloadCommandTestPipeline(),
	)

	result := command.HandleCommand(context.Background(), opskit.NewCommandRequest("other/reload", nil))
	if result.Accepted {
		t.Fatalf("accepted = true, want false")
	}
	if result.State != opskit.StateNotReady {
		t.Fatalf("state = %s, want not ready", result.State)
	}
	if result.Result != nil {
		t.Fatalf("result = %+v, want nil", result.Result)
	}
}

func TestReloadCommandSuccessfulReloadPayload(t *testing.T) {
	manager := configkit.NewManager[reloadCommandTestConfig]()
	source := configkit.NewBytesSource(
		[]byte(`{"name":"api","enabled":true,"port":8080}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)
	command := configkit.ReloadCommand(manager, source, reloadCommandTestPipeline())

	result := command.HandleCommand(context.Background(), opskit.NewCommandRequest("config/reload", nil))
	if result.State != opskit.StateReady {
		t.Fatalf("state = %s, want ready", result.State)
	}
	if !result.Accepted {
		t.Fatal("accepted = false, want true")
	}
	if result.Message != "config reload succeeded" {
		t.Fatalf("message = %q, want success message", result.Message)
	}

	payload := reloadCommandPayload(t, result)
	if payload.AttemptID == 0 {
		t.Fatal("attempt id = 0, want manager-assigned id")
	}
	if payload.AttemptStatus != configkit.AttemptStatusSucceeded {
		t.Fatalf("attempt status = %q, want succeeded", payload.AttemptStatus)
	}
	if payload.ManagerState != configkit.LifecycleStateLoaded {
		t.Fatalf("manager state = %q, want loaded", payload.ManagerState)
	}
	if !payload.Published || !payload.Changed {
		t.Fatalf("published/changed = %v/%v, want true/true", payload.Published, payload.Changed)
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

func TestReloadCommandFailedReloadReturnsCompletedResult(t *testing.T) {
	manager := loadedReloadCommandManager(t)
	failingSource := configkit.NewBytesSource(
		[]byte(`{"name":`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-2",
	)
	command := configkit.ReloadCommand(manager, failingSource, reloadCommandTestPipeline())

	result := command.HandleCommand(context.Background(), opskit.NewCommandRequest("config/reload", nil))
	if result.State != opskit.StateReady {
		t.Fatalf("state = %s, want ready completed command", result.State)
	}
	if !result.Accepted {
		t.Fatal("accepted = false, want true")
	}
	if result.Message != "config reload failed" {
		t.Fatalf("message = %q, want failure message", result.Message)
	}

	payload := reloadCommandPayload(t, result)
	if payload.AttemptStatus != configkit.AttemptStatusFailed {
		t.Fatalf("attempt status = %q, want failed", payload.AttemptStatus)
	}
	if payload.ManagerState != configkit.LifecycleStateDegraded {
		t.Fatalf("manager state = %q, want degraded", payload.ManagerState)
	}
	if payload.Published || payload.Changed {
		t.Fatalf("published/changed = %v/%v, want false/false", payload.Published, payload.Changed)
	}
	if payload.CurrentRevision != "rev-1" {
		t.Fatalf("current revision = %q, want last-known-good rev-1", payload.CurrentRevision)
	}
	if payload.Error == "" {
		t.Fatal("error = empty, want failure details")
	}
}

func TestReloadCommandContextCanceledReturnsFailedCommand(t *testing.T) {
	manager := configkit.NewManager[reloadCommandTestConfig]()
	source := configkit.NewBytesSource(
		[]byte(`{"name":"api","enabled":true,"port":8080}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)
	command := configkit.ReloadCommand(manager, source, reloadCommandTestPipeline())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := command.HandleCommand(ctx, opskit.NewCommandRequest("config/reload", nil))
	if result.State != opskit.StateFailed {
		t.Fatalf("state = %s, want failed", result.State)
	}
	if !result.Accepted {
		t.Fatal("accepted = false, want true")
	}
	if result.Error != context.Canceled.Error() {
		t.Fatalf("error = %q, want context canceled", result.Error)
	}
	if result.Result != nil {
		t.Fatalf("result = %+v, want nil", result.Result)
	}
}

func TestReloadCommandContextDeadlineReturnsFailedCommand(t *testing.T) {
	manager := configkit.NewManager[reloadCommandTestConfig]()
	source := configkit.NewBytesSource(
		[]byte(`{"name":"api","enabled":true,"port":8080}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)
	command := configkit.ReloadCommand(manager, source, reloadCommandTestPipeline())
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	result := command.HandleCommand(ctx, opskit.NewCommandRequest("config/reload", nil))
	if result.State != opskit.StateFailed {
		t.Fatalf("state = %s, want failed", result.State)
	}
	if result.Error != context.DeadlineExceeded.Error() {
		t.Fatalf("error = %q, want deadline exceeded", result.Error)
	}
	if result.Result != nil {
		t.Fatalf("result = %+v, want nil", result.Result)
	}
}

func TestReloadCommandMissingManagerRejected(t *testing.T) {
	command := configkit.ReloadCommand(nil, nil, reloadCommandTestPipeline())

	if command.Status(context.Background()).State != opskit.StateNotReady {
		t.Fatalf("status = %+v, want not ready", command.Status(context.Background()))
	}
	result := command.HandleCommand(context.Background(), opskit.NewCommandRequest("config/reload", nil))
	if result.Accepted {
		t.Fatal("accepted = true, want false")
	}
	if result.Result != nil {
		t.Fatalf("result = %+v, want nil", result.Result)
	}
}

func TestReloadCommandMissingSourceReturnsFailurePayload(t *testing.T) {
	manager := configkit.NewManager[reloadCommandTestConfig]()
	command := configkit.ReloadCommand(manager, nil, reloadCommandTestPipeline())

	result := command.HandleCommand(context.Background(), opskit.NewCommandRequest("config/reload", nil))
	if result.State != opskit.StateReady {
		t.Fatalf("state = %s, want completed command", result.State)
	}
	payload := reloadCommandPayload(t, result)
	if payload.AttemptStatus != configkit.AttemptStatusFailed {
		t.Fatalf("attempt status = %q, want failed", payload.AttemptStatus)
	}
	if payload.ManagerState != configkit.LifecycleStateFailed {
		t.Fatalf("manager state = %q, want failed", payload.ManagerState)
	}
	if !strings.Contains(payload.Error, configkit.ErrMissingSource.Error()) {
		t.Fatalf("payload error = %q, want missing source", payload.Error)
	}
}

func TestReloadCommandPanicPayloadIsSanitized(t *testing.T) {
	const secret = "postgres://user:pass@example/db"
	manager := configkit.NewManager[reloadCommandTestConfig]()
	source := configkit.NewBytesSource(
		[]byte(`{"name":"api","enabled":true,"port":8080}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)
	pipeline := reloadCommandTestPipeline()
	pipeline.ValidateConfig = func(ctx context.Context, cfg reloadCommandTestConfig) error {
		panic(errors.New(secret))
	}
	command := configkit.ReloadCommand(manager, source, pipeline)

	result := command.HandleCommand(context.Background(), opskit.NewCommandRequest("config/reload", nil))
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal command result: %v", err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("command result = %s, must not contain %q", encoded, secret)
	}
	payload := reloadCommandPayload(t, result)
	if payload.Error != "validate config panicked" {
		t.Fatalf("payload error = %q, want safe panic message", payload.Error)
	}
}

func TestReloadCommandPayloadDoesNotIncludeConfigOrRedactedInspection(t *testing.T) {
	manager := configkit.NewManager[reloadCommandTestConfig]()
	source := configkit.NewBytesSource(
		[]byte(`{"name":"api","enabled":true,"port":8080}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)
	pipeline := reloadCommandTestPipeline()
	pipeline.Redact = func(ctx context.Context, cfg reloadCommandTestConfig) (configkit.RedactedView, error) {
		return configkit.RedactedView{"redacted_name": cfg.Name}, nil
	}
	command := configkit.ReloadCommand(manager, source, pipeline)

	result := command.HandleCommand(context.Background(), opskit.NewCommandRequest("config/reload", nil))
	encoded, err := json.Marshal(result.Result)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	body := string(encoded)
	for _, unsafe := range []string{"api", "enabled", "port", "redacted_name"} {
		if strings.Contains(body, unsafe) {
			t.Fatalf("payload = %s, must not contain %q", body, unsafe)
		}
	}
}

func loadedReloadCommandManager(t *testing.T) *configkit.Manager[reloadCommandTestConfig] {
	t.Helper()

	manager := configkit.NewManager[reloadCommandTestConfig]()
	source := configkit.NewBytesSource(
		[]byte(`{"name":"api","enabled":true,"port":8080}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)
	if _, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindInitialLoad, source, reloadCommandTestPipeline()); err != nil {
		t.Fatalf("initial load: %v", err)
	}
	return manager
}

func reloadCommandTestPipeline() configkit.Pipeline[reloadCommandTestConfig] {
	return configkit.Pipeline[reloadCommandTestConfig]{
		Decode: configkit.JSONDecoder[reloadCommandTestConfig](),
		ValidateConfig: func(ctx context.Context, cfg reloadCommandTestConfig) error {
			if cfg.Name == "" {
				return errors.New("name is required")
			}
			return nil
		},
		Redact:   configkit.EmptyRedactor[reloadCommandTestConfig](),
		Checksum: configkit.SHA256JSONChecksum[reloadCommandTestConfig](),
	}
}

func reloadCommandPayload(t *testing.T, result opskit.CommandResult) configkit.ReloadCommandResult {
	t.Helper()

	payload, ok := result.Result.(configkit.ReloadCommandResult)
	if !ok {
		t.Fatalf("result payload type = %T, want configkit.ReloadCommandResult", result.Result)
	}
	return payload
}
