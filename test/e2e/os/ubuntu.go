package os

import (
	"context"
	_ "embed"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/aws/eks-hybrid/test/e2e"
)

//go:embed testdata/ubuntu/2004/cloud-init.txt
var ubuntu2004CloudInit []byte

//go:embed testdata/ubuntu/2204/cloud-init.txt
var ubuntu2204CloudInit []byte

//go:embed testdata/ubuntu/2404/cloud-init.txt
var ubuntu2404CloudInit []byte

type ubuntuCloudInitData struct {
	e2e.UserDataInput
	NodeadmUrl            string
	NodeadmInitScript     string
	NodeadmAdditionalArgs string
	PreinstallContainerd  bool
}

func templateFuncMap() map[string]interface{} {
	return map[string]interface{}{
		"indent": func(spaces int, v string) string {
			pad := strings.Repeat(" ", spaces)
			return pad + strings.ReplaceAll(v, "\n", "\n"+pad)
		},
	}
}

type Ubuntu2004 struct {
	architecture     architecture
	amiArchitecture  string
	containerdSource string
}

func NewUbuntu2004AMD() *Ubuntu2004 {
	u := new(Ubuntu2004)
	u.amiArchitecture = amd64Arch
	u.architecture = amd64
	u.containerdSource = "distro"
	return u
}

func NewUbuntu2004DockerSource() *Ubuntu2004 {
	u := new(Ubuntu2004)
	u.amiArchitecture = amd64Arch
	u.architecture = amd64
	u.containerdSource = "docker"
	return u
}

func NewUbuntu2004ARM() *Ubuntu2004 {
	u := new(Ubuntu2004)
	u.amiArchitecture = arm64Arch
	u.architecture = arm64
	u.containerdSource = "distro"
	return u
}

func (u Ubuntu2004) Name() string {
	name := "ubuntu2004-" + u.architecture.String()
	if u.containerdSource == "docker" {
		name += "-docker"
	}
	return name
}

func (u Ubuntu2004) InstanceType(region string, instanceSize e2e.InstanceSize, computeType e2e.ComputeType) string {
	return getInstanceTypeFromRegionAndArch(region, u.architecture, instanceSize, computeType)
}

func (u Ubuntu2004) AMIName(ctx context.Context, awsConfig aws.Config, _ string, _ e2e.ComputeType) (string, error) {
	amiId, err := getAmiIDFromSSM(ctx, ssm.NewFromConfig(awsConfig), "/aws/service/canonical/ubuntu/server/20.04/stable/current/"+u.amiArchitecture+"/hvm/ebs-gp2/ami-id")
	return *amiId, err
}

func (u Ubuntu2004) BuildUserData(userDataInput e2e.UserDataInput) ([]byte, error) {
	nodeadmConfigYaml, err := generateNodeadmConfigYaml(userDataInput.NodeadmConfig)
	if err != nil {
		return nil, err
	}
	userDataInput.NodeadmConfigYaml = nodeadmConfigYaml

	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}

	data := ubuntuCloudInitData{
		UserDataInput: userDataInput,
		NodeadmUrl:    userDataInput.NodeadmUrls.AMD,
	}

	if u.architecture.arm() {
		data.NodeadmUrl = userDataInput.NodeadmUrls.ARM
	}

	if u.containerdSource == "docker" {
		data.NodeadmAdditionalArgs = "--containerd-source docker"
	}

	return executeTemplate(ubuntu2004CloudInit, data)
}

type Ubuntu2204 struct {
	amiArchitecture  string
	architecture     architecture
	containerdSource string
}

func NewUbuntu2204AMD() *Ubuntu2204 {
	u := new(Ubuntu2204)
	u.amiArchitecture = amd64Arch
	u.architecture = amd64
	u.containerdSource = "distro"
	return u
}

func NewUbuntu2204DockerSource() *Ubuntu2204 {
	u := new(Ubuntu2204)
	u.amiArchitecture = amd64Arch
	u.architecture = amd64
	u.containerdSource = "docker"
	return u
}

func NewUbuntu2204ARM() *Ubuntu2204 {
	u := new(Ubuntu2204)
	u.amiArchitecture = arm64Arch
	u.architecture = arm64
	u.containerdSource = "distro"
	return u
}

func (u Ubuntu2204) Name() string {
	name := "ubuntu2204-" + u.architecture.String()
	if u.containerdSource == "docker" {
		name += "-docker"
	}
	return name
}

