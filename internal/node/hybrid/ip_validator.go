package hybrid

import (
	"github.com/aws/eks-hybrid/internal/network"
)

const (
	nodeIPFlag           = "node-ip"
	hostnameOverrideFlag = "hostname-override"
)

func (hnp *HybridNodeProvider) ValidateNodeIP() error {
	if hnp.cluster == nil {
		hnp.Logger().Info("Node IP validation skipped")
		return nil
	} else {
		hnp.logger.Info("Validating Node IP...")

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
			return err
		}

		cluster := hnp.cluster
		if err := network.ValidateClusterRemoteNetworkConfig(cluster); err != nil {
			return err
		}

		if err = network.ValidateIPInRemoteNodeNetwork(nodeIp, cluster.RemoteNetworkConfig.RemoteNodeNetworks); err != nil {
			return err
		}
	}

	return nil
}
