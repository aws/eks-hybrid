package ssm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsSsm "github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/ssm"
	"github.com/aws/eks-hybrid/internal/test"
	"github.com/aws/eks-hybrid/internal/tracker"
)

func generateKeyPair(t *testing.T) (string, *crypto.Key) {
	g := NewGomegaWithT(t)

	pgp := crypto.PGP()
	key, err := pgp.KeyGeneration().
		AddUserId("test", "test@example.com").
		New().GenerateKey()
	g.Expect(err).NotTo(HaveOccurred())

	armoredPublicKey, err := key.GetArmoredPublicKey()
	g.Expect(err).NotTo(HaveOccurred())

	return armoredPublicKey, key
}

func generateSignature(t *testing.T, key *crypto.Key, data []byte) []byte {
	g := NewGomegaWithT(t)

	pgp := crypto.PGP()
	signer, err := pgp.Sign().SigningKey(key).Detached().New()
	g.Expect(err).NotTo(HaveOccurred())

	signature, err := signer.Sign(data, crypto.Bytes)
	g.Expect(err).NotTo(HaveOccurred())
	return signature
}

func TestInstall(t *testing.T) {
	publicKey, privateKey := generateKeyPair(t)
	_, wrongPrivateKey := generateKeyPair(t)

	installerData := []byte("#!/bin/echo\n")

	tests := []struct {
		name          string
		installerData []byte
		signature     []byte
		statusCode    int
		wantErr       string
		setupConfig   func(tmpDir string) error
	}{
		{
			name:          "successful installation",
			installerData: installerData,
			signature:     generateSignature(t, privateKey, installerData),
			statusCode:    http.StatusOK,
			wantErr:       "",
		},
		{
			name:          "successful installation with existing config",
			installerData: installerData,
			signature:     generateSignature(t, privateKey, installerData),
			statusCode:    http.StatusOK,
			wantErr:       "",
			setupConfig: func(tmpDir string) error {
				configDir := filepath.Join(tmpDir, "etc/amazon/ssm")
				configFile := filepath.Join(configDir, "amazon-ssm-agent.json")
				if err := os.MkdirAll(configDir, 0o755); err != nil {
					return err
				}
				existingConfig := `{"Identity": {"ConsumptionOrder": ["OnPrem"]},"Ssm": {"OtherSetting": true}}`
				return os.WriteFile(configFile, []byte(existingConfig), 0o644)
			},
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			wantErr:    "unexpected status code: 500",
		},
		{
			name:          "invalid signature",
			installerData: installerData,
			signature:     generateSignature(t, privateKey, append(installerData, []byte("extra data to make signature invalid")...)),
			statusCode:    http.StatusOK,
			wantErr:       "failed to install ssm installer: validating ssm-setup-cli signature: Signature Verification Error: Invalid signature caused by openpgp: invalid signature: EdDSA verification failure",
		},
		{
			name:          "wrong key signature",
			installerData: installerData,
			signature:     generateSignature(t, wrongPrivateKey, installerData),
			statusCode:    http.StatusOK,
			wantErr:       "failed to install ssm installer: validating ssm-setup-cli signature: Signature Verification Error: No matching signature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			tmpDir := t.TempDir()

			if tt.setupConfig != nil {
				err := tt.setupConfig(tmpDir)
				g.Expect(err).NotTo(HaveOccurred())
			}

			server := test.NewHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
				if tt.statusCode != http.StatusOK {
					w.WriteHeader(tt.statusCode)
					return
				}

				switch r.URL.Path {
				case "/latest/linux_amd64/ssm-setup-cli":
					if _, err := w.Write(tt.installerData); err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
				case "/latest/linux_amd64/ssm-setup-cli.sig":
					if _, err := w.Write(tt.signature); err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
				}
			})

			source := ssm.NewSSMInstaller(zap.NewNop(), "test-region",
				ssm.WithURLBuilder(func() (string, error) {
					return server.URL + "/latest/linux_amd64/ssm-setup-cli", nil
				}),
				ssm.WithPublicKey(publicKey),
			)

			tr := &tracker.Tracker{Artifacts: &tracker.InstalledArtifacts{}}
			logger := zap.NewNop()

			err := ssm.Install(context.Background(), ssm.InstallOptions{
				Tracker:     tr,
				Source:      source,
				Logger:      logger,
				InstallRoot: tmpDir,
			})

			if tt.wantErr != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
				return
			}

			g.Expect(err).NotTo(HaveOccurred())

			installedData, err := os.ReadFile(tmpDir + "/opt/ssm/ssm-setup-cli")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(installedData).To(Equal(tt.installerData))

			g.Expect(tr.Artifacts.Ssm).To(BeTrue())

			// Verify that configureSSMAgent
			configFile := filepath.Join(tmpDir, "etc/amazon/ssm/amazon-ssm-agent.json")
			g.Expect(configFile).To(BeAnExistingFile())

			data, err := os.ReadFile(configFile)
			g.Expect(err).NotTo(HaveOccurred())

			var result map[string]interface{}
			err = json.Unmarshal(data, &result)
			g.Expect(err).NotTo(HaveOccurred())

			// Check Hybrid node SSM configuration applied
			ssmConfig, exists := result["Ssm"].(map[string]interface{})
			g.Expect(exists).To(BeTrue(), "SSM configuration block not found")

			value, ok := ssmConfig["CredentialRetryMaxSleepSeconds"]
			g.Expect(ok).To(BeTrue(), "CredentialRetryMaxSleepSeconds not found in SSM config")
			g.Expect(value).To(Equal(float64(60)), "Expected CredentialRetryMaxSleepSeconds to be 60")

			// If we had existing config, verify it was preserved
			if tt.setupConfig != nil {
				otherSetting, exists := ssmConfig["OtherSetting"]
				g.Expect(exists).To(BeTrue(), "Existing OtherSetting should be preserved")
				g.Expect(otherSetting).To(Equal(true))

				identity, exists := result["Identity"].(map[string]interface{})
				g.Expect(exists).To(BeTrue(), "Identity configuration should be preserved")
				consumptionOrder, exists := identity["ConsumptionOrder"].([]interface{})
				g.Expect(exists).To(BeTrue(), "ConsumptionOrder should be preserved")
				g.Expect(consumptionOrder).To(Equal([]interface{}{"OnPrem"}))
			}
		})
	}
}

