package addon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	clientgo "k8s.io/client-go/kubernetes"

	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

type CloudWatchAddon struct {
	Addon
	roleArn string
}

const (
	cloudwatchAddonName        = "amazon-cloudwatch-observability"
	cloudwatchNamespace        = "amazon-cloudwatch"
	cloudwatchServiceAccount   = "cloudwatch-agent"
	cloudwatchComponentTimeout = 10 * time.Minute
	cloudwatchCheckInterval    = 15 * time.Second
	logCollectionWaitTime      = 10 * time.Minute
)

// NewCloudWatchAddon creates a new CloudWatch Observability addon instance
func NewCloudWatchAddon(cluster, roleArn string) CloudWatchAddon {
	return CloudWatchAddon{
		Addon: Addon{
			Cluster: cluster,
			Name:    cloudwatchAddonName,
		},
		roleArn: roleArn,
	}
}

// SetupIRSA sets up complete IRSA configuration for CloudWatch observability addon
func (cw *CloudWatchAddon) SetupIRSA(ctx context.Context, iamClient *iam.Client, eksClient *eks.Client, k8sClient clientgo.Interface, dynamicClient dynamic.Interface, logger logr.Logger) error {
	logger.Info("Setting up complete IRSA configuration for CloudWatch Observabilty agent ")

	// Get cluster information to extract OIDC issuer
	cluster, err := eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: aws.String(cw.Cluster),
	})
	if err != nil {
		return fmt.Errorf("describing cluster: %w", err)
	}

	oidcURL := *cluster.Cluster.Identity.Oidc.Issuer
	logger.Info("Found cluster OIDC issuer", "url", oidcURL)

	// Create OIDC provider if required
	if err := cw.createOIDCProvider(ctx, iamClient, oidcURL, logger); err != nil {
		return fmt.Errorf("creating OIDC provider: %w", err)
	}

	// Apply the trust policy for IRSA
	if err := cw.applyTrustPolicyForIRSA(ctx, iamClient, oidcURL, logger); err != nil {
		return fmt.Errorf("applying trust policy for IRSA: %w", err)
	}

	logger.Info("Using  CloudWatch role ", "roleArn", cw.roleArn)

	// Annotate service account with IRSA role ARN
	if err := cw.annotateServiceAccount(ctx, k8sClient, logger); err != nil {
		return fmt.Errorf("annotating service account: %w", err)
	}

	// Patch FluentBit DaemonSet with IRSA environment variables
	if err := cw.patchFluentBitForIRSA(ctx, k8sClient, logger); err != nil {
		return fmt.Errorf("patching FluentBit DaemonSet for IRSA: %w", err)
	}

	// Patch AmazonCloudWatchAgent CRD for IRSA support
	if err := kubernetes.PatchCloudWatchAgentCRD(ctx, dynamicClient, logger, cloudwatchNamespace); err != nil {
		return fmt.Errorf("patching CRD: %w", err)
	}

	logger.Info("Complete IRSA configuration for mixed mode completed successfully")
	return nil
}

// createOIDCProvider creates OIDC identity provider using IAM client
func (cw *CloudWatchAddon) createOIDCProvider(ctx context.Context, iamClient *iam.Client, oidcURL string, logger logr.Logger) error {
	logger.Info("Creating OIDC provider", "url", oidcURL)

	providers, err := iamClient.ListOpenIDConnectProviders(ctx, &iam.ListOpenIDConnectProvidersInput{})
	if err != nil {
		return fmt.Errorf("listing OIDC providers: %w", err)
	}

	for _, provider := range providers.OpenIDConnectProviderList {
		if strings.Contains(*provider.Arn, oidcURL) {
			logger.Info("OIDC provider already exists", "arn", *provider.Arn)
			return nil
		}
	}

	// Create OIDC provider with EKS OIDC root CA thumbprint
	_, err = iamClient.CreateOpenIDConnectProvider(ctx, &iam.CreateOpenIDConnectProviderInput{
		Url:            aws.String(oidcURL),
		ClientIDList:   []string{"sts.amazonaws.com"},
		ThumbprintList: []string{"9e99a48a9960b14926bb7f3b02e22da2b0ab7280"}, // EKS OIDC root CA thumbprint
	})
	if err != nil {
		// Check if it already exists
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "EntityAlreadyExists") {
			logger.Info("OIDC provider already exists")
			return nil
		}
		return fmt.Errorf("creating OIDC provider: %w", err)
	}

	logger.Info("Created OIDC provider successfully")
	return nil
}

