package upgrade

import (
	"context"
	"os"

	"github.com/integrii/flaggy"
	"go.uber.org/zap"
	"k8s.io/utils/strings/slices"

	initialize "github.com/aws/eks-hybrid/cmd/nodeadm/init"
	"github.com/aws/eks-hybrid/cmd/nodeadm/install"
	"github.com/aws/eks-hybrid/cmd/nodeadm/uninstall"
	"github.com/aws/eks-hybrid/internal/aws/eks"
	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/internal/containerd"
	"github.com/aws/eks-hybrid/internal/creds"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/node"
	"github.com/aws/eks-hybrid/internal/packagemanager"
	"github.com/aws/eks-hybrid/internal/pods"
	"github.com/aws/eks-hybrid/internal/ssm"
	"github.com/aws/eks-hybrid/internal/tracker"
)

const (
	skipPodPreflightCheck  = "pod-validation"
	skipNodePreflightCheck = "node-validation"
)

func NewUpgradeCommand() cli.Command {
	cmd := command{}

	fc := flaggy.NewSubcommand("upgrade")
	fc.Description = "Upgrade components installed using the install sub-command"
	fc.AddPositionalValue(&cmd.kubernetesVersion, "KUBERNETES_VERSION", 1, true, "The major[.minor[.patch]] version of Kubernetes to install")
	fc.StringSlice(&cmd.skipPhases, "s", "skip", "phases of the upgrade you want to skip")
	cmd.flaggy = fc
	return &cmd
}

type command struct {
	flaggy            *flaggy.Subcommand
	skipPhases        []string
	kubernetesVersion string
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

	if !slices.Contains(c.skipPhases, skipPodPreflightCheck) {
		log.Info("Validating running pods on the node...")
		if err := pods.ValidateRunningPodsForUninstall(); err != nil {
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

	ctx := context.Background()
	log.Info("Validating Kubernetes version", zap.Reflect("kubernetes version", c.kubernetesVersion))
	// Create a Source for all EKS managed artifacts.
	release, err := eks.FindLatestRelease(ctx, c.kubernetesVersion)
	if err != nil {
		return err
	}
	log.Info("Using Kubernetes version", zap.Reflect("kubernetes version", release.Version))

	log.Info("Loading installed components")
	installed, err := tracker.GetInstalledArtifacts()
	if err != nil && os.IsNotExist(err) {
		log.Info("No nodeadm components installed. Please use nodeadm install and nodeadm init commands to bootstrap a node")
		return nil
	} else if err != nil {
		return err
	}

	artifacts := installed.Artifacts
	log.Info("Creating package manager...")
	containerdSource := containerd.GetContainerdSource(artifacts.Containerd)
	log.Info("Configuring package manager with", zap.Reflect("containerd source", string(containerdSource)))
	packageManager, err := packagemanager.New(containerdSource, log)
	if err != nil {
		return err
	}

	if err := uninstall.UninstallBinaries(artifacts, packageManager, log); err != nil {
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
		if err := ssm.Uninstall(packageManager); err != nil {
			return err
		}
	}
	if artifacts.Containerd != string(containerd.ContainerdSourceNone) {
		log.Info("Uninstalling containerd...")
		if err := daemonManager.StopDaemon(containerd.ContainerdDaemonName); err != nil {
			return err
		}
		if err := containerd.Uninstall(packageManager); err != nil {
			return err
		}
	}

	if err := tracker.Clear(); err != nil {
		return err
	}

	credsProvider, err := creds.GetCredentialProviderFromNodeConfig(nodeProvider.GetNodeConfig())
	if err != nil {
		return err
	}

	// Installing new version of artifacts
	if err := install.Install(ctx, release, credsProvider, containerdSource, log); err != nil {
		return err
	}

	return initialize.Init(nodeProvider, c.skipPhases)
}
