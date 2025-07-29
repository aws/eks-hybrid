package os

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"text/template"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecrpublic"
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

	userDataInput.Files = append(userDataInput.Files,
		e2e.File{Content: string(nodeAdmInitScript), Path: "/tmp/nodeadm-init.sh", Permissions: "0755"},
		e2e.File{Content: string(logCollector), Path: "/tmp/log-collector.sh", Permissions: "0755"},
		e2e.File{Content: string(nodeadmWrapper), Path: "/tmp/nodeadm-wrapper.sh", Permissions: "0755"},
		e2e.File{Content: string(installContainerdScript), Path: "/tmp/install-containerd.sh", Permissions: "0755"},
		e2e.File{Content: string(nvidiaDriverInstallScript), Path: "/tmp/nvidia-driver-install.sh", Permissions: "0755"},
	)

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

func getAmiIDFromSSM(ctx context.Context, client *ssm.Client, amiName string) (string, error) {
	getParameterInput := &ssm.GetParameterInput{
		Name:           aws.String(amiName),
		WithDecryption: aws.Bool(true),
	}

	output, err := client.GetParameter(ctx, getParameterInput)
	if err != nil {
		return "", err
	}

	return *output.Parameter.Value, nil
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

func getAuthToken(ctx context.Context, client *ecrpublic.Client) (string, error) {
	output, err := client.GetAuthorizationToken(ctx, &ecrpublic.GetAuthorizationTokenInput{})
	if err != nil {
		return "", nil
	}

	return *output.AuthorizationData.AuthorizationToken, nil
}
