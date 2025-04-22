package cleanup

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/errors"
)

const (
	vpcPeeringConnectionDeletedWaiterTimeout = 5 * time.Minute
)

// VPCCleaner is responsible for cleaning up AWS VPC resources
type VPCCleaner struct {
	ec2Client *ec2.Client
	logger    logr.Logger
}

// NewVPCCleaner creates a new VPC cleaner
func NewVPCCleaner(ec2Client *ec2.Client, logger logr.Logger) *VPCCleaner {
	return &VPCCleaner{
		ec2Client: ec2Client,
		logger:    logger,
	}
}

func (v *VPCCleaner) ListPeeringConnections(ctx context.Context, input FilterInput) ([]string, error) {
	paginator := ec2.NewDescribeVpcPeeringConnectionsPaginator(v.ec2Client, &ec2.DescribeVpcPeeringConnectionsInput{
		Filters: ec2Filters(input),
	})

	var peeringConnectionIDs []string
	for paginator.HasMorePages() {
		resp, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describing peering connections: %w", err)
		}

		for _, peeringConnection := range resp.VpcPeeringConnections {
			if peeringConnection.Status.Code == types.VpcPeeringConnectionStateReasonCodeDeleted ||
				peeringConnection.Status.Code == types.VpcPeeringConnectionStateReasonCodeDeleting {
				continue
			}

			creationTime, err := creationTimeFromTags(peeringConnection.Tags)
			if err != nil {
				return nil, fmt.Errorf("getting creation time from tags: %w", err)
			}
			resource := ResourceWithTags{
				ID:           aws.ToString(peeringConnection.VpcPeeringConnectionId),
				Tags:         convertEC2Tags(peeringConnection.Tags),
				CreationTime: creationTime,
			}

			if shouldDeleteResource(resource, input) {
				peeringConnectionIDs = append(peeringConnectionIDs, *peeringConnection.VpcPeeringConnectionId)
			}
		}
	}
	return peeringConnectionIDs, nil
}

func (v *VPCCleaner) DeletePeeringConnection(ctx context.Context, peeringConnectionID string) error {
	_, err := v.ec2Client.DeleteVpcPeeringConnection(ctx, &ec2.DeleteVpcPeeringConnectionInput{
		VpcPeeringConnectionId: aws.String(peeringConnectionID),
	})
	if err != nil && errors.IsAwsError(err, "InvalidVpcPeeringConnectionId.NotFound") {
		v.logger.Info("Peering connection already deleted", "peeringConnectionID", peeringConnectionID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting peering connection %s: %w", peeringConnectionID, err)
	}

	waiter := ec2.NewVpcPeeringConnectionDeletedWaiter(v.ec2Client)
	err = waiter.Wait(ctx, &ec2.DescribeVpcPeeringConnectionsInput{
		VpcPeeringConnectionIds: []string{peeringConnectionID},
	}, vpcPeeringConnectionDeletedWaiterTimeout)
	if err != nil {
		return fmt.Errorf("waiting for peering connection %s to be deleted: %w", peeringConnectionID, err)
	}

	v.logger.Info("Deleted peering connection", "peeringConnectionID", peeringConnectionID)
	return nil
}

func (v *VPCCleaner) ListVPCs(ctx context.Context, input FilterInput) ([]string, error) {
	paginator := ec2.NewDescribeVpcsPaginator(v.ec2Client, &ec2.DescribeVpcsInput{
		Filters: ec2Filters(input),
	})

	var vpcIDs []string
	for paginator.HasMorePages() {
		resp, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describing VPCs: %w", err)
		}

		for _, vpc := range resp.Vpcs {
			creationTime, err := creationTimeFromTags(vpc.Tags)
			if err != nil {
				return nil, fmt.Errorf("getting creation time from tags: %w", err)
			}
			resource := ResourceWithTags{
				ID:           aws.ToString(vpc.VpcId),
				Tags:         convertEC2Tags(vpc.Tags),
				CreationTime: creationTime,
			}

			if shouldDeleteResource(resource, input) {
				vpcIDs = append(vpcIDs, aws.ToString(vpc.VpcId))
			}
		}
	}

	return vpcIDs, nil
}

