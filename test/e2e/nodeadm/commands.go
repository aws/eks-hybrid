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

	output, err := runner.Run(ctx, instanceIP, "", commands)
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

	output, err := runner.Run(ctx, instanceIP, "", commands)
	if err != nil {
		return fmt.Errorf("running remote command: %w", err)
	}

	if output.Status != "Success" {
		return fmt.Errorf("nodeadm upgrade remote command did not succeed")
	}

	return nil
}

func RebootInstance(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, os string) error {
	var commands []string
	if strings.Contains(os, "bottlerocket") {
		commands = []string{
			"apiclient reboot",
		}
	} else {
		commands = []string{
			"set -eux",
			"rm -rf /var/lib/cloud/instances",
			"cloud-init clean --logs --reboot",
		}
	}

	// the ssh command will exit with an error because the machine reboots after cloud-init clean
	// ignoring output
	_, err := runner.Run(ctx, instanceIP, os, commands)
	if err != nil {
		return fmt.Errorf("running remote command: %w", err)
	}

	return nil
}

func RunLogCollector(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, os, logBundleUrl string) error {
	var commands []string
	if strings.Contains(os, "bottlerocket") {
		commands = []string{
			"sudo /usr/sbin/chroot /.bottlerocket/rootfs/ logdog --output /var/log/eks-hybrid-logs.tar.gz",
			fmt.Sprintf("sudo curl --retry 5 --request PUT --upload-file /.bottlerocket/rootfs/var/log/eks-hybrid-logs.tar.gz '%s'", logBundleUrl),
			"sudo rm /.bottlerocket/rootfs/var/log/eks-hybrid-logs.tar.gz",
		}
	} else {
		commands = []string{
			fmt.Sprintf("/tmp/log-collector.sh '%s'", logBundleUrl),
		}
	}

	output, err := runner.Run(ctx, instanceIP, os, commands)
	if err != nil {
		return fmt.Errorf("running remote command: %w", err)
	}

	if output.Status != "Success" {
		return fmt.Errorf("log collector remote command did not succeed")
	}

	return nil
}

func RunNodeadmDebug(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, os string) error {
	if strings.Contains(os, "bottlerocket") {
		return nil
	}

	commands := []string{
		"/tmp/nodeadm debug -c file:///nodeadm-config.yaml",
	}

	output, err := runner.Run(ctx, instanceIP, os, commands)
	if err != nil {
		return fmt.Errorf("running remote command: %w", err)
	}

	if output.Status != "Success" {
		return fmt.Errorf("nodeadm debug remote command did not succeed")
	}

	return nil
}

func StopKubelet(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, os string) error {
	commands := []string{
		"sudo /usr/sbin/chroot /.bottlerocket/rootfs systemctl stop kubelet",
	}

	output, err := runner.Run(ctx, instanceIP, os, commands)
	if err != nil {
		return fmt.Errorf("running remote command: %w", err)
	}

	if output.Status != "Success" {
		return fmt.Errorf("systemctl remote command did not succeed")
	}

	return nil
}
