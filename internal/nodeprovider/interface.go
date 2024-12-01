package nodeprovider

import (
	"context"

	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/configenricher"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/system"
)

// NodeProvider is an interface that defines functions that a nodeProvider should implement
type NodeProvider interface {
	// GetNodeConfig gets the node config with which the node will be inited
	GetNodeConfig() *api.NodeConfig

	// PopulateNodeConfigDefaults populates the node config with default values.
	// This doesn't require having aws Credentials or accessing any external services.
	PopulateNodeConfigDefaults()

	// ValidateConfig validates the node config with appropriate validations for the provider
	ValidateConfig() error

	// PreProcessDaemon runs a pre-init hook function if required by node provider. This could be SSM registration
	// for hybrid nodes
	PreProcessDaemon(ctx context.Context) error

	// GetDaemons returns daemons to be run for the node provider
	GetDaemons() ([]daemon.Daemon, error)

	// GetAspects returns the aspects to be configured for node provider
	GetAspects() []system.SystemAspect

	// Logger defines the logger for the node provider
	Logger() *zap.Logger

	// Cleanup runs post init cleanup if any are required by node provider.
	Cleanup() error

	configenricher.ConfigEnricher
	aws.Config
}