func (v *VPCCleaner) DeleteVPC(ctx context.Context, vpcID string) error {
	_, err := v.ec2Client.DeleteVpc(ctx, &ec2.DeleteVpcInput{
		VpcId: aws.String(vpcID),
	}, func(o *ec2.Options) {
		o.Retryer = retry.NewStandard(func(o *retry.StandardOptions) {
			o.MaxAttempts = 10 // ~ 2 minutes
			o.Retryables = append(o.Retryables, dependencyViolationRetryable{})
		})
	})
	if err != nil && errors.IsAwsError(err, "InvalidVpcID.NotFound") {
		v.logger.Info("VPC already deleted", "vpcID", vpcID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting VPC %s: %w", vpcID, err)
	}

	v.logger.Info("Deleted VPC", "vpcID", vpcID)
	return nil
}

func (v *VPCCleaner) ListInternetGateways(ctx context.Context, input FilterInput) ([]string, error) {
	paginator := ec2.NewDescribeInternetGatewaysPaginator(v.ec2Client, &ec2.DescribeInternetGatewaysInput{
		Filters: ec2Filters(input),
	})

	var igwIDs []string
	for paginator.HasMorePages() {
		resp, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describing internet gateways: %w", err)
		}

		for _, igw := range resp.InternetGateways {
			creationTime, err := creationTimeFromTags(igw.Tags)
			if err != nil {
				return nil, fmt.Errorf("getting creation time from tags: %w", err)
			}
			resource := ResourceWithTags{
				ID:           aws.ToString(igw.InternetGatewayId),
				Tags:         convertEC2Tags(igw.Tags),
				CreationTime: creationTime,
			}

			if shouldDeleteResource(resource, input) {
				igwIDs = append(igwIDs, *igw.InternetGatewayId)
			}
		}
	}

	return igwIDs, nil
}

func (v *VPCCleaner) DeleteInternetGateway(ctx context.Context, igwID string) error {
	// Get the gateway to find its attachments
	resp, err := v.ec2Client.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{
		InternetGatewayIds: []string{igwID},
	})
	if err != nil && errors.IsAwsError(err, "InvalidInternetGatewayID.NotFound") {
		v.logger.Info("Internet gateway already deleted", "igwID", igwID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("describing internet gateway %s: %w", igwID, err)
	}
	if len(resp.InternetGateways) == 0 {
		return nil
	}

	igw := resp.InternetGateways[0]
	// Detach from any VPCs first
	for _, attachment := range igw.Attachments {
		vpcID := aws.ToString(attachment.VpcId)
		v.logger.Info("Detaching internet gateway", "igwID", igwID, "vpcID", vpcID)

		_, err := v.ec2Client.DetachInternetGateway(ctx, &ec2.DetachInternetGatewayInput{
			InternetGatewayId: aws.String(igwID),
			VpcId:             aws.String(vpcID),
		})
		if err != nil {
			return fmt.Errorf("detaching internet gateway %s from VPC %s: %w", igwID, vpcID, err)
		}
	}

	_, err = v.ec2Client.DeleteInternetGateway(ctx, &ec2.DeleteInternetGatewayInput{
		InternetGatewayId: aws.String(igwID),
	})
	if err != nil && !errors.IsAwsError(err, "InvalidInternetGatewayID.NotFound") {
		return fmt.Errorf("deleting internet gateway %s: %w", igwID, err)
	}

	v.logger.Info("Deleted internet gateway", "igwID", igwID)
	return nil
}

