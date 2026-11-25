package cleanup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/errors"
)

const privateInstallDeleteTimeout = 10 * time.Minute

func privateInstallFilters(input FilterInput) []types.Filter {
	return append(ec2Filters(input), types.Filter{
		Name:   aws.String("tag-key"),
		Values: []string{constants.PrivateInstallTagKey},
	})
}

// ListPrivateInstallVPCEndpoints lists the VPC endpoints created by the private install suite.
func (v *VPCCleaner) ListPrivateInstallVPCEndpoints(ctx context.Context, input FilterInput) ([]string, error) {
	paginator := ec2.NewDescribeVpcEndpointsPaginator(v.ec2Client, &ec2.DescribeVpcEndpointsInput{
		Filters: privateInstallFilters(input),
	})

	var endpointIDs []string
	for paginator.HasMorePages() {
		resp, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describing VPC endpoints: %w", err)
		}
		for _, endpoint := range resp.VpcEndpoints {
			if strings.EqualFold(string(endpoint.State), "deleted") {
				continue
			}
			resource := ResourceWithTags{
				ID:           aws.ToString(endpoint.VpcEndpointId),
				Tags:         convertEC2Tags(endpoint.Tags),
				CreationTime: aws.ToTime(endpoint.CreationTimestamp),
			}
			if shouldDeleteResource(resource, input) {
				endpointIDs = append(endpointIDs, aws.ToString(endpoint.VpcEndpointId))
			}
		}
	}
	return endpointIDs, nil
}

// ListPrivateInstallSecurityGroups lists the security groups created by the private install suite.
func (v *VPCCleaner) ListPrivateInstallSecurityGroups(ctx context.Context, input FilterInput) ([]string, error) {
	paginator := ec2.NewDescribeSecurityGroupsPaginator(v.ec2Client, &ec2.DescribeSecurityGroupsInput{
		Filters: privateInstallFilters(input),
	})

	var securityGroupIDs []string
	for paginator.HasMorePages() {
		resp, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describing security groups: %w", err)
		}
		for _, sg := range resp.SecurityGroups {
			creationTime, err := creationTimeFromTags(sg.Tags)
			if err != nil {
				return nil, fmt.Errorf("getting creation time from tags: %w", err)
			}
			resource := ResourceWithTags{
				ID:           aws.ToString(sg.GroupId),
				Tags:         convertEC2Tags(sg.Tags),
				CreationTime: creationTime,
			}
			if shouldDeleteResource(resource, input) {
				securityGroupIDs = append(securityGroupIDs, aws.ToString(sg.GroupId))
			}
		}
	}
	return securityGroupIDs, nil
}

// DeleteVPCEndpointsAndWait deletes the endpoints and waits until they are gone, so their
// network interfaces don't block deleting security groups or the VPC.
func (v *VPCCleaner) DeleteVPCEndpointsAndWait(ctx context.Context, endpointIDs []string) error {
	if len(endpointIDs) == 0 {
		return nil
	}
	if _, err := v.ec2Client.DeleteVpcEndpoints(ctx, &ec2.DeleteVpcEndpointsInput{VpcEndpointIds: endpointIDs}); err != nil {
		return fmt.Errorf("deleting VPC endpoints %v: %w", endpointIDs, err)
	}

	ctx, cancel := context.WithTimeout(ctx, privateInstallDeleteTimeout)
	defer cancel()
	for {
		resp, err := v.ec2Client.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{
			Filters: []types.Filter{{Name: aws.String("vpc-endpoint-id"), Values: endpointIDs}},
		})
		if err != nil {
			return fmt.Errorf("describing VPC endpoints: %w", err)
		}
		remaining := 0
		for _, endpoint := range resp.VpcEndpoints {
			if !strings.EqualFold(string(endpoint.State), "deleted") {
				remaining++
			}
		}
		if remaining == 0 {
			v.logger.Info("Deleted VPC endpoints", "vpcEndpointIDs", endpointIDs)
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for VPC endpoints to be deleted: %w", ctx.Err())
		case <-time.After(10 * time.Second):
		}
	}
}

// DeleteSecurityGroupWithRetry deletes the security group, retrying while network interfaces
// from deleted endpoints or instances are still being released.
func (v *VPCCleaner) DeleteSecurityGroupWithRetry(ctx context.Context, securityGroupID string) error {
	ctx, cancel := context.WithTimeout(ctx, privateInstallDeleteTimeout)
	defer cancel()
	for {
		_, err := v.ec2Client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{GroupId: aws.String(securityGroupID)})
		if err == nil || errors.IsAwsError(err, "InvalidGroup.NotFound") || errors.IsAwsError(err, "InvalidSecurityGroupId.NotFound") {
			v.logger.Info("Deleted security group", "securityGroupID", securityGroupID)
			return nil
		}
		if !errors.IsAwsError(err, "DependencyViolation") {
			return fmt.Errorf("deleting security group %s: %w", securityGroupID, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("deleting security group %s: %w", securityGroupID, err)
		case <-time.After(10 * time.Second):
		}
	}
}
