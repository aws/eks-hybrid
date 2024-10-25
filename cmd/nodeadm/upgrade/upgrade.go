package upgrade

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/integrii/flaggy"
	"go.uber.org/zap"
	"k8s.io/utils/strings/slices"

	initialize "github.com/aws/eks-hybrid/cmd/nodeadm/init"
	"github.com/aws/eks-hybrid/cmd/nodeadm/install"
	"github.com/aws/eks-hybrid/cmd/nodeadm/uninstall"
	"github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/internal/containerd"
	"github.com/aws/eks-hybrid/internal/creds"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/node"
	"github.com/aws/eks-hybrid/internal/packagemanager"
	"github.com/aws/eks-hybrid/internal/ssm"
	"github.com/aws/eks-hybrid/internal/tracker"
)

const (
	skipPodPreflightCheck  = "pod-validation"
	skipNodePreflightCheck = "node-validation"
)

func NewUpgradeCommand() cli.Command {
	cmd := command{
		downloadTimeout: 10 * time.Minute,
	}

	fc := flaggy.NewSubcommand("upgrade")
	fc.Description = "Upgrade components installed using the install sub-command"
	fc.AddPositionalValue(&cmd.kubernetesVersion, "KUBERNETES_VERSION", 1, true, "The major[.minor[.patch]] version of Kubernetes to install")
	fc.StringSlice(&cmd.skipPhases, "s", "skip", "phases of the upgrade you want to skip")
	fc.Duration(&cmd.downloadTimeout, "dt", "download-timeout", "Timeout for downloading artifacts. Input follows duration format. Example: 1h23s")
	cmd.flaggy = fc
	return &cmd
}

type command struct {
	flaggy            *flaggy.Subcommand
	skipPhases        []string
	kubernetesVersion string
	downloadTimeout   time.Duration
}

func (c *command) Flaggy() *flaggy.Subcommand {
	return c.flaggy
}

func (c *command) Run(log *zap.Logger, opts *cli.GlobalOptions) error {
	root, err := cli.IsRunningAsRoot()
	if err != nil {
		return err
	}
	if !root {
		return cli.ErrMustRunAsRoot
	}

	log.Info("Loading installed components")
	installed, err := tracker.GetInstalledArtifacts()
	if err != nil && os.IsNotExist(err) {
		log.Info("No nodeadm components installed. Please use nodeadm install and nodeadm init commands to bootstrap a node")
		return nil
	} else if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.downloadTimeout)
	defer cancel()

	if !slices.Contains(c.skipPhases, skipPodPreflightCheck) {
		log.Info("Validating if pods have been drained...")
		if err := node.IsDrained(ctx); err != nil {
			return err
		}
	}
	if !slices.Contains(c.skipPhases, skipNodePreflightCheck) {
		log.Info("Validating if node has been marked unschedulable...")
		if err := node.IsUnscheduled(ctx); err != nil {
			return err
		}
	}

	log.Info("Loading configuration..", zap.String("configSource", opts.ConfigSource))
	nodeProvider, err := node.NewNodeProvider(opts.ConfigSource, log)
	if err != nil {
		return err
	}

	// Ensure hybrid configuration and enrich
	if err := nodeProvider.ValidateConfig(); err != nil {
		return err
	}
	if err := nodeProvider.Enrich(); err != nil {
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

	artifacts := installed.Artifacts
	log.Info("Creating package manager...")
	containerdSource := containerd.GetContainerdSource(artifacts.Containerd)
	log.Info("Configuring package manager with", zap.Reflect("containerd source", string(containerdSource)))
	packageManager, err := packagemanager.New(containerdSource, log)
	if err != nil {
		return err
	}

	if err := uninstall.UninstallBinaries(ctx, artifacts, packageManager, log); err != nil {
		return err
	}

	log.Info("Creating daemon manager..")
	daemonManager, err := daemon.NewDaemonManager()
	if err != nil {
		return err
	}
	defer daemonManager.Close()

	if artifacts.Kubelet {
		log.Info("Uninstalling kubelet...")
		if err := daemonManager.StopDaemon(kubelet.KubeletDaemonName); err != nil {
			return err
		}
		if err := kubelet.Uninstall(); err != nil {
			return err
		}
	}
	if artifacts.Ssm {
		log.Info("Uninstalling and de-registering SSM agent...")
		if err := daemonManager.StopDaemon(ssm.SsmDaemonName); err != nil {
			return err
		}
		if err := ssm.Uninstall(ctx, packageManager); err != nil {
			return err
		}
	}
	if artifacts.Containerd != string(containerd.ContainerdSourceNone) {
		log.Info("Uninstalling containerd...")
		if err := daemonManager.StopDaemon(containerd.ContainerdDaemonName); err != nil {
			return err
		}
		if err := containerd.Uninstall(ctx, packageManager); err != nil {
			return err
		}
	}

	if err := tracker.Clear(); err != nil {
		return err
	}

	installer := &install.Config{
		AwsSource:          awsSource,
		ContainerdSource:   containerdSource,
		CredentialProvider: credsProvider,
		Log:                log,
		DownloadTimeout:    c.downloadTimeout,
	}

	// Installing new version of artifacts
	if err := installer.Install(ctx); err != nil {
		return err
	}

	return initialize.Init(nodeProvider, c.skipPhases)
}
