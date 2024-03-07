// Code generated by smithy-go-codegen DO NOT EDIT.

package types

import (
	"fmt"
	smithy "github.com/aws/smithy-go"
)

// You don't have permissions to perform the requested operation. The IAM principal (https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_terms-and-concepts.html)
// making the request must have at least one IAM permissions policy attached that
// grants the required permissions. For more information, see Access management (https://docs.aws.amazon.com/IAM/latest/UserGuide/access.html)
// in the IAM User Guide.
type AccessDeniedException struct {
	Message *string

	ErrorCodeOverride *string

	noSmithyDocumentSerde
}

func (e *AccessDeniedException) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorCode(), e.ErrorMessage())
}
func (e *AccessDeniedException) ErrorMessage() string {
	if e.Message == nil {
		return ""
	}
	return *e.Message
}
func (e *AccessDeniedException) ErrorCode() string {
	if e == nil || e.ErrorCodeOverride == nil {
		return "AccessDeniedException"
	}
	return *e.ErrorCodeOverride
}
func (e *AccessDeniedException) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

// This exception is thrown if the request contains a semantic error. The precise
// meaning will depend on the API, and will be documented in the error message.
type BadRequestException struct {
	Message *string

	ErrorCodeOverride *string

	noSmithyDocumentSerde
}

func (e *BadRequestException) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorCode(), e.ErrorMessage())
}
func (e *BadRequestException) ErrorMessage() string {
	if e.Message == nil {
		return ""
	}
	return *e.Message
}
func (e *BadRequestException) ErrorCode() string {
	if e == nil || e.ErrorCodeOverride == nil {
		return "BadRequestException"
	}
	return *e.ErrorCodeOverride
}
func (e *BadRequestException) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

// These errors are usually caused by a client action. Actions can include using
// an action or resource on behalf of an IAM principal (https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_terms-and-concepts.html)
// that doesn't have permissions to use the action or resource or specifying an
// identifier that is not valid.
type ClientException struct {
	Message *string

	ErrorCodeOverride *string

	ClusterName    *string
	NodegroupName  *string
	AddonName      *string
	SubscriptionId *string

	noSmithyDocumentSerde
}

func (e *ClientException) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorCode(), e.ErrorMessage())
}
func (e *ClientException) ErrorMessage() string {
	if e.Message == nil {
		return ""
	}
	return *e.Message
}
func (e *ClientException) ErrorCode() string {
	if e == nil || e.ErrorCodeOverride == nil {
		return "ClientException"
	}
	return *e.ErrorCodeOverride
}
func (e *ClientException) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

// The specified parameter is invalid. Review the available parameters for the API
// request.
type InvalidParameterException struct {
	Message *string

	ErrorCodeOverride *string

	ClusterName        *string
	NodegroupName      *string
	FargateProfileName *string
	AddonName          *string
	SubscriptionId     *string

	noSmithyDocumentSerde
}

func (e *InvalidParameterException) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorCode(), e.ErrorMessage())
}
func (e *InvalidParameterException) ErrorMessage() string {
	if e.Message == nil {
		return ""
	}
	return *e.Message
}
func (e *InvalidParameterException) ErrorCode() string {
	if e == nil || e.ErrorCodeOverride == nil {
		return "InvalidParameterException"
	}
	return *e.ErrorCodeOverride
}
func (e *InvalidParameterException) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

// The request is invalid given the state of the cluster. Check the state of the
// cluster and the associated operations.
type InvalidRequestException struct {
	Message *string

	ErrorCodeOverride *string

	ClusterName    *string
	NodegroupName  *string
	AddonName      *string
	SubscriptionId *string

	noSmithyDocumentSerde
}

func (e *InvalidRequestException) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorCode(), e.ErrorMessage())
}
func (e *InvalidRequestException) ErrorMessage() string {
	if e.Message == nil {
		return ""
	}
	return *e.Message
}
func (e *InvalidRequestException) ErrorCode() string {
	if e == nil || e.ErrorCodeOverride == nil {
		return "InvalidRequestException"
	}
	return *e.ErrorCodeOverride
}
func (e *InvalidRequestException) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

// A service resource associated with the request could not be found. Clients
// should not retry such requests.
type NotFoundException struct {
	Message *string

	ErrorCodeOverride *string

	noSmithyDocumentSerde
}

func (e *NotFoundException) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorCode(), e.ErrorMessage())
}
func (e *NotFoundException) ErrorMessage() string {
	if e.Message == nil {
		return ""
	}
	return *e.Message
}
func (e *NotFoundException) ErrorCode() string {
	if e == nil || e.ErrorCodeOverride == nil {
		return "NotFoundException"
	}
	return *e.ErrorCodeOverride
}
func (e *NotFoundException) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

// The specified resource is in use.
type ResourceInUseException struct {
	Message *string

	ErrorCodeOverride *string

	ClusterName   *string
	NodegroupName *string
	AddonName     *string

	noSmithyDocumentSerde
}

