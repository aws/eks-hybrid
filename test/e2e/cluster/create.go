package cluster

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/cni"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
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

func (t *TestResources) CreateResources(ctx context.Context) error {
	awsConfig, err := e2e.NewAWSConfig(ctx, t.ClusterRegion)
	if err != nil {
		return fmt.Errorf("initializing AWS config: %w", err)
	}
	cfnClient := cloudformation.NewFromConfig(awsConfig)
	ec2Client := ec2.NewFromConfig(awsConfig)
	eksClient := eks.NewFromConfig(awsConfig)
	logger := e2e.NewLogger()

	resources, err := t.DeployStack(ctx, cfnClient, logger)
	if err != nil {
		return fmt.Errorf("creating architecture: %w", err)
	}

	if err = AcceptVPCPeeringConnection(ctx, ec2Client, resources.VPCPeeringConnection); err != nil {
		return fmt.Errorf("accepting VPC peering connection: %w", err)
	}

	hybridCluster := HybridCluster{
		Name:              t.ClusterName,
		Region:            t.ClusterRegion,
		KubernetesVersion: t.KubernetesVersion,
		SecurityGroup:     resources.ClusterVpcConfig.SecurityGroup,
		SubnetIDs:         []string{resources.ClusterVpcConfig.PublicSubnet, resources.ClusterVpcConfig.PrivateSubnet},
		Role:              resources.ClusterRole,
		HybridNetwork:     t.HybridNetwork,
	}

	logger.Info("Creating EKS hybrid cluster..", "cluster", t.ClusterName)
	err = hybridCluster.CreateCluster(ctx, eksClient, logger)
	if err != nil {
		return fmt.Errorf("creating %s EKS cluster: %v", t.KubernetesVersion, err)
	}

	kubeconfig := KubeconfigPath(t.ClusterName)
	err = hybridCluster.UpdateKubeconfig(kubeconfig)
	if err != nil {
		return fmt.Errorf("saving kubeconfig for %s EKS cluster: %v", t.KubernetesVersion, err)
	}

	clientConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return fmt.Errorf("loading kubeconfig: %v", err)
	}

	dynamicK8s, err := dynamic.NewForConfig(clientConfig)
	if err != nil {
		return fmt.Errorf("creating dynamic Kubernetes client: %v", err)
	}

	switch t.Cni {
	case ciliumCni:
		cilium := cni.NewCilium(dynamicK8s, t.HybridNetwork.PodCidr)
		logger.Info("Installing cilium on cluster...", "cluster", t.ClusterName)
		if err = cilium.Deploy(ctx); err != nil {
			return fmt.Errorf("installing cilium for %s EKS cluster: %v", t.KubernetesVersion, err)
		}
		logger.Info("Cilium installed sucessfully.")
	case calicoCni:
		calico := cni.NewCalico(dynamicK8s, t.HybridNetwork.PodCidr)
		logger.Info("Installing calico on cluster...", "cluster", t.ClusterName)
		if err = calico.Deploy(ctx); err != nil {
			return fmt.Errorf("installing calico for %s EKS cluster: %v", t.KubernetesVersion, err)
		}
		logger.Info("Calico installed sucessfully.")
	}
	logger.Info("Setup finished successfully!")

	return nil
}

func KubeconfigPath(clusterName string) string {
	return fmt.Sprintf("/tmp/%s.kubeconfig", clusterName)
}

func AcceptVPCPeeringConnection(ctx context.Context, client *ec2.Client, peeringConnectionID string) error {
	_, err := client.AcceptVpcPeeringConnection(ctx, &ec2.AcceptVpcPeeringConnectionInput{
		VpcPeeringConnectionId: aws.String(peeringConnectionID),
	}, func(o *ec2.Options) {
		o.Retryer = retry.AddWithErrorCodes(retry.NewStandard(), "InvalidVpcPeeringConnectionID.NotFound")
		o.RetryMaxAttempts = 20
		o.RetryMode = aws.RetryModeAdaptive
	})
	return err
}
