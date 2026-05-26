package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	configkit "github.com/jaredjakacky/configkit"
	workerkit "github.com/jaredjakacky/workerkit"
)

const (
	defaultReloadCommandName        = "config/reload"
	defaultReloadCommandDescription = "reloads Configkit configuration from source"
)

// ReloadCommandOption configures the Configkit reload command.
type ReloadCommandOption func(*reloadCommandOptions)

type reloadCommandOptions struct {
	name        string
	description string
}

// WithCommandName sets the Workerkit command name.
//
// The default is config/reload.
func WithCommandName(name string) ReloadCommandOption {
	return func(options *reloadCommandOptions) {
		if name == "" {
			return
		}
		options.name = name
	}
}

// WithDescription sets the command discovery description.
func WithDescription(description string) ReloadCommandOption {
	return func(options *reloadCommandOptions) {
		options.description = description
	}
}

// ReloadCommand creates a Workerkit command spec that reloads Configkit state.
//
// The command calls Manager.LoadFromSource with AttemptKindReload. Its result
// payload contains compact operational metadata and never includes the typed
// configuration value or redacted inspection view. Completed reload failures
// are reported in the result payload instead of as Workerkit command errors.
// The payload can include current_revision, current_checksum, and error fields;
// those values are operationally visible and should be safe for whoever can
// dispatch or inspect the command result.
func ReloadCommand[T any](manager *configkit.Manager[T], source configkit.Source, pipeline configkit.Pipeline[T], opts ...ReloadCommandOption) workerkit.CommandSpec {
	options := defaultReloadCommandOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}

	return workerkit.CommandSpec{
		Name:        options.name,
		Description: options.description,
		Handler: workerkit.CommandHandlerFunc(func(ctx context.Context, req workerkit.CommandRequest) (workerkit.CommandResult, error) {
			return runReloadCommand(ctx, manager, source, pipeline)
		}),
	}
}

func defaultReloadCommandOptions() reloadCommandOptions {
	return reloadCommandOptions{
		name:        defaultReloadCommandName,
		description: defaultReloadCommandDescription,
	}
}

func runReloadCommand[T any](ctx context.Context, manager *configkit.Manager[T], source configkit.Source, pipeline configkit.Pipeline[T]) (workerkit.CommandResult, error) {
	if manager == nil {
		return workerkit.CommandResult{}, errors.New("configkit/worker: missing manager")
	}

	result, loadErr := manager.LoadFromSource(ctx, configkit.AttemptKindReload, source, pipeline)
	if isCommandContextError(loadErr) {
		return workerkit.CommandResult{}, loadErr
	}

	status := manager.Status()

	payload, err := json.Marshal(reloadCommandPayload(result, status, loadErr))
	if err != nil {
		return workerkit.CommandResult{}, fmt.Errorf("encode config reload result: %w", err)
	}

	commandResult := workerkit.CommandResult{
		Message: reloadCommandMessage(result.Load.Attempt.Status),
		Payload: payload,
	}
	return commandResult, nil
}

func isCommandContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
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

func reloadCommandPayload[T any](result configkit.ManagedLoadResult[T], status configkit.Status, loadErr error) reloadResultPayload {
	payload := reloadResultPayload{
		AttemptID:     result.Load.Attempt.ID,
		AttemptStatus: result.Load.Attempt.Status,
		ManagerState:  status.State,
		Published:     result.Apply.Published,
		Changed:       result.Apply.Changed,
	}
	if result.Apply.Current != nil {
		payload.CurrentChecksum = result.Apply.Current.Checksum
		payload.CurrentRevision = result.Apply.Current.Revision
	}
	if loadErr != nil {
		payload.Error = loadErr.Error()
	}
	return payload
}

func reloadCommandMessage(status configkit.AttemptStatus) string {
	if status == configkit.AttemptStatusSucceeded {
		return "config reload succeeded"
	}
	return "config reload failed"
}
