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
	"github.com/aws/eks-hybrid/internal/validation"
)

func TestCheckConnectionSuccess(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()

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

	validator := kubernetes.NewConnectionValidator()
	g.Expect(validator.CheckConnection(ctx, config)).To(Succeed())
}

func TestCheckConnectionInvalidURL(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := test.ContextWithTimeout(t, 10*time.Millisecond)
	informer := test.NewFakeInformer()

	config := &api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				APIServerEndpoint: "\n",
			},
		},
	}

	validator := kubernetes.NewConnectionValidator()
	err := validator.Run(ctx, informer, config)
	g.Expect(err).NotTo(Succeed())
	g.Expect(informer.Started).To(BeTrue())
	g.Expect(informer.DoneWith).To(HaveOccurred())
	g.Expect(validation.Remediation(informer.DoneWith)).To(Equal("Ensure the Kubernetes API server endpoint provided is correct."))
}

func TestCheckConnectionFailureWithAccess(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := test.ContextWithTimeout(t, 10*time.Millisecond)
	informer := test.NewFakeInformer()

	config := &api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				APIServerEndpoint: "https://localhost:1234",
			},
		},
	}

	validator := kubernetes.NewConnectionValidator()
	err := validator.Run(ctx, informer, config)
	g.Expect(err).NotTo(Succeed())
	g.Expect(informer.Started).To(BeTrue())
	g.Expect(informer.DoneWith).To(MatchError(ContainSubstring("connect: connection refused")))
	g.Expect(validation.Remediation(informer.DoneWith)).To(Equal("Ensure your network configuration allows the node to access the Kubernetes API endpoint."))
}
