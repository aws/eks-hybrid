package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"gopkg.in/yaml.v2"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/aws/eks-hybrid/test/e2e/cni"
)

const TestClusterTagKey = "Nodeadm-E2E-Tests-Cluster"

type TestRunner struct {
	Config  awsconfig.Config   `yaml:"-"`
	Session *session.Session   `yaml:"-"`
	Spec    TestResourceSpec   `yaml:"spec"`
	Status  TestResourceStatus `yaml:"status"`
}

type TestResourceSpec struct {
	ClusterName       string        `yaml:"clusterName"`
	ClusterRegion     string        `yaml:"clusterRegion"`
	ClusterNetwork    NetworkConfig `yaml:"clusterNetwork"`
	HybridNetwork     NetworkConfig `yaml:"hybridNetwork"`
	KubernetesVersion string        `yaml:"kubernetesVersion"`
	Cni               string        `yaml:"cni"`
}

type TestResourceStatus struct {
	ClusterVpcID     string   `yaml:"clusterVpcID"`
	ClusterSubnetIDs []string `yaml:"clusterSubnetIDs"`
	HybridVpcID      string   `yaml:"hybridVpcID"`
	HybridSubnetIDs  []string `yaml:"hybridSubnetIDs"`
	PeeringConnID    string   `yaml:"peeringConnID"`
	RoleArn          string   `yaml:"roleArn"`
}

type NetworkConfig struct {
	VpcCidr           string `yaml:"vpcCidr"`
	PrivateSubnetCidr string `yaml:"privateSubnetCidr"`
	PublicSubnetCidr  string `yaml:"publicSubnetCidr"`
	PodCidr           string `yaml:"podCidr"`
}

const (
	outputDir = "/tmp"
	ciliumCni = "cilium"
	calicoCni = "calico"
)

func (t *TestRunner) NewAWSSession() (*session.Session, error) {
	// Create a new session using shared credentials or environment variables
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(t.Spec.ClusterRegion),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create new AWS session: %v", err)
	}

	// Optionally, you can log the region for debugging purposes
	fmt.Printf("AWS session initialized in region: %s\n", t.Spec.ClusterRegion)

	return sess, nil
}

func (t *TestRunner) NewAWSConfig(ctx context.Context) (awsconfig.Config, error) {
	// Create a new config using shared credentials or environment variables
	config, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(t.Spec.ClusterRegion))
	if err != nil {
		return nil, fmt.Errorf("failed to create new AWS config: %v", err)
	}

	// Optionally, you can log the region for debugging purposes
	fmt.Printf("AWS config initialized in region: %s\n", t.Spec.ClusterRegion)

	return config, nil
}