func (v *VPCCleaner) ListSubnets(ctx context.Context, input FilterInput) ([]string, error) {
	paginator := ec2.NewDescribeSubnetsPaginator(v.ec2Client, &ec2.DescribeSubnetsInput{
		Filters: ec2Filters(input),
	})

	var subnetIDs []string
	for paginator.HasMorePages() {
		resp, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describing subnets: %w", err)
		}

		for _, subnet := range resp.Subnets {
			creationTime, err := creationTimeFromTags(subnet.Tags)
			if err != nil {
				return nil, fmt.Errorf("getting creation time from tags: %w", err)
			}
			resource := ResourceWithTags{
				ID:           aws.ToString(subnet.SubnetId),
				Tags:         convertEC2Tags(subnet.Tags),
				CreationTime: creationTime,
			}

			if shouldDeleteResource(resource, input) {
				subnetIDs = append(subnetIDs, *subnet.SubnetId)
			}
		}
	}

	return subnetIDs, nil
}

func (v *VPCCleaner) DeleteSubnet(ctx context.Context, subnetID string) error {
	_, err := v.ec2Client.DeleteSubnet(ctx, &ec2.DeleteSubnetInput{
		SubnetId: aws.String(subnetID),
	}, func(o *ec2.Options) {
		o.Retryer = retry.NewStandard(func(o *retry.StandardOptions) {
			o.MaxAttempts = 10 // ~ 2 minutes
			o.Retryables = append(o.Retryables, dependencyViolationRetryable{})
		})
	})
	if err != nil && (errors.IsAwsError(err, "InvalidSubnetID.NotFound") || errors.IsAwsError(err, "InvalidSubnetId.NotFound")) {
		v.logger.Info("Subnet already deleted", "subnetID", subnetID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting subnet %s: %w", subnetID, err)
	}

	v.logger.Info("Deleted subnet", "subnetID", subnetID)
	return nil
}

func (v *VPCCleaner) ListRouteTables(ctx context.Context, input FilterInput) ([]string, error) {
	paginator := ec2.NewDescribeRouteTablesPaginator(v.ec2Client, &ec2.DescribeRouteTablesInput{
		Filters: ec2Filters(input),
	})

	var routeTableIDs []string
	for paginator.HasMorePages() {
		resp, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describing route tables: %w", err)
		}

		for _, rt := range resp.RouteTables {
			creationTime, err := creationTimeFromTags(rt.Tags)
			if err != nil {
				return nil, fmt.Errorf("getting creation time from tags: %w", err)
			}
			resource := ResourceWithTags{
				ID:           aws.ToString(rt.RouteTableId),
				Tags:         convertEC2Tags(rt.Tags),
				CreationTime: creationTime,
			}

			// Skip main route tables - we'll handle them when deleting the VPC
			isMain := false
			for _, assoc := range rt.Associations {
				if aws.ToBool(assoc.Main) {
					isMain = true
					break
				}
			}

			if !isMain && shouldDeleteResource(resource, input) {
				routeTableIDs = append(routeTableIDs, *rt.RouteTableId)
			}
		}
	}

	return routeTableIDs, nil
}

func (v *VPCCleaner) DeleteRouteTable(ctx context.Context, routeTableID string) error {
	_, err := v.ec2Client.DeleteRouteTable(ctx, &ec2.DeleteRouteTableInput{
		RouteTableId: aws.String(routeTableID),
	})
	if err != nil && errors.IsAwsError(err, "InvalidRouteTableID.NotFound") {
		v.logger.Info("Route table already deleted", "routeTableID", routeTableID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting route table %s: %w", routeTableID, err)
	}

	v.logger.Info("Deleted route table", "routeTableID", routeTableID)
	return nil
}

func (v *VPCCleaner) ListSecurityGroups(ctx context.Context, input FilterInput) ([]string, error) {
	paginator := ec2.NewDescribeSecurityGroupsPaginator(v.ec2Client, &ec2.DescribeSecurityGroupsInput{
		Filters: ec2Filters(input),
	})

	var securityGroupIDs []string
	for paginator.HasMorePages() {
		resp, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describing security groups: %w", err)
		}

		for _, sg := range resp.SecurityGroups {
			if aws.ToString(sg.GroupName) == "default" {
				continue
			}

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
				securityGroupIDs = append(securityGroupIDs, *sg.GroupId)
			}
		}
	}

	return securityGroupIDs, nil
}

