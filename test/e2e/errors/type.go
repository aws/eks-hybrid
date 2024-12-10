package errors

import "errors"

// IsType returns true if any errors in the chain are of the specified type.
func IsType(err error, targetType interface{}) bool {
	return errors.As(err, &targetType)
}
