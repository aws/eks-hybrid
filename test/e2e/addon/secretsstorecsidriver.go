package addon

import (
	"context"
	_ "embed"
	"fmt"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/errors"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
	peeredtypes "github.com/aws/eks-hybrid/test/e2e/peered/types"
)

const (
	secretsStoreCSIDriver             = "aws-secrets-store-csi-driver-provider"
	secretsStoreCSIDriverNamespace    = "aws-secrets-manager"
	secretsStoreCSIDriverMainDriver   = "secrets-store-csi-driver"
	secretsStoreCSIDriverProvider     = "secrets-store-csi-driver"
	secretsStoreTestPod               = "secrets-store-app"
	secretsStoreTestPodServiceAccount = "nginx-pod-identity-deployment-sa"
)

//go:embed testdata/secrets_store_csi_static_provisioning.yaml
var secretsStoreStaticProvisioningYaml string

// SecretsStoreCSIDriverTest tests the Secrets Store CSI driver addon
type SecretsStoreCSIDriverTest struct {
	Cluster              string
	addon                *Addon
	K8S                  peeredtypes.K8s
	EKSClient            *eks.Client
	SecretsManagerClient *secretsmanager.Client
	K8SConfig            *rest.Config
	Logger               logr.Logger
	PodIdentityRoleArn   string
	Region               string

	secretName  string
	secretValue string
}

// Create installs the Secrets Store CSI driver addon
func (s *SecretsStoreCSIDriverTest) Create(ctx context.Context) error {
	s.addon = &Addon{
		Cluster:   s.Cluster,
		Namespace: secretsStoreCSIDriverNamespace,
		Name:      secretsStoreCSIDriver,
	}

	if err := s.addon.CreateAndWaitForActive(ctx, s.EKSClient, s.K8S, s.Logger); err != nil {
		return err
	}

	// Wait for CSI driver daemonset to be ready
	if err := kubernetes.DaemonSetWaitForReady(ctx, s.Logger, s.K8S, secretsStoreCSIDriverNamespace, secretsStoreCSIDriver); err != nil {
		return fmt.Errorf("daemonset %s not ready: %w", secretsStoreCSIDriver, err)
	}

	if err := kubernetes.DaemonSetWaitForReady(ctx, s.Logger, s.K8S, secretsStoreCSIDriverNamespace, secretsStoreCSIDriverProvider); err != nil {
		return fmt.Errorf("daemonset %s not ready: %w", secretsStoreCSIDriverProvider, err)
	}

	return nil
}

