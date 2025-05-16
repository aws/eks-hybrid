package e2e

import (
	"context"
	_ "embed"

	"github.com/aws/aws-sdk-go-v2/aws"
	corev1 "k8s.io/api/core/v1"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/creds"
	"github.com/aws/eks-hybrid/test/e2e/commands"
)

// NodeadmOS defines an interface for operating system-specific behavior.
type NodeadmOS interface {
	Name() string
	AMIName(ctx context.Context, awsConfig aws.Config, kubernetesVersion string) (string, error)
	BuildUserData(userDataInput UserDataInput) ([]byte, error)
	InstanceType(region string, instanceSize InstanceSize) string
	RebootInstance(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error
	CollectLogs(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, logBundleUrl string) error
	Uninstall(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error
	GetNodeadmVersion(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) (string, error)
	RunNodeadmDebug(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error
	ShouldRunNodeadmDebug() bool
	Upgrade(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, kubernetesVersion string) error
	PodIdentityAgentDaemonsetName() string
}

type InstanceSize int

const (
	Large InstanceSize = iota
	XLarge
)

type UserDataInput struct {
	CredsProviderName string
	EKSEndpoint       string
	KubernetesVersion string
	NodeadmUrls       NodeadmURLs
	NodeadmConfig     *api.NodeConfig
	NodeadmConfigYaml string
	Provider          string
	PublicKey         string
	Region            string
	RootPasswordHash  string
	Files             []File

	KubernetesAPIServer string
	HostName            string
	ClusterName         string
	ClusterCert         []byte
}

type NodeadmURLs struct {
	AMD string
	ARM string
}

type LogsUploadUrl struct {
	Name string
	Url  string
}

// File represents a file in disk.
type File struct {
	Content     string
	Path        string
	Permissions string
}

type NodeadmCredentialsProvider interface {
	Name() creds.CredentialProvider
	NodeadmConfig(ctx context.Context, node NodeSpec) (*api.NodeConfig, error)
	VerifyUninstall(ctx context.Context, instanceId string) error
	FilesForNode(spec NodeSpec) ([]File, error)
}

// HybridEC2Node represents a Hybrid Node backed by an EC2 instance.
type HybridEC2Node struct {
	Node corev1.Node
}

// NodeSpec is a specification for a node.
type NodeSpec struct {
	Cluster  *Cluster
	Name     string
	OS       CredsOS
	Provider NodeadmCredentialsProvider
}

type Cluster struct {
	Name   string
	Region string
}

// CredsOS is the Node OS.
type CredsOS interface {
	Name() string
}
