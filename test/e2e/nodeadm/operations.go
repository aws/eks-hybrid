package nodeadm

import (
	"context"
	"fmt"

	"github.com/aws/eks-hybrid/test/e2e/commands"
	"github.com/aws/eks-hybrid/test/e2e/os"
)

// NodeUninstaller defines how to uninstall Kubernetes components from a node
type NodeUninstaller interface {
	Uninstall(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error
}

// NodeRebootter defines how to reboot a node
type NodeRebootter interface {
	Reboot(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error
}

// NodeLogCollector defines how to collect logs from a node
type NodeLogCollector interface {
	CollectLogs(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, logBundleUrl string) error
}

// NodeDebugger defines how to run debug operations on a node
type NodeDebugger interface {
	RunDebug(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error
	ShouldRunDebug() bool
}

// NodeadmVersionGetter defines how to get the nodeadm version from a node
type NodeadmVersionGetter interface {
	GetVersion(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) (string, error)
	ShouldGetVersion() bool
}

// NodeUpgrader defines how to upgrade a node
type NodeUpgrader interface {
	Upgrade(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, kubernetesVersion string) error
}

// BottlerocketUninstaller implements NodeUninstaller for Bottlerocket
type BottlerocketUninstaller struct{}

func (b *BottlerocketUninstaller) Uninstall(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	return StopKubelet(ctx, runner, instanceIP)
}

// GenericLinuxUninstaller implements NodeUninstaller for generic Linux
type GenericLinuxUninstaller struct{}

func (g *GenericLinuxUninstaller) Uninstall(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	return RunNodeadmUninstall(ctx, runner, instanceIP)
}

// BottlerocketRebootter implements NodeRebootter for Bottlerocket
type BottlerocketRebootter struct{}

func (b *BottlerocketRebootter) Reboot(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	commands := []string{
		"apiclient reboot",
	}

	// the ssh command will exit with an error because the machine reboots
	// ignoring output
	_, err := runner.Run(ctx, instanceIP, commands)
	if err != nil {
		return err
	}

	return nil
}

// GenericLinuxRebootter implements NodeRebootter for generic Linux
type GenericLinuxRebootter struct{}

func (g *GenericLinuxRebootter) Reboot(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	commands := []string{
		"set -eux",
		"rm -rf /var/lib/cloud/instances",
		"cloud-init clean --logs --reboot",
	}

	// the ssh command will exit with an error because the machine reboots
	// ignoring output
	_, err := runner.Run(ctx, instanceIP, commands)
	if err != nil {
		return err
	}

	return nil
}

// BottlerocketLogCollector implements NodeLogCollector for Bottlerocket
type BottlerocketLogCollector struct{}

func (b *BottlerocketLogCollector) CollectLogs(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, logBundleUrl string) error {
	commands := []string{
		"sudo /usr/sbin/chroot /.bottlerocket/rootfs/ logdog --output /var/log/eks-hybrid-logs.tar.gz",
		"sudo curl --retry 5 --request PUT --upload-file /.bottlerocket/rootfs/var/log/eks-hybrid-logs.tar.gz '" + logBundleUrl + "'",
		"sudo rm /.bottlerocket/rootfs/var/log/eks-hybrid-logs.tar.gz",
	}

	output, err := runner.Run(ctx, instanceIP, commands)
	if err != nil {
		return err
	}

	if output.Status != "Success" {
		return err
	}

	return nil
}

// GenericLinuxLogCollector implements NodeLogCollector for generic Linux
type GenericLinuxLogCollector struct{}

func (g *GenericLinuxLogCollector) CollectLogs(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, logBundleUrl string) error {
	commands := []string{
		"/tmp/log-collector.sh '" + logBundleUrl + "'",
	}

	output, err := runner.Run(ctx, instanceIP, commands)
	if err != nil {
		return err
	}

	if output.Status != "Success" {
		return err
	}

	return nil
}

// BottlerocketDebugger implements NodeDebugger for Bottlerocket
type BottlerocketDebugger struct{}

func (b *BottlerocketDebugger) RunDebug(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	// Bottlerocket doesn't support nodeadm debug
	return nil
}

func (b *BottlerocketDebugger) ShouldRunDebug() bool {
	return false
}

// GenericLinuxDebugger implements NodeDebugger for generic Linux
type GenericLinuxDebugger struct{}

func (g *GenericLinuxDebugger) RunDebug(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	return RunNodeadmDebug(ctx, runner, instanceIP)
}

func (g *GenericLinuxDebugger) ShouldRunDebug() bool {
	return true
}

// BottlerocketNodeadmVersionGetter implements NodeadmVersionGetter for Bottlerocket
type BottlerocketNodeadmVersionGetter struct{}

func (b *BottlerocketNodeadmVersionGetter) GetVersion(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) (string, error) {
	// Bottlerocket doesn't use nodeadm in the same way
	return "", nil
}

func (b *BottlerocketNodeadmVersionGetter) ShouldGetVersion() bool {
	return false
}

// GenericLinuxNodeadmVersionGetter implements NodeadmVersionGetter for generic Linux
type GenericLinuxNodeadmVersionGetter struct{}

func (g *GenericLinuxNodeadmVersionGetter) GetVersion(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) (string, error) {
	return RunNodeadmVersion(ctx, runner, instanceIP)
}

func (g *GenericLinuxNodeadmVersionGetter) ShouldGetVersion() bool {
	return true
}

// BottlerocketUpgrader implements NodeUpgrader for Bottlerocket
type BottlerocketUpgrader struct{}

func (b *BottlerocketUpgrader) Upgrade(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, kubernetesVersion string) error {
	// Bottlerocket doesn't support nodeadm upgrade
	return fmt.Errorf("upgrade not supported for Bottlerocket")
}

// GenericLinuxUpgrader implements NodeUpgrader for generic Linux
type GenericLinuxUpgrader struct{}

func (g *GenericLinuxUpgrader) Upgrade(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, kubernetesVersion string) error {
	return RunNodeadmUpgrade(ctx, runner, instanceIP, kubernetesVersion)
}

// NewNodeUninstaller returns the appropriate NodeUninstaller for the given OS
func NewNodeUninstaller(osName string) NodeUninstaller {
	if os.IsBottlerocket(osName) {
		return &BottlerocketUninstaller{}
	}
	return &GenericLinuxUninstaller{}
}

// NewNodeRebootter returns the appropriate NodeRebootter for the given OS
func NewNodeRebootter(osName string) NodeRebootter {
	if os.IsBottlerocket(osName) {
		return &BottlerocketRebootter{}
	}
	return &GenericLinuxRebootter{}
}

// NewNodeLogCollector returns the appropriate NodeLogCollector for the given OS
func NewNodeLogCollector(osName string) NodeLogCollector {
	if os.IsBottlerocket(osName) {
		return &BottlerocketLogCollector{}
	}
	return &GenericLinuxLogCollector{}
}

// NewNodeDebugger returns the appropriate NodeDebugger for the given OS
func NewNodeDebugger(osName string) NodeDebugger {
	if os.IsBottlerocket(osName) {
		return &BottlerocketDebugger{}
	}
	return &GenericLinuxDebugger{}
}

// NewNodeadmVersionGetter returns the appropriate NodeadmVersionGetter for the given OS
func NewNodeadmVersionGetter(osName string) NodeadmVersionGetter {
	if os.IsBottlerocket(osName) {
		return &BottlerocketNodeadmVersionGetter{}
	}
	return &GenericLinuxNodeadmVersionGetter{}
}

// NewNodeUpgrader returns the appropriate NodeUpgrader for the given OS
func NewNodeUpgrader(osName string) NodeUpgrader {
	if os.IsBottlerocket(osName) {
		return &BottlerocketUpgrader{}
	}
	return &GenericLinuxUpgrader{}
}