func (e *ResourceInUseException) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorCode(), e.ErrorMessage())
}
func (e *ResourceInUseException) ErrorMessage() string {
	if e.Message == nil {
		return ""
	}
	return *e.Message
}
func (e *ResourceInUseException) ErrorCode() string {
	if e == nil || e.ErrorCodeOverride == nil {
		return "ResourceInUseException"
	}
	return *e.ErrorCodeOverride
}
func (e *ResourceInUseException) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

// You have encountered a service limit on the specified resource.
type ResourceLimitExceededException struct {
	Message *string

	ErrorCodeOverride *string

	ClusterName    *string
	NodegroupName  *string
	SubscriptionId *string

	noSmithyDocumentSerde
}

func (e *ResourceLimitExceededException) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorCode(), e.ErrorMessage())
}
func (e *ResourceLimitExceededException) ErrorMessage() string {
	if e.Message == nil {
		return ""
	}
	return *e.Message
}
func (e *ResourceLimitExceededException) ErrorCode() string {
	if e == nil || e.ErrorCodeOverride == nil {
		return "ResourceLimitExceededException"
	}
	return *e.ErrorCodeOverride
}
func (e *ResourceLimitExceededException) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

// The specified resource could not be found. You can view your available clusters
// with ListClusters . You can view your available managed node groups with
// ListNodegroups . Amazon EKS clusters and node groups are Amazon Web Services
// Region specific.
type ResourceNotFoundException struct {
	Message *string

	ErrorCodeOverride *string

	ClusterName        *string
	NodegroupName      *string
	FargateProfileName *string
	AddonName          *string
	SubscriptionId     *string

	noSmithyDocumentSerde
}

func (e *ResourceNotFoundException) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorCode(), e.ErrorMessage())
}
func (e *ResourceNotFoundException) ErrorMessage() string {
	if e.Message == nil {
		return ""
	}
	return *e.Message
}
func (e *ResourceNotFoundException) ErrorCode() string {
	if e == nil || e.ErrorCodeOverride == nil {
		return "ResourceNotFoundException"
	}
	return *e.ErrorCodeOverride
}
func (e *ResourceNotFoundException) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

// Required resources (such as service-linked roles) were created and are still
// propagating. Retry later.
type ResourcePropagationDelayException struct {
	Message *string

	ErrorCodeOverride *string

	noSmithyDocumentSerde
}

func (e *ResourcePropagationDelayException) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorCode(), e.ErrorMessage())
}
func (e *ResourcePropagationDelayException) ErrorMessage() string {
	if e.Message == nil {
		return ""
	}
	return *e.Message
}
func (e *ResourcePropagationDelayException) ErrorCode() string {
	if e == nil || e.ErrorCodeOverride == nil {
		return "ResourcePropagationDelayException"
	}
	return *e.ErrorCodeOverride
}
func (e *ResourcePropagationDelayException) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

// These errors are usually caused by a server-side issue.
type ServerException struct {
	Message *string

	ErrorCodeOverride *string

	ClusterName    *string
	NodegroupName  *string
	AddonName      *string
	SubscriptionId *string

	noSmithyDocumentSerde
}

func (e *ServerException) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorCode(), e.ErrorMessage())
}
func (e *ServerException) ErrorMessage() string {
	if e.Message == nil {
		return ""
	}
	return *e.Message
}
func (e *ServerException) ErrorCode() string {
	if e == nil || e.ErrorCodeOverride == nil {
		return "ServerException"
	}
	return *e.ErrorCodeOverride
}
func (e *ServerException) ErrorFault() smithy.ErrorFault { return smithy.FaultServer }

// The service is unavailable. Back off and retry the operation.
type ServiceUnavailableException struct {
	Message *string

	ErrorCodeOverride *string

	noSmithyDocumentSerde
}

func (e *ServiceUnavailableException) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorCode(), e.ErrorMessage())
}
func (e *ServiceUnavailableException) ErrorMessage() string {
	if e.Message == nil {
		return ""
	}
	return *e.Message
}
func (e *ServiceUnavailableException) ErrorCode() string {
	if e == nil || e.ErrorCodeOverride == nil {
		return "ServiceUnavailableException"
	}
	return *e.ErrorCodeOverride
}
func (e *ServiceUnavailableException) ErrorFault() smithy.ErrorFault { return smithy.FaultServer }

// At least one of your specified cluster subnets is in an Availability Zone that
// does not support Amazon EKS. The exception output specifies the supported
// Availability Zones for your account, from which you can choose subnets for your
// cluster.
type UnsupportedAvailabilityZoneException struct {
	Message *string

	ErrorCodeOverride *string

	ClusterName   *string
	NodegroupName *string
	ValidZones    []string

	noSmithyDocumentSerde
}

func (e *UnsupportedAvailabilityZoneException) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorCode(), e.ErrorMessage())
}
func (e *UnsupportedAvailabilityZoneException) ErrorMessage() string {
	if e.Message == nil {
		return ""
	}
	return *e.Message
}
func (e *UnsupportedAvailabilityZoneException) ErrorCode() string {
	if e == nil || e.ErrorCodeOverride == nil {
		return "UnsupportedAvailabilityZoneException"
	}
	return *e.ErrorCodeOverride
}
func (e *UnsupportedAvailabilityZoneException) ErrorFault() smithy.ErrorFault {
	return smithy.FaultClient
}