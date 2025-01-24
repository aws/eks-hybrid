package init

import (
	"context"
	"fmt"
	"os"

	"github.com/integrii/flaggy"
	"go.uber.org/zap"
	"k8s.io/utils/strings/slices"

	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/internal/containerd"
	"github.com/aws/eks-hybrid/internal/flows"
	"github.com/aws/eks-hybrid/internal/logger"
	"github.com/aws/eks-hybrid/internal/node"
	"github.com/aws/eks-hybrid/internal/tracker"
)

const installValidation = "install-validation"

const initHelpText = `Examples:
  # Initialize using configuration file
  nodeadm init --config-source file://nodeConfig.yaml

Documentation:
  https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-nodeadm.html#_init`

func NewInitCommand() cli.Command {
	init := initCmd{}
	init.cmd = flaggy.NewSubcommand("init")
	init.cmd.String(&init.configSource, "c", "config-source", "Source of node configuration. The format is a URI with supported schemes: [file, imds].")
	init.cmd.StringSlice(&init.daemons, "d", "daemon", "Specify one or more of `containerd` and `kubelet`. This is intended for testing and should not be used in a production environment.")
	init.cmd.StringSlice(&init.skipPhases, "s", "skip", "Phases of the bootstrap to skip. Allowed values: [install-validation, preprocess, config, run].")
	init.cmd.Description = "Initialize this instance as a node in an EKS cluster"
	init.cmd.AdditionalHelpAppend = initHelpText
	return &init
}

type initCmd struct {
	cmd          *flaggy.Subcommand
	configSource string
	skipPhases   []string
	daemons      []string
}

func (c *initCmd) Flaggy() *flaggy.Subcommand {
	return c.cmd
}

func (c *initCmd) Run(log *zap.Logger, opts *cli.GlobalOptions) error {
	ctx := context.Background()
	ctx = logger.NewContext(ctx, log)

	log.Info("Checking user is root..")
	root, err := cli.IsRunningAsRoot()
	if err != nil {
		return err
	} else if !root {
		return cli.ErrMustRunAsRoot
	}

	if c.configSource == "" {
		flaggy.ShowHelpAndExit("--config-source is a required flag. The format is a URI with supported schemes: [file, imds]." +
			" For example on hybrid nodes --config-source file://nodeConfig.yaml")
	}

	if !slices.Contains(c.skipPhases, installValidation) {
		log.Info("Loading installed components")
		_, err = tracker.GetInstalledArtifacts()
		if err != nil && os.IsNotExist(err) {
			log.Info("Nodeadm components are not installed. Please run `nodeadm install` before running init")
			return nil
		} else if err != nil {
			return err
		}

		if err := containerd.ValidateSystemdUnitFile(); err != nil {
			return fmt.Errorf("a systemd unit file for containerd is required to init the node: %w", err)
		}
	}

	nodeProvider, err := node.NewNodeProvider(c.configSource, log)
	if err != nil {
		return err
	}

	initer := &flows.Initer{
		NodeProvider: nodeProvider,
		SkipPhases:   c.skipPhases,
		Logger:       log,
	}

	return initer.Run(ctx)
}
