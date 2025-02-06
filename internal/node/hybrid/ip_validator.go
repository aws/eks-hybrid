package hybrid

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/aws/eks-hybrid/internal/aws/eks"
	"github.com/aws/eks-hybrid/internal/nodeprovider"
	apimachinerynet "k8s.io/apimachinery/pkg/util/net"
	nodeutil "k8s.io/component-helpers/node/util"
	k8snet "k8s.io/utils/net"
)

const (
	nodeIPFlag           = "--node-ip="
	hostnameOverrideFlag = "--hostname-override="
)

func containsIP(cidr string, ip net.IP) (bool, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false, err
	}

	return ipnet.Contains(ip), nil
}

func isIPInClusterNetworks(ip net.IP, cluster *eks.Cluster) (bool, error) {
	for _, network := range cluster.RemoteNetworkConfig.RemoteNodeNetworks {
		for _, cidr := range network.CIDRs {
			if cidr == nil {
				continue
			}

			if ipInCidr, err := containsIP(*cidr, ip); err != nil {
				return false, err
			} else if ipInCidr {
				return true, nil
			}
		}
	}

	return false, nil
}

func validateIP(ipAddr net.IP, cluster *eks.Cluster) error {
	if validIP, err := isIPInClusterNetworks(ipAddr, cluster); err != nil {
		return err
	} else if !validIP {
		cidrs := getClusterCIDRs(cluster)

		return fmt.Errorf(
			"node IP %s is not in any of the remote network CIDR blocks: %s; "+
				"use .spec.kubelet.flags field in config-source yaml to set node-ip to an IP within one of these CIDR blocks"+
				"(e.g. --node-ip=10.0.0.1) "+
				"or use --skip ip-validation",
			ipAddr, cidrs,
		)
	}
	return nil
}

func getClusterCIDRs(cluster *eks.Cluster) []string {
	var cidrs []string
	for _, network := range cluster.RemoteNetworkConfig.RemoteNodeNetworks {
		for _, cidr := range network.CIDRs {
			if cidr != nil {
				cidrs = append(cidrs, *cidr)
			}
		}
	}
	return cidrs
}

func extractFlagValue(kubeletEnv, flag string) (string, error) {
	if count := strings.Count(kubeletEnv, flag); count > 1 {
		return "", fmt.Errorf("multiple %s flags found in kubelet args", flag)
	}

	for _, s := range strings.Fields(kubeletEnv) {
		if strings.HasPrefix(s, flag) {
			return strings.TrimPrefix(s, flag), nil
		}
	}
	return "", nil
}

func extractNodeIPFromFlag(kubeletEnv string) (net.IP, error) {
	ipStr, err := extractFlagValue(kubeletEnv, nodeIPFlag)
	if err != nil {
		return nil, err
	}

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

func extractHostName(kubeletEnv string) (string, error) {
	hostnameOverride, err := extractFlagValue(kubeletEnv, hostnameOverrideFlag)
	if err != nil {
		return "", fmt.Errorf("failed to extract hostname override: %w", err)
	}

	// tracks how kubelet finds hostname:
	// https://github.com/kubernetes/kubernetes/blob/48f36acc7a13d37c357aa6abff55a01267eab8a9/cmd/kubelet/app/options/options.go#L293
	// https://github.com/kubernetes/kubernetes/blob/28ad751946bca0376f7138cdcac1ad0ec094e9ff/cmd/kubelet/app/server.go#L259
	// https://github.com/kubernetes/kubernetes/blob/28ad751946bca0376f7138cdcac1ad0ec094e9ff/cmd/kubelet/app/server.go#L1228
	hostname, err := nodeutil.GetHostname(hostnameOverride) // returns error if it cannot resolve to a non-empty hostname
	if err != nil {
		return "", fmt.Errorf("failed to get hostname: %w", err)
	}

	return hostname, nil
}

func getNodeName(kubeletEnv string) (string, error) {
	return extractHostName(kubeletEnv)
}

// Validate given node IP belongs to the current host.
//
// validateNodeIP adapts the unexported 'validateNodeIP' function from Kubernetes.
// Source: https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/kubelet_node_status.go#L796
func validateNodeIP(nodeIP net.IP) error {
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

	addrs, err := net.InterfaceAddrs()
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
func getNodeIP(kubeletEnv string) (net.IP, error) {
	// Follows algorithm used by kubelet to assign nodeIP
	// Implementation adapted for hybrid nodes
	// 1) Use nodeIP if set (and not "0.0.0.0"/"::")
	// 2) If the user has specified an IP to HostnameOverride, use it
	// 3) Lookup the IP from node name by DNS
	// 4) Try to get the IP from the network interface used as default gateway
	// Original source: https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/nodestatus/setters.go#L206

	nodeIP, err := extractNodeIPFromFlag(kubeletEnv)
	if err != nil {
		return nil, err
	}
	hostname, err := extractHostName(kubeletEnv)
	if err != nil {
		return nil, err
	}

	var ipAddr net.IP

	nodeIPSpecified := nodeIP != nil && nodeIP.To4() != nil && !nodeIP.IsUnspecified()

	if nodeIPSpecified {
		ipAddr = nodeIP
	} else if addr := k8snet.ParseIPSloppy(hostname); addr != nil {
		// error out if hostname is an IPv6 address
		if addr.To4() == nil {
			return nil, fmt.Errorf("hostname address %s is not IPv4", addr)
		}
		ipAddr = addr
	} else {
		var addrs []net.IP
		nodeName, err := getNodeName(kubeletEnv)
		if err != nil {
			return nil, err
		}

		addrs, _ = net.LookupIP(nodeName)
		for _, addr := range addrs {
			if err = validateNodeIP(addr); addr.To4() != nil && err == nil { // kubelet will also pick an IPv4 addr: https://github.com/kubernetes/kubernetes/blob/5d3c07e89db298e9b7f79718ccb8cf2116b7116e/pkg/kubelet/nodestatus/setters.go#L79
				ipAddr = addr
				break
			}
		}

		if ipAddr == nil {
			ipAddr, err = apimachinerynet.ResolveBindAddress(nodeIP) // current standard function for resolving bind address: https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/kubelet_node_status.go#L768
		}

		if ipAddr == nil {
			// We tried everything we could, but the IP address wasn't fetchable; error out
			return nil, fmt.Errorf("can't get ip address of node %s. error: %v", nodeName, err)
		}

	}

	return ipAddr, nil
}

// ValidateNodeIp checks if the node's IP is within one of the CIDR ranges in the RemoteNetworkConfig
func ValidateNodeIp(ctx context.Context, provider nodeprovider.NodeProvider, kubeletEnv string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// only validate hybrid nodes
	if !provider.GetNodeConfig().IsHybridNode() {
		return nil
	}

	nodeIp, err := getNodeIP(kubeletEnv)
	if err != nil {
		return err
	}
	cluster, err := readCluster(ctx, *provider.GetConfig(), provider.GetNodeConfig())
	if err != nil {
		return err
	}

	if err := validateIP(nodeIp, cluster); err != nil {
		return err
	}

	return nil
}