// Validate checks if Secrets Store CSI driver is working correctly
func (s *SecretsStoreCSIDriverTest) Validate(ctx context.Context) error {
	// Set up pod identity association for test pod service account
	createPodIdentityAssociationInput := &eks.CreatePodIdentityAssociationInput{
		ClusterName:    &s.Cluster,
		Namespace:      aws.String(defaultNamespace),
		RoleArn:        &s.PodIdentityRoleArn,
		ServiceAccount: aws.String(secretsStoreTestPodServiceAccount),
	}

	_, err := s.EKSClient.CreatePodIdentityAssociation(ctx, createPodIdentityAssociationInput)
	if err != nil && !errors.IsType(err, &types.ResourceInUseException{}) {
		return fmt.Errorf("failed to create pod identity association: %w", err)
	}

	// Find the test secret by cluster tag
	if err := s.findTestSecret(ctx); err != nil {
		return fmt.Errorf("failed to find test secret: %w", err)
	}

	// Replace yaml file placeholder values
	replacer := strings.NewReplacer(
		"{{NAMESPACE}}", defaultNamespace,
		"{{SECRETS_STORE_TEST_POD}}", secretsStoreTestPod,
		"{{SECRET_NAME}}", s.secretName,
		"{{SERVICE_ACCOUNT_NAME}}", secretsStoreTestPodServiceAccount,
		"{{ECR_ACCOUNT_ID}}", constants.EcrAccountId,
		"{{REGION}}", s.Region,
	)

	replacedYaml := replacer.Replace(secretsStoreStaticProvisioningYaml)
	objs, err := kubernetes.YamlToUnstructured([]byte(replacedYaml))
	if err != nil {
		return fmt.Errorf("failed to read secrets store static provisioning yaml file: %w", err)
	}

	s.Logger.Info("Applying Secrets Store CSI static provisioning yaml")

	if err := kubernetes.UpsertManifestsWithRetries(ctx, s.K8S, objs); err != nil {
		return fmt.Errorf("failed to deploy Secrets Store CSI static provisioning yaml: %w", err)
	}

	// Verify ServiceAccount was created
	_, err = s.K8S.CoreV1().ServiceAccounts(defaultNamespace).Get(ctx, secretsStoreTestPodServiceAccount, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("ServiceAccount %s not found in namespace %s: %w", secretsStoreTestPodServiceAccount, defaultNamespace, err)
	}

	podListOptions := metav1.ListOptions{
		FieldSelector: "metadata.name=" + secretsStoreTestPod,
	}

	if err := kubernetes.WaitForPodsToBeRunning(ctx, s.K8S, podListOptions, defaultNamespace, s.Logger); err != nil {
		return fmt.Errorf("failed to wait for test pod to be running: %w", err)
	}

	// Validate that the secret was mounted correctly by checking the pod's mounted file
	s.Logger.Info("Validating secret was mounted correctly in pod")

	// Try to read the secret content
	execCmd := []string{"cat", "/mnt/secrets-store/" + s.secretName}
	stdout, stderr, err := kubernetes.ExecPodWithRetries(ctx, s.K8SConfig, s.K8S, secretsStoreTestPod, defaultNamespace, execCmd...)
	if err != nil {
		return fmt.Errorf("could not read secrets from secrets manager: %w", err)
	}

	if stderr != "" {
		return fmt.Errorf("stderr is not empty: %s", stderr)
	}

	if trimAllLineBreaks(stdout) != trimAllLineBreaks(s.secretValue) {
		return fmt.Errorf("expected secret value %s, got %s", s.secretValue, stdout)
	}

	s.Logger.Info("Successfully validated Secrets Store CSI driver functionality")

	// Clean up - delete static provisioning yaml
	if err := kubernetes.DeleteManifestsWithRetries(ctx, s.K8S, objs); err != nil {
		return fmt.Errorf("failed to delete Secrets Store CSI static provisioning yaml: %w", err)
	}

	return nil
}

func (s *SecretsStoreCSIDriverTest) Delete(ctx context.Context) error {
	return s.addon.Delete(ctx, s.EKSClient, s.Logger)
}

func (s *SecretsStoreCSIDriverTest) findTestSecret(ctx context.Context) error {
	s.Logger.Info("Finding test secret by cluster tag", "cluster", s.Cluster, "tagKey", constants.TestClusterTagKey)

	// List all secrets with pagination support
	var allSecrets []smtypes.SecretListEntry
	listSecretsInput := &secretsmanager.ListSecretsInput{}
	pageCount := 0

	for {
		pageCount++

		listSecretsOutput, err := s.SecretsManagerClient.ListSecrets(ctx, listSecretsInput)
		if err != nil {
			return fmt.Errorf("failed to list secrets: %w", err)
		}

		allSecrets = append(allSecrets, listSecretsOutput.SecretList...)

		if listSecretsOutput.NextToken == nil {
			break
		}
		listSecretsInput.NextToken = listSecretsOutput.NextToken
	}

	s.Logger.Info("Listed all secrets from AWS with pagination", "totalPages", pageCount, "totalSecrets", len(allSecrets))

	// Find secret with the cluster tag
	for _, secret := range allSecrets {
		describeSecretOutput, err := s.SecretsManagerClient.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
			SecretId: secret.Name,
		})
		if err != nil {
			s.Logger.Error(err, "Failed to describe secret", "arn", *secret.ARN)
			continue
		}

		// Check if this secret has the cluster tag
		for _, tag := range describeSecretOutput.Tags {
			if *tag.Key == constants.TestClusterTagKey && *tag.Value == s.Cluster {
				s.Logger.Info("Found test secret", "name", *secret.Name)
				// Get the secret value to extract key and value
				s.secretName = *secret.Name
				return s.extractSecretValue(ctx, secret.Name)
			}
		}
	}

	return fmt.Errorf("test secret not found for cluster %s", s.Cluster)
}

func (s *SecretsStoreCSIDriverTest) extractSecretValue(ctx context.Context, secretName *string) error {
	getSecretValueOutput, err := s.SecretsManagerClient.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: secretName,
	})
	if err != nil {
		return fmt.Errorf("failed to get secret value: %w", err)
	}

	s.secretValue = *getSecretValueOutput.SecretString
	return nil
}

func trimAllLineBreaks(s string) string {
	re := regexp.MustCompile(`[\r\n]+`)
	return re.ReplaceAllString(s, "")
}
