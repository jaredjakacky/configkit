package worker

import (
	"context"
	"encoding/json"
	"errors"

	configkit "github.com/jaredjakacky/configkit"
	opskit "github.com/jaredjakacky/opskit"
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

	command := configkit.ReloadCommand(manager, source, pipeline,
		configkit.WithReloadCommandName(options.name),
		configkit.WithReloadCommandDescription(options.description),
	)

	return workerkit.CommandSpec{
		Name:        options.name,
		Description: options.description,
		Handler: workerkit.CommandHandlerFunc(func(ctx context.Context, req workerkit.CommandRequest) (workerkit.CommandResult, error) {
			result := command.HandleCommand(ctx, opskit.CommandRequest{
				Name:        req.Name,
				Payload:     json.RawMessage(req.Payload),
				RequestedAt: &req.RequestedAt,
			})
			return workerResultFromOpskitResult(result)
		}),
	}
}

func defaultReloadCommandOptions() reloadCommandOptions {
	return reloadCommandOptions{
		name:        defaultReloadCommandName,
		description: defaultReloadCommandDescription,
	}
}

func workerResultFromOpskitResult(result opskit.CommandResult) (workerkit.CommandResult, error) {
	if !result.Accepted {
		if result.Message != "" {
			return workerkit.CommandResult{}, errors.New(result.Message)
		}
		return workerkit.CommandResult{}, errors.New("configkit/worker: command was not accepted")
	}
	if result.State == opskit.StateFailed && result.Result == nil {
		switch result.Error {
		case context.Canceled.Error():
			return workerkit.CommandResult{}, context.Canceled
		case context.DeadlineExceeded.Error():
			return workerkit.CommandResult{}, context.DeadlineExceeded
		case "":
			return workerkit.CommandResult{}, errors.New(result.Message)
		default:
			return workerkit.CommandResult{}, errors.New(result.Error)
		}
	}

	payload, err := json.Marshal(result.Result)
	if err != nil {
		return workerkit.CommandResult{}, errors.New("encode config reload result: " + err.Error())
	}
	return workerkit.CommandResult{
		Message: result.Message,
		Payload: payload,
	}, nil
}
