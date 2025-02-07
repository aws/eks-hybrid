package flows

import (
	"context"

	"go.uber.org/zap"
	"k8s.io/utils/strings/slices"

	"github.com/aws/eks-hybrid/internal/nodeprovider"
)

const (
	preprocessPhase = "preprocess"
	configPhase     = "config"
	runPhase        = "run"
	ipValidation    = "ip-validation"
)

type Initer struct {
	NodeProvider nodeprovider.NodeProvider
	SkipPhases   []string
	Logger       *zap.Logger
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

	if err := i.NodeProvider.Enrich(ctx); err != nil {
		return err
	}

	if !slices.Contains(i.SkipPhases, ipValidation) {
		i.Logger.Info("Validating Node IP...")

		if err := i.NodeProvider.ValidateNodeIP(); err != nil {
			return err
		}
	}

	aspects := i.NodeProvider.GetAspects()
	i.Logger.Info("Setting up system aspects...")
	for _, aspect := range aspects {
		nameField := zap.String("name", aspect.Name())
		i.Logger.Info("Setting up system aspect..", nameField)
		if err := aspect.Setup(); err != nil {
			return err
		}
		i.Logger.Info("Finished setting up system aspect", nameField)
	}

	if !slices.Contains(i.SkipPhases, preprocessPhase) {
		i.Logger.Info("Configuring Pre-process daemons...")
		if err := i.NodeProvider.PreProcessDaemon(ctx); err != nil {
			return err
		}
	}

	daemons, err := i.NodeProvider.GetDaemons()
	if err != nil {
		return err
	}
	if !slices.Contains(i.SkipPhases, configPhase) {
		i.Logger.Info("Configuring daemons...")
		for _, daemon := range daemons {
			nameField := zap.String("name", daemon.Name())

			i.Logger.Info("Configuring daemon...", nameField)
			if err := daemon.Configure(); err != nil {
				return err
			}
			i.Logger.Info("Configured daemon", nameField)
		}
	}

	if !slices.Contains(i.SkipPhases, runPhase) {
		for _, daemon := range daemons {
			nameField := zap.String("name", daemon.Name())

			i.Logger.Info("Ensuring daemon is running..", nameField)
			if err := daemon.EnsureRunning(ctx); err != nil {
				return err
			}
			i.Logger.Info("Daemon is running", nameField)

			i.Logger.Info("Running post-launch tasks..", nameField)
			if err := daemon.PostLaunch(); err != nil {
				return err
			}
			i.Logger.Info("Finished post-launch tasks", nameField)
		}
	}

	return i.NodeProvider.Cleanup()
}
