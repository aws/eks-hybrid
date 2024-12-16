package os

import (
	"bytes"
	"context"
	"text/template"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ssm"

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

func (a architecture) String() string {
	return string(a)
}

func (a architecture) arm() bool {
	return a == arm64
}

func (a architecture) amd() bool {
	return a == amd64
}

func populateBaseScripts(userDataInput *e2e.UserDataInput) error {
	logCollector, err := executeTemplate(logCollectorScript, userDataInput)
	if err != nil {
		return err
	}

	userDataInput.Files = append(userDataInput.Files,
		e2e.File{Content: string(nodeAdmInitScript), Path: "/tmp/nodeadm-init.sh", Permissions: "0755"},
		e2e.File{Content: string(logCollector), Path: "/tmp/log-collector.sh", Permissions: "0755"},
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

func getAmiIDFromSSM(ctx context.Context, client *ssm.SSM, amiName string) (*string, error) {
	getParameterInput := &ssm.GetParameterInput{
		Name:           aws.String(amiName),
		WithDecryption: aws.Bool(true),
	}

	output, err := client.GetParameterWithContext(ctx, getParameterInput)
	if err != nil {
		return nil, err
	}

	return output.Parameter.Value, nil
}

func getInstanceTypeFromRegionAndArch(_ string, arch architecture) string {
	if arch.amd() {
		return "t3.large"
	}
	return "t4g.large"
}
