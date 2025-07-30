package network

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/aws/eks"
	"github.com/aws/eks-hybrid/internal/validation"
)

type NetworkInterfaceValidator struct {
	awsConfig aws.Config
	network   Network
}

func NewNetworkInterfaceValidator(awsConfig aws.Config, opts ...func(*NetworkInterfaceValidator)) NetworkInterfaceValidator {
	v := &NetworkInterfaceValidator{
		awsConfig: awsConfig,
		network:   NewDefaultNetwork(),
	}
	for _, opt := range opts {
		opt(v)
	}
	return *v
}

func WithNetwork(network Network) func(*NetworkInterfaceValidator) {
	return func(v *NetworkInterfaceValidator) {
		v.network = network
	}
}

func (v NetworkInterfaceValidator) Run(ctx context.Context, informer validation.Informer, node *api.NodeConfig) error {
	cluster, err := eks.ReadCluster(ctx, v.awsConfig, node)
	if err != nil {
		// Only if reading the EKS fail is when we "start" a validation and signal it as failed.
		// Otherwise, there is no need to surface we are reading from the EKS API.
		informer.Starting(ctx, "kubernetes-endpoint-access", "Validating access to Kubernetes API endpoint")
		informer.Done(ctx, "kubernetes-endpoint-access", err)
		return err
	}

	name := "network-interface-validation"
	informer.Starting(ctx, name, "Validating hybrid node network interface")
	defer func() {
		informer.Done(ctx, name, err)
	}()

	if err = ValidateClusterRemoteNetworkConfig(cluster); err != nil {
		err = validation.WithRemediation(err,
			"Ensure the EKS cluster has remote network configuration set up properly. "+
				"The cluster must have remote node networks configured to validate hybrid node connectivity.")
		return err
	}

	// Get kubelet arguments from the node configuration
	kubeletArgs := node.Spec.Kubelet.Flags
	var iamNodeName string
	if node.IsIAMRolesAnywhere() {
		iamNodeName = node.Status.Hybrid.NodeName
	}

	// Get the node IP using the shared utility function
	nodeIP, err := GetNodeIP(kubeletArgs, iamNodeName, v.network)
	if err != nil {
		err = validation.WithRemediation(err,
			"Ensure the node has a valid network interface configuration. "+
				"Check that the node can resolve its hostname or has a valid --node-ip flag set. "+
				"See https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-troubleshooting.html")
		return err
	}

	// Validate that the node IP is in the remote node networks using shared utility function
	if err = ValidateIPInRemoteNodeNetwork(nodeIP, cluster.RemoteNetworkConfig.RemoteNodeNetworks); err != nil {
		err = validation.WithRemediation(err,
			"Ensure the node IP is within the configured remote network CIDR blocks. "+
				"Update the remote network configuration in the EKS cluster or adjust the node's network configuration. "+
				"See https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-troubleshooting.html")
		return err
	}

	// Validate MTU for the network interface associated with the node IP
	if err = ValidateNetworkInterfaceMTUForIP(nodeIP); err != nil {
		err = validation.WithRemediation(err,
			"Ensure the network interface with the node IP has a valid MTU value. "+
				"MTU should be <= 1500 (standard Ethernet) or between 8000-9001 (jumbo frames). "+
				"Update the network interface configuration to use acceptable MTU values. "+
				"See https://docs.aws.amazon.com/vpc/latest/tgw/transit-gateway-quotas.html#mtu-quotas")
		return err
	}

	return nil
}
