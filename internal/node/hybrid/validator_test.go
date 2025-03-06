package hybrid_test

import (
	"os"
	"testing"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/node/hybrid"
)

func Test_HybridNodeProviderValidateConfig(t *testing.T) {
	g := NewWithT(t)
	tmpDir := t.TempDir()
	certPath := tmpDir + "/my-server.crt"
	keyPath := tmpDir + "/my-server.key"
	g.Expect(os.WriteFile(certPath, []byte("cert"), 0o644)).To(Succeed())
	g.Expect(os.WriteFile(keyPath, []byte("key"), 0o644)).To(Succeed())

	testCases := []struct {
		name      string
		node      *api.NodeConfig
		wantError string
	}{
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
							CertificatePath: "/etc/certificates/iam/pki/my-server.crt",
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
