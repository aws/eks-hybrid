package debug

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/smithy-go/logging"
	"github.com/integrii/flaggy"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/aws/sts"
	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/internal/configprovider"
	"github.com/aws/eks-hybrid/internal/creds"
	"github.com/aws/eks-hybrid/internal/errors"
	"github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/internal/logger"
	"github.com/aws/eks-hybrid/internal/node"
	"github.com/aws/eks-hybrid/internal/validation"
)

const debugHelpText = `Examples:
  # Debug using a local config file
  nodeadm debug --config-source file://nodeConfig.yaml

Documentation:
  https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-nodeadm.html`

func NewCommand() cli.Command {
	debug := debug{}
	debug.cmd = flaggy.NewSubcommand("debug")
	debug.cmd.String(&debug.nodeConfigSource, "c", "config-source", "Source of node configuration. The format is a URI with supported schemes: [file, imds].")
	debug.cmd.Description = "Debug the node registration process"
	debug.cmd.AdditionalHelpPrepend = debugHelpText
	return &debug
}

type debug struct {
	cmd              *flaggy.Subcommand
	nodeConfigSource string
}

func (c *debug) Flaggy() *flaggy.Subcommand {
	return c.cmd
}

func (c *debug) Run(log *zap.Logger, opts *cli.GlobalOptions) error {
	ctx := context.Background()
	ctx = logger.NewContext(ctx, log)

	if c.nodeConfigSource == "" {
		flaggy.ShowHelpAndExit("--config-source is a required flag. The format is a URI with supported schemes: [file, imds]." +
			" For example on hybrid nodes --config-source file://nodeConfig.yaml")
	}

	provider, err := configprovider.BuildConfigProvider(c.nodeConfigSource)
	if err != nil {
		return err
	}
	nodeConfig, err := provider.Provide()
	if err != nil {
		return err
	}

	awsConfig, err := creds.ReadConfig(ctx, nodeConfig, config.WithLogger(logging.Nop{}))
	if err != nil {
		return err
	}

	printer := validation.NewPrinterWithStdCapture("stderr")
	if err := printer.Init(); err != nil {
		return err
	}
	defer printer.Close()

	// We want to capture stderr and let the printer control it.
	// When the AWS SDK calls the credentials_process for IAM Roles Anywhere
	// or when the k8s client-go calls the aws-iam-authenticator binary, those processes
	// output to stderr and those logs are not returned to the caller in the go error.
	// In order to not have interfere with the printer logs or get lost,
	// we just override the global stderr and restore after we are done running validations.
	originalStderr := os.Stderr
	defer func() { os.Stderr = originalStderr }()
	os.Stderr = printer.File

	runner := validation.NewRunner[*api.NodeConfig](printer)
	apiServerValidator := node.NewAPIServerValidator()

	runner.Register(creds.Validations(awsConfig, nodeConfig)...)
	runner.Register(
		validation.New("aws-auth", sts.NewAuthenticationValidator(awsConfig).Run),
		runner.UntilError(
			validation.New("k8s-endpoint-network", kubernetes.NewAccessValidator(awsConfig).Run),
			validation.New("k8s-authentication", apiServerValidator.MakeAuthenticatedRequest),
			validation.New("k8s-identity", apiServerValidator.CheckIdentity),
			validation.New("k8s-vpc-network", apiServerValidator.CheckVPCEndpointAccess),
		),
	)

	if err := runner.Sequentially(ctx, nodeConfig); err != nil {
		fmt.Println("")
		fmt.Println("Issues found during validation. Please follow the remediation advice above.")
		// Errors are already presented by the printer
		// so we just need to exit with a non-zero status code
		return errors.NewSilent(err)
	}

	return nil
}
