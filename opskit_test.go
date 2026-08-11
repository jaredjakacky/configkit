package configkit_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	configkit "github.com/jaredjakacky/configkit"
	opskit "github.com/jaredjakacky/opskit"
)

func TestManagerImplementsOpskitContracts(t *testing.T) {
	var manager *configkit.Manager[stepsTestConfig]

	var _ opskit.Component = manager
	var _ opskit.ReadinessContributor = manager
	var _ opskit.Inspector = manager
}

func TestManagerOpskitComponentInfoDefaultsAndOptions(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()
	info := manager.ComponentInfo()
	if info.Name != "config" {
		t.Fatalf("component name = %q, want config", info.Name)
	}
	if info.Kind != "config" {
		t.Fatalf("component kind = %q, want config", info.Kind)
	}
	if info.Description != "application configuration" {
		t.Fatalf("component description = %q, want default", info.Description)
	}
	if got := opsAttributeMap(info.Labels)["kit"]; got != "configkit" {
		t.Fatalf("component kit label = %q, want configkit", got)
	}

	manager = configkit.NewManager[stepsTestConfig](configkit.WithIdentity("tenant_config"))
	if info := manager.ComponentInfo(); info.Name != "tenant_config" || info.Kind != "config" {
		t.Fatalf("component info with identity = %+v, want custom name and default kind", info)
	}

	manager = configkit.NewManager[stepsTestConfig](configkit.WithComponentInfo(opskit.ComponentInfo{
		Name:        "runtime_config",
		Kind:        "configuration",
		Description: "runtime config",
		Labels:      []opskit.Attribute{opskit.Attr("owner", "platform")},
	}))
	info = manager.ComponentInfo()
	if info.Name != "runtime_config" || info.Kind != "configuration" || info.Description != "runtime config" {
		t.Fatalf("component info override = %+v, want supplied fields", info)
	}
	attrs := opsAttributeMap(info.Labels)
	if attrs["owner"] != "platform" || attrs["kit"] != "configkit" {
		t.Fatalf("component labels = %+v, want supplied labels plus stable kit label", attrs)
	}

	manager = configkit.NewManager[stepsTestConfig](configkit.WithComponentInfo(opskit.ComponentInfo{
		Labels: []opskit.Attribute{
			opskit.Attr("kit", "custom"),
			opskit.Attr("owner", "platform"),
		},
	}))
	attrs = opsAttributeMap(manager.ComponentInfo().Labels)
	if attrs["kit"] != "configkit" {
		t.Fatalf("component kit label = %q, want stable configkit label", attrs["kit"])
	}
	if attrs["owner"] != "platform" {
		t.Fatalf("component owner label = %q, want platform", attrs["owner"])
	}
}

func TestManagerOpskitStatusAndReadinessMapLifecycleStates(t *testing.T) {
	tests := []struct {
		name          string
		manager       *configkit.Manager[stepsTestConfig]
		wantState     opskit.State
		wantReady     bool
		wantReadiness bool
	}{
		{
			name:          "unloaded",
			manager:       configkit.NewManager[stepsTestConfig](),
			wantState:     opskit.StateNotReady,
			wantReady:     false,
			wantReadiness: false,
		},
		{
			name:          "failed",
			manager:       failedOpskitTestManager(t),
			wantState:     opskit.StateFailed,
			wantReady:     false,
			wantReadiness: false,
		},
		{
			name:          "loaded",
			manager:       loadedOpskitTestManager(t),
			wantState:     opskit.StateReady,
			wantReady:     true,
			wantReadiness: true,
		},
		{
			name:          "degraded default ready",
			manager:       degradedOpskitTestManager(t),
			wantState:     opskit.StateDegraded,
			wantReady:     true,
			wantReadiness: true,
		},
		{
			name:          "degraded strict not ready",
			manager:       degradedOpskitTestManager(t, configkit.WithDegradedReady(false)),
			wantState:     opskit.StateDegraded,
			wantReady:     false,
			wantReadiness: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := tt.manager.Status(context.Background())
			if status.State != tt.wantState {
				t.Fatalf("opskit status state = %s, want %s", status.State, tt.wantState)
			}
			if status.Ready != tt.wantReady {
				t.Fatalf("opskit status ready = %v, want %v", status.Ready, tt.wantReady)
			}

			readiness := tt.manager.Readiness(context.Background())
			if readiness.Ready != tt.wantReadiness {
				t.Fatalf("opskit readiness = %v, want %v", readiness.Ready, tt.wantReadiness)
			}
			if len(readiness.Items) != 0 {
				t.Fatalf("readiness items = %+v, want no synthetic child items", readiness.Items)
			}
		})
	}
}

