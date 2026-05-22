package opshttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"

	configkit "github.com/jaredjakacky/configkit"
	servekit "github.com/jaredjakacky/servekit"
)

const defaultPathPrefix = "/admin/config"

var (
	// ErrMissingServer is returned when Mount is called without a Servekit server.
	ErrMissingServer = errors.New("configkit/opshttp: missing server")

	// ErrMissingInspector is returned when Mount is called without a Configkit inspector.
	ErrMissingInspector = errors.New("configkit/opshttp: missing inspector")
)

// AttemptProvider exposes recent Configkit load attempts.
//
// Manager implements this interface. Mount registers the attempts route only
// when the supplied inspector also implements AttemptProvider.
type AttemptProvider interface {
	Attempts() []configkit.AttemptRecord
}

// Option configures Configkit operational route mounting.
type Option func(*options)

type options struct {
	pathPrefix      string
	endpointOptions []servekit.EndpointOption
}

// WithPathPrefix sets the base route for Configkit operational endpoints.
//
// The default is /admin/config. Mount registers GET <prefix> for inspection
// and, when attempts are available, GET <prefix>/attempts.
func WithPathPrefix(prefix string) Option {
	return func(options *options) {
		options.pathPrefix = prefix
	}
}

// WithEndpointOptions applies Servekit endpoint options to every mounted route.
//
// Use this to attach auth gates, route-local middleware, timeouts, body limits,
// telemetry controls, or response encoding policy owned by Servekit.
func WithEndpointOptions(opts ...servekit.EndpointOption) Option {
	return func(options *options) {
		options.endpointOptions = append(options.endpointOptions, opts...)
	}
}

// Mount registers read-only Configkit operational endpoints on server.
//
// Mount always registers GET /admin/config by default, returning
// configkit.Inspection. If inspector also implements AttemptProvider, Mount
// registers GET /admin/config/attempts, returning recent attempt records.
func Mount(server *servekit.Server, inspector configkit.Inspector, opts ...Option) error {
	if server == nil {
		return ErrMissingServer
	}
	if inspector == nil {
		return ErrMissingInspector
	}

	options := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	if err := options.validate(); err != nil {
		return err
	}

	endpointOptions := append([]servekit.EndpointOption(nil), options.endpointOptions...)
	server.Handle(http.MethodGet, options.pathPrefix, func(r *http.Request) (any, error) {
		return inspector.Inspect(), nil
	}, endpointOptions...)

	attempts, ok := inspector.(AttemptProvider)
	if !ok {
		return nil
	}

	server.Handle(http.MethodGet, options.pathPrefix+"/attempts", func(r *http.Request) (any, error) {
		return attempts.Attempts(), nil
	}, endpointOptions...)

	return nil
}

func defaultOptions() options {
	return options{
		pathPrefix: defaultPathPrefix,
	}
}

func (o options) validate() error {
	if o.pathPrefix == "" {
		return errors.New("configkit/opshttp: path prefix must not be empty")
	}
	if !strings.HasPrefix(o.pathPrefix, "/") {
		return fmt.Errorf("configkit/opshttp: path prefix %q must start with /", o.pathPrefix)
	}
	if o.pathPrefix == "/" {
		return errors.New("configkit/opshttp: path prefix must not be /")
	}
	if strings.HasSuffix(o.pathPrefix, "/") {
		return fmt.Errorf("configkit/opshttp: path prefix %q must not end with /", o.pathPrefix)
	}
	if clean := path.Clean(o.pathPrefix); clean != o.pathPrefix {
		return fmt.Errorf("configkit/opshttp: path prefix %q must be clean", o.pathPrefix)
	}
	return nil
}

// ReadinessProvider exposes Configkit status for readiness checks.
type ReadinessProvider interface {
	Status() configkit.Status
}

// ReadinessOption configures Configkit readiness behavior.
type ReadinessOption func(*readinessOptions)

type readinessOptions struct {
	degradedReady bool
}

// WithDegradedReady configures whether degraded Configkit state is ready.
//
// The default is true because degraded means a valid last-known-good snapshot
// remains active after a failed later attempt.
func WithDegradedReady(ready bool) ReadinessOption {
	return func(options *readinessOptions) {
		options.degradedReady = ready
	}
}

// ReadinessCheck adapts Configkit status into a Servekit readiness check.
//
// By default, loaded and degraded states are ready, while unloaded and failed
// states are not ready.
func ReadinessCheck(provider ReadinessProvider, opts ...ReadinessOption) servekit.ReadinessCheck {
	options := readinessOptions{
		degradedReady: true,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}

	return func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if provider == nil {
			return errors.New("configkit/opshttp: readiness provider missing")
		}

		state := provider.Status().State
		switch state {
		case configkit.StatusStateLoaded:
			return nil
		case configkit.StatusStateDegraded:
			if options.degradedReady {
				return nil
			}
		}

		return fmt.Errorf("configkit/opshttp: config not ready: %s", state)
	}
}
