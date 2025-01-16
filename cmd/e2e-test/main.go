package main

import (
	"github.com/integrii/flaggy"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/cmd/e2e-test/cleanup"
	"github.com/aws/eks-hybrid/cmd/e2e-test/setup"
	"github.com/aws/eks-hybrid/cmd/e2e-test/ssh"
	"github.com/aws/eks-hybrid/internal/cli"
)

func main() {
	flaggy.SetName("e2e-test")
	flaggy.SetDescription("Manage the lifecycle of EKS clusters for E2E tests")

	cmds := []cli.Command{
		setup.NewCommand(),
		cleanup.NewCommand(),
		ssh.NewCommand(),
	}

	for _, cmd := range cmds {
		flaggy.AttachSubcommand(cmd.Flaggy(), 1)
	}

	flaggy.Parse()

	opts := cli.NewGlobalOptions()
	log := cli.NewLogger(opts)

	for _, cmd := range cmds {
		if cmd.Flaggy().Used {
			err := cmd.Run(log, opts)
			if err != nil {
				log.Fatal("Command failed", zap.Error(err))
			}
			return
		}
	}
	flaggy.ShowHelpAndExit("No command specified")
}
