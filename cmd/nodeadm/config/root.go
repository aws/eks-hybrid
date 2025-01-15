package config

import (
	"github.com/aws/eks-hybrid/internal/cli"
)

const configHelpText = `Examples:
  # Check configuration file
  nodeadm config check --config-source file:///root/nodeConfig.yaml
  
Documentation:
  https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-nodeadm.html`

func NewConfigCommand() cli.Command {
	container := cli.NewCommandContainer("config", "Manage configuration")
	container.Flaggy().AdditionalHelpAppend = configHelpText
	container.AddCommand(NewCheckCommand())
	return container.AsCommand()
}
