package configkit_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	configkit "github.com/jaredjakacky/configkit"
)

type stepsTestConfig struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port"`
}

func TestJSONDecoderDecodesSourceData(t *testing.T) {
	decode := configkit.JSONDecoder[stepsTestConfig]()

	got, err := decode(context.Background(), configkit.SourceData{
		Data: []byte(`{"name":"api","enabled":true,"port":8080}`),
	})
	if err != nil {
		t.Fatalf("decode JSON config: %v", err)
	}

	want := stepsTestConfig{
		Name:    "api",
		Enabled: true,
		Port:    8080,
	}
	if got != want {
		t.Fatalf("decoded config = %+v, want %+v", got, want)
	}
}

func TestJSONDecoderReturnsUnmarshalError(t *testing.T) {
	decode := configkit.JSONDecoder[stepsTestConfig]()

	if _, err := decode(context.Background(), configkit.SourceData{Data: []byte(`{"name":`)}); err == nil {
		t.Fatal("decode invalid JSON error = nil, want error")
	}
}

func TestEmptyRedactorReturnsEmptyView(t *testing.T) {
	redact := configkit.EmptyRedactor[stepsTestConfig]()

	got, err := redact(context.Background(), stepsTestConfig{Name: "api"})
	if err != nil {
		t.Fatalf("redact config: %v", err)
	}
	if got == nil {
		t.Fatal("redacted view = nil, want empty map")
	}
	if len(got) != 0 {
		t.Fatalf("redacted view len = %d, want 0", len(got))
	}
}

func TestSHA256JSONChecksumHashesJSONRepresentation(t *testing.T) {
	checksum := configkit.SHA256JSONChecksum[stepsTestConfig]()
	value := stepsTestConfig{
		Name:    "api",
		Enabled: true,
		Port:    8080,
	}

	got, err := checksum(context.Background(), value)
	if err != nil {
		t.Fatalf("checksum config: %v", err)
	}

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal expected config: %v", err)
	}
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("checksum = %q, want %q", got, want)
	}
}

func TestSHA256JSONChecksumReturnsMarshalError(t *testing.T) {
	checksum := configkit.SHA256JSONChecksum[chan int]()

	if _, err := checksum(context.Background(), make(chan int)); err == nil {
		t.Fatal("checksum unsupported value error = nil, want error")
	}
}
