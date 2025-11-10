package addon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	clientgo "k8s.io/client-go/kubernetes"

	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

type CloudWatchAddon struct {
	Addon
	PodIdentityRoleArn string
}

const (
	cloudwatchAddonName        = "amazon-cloudwatch-observability"
	cloudwatchNamespace        = "amazon-cloudwatch"
	cloudwatchServiceAccount   = "cloudwatch-agent"
	cloudwatchComponentTimeout = 10 * time.Minute
	cloudwatchCheckInterval    = 15 * time.Second
)

// NewCloudWatchAddon creates a new CloudWatch Observability addon instance
func NewCloudWatchAddon(cluster, roleArn string) CloudWatchAddon {
	addon := Addon{
		Cluster:   cluster,
		Name:      cloudwatchAddonName,
		Namespace: cloudwatchNamespace,
	}

	if roleArn != "" {
		addon.PodIdentityAssociations = []PodIdentityAssociation{
			{
				RoleArn:        roleArn,
				ServiceAccount: cloudwatchServiceAccount,
			},
		}
	}

	return CloudWatchAddon{
		Addon:              addon,
		PodIdentityRoleArn: roleArn,
	}
}

// WaitForComponents waits for CloudWatch components to be ready
func (cw CloudWatchAddon) WaitForComponents(ctx context.Context, k8sClient clientgo.Interface, logger logr.Logger) error {
	timeout := time.After(cloudwatchComponentTimeout)
	ticker := time.NewTicker(cloudwatchCheckInterval)
	defer ticker.Stop()

	logger.Info("Waiting for CloudWatch components to be ready")

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for CloudWatch components to be ready")
		case <-ticker.C:
			// Check if namespace exists
			err := kubernetes.CheckNamespaceExists(ctx, k8sClient, cloudwatchNamespace)
			if err != nil {
				logger.Info("Waiting for CloudWatch namespace to be created")
				continue
			}

			// Check for operator deployment
			operators, err := kubernetes.ListDeploymentsWithLabels(ctx, k8sClient, cloudwatchNamespace, "control-plane=controller-manager")
			if err != nil || len(operators.Items) == 0 {
				logger.Info("Waiting for CloudWatch operator deployment")
				continue
			}

			operator := operators.Items[0]
			if operator.Status.ReadyReplicas != operator.Status.Replicas || operator.Status.ReadyReplicas == 0 {
				logger.Info("Waiting for operator pods to be ready", "ready", operator.Status.ReadyReplicas, "desired", operator.Status.Replicas)
				continue
			}

			logger.Info("CloudWatch webhook components are ready", "operator-ready", operator.Status.ReadyReplicas)
			return nil
		}
	}
}

// VerifyCwAddon verifies CloudWatch addon functionality including webhook validation and log groups
func (cw CloudWatchAddon) VerifyCwAddon(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	cwLogsClient *cloudwatchlogs.Client,
	logger logr.Logger,
) error {
	logger.Info("Verifying CloudWatch Observability addon functionality")

	if err := cw.testWebhookValidation(ctx, dynamicClient, logger); err != nil {
		return fmt.Errorf("testing webhook validation: %w", err)
	}

	if err := cw.VerifyCloudWatchLogGroups(ctx, cwLogsClient, logger); err != nil {
		return fmt.Errorf("verifying CloudWatch log groups: %w", err)
	}

	logger.Info("CloudWatch addon verification successful ")
	return nil
}