func (u Ubuntu2204) InstanceType(region string, instanceSize e2e.InstanceSize, computeType e2e.ComputeType) string {
	return getInstanceTypeFromRegionAndArch(region, u.architecture, instanceSize, computeType)
}

func (u Ubuntu2204) AMIName(ctx context.Context, awsConfig aws.Config, _ string, _ e2e.ComputeType) (string, error) {
	amiId, err := getAmiIDFromSSM(ctx, ssm.NewFromConfig(awsConfig), "/aws/service/canonical/ubuntu/server/22.04/stable/current/"+u.amiArchitecture+"/hvm/ebs-gp2/ami-id")
	return *amiId, err
}

func (u Ubuntu2204) BuildUserData(userDataInput e2e.UserDataInput) ([]byte, error) {
	nodeadmConfigYaml, err := generateNodeadmConfigYaml(userDataInput.NodeadmConfig)
	if err != nil {
		return nil, err
	}
	userDataInput.NodeadmConfigYaml = nodeadmConfigYaml

	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}

	data := ubuntuCloudInitData{
		UserDataInput: userDataInput,
		NodeadmUrl:    userDataInput.NodeadmUrls.AMD,
	}

	if u.architecture.arm() {
		data.NodeadmUrl = userDataInput.NodeadmUrls.ARM
	}

	if u.containerdSource == "docker" {
		data.NodeadmAdditionalArgs = "--containerd-source docker"
	}

	return executeTemplate(ubuntu2204CloudInit, data)
}

type Ubuntu2404 struct {
	amiArchitecture  string
	architecture     architecture
	containerdSource string
}

func NewUbuntu2404AMD() *Ubuntu2404 {
	u := new(Ubuntu2404)
	u.amiArchitecture = amd64Arch
	u.architecture = amd64
	u.containerdSource = "distro"
	return u
}

func NewUbuntu2404DockerSource() *Ubuntu2404 {
	u := new(Ubuntu2404)
	u.amiArchitecture = amd64Arch
	u.architecture = amd64
	u.containerdSource = "docker"
	return u
}

func NewUbuntu2404NoDockerSource() *Ubuntu2404 {
	u := new(Ubuntu2404)
	u.amiArchitecture = amd64Arch
	u.architecture = amd64
	u.containerdSource = "none"
	return u
}

func NewUbuntu2404ARM() *Ubuntu2404 {
	u := new(Ubuntu2404)
	u.amiArchitecture = arm64Arch
	u.architecture = arm64
	u.containerdSource = "distro"
	return u
}

func (u Ubuntu2404) Name() string {
	name := "ubuntu2404-" + u.architecture.String()
	switch u.containerdSource {
	case "docker":
		name += "-docker"
	case "none":
		name += "-source-none"
	}
	return name
}

func (u Ubuntu2404) InstanceType(region string, instanceSize e2e.InstanceSize, computeType e2e.ComputeType) string {
	return getInstanceTypeFromRegionAndArch(region, u.architecture, instanceSize, computeType)
}

func (u Ubuntu2404) AMIName(ctx context.Context, awsConfig aws.Config, _ string, _ e2e.ComputeType) (string, error) {
	amiId, err := getAmiIDFromSSM(ctx, ssm.NewFromConfig(awsConfig), "/aws/service/canonical/ubuntu/server/24.04/stable/current/"+u.amiArchitecture+"/hvm/ebs-gp3/ami-id")
	return *amiId, err
}

func (u Ubuntu2404) BuildUserData(userDataInput e2e.UserDataInput) ([]byte, error) {
	nodeadmConfigYaml, err := generateNodeadmConfigYaml(userDataInput.NodeadmConfig)
	if err != nil {
		return nil, err
	}
	userDataInput.NodeadmConfigYaml = nodeadmConfigYaml

	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}

	data := ubuntuCloudInitData{
		UserDataInput: userDataInput,
		NodeadmUrl:    userDataInput.NodeadmUrls.AMD,
	}

	if u.architecture.arm() {
		data.NodeadmUrl = userDataInput.NodeadmUrls.ARM
	}

	switch u.containerdSource {
	case "docker":
		data.NodeadmAdditionalArgs = "--containerd-source docker"
	case "none":
		data.NodeadmAdditionalArgs = "--containerd-source none"
		data.PreinstallContainerd = true
	}

	return executeTemplate(ubuntu2404CloudInit, data)
}
