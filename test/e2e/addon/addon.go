package addon

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/go-logr/logr"
)

type Addon struct {
	Name          string
	Cluster       string
	Configuration string
}

const (
	backoff = 10 * time.Second
)

func (a Addon) Create(ctx context.Context, client *eks.Client, logger logr.Logger) error {
	logger.Info("Create cluster add-on", "ClusterAddon", a.Name)

	params := &eks.CreateAddonInput{
		ClusterName:         &a.Cluster,
		AddonName:           &a.Name,
		ConfigurationValues: &a.Configuration,
	}

	_, err := client.CreateAddon(ctx, params)

	return err
}

func (a Addon) WaitUtilActive(ctx context.Context, client *eks.Client, logger logr.Logger) error {
	logger.Info("Describe cluster add-on", "ClusterAddon", a.Name)

	params := &eks.DescribeAddonInput{
		ClusterName: &a.Cluster,
		AddonName:   &a.Name,
	}

	for {
		describeAddonOutput, err := client.DescribeAddon(ctx, params)
		if err != nil {
			logger.Info("Failed to describe cluster add-on", "Error", err)
		} else {
			addon := describeAddonOutput.Addon
			if addon.Status == types.AddonStatusActive {
				return nil
			}

			if addon.Status == types.AddonStatusCreateFailed ||
				addon.Status == types.AddonStatusDegraded ||
				addon.Status == types.AddonStatusDeleteFailed ||
				addon.Status == types.AddonStatusUpdateFailed {
				return fmt.Errorf("add-on %s is in errored terminal status: %s", a.Name, addon.Status)
			}
		}

		logger.Info("Wait for add-on to be ACTIVE", "ClusterAddon", a.Name)

		select {
		case <-ctx.Done():
			return fmt.Errorf("add-on %s still has status %s: %w", a.Name, describeAddonOutput.Addon.Status, ctx.Err())
		case <-time.After(backoff):
		}
	}
}
