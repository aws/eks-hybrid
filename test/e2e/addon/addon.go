package addon

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgo "k8s.io/client-go/kubernetes"

	"github.com/aws/eks-hybrid/test/e2e/errors"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

type Addon struct {
	Name          string
	Namespace     string
	Cluster       string
	Configuration string
}

const (
	backoff         = 10 * time.Second
	tailLines       = 10
	addonMaxRetries = 5
)

var retryBackoff = wait.Backoff{
	Duration: 1 * time.Second,
	Factor:   2,
	Jitter:   0.1,
	Steps:    5,
	Cap:      30 * time.Second,
}

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
			logger.Info("Failed to describe cluster add-on", "Error", err)
		} else {
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
			return fmt.Errorf("add-on %s still has status %s: %w", a.Name, addon.Status, ctx.Err())
		case <-time.After(backoff):
		}
	}
}

func (a Addon) CreateAddon(ctx context.Context, eksClient *eks.Client, k8s clientgo.Interface, logger logr.Logger) error {
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

	if err == nil || errors.IsType(err, &types.ResourceNotFoundException{}) {
		// Ignore if add-on doesn't exist
		return nil
	}
	return err
}

func (a Addon) FetchLogs(ctx context.Context, k8s clientgo.Interface, logger logr.Logger, containers []string, tailLines int64) error {
	var pods *corev1.PodList
	AddonListOptions := getAddonListOptions(a.Name)
	err := wait.ExponentialBackoffWithContext(ctx, retryBackoff, func(ctx context.Context) (bool, error) {
		var err error
		pods, err = k8s.CoreV1().Pods(a.Namespace).List(ctx, AddonListOptions)
		if err != nil {
			// Log error and return false to retry
			logger.Error(err, "Failed to list pods", "namespace", a.Namespace, "addon", a.Name)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to get pods for %s after retries: %v", a.Name, err)
	}

	for _, pod := range pods.Items {
		for _, container := range containers {
			logOpts := getPodLogOptions(container, aws.Int64(tailLines))
			logs, err := kubernetes.GetPodLogsWithRetries(ctx, k8s, pod.Name, pod.Namespace, logOpts)
			if err != nil {
				return err
			}

			logger.Info(fmt.Sprintf("Logs for %s:\n", a.Name), "pod", pod.Name, "container", container, "logs", logs)
		}
	}

	return nil
}

func getPodLogOptions(containerName string, lines *int64) *corev1.PodLogOptions {
	return &corev1.PodLogOptions{
		Container: containerName, // specify container name if multiple containers
		TailLines: lines,         // get last N lines
	}
}

func getAddonListOptions(addonName string) v1.ListOptions {
	return v1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", "app.kubernetes.io/instance", addonName),
	}
}