func TestManagerOpskitStatusUpdatedAtUsesApplyTime(t *testing.T) {
	t.Run("successful apply", func(t *testing.T) {
		manager := configkit.NewManager[stepsTestConfig]()
		endedAt := time.Now().UTC().Add(-2 * time.Hour)
		apply, err := manager.Apply(context.Background(), succeededStatusTestResultAt("v1", "sum-1", endedAt))
		if err != nil {
			t.Fatalf("apply success: %v", err)
		}

		assertOpskitUpdatedAtMatchesApply(t, manager, apply, endedAt)
	})

	t.Run("failed initial apply", func(t *testing.T) {
		manager := configkit.NewManager[stepsTestConfig]()
		endedAt := time.Now().UTC().Add(-2 * time.Hour)
		apply, err := manager.Apply(context.Background(), failedStatusTestResultAt("safe failure", endedAt))
		if err != nil {
			t.Fatalf("apply failure: %v", err)
		}

		assertOpskitUpdatedAtMatchesApply(t, manager, apply, endedAt)
	})

	t.Run("degraded apply", func(t *testing.T) {
		manager := configkit.NewManager[stepsTestConfig]()
		if _, err := manager.Apply(context.Background(), succeededStatusTestResultAt("v1", "sum-1", time.Now().UTC().Add(-3*time.Hour))); err != nil {
			t.Fatalf("seed manager: %v", err)
		}
		endedAt := time.Now().UTC().Add(-2 * time.Hour)
		apply, err := manager.Apply(context.Background(), failedStatusTestResultAt("safe failure", endedAt))
		if err != nil {
			t.Fatalf("apply failure: %v", err)
		}

		assertOpskitUpdatedAtMatchesApply(t, manager, apply, endedAt)
		if status := manager.Status(context.Background()); status.State != opskit.StateDegraded {
			t.Fatalf("status state = %q, want %q", status.State, opskit.StateDegraded)
		}
	})

	t.Run("unloaded", func(t *testing.T) {
		status := configkit.NewManager[stepsTestConfig]().Status(context.Background())
		if status.UpdatedAt != nil {
			t.Fatalf("updated at = %v, want nil", status.UpdatedAt)
		}
	})
}

func assertOpskitUpdatedAtMatchesApply(t *testing.T, manager *configkit.Manager[stepsTestConfig], apply configkit.ApplyResult, endedAt time.Time) {
	t.Helper()

	status := manager.Status(context.Background())
	if status.UpdatedAt == nil || !status.UpdatedAt.Equal(apply.AppliedAt) {
		t.Fatalf("updated at = %v, want apply time %v", status.UpdatedAt, apply.AppliedAt)
	}
	if status.UpdatedAt.Equal(endedAt) {
		t.Fatalf("updated at = %v, must not use historical attempt end time", status.UpdatedAt)
	}
}

func TestManagerOpskitRegistryOwnsParentReadinessIdentityAndPolicy(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig](configkit.WithComponentInfo(opskit.ComponentInfo{
		Name: "runtime_config",
		Kind: "configuration",
	}))
	registry := opskit.NewRegistry()
	if err := registry.Register(manager, opskit.Required()); err != nil {
		t.Fatalf("register manager: %v", err)
	}

	readiness := registry.Readiness(context.Background())
	if len(readiness.Components) != 1 {
		t.Fatalf("component count = %d, want 1", len(readiness.Components))
	}
	component := readiness.Components[0]
	if component.Component.Name != "runtime_config" || component.Component.Kind != "configuration" {
		t.Fatalf("component identity = %+v, want registered Configkit manager", component.Component)
	}
	if component.Registration.ReadinessPolicy != opskit.ReadinessRequired {
		t.Fatalf("registration policy = %q, want required", component.Registration.ReadinessPolicy)
	}
	if len(component.Readiness.Items) != 0 {
		t.Fatalf("component child items = %+v, want none", component.Readiness.Items)
	}
}

