package errors

import (
	"errors"
	"strings"

	iamTypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	s3Types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// IsCFNStackNotFound returns true if the error is a CloudFormation stack not found error.
func IsCFNStackNotFound(err error) bool {
	var ae smithy.APIError
	return errors.As(err, &ae) &&
		ae.ErrorCode() == "ValidationError" &&
		strings.Contains(ae.ErrorMessage(), "does not exist")
}

// IsS3BucketNotFound returns true if the error is an S3 bucket not found error.
// The sdk does not always return the specific NoSuchBucket error, so we check for the generic NoSuchBucket error code.
func IsS3BucketNotFound(err error) bool {
	if IsType(err, &s3Types.NoSuchBucket{}) {
		return true
	}

	var ae smithy.APIError
	return errors.As(err, &ae) &&
		ae.ErrorCode() == "NoSuchBucket"
}

func IsIAMRoleNotFound(err error) bool {
	if IsType(err, &iamTypes.NoSuchEntityException{}) {
		return true
	}

	var ae smithy.APIError
	return errors.As(err, &ae) &&
		ae.ErrorCode() == "NoSuchEntityException"
}

func IsAwsError(err error, code string) bool {
	var awsErr smithy.APIError
	ok := errors.As(err, &awsErr)
	return err != nil && ok && awsErr.ErrorCode() == code
}
