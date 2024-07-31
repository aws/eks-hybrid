package validate

import (
	"fmt"

	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/internal/validation/logger"
	"github.com/aws/eks-hybrid/internal/validation/validator"
	"github.com/integrii/flaggy"
	"go.uber.org/zap"
)

func NewCommand() cli.Command {
	validate := validateCmd{}
	validate.cmd = flaggy.NewSubcommand("validate")
	validate.cmd.Description = "Validate the node can join an EKS cluster"
	return &validate
}

type validateCmd struct {
	cmd *flaggy.Subcommand
}

func (c *validateCmd) Flaggy() *flaggy.Subcommand {
	return c.cmd
}

func (c *validateCmd) Run(log *zap.Logger, opts *cli.GlobalOptions) error {
	runner := validator.NewRunner()

	runner.Register(
		validator.DefaultNumCPU(),
		validator.DefaultSysMem(),
		validator.DefaultDiskSize(),
		validator.DefaultNodeOS(),
		validator.DefaultArch(),
		validator.DefaultSysD(),
	)

	logger.Init()

	errs := runner.Run()
	if errs != nil {
		errorMsg := fmt.Sprintf("Following validations failed with warnings or error: %v ", errs.Error())
		logger.Info(errorMsg)
		return fmt.Errorf("validate command failed with warnings or error")
	}
	return nil
}
