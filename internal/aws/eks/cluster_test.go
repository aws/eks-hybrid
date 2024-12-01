package eks_test

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks_sdk "github.com/aws/aws-sdk-go-v2/service/eks/types"
	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/aws/eks"
	"github.com/aws/eks-hybrid/internal/test"
)

func TestReadClusterDetailsAlreadyHasAllDetails(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()

	config := aws.Config{}

	node := &api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Name:                 "test",
				Region:               "us-west-2",
				APIServerEndpoint:    "https://my-endpoint.example.com",
				CertificateAuthority: []byte("my-ca-cert"),
				CIDR:                 "172.0.0.0/16",
			},
		},
	}

	cluster, err := eks.ReadClusterDetails(ctx, config, node)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(*cluster).To(BeComparableTo(node.Spec.Cluster))
}

func TestReadClusterDetailsSuccess(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()

	resp := &eks.DescribeClusterOutput{
		Cluster: &eks.Cluster{
			Endpoint: aws.String("https://my-endpoint.example.com"),
			Name:     aws.String("my-cluster"),
			Status:   eks_sdk.ClusterStatusActive,
			CertificateAuthority: &eks_sdk.Certificate{
				Data: aws.String(base64.StdEncoding.EncodeToString([]byte("my-ca-cert"))),
			},
			KubernetesNetworkConfig: &eks_sdk.KubernetesNetworkConfigResponse{
				ServiceIpv4Cidr: aws.String("172.0.0.0/16"),
			},
		},
	}

	server := test.NewEKSDescribeClusterAPI(t, resp)

	config := aws.Config{
		BaseEndpoint: &server.URL,
		HTTPClient:   server.Client(),
	}

	node := &api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Name:   "test",
				Region: "us-west-2",
			},
		},
	}

	cluster, err := eks.ReadClusterDetails(ctx, config, node)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cluster).NotTo(BeNil())
	g.Expect(cluster.APIServerEndpoint).To(Equal(*resp.Cluster.Endpoint))
	g.Expect(cluster.CertificateAuthority).To(Equal([]byte("my-ca-cert")))
	g.Expect(cluster.CIDR).To(Equal(*resp.Cluster.KubernetesNetworkConfig.ServiceIpv4Cidr))
}

func TestReadClusterDetailsErrorClusterNotActive(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()

	resp := &eks.DescribeClusterOutput{
		Cluster: &eks.Cluster{
			Endpoint: aws.String("https://my-endpoint.example.com"),
			Name:     aws.String("my-cluster"),
			Status:   eks_sdk.ClusterStatusCreating,
		},
	}

	server := test.NewEKSDescribeClusterAPI(t, resp)

	config := aws.Config{
		BaseEndpoint: &server.URL,
		HTTPClient:   server.Client(),
	}

	node := &api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Name:   "test",
				Region: "us-west-2",
			},
		},
	}

	_, err := eks.ReadClusterDetails(ctx, config, node)
	g.Expect(err).To(MatchError(ContainSubstring("eks cluster my-cluster is not active")))
}
