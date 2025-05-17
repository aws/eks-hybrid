package addon

import (
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	s3v2 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-logr/logr"
	"k8s.io/client-go/dynamic"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// AddonEc2Test is a wrapper around the fields needed for addon tests
// that decouples the PeeredVPCTest from specific addon test implementations.
type AddonEc2Test struct {
	clusterName         string
	k8s                 clientgo.Interface
	dynamicK8s          dynamic.Interface
	eksClient           *eks.Client
	iamClient           *iam.Client
	s3Client            *s3v2.Client
	logger              logr.Logger
	k8sConfig           *rest.Config
	region              string
	podIdentityS3Bucket string
}

// NewAddonEc2Test creates a new addon tests wrapper
func NewAddonEc2Test(
	clusterName string,
	k8s clientgo.Interface,
	dynamicK8s dynamic.Interface,
	eksClient *eks.Client,
	iamClient *iam.Client,
	s3Client *s3v2.Client,
	logger logr.Logger,
	k8sConfig *rest.Config,
	region string,
	podIdentityS3Bucket string,
) *AddonEc2Test {
	return &AddonEc2Test{
		clusterName:         clusterName,
		k8s:                 k8s,
		dynamicK8s:          dynamicK8s,
		eksClient:           eksClient,
		iamClient:           iamClient,
		s3Client:            s3Client,
		logger:              logger,
		k8sConfig:           k8sConfig,
		region:              region,
		podIdentityS3Bucket: podIdentityS3Bucket,
	}
}

// NewNodeMonitoringAgentTest creates a new NodeMonitoringAgentTest
func (a *AddonEc2Test) NewNodeMonitoringAgentTest() *NodeMonitoringAgentTest {
	return &NodeMonitoringAgentTest{
		Cluster:   a.clusterName,
		K8S:       a.k8s,
		EKSClient: a.eksClient,
		K8SConfig: a.k8sConfig,
		Logger:    a.logger,
	}
}

// NewVerifyPodIdentityAddon creates a new VerifyPodIdentityAddon
func (a *AddonEc2Test) NewVerifyPodIdentityAddon(nodeName string) *VerifyPodIdentityAddon {
	return &VerifyPodIdentityAddon{
		Cluster:             a.clusterName,
		NodeName:            nodeName,
		PodIdentityS3Bucket: a.podIdentityS3Bucket,
		K8S:                 a.k8s,
		EKSClient:           a.eksClient,
		IAMClient:           a.iamClient,
		S3Client:            a.s3Client,
		Logger:              a.logger,
		K8SConfig:           a.k8sConfig,
		Region:              a.region,
	}
}
