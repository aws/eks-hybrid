package os

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/commands"
	"github.com/aws/eks-hybrid/test/e2e/constants"
)

const (
	sshUser                    = "ec2-user"
	iamRaSetupBootstrapCommand = "eks-hybrid-iam-ra-setup"
	iamRaCertificatePath       = "/root/.aws/node.crt"
	iamRaKeyPath               = "/root/.aws/node.key"
	ssmSetupBootstrapCommand   = "eks-hybrid-ssm-setup"
	awsSigningHelperBinary     = "aws_signing_helper"
)

//go:embed testdata/bottlerocket/settings.toml
var brSettingsToml []byte

type brSettingsTomlInitData struct {
	e2e.UserDataInput
	NodeadmUrl              string
	AdminContainerUserData  string
	AWSConfig               string
	ClusterCertificate      string
	HybridContainerUserData string
	IamRA                   bool
}

type Bottlerocket struct {
	amiArchitecture string
	architecture    architecture
}

func NewBottlerocket() *Bottlerocket {
	br := new(Bottlerocket)
	br.amiArchitecture = x8664Arch
	br.architecture = amd64
	return br
}

func NewBottlerocketARM() *Bottlerocket {
	br := new(Bottlerocket)
	br.amiArchitecture = arm64Arch
	br.architecture = arm64
	return br
}

func (b Bottlerocket) Name() string {
	return "bottlerocket-" + b.architecture.String()
}

func (b Bottlerocket) InstanceType(region string, instanceSize e2e.InstanceSize) string {
	return getInstanceTypeFromRegionAndArch(region, b.architecture, instanceSize)
}

func (b Bottlerocket) AMIName(ctx context.Context, awsConfig aws.Config, kubernetesVersion string) (string, error) {
	amiId, err := getAmiIDFromSSM(ctx, ssm.NewFromConfig(awsConfig), fmt.Sprintf("/aws/service/bottlerocket/aws-k8s-%s/%s/latest/image_id", kubernetesVersion, b.amiArchitecture))
	return *amiId, err
}

func (b Bottlerocket) BuildUserData(userDataInput e2e.UserDataInput) ([]byte, error) {
	if err := populateBaseScripts(&userDataInput); err != nil {
		return nil, err
	}
	sshData := map[string]interface{}{
		"user":          sshUser,
		"password-hash": userDataInput.RootPasswordHash,
		"ssh": map[string][]string{
			"authorized-keys": {
				strings.TrimSuffix(userDataInput.PublicKey, "\n"),
			},
		},
	}

	jsonData, err := json.Marshal(sshData)
	if err != nil {
		return nil, err
	}
	sshKey := base64.StdEncoding.EncodeToString([]byte(jsonData))

	awsConfig := ""
	bootstrapContainerCommand := ""
	if userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere != nil {
		var certificate, key string
		for _, file := range userDataInput.Files {
			if file.Path == constants.RolesAnywhereCertPath {
				certificate = strings.ReplaceAll(file.Content, "\\n", "\n")
			}
			if file.Path == constants.RolesAnywhereKeyPath {
				key = strings.ReplaceAll(file.Content, "\\n", "\n")
			}
		}
		bootstrapContainerCommand = fmt.Sprintf("%s --certificate='%s' --key='%s' --enable-credentials-file=%t", iamRaSetupBootstrapCommand, certificate, key, userDataInput.NodeadmConfig.Spec.Hybrid.EnableCredentialsFile)
		awsConfig = fmt.Sprintf(`
[default]
credential_process = %s credential-process --certificate %s --private-key %s --profile-arn %s --role-arn %s --trust-anchor-arn %s --role-session-name %s
`, awsSigningHelperBinary, iamRaCertificatePath, iamRaKeyPath, userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere.ProfileARN, userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere.RoleARN, userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere.TrustAnchorARN, userDataInput.HostName)
	} else if userDataInput.NodeadmConfig.Spec.Hybrid.SSM != nil {
		bootstrapContainerCommand = fmt.Sprintf("%s --activation-id=%q --activation-code=%q --region=%q --enable-credentials-file=%t", ssmSetupBootstrapCommand, userDataInput.NodeadmConfig.Spec.Hybrid.SSM.ActivationID, userDataInput.NodeadmConfig.Spec.Hybrid.SSM.ActivationCode, userDataInput.Region, userDataInput.NodeadmConfig.Spec.Hybrid.EnableCredentialsFile)
	}
	data := brSettingsTomlInitData{
		UserDataInput:           userDataInput,
		AdminContainerUserData:  sshKey,
		AWSConfig:               base64.StdEncoding.EncodeToString([]byte(awsConfig)),
		ClusterCertificate:      base64.StdEncoding.EncodeToString(userDataInput.ClusterCert),
		IamRA:                   userDataInput.NodeadmConfig.Spec.Hybrid.SSM == nil,
		HybridContainerUserData: base64.StdEncoding.EncodeToString([]byte(bootstrapContainerCommand)),
	}

	return executeTemplate(brSettingsToml, data)
}

func (b Bottlerocket) RebootInstance(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	commands := []string{
		"apiclient reboot",
	}

	// the ssh command will exit with an error because the machine reboots
	// ignoring output
	_, err := runner.Run(ctx, instanceIP, commands)
	if err != nil {
		return fmt.Errorf("running remote command: %w", err)
	}

	return nil
}

func (b Bottlerocket) CollectLogs(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, logBundleUrl string) error {
	commands := []string{
		"sudo /usr/sbin/chroot /.bottlerocket/rootfs/ logdog --output /var/log/eks-hybrid-logs.tar.gz",
		fmt.Sprintf("sudo curl --retry 5 --request PUT --upload-file /.bottlerocket/rootfs/var/log/eks-hybrid-logs.tar.gz '%s'", logBundleUrl),
		"sudo rm /.bottlerocket/rootfs/var/log/eks-hybrid-logs.tar.gz",
	}

	output, err := runner.Run(ctx, instanceIP, commands)
	if err != nil {
		return fmt.Errorf("running remote command: %w", err)
	}

	if output.Status != "Success" {
		return fmt.Errorf("log collector remote command did not succeed")
	}

	return nil
}

func (b Bottlerocket) Uninstall(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	commands := []string{
		"sudo /usr/sbin/chroot /.bottlerocket/rootfs systemctl stop kubelet",
	}

	output, err := runner.Run(ctx, instanceIP, commands)
	if err != nil {
		return fmt.Errorf("running remote command: %w", err)
	}

	if output.Status != "Success" {
		return fmt.Errorf("systemctl remote command did not succeed")
	}

	return nil
}

func (b Bottlerocket) GetNodeadmVersion(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) (string, error) {
	// Bottlerocket doesn't use nodeadm so the version is N/A
	return "N/A", nil
}

func (b Bottlerocket) RunNodeadmDebug(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP string) error {
	// Bottlerocket doesn't support nodeadm debug
	return nil
}

func (b Bottlerocket) ShouldRunNodeadmDebug() bool {
	return false
}

func (b Bottlerocket) Upgrade(ctx context.Context, runner commands.RemoteCommandRunner, instanceIP, kubernetesVersion string) error {
	// Bottlerocket doesn't support nodeadm upgrade
	return nil
}

func (b Bottlerocket) PodIdentityAgentDaemonsetName() string {
	return "eks-pod-identity-agent-hybrid-bottlerocket"
}