type MockSSMClient struct {
	g                                 *GomegaWithT
	instanceId                        string
	describeInstanceInformationOutput *awsSsm.DescribeInstanceInformationOutput
	describeInstanceInformationErr    error
	deregisterManagedInstanceOutput   *awsSsm.DeregisterManagedInstanceOutput
	deregisterManagedInstanceErr      error
}

func (m *MockSSMClient) DescribeInstanceInformation(ctx context.Context, params *awsSsm.DescribeInstanceInformationInput, optFns ...func(*awsSsm.Options)) (*awsSsm.DescribeInstanceInformationOutput, error) {
	m.g.Expect(*params.Filters[0].Key).To(Equal("InstanceIds"))
	m.g.Expect(params.Filters[0].Values[0]).To(Equal(m.instanceId))
	return m.describeInstanceInformationOutput, m.describeInstanceInformationErr
}

func (m *MockSSMClient) DeregisterManagedInstance(ctx context.Context, params *awsSsm.DeregisterManagedInstanceInput, optFns ...func(*awsSsm.Options)) (*awsSsm.DeregisterManagedInstanceOutput, error) {
	m.g.Expect(*params.InstanceId).To(Equal(m.instanceId))
	return m.deregisterManagedInstanceOutput, m.deregisterManagedInstanceErr
}

type TestPackageManager struct {
	mock.Mock
	InstallRoot string
}

func (m *TestPackageManager) GetSSMPackage() artifact.Package {
	return artifact.NewPackageSource(
		artifact.NewCmd("not-used", "install", "amazon-ssm-agent"),
		artifact.NewCmd("rm", "-rf", filepath.Join(m.InstallRoot, "/usr/bin/ssm-agent-worker")),
		artifact.NewCmd("not-used", "update", "amazon-ssm-agent"),
	)
}

