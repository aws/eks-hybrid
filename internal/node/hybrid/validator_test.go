package hybrid_test

import (
	"os"
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
			wantError: "IAM Roles Anywhere certificate error - reading iam-roles-anywhere certificate: open " + wrongPermCertPath + ": permission denied",
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
			wantError: "IAM Roles Anywhere certificate error - parsing iam-roles-anywhere certificate",
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
			wantError: "IAM Roles Anywhere certificate error - iam-roles-anywhere server certificate has expired",
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
			wantError: "IAM Roles Anywhere certificate error - iam-roles-anywhere server certificate is not yet valid",
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
