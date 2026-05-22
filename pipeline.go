package configkit

import "errors"

// Pipeline validation errors identify missing required lifecycle steps.
var (
	ErrMissingDecoder  = errors.New("configkit: missing decoder")
	ErrMissingRedactor = errors.New("configkit: missing redactor")
	ErrMissingChecksum = errors.New("configkit: missing checksum")
)

// Pipeline describes the steps used to turn raw source data into a publishable
// configuration snapshot.
//
// Pipeline does not read configuration from a source and does not store the
// current snapshot. It only describes the transformation lifecycle for one load
// attempt.
type Pipeline[T any] struct {
	Decode         Decoder[T]
	ApplyDefaults  DefaultApplier[T]
	ValidateConfig Validator[T]
	Redact         Redactor[T]
	Checksum       Checksummer[T]
	Copy           Copier[T]
}

// Validate verifies that the pipeline has the required lifecycle steps.
func (p Pipeline[T]) Validate() error {
	if p.Decode == nil {
		return ErrMissingDecoder
	}
	if p.Redact == nil {
		return ErrMissingRedactor
	}
	if p.Checksum == nil {
		return ErrMissingChecksum
	}

	return nil
}
