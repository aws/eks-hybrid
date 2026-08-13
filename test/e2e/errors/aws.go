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

func IsAwsError(err error, code string) bool {
	var awsErr smithy.APIError
	ok := errors.As(err, &awsErr)
	return err != nil && ok && awsErr.ErrorCode() == code
}

// instanceTypeUnavailableCodes are the RunInstances error codes that indicate the
// requested instance type could not be launched, and that trying a different
// instance type is likely to succeed.
//
// Account level quota errors (VcpuLimitExceeded, InstanceLimitExceeded) are
// deliberately excluded as all instance types would run into the same issue.
var instanceTypeUnavailableCodes = []string{
	// The instance type is not offered in the requested region or availability zone.
	"Unsupported",
	"InvalidInstanceType",
	// AWS does not currently have enough capacity for this instance type in the AZ.
	// This is transient, but trying another type is cheaper than waiting.
	"InsufficientInstanceCapacity",
	"InsufficientHostCapacity",
}

// IsInstanceTypeUnavailable returns true if the error indicates the requested instance
// type cannot be launched and a different instance type should be tried instead.
func IsInstanceTypeUnavailable(err error) bool {
	if err == nil {
		return false
	}

	for _, code := range instanceTypeUnavailableCodes {
		if IsAwsError(err, code) {
			return true
		}
	}

	// InvalidParameterValue is returned for an unusable instance type but also for
	// a not-yet-propagated IAM instance profile, which is retryable.
	return IsInvalidInstanceTypeParameter(err)
}

// IsInvalidInstanceTypeParameter returns true if the error is an InvalidParameterValue
// error whose message refers to the instance type. This distinguishes a permanently
// unusable instance type from other InvalidParameterValue causes, such as IAM instance
// profile propagation delays.
func IsInvalidInstanceTypeParameter(err error) bool {
	var ae smithy.APIError
	if !errors.As(err, &ae) || ae.ErrorCode() != "InvalidParameterValue" {
		return false
	}

	return strings.Contains(strings.ToLower(ae.ErrorMessage()), "instance type")
}

// IsIAMRoleNotFound returns true if the error is an IAM role not found error.
func IsIAMRoleNotFound(err error) bool {
	if IsType(err, &iamTypes.NoSuchEntityException{}) {
		return true
	}

	var ae smithy.APIError
	return errors.As(err, &ae) &&
		ae.ErrorCode() == "NoSuchEntity"
}