func TestManagerOpskitStatusAttributesAreSafe(t *testing.T) {
	const secret = "secret-token"
	manager := configkit.NewManager[stepsTestConfig]()
	source := configkit.NewBytesSource(
		[]byte(`{"name":"","enabled":true,"port":8080}`),
		configkit.SourceMetadata{Name: "secret-file-path", Kind: "file"},
		"rev-secret",
	)
	pipeline := testPipeline()
	pipeline.ValidateConfig = func(ctx context.Context, value stepsTestConfig) error {
		return errString(secret)
	}
	_, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindReload, source, pipeline)
	if err == nil {
		t.Fatal("load error = nil, want validation failure")
	}

	status := manager.Status(context.Background())
	attrs := opsAttributeMap(status.Attributes)
	wantKeys := map[string]bool{
		"config_state":        true,
		"has_current":         true,
		"source_kind":         true,
		"last_attempt_kind":   true,
		"last_attempt_status": true,
		"last_attempt_stage":  true,
	}
	for _, attr := range status.Attributes {
		if !wantKeys[attr.Key] {
			t.Fatalf("unexpected status attribute %q in %+v", attr.Key, status.Attributes)
		}
	}
	if attrs["source_kind"] != "file" {
		t.Fatalf("source_kind = %q, want file", attrs["source_kind"])
	}
	encoded, marshalErr := json.Marshal(status)
	if marshalErr != nil {
		t.Fatalf("marshal status: %v", marshalErr)
	}
	for _, unsafe := range []string{"secret-file-path", "rev-secret", secret} {
		if strings.Contains(string(encoded), unsafe) {
			t.Fatalf("opskit status = %s, must not contain %q", encoded, unsafe)
		}
	}

	inspection, inspectErr := manager.Inspect(context.Background())
	if inspectErr != nil {
		t.Fatalf("inspect: %v", inspectErr)
	}
	encoded, marshalErr = json.Marshal(inspection)
	if marshalErr != nil {
		t.Fatalf("marshal inspection: %v", marshalErr)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("opskit inspection = %s, must not contain internal error %q", encoded, secret)
	}
}

func TestManagerOpskitInspectIncludesLifecycleAndRedactedDetails(t *testing.T) {
	manager := loadedOpskitTestManager(t)

	inspection, err := manager.Inspect(context.Background())
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if inspection.Summary == nil {
		t.Fatal("inspection summary = nil, want summary")
	}
	if inspection.Details == nil {
		t.Fatal("inspection details = nil, want details")
	}
	encoded, err := json.Marshal(inspection)
	if err != nil {
		t.Fatalf("marshal inspection: %v", err)
	}
	body := string(encoded)
	for _, want := range []string{`"state":"loaded"`, `"ready":true`, `"recent_attempts"`, `"redacted"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("inspection = %s, want containing %s", body, want)
		}
	}
	if strings.Contains(body, `"enabled":true`) || strings.Contains(body, `"port":8080`) {
		t.Fatalf("inspection = %s, must not expose typed config fields", body)
	}
}

func TestManagerOpskitInspectSnapshotIsInternallyConsistent(t *testing.T) {
	manager := loadedOpskitTestManager(t)

	inspection, err := manager.Inspect(context.Background())
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	encoded, err := json.Marshal(inspection)
	if err != nil {
		t.Fatalf("marshal inspection: %v", err)
	}

	var decoded struct {
		Summary struct {
			State       configkit.LifecycleState     `json:"state"`
			Ready       bool                         `json:"ready"`
			Current     *configkit.SnapshotMetadata  `json:"current,omitempty"`
			LastAttempt *opskitAttemptSummaryFixture `json:"last_attempt,omitempty"`
		} `json:"summary"`
		Details struct {
			Redacted       configkit.RedactedView    `json:"redacted,omitempty"`
			RecentAttempts []configkit.AttemptRecord `json:"recent_attempts,omitempty"`
			LastApply      *configkit.ApplyResult    `json:"last_apply,omitempty"`
		} `json:"details"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal inspection: %v", err)
	}

	if decoded.Summary.State != configkit.LifecycleStateLoaded || !decoded.Summary.Ready {
		t.Fatalf("inspection summary = %+v, want loaded and ready", decoded.Summary)
	}
	if decoded.Summary.Current == nil {
		t.Fatal("summary current = nil, want current snapshot metadata")
	}
	if decoded.Details.LastApply == nil || decoded.Details.LastApply.Current == nil {
		t.Fatal("details last apply current = nil, want current snapshot metadata")
	}
	if decoded.Summary.Current.Checksum != decoded.Details.LastApply.Current.Checksum {
		t.Fatalf("summary current checksum = %q, last apply current checksum = %q", decoded.Summary.Current.Checksum, decoded.Details.LastApply.Current.Checksum)
	}
	if decoded.Summary.LastAttempt == nil {
		t.Fatal("summary last attempt = nil, want last attempt")
	}
	if len(decoded.Details.RecentAttempts) == 0 {
		t.Fatal("details recent attempts = empty, want retained attempt")
	}
	lastRecent := decoded.Details.RecentAttempts[len(decoded.Details.RecentAttempts)-1]
	if decoded.Summary.LastAttempt.ID != lastRecent.ID {
		t.Fatalf("summary last attempt id = %d, recent last id = %d", decoded.Summary.LastAttempt.ID, lastRecent.ID)
	}
	if got := decoded.Details.Redacted["name"]; got != "api" {
		t.Fatalf("redacted name = %v, want api", got)
	}
}

