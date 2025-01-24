package cluster

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/go-logr/logr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/addon"

	"github.com/aws/eks-hybrid/test/e2e/cni"
)

type TestResources struct {
	ClusterName       string        `yaml:"clusterName"`
	ClusterRegion     string        `yaml:"clusterRegion"`
	ClusterNetwork    NetworkConfig `yaml:"clusterNetwork"`
	HybridNetwork     NetworkConfig `yaml:"hybridNetwork"`
	KubernetesVersion string        `yaml:"kubernetesVersion"`
	Cni               string        `yaml:"cni"`
	Endpoint          string        `yaml:"endpoint"`
}

type NetworkConfig struct {
	VpcCidr           string `yaml:"vpcCidr"`
	PublicSubnetCidr  string `yaml:"publicSubnetCidr"`
	PrivateSubnetCidr string `yaml:"privateSubnetCidr"`
	PodCidr           string `yaml:"podCidr"`
}

const (
	ciliumCni        = "cilium"
	calicoCni        = "calico"
	podIdentityAddon = "eks-pod-identity-agent"
)

type Create struct {
	logger logr.Logger
	eks    *eks.Client
	stack  *stack
}

// NewCreate creates a new workflow to create an EKS cluster. The EKS client will use
// the specified endpoint or the default endpoint if empty string is passed.
func NewCreate(aws aws.Config, logger logr.Logger, endpoint string) Create {
	return Create{
		logger: logger,
		eks: eks.NewFromConfig(aws, func(o *eks.Options) {
			o.EndpointResolverV2 = &e2e.EksResolverV2{Endpoint: endpoint}
		}),
		stack: &stack{
			cfn:       cloudformation.NewFromConfig(aws),
			logger:    logger,
			ssmClient: ssm.NewFromConfig(aws),
		},
	}
}

func (c *Create) Run(ctx context.Context, test TestResources) error {
	stackOut, err := c.stack.deploy(ctx, test)
	if err != nil {
		return fmt.Errorf("creating stack for cluster infra: %w", err)
	}

	hybridCluster := hybridCluster{
		Name:              test.ClusterName,
		Region:            test.ClusterRegion,
		KubernetesVersion: test.KubernetesVersion,
		SecurityGroup:     stackOut.clusterVpcConfig.securityGroup,
		SubnetIDs:         []string{stackOut.clusterVpcConfig.publicSubnet, stackOut.clusterVpcConfig.privateSubnet},
		Role:              stackOut.clusterRole,
		HybridNetwork:     test.HybridNetwork,
	}

	c.logger.Info("Creating EKS cluster..", "cluster", test.ClusterName)
	err = hybridCluster.create(ctx, c.eks, c.logger)
	if err != nil {
		return fmt.Errorf("creating %s EKS cluster: %w", test.KubernetesVersion, err)
	}

	podIdentityAddon := addon.Addon{
		Cluster:       hybridCluster.Name,
		Name:          podIdentityAddon,
		Configuration: "{\"daemonsets\":{\"hybrid\":{\"create\": true}}}",
	}

	err = podIdentityAddon.Create(ctx, c.eks, c.logger)
	if err != nil {
		return fmt.Errorf("error creating add-on %s for EKS cluster: %w", podIdentityAddon, err)
	}

	kubeconfig := KubeconfigPath(test.ClusterName)
	err = hybridCluster.UpdateKubeconfig(kubeconfig)
	if err != nil {
		return fmt.Errorf("saving kubeconfig for %s EKS cluster: %w", test.KubernetesVersion, err)
	}

	clientConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return fmt.Errorf("loading kubeconfig: %w", err)
	}

	dynamicK8s, err := dynamic.NewForConfig(clientConfig)
	if err != nil {
		return fmt.Errorf("creating dynamic Kubernetes client: %w", err)
	}

	switch test.Cni {
	case ciliumCni:
		cilium := cni.NewCilium(dynamicK8s, test.HybridNetwork.PodCidr, test.ClusterRegion, test.KubernetesVersion)
		c.logger.Info("Installing cilium on cluster...", "cluster", test.ClusterName)
		if err = cilium.Deploy(ctx); err != nil {
			return fmt.Errorf("installing cilium for %s EKS cluster: %w", test.KubernetesVersion, err)
		}
		c.logger.Info("Cilium installed successfully.")
	case calicoCni:
		calico := cni.NewCalico(dynamicK8s, test.HybridNetwork.PodCidr, test.ClusterRegion)
		c.logger.Info("Installing calico on cluster...", "cluster", test.ClusterName)
		if err = calico.Deploy(ctx); err != nil {
			return fmt.Errorf("installing calico for %s EKS cluster: %w", test.KubernetesVersion, err)
		}
		c.logger.Info("Calico installed successfully.")
	}

	return nil
}

func KubeconfigPath(clusterName string) string {
	return fmt.Sprintf("/tmp/%s.kubeconfig", clusterName)
}
