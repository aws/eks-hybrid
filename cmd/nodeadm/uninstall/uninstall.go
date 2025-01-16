package uninstall

import (
	"context"
	"fmt"
	"os"

	"github.com/integrii/flaggy"
	"go.uber.org/zap"
	"k8s.io/utils/strings/slices"

	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/internal/cni"
	"github.com/aws/eks-hybrid/internal/containerd"
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
)

const uninstallHelpText = `Examples:
  # Uninstall all components
  nodeadm uninstall

  # Uninstall all components and skip pod-validation and node-validation pre-flight validation
  nodeadm uninstall --skip node-validation,pod-validation

Documentation:
  https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-nodeadm.html`

func NewCommand() cli.Command {
	cmd := command{}

	fc := flaggy.NewSubcommand("uninstall")
	fc.Description = "Uninstall components installed using the install sub-command"
	fc.AdditionalHelpAppend = uninstallHelpText
	fc.StringSlice(&cmd.skipPhases, "s", "skip", "Phases of uninstall you want to skip.")
	cmd.flaggy = fc

	return &cmd
}

type command struct {
	flaggy     *flaggy.Subcommand
	skipPhases []string
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

	log.Info("Loading installed components")
	installed, err := tracker.GetInstalledArtifacts()
	if err != nil && os.IsNotExist(err) {
		log.Info("Nodeadm components are already uninstalled")
		return nil
	} else if err != nil {
		return err
	}

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
		SSMUninstall:   ssm.DeregisterAndUninstall,
		Logger:         log,
		CNIUninstall:   cni.Uninstall,
	}

	return uninstaller.Run(ctx)
}
