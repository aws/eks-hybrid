package errors

import "errors"

// IsType returns true if any errors in the chain are of the specified type.
func IsType[T error](err error, targetType T) bool {
	return errors.As(err, &targetType)
}
