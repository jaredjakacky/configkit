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
	retrykit "github.com/jaredjakacky/workerkit/retry"
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

func TestReloadCommandResultRetainsManagedOperationStateAfterLaterAttempt(t *testing.T) {
	manager := loadedReloadCommandManager(t)
	succeeded, err := manager.LoadFromSource(
		context.Background(),
		configkit.AttemptKindReload,
		configkit.NewBytesSource(
			[]byte(`{"name":"worker","enabled":false,"port":9090}`),
			configkit.SourceMetadata{Name: "memory", Kind: "memory"},
			"rev-2",
		),
		reloadCommandTestPipeline(),
	)
	if err != nil {
		t.Fatalf("successful reload: %v", err)
	}
	if succeeded.Apply.ManagerState != configkit.LifecycleStateLoaded {
		t.Fatalf("successful apply manager state = %q, want %q", succeeded.Apply.ManagerState, configkit.LifecycleStateLoaded)
	}

	failed, err := manager.LoadFromSource(
		context.Background(),
		configkit.AttemptKindReload,
		configkit.NewBytesSource(
			[]byte(`{"name":`),
			configkit.SourceMetadata{Name: "memory", Kind: "memory"},
			"rev-3",
		),
		reloadCommandTestPipeline(),
	)
	if err == nil {
		t.Fatal("later reload error = nil, want decode failure")
	}
	if failed.Apply.ManagerState != configkit.LifecycleStateDegraded {
		t.Fatalf("later apply manager state = %q, want %q", failed.Apply.ManagerState, configkit.LifecycleStateDegraded)
	}
	if state := manager.LifecycleStatus().State; state != configkit.LifecycleStateDegraded {
		t.Fatalf("live manager state = %q, want %q", state, configkit.LifecycleStateDegraded)
	}

	payload := configkit.NewReloadCommandResult(succeeded, nil)
	if payload.AttemptID != succeeded.Load.Attempt.ID || payload.AttemptStatus != configkit.AttemptStatusSucceeded {
		t.Fatalf("payload attempt = %d/%q, want successful attempt %d", payload.AttemptID, payload.AttemptStatus, succeeded.Load.Attempt.ID)
	}
	if payload.ManagerState != configkit.LifecycleStateLoaded {
		t.Fatalf("payload manager state = %q, want event-time %q", payload.ManagerState, configkit.LifecycleStateLoaded)
	}
	if !payload.Published || payload.CurrentRevision != "rev-2" {
		t.Fatalf("payload = %+v, want published rev-2", payload)
	}
}

