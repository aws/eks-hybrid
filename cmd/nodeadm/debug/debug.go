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
	"github.com/aws/eks-hybrid/internal/aws/eks"
	"github.com/aws/eks-hybrid/internal/aws/sts"
	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/internal/configprovider"
	"github.com/aws/eks-hybrid/internal/creds"
	"github.com/aws/eks-hybrid/internal/errors"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/internal/logger"
	"github.com/aws/eks-hybrid/internal/network"
	"github.com/aws/eks-hybrid/internal/nodevalidator"
	"github.com/aws/eks-hybrid/internal/system"
	"github.com/aws/eks-hybrid/internal/validation"
)

const debugHelpText = `Examples:
  # Debug using a local config file
  nodeadm debug --config-source file://nodeConfig.yaml

Documentation:
  https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-nodeadm.html#_debug`

func NewCommand() cli.Command {
	debug := debug{}
	debug.cmd = flaggy.NewSubcommand("debug")
	debug.cmd.String(&debug.nodeConfigSource, "c", "config-source", "Source of node configuration. The format is a URI with supported schemes: [file, imds].")
	debug.cmd.Bool(&debug.noColor, "", "no-color", "If set, suppresses color output.")
	debug.cmd.Description = "Debug the node registration process"
	debug.cmd.AdditionalHelpPrepend = debugHelpText
	return &debug
}

type debug struct {
	cmd              *flaggy.Subcommand
	nodeConfigSource string
	noColor          bool
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

	awsConfig, err := creds.ReadConfigAsKubelet(ctx, nodeConfig, config.WithLogger(logging.Nop{}))
	if err != nil {
		return err
	}

	printer := validation.NewPrinterWithStdCapture("stderr", c.noColor)
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
	apiServerValidator := kubernetes.NewAPIServerValidator(kubelet.New())
	clusterProvider := kubernetes.NewClusterProvider(awsConfig)

	// Register validations that do not require cluster details first
	runner.Register(creds.Validations(awsConfig, nodeConfig)...)
	runner.Register(
		validation.New("ntp-sync", system.NewNTPValidator().Run),
		validation.New("swap", system.NewSwapValidator().Run),
		validation.New("ulimit", system.NewUlimitValidator().Run),
		validation.New("aws-auth", sts.NewAuthenticationValidator(awsConfig).Run),
		validation.New("proxy-config", network.NewProxyValidator().Run),
	)

	clusterDetail, err := clusterProvider.ReadClusterDetails(ctx, nodeConfig)
	if err != nil {
		// Only if reading the EKS fail is when we "start" a validation and signal it as failed.
		// Otherwise, there is no need to surface we are reading from the EKS API.
		err = validation.WithRemediation(err, "Ensure the Kubernetes API server endpoint is provided or "+
			"the node has access and permissions to call EKS DescribeCluster API.")
		printer.Starting(ctx, "cluster-details-retrieval", "Retrieving cluster details")
		printer.Done(ctx, "cluster-details-retrieval", err)
		return err
	}

	runner.Register(
		runner.UntilError(
			validation.New("k8s-endpoint-network", kubernetes.NewAccessValidator(clusterDetail).Run),
			validation.New("k8s-authentication", apiServerValidator.MakeAuthenticatedRequest),
			validation.New("k8s-identity", apiServerValidator.CheckIdentity),
			validation.New("k8s-vpc-network", apiServerValidator.CheckVPCEndpointAccess),
		),
		validation.New("k8s-certificate", kubernetes.NewKubeletCertificateValidator(clusterDetail).Run),
	)

	cluster, _ := eks.ReadCluster(ctx, awsConfig, nodeConfig)
	runner.Register(validation.New("network-interface", network.NewNetworkInterfaceValidator(network.WithCluster(cluster)).Run))

	runner.Register(validation.New("active-node-validation", nodevalidator.NewActiveNodeValidator().Run))

	if err := runner.Sequentially(ctx, nodeConfig); err != nil {
		fmt.Println("")
		fmt.Println("Issues found during validation. Please follow the remediation advice above.")
		// Errors are already presented by the printer
		// so we just need to exit with a non-zero status code
		return errors.NewSilent(err)
	}

	return nil
}
