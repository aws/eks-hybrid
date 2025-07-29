package system

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/validation"
)

const (
	noFileRecommendedLimit uint64 = 1024
	nProcRecommendedLimit  uint64 = 4096
)

// UlimitValidator validates ulimit configuration before nodeadm init
type UlimitValidator struct{}

// NewUlimitValidator creates a new UlimitValidator
func NewUlimitValidator() *UlimitValidator {
	return &UlimitValidator{}
}

// Run validates the ulimit configuration
func (v *UlimitValidator) Run(ctx context.Context, informer validation.Informer, nodeConfig *api.NodeConfig) error {
	var err error
	informer.Starting(ctx, "ulimit", "Validating ulimit configuration")
	defer func() {
		informer.Done(ctx, "ulimit", err)
	}()

	noFileLimit, nProcLimit, err := getUlimits()
	if err != nil {
		return fmt.Errorf("unable to read ulimit configuration: %w", err)
	}

	issues := v.checkCriticalLimits(noFileLimit, nProcLimit)
	if len(issues) > 0 {
		err = validation.WithRemediation(fmt.Errorf("ulimit configuration issues detected: %d issues found:\n%s", len(issues), strings.Join(issues, "\n")),
			"Consider adjusting the above ulimit values for optimal Kubernetes node operation",
		)
		return err
	}

	return nil
}

// checkCriticalLimits checks for ulimit values that could impact Kubernetes operation
func (v *UlimitValidator) checkCriticalLimits(noFileLimit, nProcLimit uint64) []string {
	var issues []string
	count := 0
	if noFileLimit < noFileRecommendedLimit {
		count++
		issues = append(issues, fmt.Sprintf("        %d. max open file descriptors limit is %d, which is lower than the recommended value of %d", count, noFileLimit, noFileRecommendedLimit))
	}

	if nProcLimit < nProcRecommendedLimit {
		count++
		issues = append(issues, fmt.Sprintf("        %d. max processes limit is %d, which is lower than the recommended value of %d", count, nProcLimit, nProcRecommendedLimit))
	}

	return issues
}
