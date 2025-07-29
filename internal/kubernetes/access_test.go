package kubernetes_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/internal/test"
	"github.com/aws/eks-hybrid/internal/validation"
)

func TestAccessValidatorRunFailSuccess(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()
	informer := test.NewFakeInformer()

	unauthResponse := &apiServerResponse{
		Status:  "Failure",
		Message: "Forbidden",
		Reason:  "Forbidden",
		Code:    403,
	}

	server := test.NewHTTPSServerForJSON(t, http.StatusForbidden, unauthResponse)

	node := &api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Name:                 "test",
				APIServerEndpoint:    server.URL,
				CertificateAuthority: server.CAPEM(),
				CIDR:                 "172.18.0.0/16",
			},
		},
	}

	config := aws.Config{}

	v := kubernetes.NewAccessValidator(kubernetes.NewClusterProvider(config))

	g.Expect(v.Run(ctx, informer, node)).To(Succeed())
	g.Expect(informer.Started).To(BeTrue())
	g.Expect(informer.DoneWith).To(BeNil())
}

func TestAccessValidatorRunFailReadingClusterDetails(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := test.ContextWithTimeout(t, 10*time.Millisecond)
	informer := test.NewFakeInformer()

	unauthResponse := &apiServerResponse{
		Status:  "Failure",
		Message: "Forbidden",
		Reason:  "Forbidden",
		Code:    403,
	}

	cluster := &eks.DescribeClusterOutput{}

	eksAPI := test.NewHTTPSServerForJSON(t, http.StatusForbidden, cluster)
	server := test.NewHTTPSServerForJSON(t, http.StatusForbidden, unauthResponse)

	node := &api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Name:                 "test",
				APIServerEndpoint:    server.URL,
				CertificateAuthority: server.CAPEM(),
			},
		},
	}

	config := aws.Config{
		BaseEndpoint: &eksAPI.URL,
		HTTPClient:   eksAPI.Client(),
	}

	v := kubernetes.NewAccessValidator(kubernetes.NewClusterProvider(config))

	err := v.Run(ctx, informer, node)
	g.Expect(err).To(HaveOccurred())
	g.Expect(informer.Started).To(BeTrue())
	g.Expect(informer.DoneWith).To(MatchError(ContainSubstring("https response error StatusCode: 403")))
	g.Expect(validation.Remediation(informer.DoneWith)).To(Equal("Either provide the Kubernetes API server endpoint or ensure the node has access and permissions to call DescribeCluster EKS API."))
}

func TestAccessValidatorRunFailValidatingAccess(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := test.ContextWithTimeout(t, 10*time.Millisecond)
	informer := test.NewFakeInformer()

	unauthResponse := &apiServerResponse{
		Status:  "Failure",
		Message: "Forbidden",
		Reason:  "Forbidden",
		Code:    403,
	}

	server := test.NewHTTPSServerForJSON(t, http.StatusForbidden, unauthResponse)

	node := &api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Name:                 "test",
				APIServerEndpoint:    "https://my-endpoint.example.com",
				CertificateAuthority: server.CAPEM(),
				CIDR:                 "172.18.0.0/16",
			},
		},
	}

	config := aws.Config{}

	v := kubernetes.NewAccessValidator(kubernetes.NewClusterProvider(config))

	err := v.Run(ctx, informer, node)
	g.Expect(err).To(HaveOccurred())
	g.Expect(informer.Started).To(BeTrue())
	g.Expect(informer.DoneWith).To(MatchError(ContainSubstring("failed to resolve hostname")))
	g.Expect(validation.Remediation(informer.DoneWith)).To(Equal("Ensure DNS server settings and network connectivity are correct, and verify the hostname is reachable"))
}
