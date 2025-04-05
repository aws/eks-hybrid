package peered

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

type HybridCluster struct {
	Name              string
	Arn               string
	Region            string
	KubernetesVersion string
	SubnetID          string
	SecurityGroupID   string
}

// GetHybridCluster returns the hybrid cluster details.
func GetHybridCluster(ctx context.Context, eksClient *eks.Client, ec2Client *ec2.Client, clusterName string) (*HybridCluster, error) {
	cluster := &HybridCluster{
		Name:   clusterName,
		Region: eksClient.Options().Region,
	}

	clusterDetails, err := getClusterDetails(ctx, eksClient, clusterName)
	if err != nil {
		return nil, fmt.Errorf("getting cluster kubernetes version: %w", err)
	}

	cluster.KubernetesVersion = *clusterDetails.Version
	cluster.Arn = *clusterDetails.Arn

	hybridVpcID, err := findHybridVPC(ctx, ec2Client, *clusterDetails.ResourcesVpcConfig.VpcId)
	if err != nil {
		return nil, fmt.Errorf("getting peered VPC for the given cluster %s: %w", clusterName, err)
	}

	cluster.SubnetID, err = findSubnetInVPC(ctx, ec2Client, hybridVpcID)
	if err != nil {
		return nil, fmt.Errorf("getting public subnet in the given hybrid node vpc %s: %w", hybridVpcID, err)
	}

	cluster.SecurityGroupID, err = getDefaultSecurityGroup(ctx, ec2Client, hybridVpcID)
	if err != nil {
		return nil, fmt.Errorf("getting default security group in the given hybrid node vpc %s: %w", hybridVpcID, err)
	}

	return cluster, nil
}

func getClusterDetails(ctx context.Context, client *eks.Client, clusterName string) (ekstypes.Cluster, error) {
	input := &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	}
	result, err := client.DescribeCluster(ctx, input)
	if err != nil {
		return ekstypes.Cluster{}, fmt.Errorf("getting cluster details: %w", err)
	}

	return *result.Cluster, nil
}

func findHybridVPC(ctx context.Context, client *ec2.Client, clusterVpcID string) (vpcID string, err error) {
	attachments, err := client.DescribeTransitGatewayAttachments(ctx, &ec2.DescribeTransitGatewayAttachmentsInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("resource-id"),
				Values: []string{clusterVpcID},
			},
			{
				Name:   aws.String("resource-type"),
				Values: []string{"vpc"},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to describe TGW attachments for VPC %s: %w", clusterVpcID, err)
	}

	if len(attachments.TransitGatewayAttachments) == 0 {
		return "", fmt.Errorf("no TGW attachments found for VPC %s", clusterVpcID)
	}

	if len(attachments.TransitGatewayAttachments) > 1 {
		return "", fmt.Errorf("more than one TGW attachment found for VPC %s", clusterVpcID)
	}

	tgwID := *attachments.TransitGatewayAttachments[0].TransitGatewayId

	attachments, err = client.DescribeTransitGatewayAttachments(ctx, &ec2.DescribeTransitGatewayAttachmentsInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("transit-gateway-id"),
				Values: []string{tgwID},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to describe TGW attachments: %w", err)
	}

	if len(attachments.TransitGatewayAttachments) != 2 {
		return "", fmt.Errorf("expected 2 TGW attachments for TGW %s, got %d", tgwID, len(attachments.TransitGatewayAttachments))
	}

	for _, attachment := range attachments.TransitGatewayAttachments {
		if attachment.ResourceType == "vpc" && *attachment.ResourceId != clusterVpcID {
			return *attachment.ResourceId, nil
		}
	}

	return "", fmt.Errorf("no peered VPC found for cluster VPC %s", clusterVpcID)
}

func findSubnetInVPC(ctx context.Context, client *ec2.Client, vpcID string) (subnetID string, err error) {
	input := &ec2.DescribeSubnetsInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{vpcID},
			},
			{
				Name:   aws.String("map-public-ip-on-launch"),
				Values: []string{"true"},
			},
		},
	}
	result, err := client.DescribeSubnets(ctx, input)
	if err != nil {
		return "", err
	}

	if len(result.Subnets) == 0 {
		return "", fmt.Errorf("no subnets found in VPC %s", vpcID)
	}
	return *result.Subnets[0].SubnetId, nil
}

func getDefaultSecurityGroup(ctx context.Context, client *ec2.Client, vpcID string) (string, error) {
	input := &ec2.DescribeSecurityGroupsInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{vpcID},
			},
		},
	}

	result, err := client.DescribeSecurityGroups(ctx, input)
	if err != nil {
		return "", err
	}

	if len(result.SecurityGroups) == 0 {
		return "", fmt.Errorf("no default security group found for VPC %s", vpcID)
	}

	return *result.SecurityGroups[0].GroupId, nil
}
