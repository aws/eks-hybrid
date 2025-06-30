package system

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/validation"
)

// SwapValidator validates swap configuration before nodeadm init
type SwapValidator struct {
	logger *zap.Logger
}

// NewSwapValidator creates a new SwapValidator
func NewSwapValidator(logger *zap.Logger) *SwapValidator {
	return &SwapValidator{
		logger: logger,
	}
}

// Run validates the swap configuration
func (v *SwapValidator) Run(ctx context.Context, informer validation.Informer, nodeConfig *api.NodeConfig) error {
	informer.Starting(ctx, "swap", "Checking swap configuration...")

	// Check if kubelet is configured to allow swap (failSwapOn=false)
	swapAllowed, err := api.IsFailSwapOnDisabled(nodeConfig)
	if err != nil {
		v.logger.Error("Failed to check kubelet failSwapOn configuration", zap.Error(err))
		informer.Done(ctx, "swap", err)
		return fmt.Errorf("failed to check kubelet failSwapOn configuration: %w", err)
	}

	if swapAllowed {
		v.logger.Info("Kubelet configured to allow swap (failSwapOn=false), skipping swap validation and disablement")
		informer.Done(ctx, "swap", nil)
		return nil
	}

	// Check for partition-type swap that would cause init to fail
	hasPartitionSwap, err := partitionSwapExists()
	if err != nil {
		v.logger.Error("Failed to check swap configuration", zap.Error(err))
		informer.Done(ctx, "swap", err)
		return fmt.Errorf("failed to check swap configuration: %w", err)
	}

	if hasPartitionSwap {
		err := fmt.Errorf("partition type swap found on the host. Nodeadm can only disable file-type swap automatically. " +
			"Please manually disable swap partitions before running nodeadm init, or set kubelet config 'failSwapOn: false' to allow swap. " +
			"You can disable swap with: 'sudo swapoff -a' and remove swap entries from /etc/fstab")
		v.logger.Error("Swap validation failed", zap.Error(err))
		informer.Done(ctx, "swap", err)
		return err
	}

	// Check if there are any file-type swaps (these can be handled automatically)
	swapfiles, err := getSwapfilePaths()
	if err != nil {
		v.logger.Error("Failed to get swap file paths", zap.Error(err))
		informer.Done(ctx, "swap", err)
		return fmt.Errorf("failed to get swap file paths: %w", err)
	}

	fileSwapCount := 0
	for _, swap := range swapfiles {
		if swap.swapType == swapTypeFile {
			fileSwapCount++
		}
	}

	if fileSwapCount > 0 {
		v.logger.Info("File-type swap detected - will be automatically disabled during init",
			zap.Int("swap_files", fileSwapCount))
	} else {
		v.logger.Info("No swap detected")
	}

	informer.Done(ctx, "swap", nil)
	return nil
}
