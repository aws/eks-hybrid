package nodeadm

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/eks-hybrid/test/e2e/commands"
)

func RunNodeadmUninstall(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	commands := []string{
		"/tmp/nodeadm uninstall",
	}

	output, err := runner.Run(ctx, instanceIP, commands)
	if err != nil {
		return fmt.Errorf("running remote command: %w", err)
	}

	if output.Status != "Success" {
		return fmt.Errorf("nodeadm uninstall remote command did not succeed")
	}

	return nil
}

func RunNodeadmUpgrade(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, kubernetesVersion string) error {
	commands := []string{
		fmt.Sprintf("/tmp/nodeadm upgrade %s -c file:///nodeadm-config.yaml", kubernetesVersion),
	}

	output, err := runner.Run(ctx, instanceIP, commands)
	if err != nil {
		return fmt.Errorf("running remote command: %w", err)
	}

	if output.Status != "Success" {
		return fmt.Errorf("nodeadm upgrade remote command did not succeed")
	}

	return nil
}

// RebootInstance reboots the remote instance
// DEPRECATED: Use NewNodeRebootter(osName).Reboot() instead
func RebootInstance(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, osName string) error {
	rebootter := NewNodeRebootter(osName)
	return rebootter.Reboot(ctx, runner, instanceIP)
}

// RunLogCollector runs the log collector on the remote instance
// DEPRECATED: Use NewNodeLogCollector(osName).CollectLogs() instead
func RunLogCollector(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, osName, logBundleUrl string) error {
	collector := NewNodeLogCollector(osName)
	return collector.CollectLogs(ctx, runner, instanceIP, logBundleUrl)
}

func RunNodeadmDebug(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	commands := []string{
		"/tmp/nodeadm debug -c file:///nodeadm-config.yaml",
	}

	output, err := runner.Run(ctx, instanceIP, commands)
	if err != nil {
		return fmt.Errorf("running remote command: %w", err)
	}

	if output.Status != "Success" {
		return fmt.Errorf("nodeadm debug remote command did not succeed")
	}

	return nil
}

func RunNodeadmVersion(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) (string, error) {
	commands := []string{
		"/tmp/nodeadm version",
	}

	output, err := runner.Run(ctx, instanceIP, commands)
	if err != nil {
		return "", fmt.Errorf("running remote command: %w", err)
	}

	if output.Status != "Success" {
		return "", fmt.Errorf("nodeadm version remote command did not succeed")
	}

	return strings.TrimSpace(output.StandardOutput), nil
}

func StopKubelet(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	commands := []string{
		"sudo /usr/sbin/chroot /.bottlerocket/rootfs systemctl stop kubelet",
	}

	output, err := runner.Run(ctx, instanceIP, commands)
	if err != nil {
		return fmt.Errorf("running remote command: %w", err)
	}

	if output.Status != "Success" {
		return fmt.Errorf("systemctl remote command did not succeed")
	}

	return nil
}
