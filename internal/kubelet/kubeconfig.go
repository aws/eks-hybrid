package kubelet

import (
	"bytes"
	_ "embed"
	"path"
	"text/template"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/iamauthenticator"
	"github.com/aws/eks-hybrid/internal/util"
)

const (
	kubeconfigRoot          = "/var/lib/kubelet"
	kubeconfigFile          = "kubeconfig"
	kubeconfigBootstrapFile = "bootstrap-kubeconfig"
	kubeconfigPerm          = 0644
)

var (
	//go:embed kubeconfig.template.yaml
	kubeconfigTemplateData string
	//go:embed hybrid-kubeconfig.template.yaml
	hybridKubeconfigTemplateData string
	KubeconfigPath               = path.Join(kubeconfigRoot, kubeconfigFile)
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
		k.flags["kubeconfig"] = KubeconfigPath
		return util.WriteFileWithDir(KubeconfigPath, kubeconfig, kubeconfigPerm)
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
		kct.withIamRolesAnywhereHybridVars(cfg)
	} else if cfg.IsSSM() {
		kct.withSsmHybridVars(cfg)
	}
	kct.AwsIamAuthenticatorPath = iamauthenticator.IAMAuthenticatorBinPath
}

func (kct *kubeconfigTemplateVars) withIamRolesAnywhereHybridVars(cfg *api.NodeConfig) {
	kct.Region = cfg.Spec.Cluster.Region
	kct.AwsConfigPath = cfg.Spec.Hybrid.IAMRolesAnywhere.AwsConfigPath
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
