package errors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/smithy-go"
)

func apiErr(code, message string) error {
	return &smithy.GenericAPIError{Code: code, Message: message}
}

func TestIsInstanceTypeUnavailable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "insufficient instance capacity",
			err:  apiErr("InsufficientInstanceCapacity", "We currently do not have sufficient t3.large capacity in the Availability Zone you requested"),
			want: true,
		},
		{
			name: "unsupported in availability zone",
			err:  apiErr("Unsupported", "Your requested instance type (t3.large) is not supported in your requested Availability Zone"),
			want: true,
		},
		{
			name: "invalid instance type",
			err:  apiErr("InvalidInstanceType", "The instance type is not valid"),
			want: true,
		},
		{
			name: "insufficient host capacity",
			err:  apiErr("InsufficientHostCapacity", "There is insufficient capacity on the host"),
			want: true,
		},
		{
			name: "invalid parameter value referencing instance type",
			err:  apiErr("InvalidParameterValue", "Invalid value 't3.large' for InstanceType. The instance type is not available"),
			want: true,
		},
		{
			name: "wrapped unavailable error is detected",
			err:  fmt.Errorf("could not create hybrid EC2 instance: %w", apiErr("Unsupported", "instance type not supported")),
			want: true,
		},
		{
			// This must not be treated as an unavailable instance type. It is retryable
			// while the IAM instance profile propagates, and falling through to another
			// instance type would not help.
			name: "invalid parameter value for iam instance profile",
			err:  apiErr("InvalidParameterValue", "Value (test-profile) for parameter iamInstanceProfile.arn is invalid"),
			want: false,
		},
		{
			// Quota errors are shared across instance families, so a different type
			// would hit the same limit. These must surface rather than fall through.
			name: "vcpu limit exceeded",
			err:  apiErr("VcpuLimitExceeded", "You have requested more vCPU capacity than your current vCPU limit"),
			want: false,
		},
		{
			name: "instance limit exceeded",
			err:  apiErr("InstanceLimitExceeded", "You have reached your quota for maximum number of instances"),
			want: false,
		},
		{
			name: "unrelated aws error",
			err:  apiErr("UnauthorizedOperation", "You are not authorized to perform this operation"),
			want: false,
		},
		{
			name: "non aws error",
			err:  errors.New("some other failure"),
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsInstanceTypeUnavailable(tc.err); got != tc.want {
				t.Errorf("IsInstanceTypeUnavailable() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsInvalidInstanceTypeParameter(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "instance type message",
			err:  apiErr("InvalidParameterValue", "Invalid value for InstanceType. The instance type is not available"),
			want: true,
		},
		{
			name: "iam instance profile message is retryable",
			err:  apiErr("InvalidParameterValue", "Value (test-profile) for parameter iamInstanceProfile.arn is invalid"),
			want: false,
		},
		{
			name: "different error code",
			err:  apiErr("Unsupported", "instance type not supported"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsInvalidInstanceTypeParameter(tc.err); got != tc.want {
				t.Errorf("IsInvalidInstanceTypeParameter() = %v, want %v", got, tc.want)
			}
		})
	}
}
