package upgrade

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/integrii/flaggy"
	"go.uber.org/zap"
	"k8s.io/utils/strings/slices"

	"github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/internal/cni"
	"github.com/aws/eks-hybrid/internal/containerd"
	"github.com/aws/eks-hybrid/internal/creds"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/flows"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/logger"
	"github.com/aws/eks-hybrid/internal/node"
	"github.com/aws/eks-hybrid/internal/packagemanager"
	"github.com/aws/eks-hybrid/internal/ssm"
	"github.com/aws/eks-hybrid/internal/tracker"
)

const (
	skipPodPreflightCheck  = "pod-validation"
	skipNodePreflightCheck = "node-validation"
	initNodePreflightCheck = "init-validation"
)

const upgradeHelpText = `Examples:
  # Upgrade all components
  nodeadm upgrade 1.31 --config-source file:///root/nodeConfig.yaml

  # Upgrade all components with a maximum timeout
  nodeadm upgrade 1.31 --config-source file:///root/nodeConfig.yaml --timeout 1h23s

Documentation:
  https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-nodeadm.html`

func NewUpgradeCommand() cli.Command {
	cmd := command{
		timeout: 20 * time.Minute,
	}

	fc := flaggy.NewSubcommand("upgrade")
	fc.Description = "Upgrade components installed using the install sub-command"
	fc.AdditionalHelpAppend = upgradeHelpText
	fc.AddPositionalValue(&cmd.kubernetesVersion, "KUBERNETES_VERSION", 1, true, "The major[.minor[.patch]] version of Kubernetes to install")
	fc.String(&cmd.configSource, "c", "config-source", "Source of node configuration. The format is a URI with supported schemes: [file, imds].")
	fc.StringSlice(&cmd.skipPhases, "s", "skip", "Phases of the upgrade you want to skip.")
	fc.Duration(&cmd.timeout, "t", "timeout", "Maximum upgrade command duration. Input follows duration format. Example: 1h23s")
	cmd.flaggy = fc
	return &cmd
}

type command struct {
	flaggy            *flaggy.Subcommand
	configSource      string
	skipPhases        []string
	kubernetesVersion string
	timeout           time.Duration
}

func (c *command) Flaggy() *flaggy.Subcommand {
	return c.flaggy
}

func (c *command) Run(log *zap.Logger, opts *cli.GlobalOptions) error {
	ctx := context.Background()
	ctx = logger.NewContext(ctx, log)

	root, err := cli.IsRunningAsRoot()
	if err != nil {
		return err
	}
	if !root {
		return cli.ErrMustRunAsRoot
	}

	if c.configSource == "" {
		flaggy.ShowHelpAndExit("--config-source is a required flag. The format is a URI with supported schemes: [file, imds]." +
			" For example on hybrid nodes --config-source file://nodeConfig.yaml")
	}

	log.Info("Loading installed components")
	installed, err := tracker.GetInstalledArtifacts()
	if err != nil && os.IsNotExist(err) {
		log.Info("No nodeadm components installed. Please use nodeadm install and nodeadm init commands to bootstrap a node")
		return nil
	} else if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	if !slices.Contains(c.skipPhases, initNodePreflightCheck) {
		log.Info("Validating if node has initialized")
		if err := node.IsInitialized(ctx); err != nil {
			return fmt.Errorf("node not initialized. Please use nodeadm init command to bootstrap a node. err: %v", err)
		}
	}

	log.Info("Loading configuration..", zap.String("configSource", c.configSource))
	nodeProvider, err := node.NewNodeProvider(c.configSource, log)
	if err != nil {
		return err
	}

	nodeProvider.PopulateNodeConfigDefaults()

	if err := nodeProvider.ValidateConfig(); err != nil {
		return err
	}

	credsProvider, err := creds.GetCredentialProviderFromNodeConfig(nodeProvider.GetNodeConfig())
	if err != nil {
		return err
	}

	// Validating credential provider. Upgrade does not allow changes to credential providers
	installedCredsProvider, err := creds.GetCredentialProviderFromInstalledArtifacts(installed.Artifacts)
	if err != nil {
		return err
	}
	if installedCredsProvider != credsProvider {
		return fmt.Errorf("upgrade does not support changing credential providers. Please uninstall and install with new credential provider")
	}

	log.Info("Validating Kubernetes version", zap.Reflect("kubernetes version", c.kubernetesVersion))
	// Create a Source for all AWS managed artifacts.
	awsSource, err := aws.GetLatestSource(ctx, c.kubernetesVersion)
	if err != nil {
		return err
	}
	log.Info("Using Kubernetes version", zap.Reflect("kubernetes version", awsSource.Eks.Version))

	log.Info("Creating daemon manager..")
	daemonManager, err := daemon.NewDaemonManager()
	if err != nil {
		return err
	}
	defer daemonManager.Close()

	if installed.Artifacts.Kubelet {
		kubeletStatus, err := daemonManager.GetDaemonStatus(kubelet.KubeletDaemonName)
		if err != nil {
			return err
		}
		if kubeletStatus == daemon.DaemonStatusRunning {
			if !slices.Contains(c.skipPhases, skipPodPreflightCheck) {
				log.Info("Validating if node has been drained...")
				if err := node.IsDrained(ctx); err != nil {
					return fmt.Errorf("only static pods and pods controlled by daemon-sets can be running on the node. Please move pods " +
						"to different node or use --skip pod-validation")
				}
			}
			if !slices.Contains(c.skipPhases, skipNodePreflightCheck) {
				log.Info("Validating if node has been marked unschedulable...")
				if err := node.IsUnscheduled(ctx); err != nil {
					return fmt.Errorf("please drain or cordon node to mark it unschedulable or use --skip node-validation. %v", err)
				}
			}
		}
	}

	log.Info("Creating package manager...")
	containerdSource := containerd.GetContainerdSource(installed.Artifacts.Containerd)
	log.Info("Configuring package manager with", zap.Reflect("containerd source", string(containerdSource)))
	packageManager, err := packagemanager.New(containerdSource, log)
	if err != nil {
		return err
	}

	uninstaller := &flows.Uninstaller{
		Artifacts:      installed.Artifacts,
		DaemonManager:  daemonManager,
		PackageManager: packageManager,
		SSMUninstall:   ssm.Uninstall,
		Logger:         log,
		CNIUninstall:   cni.NoOp,
	}

	installer := &flows.Installer{
		AwsSource:          awsSource,
		ContainerdSource:   containerdSource,
		PackageManager:     packageManager,
		CredentialProvider: credsProvider,
		Logger:             log,
	}

	initer := &flows.Initer{
		NodeProvider: nodeProvider,
		SkipPhases:   c.skipPhases,
		Logger:       log,
	}

	upgrader := &flows.Upgrader{
		Uninstaller: uninstaller,
		Installer:   installer,
		Initer:      initer,
	}

	return upgrader.Run(ctx)
}