func TestReloadCommandFailedReloadReturnsFailedCommand(t *testing.T) {
	manager := loadedReloadCommandManager(t)
	failingSource := configkit.NewBytesSource(
		[]byte(`{"name":`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-2",
	)
	command := configkit.ReloadCommand(manager, failingSource, reloadCommandTestPipeline())

	result := command.HandleCommand(context.Background(), opskit.NewCommandRequest("config/reload", nil))
	if result.State != opskit.StateFailed {
		t.Fatalf("state = %s, want failed command", result.State)
	}
	if !result.Accepted {
		t.Fatal("accepted = false, want true")
	}
	if result.Message != "config reload failed" {
		t.Fatalf("message = %q, want failure message", result.Message)
	}

	if result.Failure == nil || result.Failure.Code != configkit.FailureCodeDecodeFailed || result.Failure.Message != "config decode failed" {
		t.Fatalf("failure = %+v, want safe decode failure", result.Failure)
	}
	if result.Result != nil {
		t.Fatalf("result = %+v, want nil", result.Result)
	}

	status := manager.LifecycleStatus()
	if status.State != configkit.LifecycleStateDegraded {
		t.Fatalf("manager state = %q, want degraded", status.State)
	}
	if status.Current == nil || status.Current.Revision != "rev-1" {
		t.Fatalf("current = %+v, want last-known-good rev-1", status.Current)
	}
	if status.LastAttempt == nil || status.LastAttempt.Status != configkit.AttemptStatusFailed {
		t.Fatalf("last attempt = %+v, want failed", status.LastAttempt)
	}
	if handlerStatus := command.Status(context.Background()); handlerStatus.State != opskit.StateReady || !handlerStatus.Ready {
		t.Fatalf("handler status = %+v, want ready despite failed command", handlerStatus)
	}
	if readiness := manager.Readiness(context.Background()); !readiness.Ready {
		t.Fatalf("manager readiness = %+v, want last-known-good ready", readiness)
	}
}

func TestReloadCommandLifecycleFailuresReturnFailedCommands(t *testing.T) {
	tests := []struct {
		name        string
		source      func() configkit.Source
		mutate      func(*configkit.Pipeline[reloadCommandTestConfig])
		failureCode string
	}{
		{
			name: "source read",
			source: func() configkit.Source {
				return reloadCommandSource{
					metadata: configkit.SourceMetadata{Name: "memory", Kind: "memory"},
					read: func(context.Context) (configkit.SourceData, error) {
						return configkit.SourceData{}, errors.New("source unavailable")
					},
				}
			},
			failureCode: configkit.FailureCodeSourceReadFailed,
		},
		{
			name: "decode",
			source: func() configkit.Source {
				return configkit.NewBytesSource([]byte(`{"name":`), configkit.SourceMetadata{}, "rev-2")
			},
			failureCode: configkit.FailureCodeDecodeFailed,
		},
		{
			name:   "defaults",
			source: validReloadCommandSource,
			mutate: func(p *configkit.Pipeline[reloadCommandTestConfig]) {
				p.ApplyDefaults = func(context.Context, reloadCommandTestConfig) (reloadCommandTestConfig, error) {
					return reloadCommandTestConfig{}, errors.New("defaults failed")
				}
			},
			failureCode: configkit.FailureCodeDefaultsFailed,
		},
		{
			name:   "validation",
			source: validReloadCommandSource,
			mutate: func(p *configkit.Pipeline[reloadCommandTestConfig]) {
				p.ValidateConfig = func(context.Context, reloadCommandTestConfig) error {
					return errors.New("validation failed")
				}
			},
			failureCode: configkit.FailureCodeValidateConfigFailed,
		},
		{
			name:   "copy",
			source: validReloadCommandSource,
			mutate: func(p *configkit.Pipeline[reloadCommandTestConfig]) {
				p.Copy = func(context.Context, reloadCommandTestConfig) (reloadCommandTestConfig, error) {
					return reloadCommandTestConfig{}, errors.New("copy failed")
				}
			},
			failureCode: configkit.FailureCodeCopyFailed,
		},
		{
			name:   "redaction",
			source: validReloadCommandSource,
			mutate: func(p *configkit.Pipeline[reloadCommandTestConfig]) {
				p.Redact = func(context.Context, reloadCommandTestConfig) (configkit.RedactedView, error) {
					return nil, errors.New("redaction failed")
				}
			},
			failureCode: configkit.FailureCodeRedactFailed,
		},
		{
			name:   "checksum",
			source: validReloadCommandSource,
			mutate: func(p *configkit.Pipeline[reloadCommandTestConfig]) {
				p.Checksum = func(context.Context, reloadCommandTestConfig) (string, error) {
					return "", errors.New("checksum failed")
				}
			},
			failureCode: configkit.FailureCodeChecksumFailed,
		},
		{
			name:   "empty checksum",
			source: validReloadCommandSource,
			mutate: func(p *configkit.Pipeline[reloadCommandTestConfig]) {
				p.Checksum = func(context.Context, reloadCommandTestConfig) (string, error) {
					return "", nil
				}
			},
			failureCode: configkit.FailureCodeChecksumFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := loadedReloadCommandManager(t)
			pipeline := reloadCommandTestPipeline()
			if tt.mutate != nil {
				tt.mutate(&pipeline)
			}
			command := configkit.ReloadCommand(manager, tt.source(), pipeline)

			result := command.HandleCommand(context.Background(), opskit.NewCommandRequest("config/reload", nil))
			if result.State != opskit.StateFailed || !result.Accepted {
				t.Fatalf("result = %+v, want accepted failed command", result)
			}
			if result.Failure == nil || result.Failure.Code != tt.failureCode {
				t.Fatalf("failure = %+v, want code %q", result.Failure, tt.failureCode)
			}
			if result.Result != nil {
				t.Fatalf("result payload = %+v, want nil", result.Result)
			}
			if status := manager.LifecycleStatus(); status.State != configkit.LifecycleStateDegraded || status.Current == nil || status.Current.Revision != "rev-1" {
				t.Fatalf("manager status = %+v, want degraded with last-known-good rev-1", status)
			}
		})
	}
}

