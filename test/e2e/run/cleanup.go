package run

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/cleanup"
	"github.com/aws/eks-hybrid/test/e2e/cluster"
)

const (
	phaseNameCleanupStack   = "StackCleanup"
	phaseNameCleanupSweeper = "SweeperCleanup"
)

type E2ECleanup struct {
	AwsCfg        aws.Config
	Logger        logr.Logger
	TestResources cluster.TestResources
}

func (e *E2ECleanup) Run(ctx context.Context) []Phase {
	phases := []Phase{}
	// We want to run both to ensure any dangling resources are cleaned up
	// The sweeper cleanup is configured for this specific cluster name
	err := e.clusterStackCleanup(ctx)
	phases = phaseCompleted(phases, phaseNameCleanupStack, "running cleanup cluster via stack deletion", err)
	if err != nil {
		e.Logger.Error(err, "Failed to run cleanup cluster via stack deletion")
	}

	err = e.clusterSweeperCleanup(ctx)
	phases = phaseCompleted(phases, phaseNameCleanupSweeper, "running cleanup cluster via sweeper", err)
	if err != nil {
		e.Logger.Error(err, "Failed to run cleanup cluster via sweeper")
	}

	return phases
}

func (e *E2ECleanup) clusterStackCleanup(ctx context.Context) error {
	delete := cluster.NewDelete(e.AwsCfg, e.Logger, e.TestResources.Endpoint)
	e.Logger.Info("Cleaning up E2E cluster resources via Stack deletion")
	deleteCluster := cluster.DeleteInput{
		ClusterName:   e.TestResources.ClusterName,
		ClusterRegion: e.TestResources.ClusterRegion,
		Endpoint:      e.TestResources.Endpoint,
	}
	if err := delete.Run(ctx, deleteCluster); err != nil {
		return fmt.Errorf("cleaning up e2e resources: %w", err)
	}

	e.Logger.Info("Cleanup completed successfully")
	return nil
}

func (e *E2ECleanup) clusterSweeperCleanup(ctx context.Context) error {
	sweeper := cleanup.NewSweeper(e.AwsCfg, e.Logger)
	e.Logger.Info("Cleaning up E2E cluster resources via Sweeper")
	err := sweeper.Run(ctx, cleanup.SweeperInput{ClusterName: e.TestResources.ClusterName})
	if err != nil {
		return fmt.Errorf("cleaning up e2e resources: %w", err)
	}

	e.Logger.Info("Cleanup completed successfully")
	return nil
}
