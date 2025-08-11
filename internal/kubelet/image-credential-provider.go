package kubelet

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	config "k8s.io/kubelet/config/v1"

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

var imageCredentialProviderConfigPath = path.Join(imageCredentialProviderRoot, imageCredentialProviderConfig)

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

	config, err := generateImageCredentialProviderConfig(k.nodeConfig, ecrCredentialProviderBinPath, k.credentialProviderAwsConfig)
	if err != nil {
		return err
	}

	k.flags["image-credential-provider-bin-dir"] = path.Dir(ecrCredentialProviderBinPath)
	k.flags["image-credential-provider-config"] = imageCredentialProviderConfigPath

	return util.WriteFileWithDir(imageCredentialProviderConfigPath, config, imageCredentialProviderPerm)
}

func generateImageCredentialProviderConfig(cfg *api.NodeConfig, ecrCredentialProviderBinPath string, kubeletCredentialProviderAwsConfig CredentialProviderAwsConfig) ([]byte, error) {
	configApiVersion := "kubelet.config.k8s.io/v1"
	providerApiVersion := "credentialprovider.kubelet.k8s.io/v1"

	env := []config.ExecEnvVar{}
	if cfg.IsIAMRolesAnywhere() {
		env = append(env, config.ExecEnvVar{
			Name:  "AWS_CONFIG_FILE",
			Value: cfg.Spec.Hybrid.IAMRolesAnywhere.AwsConfigPath,
		})
	}
	if kubeletCredentialProviderAwsConfig.Profile != "" {
		env = append(env, config.ExecEnvVar{
			Name:  "AWS_PROFILE",
			Value: kubeletCredentialProviderAwsConfig.Profile,
		})
	}
	if kubeletCredentialProviderAwsConfig.CredentialsPath != "" {
		env = append(env, config.ExecEnvVar{
			Name:  "AWS_SHARED_CREDENTIALS_FILE",
			Value: kubeletCredentialProviderAwsConfig.CredentialsPath,
		})
	}

	providerConfig := config.CredentialProviderConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: configApiVersion,
			Kind:       "CredentialProviderConfig",
		},
		Providers: []config.CredentialProvider{
			{
				Name: filepath.Base(ecrCredentialProviderBinPath),
				MatchImages: []string{
					"*.dkr.ecr.*.amazonaws.com",
					"*.dkr.ecr.*.amazonaws.com.cn",
					"*.dkr.ecr-fips.*.amazonaws.com",
					"*.dkr.ecr.*.c2s.ic.gov",
					"*.dkr.ecr.*.sc2s.sgov.gov",
				},
				DefaultCacheDuration: &metav1.Duration{Duration: 12 * time.Hour},
				APIVersion:           providerApiVersion,
				Env:                  env,
			},
		},
	}

	data, err := json.MarshalIndent(providerConfig, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling image credential provider config: %w", err)
	}
	return data, nil
}

func ensureCredentialProviderBinaryExists(binPath string) error {
	if _, err := os.Stat(binPath); err != nil {
		return fmt.Errorf("image credential provider binary was not found on path %s. error: %s", binPath, err)
	}
	return nil
}
