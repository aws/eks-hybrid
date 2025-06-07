package kubelet

import (
	"bytes"
	_ "embed"
	"path"
	"text/template"

	"github.com/pkg/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/iamauthenticator"
	"github.com/aws/eks-hybrid/internal/iamrolesanywhere"
	"github.com/aws/eks-hybrid/internal/util"
)

const (
	kubeconfigRoot          = "/var/lib/kubelet"
	kubeconfigFile          = "kubeconfig"
	kubeconfigBootstrapFile = "bootstrap-kubeconfig"
	kubeconfigPerm          = 0o644
)

var (
	//go:embed kubeconfig.template.yaml
	kubeconfigTemplateData string
	//go:embed hybrid-kubeconfig.template.yaml
	hybridKubeconfigTemplateData string
	kubeconfigPath               = path.Join(kubeconfigRoot, kubeconfigFile)
	kubeconfigBootstrapPath      = path.Join(kubeconfigRoot, kubeconfigBootstrapFile)
)

func (k *kubelet) writeKubeconfig() error {
	kubeconfig, err := generateKubeconfig(k.nodeConfig)
	if err != nil {
		return err
	}
	if k.nodeConfig.IsOutpostNode() {
		// kubelet bootstrap kubeconfig uses aws-iam-authenticator with cluster id to authenticate to cluster
		//   - if "aws eks describe-cluster" is bypassed, for local outpost, the value of CLUSTER_NAME parameter will be cluster id.
		//   - otherwise, the cluster id will use the id returned by "aws eks describe-cluster".
		k.flags["bootstrap-kubeconfig"] = kubeconfigBootstrapPath
		return util.WriteFileWithDir(kubeconfigBootstrapPath, kubeconfig, kubeconfigPerm)
	} else {
		k.flags["kubeconfig"] = kubeconfigPath
		return util.WriteFileWithDir(kubeconfigPath, kubeconfig, kubeconfigPerm)
	}
}

type kubeconfigTemplateVars struct {
	Cluster                 string
	Region                  string
	APIServerEndpoint       string
	CaCertPath              string
	SessionName             string
	AssumeRole              string
	AwsConfigPath           string
	AwsIamAuthenticatorPath string
	AwsProfile              string
}

func newKubeconfigTemplateVars(cfg *api.NodeConfig) *kubeconfigTemplateVars {
	return &kubeconfigTemplateVars{
		Cluster:           cfg.Spec.Cluster.Name,
		Region:            cfg.Status.Instance.Region,
		APIServerEndpoint: cfg.Spec.Cluster.APIServerEndpoint,
		CaCertPath:        caCertificatePath,
	}
}

func (kct *kubeconfigTemplateVars) withOutpostVars(cfg *api.NodeConfig) {
	kct.Cluster = cfg.Spec.Cluster.ID
}

func (kct *kubeconfigTemplateVars) withHybridTemplateVars(cfg *api.NodeConfig) {
	if cfg.IsIAMRolesAnywhere() {
		kct.withIamRolesAnywhereHybridVars(cfg, iamrolesanywhere.ProfileName)
	} else if cfg.IsSSM() {
		kct.withSsmHybridVars(cfg)
	}
	kct.AwsIamAuthenticatorPath = iamauthenticator.IAMAuthenticatorBinPath
}

func (kct *kubeconfigTemplateVars) withIamRolesAnywhereHybridVars(cfg *api.NodeConfig, awsProfile string) {
	kct.Region = cfg.Spec.Cluster.Region
	kct.AwsConfigPath = cfg.Spec.Hybrid.IAMRolesAnywhere.AwsConfigPath
	kct.AwsProfile = awsProfile
}

func (kct *kubeconfigTemplateVars) withSsmHybridVars(cfg *api.NodeConfig) {
	kct.Region = cfg.Spec.Cluster.Region
}

func generateKubeconfig(cfg *api.NodeConfig) ([]byte, error) {
	config := newKubeconfigTemplateVars(cfg)
	if cfg.IsOutpostNode() {
		config.withOutpostVars(cfg)
	}

	if cfg.IsHybridNode() {
		config.withHybridTemplateVars(cfg)
	}

	var buf bytes.Buffer
	var kubeconfigTemplate *template.Template
	// SSM based hybrid nodes can still use the normal eks get-token api for authentication
	if cfg.IsHybridNode() {
		kubeconfigTemplate = template.Must(template.New(kubeconfigFile).Parse(hybridKubeconfigTemplateData))
	} else {
		kubeconfigTemplate = template.Must(template.New(kubeconfigFile).Parse(kubeconfigTemplateData))
	}
	if err := kubeconfigTemplate.Execute(&buf, config); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type KubeClientOptions struct {
	awsEnvVars map[string]string
}

type KubeClientOption func(*KubeClientOptions)

func WithAwsEnvironmentVariables(envVars map[string]string) KubeClientOption {
	return func(o *KubeClientOptions) {
		o.awsEnvVars = envVars
	}
}

// GetKubeClientFromKubeConfig gets kubernetes client from kubeconfig on the disk
func GetKubeClientFromKubeConfig(opts ...KubeClientOption) (kubernetes.Interface, error) {
	options := &KubeClientOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// Load the kubeconfig file
	kubeconfigPath := KubeconfigPath()

	// Apply AWS environment variables if provided
	if len(options.awsEnvVars) > 0 {
		// Load the raw kubeconfig to modify it
		rawConfig, err := clientcmd.LoadFromFile(kubeconfigPath)
		if err != nil {
			return nil, errors.Wrap(err, "loading kubeconfig file")
		}

		// Apply environment variables to the auth info
		if authInfo, exists := rawConfig.AuthInfos["kubelet"]; exists && authInfo.Exec != nil {
			envVars := make([]clientcmdapi.ExecEnvVar, 0, len(options.awsEnvVars))
			for name, value := range options.awsEnvVars {
				envVars = append(envVars, clientcmdapi.ExecEnvVar{
					Name:  name,
					Value: value,
				})
			}
			authInfo.Exec.Env = append(authInfo.Exec.Env, envVars...)
		}

		clientConfig := clientcmd.NewDefaultClientConfig(*rawConfig, &clientcmd.ConfigOverrides{})
		restConfig, err := clientConfig.ClientConfig()
		if err != nil {
			return nil, errors.Wrap(err, "building config from kubeconfig")
		}

		return kubernetes.NewForConfig(restConfig)
	}

	// Use the current context in the kubeconfig file (simple case without env vars)
	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, errors.Wrap(err, "building client for kubelet from default kubeconfig path")
	}

	return kubernetes.NewForConfig(restConfig)
}

// KubeconfigPath returns the path to the kubeconfig file used by the kubelet.
func KubeconfigPath() string {
	return kubeconfigPath
}

// Kubeconfig is the default kubeconfig generated for the kubelet.
type Kubeconfig struct{}

// Path returns the path to the kubeconfig file used by the kubelet.
func (d Kubeconfig) Path() string {
	return KubeconfigPath()
}

// BuildClient builds a new Kubernetes client from the kubeconfig.
func (d Kubeconfig) BuildClient() (kubernetes.Interface, error) {
	return GetKubeClientFromKubeConfig()
}
