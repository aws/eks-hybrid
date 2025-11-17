package init

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/integrii/flaggy"
	"go.uber.org/zap"
	"k8s.io/utils/strings/slices"

	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/internal/containerd"
	"github.com/aws/eks-hybrid/internal/flows"
	"github.com/aws/eks-hybrid/internal/logger"
	"github.com/aws/eks-hybrid/internal/node"
	nodevalidator "github.com/aws/eks-hybrid/internal/nodevalidator"
	"github.com/aws/eks-hybrid/internal/system"
	"github.com/aws/eks-hybrid/internal/tracker"
)

const (
	installValidation        = "install-validation"
	cniPortCheckValidation   = "cni-validation"
	calicoVxLanPort          = "4789"
	ciliumVxLanPort          = "8472"
	vxLanProtocol            = "udp"
	defaultValidationTimeout = "10m"
)

// Phases returns the list of valid phases that can be skipped in init command
func Phases() []string {
	return []string{
		"install-validation",
		"cni-validation",
		"node-ip-validation",
		"credentials-validation",
		"kubelet-cert-validation",
		"ssm-api-network-validation",
		"iam-ra-api-network-validation",
		"aws-auth-validation",
		"k8s-endpoint-network-validation",
		"k8s-authentication-validation",
		"kubelet-version-skew-validation",
		"api-server-endpoint-resolution-validation",
		"proxy-validation",
		"node-inactive-validation",
		"preprocess",
		"config",
		"run",
	}
}

const initHelpText = `Examples:
  # Initialize using configuration file
  nodeadm init --config-source file://nodeConfig.yaml

  # Initialize with custom validation timeout
  # validation timeout: 15 mins
  nodeadm init --config-source file://nodeConfig.yaml --validation-timeout 15m

  # disable validation
  nodeadm init --config-source file://nodeConfig.yaml --validation-timeout 0s

Documentation:
  https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-nodeadm.html#_init`

func NewInitCommand() cli.Command {
	init := initCmd{
		validationTimeout: defaultValidationTimeout,
	}
	init.cmd = flaggy.NewSubcommand("init")
	init.cmd.String(&init.configSource, "c", "config-source", "Source of node configuration. The format is a URI with supported schemes: [file, imds].")
	init.cmd.StringSlice(&init.daemons, "d", "daemon", "Specify one or more of `containerd` and `kubelet`. This is intended for testing and should not be used in a production environment.")
	init.cmd.StringSlice(&init.skipPhases, "s", "skip", fmt.Sprintf("Phases of the bootstrap to skip. Allowed values: [%s].", strings.Join(Phases(), ", ")))
	init.cmd.String(&init.validationTimeout, "v", "validation-timeout", "Validation timeout duration (e.g., 30s, 10m, 1h). This is the timeout for node validation with the Kubernetes cluster after initialization. Set to '0s' to disable validation. Default: 10m")
	init.cmd.Description = "Initialize this instance as a node in an EKS cluster"
	init.cmd.AdditionalHelpAppend = initHelpText
	return &init
}

type initCmd struct {
	cmd               *flaggy.Subcommand
	configSource      string
	skipPhases        []string
	daemons           []string
	validationTimeout string
}

func (c *initCmd) Flaggy() *flaggy.Subcommand {
	return c.cmd
}

func (c *initCmd) Run(log *zap.Logger, opts *cli.GlobalOptions) error {
	ctx := context.Background()
	ctx = logger.NewContext(ctx, log)

	log.Info("Checking user is root...")
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

	// Check if either of cilium or calico vxlan port are open
	if !slices.Contains(c.skipPhases, cniPortCheckValidation) {
		log.Info("Validating firewall ports for cilium and calico")
		if err := validateFirewallOpenPorts(); err != nil {
			return fmt.Errorf("Cilium (%s/%s) or Calico (%s/%s) VxLan ports are not open on the host. If you are not using VxLan, this validation can by bypassed with --skip %s",
				ciliumVxLanPort, vxLanProtocol, calicoVxLanPort, vxLanProtocol, cniPortCheckValidation)
		}
	}

	nodeProvider, err := node.NewNodeProvider(c.configSource, c.skipPhases, log)
	if err != nil {
		return err
	}

	initer := &flows.Initer{
		NodeProvider: nodeProvider,
		SkipPhases:   c.skipPhases,
		Logger:       log,
	}

	err = initer.Run(ctx)
	if err != nil {
		return err
	}

	// Parse and validate validation-timeout duration for node validation with the Kubernetes cluster
	var validationTimeout time.Duration
	validationTimeout, err = time.ParseDuration(c.validationTimeout)
	if err != nil {
		return fmt.Errorf("invalid validation-timeout duration '%s': expected format like '15m', '600s', '1h30m', got error: %w", c.validationTimeout, err)
	}

	if validationTimeout == 0 {
		log.Info("Node initialization finished. Post-initialization validation to check node is active has been skipped since the validation-timeout is disabled")
		return nil
	}

	// Performing post-initialization validation to check the node is active
	ctx, cancel := context.WithTimeout(ctx, validationTimeout)
	defer cancel()

	log.Info("Node initialization finished. Running post-initialization validation to check node status in cluster", zap.Duration("timeout", validationTimeout))
	if err := nodevalidator.ExecuteActiveNodeValidator(ctx, log); err != nil {
		log.Warn("Post-initialization validation encountered issues but init completed successfully", zap.Error(err))
	} else {
		log.Info("Post-initialization validation successful. Node is ready in the Kubernetes cluster")
	}

	return nil
}

func validateFirewallOpenPorts() error {
	firewallManager := system.NewFirewallManager()
	enabled, err := firewallManager.IsEnabled()
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	if err := firewallManager.FlushRules(); err != nil {
		return err
	}
	ciliumVxlanPortOpen, err := firewallManager.IsPortOpen(ciliumVxLanPort, vxLanProtocol)
	if err != nil {
		return err
	}
	calicoVxlanPortOpen, err := firewallManager.IsPortOpen(calicoVxLanPort, vxLanProtocol)
	if err != nil {
		return err
	}

	if !ciliumVxlanPortOpen && !calicoVxlanPortOpen {
		return fmt.Errorf("both cilium and calico vxlan ports are closed")
	}
	return nil
}