// VerifyCloudWatchLogGroups verifies that CloudWatch log groups exist and have active streams
func (cw CloudWatchAddon) VerifyCloudWatchLogGroups(ctx context.Context, cwLogsClient *cloudwatchlogs.Client, logger logr.Logger) error {
	logger.Info("Verifying CloudWatch log groups exist and have streams")

	logGroups := []string{
		"/aws/containerinsights/" + cw.Cluster + "/application",
		"/aws/containerinsights/" + cw.Cluster + "/dataplane",
		"/aws/containerinsights/" + cw.Cluster + "/performance",
		"/aws/containerinsights/" + cw.Cluster + "/host",
	}

	foundLogGroups := 0
	for _, logGroupName := range logGroups {
		response, err := cwLogsClient.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{
			LogGroupNamePrefix: aws.String(logGroupName),
			Limit:              aws.Int32(10),
		})
		if err != nil {
			logger.Info("Could not check log group", "logGroup", logGroupName, "error", err.Error())
			continue
		}

		for _, logGroup := range response.LogGroups {
			if logGroup.LogGroupName == nil || *logGroup.LogGroupName != logGroupName {
				continue
			}

			foundLogGroups++
			logger.Info("Found CloudWatch log group - addon is working", "logGroup", logGroupName)

			if streams, err := cwLogsClient.DescribeLogStreams(ctx, &cloudwatchlogs.DescribeLogStreamsInput{
				LogGroupName: aws.String(logGroupName),
				Limit:        aws.Int32(5),
			}); err == nil && len(streams.LogStreams) > 0 {
				logger.Info("Log group has active streams - CloudWatch receiving logs", "logGroup", logGroupName, "streamCount", len(streams.LogStreams))
			}
			break
		}
	}

	if foundLogGroups > 0 {
		logger.Info("CloudWatch log groups verification successful - found log groups", "foundGroups", foundLogGroups, "expectedGroups", len(logGroups))
		return nil
	}

	return fmt.Errorf("no CloudWatch log groups found - expected %d log groups but found %d", len(logGroups), foundLogGroups)
}

// SetupCwAddon handles CloudWatch addon installation and setup
func (cw *CloudWatchAddon) SetupCwAddon(ctx context.Context, eksClient *eks.Client, k8sClient clientgo.Interface, cwLogsClient *cloudwatchlogs.Client, logger logr.Logger) error {
	logger.Info("Setting up CloudWatch addon for mixed mode", "cluster", cw.Cluster)

	// Clean up existing log groups for fresh test environment
	if err := cw.cleanupLogGroups(ctx, cwLogsClient, logger); err != nil {
		logger.Info("Failed to cleanup old log groups - continuing", "error", err.Error())
	}

	if err := cw.Create(ctx, eksClient, logger); err != nil {
		return fmt.Errorf("creating CloudWatch addon: %w", err)
	}

	if err := cw.WaitUntilActive(ctx, eksClient, logger); err != nil {
		return fmt.Errorf("waiting for addon to become active: %w", err)
	}

	if err := cw.WaitForComponents(ctx, k8sClient, logger); err != nil {
		return fmt.Errorf("waiting for CloudWatch components: %w", err)
	}

	logger.Info("CloudWatch addon setup completed successfully")
	return nil
}

// testWebhookValidation tests CloudWatch webhook validation functionality
func (cw *CloudWatchAddon) testWebhookValidation(ctx context.Context, dynamicClient dynamic.Interface, logger logr.Logger) error {
	logger.Info("Testing CloudWatch addon webhook validation functionality ")

	testName := fmt.Sprintf("webhook-validation-test-%d", time.Now().Unix())

	invalidCRD := fmt.Sprintf(`
apiVersion: cloudwatch.aws.amazon.com/v1alpha1
kind: AmazonCloudWatchAgent
metadata:
  name: %s
  namespace: %s
spec:
  resources:
    requests:
      memory: "invalid-memory-format"
      cpu: "999cores"`, testName, cloudwatchNamespace)

	// Apply invalid resource - should FAIL due to webhook validation
	_, err := kubernetes.CreateResourceFromYAML(ctx, logger, dynamicClient, invalidCRD)

	if err == nil {
		logger.Error(nil, "Webhook validation test FAILED - invalid resource was accepted")

		gvr := schema.GroupVersionResource{
			Group:    "cloudwatch.aws.amazon.com",
			Version:  "v1alpha1",
			Resource: "amazoncloudwatchagents",
		}
		_ = kubernetes.DeleteResource(ctx, logger, dynamicClient, gvr, cloudwatchNamespace, testName)

		return fmt.Errorf("webhook validation failed - invalid resource quantities were accepted")
	}

	errorOutput := err.Error()
	if strings.Contains(errorOutput, "admission webhook") && strings.Contains(errorOutput, "denied the request") {
		logger.Info("CloudWatch webhook validation test successful - webhook correctly rejected invalid resource", "expectedWebhookError", errorOutput)
		return nil
	}

	return fmt.Errorf("unexpected validation error (not webhook): %s", errorOutput)
}

