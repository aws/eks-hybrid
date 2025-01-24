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

func (a Addon) Get(ctx context.Context, client *eks.Client, logger logr.Logger) error {
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
			if describeAddonOutput.Addon.Status == types.AddonStatusActive {
				return nil
			}
			logger.Info("Add-on is not in ACTIVE status yet", "ClusterAddon", a.Name)
		}

		logger.Info("Wait for add-on to be ACTIVE", "ClusterAddon", a.Name)

		select {
		case <-ctx.Done():
			return fmt.Errorf("add-on %s still has status %s: %w", a.Name, describeAddonOutput.Addon.Status, ctx.Err())
		case <-time.After(backoff):
		}
	}
}
