package hybrid_test

import (
	"context"
	"encoding/base64"
	"testing"

	aws_sdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/node/hybrid"
	"github.com/aws/eks-hybrid/internal/test"
)

func Test_hybridNodeProvider_Enrich(t *testing.T) {
	testCases := []struct {
		name               string
		cluster            *types.Cluster
		node               *api.NodeConfig
		wantClusterDetails api.ClusterDetails
		wantStatus         api.NodeConfigStatus
		wantErr            string
	}{
		{
			name: "needs all cluster details",
			cluster: &types.Cluster{
				Endpoint: aws_sdk.String("https://my-endpoint.example.com"),
				Name:     aws_sdk.String("my-cluster"),
				Status:   types.ClusterStatusActive,
				CertificateAuthority: &types.Certificate{
					Data: aws_sdk.String(base64.StdEncoding.EncodeToString([]byte("my-ca-cert"))),
				},
				KubernetesNetworkConfig: &types.KubernetesNetworkConfigResponse{
					ServiceIpv4Cidr: aws_sdk.String("172.0.0.0/16"),
				},
				RemoteNetworkConfig: &types.RemoteNetworkConfigResponse{
					RemoteNodeNetworks: []types.RemoteNodeNetwork{
						{
							Cidrs: []string{"10.1.0.0/16"},
						},
					},
				},
			},
			node: &api.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-node",
				},
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:   "my-cluster",
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
				Status: api.NodeConfigStatus{
					Hybrid: api.HybridDetails{
						NodeName: "my-node",
					},
				},
			},
			wantClusterDetails: api.ClusterDetails{
				Name:                 "my-cluster",
				Region:               "us-west-2",
				APIServerEndpoint:    "https://my-endpoint.example.com",
				CertificateAuthority: []byte("my-ca-cert"),
				CIDR:                 "172.0.0.0/16",
			},
			wantStatus: api.NodeConfigStatus{
				Hybrid: api.HybridDetails{NodeName: "my-node"},
				Defaults: api.DefaultOptions{
					SandboxImage: "602401143452.dkr.ecr.us-west-2.amazonaws.com/eks/pause:3.5",
				},
			},
		},
		{
			name: "cluster is not active",
			cluster: &types.Cluster{
				Endpoint: aws_sdk.String("https://my-endpoint.example.com"),
				Name:     aws_sdk.String("my-cluster"),
				Status:   types.ClusterStatusCreating,
				CertificateAuthority: &types.Certificate{
					Data: aws_sdk.String(base64.StdEncoding.EncodeToString([]byte("my-ca-cert"))),
				},
				KubernetesNetworkConfig: &types.KubernetesNetworkConfigResponse{
					ServiceIpv4Cidr: aws_sdk.String("172.0.0.0/16"),
				},
				RemoteNetworkConfig: &types.RemoteNetworkConfigResponse{
					RemoteNodeNetworks: []types.RemoteNodeNetwork{
						{
							Cidrs: []string{"10.1.0.0/16"},
						},
					},
				},
			},
			node: &api.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-node",
				},
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:   "my-cluster",
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
				Status: api.NodeConfigStatus{
					Hybrid: api.HybridDetails{
						NodeName: "my-node",
					},
				},
			},
			wantErr: "eks cluster is not active",
		},
		{
			name: "cluster is not active",
			cluster: &types.Cluster{
				Endpoint: aws_sdk.String("https://my-endpoint.example.com"),
				Name:     aws_sdk.String("my-cluster"),
				Status:   types.ClusterStatusActive,
				CertificateAuthority: &types.Certificate{
					Data: aws_sdk.String(base64.StdEncoding.EncodeToString([]byte("my-ca-cert"))),
				},
				KubernetesNetworkConfig: &types.KubernetesNetworkConfigResponse{
					ServiceIpv4Cidr: aws_sdk.String("172.0.0.0/16"),
				},
			},
			node: &api.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-node",
				},
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:   "my-cluster",
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
				Status: api.NodeConfigStatus{
					Hybrid: api.HybridDetails{
						NodeName: "my-node",
					},
				},
			},
			wantErr: "eks cluster does not have remoteNetworkConfig enabled, which is required for Hybrid Nodes",
		},
		{
			name: "endpoint is configured",
			cluster: &types.Cluster{
				Endpoint: aws_sdk.String("https://my-endpoint.example.com"),
				Name:     aws_sdk.String("my-cluster"),
				Status:   types.ClusterStatusActive,
				CertificateAuthority: &types.Certificate{
					Data: aws_sdk.String(base64.StdEncoding.EncodeToString([]byte("my-ca-cert"))),
				},
				KubernetesNetworkConfig: &types.KubernetesNetworkConfigResponse{
					ServiceIpv4Cidr: aws_sdk.String("172.0.0.0/16"),
				},
				RemoteNetworkConfig: &types.RemoteNetworkConfigResponse{
					RemoteNodeNetworks: []types.RemoteNodeNetwork{
						{
							Cidrs: []string{"10.1.0.0/16"},
						},
					},
				},
			},
			node: &api.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-node",
				},
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:              "my-cluster",
						Region:            "us-west-2",
						APIServerEndpoint: "https://my-endpoint-2.example.com",
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
				Status: api.NodeConfigStatus{
					Hybrid: api.HybridDetails{
						NodeName: "my-node",
					},
				},
			},
			wantClusterDetails: api.ClusterDetails{
				Name:                 "my-cluster",
				Region:               "us-west-2",
				APIServerEndpoint:    "https://my-endpoint-2.example.com",
				CertificateAuthority: []byte("my-ca-cert"),
				CIDR:                 "172.0.0.0/16",
			},
			wantStatus: api.NodeConfigStatus{
				Hybrid: api.HybridDetails{NodeName: "my-node"},
				Defaults: api.DefaultOptions{
					SandboxImage: "602401143452.dkr.ecr.us-west-2.amazonaws.com/eks/pause:3.5",
				},
			},
		},
		{
			name: "CA is configured",
			cluster: &types.Cluster{
				Endpoint: aws_sdk.String("https://my-endpoint.example.com"),
				Name:     aws_sdk.String("my-cluster"),
				Status:   types.ClusterStatusActive,
				CertificateAuthority: &types.Certificate{
					Data: aws_sdk.String(base64.StdEncoding.EncodeToString([]byte("my-ca-cert"))),
				},
				KubernetesNetworkConfig: &types.KubernetesNetworkConfigResponse{
					ServiceIpv4Cidr: aws_sdk.String("172.0.0.0/16"),
				},
				RemoteNetworkConfig: &types.RemoteNetworkConfigResponse{
					RemoteNodeNetworks: []types.RemoteNodeNetwork{
						{
							Cidrs: []string{"10.1.0.0/16"},
						},
					},
				},
			},
			node: &api.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-node",
				},
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:                 "my-cluster",
						Region:               "us-west-2",
						CertificateAuthority: []byte("my-other-CA"),
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
				Status: api.NodeConfigStatus{
					Hybrid: api.HybridDetails{
						NodeName: "my-node",
					},
				},
			},
			wantClusterDetails: api.ClusterDetails{
				Name:                 "my-cluster",
				Region:               "us-west-2",
				APIServerEndpoint:    "https://my-endpoint.example.com",
				CertificateAuthority: []byte("my-other-CA"),
				CIDR:                 "172.0.0.0/16",
			},
			wantStatus: api.NodeConfigStatus{
				Hybrid: api.HybridDetails{NodeName: "my-node"},
				Defaults: api.DefaultOptions{
					SandboxImage: "602401143452.dkr.ecr.us-west-2.amazonaws.com/eks/pause:3.5",
				},
			},
		},
		{
			name: "service CIDR is configured",
			cluster: &types.Cluster{
				Endpoint: aws_sdk.String("https://my-endpoint.example.com"),
				Name:     aws_sdk.String("my-cluster"),
				Status:   types.ClusterStatusActive,
				CertificateAuthority: &types.Certificate{
					Data: aws_sdk.String(base64.StdEncoding.EncodeToString([]byte("my-ca-cert"))),
				},
				KubernetesNetworkConfig: &types.KubernetesNetworkConfigResponse{
					ServiceIpv4Cidr: aws_sdk.String("172.0.0.0/16"),
				},
				RemoteNetworkConfig: &types.RemoteNetworkConfigResponse{
					RemoteNodeNetworks: []types.RemoteNodeNetwork{
						{
							Cidrs: []string{"10.1.0.0/16"},
						},
					},
				},
			},
			node: &api.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-node",
				},
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:   "my-cluster",
						Region: "us-west-2",
						CIDR:   "172.1.0.0/16",
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
				Status: api.NodeConfigStatus{
					Hybrid: api.HybridDetails{
						NodeName: "my-node",
					},
				},
			},
			wantClusterDetails: api.ClusterDetails{
				Name:                 "my-cluster",
				Region:               "us-west-2",
				APIServerEndpoint:    "https://my-endpoint.example.com",
				CertificateAuthority: []byte("my-ca-cert"),
				CIDR:                 "172.1.0.0/16",
			},
			wantStatus: api.NodeConfigStatus{
				Hybrid: api.HybridDetails{NodeName: "my-node"},
				Defaults: api.DefaultOptions{
					SandboxImage: "602401143452.dkr.ecr.us-west-2.amazonaws.com/eks/pause:3.5",
				},
			},
		},
		{
			name: "node config has all cluster details",
			cluster: &types.Cluster{
				Endpoint: aws_sdk.String("https://my-endpoint.example.com"),
				Name:     aws_sdk.String("my-cluster"),
				Status:   types.ClusterStatusActive,
				CertificateAuthority: &types.Certificate{
					Data: aws_sdk.String(base64.StdEncoding.EncodeToString([]byte("my-ca-cert"))),
				},
				KubernetesNetworkConfig: &types.KubernetesNetworkConfigResponse{
					ServiceIpv4Cidr: aws_sdk.String("172.0.0.0/16"),
				},
				RemoteNetworkConfig: &types.RemoteNetworkConfigResponse{
					RemoteNodeNetworks: []types.RemoteNodeNetwork{
						{
							Cidrs: []string{"10.1.0.0/16"},
						},
					},
				},
			},
			node: &api.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-node",
				},
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:                 "my-cluster",
						Region:               "us-west-2",
						APIServerEndpoint:    "https://my-endpoint-2.example.com",
						CertificateAuthority: []byte("my-other-CA"),
						CIDR:                 "172.1.0.0/16",
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
				Status: api.NodeConfigStatus{
					Hybrid: api.HybridDetails{
						NodeName: "my-node",
					},
				},
			},
			wantClusterDetails: api.ClusterDetails{
				Name:                 "my-cluster",
				Region:               "us-west-2",
				APIServerEndpoint:    "https://my-endpoint-2.example.com",
				CertificateAuthority: []byte("my-other-CA"),
				CIDR:                 "172.1.0.0/16",
			},
			wantStatus: api.NodeConfigStatus{
				Hybrid: api.HybridDetails{NodeName: "my-node"},
				Defaults: api.DefaultOptions{
					SandboxImage: "602401143452.dkr.ecr.us-west-2.amazonaws.com/eks/pause:3.5",
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()

			resp := &eks.DescribeClusterOutput{
				Cluster: tc.cluster,
			}

			server := test.NewEKSDescribeClusterAPI(t, resp)

			config := &aws_sdk.Config{
				BaseEndpoint: &server.URL,
				HTTPClient:   server.Client(),
			}

			p, err := hybrid.NewHybridNodeProvider(tc.node, zap.NewNop(),
				hybrid.WithAWSConfig(config),
			)
			g.Expect(err).To(Succeed())

			err = p.Enrich(ctx)
			if tc.wantErr != "" {
				g.Expect(err).To(MatchError(ContainSubstring(tc.wantErr)))
			} else {
				g.Expect(err).To(Succeed())
				g.Expect(tc.node.Spec.Cluster).To(Equal(tc.wantClusterDetails))
				g.Expect(tc.node.Status).To(Equal(tc.wantStatus))
			}
		})
	}
}
