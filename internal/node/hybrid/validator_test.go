package hybrid_test

import (
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/node/hybrid"
	"github.com/aws/eks-hybrid/internal/test"
)

func Test_HybridNodeProviderValidateConfig(t *testing.T) {
	g := NewWithT(t)
	tmpDir := t.TempDir()

	// Certificate paths for validation
	certPath := tmpDir + "/my-server.crt"
	invalidCA := tmpDir + "/my-server_invalid.crt"
	expiredCertPath := tmpDir + "/my-server_expired.crt"
	wrongPermCertPath := tmpDir + "/my-server_wrong_perm.crt"
	invalidSysTimeCertPath := tmpDir + "/my-server_invalid_systime.crt"

	// Adding certificates to local files
	caBytes, ca, caKey := test.GenerateCA(g)
	g.Expect(os.WriteFile(certPath, caBytes, 0o644)).To(Succeed())
	g.Expect(os.WriteFile(wrongPermCertPath, caBytes, 0o333)).To(Succeed())
	g.Expect(os.WriteFile(invalidCA, []byte("invalid ca data"), 0o644)).To(Succeed())

	expiredCABytes := test.GenerateKubeletCert(g, ca, caKey, time.Now().AddDate(0, 0, -1), time.Now().AddDate(0, 0, -1))
	g.Expect(os.WriteFile(expiredCertPath, expiredCABytes, 0o644)).To(Succeed())

	invalidSysTimeCABytes := test.GenerateKubeletCert(g, ca, caKey, time.Now().AddDate(0, 0, 10), time.Now().AddDate(0, 0, 20))
	g.Expect(os.WriteFile(invalidSysTimeCertPath, invalidSysTimeCABytes, 0o644)).To(Succeed())

	// Key path for validation
	keyPath := tmpDir + "/my-server.key"
	g.Expect(os.WriteFile(keyPath, []byte("key"), 0o644)).To(Succeed())

	testCases := []struct {
		name      string
		node      *api.NodeConfig
		wantError string
	}{
		// IAM Roles Anywhere NodeConfig spec validation
		{
			name: "happy path",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:        "my-node",
							TrustAnchorARN:  "trust-anchor-arn",
							ProfileARN:      "profile-arn",
							RoleARN:         "role-arn",
							CertificatePath: certPath,
							PrivateKeyPath:  keyPath,
						},
					},
				},
			},
		},
		{
			name: "no node name",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							TrustAnchorARN: "trust-anchor-arn",
							ProfileARN:     "profile-arn",
							RoleARN:        "role-arn",
						},
					},
				},
			},
			wantError: "NodeName can't be empty in hybrid iam roles anywhere configuration",
		},
		{
			name: "node name too long",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:       "my-node-too-long-1111111111111111111111111111111111111111111111111111",
							TrustAnchorARN: "trust-anchor-arn",
							ProfileARN:     "profile-arn",
							RoleARN:        "role-arn",
						},
					},
				},
			},
			wantError: "NodeName can't be longer than 64 characters in hybrid iam roles anywhere configuration",
		},
		{
			name: "no certificate path",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:       "my-node",
							TrustAnchorARN: "trust-anchor-arn",
							ProfileARN:     "profile-arn",
							RoleARN:        "role-arn",
							PrivateKeyPath: "/etc/certificates/iam/pki/my-server.key",
						},
					},
				},
			},
			wantError: "CertificatePath is missing in hybrid iam roles anywhere configuration",
		},
		{
			name: "no private key path",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:        "my-node",
							TrustAnchorARN:  "trust-anchor-arn",
							ProfileARN:      "profile-arn",
							RoleARN:         "role-arn",
							CertificatePath: certPath,
						},
					},
				},
			},
			wantError: "PrivateKeyPath is missing in hybrid iam roles anywhere configuration",
		},
		{
			name: "no certificate",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:        "my-node",
							TrustAnchorARN:  "trust-anchor-arn",
							ProfileARN:      "profile-arn",
							RoleARN:         "role-arn",
							PrivateKeyPath:  keyPath,
							CertificatePath: tmpDir + "/missing.crt",
						},
					},
				},
			},
			wantError: "IAM Roles Anywhere certificate " + tmpDir + "/missing.crt not found",
		},
		{
			name: "no private key",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:        "my-node",
							TrustAnchorARN:  "trust-anchor-arn",
							ProfileARN:      "profile-arn",
							RoleARN:         "role-arn",
							CertificatePath: certPath,
							PrivateKeyPath:  tmpDir + "/missing.key",
						},
					},
				},
			},
			wantError: "IAM Roles Anywhere private key " + tmpDir + "/missing.key not found",
		},
		{
			name: "hostname-override present",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:        "my-node",
							TrustAnchorARN:  "trust-anchor-arn",
							ProfileARN:      "profile-arn",
							RoleARN:         "role-arn",
							CertificatePath: certPath,
							PrivateKeyPath:  keyPath,
						},
					},
					Kubelet: api.KubeletOptions{
						Flags: []string{"--hostname-override=bad-config"},
					},
				},
			},
			wantError: "hostname-override kubelet flag is not supported for hybrid nodes but found override: bad-config",
		},
		{
			name: "certificate with wrong permission",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:        "my-node",
							TrustAnchorARN:  "trust-anchor-arn",
							ProfileARN:      "profile-arn",
							RoleARN:         "role-arn",
							PrivateKeyPath:  keyPath,
							CertificatePath: wrongPermCertPath,
						},
					},
				},
			},
			wantError: "error in validating iam-roles-anywhere certificate: reading certificate: open " + wrongPermCertPath + ": permission denied",
		},
		{
			name: "invalid certificate",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:        "my-node",
							TrustAnchorARN:  "trust-anchor-arn",
							ProfileARN:      "profile-arn",
							RoleARN:         "role-arn",
							PrivateKeyPath:  keyPath,
							CertificatePath: invalidCA,
						},
					},
				},
			},
			wantError: "error in validating iam-roles-anywhere certificate: parsing certificate",
		},
		{
			name: "expired certificate",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:        "my-node",
							TrustAnchorARN:  "trust-anchor-arn",
							ProfileARN:      "profile-arn",
							RoleARN:         "role-arn",
							PrivateKeyPath:  keyPath,
							CertificatePath: expiredCertPath,
						},
					},
				},
			},
			wantError: "error in validating iam-roles-anywhere certificate: server certificate has expired",
		},
		{
			name: "invalid systime certificate",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:        "my-node",
							TrustAnchorARN:  "trust-anchor-arn",
							ProfileARN:      "profile-arn",
							RoleARN:         "role-arn",
							PrivateKeyPath:  keyPath,
							CertificatePath: invalidSysTimeCertPath,
						},
					},
				},
			},
			wantError: "error in validating iam-roles-anywhere certificate: server certificate is not yet valid",
		},
		{
			name: "invalid when both iamRoleAnywhere and ssm provided",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:        "my-node",
							TrustAnchorARN:  "trust-anchor-arn",
							ProfileARN:      "profile-arn",
							RoleARN:         "role-arn",
							CertificatePath: certPath,
							PrivateKeyPath:  keyPath,
						},
						SSM: &api.SSM{
							ActivationID:   "activation-id",
							ActivationCode: "activation-code",
						},
					},
				},
			},
			wantError: "Only one of IAMRolesAnywhere or SSM must be provided for hybrid node configuration",
		},
		{
			name: "valid ssm activation code and activation id",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						SSM: &api.SSM{
							ActivationCode: "Fjz3/sZfSvv78EXAMPLE",
							ActivationID:   "e488f2f6-e686-4afb-8a04-ef6dfabcdeff",
						},
					},
				},
			},
		},
		{
			name: "missing ssm activation code",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						SSM: &api.SSM{
							ActivationCode: "",
							ActivationID:   "e488f2f6-e686-4afb-8a04-ef6dfabcdeff",
						},
					},
				},
			},
			wantError: "ActivationCode is missing in hybrid ssm configuration",
		},
		{
			name: "missing ssm activation id",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						SSM: &api.SSM{
							ActivationCode: "Fjz3/sZfSvv78EXAMPLE",
							ActivationID:   "",
						},
					},
				},
			},
			wantError: "ActivationID is missing in hybrid ssm configuration",
		},
		{
			name: "invalid ssm activation code (too short)",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						SSM: &api.SSM{
							ActivationCode: "activation-code",
							ActivationID:   "e488f2f6-e686-4afb-8a04-ef6dfabcdeff",
						},
					},
				},
			},
			wantError: "invalid ActivationCode format: activation-code. Must be 20-250 characters",
		},
		{
			name: "invalid ssm activation code (too long - 251 chars)",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						SSM: &api.SSM{
							ActivationCode: strings.Repeat("a", 251),
							ActivationID:   "e488f2f6-e686-4afb-8a04-ef6dfabcdeff",
						},
					},
				},
			},
			wantError: "invalid ActivationCode format: " + strings.Repeat("a", 251) + ". Must be 20-250 characters",
		},
		{
			name: "invalid ssm activation id by length",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						SSM: &api.SSM{
							ActivationCode: "Fjz3/sZfSvv78EXAMPLE",
							ActivationID:   "e488f2f6-e686-4afb-8a04-ef6dfabcdefff",
						},
					},
				},
			},
			wantError: "invalid ActivationID format: e488f2f6-e686-4afb-8a04-ef6dfabcdefff. Must be in format: ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$",
		},
		{
			name: "invalid ssm activation id by characters",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
						Name:   "my-cluster",
					},
					Hybrid: &api.HybridOptions{
						SSM: &api.SSM{
							ActivationCode: "Fjz3/sZfSvv78EXAMPLE",
							ActivationID:   "e488f2f6-e686-4afb-8A04-ef6dfabcdefff",
						},
					},
				},
			},
			wantError: "invalid ActivationID format: e488f2f6-e686-4afb-8A04-ef6dfabcdefff. Must be in format: ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			p, err := hybrid.NewHybridNodeProvider(tc.node, []string{}, zap.NewNop())
			g.Expect(err).NotTo(HaveOccurred())

			err = p.ValidateConfig()
			if tc.wantError == "" {
				g.Expect(err).NotTo(HaveOccurred())
			} else {
				g.Expect(err).To(MatchError(tc.wantError))
			}
		})
	}
}