func TestReloadCommandFailureRunsThroughWorkerkitGenericAdapter(t *testing.T) {
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
	if !errors.Is(err, workerkit.ErrOpsCommandFailed) {
		t.Fatalf("generic Workerkit command error = %v, want ErrOpsCommandFailed", err)
	}
	var opsErr *workerkit.OpskitCommandError
	if !errors.As(err, &opsErr) {
		t.Fatalf("generic Workerkit command error = %T, want *OpskitCommandError", err)
	}
	if opsErr.Failure.Code != configkit.FailureCodeDecodeFailed || opsErr.Failure.Message != "config decode failed" {
		t.Fatalf("generic Workerkit failure = %+v, want safe decode failure", opsErr.Failure)
	}
	if result.Message != "" || len(result.Payload) != 0 {
		t.Fatalf("generic Workerkit result = %+v, want empty result on failure", result)
	}
}

func TestReloadCommandWorkerkitFailureDoesNotFailRuntimeOrReadiness(t *testing.T) {
	manager := loadedReloadCommandManager(t)
	failingSource := configkit.NewBytesSource(
		[]byte(`{"name":`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-2",
	)
	observer := &reloadCommandWorkerObserver{}
	runtime, err := workerkit.New(
		workerkit.Identity{Name: "config-runtime"},
		workerkit.WithObserver(observer),
	)
	if err != nil {
		t.Fatalf("create Workerkit runtime: %v", err)
	}
	reload := configkit.ReloadCommand(manager, failingSource, reloadCommandTestPipeline())
	if err := runtime.Register(
		workerkit.WorkerSpec{Name: "config", Worker: reloadCommandWorker{}},
		workerkit.WithCommandSpec(workerkit.CommandFromOpskit(reload.Commands(context.Background())[0], reload)),
	); err != nil {
		t.Fatalf("register Workerkit command: %v", err)
	}
	if err := runtime.StartAll(context.Background()); err != nil {
		t.Fatalf("start Workerkit runtime: %v", err)
	}
	t.Cleanup(func() { shutdownReloadCommandRuntime(t, runtime) })

	_, err = runtime.Dispatch(context.Background(), workerkit.CommandRequest{Worker: "config", Name: "config/reload"})
	if !errors.Is(err, workerkit.ErrOpsCommandFailed) {
		t.Fatalf("dispatch error = %v, want ErrOpsCommandFailed", err)
	}

	worker, ok := runtime.Worker("config")
	if !ok {
		t.Fatal("Workerkit worker not found")
	}
	if worker.Status.State != workerkit.StateRunning || !worker.Status.Ready || !worker.Status.AcceptingWork {
		t.Fatalf("worker status = %+v, want running, ready, and accepting work", worker.Status)
	}
	if worker.Status.LastFailure != nil {
		t.Fatalf("worker lifecycle failure = %+v, want nil", worker.Status.LastFailure)
	}
	if worker.Status.LastCommandFailure == nil || worker.Status.LastCommandFailure.Code != configkit.FailureCodeDecodeFailed {
		t.Fatalf("last command failure = %+v, want decode failure", worker.Status.LastCommandFailure)
	}
	if status := runtime.RuntimeStatus(); status.State != workerkit.StateRunning || !status.Ready {
		t.Fatalf("runtime status = %+v, want running and ready", status)
	}
	if status := manager.LifecycleStatus(); status.State != configkit.LifecycleStateDegraded || status.Current == nil || status.Current.Revision != "rev-1" {
		t.Fatalf("manager status = %+v, want degraded with last-known-good rev-1", status)
	}
	if len(observer.failures) != 1 || observer.failures[0].Code != configkit.FailureCodeDecodeFailed {
		t.Fatalf("Workerkit failure events = %+v, want one decode failure", observer.failures)
	}
	if len(observer.ends) != 1 || observer.ends[0].Success || observer.ends[0].Code != configkit.FailureCodeDecodeFailed {
		t.Fatalf("Workerkit command end events = %+v, want failed decode outcome", observer.ends)
	}
}

func TestReloadCommandWorkerkitRetrySeesFailedReload(t *testing.T) {
	manager := loadedReloadCommandManager(t)
	reads := 0
	source := reloadCommandSource{
		metadata: configkit.SourceMetadata{Name: "retrying", Kind: "memory"},
		read: func(ctx context.Context) (configkit.SourceData, error) {
			reads++
			if reads == 1 {
				return configkit.SourceData{}, errors.New("temporary source failure")
			}
			return configkit.SourceData{
				Data:     []byte(`{"name":"api","enabled":true,"port":9090}`),
				Metadata: configkit.SourceMetadata{Name: "retrying", Kind: "memory"},
				Revision: "rev-2",
			}, nil
		},
	}
	observer := &reloadCommandWorkerObserver{}
	runtime, err := workerkit.New(
		workerkit.Identity{Name: "config-runtime"},
		workerkit.WithObserver(observer),
	)
	if err != nil {
		t.Fatalf("create Workerkit runtime: %v", err)
	}
	reload := configkit.ReloadCommand(manager, source, reloadCommandTestPipeline())
	if err := runtime.Register(
		workerkit.WorkerSpec{Name: "config", Worker: reloadCommandWorker{}},
		workerkit.WithWorkerCommandRetry(retrykit.AttemptsIf(2, nil, nil, func(err error) bool {
			var opsErr *workerkit.OpskitCommandError
			return errors.As(err, &opsErr) && opsErr.Failure.Code == configkit.FailureCodeSourceReadFailed
		})),
		workerkit.WithCommandSpec(workerkit.CommandFromOpskit(reload.Commands(context.Background())[0], reload)),
	); err != nil {
		t.Fatalf("register Workerkit command: %v", err)
	}
	if err := runtime.StartAll(context.Background()); err != nil {
		t.Fatalf("start Workerkit runtime: %v", err)
	}
	t.Cleanup(func() { shutdownReloadCommandRuntime(t, runtime) })

	result, err := runtime.Dispatch(context.Background(), workerkit.CommandRequest{Worker: "config", Name: "config/reload"})
	if err != nil {
		t.Fatalf("dispatch retried reload: %v", err)
	}
	if reads != 2 {
		t.Fatalf("source reads = %d, want 2", reads)
	}
	var payload configkit.ReloadCommandResult
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		t.Fatalf("decode successful retry payload: %v", err)
	}
	if payload.AttemptStatus != configkit.AttemptStatusSucceeded || payload.ManagerState != configkit.LifecycleStateLoaded || payload.CurrentRevision != "rev-2" {
		t.Fatalf("successful retry payload = %+v, want loaded rev-2", payload)
	}
	if len(observer.failures) != 1 || observer.failures[0].Code != configkit.FailureCodeSourceReadFailed || observer.failures[0].Attempt != 1 {
		t.Fatalf("Workerkit retry failure events = %+v, want first-attempt source failure", observer.failures)
	}
	if len(observer.ends) != 1 || !observer.ends[0].Success || observer.ends[0].Attempts != 2 {
		t.Fatalf("Workerkit retry command end events = %+v, want successful two-attempt outcome", observer.ends)
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

func TestReloadCommandMissingSourceIsNotReadyAndRejected(t *testing.T) {
	var typedNil *typedNilTestSource
	tests := []struct {
		name   string
		source configkit.Source
	}{
		{name: "nil"},
		{name: "typed nil", source: typedNil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := loadedReloadCommandManager(t)
			command := configkit.ReloadCommand(manager, tt.source, reloadCommandTestPipeline())

			if status := command.Status(context.Background()); status.State != opskit.StateNotReady || status.Ready {
				t.Fatalf("status = %+v, want not ready", status)
			}
			result := command.HandleCommand(context.Background(), opskit.NewCommandRequest("config/reload", nil))
			if result.State != opskit.StateNotReady || result.Accepted {
				t.Fatalf("result = %+v, want rejected command", result)
			}
			if result.Failure == nil || result.Failure.Code != configkit.FailureCodeMissingSource {
				t.Fatalf("failure = %+v, want missing source", result.Failure)
			}
			if result.Result != nil {
				t.Fatalf("result payload = %+v, want nil", result.Result)
			}
			if status := manager.LifecycleStatus(); status.State != configkit.LifecycleStateLoaded || status.Current == nil || status.Current.Revision != "rev-1" {
				t.Fatalf("manager status = %+v, want unchanged loaded rev-1", status)
			}
			if attempts := manager.Attempts(); len(attempts) != 1 {
				t.Fatalf("manager attempts = %d, want only initial load", len(attempts))
			}
		})
	}
}

func TestReloadCommandInvalidPipelineIsNotReadyAndRejected(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*configkit.Pipeline[reloadCommandTestConfig])
	}{
		{name: "missing decoder", mutate: func(p *configkit.Pipeline[reloadCommandTestConfig]) { p.Decode = nil }},
		{name: "missing redactor", mutate: func(p *configkit.Pipeline[reloadCommandTestConfig]) { p.Redact = nil }},
		{name: "missing checksum", mutate: func(p *configkit.Pipeline[reloadCommandTestConfig]) { p.Checksum = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := loadedReloadCommandManager(t)
			pipeline := reloadCommandTestPipeline()
			tt.mutate(&pipeline)
			command := configkit.ReloadCommand(
				manager,
				configkit.NewBytesSource([]byte(`{}`), configkit.SourceMetadata{}, "rev-2"),
				pipeline,
			)

			if status := command.Status(context.Background()); status.State != opskit.StateNotReady || status.Ready {
				t.Fatalf("status = %+v, want not ready", status)
			}
			result := command.HandleCommand(context.Background(), opskit.NewCommandRequest("config/reload", nil))
			if result.State != opskit.StateNotReady || result.Accepted {
				t.Fatalf("result = %+v, want rejected command", result)
			}
			if result.Failure == nil || result.Failure.Code != configkit.FailureCodePipelineValidateFailed {
				t.Fatalf("failure = %+v, want pipeline validation failure", result.Failure)
			}
			if status := manager.LifecycleStatus(); status.State != configkit.LifecycleStateLoaded || status.Current == nil || status.Current.Revision != "rev-1" {
				t.Fatalf("manager status = %+v, want unchanged loaded rev-1", status)
			}
			if attempts := manager.Attempts(); len(attempts) != 1 {
				t.Fatalf("manager attempts = %d, want only initial load", len(attempts))
			}
		})
	}
}

