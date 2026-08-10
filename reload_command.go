package configkit

import (
	"context"
	"errors"
	"time"

	opskit "github.com/jaredjakacky/opskit"
)

const (
	defaultReloadCommandComponentName        = "config-reload"
	defaultReloadCommandComponentKind        = "command_handler"
	defaultReloadCommandComponentDescription = "Configkit reload command handler"
	defaultReloadCommandName                 = "config/reload"
	defaultReloadCommandDescription          = "reloads Configkit configuration from source"
)

var (
	_ opskit.Component        = (*ReloadCommandHandler[any])(nil)
	_ opskit.CommandHandler   = (*ReloadCommandHandler[any])(nil)
	_ opskit.CommandDescriber = (*ReloadCommandHandler[any])(nil)
)

// ReloadCommandOption configures a Configkit Opskit reload command.
type ReloadCommandOption func(*reloadCommandOptions)

type reloadCommandOptions struct {
	componentInfo opskit.ComponentInfo
	commandName   string
	description   string
}

// WithReloadCommandName sets the Opskit command name.
//
// The default is config/reload. Empty names preserve the default.
func WithReloadCommandName(name string) ReloadCommandOption {
	return func(options *reloadCommandOptions) {
		if name == "" {
			return
		}
		options.commandName = name
	}
}

// WithReloadCommandDescription sets the command discovery description.
func WithReloadCommandDescription(description string) ReloadCommandOption {
	return func(options *reloadCommandOptions) {
		options.description = description
	}
}

// WithReloadCommandComponentInfo sets the Opskit component identity for the
// reload command handler.
//
// Empty fields fall back to Configkit defaults. Labels are appended to stable
// Configkit labels; the kit=configkit label is always preserved.
func WithReloadCommandComponentInfo(info opskit.ComponentInfo) ReloadCommandOption {
	return func(options *reloadCommandOptions) {
		options.componentInfo = reloadCommandComponentInfo(info)
	}
}

// ReloadCommandHandler exposes Configkit reload as an Opskit command component.
type ReloadCommandHandler[T any] struct {
	manager       *Manager[T]
	source        Source
	pipeline      Pipeline[T]
	componentInfo opskit.ComponentInfo
	descriptor    opskit.CommandDescriptor
}

// ReloadCommand creates an Opskit command handler that reloads Configkit state.
//
// The command calls Manager.LoadFromSource with AttemptKindReload. Successful
// reloads return completed Opskit command results. Admitted reload failures
// return failed command results with explicit safe public failure detail while
// the manager independently preserves any last-known-good snapshot. A handler
// missing its manager, source, or required pipeline steps rejects the command
// without recording a load attempt.
func ReloadCommand[T any](manager *Manager[T], source Source, pipeline Pipeline[T], opts ...ReloadCommandOption) *ReloadCommandHandler[T] {
	options := defaultReloadCommandOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}

	return &ReloadCommandHandler[T]{
		manager:       manager,
		source:        source,
		pipeline:      pipeline,
		componentInfo: cloneOpsComponentInfo(options.componentInfo),
		descriptor: opskit.CommandDescriptor{
			Name:        options.commandName,
			Description: options.description,
			Dangerous:   true,
			Idempotent:  false,
			Attributes: []opskit.Attribute{
				opskit.Attr("kit", "configkit"),
				opskit.Attr("command", "reload"),
			},
		},
	}
}

func defaultReloadCommandOptions() reloadCommandOptions {
	return reloadCommandOptions{
		componentInfo: reloadCommandComponentInfo(opskit.ComponentInfo{}),
		commandName:   defaultReloadCommandName,
		description:   defaultReloadCommandDescription,
	}
}

func reloadCommandComponentInfo(info opskit.ComponentInfo) opskit.ComponentInfo {
	out := opskit.ComponentInfo{
		Name:        defaultReloadCommandComponentName,
		Kind:        defaultReloadCommandComponentKind,
		Description: defaultReloadCommandComponentDescription,
		Labels: []opskit.Attribute{
			opskit.Attr("kit", "configkit"),
		},
	}
	if info.Name != "" {
		out.Name = info.Name
	}
	if info.Kind != "" {
		out.Kind = info.Kind
	}
	if info.Description != "" {
		out.Description = info.Description
	}
	if len(info.Labels) > 0 {
		out.Labels = mergeOpsComponentLabels(info.Labels)
	}
	return out
}

// ComponentInfo returns the Opskit identity for this reload command handler.
func (h *ReloadCommandHandler[T]) ComponentInfo() opskit.ComponentInfo {
	if h == nil {
		return reloadCommandComponentInfo(opskit.ComponentInfo{})
	}
	return cloneOpsComponentInfo(h.componentInfo)
}

// Status returns whether this reload command handler is statically usable.
//
// Status checks only local handler configuration. It does not read the source,
// run the pipeline, or mirror manager lifecycle and readiness state.
func (h *ReloadCommandHandler[T]) Status(context.Context) opskit.Status {
	if failure := h.configurationFailure(); failure != nil {
		return opskit.NotReadyStatus(failure.message, opskit.Attr("command", "reload"))
	}
	return opskit.ReadyStatus("config reload command handler ready", opskit.Attr("command", "reload"))
}

