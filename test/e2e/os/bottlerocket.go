package os

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecrpublic"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/blang/semver/v4"

	"github.com/aws/eks-hybrid/test/e2e"
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

var brVariantForComputeType = map[e2e.ComputeType]string{
	e2e.CPUInstance: "aws-k8s-%s",
	e2e.GPUInstance: "aws-k8s-%s-nvidia",
}

type brSettingsTomlInitData struct {
	e2e.UserDataInput
	AdminContainerTag       string
	BootstrapContainerTag   string
	ControlContainerTag     string
	AdminContainerUserData  string
	AWSConfig               string
	ClusterCertificate      string
	HybridContainerUserData string
	IamRA                   bool
}

type BottleRocket struct {
	amiArchitecture string
	architecture    architecture
}

func NewBottleRocket() *BottleRocket {
	br := new(BottleRocket)
	br.amiArchitecture = x8664Arch
	br.architecture = amd64
	return br
}

func NewBottleRocketARM() *BottleRocket {
	br := new(BottleRocket)
	br.amiArchitecture = arm64Arch
	br.architecture = arm64
	return br
}

func (a BottleRocket) Name() string {
	return "bottlerocket-" + a.architecture.String()
}

func (a BottleRocket) InstanceType(region string, instanceSize e2e.InstanceSize, computeType e2e.ComputeType) string {
	return getInstanceTypeFromRegionAndArch(region, a.architecture, instanceSize, computeType)
}

func (a BottleRocket) AMIName(ctx context.Context, awsConfig aws.Config, kubernetesVersion string, computeType e2e.ComputeType) (string, error) {
	return getAmiIDFromSSM(ctx, ssm.NewFromConfig(awsConfig), fmt.Sprintf("/aws/service/bottlerocket/%s/%s/latest/image_id", fmt.Sprintf(brVariantForComputeType[computeType], kubernetesVersion), a.amiArchitecture))
}

func (a BottleRocket) BuildUserData(ctx context.Context, userDataInput e2e.UserDataInput) ([]byte, error) {
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

	awsConfigData := ""
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
		bootstrapContainerCommand = fmt.Sprintf("%s --certificate='%s' --key='%s'", iamRaSetupBootstrapCommand, certificate, key)
		awsConfigData = fmt.Sprintf(`
[default]
credential_process = %s credential-process --certificate %s --private-key %s --profile-arn %s --role-arn %s --trust-anchor-arn %s --role-session-name %s
`, awsSigningHelperBinary, iamRaCertificatePath, iamRaKeyPath, userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere.ProfileARN, userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere.RoleARN, userDataInput.NodeadmConfig.Spec.Hybrid.IAMRolesAnywhere.TrustAnchorARN, userDataInput.HostName)
	} else if userDataInput.NodeadmConfig.Spec.Hybrid.SSM != nil {
		bootstrapContainerCommand = fmt.Sprintf("%s --activation-id=%q --activation-code=%q --region=%q", ssmSetupBootstrapCommand, userDataInput.NodeadmConfig.Spec.Hybrid.SSM.ActivationID, userDataInput.NodeadmConfig.Spec.Hybrid.SSM.ActivationCode, userDataInput.Region)
	}

	// This is essentially the same AWS config used in the tests, but for the us-east-1 region.
	// We need to do this since ECR Public is only supported in us-east-1.
	awsConfig, err := e2e.NewAWSConfig(ctx, config.WithRegion("us-east-1"),
		config.WithAppID("bottlerocket-e2e-test"),
	)
	if err != nil {
		return nil, err
	}

	authToken, err := getAuthToken(ctx, ecrpublic.NewFromConfig(awsConfig))
	if err != nil {
		return nil, err
	}

	adminContainerLatestTag, bootstrapContainerLatestTag, controlContainerLatestTag, err := getLatestImageTags(authToken)
	if err != nil {
		return nil, err
	}

	data := brSettingsTomlInitData{
		UserDataInput:           userDataInput,
		AdminContainerTag:       adminContainerLatestTag,
		BootstrapContainerTag:   bootstrapContainerLatestTag,
		ControlContainerTag:     controlContainerLatestTag,
		AdminContainerUserData:  sshKey,
		AWSConfig:               base64.StdEncoding.EncodeToString([]byte(awsConfigData)),
		ClusterCertificate:      base64.StdEncoding.EncodeToString(userDataInput.ClusterCert),
		IamRA:                   userDataInput.NodeadmConfig.Spec.Hybrid.SSM == nil,
		HybridContainerUserData: base64.StdEncoding.EncodeToString([]byte(bootstrapContainerCommand)),
	}

	return executeTemplate(brSettingsToml, data)
}

// IsBottlerocket returns true if the given name is a Bottlerocket OS name.
func IsBottlerocket(name string) bool {
	return strings.HasPrefix(name, constants.BottlerocketOsName)
}

type RegistryResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

func getLatestImageTags(authToken string) (string, string, string, error) {
	bottlerocketRepos := []string{
		"bottlerocket-admin",
		"bottlerocket-bootstrap",
		"bottlerocket-control",
	}
	latestTags := []string{}

	for _, repo := range bottlerocketRepos {
		requestUrl := fmt.Sprintf("https://public.ecr.aws/v2/bottlerocket/%s/tags/list", repo)
		req, err := http.NewRequest("GET", requestUrl, nil)
		if err != nil {
			return "", "", "", err
		}
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", authToken))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", "", "", err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", "", "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", "", "", err
		}

		var regResp RegistryResponse
		if err := json.Unmarshal(body, &regResp); err != nil {
			return "", "", "", err
		}

		tags := regResp.Tags
		if err != nil {
			return "", "", "", fmt.Errorf("getting all tags for %s repository: %v", repo, err)
		}

		latest := tags[0]
		latestSemver, err := semver.Parse(strings.TrimPrefix(latest, "v"))
		if err != nil {
			return "", "", "", err
		}
		for _, tag := range tags {
			if tag == "latest" {
				latest = tag
				break
			}
			tagSemver, err := semver.Parse(strings.TrimPrefix(tag, "v"))
			if err != nil {
				return "", "", "", err
			}

			if tagSemver.GT(latestSemver) {
				latest = tag
				latestSemver = tagSemver
			}
		}
		latestTags = append(latestTags, latest)
	}

	return latestTags[0], latestTags[1], latestTags[2], nil
}
