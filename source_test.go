package configkit_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	configkit "github.com/jaredjakacky/configkit"
)

func TestBytesSourceMetadata(t *testing.T) {
	metadata := configkit.SourceMetadata{
		Name:        "memory-config",
		Kind:        "memory",
		Description: "test config",
	}
	source := configkit.NewBytesSource([]byte(`{"name":"api"}`), metadata, "rev-1")

	if got := source.Metadata(); got != metadata {
		t.Fatalf("metadata = %+v, want %+v", got, metadata)
	}
}

func TestNewBytesSourceCopiesInputBytes(t *testing.T) {
	input := []byte(`{"name":"api"}`)
	source := configkit.NewBytesSource(input, configkit.SourceMetadata{Name: "memory", Kind: "memory"}, "rev-1")
	input[0] = '['

	got, err := source.Read(context.Background())
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	if got, want := string(got.Data), `{"name":"api"}`; got != want {
		t.Fatalf("data = %q, want %q", got, want)
	}
}

func TestBytesSourceReadReturnsCopiedData(t *testing.T) {
	source := configkit.NewBytesSource([]byte(`{"name":"api"}`), configkit.SourceMetadata{Name: "memory", Kind: "memory"}, "rev-1")

	first, err := source.Read(context.Background())
	if err != nil {
		t.Fatalf("read source: %v", err)
	}

	first.Data[0] = '['
	second, err := source.Read(context.Background())
	if err != nil {
		t.Fatalf("read source again: %v", err)
	}
	if got, want := string(second.Data), `{"name":"api"}`; got != want {
		t.Fatalf("second read data = %q, want %q", got, want)
	}
}

func TestBytesSourceReadReturnsSourceData(t *testing.T) {
	metadata := configkit.SourceMetadata{
		Name:        "memory-config",
		Kind:        "memory",
		Description: "safe description",
	}
	source := configkit.NewBytesSource([]byte(`{"name":"api"}`), metadata, "rev-1")

	got, err := source.Read(context.Background())
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	if gotData, want := string(got.Data), `{"name":"api"}`; gotData != want {
		t.Fatalf("data = %q, want %q", gotData, want)
	}
	if got.Metadata != metadata {
		t.Fatalf("metadata = %+v, want %+v", got.Metadata, metadata)
	}
	if got.Revision != "rev-1" {
		t.Fatalf("revision = %q, want %q", got.Revision, "rev-1")
	}
}

func TestBytesSourceReadReturnsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := configkit.NewBytesSource([]byte(`{}`), configkit.SourceMetadata{}, "")

	_, err := source.Read(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("read canceled source error = %v, want context.Canceled", err)
	}
}

func TestNewFileSourceDefaultsMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	source := configkit.NewFileSource(path, configkit.SourceMetadata{})

	got := source.Metadata()
	if got.Name != "config.json" {
		t.Fatalf("metadata name = %q, want %q", got.Name, "config.json")
	}
	if got.Kind != "file" {
		t.Fatalf("metadata kind = %q, want %q", got.Kind, "file")
	}
}

func TestNewFileSourceDefaultsOnlyEmptyMetadataFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	t.Run("default empty name only", func(t *testing.T) {
		source := configkit.NewFileSource(path, configkit.SourceMetadata{Kind: "local"})

		got := source.Metadata()
		if got.Name != "config.json" {
			t.Fatalf("metadata name = %q, want %q", got.Name, "config.json")
		}
		if got.Kind != "local" {
			t.Fatalf("metadata kind = %q, want %q", got.Kind, "local")
		}
	})

	t.Run("default empty kind only", func(t *testing.T) {
		source := configkit.NewFileSource(path, configkit.SourceMetadata{Name: "runtime-config"})

		got := source.Metadata()
		if got.Name != "runtime-config" {
			t.Fatalf("metadata name = %q, want %q", got.Name, "runtime-config")
		}
		if got.Kind != "file" {
			t.Fatalf("metadata kind = %q, want %q", got.Kind, "file")
		}
	})
}

func TestNewFileSourcePreservesMetadata(t *testing.T) {
	metadata := configkit.SourceMetadata{
		Name:        "custom-name",
		Kind:        "custom-kind",
		Description: "safe description",
	}
	source := configkit.NewFileSource("config.json", metadata)

	if got := source.Metadata(); got != metadata {
		t.Fatalf("metadata = %+v, want %+v", got, metadata)
	}
}

func TestFileSourceMetadataDoesNotExposeFullPathByDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	source := configkit.NewFileSource(path, configkit.SourceMetadata{})

	got := source.Metadata()
	if got.Name == path {
		t.Fatalf("metadata name = full path %q, want base filename only", got.Name)
	}
	if got.Name != "config.json" {
		t.Fatalf("metadata name = %q, want %q", got.Name, "config.json")
	}
	if got.Description != "" {
		t.Fatalf("metadata description = %q, want empty", got.Description)
	}
}

func TestFileSourceReadReturnsFileDataAndRevision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := []byte(`{"name":"api"}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	metadata := configkit.SourceMetadata{Name: "config-file", Kind: "file"}
	source := configkit.NewFileSource(path, metadata)

	got, err := source.Read(context.Background())
	if err != nil {
		t.Fatalf("read file source: %v", err)
	}
	if string(got.Data) != string(data) {
		t.Fatalf("data = %q, want %q", string(got.Data), string(data))
	}
	if got.Metadata != metadata {
		t.Fatalf("metadata = %+v, want %+v", got.Metadata, metadata)
	}
	if got.Revision != expectedSourceRevision(data) {
		t.Fatalf("revision = %q, want %q", got.Revision, expectedSourceRevision(data))
	}
}

func TestFileSourceReadReturnsContextErrorBeforeRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := configkit.NewFileSource("missing.json", configkit.SourceMetadata{})

	_, err := source.Read(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("read canceled file source error = %v, want context.Canceled", err)
	}
}

func TestFileSourceReadReturnsFileError(t *testing.T) {
	source := configkit.NewFileSource(filepath.Join(t.TempDir(), "missing.json"), configkit.SourceMetadata{})

	_, err := source.Read(context.Background())
	if err == nil {
		t.Fatal("read missing file error = nil, want error")
	}
}

func expectedSourceRevision(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