// Commands returns the reload command descriptor.
func (h *ReloadCommandHandler[T]) Commands(context.Context) []opskit.CommandDescriptor {
	if h == nil {
		return nil
	}

	descriptor := h.descriptor
	descriptor.Attributes = cloneOpsAttributes(descriptor.Attributes)
	return []opskit.CommandDescriptor{descriptor}
}

// HandleCommand runs the Configkit reload command.
func (h *ReloadCommandHandler[T]) HandleCommand(ctx context.Context, request opskit.CommandRequest) opskit.CommandResult {
	if ctx == nil {
		ctx = context.Background()
	}
	startedAt := time.Now()

	if configurationFailure := h.configurationFailure(); configurationFailure != nil {
		if configurationFailure.failure != nil {
			return opskit.RejectedCommandWithFailure(
				configurationFailure.message,
				*configurationFailure.failure,
				opskit.Attr("command", "reload"),
			)
		}
		return opskit.RejectedCommand(configurationFailure.message, opskit.Attr("command", "reload"))
	}
	if request.Name != h.descriptor.Name {
		return opskit.RejectedCommand("unsupported config reload command", opskit.Attr("command", "reload"))
	}

	result, loadErr := h.manager.LoadFromSource(ctx, AttemptKindReload, h.source, h.pipeline)
	duration := time.Since(startedAt)
	if isReloadCommandContextError(loadErr) {
		contextErr := reloadCommandContextError(loadErr)
		failure := attemptFailure(AttemptStageContext, contextErr)
		failure.Message = reloadCommandContextMessage(contextErr)
		return opskit.FailedCommandWithFailure(
			failure.Message,
			*failure,
			duration,
			opskit.Attr("command", "reload"),
		)
	}

	status := h.manager.LifecycleStatus()
	payload := NewReloadCommandResult(result, status, loadErr)
	if loadErr != nil || result.Load.Attempt.Status != AttemptStatusSucceeded {
		return opskit.FailedCommandWithFailure(
			"config reload failed",
			reloadCommandFailure(payload),
			duration,
			opskit.Attr("command", "reload"),
		)
	}
	return opskit.CompletedCommand("config reload succeeded", payload, duration, opskit.Attr("command", "reload"))
}

// ReloadCommandResult is the safe operational view of a Configkit reload.
// ReloadCommand returns it as Result only for successfully completed commands;
// NewReloadCommandResult can also build a view of a failed managed load.
//
// It intentionally excludes typed configuration values and redacted inspection
// output. Revisions, checksums, and public failure detail are still
// operationally visible. Internal errors and recovered panic payloads are not
// serialized.
type ReloadCommandResult struct {
	AttemptID       uint64          `json:"attempt_id,omitempty"`
	AttemptStatus   AttemptStatus   `json:"attempt_status"`
	ManagerState    LifecycleState  `json:"manager_state"`
	Published       bool            `json:"published"`
	Changed         bool            `json:"changed"`
	CurrentChecksum string          `json:"current_checksum,omitempty"`
	CurrentRevision string          `json:"current_revision,omitempty"`
	Failure         *opskit.Failure `json:"failure,omitempty"`
}

// NewReloadCommandResult builds the safe operational reload command payload.
func NewReloadCommandResult[T any](result ManagedLoadResult[T], status LifecycleStatus, loadErr error) ReloadCommandResult {
	payload := ReloadCommandResult{
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
	if loadErr != nil || result.Load.Attempt.Status == AttemptStatusFailed {
		payload.Failure = cloneFailure(result.Load.Attempt.Failure)
		if payload.Failure == nil {
			payload.Failure = newFailure(FailureCodeReloadFailed, "config reload failed")
		}
	}
	return payload
}

type reloadCommandConfigurationFailure struct {
	message string
	failure *opskit.Failure
}

func (h *ReloadCommandHandler[T]) configurationFailure() *reloadCommandConfigurationFailure {
	if h == nil || h.manager == nil {
		return &reloadCommandConfigurationFailure{
			message: "config reload command handler is missing manager",
		}
	}
	if h.source == nil {
		return &reloadCommandConfigurationFailure{
			message: "config reload command handler is missing source",
			failure: newFailure(FailureCodeMissingSource, "config source is missing"),
		}
	}
	if err := h.pipeline.Validate(); err != nil {
		return &reloadCommandConfigurationFailure{
			message: "config reload command handler has invalid pipeline",
			failure: newFailure(FailureCodePipelineValidateFailed, "config pipeline validation failed"),
		}
	}
	return nil
}

func reloadCommandFailure(payload ReloadCommandResult) opskit.Failure {
	if payload.Failure != nil {
		return *cloneFailure(payload.Failure)
	}
	return *newFailure(FailureCodeReloadFailed, "config reload failed")
}

func isReloadCommandContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func reloadCommandContextError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	default:
		return err
	}
}

func reloadCommandContextMessage(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "config reload canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "config reload deadline exceeded"
	default:
		return "config reload failed"
	}
}
