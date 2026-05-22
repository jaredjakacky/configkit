package opshttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	configkit "github.com/jaredjakacky/configkit"
	opshttp "github.com/jaredjakacky/configkit/opshttp"
	servekit "github.com/jaredjakacky/servekit"
)

type opsTestConfig struct {
	Name string `json:"name"`
}

func TestMountRejectsMissingInputs(t *testing.T) {
	inspector := newOpsTestManager(t)
	if err := opshttp.Mount(nil, inspector); !errors.Is(err, opshttp.ErrMissingServer) {
		t.Fatalf("mount missing server error = %v, want opshttp.ErrMissingServer", err)
	}
	if err := opshttp.Mount(servekit.New(), nil); !errors.Is(err, opshttp.ErrMissingInspector) {
		t.Fatalf("mount missing inspector error = %v, want opshttp.ErrMissingInspector", err)
	}
}

func TestMountRegistersInspectionRoute(t *testing.T) {
	manager := newOpsTestManager(t)
	server := newOpsTestServer()

	if err := opshttp.Mount(server, manager); err != nil {
		t.Fatalf("mount: %v", err)
	}

	body, status := getOpsTestRoute(t, server, "/admin/config")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", status, body)
	}
	var inspection configkit.Inspection
	decodeOpsTestPayload(t, body, &inspection)
	if inspection.Status.State != configkit.StatusStateLoaded {
		t.Fatalf("inspection state = %q, want loaded", inspection.Status.State)
	}
	if got := inspection.Redacted["name"]; got != "api" {
		t.Fatalf("redacted name = %v, want api", got)
	}
}

func TestMountRegistersAttemptsRouteWhenAvailable(t *testing.T) {
	manager := newOpsTestManager(t)
	server := newOpsTestServer()

	if err := opshttp.Mount(server, manager); err != nil {
		t.Fatalf("mount: %v", err)
	}

	body, status := getOpsTestRoute(t, server, "/admin/config/attempts")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", status, body)
	}
	var attempts []configkit.AttemptRecord
	decodeOpsTestPayload(t, body, &attempts)
	if len(attempts) != 1 {
		t.Fatalf("attempt count = %d, want 1", len(attempts))
	}
	if attempts[0].Status != configkit.AttemptStatusSucceeded {
		t.Fatalf("attempt status = %q, want succeeded", attempts[0].Status)
	}
}

func TestMountSkipsAttemptsRouteWhenUnavailable(t *testing.T) {
	server := newOpsTestServer()

	if err := opshttp.Mount(server, staticInspector{}); err != nil {
		t.Fatalf("mount: %v", err)
	}

	_, status := getOpsTestRoute(t, server, "/admin/config/attempts")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

func TestMountUsesCustomPathPrefix(t *testing.T) {
	manager := newOpsTestManager(t)
	server := newOpsTestServer()

	if err := opshttp.Mount(server, manager, opshttp.WithPathPrefix("/internal/config")); err != nil {
		t.Fatalf("mount: %v", err)
	}

	_, status := getOpsTestRoute(t, server, "/internal/config")
	if status != http.StatusOK {
		t.Fatalf("custom inspection status = %d, want 200", status)
	}
	_, status = getOpsTestRoute(t, server, "/internal/config/attempts")
	if status != http.StatusOK {
		t.Fatalf("custom attempts status = %d, want 200", status)
	}
}

func TestMountAppliesEndpointOptions(t *testing.T) {
	manager := newOpsTestManager(t)
	server := newOpsTestServer()

	if err := opshttp.Mount(server, manager, opshttp.WithEndpointOptions(servekit.WithAuthCheck(func(r *http.Request) bool {
		return r.Header.Get("X-Admin") == "true"
	}))); err != nil {
		t.Fatalf("mount: %v", err)
	}

	_, status := getOpsTestRoute(t, server, "/admin/config")
	if status != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", status)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/config", nil)
	req.Header.Set("X-Admin", "true")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authorized status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
}

