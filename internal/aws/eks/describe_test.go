package eks_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"testing"

	aws_sdk "github.com/aws/aws-sdk-go-v2/aws"
	eks_sdk "github.com/aws/aws-sdk-go-v2/service/eks"
	eks_sdk_types "github.com/aws/aws-sdk-go-v2/service/eks/types"
	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/aws/eks"
	"github.com/aws/eks-hybrid/internal/test"
)

func TestDescribeClusterSuccess(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	resp := &eks_sdk.DescribeClusterOutput{
		Cluster: &eks_sdk_types.Cluster{
			Endpoint: aws_sdk.String("https://my-endpoint.example.com"),
			Name:     aws_sdk.String("my-cluster"),
			Status:   eks_sdk_types.ClusterStatusActive,
			CertificateAuthority: &eks_sdk_types.Certificate{
				Data: aws_sdk.String(base64.StdEncoding.EncodeToString([]byte("my-ca-cert"))),
			},
			KubernetesNetworkConfig: &eks_sdk_types.KubernetesNetworkConfigResponse{
				ServiceIpv4Cidr: aws_sdk.String("172.0.0.0/16"),
			},
		},
	}

	server := test.NewEKSDescribeClusterAPI(t, resp)

	config := aws_sdk.Config{
		BaseEndpoint: &server.URL,
		HTTPClient:   server.Client(),
	}
	client := eks.NewClient(config)

	cluster, err := eks.DescribeCluster(ctx, client, "my-cluster")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cluster).NotTo(BeNil())
}

func TestDescribeClusterError(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	resp := &eks_sdk.DescribeClusterOutput{
		Cluster: &eks_sdk_types.Cluster{
			Endpoint: aws_sdk.String("https://my-endpoint.example.com"),
			Name:     aws_sdk.String("my-cluster"),
			Status:   eks_sdk_types.ClusterStatusActive,
			CertificateAuthority: &eks_sdk_types.Certificate{
				Data: aws_sdk.String(base64.StdEncoding.EncodeToString([]byte("my-ca-cert"))),
			},
			KubernetesNetworkConfig: &eks_sdk_types.KubernetesNetworkConfigResponse{
				ServiceIpv4Cidr: aws_sdk.String("172.0.0.0/16"),
			},
		},
	}

	server := test.NewHTTPSServerForJSON(t, http.StatusNotFound, resp)

	config := aws_sdk.Config{
		BaseEndpoint: &server.URL,
		HTTPClient:   server.Client(),
	}
	client := eks.NewClient(config)

	cluster, err := eks.DescribeCluster(ctx, client, "my-cluster")
	g.Expect(err).To(MatchError(ContainSubstring("404")))
	g.Expect(cluster).To(BeNil())
}
