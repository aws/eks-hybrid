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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
	certName                  = "test-cert"
	certTestNamespace         = "cert-test"
	issuerName                = "selfsigned-issuer"
	certSecretName            = "selfsigned-cert-tls"
	certManagerWaitTimeout    = 5 * time.Minute
)

// CertManagerTest tests the cert-manager addon
type CertManagerTest struct {
	Cluster    string
	addon      *Addon
	K8S        clientgo.Interface
	EKSClient  *eks.Client
	K8SConfig  *rest.Config
	Logger     logr.Logger
	CertClient certmanagerclientset.Interface
	PCAIssuer  *PCAIssuerTest
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
	if _, err := ik8s.GetAndWait(ctx, certManagerWaitTimeout, c.K8S.AppsV1().Deployments(c.addon.Namespace), certManagerName, func(d *appsv1.Deployment) bool {
		return d.Status.Replicas == d.Status.ReadyReplicas
	}); err != nil {
		return err
	}

	// Wait for cert-manager webhook deployment to be ready
	if _, err := ik8s.GetAndWait(ctx, certManagerWaitTimeout, c.K8S.AppsV1().Deployments(c.addon.Namespace), certManagerWebhookName, func(d *appsv1.Deployment) bool {
		return d.Status.Replicas == d.Status.ReadyReplicas
	}); err != nil {
		return err
	}

	// Wait for cert-manager cainjector deployment to be ready
	if _, err := ik8s.GetAndWait(ctx, certManagerWaitTimeout, c.K8S.AppsV1().Deployments(c.addon.Namespace), certManagerCainjectorName, func(d *appsv1.Deployment) bool {
		return d.Status.Replicas == d.Status.ReadyReplicas
	}); err != nil {
		return err
	}

	if err := c.PCAIssuer.Setup(ctx); err != nil {
		return fmt.Errorf("failed to setup AWS PCA Issuer: %v", err)
	}

	c.Logger.Info("Cert-manager setup is complete")
	return nil
}

// Validate tests cert-manager functionality by creating and validating a self-signed certificate
func (c *CertManagerTest) Validate(ctx context.Context) error {
	c.Logger.Info("Starting cert-manager validation")

	// Create test namespace if it doesn't exist
	if err := createNamespaceIfNotExists(ctx, c.K8S, certTestNamespace); err != nil {
		return fmt.Errorf("failed to create test namespace: %v", err)
	}

	// Create self-signed issuer
	if err := createSelfSignedIssuer(ctx, c.Logger, c.CertClient, certTestNamespace, issuerName); err != nil {
		return fmt.Errorf("failed to create self-signed issuer: %v", err)
	}

	// Create certificate
	if err := createCertificate(ctx, c.Logger, c.CertClient, certTestNamespace, certName, issuerName, certSecretName); err != nil {
		return fmt.Errorf("failed to create certificate: %v", err)
	}

	// Validate certificate
	if err := validateCertificate(ctx, c.Logger, c.CertClient, certTestNamespace, certName); err != nil {
		return fmt.Errorf("failed to validate certificate: %v", err)
	}

	// AWS PCA Issuer validation if it's configured
	c.Logger.Info("Starting AWS PCA Issuer validation")

	if err := c.PCAIssuer.Validate(ctx); err != nil {
		return fmt.Errorf("AWS PCA Issuer validation failed: %v", err)
	}

	c.Logger.Info("AWS PCA Issuer validation completed successfully")

	c.Logger.Info("Cert-manager validation completed successfully")
	return nil
}

// PrintLogs collects and prints logs for debugging
func (c *CertManagerTest) PrintLogs(ctx context.Context) error {
	// Fetch cert-manager logs
	logs, err := kubernetes.FetchLogs(ctx, c.K8S, c.addon.Name, c.addon.Namespace)
	if err != nil {
		c.Logger.Error(err, "Failed to collect logs for cert-manager")
	} else {
		c.Logger.Info("Logs for cert-manager", "controller", logs)
	}

	// Fetch PCA issuer logs if applicable
	if err := c.PCAIssuer.PrintLogs(ctx); err != nil {
		c.Logger.Error(err, "Failed to collect AWS PCA Issuer logs")
	}

	return nil
}

// Delete removes the addon and cleans up test resources
func (c *CertManagerTest) Delete(ctx context.Context) error {
	// Clean up test resources
	c.Logger.Info("Cleaning up cert-manager test resources")

	// Clean up AWS PCA Issuer resources if applicable
	if c.PCAIssuer != nil {
		c.Logger.Info("Cleaning up AWS PCA Issuer resources")
		if err := c.PCAIssuer.Cleanup(ctx); err != nil {
			c.Logger.Error(err, "Failed to clean up AWS PCA Issuer resources")
		}
	}

	// Delete certificate
	err := c.CertClient.CertmanagerV1().Certificates(certTestNamespace).Delete(
		ctx, certName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		c.Logger.Error(err, "Failed to delete certificate")
	}

	// Delete issuer
	err = c.CertClient.CertmanagerV1().Issuers(certTestNamespace).Delete(
		ctx, issuerName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		c.Logger.Error(err, "Failed to delete issuer")
	}

	// Delete test namespace
	err = c.K8S.CoreV1().Namespaces().Delete(ctx, certTestNamespace, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		c.Logger.Error(err, "Failed to delete test namespace")
	}

	// Delete cert-manager addon
	return c.addon.Delete(ctx, c.EKSClient, c.Logger)
}

func createNamespaceIfNotExists(ctx context.Context, k8s clientgo.Interface, namespace string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	_, err := k8s.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
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

	_, err := certClient.CertmanagerV1().Issuers(namespace).Create(ctx, issuer, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
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

	_, err := certClient.CertmanagerV1().Certificates(namespace).Create(ctx, cert, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
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
		return fmt.Errorf("error validating certificate: %v", err)
	}

	return nil
}
