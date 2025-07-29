package suite

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/acmpca"
	awspcaclientset "github.com/cert-manager/aws-privateca-issuer/pkg/clientset/v1beta1"
	certmanagerclientset "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/aws/eks-hybrid/test/e2e/addon"
	"github.com/aws/eks-hybrid/test/e2e/ssm"
)

const (
	defaultCertName                   = "test-cert"
	defaultCertNamespace              = "cert-test"
	defaultIssuerName                 = "selfsigned-issuer"
	defaultCertSecretName             = "selfsigned-cert-tls"
	defaultNodeMonitoringAgentCommand = "echo 'watchdog: BUG: soft lockup - CPU#6 stuck for 23s! [VM Thread:4054]' | sudo tee -a /dev/kmsg"
	defaultNvidiaDevicePluginCommand  = "nvidia-smi"
)

// AddonEc2Test is a wrapper around the fields needed for addon tests
// that decouples the PeeredVPCTest from specific addon test implementations.
type AddonEc2Test struct {
	*PeeredVPCTest
}

// NewNodeMonitoringAgentTest creates a new NodeMonitoringAgentTest
func (a *AddonEc2Test) NewNodeMonitoringAgentTest() *addon.NodeMonitoringAgentTest {
	commandRunner := ssm.NewStandardLinuxSSHOnSSMCommandRunner(a.SSMClient, a.JumpboxInstanceId, a.Logger)
	labelReq, _ := labels.NewRequirement("os.bottlerocket.aws/version", selection.DoesNotExist, []string{})
	return &addon.NodeMonitoringAgentTest{
		Cluster:       a.Cluster.Name,
		K8S:           a.k8sClient,
		EKSClient:     a.eksClient,
		K8SConfig:     a.K8sClientConfig,
		Logger:        a.Logger,
		Command:       defaultNodeMonitoringAgentCommand,
		CommandRunner: commandRunner,
		NodeFilter:    labels.NewSelector().Add(*labelReq),
	}
}

// NewVerifyPodIdentityAddon creates a new VerifyPodIdentityAddon
func (a *AddonEc2Test) NewVerifyPodIdentityAddon(nodeName string) *addon.VerifyPodIdentityAddon {
	return &addon.VerifyPodIdentityAddon{
		Cluster:             a.Cluster.Name,
		NodeName:            nodeName,
		PodIdentityS3Bucket: a.podIdentityS3Bucket,
		K8S:                 a.k8sClient,
		EKSClient:           a.eksClient,
		IAMClient:           a.iamClient,
		S3Client:            a.s3Client,
		Logger:              a.Logger,
		K8SConfig:           a.K8sClientConfig,
		Region:              a.Cluster.Region,
	}
}

// NewKubeStateMetricsTest creates a new KubeStateMetricsTest
func (a *AddonEc2Test) NewKubeStateMetricsTest() *addon.KubeStateMetricsTest {
	return &addon.KubeStateMetricsTest{
		Cluster:   a.Cluster.Name,
		K8S:       a.k8sClient,
		EKSClient: a.eksClient,
		K8SConfig: a.K8sClientConfig,
		Logger:    a.Logger,
	}
}

// NewMetricsServerTest creates a new MetricsServerTest
func (a *AddonEc2Test) NewMetricsServerTest() *addon.MetricsServerTest {
	metricsClient, err := metricsv1beta1.NewForConfig(a.K8sClientConfig)
	if err != nil {
		a.Logger.Error(err, "Failed to create metrics client")
	}
	return &addon.MetricsServerTest{
		Cluster:       a.Cluster.Name,
		K8S:           a.k8sClient,
		EKSClient:     a.eksClient,
		Logger:        a.Logger,
		MetricsClient: metricsClient,
	}
}

// NewPrometheusNodeExporterTest creates a new PrometheusNodeExporterTest
func (a *AddonEc2Test) NewPrometheusNodeExporterTest() *addon.PrometheusNodeExporterTest {
	return &addon.PrometheusNodeExporterTest{
		Cluster:   a.Cluster.Name,
		K8S:       a.k8sClient,
		EKSClient: a.eksClient,
		K8SConfig: a.K8sClientConfig,
		Logger:    a.Logger,
	}
}

// NewNvidiaDevicePluginTest creates a new NvidiaDevicePluginTest
func (a *AddonEc2Test) NewNvidiaDevicePluginTest(nodeName string) *addon.NvidiaDevicePluginTest {
	commandRunner := ssm.NewStandardLinuxSSHOnSSMCommandRunner(a.SSMClient, a.JumpboxInstanceId, a.Logger)
	return &addon.NvidiaDevicePluginTest{
		Cluster:       a.Cluster.Name,
		K8S:           a.k8sClient,
		EKSClient:     a.eksClient,
		K8SConfig:     a.K8sClientConfig,
		Logger:        a.Logger,
		Command:       defaultNvidiaDevicePluginCommand,
		CommandRunner: commandRunner,
		NodeName:      nodeName,
	}
}

// NewCertManagerTest creates a new CertManagerTest
func (a *AddonEc2Test) NewCertManagerTest(ctx context.Context) (*addon.CertManagerTest, error) {
	// Create cert-manager client
	certClient, err := certmanagerclientset.NewForConfig(a.K8sClientConfig)
	if err != nil {
		a.Logger.Error(err, "Failed to create cert-manager client")
	}

	// Create AWS PCA client
	k8sPcaClient, err := awspcaclientset.NewForConfig(a.K8sClientConfig)
	if err != nil {
		a.Logger.Error(err, "Failed to create AWS PCA client")
	}

	// Create PCA service client
	pcaClient := acmpca.NewFromConfig(a.aws)

	// Get pod identity role ARN
	podIdentityRoleArn, err := addon.PodIdentityRole(ctx, a.iamClient, a.Cluster.Name)
	if err != nil {
		a.Logger.Error(err, "Failed to get pod identity role ARN")
		return nil, err
	}

	// Create PCA Issuer test
	pcaIssuer := &addon.PCAIssuerTest{
		Cluster:            a.Cluster.Name,
		Namespace:          "cert-test",
		K8S:                a.k8sClient,
		EKSClient:          a.eksClient,
		CertClient:         certClient,
		K8sPcaClient:       k8sPcaClient,
		PCAClient:          pcaClient,
		Region:             a.Cluster.Region,
		PodIdentityRoleArn: podIdentityRoleArn,
		Logger:             a.Logger,
	}

	return &addon.CertManagerTest{
		Cluster:        a.Cluster.Name,
		K8S:            a.k8sClient,
		EKSClient:      a.eksClient,
		K8SConfig:      a.K8sClientConfig,
		Logger:         a.Logger,
		CertClient:     certClient,
		PCAIssuer:      pcaIssuer,
		CertName:       defaultCertName,
		CertNamespace:  defaultCertNamespace,
		CertSecretName: defaultCertSecretName,
		IssuerName:     defaultIssuerName,
	}, nil
}
