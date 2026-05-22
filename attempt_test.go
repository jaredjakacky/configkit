package configkit_test

import (
	"encoding/json"
	"testing"
	"time"

	configkit "github.com/jaredjakacky/configkit"
)

func TestAttemptKindValues(t *testing.T) {
	tests := []struct {
		name string
		kind configkit.AttemptKind
		want string
	}{
		{name: "initial load", kind: configkit.AttemptKindInitialLoad, want: "initial_load"},
		{name: "reload", kind: configkit.AttemptKindReload, want: "reload"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tt.kind); got != tt.want {
				t.Fatalf("attempt kind = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAttemptStatusValues(t *testing.T) {
	tests := []struct {
		name   string
		status configkit.AttemptStatus
		want   string
	}{
		{name: "succeeded", status: configkit.AttemptStatusSucceeded, want: "succeeded"},
		{name: "failed", status: configkit.AttemptStatusFailed, want: "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tt.status); got != tt.want {
				t.Fatalf("attempt status = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAttemptStageValues(t *testing.T) {
	tests := []struct {
		name  string
		stage configkit.AttemptStage
		want  string
	}{
		{name: "context", stage: configkit.AttemptStageContext, want: "context"},
		{name: "source read", stage: configkit.AttemptStageSourceRead, want: "source_read"},
		{name: "pipeline validate", stage: configkit.AttemptStagePipelineValidate, want: "pipeline_validate"},
		{name: "decode", stage: configkit.AttemptStageDecode, want: "decode"},
		{name: "defaults", stage: configkit.AttemptStageDefaults, want: "defaults"},
		{name: "validate config", stage: configkit.AttemptStageValidateConfig, want: "validate_config"},
		{name: "copy", stage: configkit.AttemptStageCopy, want: "copy"},
		{name: "redact", stage: configkit.AttemptStageRedact, want: "redact"},
		{name: "checksum", stage: configkit.AttemptStageChecksum, want: "checksum"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tt.stage); got != tt.want {
				t.Fatalf("attempt stage = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAttemptRecordJSONFields(t *testing.T) {
	startedAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(time.Second)
	attempt := configkit.AttemptRecord{
		ID:        12,
		Kind:      configkit.AttemptKindReload,
		Status:    configkit.AttemptStatusFailed,
		Stage:     configkit.AttemptStageDecode,
		Source:    configkit.SourceMetadata{Name: "memory", Kind: "memory"},
		Revision:  "rev-1",
		Checksum:  "sum-1",
		StartedAt: startedAt,
		EndedAt:   endedAt,
		Error:     "decode failed",
	}

	data, err := json.Marshal(attempt)
	if err != nil {
		t.Fatalf("marshal attempt: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal attempt JSON: %v", err)
	}
	for _, key := range []string{"id", "kind", "status", "stage", "source", "revision", "checksum", "started_at", "ended_at", "error"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("attempt JSON missing key %q in %s", key, data)
		}
	}
	if got["kind"] != string(configkit.AttemptKindReload) {
		t.Fatalf("kind JSON = %v, want %q", got["kind"], configkit.AttemptKindReload)
	}
	if got["status"] != string(configkit.AttemptStatusFailed) {
		t.Fatalf("status JSON = %v, want %q", got["status"], configkit.AttemptStatusFailed)
	}
	if got["stage"] != string(configkit.AttemptStageDecode) {
		t.Fatalf("stage JSON = %v, want %q", got["stage"], configkit.AttemptStageDecode)
	}
}

func TestAttemptRecordJSONOmitEmptyOperationalFields(t *testing.T) {
	attempt := configkit.AttemptRecord{
		Status:    configkit.AttemptStatusSucceeded,
		Source:    configkit.SourceMetadata{},
		StartedAt: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
		EndedAt:   time.Date(2026, 5, 21, 12, 0, 1, 0, time.UTC),
	}

	data, err := json.Marshal(attempt)
	if err != nil {
		t.Fatalf("marshal attempt: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal attempt JSON: %v", err)
	}
	for _, key := range []string{"id", "kind", "stage", "revision", "checksum", "error"} {
		if _, ok := got[key]; ok {
			t.Fatalf("attempt JSON contains omitted key %q in %s", key, data)
		}
	}
	for _, key := range []string{"status", "source", "started_at", "ended_at"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("attempt JSON missing required key %q in %s", key, data)
		}
	}
}
