package os

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"text/template"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"sigs.k8s.io/yaml"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/test/e2e"
)

const (
	amd64Arch = "amd64"
	arm64Arch = "arm64"
	x8664Arch = "x86_64"
)

type architecture string

const (
	amd64 architecture = "amd64"
	arm64 architecture = "arm64"
)

var instanceSizeToType = map[architecture]map[e2e.InstanceSize]string{
	amd64: {
		e2e.XLarge: "t3.xlarge",
		e2e.Large:  "t3.large",
	},
	arm64: {
		e2e.XLarge: "t4g.xlarge",
		e2e.Large:  "t4g.large",
	},
}

var gpuInstanceSizeToType = map[architecture]map[e2e.InstanceSize]string{
	amd64: {
		e2e.XLarge: "g4dn.2xlarge",
		e2e.Large:  "g4dn.xlarge",
	},
	arm64: {
		e2e.XLarge: "g5g.2xlarge",
		e2e.Large:  "g5g.xlarge",
	},
}

//go:embed testdata/nodeadm-init.sh
var nodeAdmInitScript []byte

//go:embed testdata/log-collector.sh
var LogCollectorScript []byte

//go:embed testdata/nodeadm-wrapper.sh
var nodeadmWrapperScript []byte

//go:embed testdata/install-containerd.sh
var installContainerdScript []byte

//go:embed testdata/nvidia-driver-install.sh
var nvidiaDriverInstallScript []byte

//go:embed testdata/proxy/systemd-proxy.conf
var systemdProxyConf []byte

//go:embed testdata/proxy/proxy-vars.sh
var proxyVarsScript []byte

func (a architecture) String() string {
	return string(a)
}

func (a architecture) arm() bool {
	return a == arm64
}

func populateBaseScripts(userDataInput *e2e.UserDataInput) error {
	logCollector, err := executeTemplate(LogCollectorScript, userDataInput)
	if err != nil {
		return fmt.Errorf("generating log collector script: %w", err)
	}
	nodeadmWrapper, err := executeTemplate(nodeadmWrapperScript, userDataInput)
	if err != nil {
		return fmt.Errorf("generating nodeadm wrapper: %w", err)
	}

	// Always create proxy-vars.sh, it will return early if no proxy is set
	proxyVars, err := executeTemplate(proxyVarsScript, map[string]interface{}{
		"Proxy": userDataInput.Proxy,
	})
	if err != nil {
		return fmt.Errorf("executing proxy vars template: %w", err)
	}

	userDataInput.Files = append(userDataInput.Files,
		e2e.File{Content: string(nodeAdmInitScript), Path: "/tmp/nodeadm-init.sh", Permissions: "0755"},
		e2e.File{Content: string(logCollector), Path: "/tmp/log-collector.sh", Permissions: "0755"},
		e2e.File{Content: string(nodeadmWrapper), Path: "/tmp/nodeadm-wrapper.sh", Permissions: "0755"},
		e2e.File{Content: string(installContainerdScript), Path: "/tmp/install-containerd.sh", Permissions: "0755"},
		e2e.File{Content: string(nvidiaDriverInstallScript), Path: "/tmp/nvidia-driver-install.sh", Permissions: "0755"},
		e2e.File{Content: string(proxyVars), Path: "/etc/proxy-vars.sh", Permissions: "0644"},
	)

	if userDataInput.Proxy != "" {
		// Use the common systemd proxy config for containerd and kubelet
		if err := addSystemdProxyConfig(userDataInput, "/etc/systemd/system/containerd.service.d/http-proxy.conf"); err != nil {
			return fmt.Errorf("adding containerd proxy config: %w", err)
		}
		if err := addSystemdProxyConfig(userDataInput, "/etc/systemd/system/kubelet.service.d/http-proxy.conf"); err != nil {
			return fmt.Errorf("adding kubelet proxy config: %w", err)
		}
		if err := addSystemdProxyConfig(userDataInput, "/etc/systemd/system/aws_signing_helper_update.service.d/http-proxy.conf"); err != nil {
			return fmt.Errorf("adding aws signing helper update proxy config: %w", err)
		}
	}

	return nil
}

// addSSMAgentProxyConfig adds the SSM agent proxy configuration at the specified path
func addSystemdProxyConfig(userDataInput *e2e.UserDataInput, path string) error {
	if userDataInput.Proxy == "" {
		return nil
	}

	proxyConf, err := executeTemplate(systemdProxyConf, map[string]interface{}{
		"Proxy": userDataInput.Proxy,
	})
	if err != nil {
		return fmt.Errorf("executing ssm agent proxy template: %w", err)
	}

	userDataInput.Files = append(userDataInput.Files, e2e.File{
		Content:     string(proxyConf),
		Path:        path,
		Permissions: "0644",
	})
	return nil
}

func executeTemplate(templateData []byte, values any) ([]byte, error) {
	tmpl, err := template.New("cloud-init").Funcs(templateFuncMap()).Parse(string(templateData))
	if err != nil {
		return nil, err
	}

	// Execute the template and write the result to a buffer
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, values); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func getAmiIDFromSSM(ctx context.Context, client *ssm.Client, amiName string) (*string, error) {
	getParameterInput := &ssm.GetParameterInput{
		Name:           aws.String(amiName),
		WithDecryption: aws.Bool(true),
	}

	output, err := client.GetParameter(ctx, getParameterInput)
	if err != nil {
		return nil, err
	}

	return output.Parameter.Value, nil
}

// an unknown size and arch combination is a coding error, so we panic
func getInstanceTypeFromRegionAndArch(_ string, arch architecture, instanceSize e2e.InstanceSize, computeType e2e.ComputeType) string {
	var instanceType string
	var ok bool

	if computeType == e2e.GPUInstance {
		instanceType, ok = gpuInstanceSizeToType[arch][instanceSize]
	} else {
		instanceType, ok = instanceSizeToType[arch][instanceSize]
	}

	if !ok {
		panic(fmt.Errorf("unknown instance size %d for architecture %s", instanceSize, arch))
	}
	return instanceType
}

func generateNodeadmConfigYaml(nodeadmConfig *api.NodeConfig) (string, error) {
	nodeadmConfigYaml, err := yaml.Marshal(nodeadmConfig)
	if err != nil {
		return "", fmt.Errorf("marshalling nodeadm config to YAML: %w", err)
	}

	return string(nodeadmConfigYaml), nil
}
