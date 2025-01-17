package peered

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2sdk "github.com/aws/aws-sdk-go-v2/service/ec2"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/go-logr/logr"
	clientgo "k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/commands"
	"github.com/aws/eks-hybrid/test/e2e/ec2"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
	"github.com/aws/eks-hybrid/test/e2e/nodeadm"
	"github.com/aws/eks-hybrid/test/e2e/os"
	"github.com/aws/eks-hybrid/test/e2e/s3"
)

const (
	ec2VolumeSize = int32(30)
)

// Node represents is a Hybrid node running as an EC2 instance in a peered VPC.
type Node struct {
	AWS                 aws.Config
	Cluster             *HybridCluster
	EC2                 *ec2sdk.Client
	SSM                 *ssm.Client
	S3                  *s3sdk.Client
	K8s                 *clientgo.Clientset
	RemoteCommandRunner commands.RemoteCommandRunner
	Logger              logr.Logger
	SkipDelete          bool
	SetRootPassword     bool

	LogsBucket  string
	NodeadmURLs e2e.NodeadmURLs
	PublicKey   string
}

// NodeSpec configures the Hybrid Node.
type NodeSpec struct {
	InstanceName       string
	InstanceProfileARN string
	NodeK8sVersion     string
	NodeNamePrefix     string
	OS                 e2e.NodeadmOS
	Provider           e2e.NodeadmCredentialsProvider
}

// Create spins up an EC2 instance with the proper user data to join as a Hybrid node to the cluster.
func (c Node) Create(ctx context.Context, spec *NodeSpec) (ec2.Instance, error) {
	if c.LogsBucket != "" {
		c.Logger.Info(fmt.Sprintf("Logs bucket: https://%s.console.aws.amazon.com/s3/buckets/%s?prefix=%s/", c.Cluster.Region, c.LogsBucket, c.logsPrefix(spec.InstanceName)))
	}

	nodeSpec := e2e.NodeSpec{
		OS:         spec.OS,
		NamePrefix: spec.NodeNamePrefix,
		Cluster: &e2e.Cluster{
			Name:   c.Cluster.Name,
			Region: c.Cluster.Region,
		},
		Provider: spec.Provider,
	}

	files, err := spec.Provider.FilesForNode(nodeSpec)
	if err != nil {
		return ec2.Instance{}, err
	}

	nodeadmConfig, err := spec.Provider.NodeadmConfig(ctx, nodeSpec)
	if err != nil {
		return ec2.Instance{}, fmt.Errorf("expected to build nodeconfig: %w", err)
	}

	nodeadmConfigYaml, err := yaml.Marshal(&nodeadmConfig)
	if err != nil {
		return ec2.Instance{}, fmt.Errorf("expected to successfully marshal nodeadm config to YAML: %w", err)
	}

	var rootPasswordHash string
	if c.SetRootPassword {
		var rootPassword string
		rootPassword, rootPasswordHash, err = os.GenerateOSPassword()
		if err != nil {
			return ec2.Instance{}, fmt.Errorf("expected to successfully generate root password: %w", err)
		}
		c.Logger.Info(fmt.Sprintf("Instance Root Password: %s", rootPassword))
	}

	userdata, err := spec.OS.BuildUserData(e2e.UserDataInput{
		KubernetesVersion: spec.NodeK8sVersion,
		NodeadmUrls:       c.NodeadmURLs,
		NodeadmConfigYaml: string(nodeadmConfigYaml),
		Provider:          string(spec.Provider.Name()),
		RootPasswordHash:  rootPasswordHash,
		Files:             files,
		PublicKey:         c.PublicKey,
	})
	if err != nil {
		return ec2.Instance{}, fmt.Errorf("expected to successfully build user data: %w", err)
	}

	amiId, err := spec.OS.AMIName(ctx, c.AWS)
	if err != nil {
		return ec2.Instance{}, fmt.Errorf("expected to successfully retrieve ami id: %w", err)
	}

	ec2Input := ec2.InstanceConfig{
		ClusterName:        c.Cluster.Name,
		InstanceName:       spec.InstanceName,
		AmiID:              amiId,
		InstanceType:       spec.OS.InstanceType(c.Cluster.Region),
		VolumeSize:         ec2VolumeSize,
		SubnetID:           c.Cluster.SubnetID,
		SecurityGroupID:    c.Cluster.SecurityGroupID,
		UserData:           userdata,
		InstanceProfileARN: spec.InstanceProfileARN,
	}

	c.Logger.Info("Creating a hybrid EC2 Instance...")
	instance, err := ec2Input.Create(ctx, c.EC2, c.SSM)
	if err != nil {
		return ec2.Instance{}, fmt.Errorf("EC2 Instance should have been created successfully: %w", err)
	}
	c.Logger.Info(fmt.Sprintf("EC2 Instance Connect: https://%s.console.aws.amazon.com/ec2-instance-connect/ssh?connType=serial&instanceId=%s&region=%s&serialPort=0", c.Cluster.Region, instance.ID, c.Cluster.Region))

	return instance, nil
}

// Cleanup collects logs and deletes the EC2 instance and Node object.
func (c *Node) Cleanup(ctx context.Context, instance ec2.Instance) error {
	logCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err := c.collectLogs(logCtx, "bundle", instance)
	if err != nil {
		c.Logger.Error(err, "issue collecting logs")
	}
	if c.SkipDelete {
		c.Logger.Info("Skipping EC2 Instance deletion", "instanceID", instance.ID)
		return nil
	}
	c.Logger.Info("Deleting EC2 Instance", "instanceID", instance.ID)
	if err := ec2.DeleteEC2Instance(ctx, c.EC2, instance.ID); err != nil {
		return fmt.Errorf("deleting EC2 Instance: %w", err)
	}
	c.Logger.Info("Successfully deleted EC2 Instance", "instanceID", instance.ID)
	if err := kubernetes.EnsureNodeWithIPIsDeleted(ctx, c.K8s, instance.IP); err != nil {
		return fmt.Errorf("deleting node for instance %s: %w", instance.ID, err)
	}

	return nil
}

func (c Node) logsPrefix(instanceName string) string {
	return fmt.Sprintf("logs/%s/%s", c.Cluster.Name, instanceName)
}

func (c Node) collectLogs(ctx context.Context, bundleName string, instance ec2.Instance) error {
	if c.LogsBucket == "" {
		return nil
	}
	key := fmt.Sprintf("%s/%s.tar.gz", c.logsPrefix(instance.Name), bundleName)
	url, err := s3.GeneratePutLogsPreSignedURL(ctx, c.S3, c.LogsBucket, key, 5*time.Minute)
	if err != nil {
		return err
	}
	err = nodeadm.RunLogCollector(ctx, c.RemoteCommandRunner, instance.IP, url)
	if err != nil {
		return err
	}
	return nil
}
