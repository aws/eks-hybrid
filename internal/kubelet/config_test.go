package kubelet

import (
	"testing"

	"github.com/aws/smithy-go/ptr"
	"github.com/stretchr/testify/assert"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/containerd"
)

func TestKubeletCredentialProvidersFeatureFlag(t *testing.T) {
	tests := []struct {
		kubeletVersion string
		expectedValue  *bool
	}{
		{kubeletVersion: "v1.23.0", expectedValue: ptr.Bool(true)},
		{kubeletVersion: "v1.27.0", expectedValue: ptr.Bool(true)},
		{kubeletVersion: "v1.28.0", expectedValue: nil},
	}

	for _, test := range tests {
		kubetConfig := defaultKubeletSubConfig()
		kubetConfig.withVersionToggles(test.kubeletVersion, make(map[string]string))
		kubeletCredentialProviders, present := kubetConfig.FeatureGates["KubeletCredentialProviders"]
		if test.expectedValue == nil && present {
			t.Errorf("KubeletCredentialProviders shouldn't be set for versions %s", test.kubeletVersion)
		} else if test.expectedValue != nil && *test.expectedValue != kubeletCredentialProviders {
			t.Errorf("expected %v but got %v for KubeletCredentialProviders feature gate", *test.expectedValue, kubeletCredentialProviders)
		}
	}
}

func TestContainerRuntime(t *testing.T) {
	tests := []struct {
		kubeletVersion           string
		expectedContainerRuntime *string
	}{
		{kubeletVersion: "v1.26.0", expectedContainerRuntime: ptr.String("remote")},
		{kubeletVersion: "v1.27.0", expectedContainerRuntime: nil},
		{kubeletVersion: "v1.28.0", expectedContainerRuntime: nil},
	}

	for _, test := range tests {
		kubeletAruments := make(map[string]string)
		kubetConfig := defaultKubeletSubConfig()
		kubetConfig.withVersionToggles(test.kubeletVersion, kubeletAruments)
		containerRuntime, present := kubeletAruments["container-runtime"]
		if test.expectedContainerRuntime == nil {
			if present {
				t.Errorf("container-runtime shouldn't be set for versions %s", test.kubeletVersion)
			} else {
				assert.Equal(t, containerd.ContainerRuntimeEndpoint, kubetConfig.ContainerRuntimeEndpoint)
			}
		} else if test.expectedContainerRuntime != nil {
			if *test.expectedContainerRuntime != containerRuntime {
				t.Errorf("expected %v but got %s for container-runtime", *test.expectedContainerRuntime, containerRuntime)
			} else {
				assert.Equal(t, containerd.ContainerRuntimeEndpoint, kubeletAruments["container-runtime-endpoint"])
			}
		}
	}
}

func TestKubeAPILimits(t *testing.T) {
	tests := []struct {
		kubeletVersion       string
		expectedKubeAPIQS    *int
		expectedKubeAPIBurst *int
	}{
		{kubeletVersion: "v1.21.0", expectedKubeAPIQS: nil, expectedKubeAPIBurst: nil},
		{kubeletVersion: "v1.22.0", expectedKubeAPIQS: ptr.Int(10), expectedKubeAPIBurst: ptr.Int(20)},
		{kubeletVersion: "v1.23.0", expectedKubeAPIQS: ptr.Int(10), expectedKubeAPIBurst: ptr.Int(20)},
		{kubeletVersion: "v1.26.0", expectedKubeAPIQS: ptr.Int(10), expectedKubeAPIBurst: ptr.Int(20)},
		{kubeletVersion: "v1.27.0", expectedKubeAPIQS: nil, expectedKubeAPIBurst: nil},
		{kubeletVersion: "v1.28.0", expectedKubeAPIQS: nil, expectedKubeAPIBurst: nil},
	}

	for _, test := range tests {
		kubetConfig := defaultKubeletSubConfig()
		kubetConfig.withVersionToggles(test.kubeletVersion, make(map[string]string))
		assert.Equal(t, test.expectedKubeAPIQS, kubetConfig.KubeAPIQPS)
		assert.Equal(t, test.expectedKubeAPIBurst, kubetConfig.KubeAPIBurst)
	}
}