func TestReloadCommandPanicFailureIsSanitized(t *testing.T) {
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
	if result.Failure == nil || result.Failure.Message != "config validation failed" {
		t.Fatalf("failure = %+v, want safe validation failure", result.Failure)
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
	if result.Failure == nil || result.Failure.Code != configkit.FailureCodeValidateConfigFailed || result.Failure.Message != "config validation failed" {
		t.Fatalf("failure = %+v, want safe validation failure", result.Failure)
	}
}

func TestNewReloadCommandResultUsesSafeFallbackWhenAttemptHasNoFailure(t *testing.T) {
	const secret = "postgres://user:pass@internal/config"
	result := configkit.ManagedLoadResult[reloadCommandTestConfig]{
		Load: configkit.LoadResult[reloadCommandTestConfig]{
			Attempt: configkit.AttemptRecord{Status: configkit.AttemptStatusFailed},
		},
		Apply: configkit.ApplyResult{ManagerState: configkit.LifecycleStateFailed},
	}

	payload := configkit.NewReloadCommandResult(
		result,
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

func validReloadCommandSource() configkit.Source {
	return configkit.NewBytesSource(
		[]byte(`{"name":"api","enabled":true,"port":9090}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-2",
	)
}

func reloadCommandPayload(t *testing.T, result opskit.CommandResult) configkit.ReloadCommandResult {
	t.Helper()

	payload, ok := result.Result.(configkit.ReloadCommandResult)
	if !ok {
		t.Fatalf("result payload type = %T, want configkit.ReloadCommandResult", result.Result)
	}
	return payload
}

type reloadCommandSource struct {
	metadata configkit.SourceMetadata
	read     func(context.Context) (configkit.SourceData, error)
}

func (s reloadCommandSource) Metadata() configkit.SourceMetadata {
	return s.metadata
}

func (s reloadCommandSource) Read(ctx context.Context) (configkit.SourceData, error) {
	return s.read(ctx)
}

type reloadCommandWorker struct{}

func (reloadCommandWorker) Start(context.Context) error {
	return nil
}

func (reloadCommandWorker) Stop(context.Context) error {
	return nil
}

type reloadCommandWorkerObserver struct {
	workerkit.NopObserver
	failures []workerkit.FailureEvent
	ends     []workerkit.CommandEndEvent
}

func (o *reloadCommandWorkerObserver) StartCommand(ctx context.Context, _ workerkit.CommandStartEvent) (context.Context, workerkit.CommandObservation) {
	return ctx, workerkit.CommandObservationFunc(func(_ context.Context, event workerkit.CommandEndEvent) {
		o.ends = append(o.ends, event)
	})
}

func (o *reloadCommandWorkerObserver) ObserveFailure(_ context.Context, event workerkit.FailureEvent) {
	o.failures = append(o.failures, event)
}

func shutdownReloadCommandRuntime(t *testing.T, runtime *workerkit.Runtime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.Shutdown(ctx); err != nil {
		t.Errorf("shutdown Workerkit runtime: %v", err)
	}
}
