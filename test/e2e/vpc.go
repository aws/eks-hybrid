package e2e

import (
	"context"
	"fmt"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2v2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// vpcConfig holds information about the VPC and its subnets
type vpcConfig struct {
	vpcID     string
	subnetIDs []string
}

// vpcSubnetParams holds information about the VPC params
type vpcSubnetParams struct {
	clusterName       string
	vpcName           string
	vpcCidr           string
	publicSubnetCidr  string
	privateSubnetCidr string
}

// createVPCResources creates a VPC and the associated subnets (public and private), route tables, internet gateway.
func (t *TestRunner) createVPCResources(client *ec2.EC2, vpcSubnetParams vpcSubnetParams) (*vpcConfig, error) {
	vpcId, err := createVPC(client, vpcSubnetParams)
	if err != nil {
		return nil, fmt.Errorf("failed to create VPC: %v", err)
	}

	azs, err := client.DescribeAvailabilityZones(&ec2.DescribeAvailabilityZonesInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to describe AZs: %v", err)
	}

	if len(azs.AvailabilityZones) < 2 {
		return nil, fmt.Errorf("failed to retrieve 2 or more AZs: %v", err)
	}

	publicSubnetId, err := createSubnet(client, vpcId, vpcSubnetParams.publicSubnetCidr, *azs.AvailabilityZones[0].ZoneName, vpcSubnetParams.vpcName+"-public-subnet", vpcSubnetParams.clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to create public subnet: %v", err)
	}
	fmt.Printf("Successfully created public subnet: %s\n", publicSubnetId)

	if err = enableAutoAssignIpv4Subnet(client, publicSubnetId); err != nil {
		return nil, fmt.Errorf("failed to enable auto-assign IPv4 for subnet %s: %v", publicSubnetId, err)
	}

	routeTableId, err := createRouteTable(client, vpcId)
	if err != nil {
		return nil, fmt.Errorf("failed to create route table: %v", err)
	}

	igwId, err := createInternetGateway(client, vpcId, vpcSubnetParams.clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to create Internet Gateway: %v", err)
	}

	// Create a route for 0.0.0.0/0 to the Internet Gateway in the Route Table.
	// Allows the hybrid nodes to reach the Public API EKS endpoint.
	if err = addGatewayRoute(client, routeTableId, "0.0.0.0/0", igwId); err != nil {
		return nil, fmt.Errorf("failed to create route to Internet Gateway: %v", err)
	}

	if err = associateRouteTableToSubnet(client, routeTableId, publicSubnetId); err != nil {
		return nil, fmt.Errorf("failed to associate route table with public subnet: %v", err)
	}

	privateSubnetId, err := createSubnet(client, vpcId, vpcSubnetParams.privateSubnetCidr, *azs.AvailabilityZones[1].ZoneName, vpcSubnetParams.vpcName+"-private-subnet", vpcSubnetParams.clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to create private subnet: %v", err)
	}
	fmt.Printf("Successfully created private subnet: %s\n", privateSubnetId)

	vpcConfig := &vpcConfig{
		vpcID:     vpcId,
		subnetIDs: []string{publicSubnetId, privateSubnetId},
	}

	return vpcConfig, nil
}

func createVPC(client *ec2.EC2, vpcParam vpcSubnetParams) (string, error) {
	vpcs, err := client.DescribeVpcs(&ec2.DescribeVpcsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("cidr"),
				Values: []*string{aws.String(vpcParam.vpcCidr)},
			},
			{
				Name:   aws.String("tag:Name"),
				Values: []*string{aws.String(vpcParam.vpcName)},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("describing VPCs: %w", err)
	}

	if len(vpcs.Vpcs) > 1 {
		return "", fmt.Errorf("found multiple VPCs with name %s and CIDR %s", vpcParam.vpcName, vpcParam.vpcCidr)
	}

	if len(vpcs.Vpcs) == 1 {
		return *vpcs.Vpcs[0].VpcId, nil
	}

	vpcOutput, err := client.CreateVpc(&ec2.CreateVpcInput{
		CidrBlock: aws.String(vpcParam.vpcCidr),

		TagSpecifications: []*ec2.TagSpecification{
			{
				ResourceType: aws.String("vpc"),
				Tags: []*ec2.Tag{
					{
						Key:   aws.String("Name"),
						Value: aws.String(vpcParam.vpcName),
					},
					{
						Key:   aws.String(TestClusterTagKey),
						Value: aws.String(vpcParam.clusterName),
					},
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to create VPC: %v", err)
	}

	vpcId := *vpcOutput.Vpc.VpcId

	fmt.Printf("Successfully created VPC: %s\n", vpcId)
	return vpcId, nil
}

func createSubnet(client *ec2.EC2, vpcID, subnetCidr, az, tagName string, clusterName string) (subnetID string, err error) {
	subnet, err := client.CreateSubnet(&ec2.CreateSubnetInput{
		VpcId:            aws.String(vpcID),
		CidrBlock:        aws.String(subnetCidr),
		AvailabilityZone: aws.String(az),
	})
	if err != nil {
		return "", fmt.Errorf("failed to create subnet: %v", err)
	}

	subnetId := *subnet.Subnet.SubnetId

	_, err = client.CreateTags(&ec2.CreateTagsInput{
		Resources: []*string{&subnetId},
		Tags: []*ec2.Tag{
			{
				Key:   aws.String("Name"),
				Value: aws.String(tagName),
			},
			{
				Key:   aws.String(TestClusterTagKey),
				Value: aws.String(clusterName),
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to tag private subnet: %v", err)
	}
	return subnetId, nil
}

func createInternetGateway(client *ec2.EC2, vpcId string, clusterName string) (string, error) {
	igwOutput, err := client.CreateInternetGateway(&ec2.CreateInternetGatewayInput{
		TagSpecifications: []*ec2.TagSpecification{
			{
				ResourceType: aws.String("internet-gateway"),
				Tags: []*ec2.Tag{
					{
						Key:   aws.String(TestClusterTagKey),
						Value: aws.String(clusterName),
					},
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to create Internet Gateway: %v", err)
	}

	igwId := *igwOutput.InternetGateway.InternetGatewayId
	fmt.Printf("Successfully created Internet Gateway: %s\n", igwId)

	_, err = client.AttachInternetGateway(&ec2.AttachInternetGatewayInput{
		InternetGatewayId: aws.String(igwId),
		VpcId:             aws.String(vpcId),
	})
	if err != nil {
		return "", fmt.Errorf("failed to attach Internet Gateway to VPC: %v", err)
	}
	return igwId, nil
}

func enableAutoAssignIpv4Subnet(client *ec2.EC2, subnetID string) error {
	_, err := client.ModifySubnetAttribute(&ec2.ModifySubnetAttributeInput{
		SubnetId: aws.String(subnetID),
		MapPublicIpOnLaunch: &ec2.AttributeBooleanValue{
			Value: aws.Bool(true),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to enable auto-assign IPv4 for subnet %s: %v", subnetID, err)
	}
	return nil
}

func createRouteTable(client *ec2.EC2, vpcId string) (string, error) {
	routeTableOutput, err := client.CreateRouteTable(&ec2.CreateRouteTableInput{
		VpcId: aws.String(vpcId),
	})
	if err != nil {
		return "", fmt.Errorf("failed to create route table: %v", err)
	}

	routeTableId := *routeTableOutput.RouteTable.RouteTableId
	fmt.Printf("Successfully created route table: %s\n", routeTableId)
	return routeTableId, nil
}

func addGatewayRoute(client *ec2.EC2, routeTableId, route, igwId string) error {
	_, err := client.CreateRoute(&ec2.CreateRouteInput{
		RouteTableId:         aws.String(routeTableId),
		DestinationCidrBlock: aws.String(route),
		GatewayId:            aws.String(igwId),
	})
	if err != nil {
		return fmt.Errorf("failed to create route to Internet Gateway: %v", err)
	}
	return nil
}

func associateRouteTableToSubnet(client *ec2.EC2, routeTableId, subnetId string) error {
	_, err := client.AssociateRouteTable(&ec2.AssociateRouteTableInput{
		RouteTableId: aws.String(routeTableId),
		SubnetId:     aws.String(subnetId),
	})
	if err != nil {
		return fmt.Errorf("failed to associate route table with subnet: %v", err)
	}
	return nil
}

func getAttachedDefaultSecurityGroup(ctx context.Context, client *ec2.EC2, vpcId string) (string, error) {
	input := &ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []*string{aws.String(vpcId)},
			},
			{
				Name:   aws.String("group-name"),
				Values: []*string{aws.String("default")},
			},
		},
	}

	result, err := client.DescribeSecurityGroupsWithContext(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to describe security groups: %v", err)
	}

	if len(result.SecurityGroups) == 0 {
		return "", fmt.Errorf("no default security group found for VPC %s", vpcId)
	}

	return *result.SecurityGroups[0].GroupId, nil
}

func addIngressRules(ctx context.Context, client *ec2.EC2, securityGroupID string, permission []*ec2.IpPermission) error {
	_, err := client.AuthorizeSecurityGroupIngressWithContext(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId:       aws.String(securityGroupID),
		IpPermissions: permission,
	})
	if err != nil {
		return fmt.Errorf("failed to update security group: %v", err)
	}
	return nil
}

// CreateVPCPeering creates a VPC peering connection between two VPCs
func (t *TestRunner) createVPCPeering(ctx context.Context) (string, error) {
	svc := ec2v2.NewFromConfig(t.Config.(awsv2.Config))

	// Create VPC peering connection
	result, err := svc.CreateVpcPeeringConnection(ctx, &ec2v2.CreateVpcPeeringConnectionInput{
		VpcId:     aws.String(t.Status.ClusterVpcID),
		PeerVpcId: aws.String(t.Status.HybridVpcID),
		TagSpecifications: []ec2v2Types.TagSpecification{
			{
				ResourceType: "vpc-peering-connection",
				Tags: []ec2v2Types.Tag{
					{
						Key:   aws.String(TestClusterTagKey),
						Value: aws.String(t.Spec.ClusterName),
					},
				},
			},
		},
	}, func(o *ec2v2.Options) {
		o.RetryMaxAttempts = 20
		o.RetryMode = awsv2.RetryModeAdaptive
	})

	if err != nil {
		return "", fmt.Errorf("failed to create VPC peering connection: %v", err)
	}

	peeringConnectionID := *result.VpcPeeringConnection.VpcPeeringConnectionId

	fmt.Printf("VPC Peering Connection created: %s\n", peeringConnectionID)

	// Accept the VPC peering request
	_, err = svc.AcceptVpcPeeringConnection(ctx, &ec2v2.AcceptVpcPeeringConnectionInput{
		VpcPeeringConnectionId: aws.String(peeringConnectionID),
	}, func(o *ec2v2.Options) {
		o.Retryer = retry.AddWithErrorCodes(retry.NewStandard(), "InvalidVpcPeeringConnectionID.NotFound")
		o.RetryMaxAttempts = 20
		o.RetryMode = awsv2.RetryModeAdaptive
	})
	if err != nil {
		return "", fmt.Errorf("failed to accept VPC peering connection: %v", err)
	}

	fmt.Printf("VPC Peering Connection accepted: %s\n", peeringConnectionID)
	return peeringConnectionID, nil
}

// UpdateRouteTablesForPeering updates the route tables to allow traffic between peered VPCs
func (t *TestRunner) updateRouteTablesForPeering() error {
	svc := ec2.New(t.Session)

	// Get the route tables for both VPCs
	routeTables1, err := svc.DescribeRouteTables(&ec2.DescribeRouteTablesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []*string{aws.String(t.Status.ClusterVpcID)},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to describe route tables for VPC %s: %v", t.Status.ClusterVpcID, err)
	}

	routeTables2, err := svc.DescribeRouteTables(&ec2.DescribeRouteTablesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []*string{aws.String(t.Status.HybridVpcID)},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to describe route tables for VPC %s: %v", t.Status.HybridVpcID, err)
	}

	// Add routes to the route tables for both VPCs
	for _, rt := range routeTables1.RouteTables {
		_, err = svc.CreateRoute(&ec2.CreateRouteInput{
			RouteTableId:           rt.RouteTableId,
			DestinationCidrBlock:   aws.String(t.Spec.HybridNetwork.VpcCidr),
			VpcPeeringConnectionId: aws.String(t.Status.PeeringConnID),
		})
		if err != nil {
			return fmt.Errorf("failed to create route in VPC %s: %v", t.Status.ClusterVpcID, err)
		}
	}

	for _, rt := range routeTables2.RouteTables {
		_, err = svc.CreateRoute(&ec2.CreateRouteInput{
			RouteTableId:           rt.RouteTableId,
			DestinationCidrBlock:   aws.String(t.Spec.ClusterNetwork.VpcCidr),
			VpcPeeringConnectionId: aws.String(t.Status.PeeringConnID),
		})
		if err != nil {
			return fmt.Errorf("failed to create route in VPC %s: %v", t.Status.HybridVpcID, err)
		}
	}

	fmt.Println("Routes updated for VPC peering connection")
	return nil
}

// deleteVpcPeering deletes the VPC peering connections.
func (t *TestRunner) deleteVpcPeering() error {
	fmt.Println("Deleting VPC peering connection...")

	svc := ec2.New(t.Session)
	input := &ec2.DeleteVpcPeeringConnectionInput{
		VpcPeeringConnectionId: aws.String(t.Status.PeeringConnID),
	}

	_, err := svc.DeleteVpcPeeringConnection(input)
	if err != nil {
		return fmt.Errorf("failed to delete VPC peering connection: %v", err)
	}

	fmt.Println("Successfully deleted VPC peering connection")
	return nil
}

// deleteVpcs deletes the VPC and their associated subnets.
func (t *TestRunner) deleteVpc(vpc vpcConfig) error {
	fmt.Printf("Deleting VPC and attached resources %s...\n", vpc.vpcID)

	svc := ec2.New(t.Session)

	if err := t.deleteInternetGateway(svc, vpc.vpcID); err != nil {
		return fmt.Errorf("failed to delete internet gateway: %v", err)
	}

	for _, subnetID := range vpc.subnetIDs {
		err := t.deleteSubnet(subnetID)
		if err != nil {
			return fmt.Errorf("failed to delete subnet %s: %v", subnetID, err)
		}
	}

	if err := t.deleteRouteTables(svc, vpc.vpcID); err != nil {
		return fmt.Errorf("failed to delete route tables: %v", err)
	}

	input := &ec2.DeleteVpcInput{
		VpcId: aws.String(vpc.vpcID),
	}

	_, err := svc.DeleteVpc(input)
	if err != nil && isErrCode(err, "InvalidVpcID.NotFound") {
		fmt.Printf("VPC %s already deleted\n", vpc.vpcID)
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to delete VPC: %v", err)
	}

	fmt.Printf("Successfully deleted VPC %s\n", vpc.vpcID)
	return nil
}

func (t *TestRunner) deleteSubnet(subnetID string) error {
	fmt.Printf("Deleting subnet %s...\n", subnetID)

	svc := ec2.New(t.Session)
	input := &ec2.DeleteSubnetInput{
		SubnetId: aws.String(subnetID),
	}

	_, err := svc.DeleteSubnet(input)
	if err != nil && isErrCode(err, "InvalidSubnetID.NotFound") {
		fmt.Printf("subnet %s already deleted\n", subnetID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to delete subnet: %v", err)
	}

	fmt.Printf("Successfully deleted subnet %s\n", subnetID)
	return nil
}

func (t *TestRunner) deleteInternetGateway(svc *ec2.EC2, vpcID string) error {
	igwOutput, err := svc.DescribeInternetGateways(&ec2.DescribeInternetGatewaysInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("attachment.vpc-id"),
				Values: []*string{aws.String(vpcID)},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to describe internet gateways for VPC %s: %v", vpcID, err)
	}

	if len(igwOutput.InternetGateways) > 0 {
		igwID := *igwOutput.InternetGateways[0].InternetGatewayId
		fmt.Printf("Detaching and deleting internet gateway %s\n", igwID)
		_, err := svc.DetachInternetGateway(&ec2.DetachInternetGatewayInput{
			InternetGatewayId: aws.String(igwID),
			VpcId:             aws.String(vpcID),
		})
		if err != nil {
			return fmt.Errorf("failed to detach internet gateway %s from VPC %s: %v", igwID, vpcID, err)
		}

		_, err = svc.DeleteInternetGateway(&ec2.DeleteInternetGatewayInput{
			InternetGatewayId: aws.String(igwID),
		})
		if err != nil {
			return fmt.Errorf("failed to delete internet gateway %s: %v", igwID, err)
		}
		fmt.Printf("Successfully deleted Internet Gateway %s\n", igwID)
	}
	return nil
}

func (t *TestRunner) deleteRouteTables(svc *ec2.EC2, vpcID string) error {
	input := &ec2.DescribeRouteTablesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []*string{aws.String(vpcID)},
			},
		},
	}

	result, err := svc.DescribeRouteTables(input)
	if err != nil {
		return fmt.Errorf("error describing route tables: %v", err)
	}

	for _, rt := range result.RouteTables {
		// Skip the main route table as it cannot be deleted while the VPC is still exists. It gets deleted with the VPC.
		if len(rt.Associations) > 0 && *rt.Associations[0].Main {
			continue
		}

		_, err = svc.DeleteRouteTable(&ec2.DeleteRouteTableInput{
			RouteTableId: rt.RouteTableId,
		})
		if err != nil {
			return fmt.Errorf("error deleting route table: %v", err)
		}
	}
	fmt.Println("Successfully deleted route tables")
	return nil
}
