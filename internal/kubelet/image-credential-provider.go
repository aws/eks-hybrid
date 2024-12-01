package kubelet

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"text/template"

	"go.uber.org/zap"
	"golang.org/x/mod/semver"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/util"
)

const (
	// #nosec G101 //constant path, not credential
	imageCredentialProviderRoot = "/etc/eks/image-credential-provider"
	// #nosec G101 //constant path, not credential
	imageCredentialProviderConfig = "config.json"
	imageCredentialProviderPerm   = 0o644
	// #nosec G101 //constant path, not credential
	ecrCredentialProviderBinPathEnvironmentName = "ECR_CREDENTIAL_PROVIDER_BIN_PATH"
)

var (
	//go:embed image-credential-provider.template.json
	imageCredentialProviderTemplateData string
	//go:embed hybrid-roles-anywhere-image-credential-provider.template.json
	hybridRolesAnywhereImageCredentialProviderTemplateData string
	imageCredentialProviderConfigPath                      = path.Join(imageCredentialProviderRoot, imageCredentialProviderConfig)
)

func (k *kubelet) writeImageCredentialProviderConfig() error {
	// fallback default for image credential provider binary if not overridden
	ecrCredentialProviderBinPath := path.Join(imageCredentialProviderRoot, "ecr-credential-provider")
	if binPath, set := os.LookupEnv(ecrCredentialProviderBinPathEnvironmentName); set {
		zap.L().Info("picked up image credential provider binary path from environment", zap.String("bin-path", binPath))
		ecrCredentialProviderBinPath = binPath
	}
	if err := ensureCredentialProviderBinaryExists(ecrCredentialProviderBinPath); err != nil {
		return err
	}

	config, err := generateImageCredentialProviderConfig(k.nodeConfig, ecrCredentialProviderBinPath)
	if err != nil {
		return err
	}

	k.flags["image-credential-provider-bin-dir"] = path.Dir(ecrCredentialProviderBinPath)
	k.flags["image-credential-provider-config"] = imageCredentialProviderConfigPath

	return util.WriteFileWithDir(imageCredentialProviderConfigPath, config, imageCredentialProviderPerm)
}

type imageCredentialProviderTemplateVars struct {
	ConfigApiVersion   string
	ProviderApiVersion string
	EcrProviderName    string
	AwsConfigPath      string
}

func generateImageCredentialProviderConfig(cfg *api.NodeConfig, ecrCredentialProviderBinPath string) ([]byte, error) {
	templateVars := imageCredentialProviderTemplateVars{
		EcrProviderName: filepath.Base(ecrCredentialProviderBinPath),
	}
	kubeletVersion, err := GetKubeletVersion()
	if err != nil {
		return nil, err
	}
	if semver.Compare(kubeletVersion, "v1.27.0") < 0 {
		templateVars.ConfigApiVersion = "kubelet.config.k8s.io/v1alpha1"
		templateVars.ProviderApiVersion = "credentialprovider.kubelet.k8s.io/v1alpha1"
	} else {
		templateVars.ConfigApiVersion = "kubelet.config.k8s.io/v1"
		templateVars.ProviderApiVersion = "credentialprovider.kubelet.k8s.io/v1"
	}

	var buf bytes.Buffer
	var imageCredentialProviderTemplate *template.Template
	if cfg.IsIAMRolesAnywhere() {
		templateVars.AwsConfigPath = cfg.Spec.Hybrid.IAMRolesAnywhere.AwsConfigPath
		imageCredentialProviderTemplate = template.Must(template.New("image-credential-provider").Parse(hybridRolesAnywhereImageCredentialProviderTemplateData))
	} else {
		// The regular non-iam roles anywhere credential provider would still work for SSM based installs
		imageCredentialProviderTemplate = template.Must(template.New("image-credential-provider").Parse(imageCredentialProviderTemplateData))
	}
	if err := imageCredentialProviderTemplate.Execute(&buf, templateVars); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func ensureCredentialProviderBinaryExists(binPath string) error {
	if _, err := os.Stat(binPath); err != nil {
		return fmt.Errorf("image credential provider binary was not found on path %s. error: %s", binPath, err)
	}
	return nil
}
