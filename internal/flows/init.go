package flows

import (
	"context"

	"go.uber.org/zap"
	"k8s.io/utils/strings/slices"

	"github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/configenricher"
	"github.com/aws/eks-hybrid/internal/nodeprovider"
)

const (
	preprocessPhase = "preprocess"
	configPhase     = "config"
	runPhase        = "run"
)

type Initer struct {
	NodeProvider     nodeprovider.NodeProvider
	SkipPhases       []string
	Logger           *zap.Logger
	ManifestOverride string
	PrivateMode      bool
}

func (i *Initer) Run(ctx context.Context) error {
	i.NodeProvider.PopulateNodeConfigDefaults()

	if err := i.NodeProvider.ValidateConfig(); err != nil {
		return err
	}

	i.Logger.Info("Configuring Aws...")
	if err := i.NodeProvider.ConfigureAws(ctx); err != nil {
		return err
	}

	var regionConfig *aws.RegionData
	var err error

	// Get region config for ECR registry lookup
	region := i.NodeProvider.GetNodeConfig().Spec.Cluster.Region
	if i.PrivateMode {
		regionConfig, err = aws.GetRegionConfigFromManifest(ctx, region, i.ManifestOverride)
		if err != nil {
			i.Logger.Warn("Failed to get region config from local file", zap.Error(err))
		}
	} else {
		regionConfig, err = aws.GetRegionConfig(ctx, region)
		if err != nil {
			i.Logger.Warn("Failed to get region config", zap.Error(err))
		}
	}

	// Enrich with region config if available
	if regionConfig != nil {
		if err := i.NodeProvider.Enrich(ctx, configenricher.WithRegionConfig(regionConfig)); err != nil {
			return err
		}
	} else {
		if err := i.NodeProvider.Enrich(ctx); err != nil {
			return err
		}
	}

	if err := i.NodeProvider.Validate(ctx); err != nil {
		return err
	}

	aspects := i.NodeProvider.GetAspects()
	i.Logger.Info("Setting up system aspects...")
	for _, aspect := range aspects {
		nameField := zap.String("name", aspect.Name())
		i.Logger.Info("Setting up system aspect...", nameField)
		if err := aspect.Setup(); err != nil {
			return err
		}
		i.Logger.Info("Finished setting up system aspect", nameField)
	}

	if err := initDaemons(ctx, i.NodeProvider, i.SkipPhases, i.Logger); err != nil {
		return err
	}

	return i.NodeProvider.Cleanup()
}

func initDaemons(ctx context.Context, nodeProvider nodeprovider.NodeProvider, skipPhases []string, logger *zap.Logger) error {
	if !slices.Contains(skipPhases, preprocessPhase) {
		logger.Info("Configuring Pre-process daemons...")
		if err := nodeProvider.PreProcessDaemon(ctx); err != nil {
			return err
		}
	}

	daemons, err := nodeProvider.GetDaemons()
	if err != nil {
		return err
	}
	if !slices.Contains(skipPhases, configPhase) {
		logger.Info("Configuring daemons...")
		for _, daemon := range daemons {
			nameField := zap.String("name", daemon.Name())

			logger.Info("Configuring daemon...", nameField)
			if err := daemon.Configure(ctx); err != nil {
				return err
			}
			logger.Info("Configured daemon", nameField)
		}
	}

	if !slices.Contains(skipPhases, runPhase) {
		for _, daemon := range daemons {
			nameField := zap.String("name", daemon.Name())

			logger.Info("Ensuring daemon is running...", nameField)
			if err := daemon.EnsureRunning(ctx); err != nil {
				return err
			}
			logger.Info("Daemon is running", nameField)

			logger.Info("Running post-launch tasks...", nameField)
			if err := daemon.PostLaunch(); err != nil {
				return err
			}
			logger.Info("Finished post-launch tasks", nameField)
		}
	}
	return nil
}