func (v *VPCCleaner) DeleteSecurityGroup(ctx context.Context, securityGroupID string) error {
	_, err := v.ec2Client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
		GroupId: aws.String(securityGroupID),
	})
	if err != nil && errors.IsAwsError(err, "InvalidSecurityGroupId.NotFound") {
		v.logger.Info("Security group already deleted", "securityGroupID", securityGroupID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting security group %s: %w", securityGroupID, err)
	}

	v.logger.Info("Deleted security group", "securityGroupID", securityGroupID)
	return nil
}

func (v *VPCCleaner) ListNetworkInterfaces(ctx context.Context, vpcID string) ([]string, error) {
	paginator := ec2.NewDescribeNetworkInterfacesPaginator(v.ec2Client, &ec2.DescribeNetworkInterfacesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{vpcID},
			},
		},
	})

	var networkInterfaceIDs []string
	for paginator.HasMorePages() {
		resp, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describing network interfaces: %w", err)
		}

		for _, eni := range resp.NetworkInterfaces {
			// Skip interfaces with attachments
			if eni.Attachment != nil && eni.Attachment.AttachmentId != nil {
				v.logger.Info("Skipping network interface with attachment",
					"networkInterfaceID", aws.ToString(eni.NetworkInterfaceId),
					"attachmentID", aws.ToString(eni.Attachment.AttachmentId))
				continue
			}

			networkInterfaceIDs = append(networkInterfaceIDs, aws.ToString(eni.NetworkInterfaceId))
		}
	}

	return networkInterfaceIDs, nil
}

func (v *VPCCleaner) DeleteNetworkInterface(ctx context.Context, networkInterfaceID string) error {
	_, err := v.ec2Client.DeleteNetworkInterface(ctx, &ec2.DeleteNetworkInterfaceInput{
		NetworkInterfaceId: aws.String(networkInterfaceID),
	})
	if err != nil && errors.IsAwsError(err, "InvalidNetworkInterfaceID.NotFound") {
		v.logger.Info("Network interface already deleted", "networkInterfaceID", networkInterfaceID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting network interface %s: %w", networkInterfaceID, err)
	}

	v.logger.Info("Deleted network interface", "networkInterfaceID", networkInterfaceID)
	return nil
}

func ec2Filters(input FilterInput) []types.Filter {
	filters := []types.Filter{
		{
			Name:   aws.String("tag-key"),
			Values: []string{constants.TestClusterTagKey},
		},
	}
	clusterNameFilter := input.ClusterName
	if input.ClusterNamePrefix != "" {
		clusterNameFilter = input.ClusterNamePrefix + "*"
	}
	if clusterNameFilter != "" {
		filters = append(filters, types.Filter{
			Name:   aws.String("tag:" + constants.TestClusterTagKey),
			Values: []string{clusterNameFilter},
		})
	}
	return filters
}

func convertEC2Tags(ec2Tags []types.Tag) []Tag {
	tags := make([]Tag, 0, len(ec2Tags))
	for _, tag := range ec2Tags {
		tags = append(tags, Tag{
			Key:   aws.ToString(tag.Key),
			Value: aws.ToString(tag.Value),
		})
	}
	return tags
}

// creationTimeFromTags parses the creation time from the tags since
// most vpc resources don't have a creation time
func creationTimeFromTags(tags []types.Tag) (time.Time, error) {
	for _, tag := range tags {
		if aws.ToString(tag.Key) == constants.CreationTimeTagKey {
			creationTime, err := time.Parse(time.RFC3339, aws.ToString(tag.Value))
			if err != nil {
				return creationTime, fmt.Errorf("parsing creation time from tag: %w", err)
			}
			return creationTime, nil
		}
	}
	// if we don't find a creation time tag, return the current time
	// if we are cleaning by cluster-name it will get deleted
	// if its the sweeper based on age of resources it will be left
	return time.Now(), nil
}

type dependencyViolationRetryable struct{}

func (c dependencyViolationRetryable) IsErrorRetryable(err error) aws.Ternary {
	if errors.IsAwsError(err, "DependencyViolation") {
		return aws.BoolTernary(true)
	}

	return aws.BoolTernary(false)
}

// ListTransitGateways lists all transit gateways
func (v *VPCCleaner) ListTransitGateways(ctx context.Context, input FilterInput) ([]string, error) {
	paginator := ec2.NewDescribeTransitGatewaysPaginator(v.ec2Client, &ec2.DescribeTransitGatewaysInput{
		Filters: ec2Filters(input),
	})

	var tgwIDs []string
	for paginator.HasMorePages() {
		resp, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describing transit gateways: %w", err)
		}

		for _, tgw := range resp.TransitGateways {
			creationTime, err := creationTimeFromTags(tgw.Tags)
			if err != nil {
				return nil, fmt.Errorf("getting creation time from tags: %w", err)
			}
			resource := ResourceWithTags{
				ID:           aws.ToString(tgw.TransitGatewayId),
				Tags:         convertEC2Tags(tgw.Tags),
				CreationTime: creationTime,
			}

			if shouldDeleteResource(resource, input) {
				tgwIDs = append(tgwIDs, *tgw.TransitGatewayId)
			}
		}
	}

	return tgwIDs, nil
}

