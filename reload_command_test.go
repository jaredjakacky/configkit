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
	workerkit "github.com/jaredjakacky/workerkit"
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
	if payload.Failure != nil {
		t.Fatalf("failure = %+v, want nil", payload.Failure)
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
	if payload.Failure == nil {
		t.Fatal("failure = nil, want safe failure details")
	}
}

func TestReloadCommandRunsThroughWorkerkitGenericAdapter(t *testing.T) {
	manager := loadedReloadCommandManager(t)
	failingSource := configkit.NewBytesSource(
		[]byte(`{"name":`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-2",
	)
	reload := configkit.ReloadCommand(manager, failingSource, reloadCommandTestPipeline())
	descriptors := reload.Commands(context.Background())
	if len(descriptors) != 1 {
		t.Fatalf("command descriptors = %d, want 1", len(descriptors))
	}

	spec := workerkit.CommandFromOpskit(descriptors[0], reload)
	if spec.Name != "config/reload" || !spec.Dangerous || spec.Idempotent {
		t.Fatalf("worker command metadata = %+v, want Configkit descriptor metadata", spec)
	}
	result, err := spec.Handler.HandleCommand(context.Background(), workerkit.CommandRequest{Name: spec.Name})
	if err != nil {
		t.Fatalf("generic Workerkit command error = %v, want completed reload result", err)
	}

	var payload configkit.ReloadCommandResult
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		t.Fatalf("decode generic Workerkit payload: %v", err)
	}
	if payload.AttemptStatus != configkit.AttemptStatusFailed || payload.ManagerState != configkit.LifecycleStateDegraded {
		t.Fatalf("payload = %+v, want failed reload with degraded last-known-good state", payload)
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
	if result.Failure == nil || result.Failure.Code != configkit.FailureCodeCanceled || result.Failure.Message != "config reload canceled" {
		t.Fatalf("failure = %+v, want canceled", result.Failure)
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
	if result.Failure == nil || result.Failure.Code != configkit.FailureCodeDeadlineExceeded || result.Failure.Message != "config reload deadline exceeded" {
		t.Fatalf("failure = %+v, want deadline exceeded", result.Failure)
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
	if payload.Failure == nil || payload.Failure.Code != "missing_source" {
		t.Fatalf("payload failure = %+v, want missing source", payload.Failure)
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
	if payload.Failure == nil || payload.Failure.Message != "config validation failed" {
		t.Fatalf("payload failure = %+v, want safe validation failure", payload.Failure)
	}
}

func TestReloadCommandReturnedErrorIsPrivate(t *testing.T) {
	const secret = "postgres://user:pass@internal/config"
	manager := configkit.NewManager[reloadCommandTestConfig]()
	source := configkit.NewBytesSource(
		[]byte(`{"name":"api","enabled":true,"port":8080}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)
	pipeline := reloadCommandTestPipeline()
	pipeline.ValidateConfig = func(context.Context, reloadCommandTestConfig) error {
		return errors.New("validation failed for " + secret)
	}
	command := configkit.ReloadCommand(manager, source, pipeline)

	result := command.HandleCommand(context.Background(), opskit.NewCommandRequest("config/reload", nil))
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal command result: %v", err)
	}
	if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), `"error"`) {
		t.Fatalf("command result exposed private or legacy error data: %s", encoded)
	}
	payload := reloadCommandPayload(t, result)
	if payload.Failure == nil || payload.Failure.Code != configkit.FailureCodeValidateConfigFailed || payload.Failure.Message != "config validation failed" {
		t.Fatalf("payload failure = %+v, want safe validation failure", payload.Failure)
	}
}

func TestNewReloadCommandResultUsesSafeFallbackWhenAttemptHasNoFailure(t *testing.T) {
	const secret = "postgres://user:pass@internal/config"
	result := configkit.ManagedLoadResult[reloadCommandTestConfig]{
		Load: configkit.LoadResult[reloadCommandTestConfig]{
			Attempt: configkit.AttemptRecord{Status: configkit.AttemptStatusFailed},
		},
	}

	payload := configkit.NewReloadCommandResult(
		result,
		configkit.LifecycleStatus{State: configkit.LifecycleStateFailed},
		errors.New(secret),
	)
	if payload.Failure == nil || payload.Failure.Code != configkit.FailureCodeReloadFailed || payload.Failure.Message != "config reload failed" {
		t.Fatalf("payload failure = %+v, want safe reload fallback", payload.Failure)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("payload exposed private fallback cause: %s", encoded)
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
