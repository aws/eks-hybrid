package addon

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgo "k8s.io/client-go/kubernetes"

	"github.com/aws/eks-hybrid/test/e2e/errors"
)

type Addon struct {
	Name          string
	Namespace     string
	Cluster       string
	Configuration string
}

const (
	backoff           = 10 * time.Second
	addonPollInterval = 10 * time.Second
	addonPollTimeout  = 5 * time.Minute
)

func (a Addon) Create(ctx context.Context, client *eks.Client, logger logr.Logger) error {
	logger.Info("Create cluster add-on", "ClusterAddon", a.Name)

	params := &eks.CreateAddonInput{
		ClusterName:         &a.Cluster,
		AddonName:           &a.Name,
		ConfigurationValues: &a.Configuration,
	}

	_, err := client.CreateAddon(ctx, params)

	if err == nil || errors.IsType(err, &types.ResourceInUseException{}) {
		// Ignore if add-on is already created
		return nil
	}
	return err
}

func (a Addon) describe(ctx context.Context, client *eks.Client) (*types.Addon, error) {
	params := &eks.DescribeAddonInput{
		ClusterName: &a.Cluster,
		AddonName:   &a.Name,
	}

	describeAddonOutput, err := client.DescribeAddon(ctx, params)
	if err != nil {
		return nil, err
	}

	return describeAddonOutput.Addon, nil
}

func (a Addon) WaitUntilActive(ctx context.Context, client *eks.Client, logger logr.Logger) error {
	logger.Info("Describe cluster add-on", "ClusterAddon", a.Name)

	for {
		addon, err := a.describe(ctx, client)
		if err != nil {
			logger.Error(err, "Failed to describe cluster add-on")
		} else if addon.Status == types.AddonStatusCreateFailed ||
			addon.Status == types.AddonStatusDeleteFailed ||
			addon.Status == types.AddonStatusUpdateFailed {
			return fmt.Errorf("add-on %s is in errored terminal status: %s", a.Name, addon.Status)
		} else if addon.Status == types.AddonStatusCreating || addon.Status == types.AddonStatusUpdating {
			logger.Info("Add-on is still in creating or updating status", "ClusterAddon", a.Name, "Status", addon.Status)
		} else {
			// Add-on is either active or degraded
			// in our case degraded is acceptable since this is usually due to there not being enough replicas
			// which happens as we create and delete nodes
			return nil
		}

		logger.Info("Waiting for add-on to be ACTIVE", "ClusterAddon", a.Name)

		select {
		case <-ctx.Done():
			return fmt.Errorf("add-on %s still has status %s: %w", a.Name, addon.Status, ctx.Err())
		case <-time.After(backoff):
		}
	}
}

func (a Addon) CreateAndWaitForActive(ctx context.Context, eksClient *eks.Client, k8s clientgo.Interface, logger logr.Logger) error {
	if err := a.Create(ctx, eksClient, logger); err != nil {
		return err
	}

	if err := a.WaitUntilActive(ctx, eksClient, logger); err != nil {
		return err
	}

	return nil
}

func (a Addon) Delete(ctx context.Context, client *eks.Client, logger logr.Logger) error {
	logger.Info("Delete cluster add-on", "ClusterAddon", a.Name)

	params := &eks.DeleteAddonInput{
		ClusterName: &a.Cluster,
		AddonName:   &a.Name,
	}

	_, err := client.DeleteAddon(ctx, params)

	// Add-on is deleted already
	if errors.IsType(err, &types.ResourceNotFoundException{}) {
		return nil
	}

	// Otherwise let's poll until it's deleted
	err = wait.PollUntilContextTimeout(ctx, addonPollInterval, addonPollTimeout, true, func(ctx context.Context) (bool, error) {
		_, err := a.describe(ctx, client)
		if errors.IsType(err, &types.ResourceNotFoundException{}) {
			return true, nil
		}

		logger.Info("Waiting for add-on to be deleted", "ClusterAddon", a.Name)
		return false, nil
	})

	return err
}
