package cluster

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/go-logr/logr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/aws/eks-hybrid/test/e2e/cni"
)

type TestResources struct {
	ClusterName       string        `yaml:"clusterName"`
	ClusterRegion     string        `yaml:"clusterRegion"`
	ClusterNetwork    NetworkConfig `yaml:"clusterNetwork"`
	HybridNetwork     NetworkConfig `yaml:"hybridNetwork"`
	KubernetesVersion string        `yaml:"kubernetesVersion"`
	Cni               string        `yaml:"cni"`
}

type NetworkConfig struct {
	VpcCidr           string `yaml:"vpcCidr"`
	PublicSubnetCidr  string `yaml:"publicSubnetCidr"`
	PrivateSubnetCidr string `yaml:"privateSubnetCidr"`
	PodCidr           string `yaml:"podCidr"`
}

const (
	outputDir = "/tmp"
	ciliumCni = "cilium"
	calicoCni = "calico"
)

type Create struct {
	logger logr.Logger
	eks    *eks.Client
	stack  *stack
}

func NewCreate(aws aws.Config, logger logr.Logger) Create {
	return Create{
		eks: eks.NewFromConfig(aws),
		stack: &stack{
			cfn:    cloudformation.NewFromConfig(aws),
			logger: logger,
		},
	}
}

func (c *Create) Run(ctx context.Context, test TestResources) error {
	resources, err := c.stack.deploy(ctx, test)
	if err != nil {
		return fmt.Errorf("creating architecture: %w", err)
	}

	hybridCluster := hybridCluster{
		Name:              test.ClusterName,
		Region:            test.ClusterRegion,
		KubernetesVersion: test.KubernetesVersion,
		SecurityGroup:     resources.clusterVpcConfig.securityGroup,
		SubnetIDs:         []string{resources.clusterVpcConfig.publicSubnet, resources.clusterVpcConfig.privateSubnet},
		Role:              resources.clusterRole,
		HybridNetwork:     test.HybridNetwork,
	}

	c.logger.Info("Creating EKS hybrid cluster..", "cluster", test.ClusterName)
	err = hybridCluster.create(ctx, c.eks, c.logger)
	if err != nil {
		return fmt.Errorf("creating %s EKS cluster: %v", test.KubernetesVersion, err)
	}

	kubeconfig := KubeconfigPath(test.ClusterName)
	err = hybridCluster.UpdateKubeconfig(kubeconfig)
	if err != nil {
		return fmt.Errorf("saving kubeconfig for %s EKS cluster: %v", test.KubernetesVersion, err)
	}

	clientConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return fmt.Errorf("loading kubeconfig: %v", err)
	}

	dynamicK8s, err := dynamic.NewForConfig(clientConfig)
	if err != nil {
		return fmt.Errorf("creating dynamic Kubernetes client: %v", err)
	}

	switch test.Cni {
	case ciliumCni:
		cilium := cni.NewCilium(dynamicK8s, test.HybridNetwork.PodCidr)
		c.logger.Info("Installing cilium on cluster...", "cluster", test.ClusterName)
		if err = cilium.Deploy(ctx); err != nil {
			return fmt.Errorf("installing cilium for %s EKS cluster: %v", test.KubernetesVersion, err)
		}
		c.logger.Info("Cilium installed sucessfully.")
	case calicoCni:
		calico := cni.NewCalico(dynamicK8s, test.HybridNetwork.PodCidr)
		c.logger.Info("Installing calico on cluster...", "cluster", test.ClusterName)
		if err = calico.Deploy(ctx); err != nil {
			return fmt.Errorf("installing calico for %s EKS cluster: %v", test.KubernetesVersion, err)
		}
		c.logger.Info("Calico installed sucessfully.")
	}
	c.logger.Info("Setup finished successfully!")

	return nil
}

func KubeconfigPath(clusterName string) string {
	return fmt.Sprintf("/tmp/%s.kubeconfig", clusterName)
}
