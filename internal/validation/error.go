package validation

import (
	"errors"
	"fmt"
)

// Remediable is an error that provides a possible remediation.
type Remediable interface {
	Remediation() string
}

// remediableError implements Fixable around a generic error.
type remediableError struct {
	error
	remediation string
}

// Remediation returns a possible solution to the error.
func (e *remediableError) Remediation() string {
	return e.remediation
}

// NewRemediableErr returns a new [Remediable] error.
func NewRemediableErr(err, remediation string) error {
	return &remediableError{
		error:       errors.New(err),
		remediation: remediation,
	}
}

// WithRemediation makes an error [Remediable].
func WithRemediation(err error, remediation string) error {
	return &remediableError{
		error:       err,
		remediation: remediation,
	}
}

// IsRemediable checks if an error has a remediation.
func IsRemediable(err error) bool {
	_, ok := err.(Remediable)
	return ok
}

// Remediation returns the Remediation message for an error if it has it.
// Otherwise it returns an empty string.
func Remediation(err error) string {
	fixable, ok := err.(Remediable)
	if !ok {
		return ""
	}

	return fixable.Remediation()
}

// FlattenRemediation flattens a [Remediable] error into a simple error
// by appending the remediation to the error message.
// If the error is not [Remediable], it returns the original error.
func FlattenRemediation(err error) error {
	rem, ok := err.(*remediableError)
	if !ok {
		return err
	}

	return fmt.Errorf("%w: %s", rem.error, rem.remediation)
}
