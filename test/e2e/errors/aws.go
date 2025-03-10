package errors

import (
	"errors"
	"strings"

	"github.com/aws/smithy-go"
)

// IsCFNStackNotFound returns true if the error is a CloudFormation stack not found error.
func IsCFNStackNotFound(err error) bool {
	var ae smithy.APIError
	return errors.As(err, &ae) &&
		ae.ErrorCode() == "ValidationError" &&
		strings.Contains(ae.ErrorMessage(), "does not exist")
}