func TestManagerOpskitInspectReturnsContextError(t *testing.T) {
	manager := configkit.NewManager[stepsTestConfig]()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := manager.Inspect(ctx)
	if err == nil {
		t.Fatal("inspect error = nil, want context error")
	}
}

func loadedOpskitTestManager(t *testing.T, opts ...configkit.ManagerOption) *configkit.Manager[stepsTestConfig] {
	t.Helper()

	manager := configkit.NewManager[stepsTestConfig](opts...)
	if _, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindInitialLoad, opskitTestSource(`{"name":"api","enabled":true,"port":8080}`, "rev-1"), opskitTestPipeline()); err != nil {
		t.Fatalf("load manager: %v", err)
	}
	return manager
}

func failedOpskitTestManager(t *testing.T, opts ...configkit.ManagerOption) *configkit.Manager[stepsTestConfig] {
	t.Helper()

	manager := configkit.NewManager[stepsTestConfig](opts...)
	if _, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindInitialLoad, opskitTestSource(`{"name":`, "rev-1"), testPipeline()); err == nil {
		t.Fatal("load manager error = nil, want failure")
	}
	return manager
}

func degradedOpskitTestManager(t *testing.T, opts ...configkit.ManagerOption) *configkit.Manager[stepsTestConfig] {
	t.Helper()

	manager := loadedOpskitTestManager(t, opts...)
	if _, err := manager.LoadFromSource(context.Background(), configkit.AttemptKindReload, opskitTestSource(`{"name":`, "rev-2"), testPipeline()); err == nil {
		t.Fatal("reload error = nil, want failure")
	}
	return manager
}

func opskitTestSource(data string, revision string) configkit.Source {
	return configkit.NewBytesSource(
		[]byte(data),
		configkit.SourceMetadata{Name: "secret-file-path", Kind: "file"},
		revision,
	)
}

func opskitTestPipeline() configkit.Pipeline[stepsTestConfig] {
	pipeline := testPipeline()
	pipeline.Redact = func(ctx context.Context, value stepsTestConfig) (configkit.RedactedView, error) {
		return configkit.RedactedView{"name": value.Name}, nil
	}
	return pipeline
}

func opsAttributeMap(attrs []opskit.Attribute) map[string]string {
	out := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		out[attr.Key] = attr.Value
	}
	return out
}

type errString string

func (e errString) Error() string {
	return string(e)
}

type opskitAttemptSummaryFixture struct {
	ID     uint64                  `json:"id,omitempty"`
	Kind   configkit.AttemptKind   `json:"kind,omitempty"`
	Status configkit.AttemptStatus `json:"status"`
	Stage  configkit.AttemptStage  `json:"stage,omitempty"`
}
