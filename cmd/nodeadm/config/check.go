package config

import (
	"github.com/integrii/flaggy"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/internal/configprovider"
	"github.com/aws/eks-hybrid/internal/node"
)

type fileCmd struct {
	cmd          *flaggy.Subcommand
	configSource string
}

func NewCheckCommand() cli.Command {
	file := fileCmd{}
	file.cmd = flaggy.NewSubcommand("check")
	file.cmd.Description = "Verify configuration"
	file.cmd.String(&file.configSource, "c", "config-source", "Source of node configuration. The format is a URI with supported schemes: [file, imds].")
	return &file
}

func (c *fileCmd) Flaggy() *flaggy.Subcommand {
	return c.cmd
}

func (c *fileCmd) Run(log *zap.Logger, opts *cli.GlobalOptions) error {
	log.Info("Checking configuration", zap.String("source", c.configSource))
	provider, err := configprovider.BuildConfigProvider(c.configSource)
	if err != nil {
		return err
	}
	_, err = provider.Provide()
	if err != nil {
		return err
	}

	nodeProvider, err := node.NewNodeProvider(c.configSource, []string{}, log)
	if err != nil {
		return err
	}

	if err := nodeProvider.ValidateConfig(); err != nil {
		return err
	}

	log.Info("Configuration is valid")
	return nil
}
