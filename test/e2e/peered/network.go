package peered

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2sdk "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	clientgo "k8s.io/client-go/kubernetes"

	"github.com/aws/eks-hybrid/test/e2e/cni"
	"github.com/aws/eks-hybrid/test/e2e/ec2"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

type Network struct {
	EC2    *ec2sdk.Client
	Logger logr.Logger
	K8s    K8s

	Cluster *HybridCluster
}

// CreateRoutesForNode creates routes in the VPC route table for the node's pod CIDRs.
func (n *Network) CreateRoutesForNode(ctx context.Context, peerdNode *PeerdNode) error {
	node, err := kubernetes.CheckForNodeWithE2ELabel(ctx, n.K8s, peerdNode.Name)
	if err != nil {
		return fmt.Errorf("reading node: %w", err)
	}

	podCIDRs, err := cni.NodePodCIDRs(ctx, n.K8s, node)
	if err != nil {
		return fmt.Errorf("getting node pod CIDRs: %w", err)
	}

	if err := n.addRoutesForCIDRs(ctx, peerdNode.Instance, podCIDRs); err != nil {
		return fmt.Errorf("adding routes for node pod CIDRs: %w", err)
	}

	return nil
}

func (n *Network) addRoutesForCIDRs(ctx context.Context, instance ec2.Instance, cidrs []string) error {
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

	for _, cidr := range cidrs {
		n.Logger.Info("Adding route for CIDR", "cidr", cidr, "instanceID", instance.ID)
		err := ec2.CreateRouteForCIDRToInstance(ctx, n.EC2, *resp.RouteTables[0].RouteTableId, cidr, instance.ID)
		if err != nil {
			return fmt.Errorf("adding route for node pod CIDR %s: %w", cidr, err)
		}
	}

	return nil
}

var (
	_ clientgo.Interface = K8s{}
	_ dynamic.Interface  = K8s{}
)

type K8s struct {
	clientgo.Interface
	Dynamic dynamic.Interface
}

func (k K8s) Resource(resource schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return k.Dynamic.Resource(resource)
}