func TestUninstall(t *testing.T) {
	tests := []struct {
		name                              string
		registration                      ssm.HybridInstanceRegistration
		registrationErr                   error
		describeInstanceInformationOutput *awsSsm.DescribeInstanceInformationOutput
		describeInstanceInformationErr    error
		deregisterManagedInstanceOutput   *awsSsm.DeregisterManagedInstanceOutput
		deregisterManagedInstanceErr      error
		wantErr                           string
	}{
		{
			name:            "registration file does not exist",
			registrationErr: os.ErrNotExist,
			wantErr:         "",
		},
		{
			name: "instance is managed and deregister succeeds",
			registration: ssm.HybridInstanceRegistration{
				ManagedInstanceID: "i-1234567890abcdef0",
				Region:            "us-west-2",
			},
			describeInstanceInformationOutput: &awsSsm.DescribeInstanceInformationOutput{
				InstanceInformationList: []types.InstanceInformation{
					{
						InstanceId: aws.String("i-1234567890abcdef0"),
					},
				},
			},
			deregisterManagedInstanceOutput: &awsSsm.DeregisterManagedInstanceOutput{},
		},
		{
			name: "instance is managed but deregister fails",
			registration: ssm.HybridInstanceRegistration{
				ManagedInstanceID: "i-1234567890abcdef0",
				Region:            "us-west-2",
			},
			describeInstanceInformationOutput: &awsSsm.DescribeInstanceInformationOutput{
				InstanceInformationList: []types.InstanceInformation{
					{
						InstanceId: aws.String("i-1234567890abcdef0"),
					},
				},
			},
			deregisterManagedInstanceOutput: &awsSsm.DeregisterManagedInstanceOutput{},
			deregisterManagedInstanceErr:    fmt.Errorf("deregister failed"),
			wantErr:                         "deregistering ssm managed instance: deregister failed",
		},
		{
			name: "instance is not managed",
			registration: ssm.HybridInstanceRegistration{
				ManagedInstanceID: "i-1234567890abcdef0",
				Region:            "us-west-2",
			},
			describeInstanceInformationOutput: &awsSsm.DescribeInstanceInformationOutput{
				InstanceInformationList: []types.InstanceInformation{},
			},
			wantErr: "",
		},
		{
			name: "check managed status fails",
			registration: ssm.HybridInstanceRegistration{
				ManagedInstanceID: "i-1234567890abcdef0",
				Region:            "us-west-2",
			},
			describeInstanceInformationErr: fmt.Errorf("check managed status failed"),
			wantErr:                        "getting managed instance information: check managed status failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			tmpDir := t.TempDir()
			var registrationFile string
			// Create registration file if needed
			if tt.registration.ManagedInstanceID != "" {
				registrationFile = filepath.Join(tmpDir, "/var/lib/amazon/ssm/registration")
				data, err := json.Marshal(tt.registration)
				g.Expect(err).NotTo(HaveOccurred())
				err = os.MkdirAll(filepath.Dir(registrationFile), 0o755)
				g.Expect(err).NotTo(HaveOccurred())
				err = os.WriteFile(registrationFile, data, 0o644)
				g.Expect(err).NotTo(HaveOccurred())
			}

			// not matter if the instnace is registered or not, the aws config files should be removed
			err := os.MkdirAll(filepath.Join(tmpDir, "/root/.aws"), 0o755)
			g.Expect(err).NotTo(HaveOccurred())
			err = os.MkdirAll(filepath.Join(tmpDir, "/eks-hybrid/.aws"), 0o755)
			g.Expect(err).NotTo(HaveOccurred())
			err = os.MkdirAll(filepath.Join(tmpDir, "/etc/amazon"), 0o755)
			g.Expect(err).NotTo(HaveOccurred())
			// ensure the ssm-agent-worker file is removed via the testpkgsource removal
			err = os.MkdirAll(filepath.Join(tmpDir, "/usr/bin"), 0o755)
			g.Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(filepath.Join(tmpDir, "/usr/bin/ssm-agent-worker"), []byte(""), 0o644)
			g.Expect(err).NotTo(HaveOccurred())

			// Create and setup mock SSM client
			mockSSM := MockSSMClient{
				g:                                 g,
				instanceId:                        tt.registration.ManagedInstanceID,
				describeInstanceInformationOutput: tt.describeInstanceInformationOutput,
				describeInstanceInformationErr:    tt.describeInstanceInformationErr,
				deregisterManagedInstanceOutput:   tt.deregisterManagedInstanceOutput,
				deregisterManagedInstanceErr:      tt.deregisterManagedInstanceErr,
			}

			err = ssm.Uninstall(context.Background(), ssm.UninstallOptions{
				Logger:          zap.NewNop(),
				SSMRegistration: ssm.NewSSMRegistration(ssm.WithInstallRoot(tmpDir)),
				SSMClient:       &mockSSM,
				PkgSource: &TestPackageManager{
					InstallRoot: tmpDir,
				},
				InstallRoot: tmpDir,
			})

			if tt.wantErr != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}

			if tt.registration.ManagedInstanceID != "" {
				g.Expect(registrationFile).NotTo(BeAnExistingFile())
			}
			g.Expect(filepath.Join(tmpDir, "/etc/amazon")).NotTo(BeADirectory())
			g.Expect(filepath.Join(tmpDir, "/root/.aws")).NotTo(BeADirectory())
			g.Expect(filepath.Join(tmpDir, "/eks-hybrid/.aws")).NotTo(BeADirectory())
			g.Expect(filepath.Join(tmpDir, "/usr/bin/ssm-agent-worker")).NotTo(BeAnExistingFile())
		})
	}
}
