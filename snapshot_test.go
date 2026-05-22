package configkit_test

import (
	"testing"
	"time"

	configkit "github.com/jaredjakacky/configkit"
)

func TestSnapshotValueReturnsStoredValue(t *testing.T) {
	value := stepsTestConfig{
		Name:    "api",
		Enabled: true,
		Port:    8080,
	}
	snapshot := configkit.NewSnapshot(value, snapshotTestMetadata(), configkit.RedactedView{"name": "api"})

	if got := snapshot.Value(); got != value {
		t.Fatalf("value = %+v, want %+v", got, value)
	}
}

func TestSnapshotMetadataReturnsStoredMetadata(t *testing.T) {
	metadata := snapshotTestMetadata()
	snapshot := configkit.NewSnapshot(stepsTestConfig{Name: "api"}, metadata, configkit.RedactedView{"name": "api"})

	if got := snapshot.Metadata(); got != metadata {
		t.Fatalf("metadata = %+v, want %+v", got, metadata)
	}
}

func TestSnapshotCopiesRedactedViewOnCreate(t *testing.T) {
	redacted := configkit.RedactedView{
		"name": "api",
		"port": 8080,
	}
	snapshot := configkit.NewSnapshot(stepsTestConfig{Name: "api"}, snapshotTestMetadata(), redacted)
	redacted["name"] = "mutated"
	redacted["new"] = "value"

	got := snapshot.Redacted()
	if got["name"] != "api" {
		t.Fatalf("redacted name = %q, want %q", got["name"], "api")
	}
	if _, ok := got["new"]; ok {
		t.Fatalf("redacted view contains externally added key")
	}
}

func TestSnapshotRedactedReturnsCopy(t *testing.T) {
	snapshot := configkit.NewSnapshot(stepsTestConfig{Name: "api"}, snapshotTestMetadata(), configkit.RedactedView{"name": "api"})

	first := snapshot.Redacted()
	first["name"] = "mutated"
	first["new"] = "value"

	second := snapshot.Redacted()
	if second["name"] != "api" {
		t.Fatalf("redacted name after external mutation = %q, want %q", second["name"], "api")
	}
	if _, ok := second["new"]; ok {
		t.Fatalf("redacted view contains externally added key")
	}
}

func TestSnapshotNilRedactedViewRemainsNil(t *testing.T) {
	snapshot := configkit.NewSnapshot(stepsTestConfig{Name: "api"}, snapshotTestMetadata(), nil)

	if got := snapshot.Redacted(); got != nil {
		t.Fatalf("redacted view = %+v, want nil", got)
	}
}

func snapshotTestMetadata() configkit.SnapshotMetadata {
	return configkit.SnapshotMetadata{
		Source: configkit.SourceMetadata{
			Name: "memory",
			Kind: "memory",
		},
		Revision: "rev-1",
		Checksum: "sum-1",
		LoadedAt: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
	}
}
