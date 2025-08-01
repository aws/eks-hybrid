package kubernetes_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/internal/test"
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

	v := kubernetes.NewAccessValidator(&node.Spec.Cluster)

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

	v := kubernetes.NewAccessValidator(&node.Spec.Cluster)

	g.Expect(v.Run(ctx, informer, node)).To(Succeed())
	g.Expect(informer.Started).To(BeTrue())
	g.Expect(informer.DoneWith).To(BeNil())
}