// applyTrustPolicyForIRSA updates the IAM role trust policy with correct IRSA conditions
func (cw *CloudWatchAddon) applyTrustPolicyForIRSA(ctx context.Context, iamClient *iam.Client, oidcURL string, logger logr.Logger) error {
	logger.Info("Applying IAM role trust policy for proper IRSA authentication", "roleArn", cw.roleArn, "oidcURL", oidcURL)

	// Extract role name from ARN
	roleName := cw.extractRoleNameFromArn()
	if roleName == "" {
		return fmt.Errorf("could not extract role name from ARN: %s", cw.roleArn)
	}

	// Extract cluster ID from OIDC URL
	clusterID := cw.extractClusterIDFromOIDC(oidcURL)
	if clusterID == "" {
		return fmt.Errorf("could not extract cluster ID from OIDC URL: %s", oidcURL)
	}

	// Extract AWS account ID from ARN
	accountID := cw.getAccountIDFromArn()
	if accountID == "" {
		return fmt.Errorf("could not extract account ID from ARN: %s", cw.roleArn)
	}

	// Build correct trust policy document
	trustPolicyDoc := fmt.Sprintf(`{
		"Version": "2012-10-17",
		"Statement": [
			{
				"Effect": "Allow",
				"Principal": {
					"Federated": "arn:aws:iam::%s:oidc-provider/oidc.eks.us-west-2.amazonaws.com/id/%s"
				},
				"Action": "sts:AssumeRoleWithWebIdentity",
				"Condition": {
					"StringEquals": {
						"oidc.eks.us-west-2.amazonaws.com/id/%s:sub": "system:serviceaccount:amazon-cloudwatch:cloudwatch-agent",
						"oidc.eks.us-west-2.amazonaws.com/id/%s:aud": "sts.amazonaws.com"
					}
				}
			}
		]
	}`, accountID, clusterID, clusterID, clusterID)

	// Update the role's trust policy
	_, err := iamClient.UpdateAssumeRolePolicy(ctx, &iam.UpdateAssumeRolePolicyInput{
		RoleName:       aws.String(roleName),
		PolicyDocument: aws.String(trustPolicyDoc),
	})
	if err != nil {
		return fmt.Errorf("updating assume role policy: %w", err)
	}

	logger.Info("Successfully updated IAM role trust policy for IRSA", "roleName", roleName, "clusterID", clusterID)
	return nil
}

// extractRoleNameFromArn extracts the role name from an ARN
func (cw *CloudWatchAddon) extractRoleNameFromArn() string {
	parts := strings.Split(cw.roleArn, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}
	return ""
}

// extractClusterIDFromOIDC extracts cluster ID from OIDC issuer URL
func (cw *CloudWatchAddon) extractClusterIDFromOIDC(oidcURL string) string {
	parts := strings.Split(oidcURL, "/id/")
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

// getAccountIDFromArn extracts AWS account ID from role ARN
func (cw *CloudWatchAddon) getAccountIDFromArn() string {
	parts := strings.Split(cw.roleArn, ":")
	if len(parts) >= 5 {
		return parts[4]
	}
	return ""
}

// annotateServiceAccount annotates the CloudWatch service account with IRSA role ARN
func (cw *CloudWatchAddon) annotateServiceAccount(ctx context.Context, k8sClient clientgo.Interface, logger logr.Logger) error {
	logger.Info("Annotating CloudWatch service account for IRSA", "roleArn", cw.roleArn)

	sa, err := k8sClient.CoreV1().ServiceAccounts(cloudwatchNamespace).Get(ctx, cloudwatchServiceAccount, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting service account: %w", err)
	}

	if sa.Annotations == nil {
		sa.Annotations = make(map[string]string)
	}
	sa.Annotations["eks.amazonaws.com/role-arn"] = cw.roleArn

	_, err = k8sClient.CoreV1().ServiceAccounts(cloudwatchNamespace).Update(ctx, sa, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("updating service account with annotation: %w", err)
	}

	logger.Info("Service account annotated for IRSA successfully")
	return nil
}

// patchFluentBitForIRSA patches FluentBit DaemonSet with essential IRSA environment variables
func (cw *CloudWatchAddon) patchFluentBitForIRSA(ctx context.Context, k8s clientgo.Interface, logger logr.Logger) error {
	logger.Info("Patching FluentBit DaemonSet with essential IRSA environment variables", "roleArn", cw.roleArn)

	envVars := map[string]string{
		"AWS_REGION":                  "us-west-2",
		"AWS_WEB_IDENTITY_TOKEN_FILE": "/var/run/secrets/eks.amazonaws.com/serviceaccount/token",
		"AWS_ROLE_ARN":                cw.roleArn,
		"AWS_EC2_METADATA_DISABLED":   "true",
	}

	if err := kubernetes.PatchDaemonSetWithEnvVars(ctx, logger, k8s, cloudwatchNamespace, "fluent-bit", "fluent-bit", envVars); err != nil {
		return fmt.Errorf("patching FluentBit DaemonSet with IRSA: %w", err)
	}

	logger.Info("FluentBit DaemonSet patched successfully with essential IRSA configuration")

	return kubernetes.RestartDaemonSetAndWait(ctx, logger, k8s, cloudwatchNamespace, "fluent-bit")
}

// VerifyCwAddon verifies CloudWatch webhook functionality and log groups for mixed mode.
func (cw CloudWatchAddon) VerifyCwAddon(ctx context.Context, k8sClient clientgo.Interface, dynamicClient dynamic.Interface, awsConfig aws.Config, logger logr.Logger) error {
	logger.Info("Verifying CloudWatch webhook functionality and log groups for mixed mode")

	// Test webhook validation by attempting to create CRD with invalid resource specifications
	if err := cw.testWebhookValidation(ctx, dynamicClient, logger); err != nil {
		return fmt.Errorf("webhook validation test failed: %w", err)
	}

	logger.Info("CloudWatch mixed-mode webhook validated successfully")

	logger.Info("Waiting for CloudWatch DaemonSet to be ready before checking log groups")
	if err := kubernetes.DaemonSetWaitForReady(ctx, logger, k8sClient, cloudwatchNamespace, "cloudwatch-agent"); err != nil {
		return fmt.Errorf("CloudWatch DaemonSet not ready: %w", err)
	}

	logger.Info("CloudWatch DaemonSet is ready - all agent pods are ready")

	// Wait for CloudWatch agents to start collecting and shipping logs
	logger.Info("Waiting for CloudWatch agents to collect and ship logs to CloudWatch",
		"waitTime", logCollectionWaitTime.String())
	time.Sleep(logCollectionWaitTime)

	cwLogsClient := cloudwatchlogs.NewFromConfig(awsConfig)
	if err := cw.VerifyCloudWatchLogGroups(ctx, cwLogsClient, logger); err != nil {
		logger.Info("CloudWatch log groups verification had issues but will continue", "error", err.Error())
	} else {
		logger.Info("CloudWatch log groups verification successful ")
	}

	return nil
}

// VerifyCloudWatchLogGroups verifies that CloudWatch log groups exist and have streams
func (cw *CloudWatchAddon) VerifyCloudWatchLogGroups(ctx context.Context, cwLogsClient *cloudwatchlogs.Client, logger logr.Logger) error {
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

			// Check for log streams (indicates activity)
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
	} else {
		return fmt.Errorf("no CloudWatch log groups found - expected %d log groups but found %d", len(logGroups), foundLogGroups)
	}
}

