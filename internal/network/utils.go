package network

import (
	"fmt"
	"net"

	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	apimachinerynet "k8s.io/apimachinery/pkg/util/net"
)

// Network interfaces with the host's network stack.
type Network interface {
	LookupIP(host string) ([]net.IP, error)
	ResolveBindAddress(bindAddress net.IP) (net.IP, error)
	InterfaceAddrs() ([]net.Addr, error)
}

// DefaultKubeletNetwork provides the network util functions used by kubelet.
type DefaultKubeletNetwork struct{}

func (u DefaultKubeletNetwork) LookupIP(host string) ([]net.IP, error) {
	return net.LookupIP(host)
}

func (u DefaultKubeletNetwork) ResolveBindAddress(bindAddress net.IP) (net.IP, error) {
	return apimachinerynet.ResolveBindAddress(bindAddress)
}

func (u DefaultKubeletNetwork) InterfaceAddrs() ([]net.Addr, error) {
	return net.InterfaceAddrs()
}

// NewDefaultNetwork creates a new instance of DefaultKubeletNetwork
func NewDefaultNetwork() Network {
	return &DefaultKubeletNetwork{}
}

// ContainsIP checks if the given IP is within the specified CIDR block
func ContainsIP(cidr string, ip net.IP) (bool, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false, err
	}

	return ipnet.Contains(ip), nil
}

// IsIPInCIDRs checks if the given IP is within any of the specified CIDR blocks
func IsIPInCIDRs(ip net.IP, cidrs []string) (bool, error) {
	if ip.To4() == nil {
		return false, fmt.Errorf("error: ip is invalid")
	}

	for _, cidr := range cidrs {
		if inNetwork, err := ContainsIP(cidr, ip); err != nil {
			return false, fmt.Errorf("error checking IP in CIDR %s: %w", cidr, err)
		} else if inNetwork {
			return true, nil
		}
	}

	return false, nil
}

// ExtractCIDRsFromNodeNetworks extracts CIDR blocks from remote node networks
func ExtractCIDRsFromNodeNetworks(networks []types.RemoteNodeNetwork) []string {
	cidrs := make([]string, 0)
	for _, network := range networks {
		for _, cidr := range network.Cidrs {
			if cidr != "" {
				cidrs = append(cidrs, cidr)
			}
		}
	}
	return cidrs
}

// ExtractFlagValue extracts the value of a specific flag from kubelet arguments
func ExtractFlagValue(args []string, flag string) string {
	flagPrefix := "--" + flag + "="
	var flagValue string

	// get last instance of flag value if it exists
	for _, arg := range args {
		if len(arg) > len(flagPrefix) && arg[:len(flagPrefix)] == flagPrefix {
			flagValue = arg[len(flagPrefix):]
		}
	}

	return flagValue
}

// ExtractNodeIPFromFlags extracts the node IP from kubelet flags
func ExtractNodeIPFromFlags(kubeletArgs []string) (net.IP, error) {
	ipStr := ExtractFlagValue(kubeletArgs, "node-ip")

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

// ValidateNodeIP validates that the given node IP belongs to the current host.
//
// ValidateNodeIP adapts the unexported 'validateNodeIP' function from kubelet.
// Source: https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/kubelet_node_status.go#L796
func ValidateNodeIP(nodeIP net.IP, network Network) error {
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

	addrs, err := network.InterfaceAddrs()
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

// GetNodeIP determines the node's IP address based on kubelet configuration and system information.
func GetNodeIP(kubeletArgs []string, nodeName string, network Network) (net.IP, error) {
	// Follows algorithm used by kubelet to assign nodeIP
	// Implementation adapted for hybrid nodes
	// 1) Use nodeIP if set (and not "0.0.0.0"/"::")
	// 2) If the user has specified an IP to HostnameOverride, use it (not allowed for hybrid nodes)
	// 3) Lookup the IP from node name by DNS
	// 4) Try to get the IP from the network interface used as default gateway
	// Source: https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/nodestatus/setters.go#L206

	nodeIP, err := ExtractNodeIPFromFlags(kubeletArgs)
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
				if err = ValidateNodeIP(addr, network); addr.To4() != nil && err == nil {
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

// ValidateIPInRemoteNodeNetwork validates that the given IP is within the remote node networks
func ValidateIPInRemoteNodeNetwork(ipAddr net.IP, remoteNodeNetwork []types.RemoteNodeNetwork) error {
	nodeNetworkCidrs := ExtractCIDRsFromNodeNetworks(remoteNodeNetwork)

	if validIP, err := IsIPInCIDRs(ipAddr, nodeNetworkCidrs); err != nil {
		return err
	} else if !validIP {
		return fmt.Errorf(
			"node IP %s is not in any of the remote network CIDR blocks: %s. "+
				"See https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-troubleshooting.html or use --skip node-ip-validation",
			ipAddr, nodeNetworkCidrs)
	}
	return nil
}

// ValidateClusterRemoteNetworkConfig validates that the cluster has proper remote network configuration
func ValidateClusterRemoteNetworkConfig(cluster *types.Cluster) error {
	if cluster.RemoteNetworkConfig == nil {
		return fmt.Errorf("remote network config is not set for cluster %s", *cluster.Name)
	}
	if cluster.RemoteNetworkConfig.RemoteNodeNetworks == nil {
		return fmt.Errorf("remote node networks not found in remote network config for cluster %s", *cluster.Name)
	}
	return nil
}

// ValidateMTU validates that the MTU value is within acceptable ranges
// MTU should be <= 1500 (standard Ethernet) or between 8000-9001 (jumbo frames)
func ValidateMTU(mtu int) error {
	if mtu <= 0 {
		return fmt.Errorf("MTU must be a positive value, got %d", mtu)
	}

	// Standard Ethernet MTU range (68 is minimum IPv4 MTU, 1500 is standard Ethernet)
	if mtu >= 68 && mtu <= 1500 {
		return nil
	}

	// Jumbo frame MTU range
	if mtu >= 8000 && mtu <= 9001 {
		return nil
	}

	return fmt.Errorf("MTU %d is not in acceptable ranges: 68-1500 (standard) or 8000-9001 (jumbo frames)", mtu)
}

// FindNetworkInterfaceForIP finds the network interface that has the given IP address
func FindNetworkInterfaceForIP(nodeIP net.IP) (*net.Interface, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get network interfaces: %w", err)
	}

	for _, iface := range interfaces {
		// Skip loopback and down interfaces for MTU validation purposes
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		// Get addresses for this interface
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		// Check if this interface has the target IP
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip != nil && ip.Equal(nodeIP) {
				return &iface, nil
			}
		}
	}

	return nil, fmt.Errorf("no active network interface found with IP %s", nodeIP.String())
}

// ValidateNetworkInterfaceMTUForIP validates MTU for the network interface associated with the given IP
func ValidateNetworkInterfaceMTUForIP(nodeIP net.IP) error {
	iface, err := FindNetworkInterfaceForIP(nodeIP)
	if err != nil {
		return err
	}

	if err := ValidateMTU(iface.MTU); err != nil {
		return fmt.Errorf("interface %s (IP: %s) has invalid MTU %d: %w", iface.Name, nodeIP.String(), iface.MTU, err)
	}

	return nil
}
