package os

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/eks-hybrid/test/e2e/commands"
)

type GenericLinuxOS struct {
	amiArchitecture string
	architecture    architecture
	osName          string
}

func NewGenericLinuxOS(osName string, arch architecture) *GenericLinuxOS {
	os := new(GenericLinuxOS)
	os.osName = osName
	os.architecture = arch

	if arch == amd64 {
		os.amiArchitecture = x8664Arch
	} else {
		os.amiArchitecture = arm64Arch
	}

	return os
}

func (a GenericLinuxOS) RebootInstance(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	commands := []string{
		"set -eux",
		"rm -rf /var/lib/cloud/instances",
		"cloud-init clean --logs --reboot",
	}

	// the ssh command will exit with an error because the machine reboots
	// ignoring output
	_, err := runner.Run(ctx, instanceIP, commands)
	if err != nil {
		return fmt.Errorf("running remote command: %w", err)
	}

	return nil
}

func (a GenericLinuxOS) CollectLogs(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, logBundleUrl string) error {
	commands := []string{
		fmt.Sprintf("/tmp/log-collector.sh '%s'", logBundleUrl),
	}

	output, err := runner.Run(ctx, instanceIP, commands)
	if err != nil {
		return fmt.Errorf("running remote command: %w", err)
	}

	if output.Status != "Success" {
		return fmt.Errorf("log collector remote command did not succeed")
	}

	return nil
}

func (a GenericLinuxOS) Uninstall(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
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

func (a GenericLinuxOS) GetNodeadmVersion(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) (string, error) {
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

func (a GenericLinuxOS) RunNodeadmDebug(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
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

func (a GenericLinuxOS) ShouldRunNodeadmDebug() bool {
	return true
}

func (a GenericLinuxOS) Upgrade(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, kubernetesVersion string) error {
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

func (a GenericLinuxOS) PodIdentityAgentDaemonsetName() string {
	return "eks-pod-identity-agent-hybrid"
}
