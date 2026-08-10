package configkit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
)

// Decoder turns raw source data into a typed configuration value.
//
// Applications can provide their own Decoder when they need a different input
// format, custom parsing rules, source-specific behavior, preprocessing,
// decryption, merging, or validation during decode. Configkit calls Decoder
// with a non-nil context, and direct callers must do the same.
type Decoder[T any] func(ctx context.Context, data SourceData) (T, error)

// JSONDecoder returns a Decoder that decodes SourceData.Data as exactly one
// JSON value using the standard library encoding/json package.
//
// JSONDecoder rejects object fields that do not match exported, non-ignored
// fields when decoding into Go structs. Field matching otherwise follows
// encoding/json semantics, including case-insensitive matches and acceptance of
// duplicate object keys. Unknown keys remain valid when decoding into maps, and
// custom UnmarshalJSON implementations control their own decoding behavior.
// Leading and trailing whitespace are accepted; trailing additional JSON values
// or other non-whitespace data are rejected.
func JSONDecoder[T any]() Decoder[T] {
	return func(ctx context.Context, data SourceData) (T, error) {
		var value T
		decoder := json.NewDecoder(bytes.NewReader(data.Data))
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(&value); err != nil {
			return value, err
		}
		if err := requireJSONEOF(decoder); err != nil {
			return value, err
		}

		return value, nil
	}
}

// LenientJSONDecoder returns a Decoder that preserves encoding/json.Unmarshal
// behavior for SourceData.Data.
//
// Unlike JSONDecoder, LenientJSONDecoder ignores unknown object fields when
// decoding into Go structs. It still requires exactly one JSON value, permits
// leading and trailing whitespace, and rejects trailing additional values or
// other non-whitespace data.
func LenientJSONDecoder[T any]() Decoder[T] {
	return func(ctx context.Context, data SourceData) (T, error) {
		var value T

		if err := json.Unmarshal(data.Data, &value); err != nil {
			return value, err
		}

		return value, nil
	}
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing json.RawMessage
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}

	return errors.New("configkit: JSON input must contain exactly one value")
}

// DefaultApplier applies mechanical defaults to a typed configuration value.
//
// A DefaultApplier does not load, decode, validate, redact, snapshot, reload,
// or decide application meaning. It only fills in default values before
// validation runs. Configkit calls DefaultApplier with a non-nil context, and
// direct callers must do the same.
type DefaultApplier[T any] func(ctx context.Context, value T) (T, error)

// Copier returns the final configuration value to publish in a snapshot.
//
// A Copier can break references shared with earlier pipeline stages before
// publication. It does not affect values later returned by Snapshot.Value.
// Redaction and checksum run after copying. Configkit calls Copier with a
// non-nil context, and direct callers must do the same.
type Copier[T any] func(ctx context.Context, value T) (T, error)

// Validator checks whether a typed configuration value is valid.
//
// A Validator owns validation only. It does not load, decode, redact,
// snapshot, reload, or decide how configuration is stored. Configkit returns a
// validation error privately to the load caller and records only stable public
// failure detail in operational output. Configkit calls Validator with a non-nil
// context, and direct callers must do the same.
type Validator[T any] func(ctx context.Context, value T) error

// Redactor builds the safe operational view for a configuration value.
//
// The returned RedactedView is intended for logs, diagnostics, status output,
// admin endpoints, support workflows, and other places where raw configuration
// should not be exposed.
//
// Applications decide what is safe to show. A Redactor can omit fields, mask
// secrets, flatten nested structs, rename fields, or match an existing
// inspection format. Redactors should be conservative because Configkit cannot
// verify whether returned values are safe. Configkit calls Redactor with a
// non-nil context, and direct callers must do the same.
type Redactor[T any] func(ctx context.Context, value T) (RedactedView, error)

// EmptyRedactor returns a Redactor that exposes no configuration fields.
//
// This is the safest built-in redactor. It lets applications use snapshots,
// metadata, checksums, load status, and manager state without accidentally
// exposing configuration values.
//
// Applications that want useful operational inspection should provide their own
// Redactor and explicitly choose which fields are safe to expose.
func EmptyRedactor[T any]() Redactor[T] {
	return func(ctx context.Context, value T) (RedactedView, error) {
		return RedactedView{}, nil
	}
}

// Checksummer computes a stable fingerprint for a typed configuration value.
//
// Applications can provide their own Checksummer when they need custom
// canonicalization, field exclusion, secret handling, source-provided versions,
// or a checksum format that matches existing operational tooling. Checksums are
// operational fingerprints, not redaction or secrecy mechanisms; avoid exposed
// checksums for low-entropy or secret-bearing config values. Configkit calls
// Checksummer with a non-nil context, and direct callers must do the same.
type Checksummer[T any] func(ctx context.Context, value T) (string, error)

// SHA256JSONChecksum returns a Checksummer that computes a hex-encoded SHA-256
// checksum from the JSON representation of the effective configuration value.
//
// This is the vanilla checksum implementation for ordinary Go config structs.
// Applications can wire a different Checksummer into Pipeline.Checksum when
// they need stricter canonicalization or different checksum behavior. Do not
// expose this checksum for low-entropy or secret-bearing config values unless
// that fingerprint is acceptable operational data.
func SHA256JSONChecksum[T any]() Checksummer[T] {
	return func(ctx context.Context, value T) (string, error) {
		data, err := json.Marshal(value)
		if err != nil {
			return "", err
		}

		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:]), nil
	}
}