func TestOptionsValidatePathPrefix(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		wantError string
	}{
		{name: "empty", prefix: "", wantError: "must not be empty"},
		{name: "relative", prefix: "admin/config", wantError: "must start with /"},
		{name: "root", prefix: "/", wantError: "must not be /"},
		{name: "trailing slash", prefix: "/admin/config/", wantError: "must not end with /"},
		{name: "unclean double slash", prefix: "/admin//config", wantError: "must be clean"},
		{name: "unclean dot dot", prefix: "/admin/../config", wantError: "must be clean"},
		{name: "valid", prefix: "/admin/config"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := opshttp.Mount(newOpsTestServer(), staticInspector{}, opshttp.WithPathPrefix(tt.prefix))
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("validate prefix error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validate prefix error = nil, want containing %q", tt.wantError)
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("validate prefix error = %q, want containing %q", err.Error(), tt.wantError)
			}
		})
	}
}

func TestReadinessCheckStates(t *testing.T) {
	tests := []struct {
		name    string
		state   configkit.StatusState
		wantErr bool
	}{
		{name: "unloaded", state: configkit.StatusStateUnloaded, wantErr: true},
		{name: "failed", state: configkit.StatusStateFailed, wantErr: true},
		{name: "loaded", state: configkit.StatusStateLoaded},
		{name: "degraded", state: configkit.StatusStateDegraded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := opshttp.ReadinessCheck(statusProvider{state: tt.state})(context.Background())
			if tt.wantErr && err == nil {
				t.Fatal("readiness error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("readiness error = %v, want nil", err)
			}
		})
	}
}

func TestReadinessCheckWithDegradedReadyFalse(t *testing.T) {
	err := opshttp.ReadinessCheck(statusProvider{state: configkit.StatusStateDegraded}, opshttp.WithDegradedReady(false))(context.Background())
	if err == nil {
		t.Fatal("readiness error = nil, want degraded not ready")
	}
}

func TestReadinessCheckMissingProvider(t *testing.T) {
	err := opshttp.ReadinessCheck(nil)(context.Background())
	if err == nil {
		t.Fatal("readiness error = nil, want missing provider")
	}
	if !strings.Contains(err.Error(), "readiness provider missing") {
		t.Fatalf("readiness error = %q, want missing provider", err.Error())
	}
}

func TestReadinessCheckContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := opshttp.ReadinessCheck(statusProvider{state: configkit.StatusStateLoaded})(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("readiness error = %v, want context.Canceled", err)
	}
}

type staticInspector struct{}

func (staticInspector) Inspect() configkit.Inspection {
	return configkit.Inspection{
		Status: configkit.Status{State: configkit.StatusStateLoaded},
	}
}

type statusProvider struct {
	state configkit.StatusState
}

func (p statusProvider) Status() configkit.Status {
	return configkit.Status{State: p.state}
}

func newOpsTestManager(t *testing.T) *configkit.Manager[opsTestConfig] {
	t.Helper()

	manager := configkit.NewManager[opsTestConfig]()
	source := configkit.NewBytesSource(
		[]byte(`{"name":"api"}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)
	_, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindInitialLoad, source, configkit.Pipeline[opsTestConfig]{
		Decode: configkit.JSONDecoder[opsTestConfig](),
		Redact: func(ctx context.Context, value opsTestConfig) (configkit.RedactedView, error) {
			return configkit.RedactedView{"name": value.Name}, nil
		},
		Checksum: configkit.SHA256JSONChecksum[opsTestConfig](),
	})
	if err != nil {
		t.Fatalf("load test manager: %v", err)
	}
	return manager
}

func newOpsTestServer() *servekit.Server {
	return servekit.New(
		servekit.WithResponseEncoder(func(w http.ResponseWriter, r *http.Request, payload any) error {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			return json.NewEncoder(w).Encode(payload)
		}),
	)
}

func getOpsTestRoute(t *testing.T, server *servekit.Server, target string) (string, int) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	return rec.Body.String(), rec.Code
}

func decodeOpsTestPayload[T any](t *testing.T, body string, out *T) {
	t.Helper()

	if err := json.Unmarshal([]byte(body), out); err != nil {
		t.Fatalf("decode response %q: %v", body, err)
	}
}
