package hybrid

import (
	"context"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/network"
	"github.com/aws/eks-hybrid/internal/validation"
)

const (
	nodeIPFlag           = "node-ip"
	hostnameOverrideFlag = "hostname-override"
)

func (hnp *HybridNodeProvider) ValidateNodeIP(ctx context.Context, informer validation.Informer, nodeConfig *api.NodeConfig) error {
	if hnp.cluster == nil {
		informer.Starting(ctx, nodeIpValidation, "Skipping validating Node IP")
		informer.Done(ctx, nodeIpValidation, nil)
		return nil
	}

	informer.Starting(ctx, nodeIpValidation, "Validating Node IP configuration")
	var err error
	defer func() {
		informer.Done(ctx, nodeIpValidation, err)
	}()

	// Only check flags set by user in the config file to help determine IP:
	// - node-ip and hostname-override are only available as flags and cannot be set via spec.kubelet.config
	// - Hybrid nodes does not set --node-ip
	// - Hybrid nodes sets --hostname-override to either the IAM-RA Node name or the SSM instance ID, which is checked separately for DNS
	kubeletArgs := hnp.nodeConfig.Spec.Kubelet.Flags
	var iamNodeName string
	if hnp.nodeConfig.IsIAMRolesAnywhere() {
		iamNodeName = hnp.nodeConfig.Status.Hybrid.NodeName
	}
	nodeIp, err := network.GetNodeIP(kubeletArgs, iamNodeName, hnp.network)
	if err != nil {
		err = validation.WithRemediation(err,
			"Ensure the node has a valid network interface configuration. "+
				"Check that the node can resolve its hostname or has a valid --node-ip flag set. "+
				"See https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-troubleshooting.html")
		return err
	}

	cluster := hnp.cluster
	if err = network.ValidateClusterRemoteNetworkConfig(cluster); err != nil {
		err = validation.WithRemediation(err,
			"Ensure the EKS cluster has remote network configuration set up properly. "+
				"The cluster must have remote node networks configured to validate hybrid node connectivity.")
		return err
	}

	if err = network.ValidateIPInRemoteNodeNetwork(nodeIp, cluster.RemoteNetworkConfig.RemoteNodeNetworks); err != nil {
		err = validation.WithRemediation(err,
			"Ensure the node IP is within the configured remote network CIDR blocks. "+
				"Update the remote network configuration in the EKS cluster or adjust the node's network configuration. "+
				"See https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-troubleshooting.html")
		return err
	}

	return nil
}
