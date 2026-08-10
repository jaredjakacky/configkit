package configkit_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	configkit "github.com/jaredjakacky/configkit"
)

type stepsTestConfig struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port"`
}

type nestedJSONTestConfig struct {
	Server struct {
		Port int `json:"port"`
	} `json:"server"`
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

func TestJSONDecoderReturnsDecodeError(t *testing.T) {
	decode := configkit.JSONDecoder[stepsTestConfig]()

	if _, err := decode(context.Background(), configkit.SourceData{Data: []byte(`{"name":`)}); err == nil {
		t.Fatal("decode invalid JSON error = nil, want error")
	}
}

func TestJSONDecoderRejectsUnknownFields(t *testing.T) {
	_, err := configkit.JSONDecoder[stepsTestConfig]()(context.Background(), configkit.SourceData{
		Data: []byte(`{"name":"api","unknown":true}`),
	})
	if err == nil || !strings.Contains(err.Error(), `unknown field "unknown"`) {
		t.Fatalf("decode error = %v, want unknown field error", err)
	}
}

func TestJSONDecoderRejectsNestedUnknownFields(t *testing.T) {
	_, err := configkit.JSONDecoder[nestedJSONTestConfig]()(context.Background(), configkit.SourceData{
		Data: []byte(`{"server":{"port":8080,"unknown":true}}`),
	})
	if err == nil || !strings.Contains(err.Error(), `unknown field "unknown"`) {
		t.Fatalf("decode error = %v, want nested unknown field error", err)
	}
}

func TestJSONDecoderUsesEncodingJSONFieldMatching(t *testing.T) {
	decode := configkit.JSONDecoder[stepsTestConfig]()

	got, err := decode(context.Background(), configkit.SourceData{
		Data: []byte(`{"NaMe":"api","ENABLED":true,"PoRt":8080}`),
	})
	if err != nil {
		t.Fatalf("decode case-insensitive fields: %v", err)
	}
	if got != (stepsTestConfig{Name: "api", Enabled: true, Port: 8080}) {
		t.Fatalf("decoded config = %+v, want case-insensitive field matches", got)
	}
}

func TestJSONDecoderUsesEncodingJSONDuplicateFieldSemantics(t *testing.T) {
	decode := configkit.JSONDecoder[stepsTestConfig]()

	got, err := decode(context.Background(), configkit.SourceData{
		Data: []byte(`{"port":8080,"port":9090}`),
	})
	if err != nil {
		t.Fatalf("decode duplicate fields: %v", err)
	}
	if got.Port != 9090 {
		t.Fatalf("decoded port = %d, want later duplicate value 9090", got.Port)
	}
}

func TestJSONDecoderAllowsArbitraryMapKeys(t *testing.T) {
	decode := configkit.JSONDecoder[map[string]any]()

	got, err := decode(context.Background(), configkit.SourceData{
		Data: []byte(`{"known":true,"arbitrary":"value"}`),
	})
	if err != nil {
		t.Fatalf("decode map: %v", err)
	}
	if got["arbitrary"] != "value" {
		t.Fatalf("decoded map = %+v, want arbitrary key", got)
	}
}

func TestJSONDecoderEnforcesExactlyOneValue(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr bool
	}{
		{name: "trailing whitespace", data: "{\"name\":\"api\"} \n\t"},
		{name: "second value", data: `{"name":"api"} {"name":"other"}`, wantErr: true},
		{name: "trailing data", data: `{"name":"api"} trailing`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := configkit.JSONDecoder[stepsTestConfig]()(context.Background(), configkit.SourceData{Data: []byte(tt.data)})
			if tt.wantErr && err == nil {
				t.Fatal("decode error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("decode error = %v, want nil", err)
			}
		})
	}
}

func TestLenientJSONDecoderPreservesUnmarshalBehavior(t *testing.T) {
	decode := configkit.LenientJSONDecoder[stepsTestConfig]()

	got, err := decode(context.Background(), configkit.SourceData{
		Data: []byte(`{"name":"api","unknown":true}`),
	})
	if err != nil {
		t.Fatalf("decode unknown field: %v", err)
	}
	if got.Name != "api" {
		t.Fatalf("decoded config = %+v, want known field populated", got)
	}

	if _, err := decode(context.Background(), configkit.SourceData{
		Data: []byte(`{"name":"api"} {"name":"other"}`),
	}); err == nil {
		t.Fatal("decode trailing value error = nil, want error")
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
