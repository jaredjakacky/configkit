package opshttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"

	configkit "github.com/jaredjakacky/configkit"
	opskit "github.com/jaredjakacky/opskit"
	servekit "github.com/jaredjakacky/servekit"
)

const defaultPathPrefix = "/admin/config"

var (
	// ErrMissingServer is returned when Mount is called without a Servekit server.
	ErrMissingServer = errors.New("configkit/opshttp: missing server")

	// ErrMissingInspector is returned when Mount is called without a Configkit lifecycle inspector.
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
// configkit.LifecycleInspection. If inspector also implements AttemptProvider, Mount
// registers GET /admin/config/attempts, returning recent attempt records.
func Mount(server *servekit.Server, inspector configkit.LifecycleInspector, opts ...Option) error {
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
		return inspector.LifecycleInspection(), nil
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

// ReadinessProvider exposes Configkit readiness for readiness checks.
type ReadinessProvider interface {
	Readiness(context.Context) opskit.Readiness
}

// ReadinessCheck adapts Configkit readiness into a Servekit readiness check.
//
// For composed Kit Series services, prefer registering Manager with an Opskit
// registry and passing that registry to Servekit with servekit.WithOps.
// ReadinessCheck remains useful for standalone Servekit services that are not
// using Opskit.
func ReadinessCheck(provider ReadinessProvider) servekit.ReadinessCheck {
	return func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if provider == nil {
			return errors.New("configkit/opshttp: readiness provider missing")
		}

		readiness := provider.Readiness(ctx)
		if readiness.Ready {
			return nil
		}
		if readiness.Reason != "" {
			return fmt.Errorf("configkit/opshttp: config not ready: %s", readiness.Reason)
		}

		return errors.New("configkit/opshttp: config not ready")
	}
}
