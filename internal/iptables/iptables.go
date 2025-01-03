package iptables

import (
	"context"
	"os/exec"
	"time"

	"github.com/pkg/errors"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/tracker"
)

const iptablesBinName = "iptables"

// Source interface for iptables package
type Source interface {
	GetIptables() artifact.Package
}

// Install iptables package required for kubelet.
func Install(ctx context.Context, tracker *tracker.Tracker, source Source) error {
	if !isIptablesInstalled() {
		iptablesSrc := source.GetIptables()
		// Sometimes install fails due to conflicts with other processes
		// updating packages, specially when automating at machine startup.
		// We assume errors are transient and just retry for a bit.
		if err := artifact.InstallPackageWithRetries(ctx, iptablesSrc, 5*time.Second); err != nil {
			return errors.Wrap(err, "failed to install iptables")
		}
		return tracker.Add(artifact.Iptables)
	}
	return nil
}

// Uninstall iptables package
func Uninstall(ctx context.Context, source Source) error {
	if isIptablesInstalled() {
		iptablesSrc := source.GetIptables()
		if err := artifact.UninstallPackageWithRetries(ctx, iptablesSrc, 5*time.Second); err != nil {
			return errors.Wrap(err, "failed to uninstall iptables")
		}
	}
	return nil
}

func isIptablesInstalled() bool {
	_, err := exec.LookPath(iptablesBinName)
	return err == nil
}
