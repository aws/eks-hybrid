package api

import (
	"encoding/json"
)

// IsFailSwapOnDisabled checks if the failSwapOn kubelet configuration is set to false.
// The failSwapOn setting defaults to true in Kubernetes, meaning kubelet will fail to start
// if swap is enabled. If a user explicitly sets it to false, kubelet will allow swap.
func IsFailSwapOnDisabled(nodeConfig *NodeConfig) (bool, error) {
	if nodeConfig == nil || len(nodeConfig.Spec.Kubelet.Config) == 0 {
		// No kubelet config provided, so failSwapOn defaults to true (swap not allowed)
		return false, nil
	}

	// Check if failSwapOn is explicitly set in the kubelet config
	if failSwapOnRaw, exists := nodeConfig.Spec.Kubelet.Config["failSwapOn"]; exists {
		var failSwapOn bool
		if err := json.Unmarshal(failSwapOnRaw.Raw, &failSwapOn); err != nil {
			return false, err
		}
		// If failSwapOn is explicitly set to false, then swap is allowed
		return !failSwapOn, nil
	}

	// failSwapOn not explicitly set, defaults to true (swap not allowed)
	return false, nil
}
