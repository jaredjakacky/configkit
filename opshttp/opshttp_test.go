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
	opskit "github.com/jaredjakacky/opskit"
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
	var inspection configkit.LifecycleInspection
	decodeOpsTestPayload(t, body, &inspection)
	if inspection.Status.State != configkit.LifecycleStateLoaded {
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

func TestMountDoesNotExposeLifecyclePanicPayload(t *testing.T) {
	const secret = "secret-token"
	manager := configkit.NewManager[opsTestConfig]()
	source := configkit.NewBytesSource(
		[]byte(`{"name":"api"}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)
	_, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindReload, source, configkit.Pipeline[opsTestConfig]{
		Decode: configkit.JSONDecoder[opsTestConfig](),
		ValidateConfig: func(ctx context.Context, value opsTestConfig) error {
			panic(secret)
		},
		Redact:   configkit.EmptyRedactor[opsTestConfig](),
		Checksum: configkit.SHA256JSONChecksum[opsTestConfig](),
	})
	if err == nil {
		t.Fatal("load panic error = nil, want error")
	}
	if !errors.Is(err, configkit.ErrLifecyclePanicked) {
		t.Fatalf("load panic error = %v, want configkit.ErrLifecyclePanicked", err)
	}

	server := newOpsTestServer()
	if err := opshttp.Mount(server, manager); err != nil {
		t.Fatalf("mount: %v", err)
	}

	body, status := getOpsTestRoute(t, server, "/admin/config")
	if status != http.StatusOK {
		t.Fatalf("inspection status = %d, want 200; body = %s", status, body)
	}
	assertOpsStringOmits(t, "inspection response body", body, secret)

	body, status = getOpsTestRoute(t, server, "/admin/config/attempts")
	if status != http.StatusOK {
		t.Fatalf("attempts status = %d, want 200; body = %s", status, body)
	}
	assertOpsStringOmits(t, "attempts response body", body, secret)
}

func TestMountDoesNotExposeReturnedLifecycleError(t *testing.T) {
	const secret = "postgres://user:pass@internal/config"
	manager := configkit.NewManager[opsTestConfig]()
	source := configkit.NewBytesSource(
		[]byte(`{"name":"api"}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)
	_, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindReload, source, configkit.Pipeline[opsTestConfig]{
		Decode: configkit.JSONDecoder[opsTestConfig](),
		ValidateConfig: func(context.Context, opsTestConfig) error {
			return errors.New("validation failed for " + secret)
		},
		Redact:   configkit.EmptyRedactor[opsTestConfig](),
		Checksum: configkit.SHA256JSONChecksum[opsTestConfig](),
	})
	if err == nil || !strings.Contains(err.Error(), secret) {
		t.Fatalf("private load error = %v, want original secret-bearing cause", err)
	}

	server := newOpsTestServer()
	if err := opshttp.Mount(server, manager); err != nil {
		t.Fatalf("mount: %v", err)
	}
	for _, route := range []string{"/admin/config", "/admin/config/attempts"} {
		body, status := getOpsTestRoute(t, server, route)
		if status != http.StatusOK {
			t.Fatalf("%s status = %d, want 200; body = %s", route, status, body)
		}
		assertOpsStringOmits(t, route+" response body", body, secret)
		if strings.Contains(body, `"error"`) {
			t.Fatalf("%s contains legacy error field: %s", route, body)
		}
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
		ready   bool
		reason  string
		wantErr bool
	}{
		{name: "not ready", ready: false, reason: "config not loaded", wantErr: true},
		{name: "ready", ready: true, reason: "config loaded"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := opshttp.ReadinessCheck(readinessProvider{ready: tt.ready, reason: tt.reason})(context.Background())
			if tt.wantErr && err == nil {
				t.Fatal("readiness error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("readiness error = %v, want nil", err)
			}
		})
	}
}

func TestReadinessCheckUsesCoreDegradedReadyPolicy(t *testing.T) {
	manager := newOpsTestManager(t, configkit.WithDegradedReady(false))
	failingSource := configkit.NewBytesSource(
		[]byte(`{"name":`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-2",
	)
	_, loadErr := manager.LoadFromSource(context.Background(), configkit.AttemptKindReload, failingSource, opsTestPipeline())
	if loadErr == nil {
		t.Fatal("reload error = nil, want failure")
	}

	err := opshttp.ReadinessCheck(manager)(context.Background())
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

	err := opshttp.ReadinessCheck(readinessProvider{ready: true})(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("readiness error = %v, want context.Canceled", err)
	}
}

type staticInspector struct{}

func (staticInspector) LifecycleInspection() configkit.LifecycleInspection {
	return configkit.LifecycleInspection{
		Status: configkit.LifecycleStatus{State: configkit.LifecycleStateLoaded},
	}
}

type readinessProvider struct {
	ready  bool
	reason string
}

func (p readinessProvider) Readiness(context.Context) opskit.Readiness {
	return opskit.Readiness{Ready: p.ready, Reason: p.reason}
}

func newOpsTestManager(t *testing.T, opts ...configkit.ManagerOption) *configkit.Manager[opsTestConfig] {
	t.Helper()

	manager := configkit.NewManager[opsTestConfig](opts...)
	source := configkit.NewBytesSource(
		[]byte(`{"name":"api"}`),
		configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		"rev-1",
	)
	_, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindInitialLoad, source, opsTestPipeline())
	if err != nil {
		t.Fatalf("load test manager: %v", err)
	}
	return manager
}

func opsTestPipeline() configkit.Pipeline[opsTestConfig] {
	return configkit.Pipeline[opsTestConfig]{
		Decode: configkit.JSONDecoder[opsTestConfig](),
		Redact: func(ctx context.Context, value opsTestConfig) (configkit.RedactedView, error) {
			return configkit.RedactedView{"name": value.Name}, nil
		},
		Checksum: configkit.SHA256JSONChecksum[opsTestConfig](),
	}
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

func assertOpsStringOmits(t *testing.T, label string, value string, secret string) {
	t.Helper()

	if strings.Contains(value, secret) {
		t.Fatalf("%s = %q, must not contain %q", label, value, secret)
	}
}
