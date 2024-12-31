package nodeadm

import (
	"context"
	"fmt"

	"github.com/aws/eks-hybrid/test/e2e/commands"
)

func RunNodeadmUninstall(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	commands := []string{
		"set -eux",
		"trap \"/tmp/log-collector.sh 'post-uninstall' 'post-final-uninstall'\" EXIT",
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
		"set -eux",
		"trap \"/tmp/log-collector.sh 'post-upgrade'\" EXIT",
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

func RebootInstance(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	commands := []string{
		"set -eux",
		"rm -rf /var/lib/cloud/instances",
		"cloud-init clean --logs --reboot",
	}

	// the ssh command will exit with an error because the machine reboots after cloud-init clean
	// ignoring output
	_, err := runner.Run(ctx, instanceIP, commands)
	if err != nil {
		return fmt.Errorf("running remote command: %w", err)
	}

	return nil
}