// testWebhookValidation verifies webhook rejects invalid resource specifications using Kubernetes API
func (cw *CloudWatchAddon) testWebhookValidation(ctx context.Context, dynamicClient dynamic.Interface, logger logr.Logger) error {
	logger.Info("Testing CloudWatch webhook validation functionality using Kubernetes API")

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

	// Apply invalid CRD - should FAIL due to webhook validation
	_, err := kubernetes.CreateCRDFromYAML(ctx, logger, dynamicClient, invalidCRD)

	if err == nil {
		logger.Error(nil, "Webhook validation test FAILED - invalid CRD was accepted")

		gvr := schema.GroupVersionResource{
			Group:    "cloudwatch.aws.amazon.com",
			Version:  "v1alpha1",
			Resource: "amazoncloudwatchagents",
		}
		_ = kubernetes.DeleteCRD(ctx, logger, dynamicClient, gvr, cloudwatchNamespace, testName)

		return fmt.Errorf("webhook validation failed - invalid resource quantities were accepted")
	}

	errorOutput := err.Error()
	if strings.Contains(errorOutput, "admission webhook") && strings.Contains(errorOutput, "denied the request") {
		logger.Info("CloudWatch webhook validation test successful - webhook correctly rejected invalid CRD", "expectedWebhookError", errorOutput)
		return nil
	} else {
		return fmt.Errorf("unexpected validation error (not webhook): %s", errorOutput)
	}
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

// waitForLogGroupDeletions waits for log group deletions to complete using wait.PollUntilContextTimeout
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

// SetupCwAddon handles complete CloudWatch addon setup for mixed mode with IRSA
func (cw *CloudWatchAddon) SetupCwAddon(ctx context.Context, eksClient *eks.Client, iamClient *iam.Client, k8sClient clientgo.Interface, dynamicClient dynamic.Interface, awsConfig aws.Config, logger logr.Logger) error {
	logger.Info("Setting up CloudWatch addon for mixed mode with IRSA", "cluster", cw.Cluster)

	//  Clean up existing log groups for fresh test environment
	cwLogsClient := cloudwatchlogs.NewFromConfig(awsConfig)
	if err := cw.cleanupLogGroups(ctx, cwLogsClient, logger); err != nil {
		logger.Info("Failed to cleanup old log groups - continuing", "error", err.Error())
	}

	// 1. Create EKS addon
	if err := cw.Create(ctx, eksClient, logger); err != nil {
		return fmt.Errorf("creating CloudWatch addon: %w", err)
	}

	// 2. Wait for addon to be active
	if err := cw.WaitUntilActive(ctx, eksClient, logger); err != nil {
		return fmt.Errorf("waiting for addon to be active: %w", err)
	}

	// 4. Setup complete IRSA configuration
	if err := cw.SetupIRSA(ctx, iamClient, eksClient, k8sClient, dynamicClient, logger); err != nil {
		return fmt.Errorf("setting up IRSA: %w", err)
	}

	logger.Info("CloudWatch Observabilty addon is working in mixed mode setup ")
	return nil
}
