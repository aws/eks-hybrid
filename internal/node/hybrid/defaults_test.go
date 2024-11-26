package hybrid_test

import (
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/node/hybrid"
)

func TestPopulateNodeDefaults(t *testing.T) {
	testCases := []struct {
		name string
		node *api.NodeConfig
		want *api.NodeConfig
	}{
		{
			name: "for SSM, nothing changes",
			node: &api.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-node",
				},
				Spec: api.NodeConfigSpec{
					Hybrid: &api.HybridOptions{
						SSM: &api.SSM{
							ActivationCode: "activation-code",
							ActivationID:   "activation-id",
						},
					},
				},
			},
			want: &api.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-node",
				},
				Spec: api.NodeConfigSpec{
					Hybrid: &api.HybridOptions{
						SSM: &api.SSM{
							ActivationCode: "activation-code",
							ActivationID:   "activation-id",
						},
					},
				},
			},
		},
		{
			name: "for IAM Roles ANywhere with no aws config path, set default",
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
							NodeName:       "my-node",
							TrustAnchorARN: "trust-anchor-arn",
							ProfileARN:     "profile-arn",
							RoleARN:        "role-arn",
						},
					},
				},
			},
			want: &api.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-node",
				},
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:       "my-node",
							TrustAnchorARN: "trust-anchor-arn",
							ProfileARN:     "profile-arn",
							RoleARN:        "role-arn",
							AwsConfigPath:  "/etc/aws/hybrid/config",
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
			name: "for IAM Roles ANywhere with custom aws config",
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
							NodeName:       "my-node",
							TrustAnchorARN: "trust-anchor-arn",
							ProfileARN:     "profile-arn",
							RoleARN:        "role-arn",
							AwsConfigPath:  "/etc/aws/hybrid/custom-config",
						},
					},
				},
			},
			want: &api.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-node",
				},
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Region: "us-west-2",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:       "my-node",
							TrustAnchorARN: "trust-anchor-arn",
							ProfileARN:     "profile-arn",
							RoleARN:        "role-arn",
							AwsConfigPath:  "/etc/aws/hybrid/custom-config",
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
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			nodeConfig := tc.node.DeepCopy()
			hybrid.PopulateNodeConfigDefaults(nodeConfig)
			g.Expect(nodeConfig).To(Equal(tc.want))
		})
	}
}