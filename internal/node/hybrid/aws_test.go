package hybrid_test

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/node/hybrid"
	"github.com/aws/eks-hybrid/internal/validation"
)

func TestRolesAnywhereAWSConfigurator_Configure(t *testing.T) {
	testCases := []struct {
		name    string
		node    *api.NodeConfig
		wantErr string
	}{
		{
			name: "happy path",
			node: &api.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-node",
				},
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:        "my-node",
							TrustAnchorARN:  "trust-anchor-arn",
							ProfileARN:      "profile-arn",
							RoleARN:         "role-arn",
							CertificatePath: "node.crt",
							PrivateKeyPath:  "node.key",
						},
					},
				},
				Status: api.NodeConfigStatus{
					Hybrid: api.HybridDetails{
						NodeName: "my-node",
					},
				},
			},
		},
		{
			name: "invalid node config",
			node: &api.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-node",
				},
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:        "my-node",
							TrustAnchorARN:  "trust-anchor-arn",
							ProfileARN:      "profile-arn",
							RoleARN:         "role-arn",
							CertificatePath: "node.crt",
							PrivateKeyPath:  "node.key",
						},
					},
				},
			},
			wantErr: "NodeName cannot be empty",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			configFile := filepath.Join(t.TempDir(), "aws-config.yaml")
			g := NewWithT(t)
			ctx := context.Background()

			c := hybrid.RolesAnywhereAWSConfigurator{}
			tc.node.Spec.Hybrid.IAMRolesAnywhere.AwsConfigPath = configFile

			err := c.Configure(ctx, tc.node)

			if tc.wantErr != "" {
				g.Expect(err).To(MatchError(ContainSubstring(tc.wantErr)))
				g.Expect(configFile).NotTo(BeAnExistingFile())
			} else {
				g.Expect(err).To(Succeed())
				g.Expect(configFile).To(BeAnExistingFile())
			}
		})
	}
}

func TestLoadAWSConfigForRolesAnywhere(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "aws-config.yaml")
	g := NewWithT(t)
	ctx := context.Background()
	node := &api.NodeConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-node",
		},
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Region: "us-west-2",
			},
			Hybrid: &api.HybridOptions{
				IAMRolesAnywhere: &api.IAMRolesAnywhere{
					AwsConfigPath:   configFile,
					NodeName:        "my-node",
					TrustAnchorARN:  "trust-anchor-arn",
					ProfileARN:      "profile-arn",
					RoleARN:         "role-arn",
					CertificatePath: "node.crt",
					PrivateKeyPath:  "node.key",
				},
			},
		},
		Status: api.NodeConfigStatus{
			Hybrid: api.HybridDetails{
				NodeName: "my-node",
			},
		},
	}

	c := hybrid.RolesAnywhereAWSConfigurator{}
	g.Expect(c.Configure(ctx, node)).To(Succeed())

	awsConfig, err := hybrid.LoadAWSConfigForRolesAnywhere(ctx, node)
	g.Expect(err).To(Succeed())
	g.Expect(awsConfig.Region).To(Equal("us-west-2"))
}

func Test_HybridNodeProvider_ConfigureAws_RolesAnywhere(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "aws-config.yaml")
	g := NewWithT(t)
	ctx := context.Background()
	node := &api.NodeConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-node",
		},
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Region: "us-west-2",
			},
			Hybrid: &api.HybridOptions{
				IAMRolesAnywhere: &api.IAMRolesAnywhere{
					AwsConfigPath:   configFile,
					NodeName:        "my-node",
					TrustAnchorARN:  "trust-anchor-arn",
					ProfileARN:      "profile-arn",
					RoleARN:         "role-arn",
					CertificatePath: "node.crt",
					PrivateKeyPath:  "node.key",
				},
			},
		},
		Status: api.NodeConfigStatus{
			Hybrid: api.HybridDetails{
				NodeName: "my-node",
			},
		},
	}

	p, err := hybrid.NewHybridNodeProvider(
		node,
		[]string{},
		zap.NewNop(),
		hybrid.WithSingleRunner(validation.NewNoopSingleRunner[*api.NodeConfig]()),
	)
	g.Expect(err).To(Succeed())
	g.Expect(p.ConfigureAws(ctx)).To(Succeed())
	g.Expect(p.GetConfig().Region).To(Equal("us-west-2"))
}
