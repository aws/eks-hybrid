package uninstall

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/integrii/flaggy"
	"go.uber.org/zap"
	"k8s.io/utils/strings/slices"

	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/internal/cni"
	"github.com/aws/eks-hybrid/internal/containerd"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/iamauthenticator"
	"github.com/aws/eks-hybrid/internal/iamrolesanywhere"
	"github.com/aws/eks-hybrid/internal/imagecredentialprovider"
	"github.com/aws/eks-hybrid/internal/iptables"
	"github.com/aws/eks-hybrid/internal/kubectl"
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

func NewCommand() cli.Command {
	cmd := command{
		downloadTimeout: 5 * time.Minute,
	}

	fc := flaggy.NewSubcommand("uninstall")
	fc.Description = "Uninstall components installed using the install sub-command"
	fc.StringSlice(&cmd.skipPhases, "s", "skip", "phases of uninstall you want to skip")
	fc.Duration(&cmd.downloadTimeout, "dt", "download-timeout", "Timeout for downloading artifacts. Input follows duration format. Example: 1h23s. Default is 5m.")
	cmd.flaggy = fc

	return &cmd
}

type command struct {
	flaggy          *flaggy.Subcommand
	skipPhases      []string
	downloadTimeout time.Duration
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

	ctx, cancel := context.WithTimeout(context.Background(), c.downloadTimeout)
	defer cancel()

	if !slices.Contains(c.skipPhases, skipPodPreflightCheck) {
		log.Info("Validating if pods have been drained...")
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

	artifacts := installed.Artifacts
	log.Info("Creating package manager...")
	containerdSource := containerd.GetContainerdSource(artifacts.Containerd)
	log.Info("Configuring package manager with", zap.Reflect("containerd source", string(containerdSource)))
	packageManager, err := packagemanager.New(containerdSource, log)
	if err != nil {
		return err
	}

	if err := UninstallBinaries(ctx, artifacts, packageManager, log); err != nil {
		return err
	}

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
	if artifacts.IamRolesAnywhere {
		log.Info("Removing aws_signing_helper_update daemon...")
		if status, err := daemonManager.GetDaemonStatus(iamrolesanywhere.DaemonName); err == nil || status != daemon.DaemonStatusUnknown {
			if err = daemonManager.StopDaemon(iamrolesanywhere.DaemonName); err != nil {
				log.Info("Stopping aws_signing_helper_update daemon...")
				return err
			}
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

	log.Info("Finished uninstallation tasks...")

	return tracker.Clear()
}

func UninstallBinaries(ctx context.Context, artifacts *tracker.InstalledArtifacts, packageManager *packagemanager.DistroPackageManger, log *zap.Logger) error {
	if artifacts.Kubectl {
		log.Info("Uninstalling kubectl...")
		if err := kubectl.Uninstall(); err != nil {
			return err
		}
	}
	if artifacts.CniPlugins {
		log.Info("Uninstalling cni-plugins...")
		if err := cni.Uninstall(); err != nil {
			return err
		}
	}
	if artifacts.IamAuthenticator {
		log.Info("Uninstalling IAM authenticator...")
		if err := iamauthenticator.Uninstall(); err != nil {
			return err
		}
	}
	if artifacts.IamRolesAnywhere {
		log.Info("Uninstalling AWS signing helper...")
		if err := iamrolesanywhere.Uninstall(); err != nil {
			return err
		}
	}
	if artifacts.ImageCredentialProvider {
		log.Info("Uninstalling image credential provider...")
		if err := imagecredentialprovider.Uninstall(); err != nil {
			return err
		}
	}
	if artifacts.Iptables {
		log.Info("Uninstalling iptables...")
		if err := iptables.Uninstall(ctx, packageManager); err != nil {
			return err
		}
	}
	return nil
}