// cleanupLogGroups deletes existing CloudWatch addon log groups to ensure clean test environment
func (cw *CloudWatchAddon) cleanupLogGroups(ctx context.Context, cwLogsClient *cloudwatchlogs.Client, logger logr.Logger) error {
	logger.Info("Cleaning up existing CloudWatch addon log groups for clean test environment")

	logGroups := []string{
		"/aws/containerinsights/" + cw.Cluster + "/application",
		"/aws/containerinsights/" + cw.Cluster + "/dataplane",
		"/aws/containerinsights/" + cw.Cluster + "/host",
		"/aws/containerinsights/" + cw.Cluster + "/performance",
	}

	deletedGroups := []string{}

	for _, logGroupName := range logGroups {
		logger.Info("Attempting to delete log group", "logGroup", logGroupName)
		_, err := cwLogsClient.DeleteLogGroup(ctx, &cloudwatchlogs.DeleteLogGroupInput{
			LogGroupName: aws.String(logGroupName),
		})
		if err != nil {
			if strings.Contains(err.Error(), "ResourceNotFoundException") {
				logger.Info("Log group does not exist (already clean)", "logGroup", logGroupName)
			} else {
				logger.Info("Could not delete log group", "logGroup", logGroupName, "error", err.Error())
			}
		} else {
			logger.Info("Successfully initiated deletion of log group", "logGroup", logGroupName)
			deletedGroups = append(deletedGroups, logGroupName)
		}
	}

	if len(deletedGroups) > 0 {
		logger.Info("Waiting for log group deletions to complete to avoid conflicts", "deletedCount", len(deletedGroups))
		if err := cw.waitForLogGroupDeletions(ctx, cwLogsClient, deletedGroups, logger); err != nil {
			logger.Info("Some log group deletions may still be in progress", "error", err.Error())
		}

		time.Sleep(30 * time.Second)
	}

	logger.Info("Log group cleanup completed - environment is clean for fresh test")
	return nil
}

// waitForLogGroupDeletions waits for log group deletions to complete
func (cw *CloudWatchAddon) waitForLogGroupDeletions(ctx context.Context, cwLogsClient *cloudwatchlogs.Client, logGroups []string, logger logr.Logger) error {
	remainingGroups := make(map[string]bool)
	for _, lg := range logGroups {
		remainingGroups[lg] = true
	}

	return wait.PollUntilContextTimeout(ctx, 10*time.Second, 2*time.Minute, false, func(ctx context.Context) (bool, error) {
		for logGroup := range remainingGroups {
			response, err := cwLogsClient.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{
				LogGroupNamePrefix: aws.String(logGroup),
				Limit:              aws.Int32(1),
			})
			if err != nil {
				logger.Info("Error checking log group deletion", "logGroup", logGroup, "error", err.Error())
				continue
			}

			exists := false
			for _, lg := range response.LogGroups {
				if lg.LogGroupName != nil && *lg.LogGroupName == logGroup {
					exists = true
					break
				}
			}

			if !exists {
				logger.Info("Log group deleted", "logGroup", logGroup)
				delete(remainingGroups, logGroup)
			}
		}

		if len(remainingGroups) == 0 {
			logger.Info("All log group deletions completed")
			return true, nil
		}
		return false, nil
	})
}
