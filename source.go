package configkit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

// ErrMissingSource is returned when LoadFromSource is called without a Source.
var ErrMissingSource = errors.New("configkit: missing source")

// SourceMetadata describes a configuration source without coupling Configkit to
// any specific backend.
type SourceMetadata struct {
	// Name is a human-readable source name, such as "env", "config.yaml",
	// "bootstrap", or "runtime-config". It may appear in logs, telemetry,
	// status, or inspection output, so it should not contain secrets or
	// sensitive tenant, user, path, or environment details.
	Name string `json:"name,omitempty"`

	// Kind is a broad source category, such as "env", "file", "memory",
	// "remote", or "composite".
	Kind string `json:"kind,omitempty"`

	// Description is optional safe operational context about the source. It may
	// be exposed through operational output and should not contain secrets.
	Description string `json:"description,omitempty"`
}

// Source reads raw configuration data from somewhere.
//
// A Source owns only source behavior: describing itself and reading
// configuration bytes. It does not decode, apply defaults, validate, redact,
// compute snapshot checksums, snapshot, reload, or decide application meaning.
// Read requires a non-nil context; passing nil is invalid and may panic.
type Source interface {
	Metadata() SourceMetadata
	Read(ctx context.Context) (SourceData, error)
}

// SourceData is the raw configuration data returned from a configuration source.
//
// SourceData does not decide how bytes are decoded into a typed config value.
// It only describes the payload and source information produced by reading a
// source.
type SourceData struct {
	// Data is the raw configuration payload.
	Data []byte

	// Metadata describes the source that produced this data.
	Metadata SourceMetadata

	// Revision is an optional source-provided version, generation, ETag,
	// resource version, commit SHA, or similar identifier. It may appear in
	// status, inspection, logs, telemetry, or future operational endpoints, so it
	// should not contain secrets or sensitive tenant, path, or environment
	// details.
	Revision string
}

// BytesSource is an in-memory Source backed by a byte slice.
//
// It is useful for tests, examples, embedded configuration, and callers that
// already have raw configuration bytes from another system.
type BytesSource struct {
	data     []byte
	metadata SourceMetadata
	revision string
}

// NewBytesSource creates an in-memory Source from raw configuration bytes.
//
// The input bytes are copied so later changes to the caller's slice do not
// change what the source returns.
func NewBytesSource(data []byte, metadata SourceMetadata, revision string) *BytesSource {
	return &BytesSource{
		data:     append([]byte(nil), data...),
		metadata: metadata,
		revision: revision,
	}
}

// Metadata returns the source metadata.
func (s *BytesSource) Metadata() SourceMetadata {
	return s.metadata
}

// Read returns the source data.
//
// The returned bytes are copied so callers cannot mutate the source's stored
// configuration data. Read requires a non-nil context; passing nil is invalid
// and may panic.
func (s *BytesSource) Read(ctx context.Context) (SourceData, error) {
	if err := ctx.Err(); err != nil {
		return SourceData{}, err
	}

	return SourceData{
		Data:     append([]byte(nil), s.data...),
		Metadata: s.metadata,
		Revision: s.revision,
	}, nil
}

// FileSource is a Source backed by a local file.
//
// FileSource uses a SHA-256 hash of the file bytes as the source revision.
type FileSource struct {
	path     string
	metadata SourceMetadata
}

// NewFileSource creates a Source that reads raw configuration bytes from path.
//
// If metadata.Name is empty, the base path is used. If metadata.Kind is
// empty, it defaults to "file".
func NewFileSource(path string, metadata SourceMetadata) *FileSource {
	if metadata.Name == "" && path != "" {
		metadata.Name = filepath.Base(path)
	}
	if metadata.Kind == "" {
		metadata.Kind = "file"
	}

	return &FileSource{
		path:     path,
		metadata: metadata,
	}
}

// Metadata returns the source metadata.
func (s *FileSource) Metadata() SourceMetadata {
	return s.metadata
}

// Read reads the current file contents.
//
// Read requires a non-nil context; passing nil is invalid and may panic.
func (s *FileSource) Read(ctx context.Context) (SourceData, error) {
	if err := ctx.Err(); err != nil {
		return SourceData{}, err
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return SourceData{}, err
	}

	if err := ctx.Err(); err != nil {
		return SourceData{}, err
	}

	return SourceData{
		Data:     data,
		Metadata: s.metadata,
		Revision: sourceBytesRevision(data),
	}, nil
}

func sourceBytesRevision(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
