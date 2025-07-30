package kubernetes

import (
	"context"
	"fmt"
	"net"
	"net/url"

	"github.com/pkg/errors"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/network"
	"github.com/aws/eks-hybrid/internal/retry"
	"github.com/aws/eks-hybrid/internal/validation"
)

// ValidateAPIServerEndpointResolution validates access to the Kubernetes API endpoint
// This function conforms to the validation framework signature
func ValidateAPIServerEndpointResolution(ctx context.Context, informer validation.Informer, nodeConfig *api.NodeConfig) error {
	var err error
	name := "kubernetes-endpoint-access"
	informer.Starting(ctx, name, "Validating access to Kubernetes API endpoint")
	defer func() {
		informer.Done(ctx, name, err)
	}()

	err = checkAPIServerConnection(ctx, nodeConfig)
	return err
}

func checkAPIServerConnection(ctx context.Context, node *api.NodeConfig) error {
	endpoint, err := url.ParseRequestURI(node.Spec.Cluster.APIServerEndpoint)
	if err != nil {
		return validation.WithRemediation(err, "Ensure the Kubernetes API server endpoint provided is correct.")
	}

	err = validateEndpointResolution(ctx, endpoint.Hostname())
	if err != nil {
		return validation.WithRemediation(err, "Ensure DNS server settings and network connectivity are correct, and verify the hostname is reachable")
	}

	err = retry.NetworkRequest(ctx, func(ctx context.Context) error {
		return network.CheckConnectionToHost(ctx, *endpoint)
	})
	if err != nil {
		return validation.WithRemediation(err, "Ensure your network configuration allows the node to access the Kubernetes API endpoint.")
	}

	return nil
}

// validateEndpointResolution validates that a hostname DNS resolves
func validateEndpointResolution(ctx context.Context, hostname string) error {
	if hostname == "" {
		return errors.New("hostname is empty")
	}

	// Resolve the hostname to IP addresses
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return fmt.Errorf("failed to resolve hostname %s: %w", hostname, err)
	}

	if len(ips) == 0 {
		return fmt.Errorf("hostname %s did not resolve to any IP addresses", hostname)
	}

	return nil
}
