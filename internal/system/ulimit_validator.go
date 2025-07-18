package system

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/validation"
)

// UlimitValidator validates ulimit configuration before nodeadm init
type UlimitValidator struct {
	logger *zap.Logger
}

// NewUlimitValidator creates a new UlimitValidator
func NewUlimitValidator(logger *zap.Logger) *UlimitValidator {
	return &UlimitValidator{
		logger: logger,
	}
}

// Run validates the ulimit configuration
func (v *UlimitValidator) Run(ctx context.Context, informer validation.Informer, nodeConfig *api.NodeConfig) error {
	var err error
	informer.Starting(ctx, "ulimit", "Checking ulimit configuration")
	defer func() {
		informer.Done(ctx, "ulimit", err)
	}()
	if err = v.Validate(); err != nil {
		return err
	}

	return nil
}

// Validate performs the ulimit validation
func (v *UlimitValidator) Validate() error {
	limits, err := getUlimits()
	if err != nil {
		v.logger.Error("Failed to get ulimit values", zap.Error(err))
		return fmt.Errorf("unable to read ulimit configuration: %w", err)
	}

	// Check for critical ulimit values that could affect Kubernetes node operation
	issues := v.checkCriticalLimits(limits)
	if len(issues) > 0 {
		for _, issue := range issues {
			v.logger.Warn("Ulimit issue detected", zap.String("issue", issue))
		}

		err := fmt.Errorf("ulimit configuration issues detected: %d issues found", len(issues))
		remediation := "Consider adjusting the following ulimit values for optimal Kubernetes node operation:\n"
		for _, issue := range issues {
			remediation += "  - " + issue + "\n"
		}
		remediation += "You can modify limits in /etc/security/limits.conf or systemd service files."

		return validation.WithRemediation(err, remediation)
	}

	v.logger.Info("Ulimit configuration appears adequate for Kubernetes node operation")
	return nil
}

// checkCriticalLimits checks for ulimit values that could impact Kubernetes operation
func (v *UlimitValidator) checkCriticalLimits(limits map[string]*ulimit) []string {
	var issues []string

	// Check max number of open files (nofile)
	if nofile, exists := limits["nofile"]; exists {
		if nofile.soft < 65536 {
			issues = append(issues, fmt.Sprintf("max open files (soft) is %d, recommended minimum is 65536", nofile.soft))
		}
		if nofile.hard < 65536 {
			issues = append(issues, fmt.Sprintf("max open files (hard) is %d, recommended minimum is 65536", nofile.hard))
		}
	} else {
		issues = append(issues, "max open files (nofile) limit not found")
	}

	// Check max number of processes (nproc)
	if nproc, exists := limits["nproc"]; exists {
		if nproc.soft < 32768 {
			issues = append(issues, fmt.Sprintf("max processes (soft) is %d, recommended minimum is 32768", nproc.soft))
		}
		if nproc.hard < 32768 {
			issues = append(issues, fmt.Sprintf("max processes (hard) is %d, recommended minimum is 32768", nproc.hard))
		}
	} else {
		issues = append(issues, "max processes (nproc) limit not found")
	}

	// Check core file size (core) - should typically be 0 or unlimited for production
	if core, exists := limits["core"]; exists {
		if core.soft != 0 && core.soft != ^uint64(0) { // 0 or unlimited
			issues = append(issues, fmt.Sprintf("core file size (soft) is %d, consider setting to 0 or unlimited for production", core.soft))
		}
	}

	return issues
}