func (t *TestRunner) CreateResources(ctx context.Context) error {
	ec2Client := ec2.New(t.Session)

	fmt.Println("Creating EKS cluster IAM Role...")
	err := t.createEKSClusterRole()
	if err != nil {
		return fmt.Errorf("error creating IAM role: %v", err)
	}

	// Create EKS cluster VPC
	clusterVpcParam := vpcSubnetParams{
		clusterName:       t.Spec.ClusterName,
		vpcName:           fmt.Sprintf("%s-vpc", t.Spec.ClusterName),
		vpcCidr:           t.Spec.ClusterNetwork.VpcCidr,
		publicSubnetCidr:  t.Spec.ClusterNetwork.PublicSubnetCidr,
		privateSubnetCidr: t.Spec.ClusterNetwork.PrivateSubnetCidr,
	}
	fmt.Println("Creating EKS cluster VPC...")
	clusterVpcConfig, err := t.createVPCResources(ec2Client, clusterVpcParam)
	if err != nil {
		return fmt.Errorf("error creating cluster VPC: %v", err)
	}
	t.Status.ClusterVpcID = clusterVpcConfig.vpcID
	t.Status.ClusterSubnetIDs = clusterVpcConfig.subnetIDs

	// Update cluster security group with hybrid node's vpc cidr to allow access to ec2 nodes.
	clusterPermissions := []*ec2.IpPermission{
		{
			IpProtocol: aws.String("tcp"),
			FromPort:   aws.Int64(443),
			ToPort:     aws.Int64(443),
			IpRanges: []*ec2.IpRange{
				{
					CidrIp: aws.String(t.Spec.HybridNetwork.VpcCidr),
				},
				{
					CidrIp: aws.String(t.Spec.HybridNetwork.PodCidr),
				},
			},
		},
	}
	clusterSecurityGroupID, err := getAttachedDefaultSecurityGroup(ctx, ec2Client, clusterVpcConfig.vpcID)
	if err != nil {
		return fmt.Errorf("error getting default security group to vpc %s: %v", clusterVpcConfig.vpcID, err)
	}

	if err = addIngressRules(ctx, ec2Client, clusterSecurityGroupID, clusterPermissions); err != nil {
		return fmt.Errorf("error updating cluster security group associated with the vpc %s with the rules: %v", clusterVpcConfig.vpcID, err)
	}

	// Create hybrid nodes VPC
	hybridNodesVpcParam := vpcSubnetParams{
		clusterName:       t.Spec.ClusterName,
		vpcName:           fmt.Sprintf("%s-hybrid-node-vpc", t.Spec.ClusterName),
		vpcCidr:           t.Spec.HybridNetwork.VpcCidr,
		publicSubnetCidr:  t.Spec.HybridNetwork.PublicSubnetCidr,
		privateSubnetCidr: t.Spec.HybridNetwork.PrivateSubnetCidr,
	}
	fmt.Println("Creating EC2 hybrid nodes VPC...")
	hybridNodesVpcConfig, err := t.createVPCResources(ec2Client, hybridNodesVpcParam)
	if err != nil {
		return fmt.Errorf("error creating EC2 hybrid nodes VPC: %v", err)
	}
	t.Status.HybridVpcID = hybridNodesVpcConfig.vpcID
	t.Status.HybridSubnetIDs = hybridNodesVpcConfig.subnetIDs

	// Update hybrid node security group with cluster's vpc cidr to allow access to CP nodes.
	hybridNodePermissions := []*ec2.IpPermission{
		{
			IpProtocol: aws.String("tcp"),
			FromPort:   aws.Int64(10250),
			ToPort:     aws.Int64(10250),
			IpRanges: []*ec2.IpRange{
				{
					CidrIp: aws.String(clusterVpcParam.vpcCidr),
				},
			},
		},
	}
	hybridSecurityGroupID, err := getAttachedDefaultSecurityGroup(ctx, ec2Client, hybridNodesVpcConfig.vpcID)
	if err != nil {
		return fmt.Errorf("error getting default security group to vpc %s: %v", hybridNodesVpcConfig.vpcID, err)
	}

	if err = addIngressRules(ctx, ec2Client, hybridSecurityGroupID, hybridNodePermissions); err != nil {
		return fmt.Errorf("error updating cluster security group associated with the vpc %s with the rules: %v", hybridNodesVpcConfig.vpcID, err)
	}

	// Create VPC Peering Connection between the cluster VPC and EC2 hybrid nodes VPC
	fmt.Println("Creating VPC peering connection...")
	t.Status.PeeringConnID, err = t.createVPCPeering(ctx)
	if err != nil {
		return fmt.Errorf("error creating VPC peering connection: %v", err)
	}

	// Update route tables for peering connection
	fmt.Println("Updating route tables for VPC peering...")
	err = t.updateRouteTablesForPeering()
	if err != nil {
		return fmt.Errorf("error updating route tables: %v", err)
	}

	// Create the EKS Cluster using the IAM role and VPC
	fmt.Printf("Creating EKS hybrid cluster %s with the kubernetes version %s..\n", t.Spec.ClusterName, t.Spec.KubernetesVersion)
	err = t.createEKSCluster(ctx, t.Spec.ClusterName, t.Spec.KubernetesVersion, clusterSecurityGroupID)
	if err != nil {
		return fmt.Errorf("creating %s EKS cluster: %v", t.Spec.KubernetesVersion, err)
	}

	// Wait for the cluster to be ready
	fmt.Println("Waiting for cluster to be ready...")
	err = t.waitForClusterCreation(t.Spec.ClusterName)
	if err != nil {
		return fmt.Errorf("while waiting for cluster creation: %v", err)
	}

	err = updateKubeconfig(t.Spec.ClusterName, t.Spec.ClusterRegion)
	if err != nil {
		return fmt.Errorf("saving kubeconfig for %s EKS cluster: %v", t.Spec.KubernetesVersion, err)
	}

	clientConfig, err := clientcmd.BuildConfigFromFlags("", KubeconfigPath(t.Spec.ClusterName))
	if err != nil {
		return fmt.Errorf("loading kubeconfig: %v", err)
	}

	dynamicK8s, err := dynamic.NewForConfig(clientConfig)
	if err != nil {
		return fmt.Errorf("creating dynamic Kubernetes client: %v", err)
	}

	switch t.Spec.Cni {
	case ciliumCni:
		cilium := cni.NewCilium(dynamicK8s, t.Spec.HybridNetwork.PodCidr)
		fmt.Printf("Installing cilium on cluster %s...\n", t.Spec.ClusterName)
		if err = cilium.Deploy(ctx); err != nil {
			return fmt.Errorf("installing cilium for %s EKS cluster: %v", t.Spec.KubernetesVersion, err)
		}
		fmt.Println("Cilium installed sucessfully.")
	case calicoCni:
		calico := cni.NewCalico(dynamicK8s, t.Spec.HybridNetwork.PodCidr)
		fmt.Printf("Installing calico on cluster %s...\n", t.Spec.ClusterName)
		if err = calico.Deploy(ctx); err != nil {
			return fmt.Errorf("installing calico for %s EKS cluster: %v", t.Spec.KubernetesVersion, err)
		}
		fmt.Println("Calico installed sucessfully.")
	}
	fmt.Println("Cilium installed sucessfully.")

	// After resources are created, write the config to a file
	configFilePath := filepath.Join(outputDir, "setup-resources-output.yaml")
	if err := t.saveSetupConfigAsYAML(configFilePath); err != nil {
		return fmt.Errorf("writing config to file: %v", err)
	}

	return nil
}

// saveKubeconfig saves the kubeconfig for the cluster
func updateKubeconfig(clusterName, region string) error {
	cmd := exec.Command("aws", "eks", "update-kubeconfig", "--name", clusterName, "--region", region, "--kubeconfig", KubeconfigPath(clusterName))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func KubeconfigPath(clusterName string) string {
	return fmt.Sprintf("/tmp/%s.kubeconfig", clusterName)
}

func (t *TestRunner) saveSetupConfigAsYAML(outputFile string) error {
	testRunnerContent, err := yaml.Marshal(t)
	if err != nil {
		return fmt.Errorf("error marshaling test runner config: %v", err)
	}
	if err = os.WriteFile(outputFile, testRunnerContent, 0o644); err != nil {
		return err
	}

	fmt.Printf("Successfully saved resource configuration to %s\n", outputFile)
	return nil
}

// replaceDotsWithDashes replaces dots in the Kubernetes version with dashes
func replaceDotsWithDashes(version string) string {
	return strings.Replace(version, ".", "-", -1)
}

func GetTruncatedName(name string, limit int) string {
	if len(name) > limit {
		name = name[:limit]
	}
	return name
}
