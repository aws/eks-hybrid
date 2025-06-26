package suite

import (
	certmanagerclientset "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/aws/eks-hybrid/test/e2e/addon"
	"github.com/aws/eks-hybrid/test/e2e/ssm"
)

// AddonEc2Test is a wrapper around the fields needed for addon tests
// that decouples the PeeredVPCTest from specific addon test implementations.
type AddonEc2Test struct {
	*PeeredVPCTest
}

// NewNodeMonitoringAgentTest creates a new NodeMonitoringAgentTest
func (a *AddonEc2Test) NewNodeMonitoringAgentTest() *addon.NodeMonitoringAgentTest {
	commandRunner := ssm.NewStandardLinuxSSHOnSSMCommandRunner(a.SSMClient, a.Jumpbox.Id, a.Logger)
	return &addon.NodeMonitoringAgentTest{
		Cluster:       a.Cluster.Name,
		K8S:           a.k8sClient,
		EKSClient:     a.eksClient,
		K8SConfig:     a.K8sClientConfig,
		Logger:        a.Logger,
		CommandRunner: commandRunner,
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
	commandRunner := ssm.NewStandardLinuxSSHOnSSMCommandRunner(a.SSMClient, a.Jumpbox.Id, a.Logger)
	return &addon.NvidiaDevicePluginTest{
		Cluster:       a.Cluster.Name,
		K8S:           a.k8sClient,
		EKSClient:     a.eksClient,
		K8SConfig:     a.K8sClientConfig,
		Logger:        a.Logger,
		CommandRunner: commandRunner,
		NodeName:      nodeName,
	}
}

// NewCertManagerTest creates a new CertManagerTest
func (a *AddonEc2Test) NewCertManagerTest() *addon.CertManagerTest {
	// Create cert-manager client
	certClient, err := certmanagerclientset.NewForConfig(a.K8sClientConfig)
	if err != nil {
		a.Logger.Error(err, "Failed to create cert-manager client")
	}

	return &addon.CertManagerTest{
		Cluster:    a.Cluster.Name,
		K8S:        a.k8sClient,
		EKSClient:  a.eksClient,
		K8SConfig:  a.K8sClientConfig,
		Logger:     a.Logger,
		CertClient: certClient,
	}
}