func TestProviderID(t *testing.T) {
	tests := []struct {
		kubeletVersion        string
		expectedCloudProvider string
	}{
		{kubeletVersion: "v1.23.0", expectedCloudProvider: "aws"},
		{kubeletVersion: "v1.25.0", expectedCloudProvider: "aws"},
		{kubeletVersion: "v1.26.0", expectedCloudProvider: "external"},
		{kubeletVersion: "v1.27.0", expectedCloudProvider: "external"},
	}

	nodeConfig := api.NodeConfig{
		Status: api.NodeConfigStatus{
			Instance: api.InstanceDetails{
				AvailabilityZone: "us-west-2f",
				ID:               "i-123456789000",
			},
		},
	}
	providerId := getProviderId(nodeConfig.Status.Instance.AvailabilityZone, nodeConfig.Status.Instance.ID)

	for _, test := range tests {
		kubeletAruments := make(map[string]string)
		kubetConfig := defaultKubeletSubConfig()
		kubetConfig.withCloudProvider(test.kubeletVersion, &nodeConfig, kubeletAruments)
		assert.Equal(t, test.expectedCloudProvider, kubeletAruments["cloud-provider"])
		if kubeletAruments["cloud-provider"] == "external" {
			assert.Equal(t, *kubetConfig.ProviderID, providerId)
			// TODO assert that the --hostname-override == PrivateDnsName
		}
	}
}

func TestHybridCloudProvider(t *testing.T) {
	nodeConfig := api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Name:   "my-cluster",
				Region: "us-west-2",
			},
			Hybrid: &api.HybridOptions{
				IAMRolesAnywhere: &api.IAMRolesAnywhere{
					NodeName:       "my-node",
					TrustAnchorARN: "arn:aws:iam::222211113333:role/AmazonEKSConnectorAgentRole",
					ProfileARN:     "dummy-profile-arn",
					RoleARN:        "dummy-assume-role-arn",
				},
			},
		},
		Status: api.NodeConfigStatus{
			Hybrid: api.HybridDetails{
				NodeName: "my-node",
			},
		},
	}
	expectedProviderId := "eks-hybrid:///us-west-2/my-cluster/my-node"
	kubeletArgs := make(map[string]string)
	kubeletConfig := defaultKubeletSubConfig()
	kubeletConfig.withHybridCloudProvider(&nodeConfig, kubeletArgs)
	assert.Equal(t, kubeletArgs["cloud-provider"], "")
	assert.Equal(t, kubeletArgs["hostname-override"], nodeConfig.Status.Hybrid.NodeName)
	assert.Equal(t, *kubeletConfig.ProviderID, expectedProviderId)
}

func TestHybridLabels(t *testing.T) {
	nodeConfig := api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Name:   "my-cluster",
				Region: "us-west-2",
			},
			Hybrid: &api.HybridOptions{
				IAMRolesAnywhere: &api.IAMRolesAnywhere{
					NodeName:       "my-node",
					TrustAnchorARN: "arn:aws:iam::222211113333:role/AmazonEKSConnectorAgentRole",
					ProfileARN:     "dummy-profile-arn",
					RoleARN:        "dummy-assume-role-arn",
				},
			},
		},
	}
	expectedLabels := "eks.amazonaws.com/compute-type=hybrid,eks.amazonaws.com/hybrid-credential-provider=iam-ra"
	kubeletArgs := make(map[string]string)
	kubeletConfig := defaultKubeletSubConfig()
	kubeletConfig.withHybridNodeLabels(&nodeConfig, kubeletArgs)
	assert.Equal(t, kubeletArgs["node-labels"], expectedLabels)
}

func TestResolvConf(t *testing.T) {
	resolvConfPath := "/dummy/path/to/resolv.conf"
	kubeletConfig := defaultKubeletSubConfig()
	kubeletConfig.withResolvConf(resolvConfPath)
	assert.Equal(t, kubeletConfig.ResolvConf, resolvConfPath)
}
