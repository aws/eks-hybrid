package containerd

import (
	"bytes"
	_ "embed"
	"path/filepath"
	"text/template"

	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/util"
)

const ContainerRuntimeEndpoint = "unix:///run/containerd/containerd.sock"

const (
	containerdConfigDir               = "/etc/containerd"
	containerdConfigFile              = "/etc/containerd/config.toml"
	containerdConfigImportDir         = "/etc/containerd/config.d"
	containerdKernelModulesConfigFile = "/etc/modules-load.d/containerd.conf"
	containerdConfigPerm              = 0o644
)

var (
	//go:embed config.template.toml
	containerdConfigTemplateData string
	containerdConfigTemplate     = template.Must(template.New(containerdConfigFile).Parse(containerdConfigTemplateData))

	//go:embed kernel-modules.conf
	containerdKernelModulesFileData string
)

type containerdTemplateVars struct {
	SandboxImage string
}

func writeContainerdConfig(cfg *api.NodeConfig) error {
	// write nodeadm's generated containerd config to the default path
	containerdConfig, err := generateContainerdConfig(cfg)
	if err != nil {
		return err
	}
	zap.L().Info("Writing containerd config to file..", zap.String("path", containerdConfigFile))
	if err := util.WriteFileWithDir(containerdConfigFile, containerdConfig, containerdConfigPerm); err != nil {
		return err
	}
	if len(cfg.Spec.Containerd.Config) > 0 {
		containerConfigImportPath := filepath.Join(containerdConfigImportDir, "00-nodeadm.toml")
		zap.L().Info("Writing user containerd config to drop-in file..", zap.String("path", containerConfigImportPath))
		return util.WriteFileWithDir(containerConfigImportPath, []byte(cfg.Spec.Containerd.Config), containerdConfigPerm)
	}
	return nil
}

func generateContainerdConfig(cfg *api.NodeConfig) ([]byte, error) {
	configVars := containerdTemplateVars{
		SandboxImage: cfg.Status.Defaults.SandboxImage,
	}
	var buf bytes.Buffer
	if err := containerdConfigTemplate.Execute(&buf, configVars); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeContainerdKernelModulesConfig() error {
	return util.WriteFileWithDir(containerdKernelModulesConfigFile, []byte(containerdKernelModulesFileData), containerdConfigPerm)
}
