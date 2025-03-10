package main

import (
	"github.com/integrii/flaggy"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/cmd/e2e-test/cleanup"
	"github.com/aws/eks-hybrid/cmd/e2e-test/node"
	"github.com/aws/eks-hybrid/cmd/e2e-test/setup"
	"github.com/aws/eks-hybrid/cmd/e2e-test/ssh"
	"github.com/aws/eks-hybrid/cmd/e2e-test/sweeper"
	"github.com/aws/eks-hybrid/internal/cli"
)

type command interface {
	Flaggy() *flaggy.Subcommand
	Commands() []cli.Command
}

func main() {
	flaggy.SetName("e2e-test")
	flaggy.SetDescription("Manage the lifecycle of EKS clusters for E2E tests")

	cmds := []command{
		setup.NewCommand(),
		cleanup.NewCommand(),
		ssh.NewCommand(),
		node.NewCommand(),
		sweeper.NewSweeperCommand(),
	}

	for _, cmd := range cmds {
		flaggy.AttachSubcommand(cmd.Flaggy(), 1)
	}

	flaggy.Parse()

	opts := cli.NewGlobalOptions()
	log := cli.NewLogger(opts)

	for _, subCmd := range cmds {
		for _, cmd := range subCmd.Commands() {
			if cmd.Flaggy().Used {
				err := cmd.Run(log, opts)
				if err != nil {
					log.Fatal("Command failed", zap.Error(err))
				}
				return
			}
		}
	}
	flaggy.ShowHelpAndExit("No command specified")
}
