package peered

import (
	"context"
	"fmt"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2sdk "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/cni"
	"github.com/aws/eks-hybrid/test/e2e/ec2"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
	peeredtypes "github.com/aws/eks-hybrid/test/e2e/peered/types"
)

type Network struct {
	EC2    *ec2sdk.Client
	Logger logr.Logger
	K8s    peeredtypes.K8s

	Cluster *HybridCluster
}

// CreateRoutesForNode creates routes in the VPC route table for the node's pod CIDRs.
func (n *Network) CreateRoutesForNode(ctx context.Context, peeredInstance *PeeredInstance) error {
	if err := ec2.DisableSourceDestCheck(ctx, n.EC2, peeredInstance.ID); err != nil {
		return fmt.Errorf("disabling source/dest check: %w", err)
	}

	node, err := kubernetes.CheckForNodeWithE2ELabel(ctx, n.K8s, peeredInstance.Name)
	if err != nil {
		return fmt.Errorf("reading node: %w", err)
	}

	podCIDRs, err := cni.NodePodCIDRs(ctx, n.K8s, node)
	if err != nil {
		return fmt.Errorf("getting node pod CIDRs: %w", err)
	}

	if err := n.addRoutesForCIDRs(ctx, peeredInstance, podCIDRs); err != nil {
		return fmt.Errorf("adding routes for node pod CIDRs: %w", err)
	}

	return nil
}

func (n *Network) addRoutesForCIDRs(ctx context.Context, instance *PeeredInstance, cidrs []string) error {
	resp, err := n.EC2.DescribeRouteTables(ctx, &ec2sdk.DescribeRouteTablesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("association.subnet-id"),
				Values: []string{n.Cluster.SubnetID},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("describing route tables: %w", err)
	}

	if len(resp.RouteTables) != 1 {
		return fmt.Errorf("expected to find one route table for subnet %s, found %d", n.Cluster.SubnetID, len(resp.RouteTables))
	}

	var routeTableCIDRs []string
	routeTable := resp.RouteTables[0]
	for _, route := range routeTable.Routes {
		routeTableCIDRs = append(routeTableCIDRs, *route.DestinationCidrBlock)
	}
	for _, cidr := range cidrs {
		if slices.Contains(routeTableCIDRs, cidr) {
			n.Logger.Info("Route for CIDR already exists in route table, skipping", "cidr", cidr, "routeTable", *routeTable.RouteTableId)
			continue
		}
		n.Logger.Info("Adding route for CIDR", "cidr", cidr, "instanceID", instance.ID)
		err := ec2.CreateRouteForCIDRToInstance(ctx, n.EC2, *routeTable.RouteTableId, cidr, instance.ID)
		if err != nil {
			return fmt.Errorf("adding route for node pod CIDR %s: %w", cidr, err)
		}
	}

	return nil
}
