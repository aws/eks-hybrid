package system

import (
	"context"
	"fmt"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/validation"
)

// SwapValidator validates swap configuration in nodeadm debug
type SwapValidator struct{}

// NewSwapValidator creates a new SwapValidator
func NewSwapValidator() *SwapValidator {
	return &SwapValidator{}
}

// Run validates the swap configuration
func (v *SwapValidator) Run(ctx context.Context, informer validation.Informer, _ *api.NodeConfig) error {
	var err error
	informer.Starting(ctx, "swap", "Validating swap configuration")
	defer func() {
		informer.Done(ctx, "swap", err)
	}()

	swapfiles, err := getSwapfilePaths()
	if err != nil {
		err = fmt.Errorf("getting swapfile paths : %w", err)
		return err
	}

	// Check for partition-type swap that would cause init to fail
	hasPartitionSwap, err := partitionSwapExists(swapfiles)
	if err != nil {
		err = fmt.Errorf("check if partition swap exists: %w", err)
		return err
	}

	if hasPartitionSwap {
		err = validation.WithRemediation(fmt.Errorf("partition swap detected on host"),
			"Nodeadm can only disable file-based swap automatically. Please manually disable partition swap before running nodeadm init. "+
				"Run 'sudo swapoff -a' to disable swap and remove swap entries from /etc/fstab to make the change persistent.")
		return err
	}

	// Check for any remaining swap (both partition and file types)
	if len(swapfiles) > 0 {
		err = fmt.Errorf("swap still active on host: %d swap entries found", len(swapfiles))

		if len(swapfiles) == 1 && swapfiles[0].swapType == swapTypeFile {
			err = validation.WithRemediation(err,
				"File-based swap should have been disabled during nodeadm init. This may indicate init did not complete successfully. "+
					"Try running 'sudo swapoff "+swapfiles[0].filePath+"' and re-run nodeadm init if needed.")
			return err
		} else {
			err = validation.WithRemediation(err,
				"All swap should be disabled before running debug. For partition swap, run 'sudo swapoff -a' and remove entries from /etc/fstab. "+
					"For file swap, this indicates nodeadm init may not have completed successfully.")
			return err
		}
	}

	return nil
}
