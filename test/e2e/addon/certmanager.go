package addon

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	certmanagerclientset "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	ik8s "github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

const (
	certManagerNamespace      = "cert-manager"
	certManagerName           = "cert-manager"
	certManagerCainjectorName = "cert-manager-cainjector"
	certManagerWebhookName    = "cert-manager-webhook"
	certManagerWaitTimeout    = 5 * time.Minute
)

// CertManagerTest tests the cert-manager addon
type CertManagerTest struct {
	Cluster                                             string
	addon                                               *Addon
	K8S                                                 clientgo.Interface
	EKSClient                                           *eks.Client
	K8SConfig                                           *rest.Config
	Logger                                              logr.Logger
	CertClient                                          certmanagerclientset.Interface
	CertName, CertNamespace, CertSecretName, IssuerName string
}

// Create installs the cert-manager addon
func (c *CertManagerTest) Create(ctx context.Context) error {
	c.addon = &Addon{
		Cluster:   c.Cluster,
		Namespace: certManagerNamespace,
		Name:      certManagerName,
	}

	if err := c.addon.CreateAndWaitForActive(ctx, c.EKSClient, c.K8S, c.Logger); err != nil {
		return err
	}

	// Wait for cert-manager deployment to be ready
	if err := kubernetes.DeploymentWaitForReplicas(ctx, certManagerWaitTimeout, c.K8S, c.addon.Namespace, certManagerName); err != nil {
		return err
	}

	// Wait for cert-manager webhook deployment to be ready
	if err := kubernetes.DeploymentWaitForReplicas(ctx, certManagerWaitTimeout, c.K8S, c.addon.Namespace, certManagerWebhookName); err != nil {
		return err
	}

	// Wait for cert-manager cainjector deployment to be ready
	if err := kubernetes.DeploymentWaitForReplicas(ctx, certManagerWaitTimeout, c.K8S, c.addon.Namespace, certManagerCainjectorName); err != nil {
		return err
	}

	c.Logger.Info("Cert-manager setup is complete")
	return nil
}

// Validate tests cert-manager functionality by creating and validating a self-signed certificate
func (c *CertManagerTest) Validate(ctx context.Context) error {
	c.Logger.Info("Starting cert-manager validation")

	// Create test namespace if it doesn't exist
	if err := kubernetes.CreateNamespace(ctx, c.K8S, c.CertNamespace); err != nil {
		return fmt.Errorf("failed to create test namespace: %w", err)
	}

	// Create self-signed issuer
	if err := createSelfSignedIssuer(ctx, c.Logger, c.CertClient, c.CertNamespace, c.IssuerName); err != nil {
		return fmt.Errorf("failed to create self-signed issuer: %w", err)
	}

	// Create certificate
	if err := createCertificate(ctx, c.Logger, c.CertClient, c.CertNamespace, c.CertName, c.IssuerName, c.CertSecretName); err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Validate certificate
	if err := validateCertificate(ctx, c.Logger, c.CertClient, c.CertNamespace, c.CertName); err != nil {
		return fmt.Errorf("failed to validate certificate: %w", err)
	}

	c.Logger.Info("Cert-manager validation completed successfully")
	return nil
}

// PrintLogs collects and prints logs for debugging
func (c *CertManagerTest) PrintLogs(ctx context.Context) error {
	// Fetch cert-manager logs
	logs, err := kubernetes.FetchLogs(ctx, c.K8S, c.addon.Name, c.addon.Namespace)
	if err != nil {
		return fmt.Errorf("failed to collect logs for %s: %w", c.addon.Name, err)
	}

	c.Logger.Info("Logs for cert-manager", "controller", logs)
	return nil
}

// Delete removes the addon and cleans up test resources
func (c *CertManagerTest) Delete(ctx context.Context) error {
	// Clean up test resources
	c.Logger.Info("Cleaning up cert-manager test resources")

	// Delete certificate
	err := ik8s.IdempotentDelete(ctx, c.CertClient.CertmanagerV1().Certificates(c.CertNamespace), c.CertName)
	if err != nil {
		return fmt.Errorf("failed to delete certificate: %w", err)
	}

	// Delete issuer
	err = ik8s.IdempotentDelete(ctx, c.CertClient.CertmanagerV1().Issuers(c.CertNamespace), c.IssuerName)
	if err != nil {
		return fmt.Errorf("failed to delete issuer: %w", err)
	}

	// Delete test namespace
	err = kubernetes.DeleteNamespace(ctx, c.K8S, c.CertNamespace)
	if err != nil {
		return fmt.Errorf("failed to delete test namespace: %w", err)
	}

	// Delete cert-manager addon
	return c.addon.Delete(ctx, c.EKSClient, c.Logger)
}

func createSelfSignedIssuer(ctx context.Context, logger logr.Logger, certClient certmanagerclientset.Interface, namespace, name string) error {
	issuer := &certmanagerv1.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				SelfSigned: &certmanagerv1.SelfSignedIssuer{},
			},
		},
	}

	err := ik8s.IdempotentCreate(ctx, certClient.CertmanagerV1().Issuers(namespace), issuer)
	if err != nil {
		return err
	}

	logger.Info("Created Issuer")
	return nil
}

func createCertificate(ctx context.Context, logger logr.Logger, certClient certmanagerclientset.Interface,
	namespace, name, issuerName, secretName string,
) error {
	cert := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: certmanagerv1.CertificateSpec{
			DNSNames: []string{
				"example.com",
			},
			SecretName: secretName,
			IssuerRef: cmmeta.ObjectReference{
				Name: issuerName,
			},
		},
	}

	err := ik8s.IdempotentCreate(ctx, certClient.CertmanagerV1().Certificates(namespace), cert)
	if err != nil {
		return err
	}

	logger.Info("Created Certificate")
	return nil
}

func validateCertificate(ctx context.Context, logger logr.Logger, certClient certmanagerclientset.Interface,
	namespace, name string,
) error {
	logger.Info("Validating certificate")

	_, err := ik8s.GetAndWait(
		ctx,
		certManagerWaitTimeout,
		certClient.CertmanagerV1().Certificates(namespace),
		name,
		func(cert *certmanagerv1.Certificate) bool {
			for _, condition := range cert.Status.Conditions {
				if condition.Type == certmanagerv1.CertificateConditionReady && condition.Status == cmmeta.ConditionTrue {
					logger.Info("Certificate validated successfully")
					return true
				}
			}
			logger.Info("Certificate not valid yet")
			return false
		},
	)
	if err != nil {
		return fmt.Errorf("error validating certificate: %w", err)
	}

	return nil
}
