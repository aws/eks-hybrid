package node

import (
	"github.com/integrii/flaggy"

	"github.com/aws/eks-hybrid/internal/cli"
)

type Command struct {
	*flaggy.Subcommand
	subcommands []cli.Command
}

func (n Command) Flaggy() *flaggy.Subcommand {
	return n.Subcommand
}

func (n Command) Commands() []cli.Command {
	return n.subcommands
}

func NewCommand() Command {
	node := flaggy.NewSubcommand("node")
	node.Description = "Manage Hybrid Nodes"
	create := NewCreateCommand()
	node.AttachSubcommand(create.Flaggy(), 1)
	delete := NewDeleteCommand()
	node.AttachSubcommand(delete.Flaggy(), 1)

	return Command{
		Subcommand:  node,
		subcommands: []cli.Command{create, delete},
	}
}