func (v *VPCCleaner) DeleteTransitGateway(ctx context.Context, tgwID string) error {
	// verify if tgw is deleted or not
	resp, err := v.ec2Client.DescribeTransitGateways(ctx, &ec2.DescribeTransitGatewaysInput{
		TransitGatewayIds: []string{tgwID},
	})
	if err != nil && errors.IsAwsError(err, "InvalidTransitGatewayID.NotFound") {
		v.logger.Info("Transit gateway already deleted", "tgwID", tgwID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("describing transit gateway %s: %w", tgwID, err)
	}
	if len(resp.TransitGateways) == 0 {
		return nil
	}

	// find tgw attachments
	attachments, err := v.ec2Client.DescribeTransitGatewayAttachments(ctx, &ec2.DescribeTransitGatewayAttachmentsInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("transit-gateway-id"),
				Values: []string{tgwID},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("describing transit gateway attachment for transit gateway %s: %w", tgwID, err)
	}

	if len(attachments.TransitGatewayAttachments) > 0 {
		for _, attachment := range attachments.TransitGatewayAttachments {
			attachmentId := attachment.TransitGatewayAttachmentId
			_, err := v.ec2Client.DeleteTransitGatewayVpcAttachment(ctx, &ec2.DeleteTransitGatewayVpcAttachmentInput{
				TransitGatewayAttachmentId: attachmentId,
			})
			if err != nil {
				return fmt.Errorf("detaching transit gateway attachment %s for tgw %s: %w", *attachmentId, tgwID, err)
			}
		}
	}

	_, err = v.ec2Client.DeleteTransitGateway(ctx, &ec2.DeleteTransitGatewayInput{
		TransitGatewayId: aws.String(tgwID),
	}, func(o *ec2.Options) {
		o.Retryer = retry.NewStandard(func(o *retry.StandardOptions) {
			o.MaxAttempts = 10 // ~ 2 minutes
			o.Retryables = append(o.Retryables, dependencyViolationRetryable{})
		})
	})

	if err != nil && !errors.IsAwsError(err, "InvalidTransitGatewayID.NotFound") {
		return fmt.Errorf("deleting transit gateway %s: %w", tgwID, err)
	}

	v.logger.Info("Deleted transit gateway", "tgwID", tgwID)
	return nil
}
