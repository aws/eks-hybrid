package hybrid

import (
	"fmt"
	"net"

	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	apimachinerynet "k8s.io/apimachinery/pkg/util/net"
)

const (
	nodeIPFlag           = "node-ip"
	hostnameOverrideFlag = "hostname-override"
)

// Network interfaces with the host's network stack.
type Network interface {
	LookupIP(host string) ([]net.IP, error)
	ResolveBindAddress(bindAddress net.IP) (net.IP, error)
	InterfaceAddrs() ([]net.Addr, error)
}

// defaultKubeletNetwork provides the network util functions used by kubelet.
type defaultKubeletNetwork struct{}

func (u defaultKubeletNetwork) LookupIP(host string) ([]net.IP, error) {
	return net.LookupIP(host)
}

func (u defaultKubeletNetwork) ResolveBindAddress(bindAddress net.IP) (net.IP, error) {
	return apimachinerynet.ResolveBindAddress(bindAddress)
}

func (u defaultKubeletNetwork) InterfaceAddrs() ([]net.Addr, error) {
	return net.InterfaceAddrs()
}

func containsIP(cidr string, ip net.IP) (bool, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false, err
	}

	return ipnet.Contains(ip), nil
}

func isIPInCIDRs(ip net.IP, cidrs []string) (bool, error) {
	if ip.To4() == nil {
		return false, fmt.Errorf("error: ip is invalid")
	}

	for _, cidr := range cidrs {
		if inNetwork, err := containsIP(cidr, ip); err != nil {
			return false, fmt.Errorf("error checking IP in CIDR %s: %w", cidr, err)
		} else if inNetwork {
			return true, nil
		}
	}

	return false, nil
}

func extractCIDRsFromNodeNetworks(networks []types.RemoteNodeNetwork) []string {
	var cidrs []string
	for _, network := range networks {
		for _, cidr := range network.Cidrs {
			if cidr != "" {
				cidrs = append(cidrs, cidr)
			}
		}
	}
	return cidrs
}

func extractNodeIPFromFlags(kubeletArgs []string) (net.IP, error) {
	ipStr := extractFlagValue(kubeletArgs, nodeIPFlag)

	if ipStr != "" {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			return nil, fmt.Errorf("invalid ip %s in --node-ip flag. only 1 IPv4 address is allowed", ipStr)
		} else if ip.To4() == nil {
			return nil, fmt.Errorf("invalid IPv6 address %s in --node-ip flag. only IPv4 is supported", ipStr)
		}
		return ip, nil
	}

	//--node-ip flag not set
	return nil, nil
}

func validateClusterRemoteNetworkConfig(cluster *types.Cluster) error {
	if cluster.RemoteNetworkConfig == nil {
		return fmt.Errorf("remote network config is not set for cluster %s", *cluster.Name)
	}
	if cluster.RemoteNetworkConfig.RemoteNodeNetworks == nil {
		return fmt.Errorf("remote node networks not found in remote network config for cluster %s", *cluster.Name)
	}
	return nil
}

// Validate given node IP belongs to the current host.
//
// validateNodeIP adapts the unexported 'validateNodeIP' function from kubelet.
// Source: https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/kubelet_node_status.go#L796
func validateNodeIP(nodeIP net.IP, interfaceAddrs func() ([]net.Addr, error)) error {
	// Honor IP limitations set in setNodeStatus()
	if nodeIP.To4() == nil && nodeIP.To16() == nil {
		return fmt.Errorf("nodeIP must be a valid IP address")
	}
	if nodeIP.IsLoopback() {
		return fmt.Errorf("nodeIP can't be loopback address")
	}
	if nodeIP.IsMulticast() {
		return fmt.Errorf("nodeIP can't be a multicast address")
	}
	if nodeIP.IsLinkLocalUnicast() {
		return fmt.Errorf("nodeIP can't be a link-local unicast address")
	}
	if nodeIP.IsUnspecified() {
		return fmt.Errorf("nodeIP can't be an all zeros address")
	}

	addrs, err := interfaceAddrs()
	if err != nil {
		return err
	}
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip != nil && ip.Equal(nodeIP) {
			return nil
		}
	}
	return fmt.Errorf("node IP: %q not found in the host's network interfaces", nodeIP.String())
}

// getNodeIP determines the node's IP address based on kubelet configuration and system information.
func getNodeIP(kubeletArgs []string, nodeName string, network Network) (net.IP, error) {
	// Follows algorithm used by kubelet to assign nodeIP
	// Implementation adapted for hybrid nodes
	// 1) Use nodeIP if set (and not "0.0.0.0"/"::")
	// 2) If the user has specified an IP to HostnameOverride, use it (not allowed for hybrid nodes)
	// 3) Lookup the IP from node name by DNS
	// 4) Try to get the IP from the network interface used as default gateway
	// Source: https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/nodestatus/setters.go#L206

	nodeIP, err := extractNodeIPFromFlags(kubeletArgs)
	if err != nil {
		return nil, err
	}

	var ipAddr net.IP

	nodeIPSpecified := nodeIP != nil && nodeIP.To4() != nil && !nodeIP.IsUnspecified()

	if nodeIPSpecified {
		ipAddr = nodeIP
	} else {
		// If using SSM, the node name will be set at initialization to the SSM instance ID,
		// so it won't resolve to anything via DNS, hence we're only checking in the case of IAM-RA
		if nodeName != "" {
			addrs, _ := network.LookupIP(nodeName)
			for _, addr := range addrs {
				if err = validateNodeIP(addr, network.InterfaceAddrs); addr.To4() != nil && err == nil {
					ipAddr = addr
					break
				}
			}
		}

		if ipAddr == nil {
			ipAddr, err = network.ResolveBindAddress(nodeIP)
		}

		if err != nil || ipAddr == nil {
			// We tried everything we could, but the IP address wasn't fetchable; error out
			return nil, fmt.Errorf("couldn't get ip address of node: %w", err)
		}

	}

	return ipAddr, nil
}

func validateIPInRemoteNodeNetwork(ipAddr net.IP, remoteNodeNetwork []types.RemoteNodeNetwork) error {
	nodeNetworkCidrs := extractCIDRsFromNodeNetworks(remoteNodeNetwork)

	if validIP, err := isIPInCIDRs(ipAddr, nodeNetworkCidrs); err != nil {
		return err
	} else if !validIP {
		// TODO: Update url with specific node IP troubleshooting section
		return fmt.Errorf(
			"node IP %s is not in any of the remote network CIDR blocks: %s. "+
				"See https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-troubleshooting.html or use --skip node-ip-validation",
			ipAddr, nodeNetworkCidrs)
	}
	return nil
}

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
		nodeIp, err := getNodeIP(kubeletArgs, iamNodeName, hnp.network)
		if err != nil {
			return err
		}

		cluster := hnp.cluster
		if validateClusterRemoteNetworkConfig(cluster) != nil {
			return err
		}

		if err = validateIPInRemoteNodeNetwork(nodeIp, cluster.RemoteNetworkConfig.RemoteNodeNetworks); err != nil {
			return err
		}
	}

	return nil
}
