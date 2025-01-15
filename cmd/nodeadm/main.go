package main

import (
	"os"

	"github.com/integrii/flaggy"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/cmd/nodeadm/config"
	"github.com/aws/eks-hybrid/cmd/nodeadm/debug"
	initcmd "github.com/aws/eks-hybrid/cmd/nodeadm/init"
	"github.com/aws/eks-hybrid/cmd/nodeadm/install"
	"github.com/aws/eks-hybrid/cmd/nodeadm/uninstall"
	"github.com/aws/eks-hybrid/cmd/nodeadm/upgrade"
	"github.com/aws/eks-hybrid/cmd/nodeadm/version"
	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/internal/errors"
)

func main() {
	flaggy.SetName("nodeadm")
	flaggy.SetDescription("From zero to Node faster than you can say Elastic Kubernetes Service")
	flaggy.SetVersion(version.GitVersion)
	flaggy.DefaultParser.AdditionalHelpPrepend = "http://github.com/aws/eks-hybrid"
	flaggy.DefaultParser.ShowHelpOnUnexpected = true
	flaggy.DefaultParser.SetHelpTemplate(cli.HelpTemplate)

	opts := cli.NewGlobalOptions()

	cmds := []cli.Command{
		config.NewConfigCommand(),
		initcmd.NewInitCommand(),
		install.NewCommand(),
		uninstall.NewCommand(),
		upgrade.NewUpgradeCommand(),
		debug.NewCommand(),
	}

	for _, cmd := range cmds {
		flaggy.AttachSubcommand(cmd.Flaggy(), 1)
	}
	flaggy.Parse()

	log := cli.NewLogger(opts)

	for _, cmd := range cmds {
		if cmd.Flaggy().Used {
			err := cmd.Run(log, opts)
			if err != nil {
				if errors.IsSilent(err) {
					os.Exit(1)
				}

				log.Fatal("Command failed", zap.Error(err))
			}
			return
		}
	}
	flaggy.ShowHelpAndExit("No command specified")
}
