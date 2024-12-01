package kubernetes_test

import (
	"context"
	"net/http"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/internal/test"
	"github.com/aws/eks-hybrid/internal/validation"
)

func TestCheckConnectionSuccess(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()
	informer := test.NewFakeInformer()

	server := test.NewHTTPSServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	config := &api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				APIServerEndpoint: server.URL,
			},
		},
	}
	g.Expect(kubernetes.CheckConnection(ctx, informer, config)).To(Succeed())
}

func TestCheckConnectionInvalidURL(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()
	informer := test.NewFakeInformer()

	config := &api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				APIServerEndpoint: "\n",
			},
		},
	}

	err := kubernetes.CheckConnection(ctx, informer, config)
	g.Expect(err).NotTo(Succeed())
	g.Expect(informer.Started).To(BeTrue())
	g.Expect(informer.DoneWith).To(HaveOccurred())
	g.Expect(validation.Remediation(informer.DoneWith)).To(Equal("Ensure the Kubernetes API server endpoint provided is correct."))
}

func TestCheckConnectionFailureWithAccess(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()
	informer := test.NewFakeInformer()

	config := &api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				APIServerEndpoint: "https://localhost:1234",
			},
		},
	}

	err := kubernetes.CheckConnection(ctx, informer, config)
	g.Expect(err).NotTo(Succeed())
	g.Expect(informer.Started).To(BeTrue())
	g.Expect(informer.DoneWith).To(HaveOccurred())
	g.Expect(validation.Remediation(informer.DoneWith)).To(Equal("Ensure your network configuration allows the node to access the Kubernetes API endpoint."))
}
