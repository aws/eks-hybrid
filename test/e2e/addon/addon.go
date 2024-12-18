package addon

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/go-logr/logr"
)

type Addon struct {
	Name          string
	Cluster       string
	Configuration string
}

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

	_, err := client.DescribeAddon(ctx, params)

	return err
}
