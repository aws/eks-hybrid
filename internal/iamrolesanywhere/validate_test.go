package iamrolesanywhere_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/iamrolesanywhere"
	"github.com/aws/eks-hybrid/internal/test"
	"github.com/aws/eks-hybrid/internal/validation"
)

func TestCheckEndpointAccessSuccess(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()

	server := test.NewHTTPSServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	config := aws.Config{
		BaseEndpoint: &server.URL,
	}

	g.Expect(iamrolesanywhere.CheckEndpointAccess(ctx, config)).To(Succeed())
}

func TestCheckEndpointAccessFailure(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()

	config := aws.Config{
		BaseEndpoint: aws.String("https://localhost:1234"),
	}

	err := iamrolesanywhere.CheckEndpointAccess(ctx, config)
	g.Expect(err).NotTo(Succeed())
	g.Expect(err).To(MatchError(ContainSubstring("checking connection to IAM Roles Anywhere endpoint")))
}

func TestAccessValidatorRunSuccess(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()

	server := test.NewHTTPSServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	config := aws.Config{
		BaseEndpoint: &server.URL,
	}

	informer := test.NewFakeInformer()
	validator := iamrolesanywhere.NewAccessValidator(config)

	g.Expect(validator.Run(ctx, informer, &api.NodeConfig{})).To(Succeed())
	g.Expect(informer.Started).To(BeTrue())
	g.Expect(informer.DoneWith).NotTo(HaveOccurred())
}

func TestAccessValidatorRunFail(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()

	config := aws.Config{
		BaseEndpoint: aws.String("https://localhost:1234"),
	}

	informer := test.NewFakeInformer()
	validator := iamrolesanywhere.NewAccessValidator(config)

	err := validator.Run(ctx, informer, &api.NodeConfig{})
	g.Expect(err).To(MatchError(ContainSubstring("checking connection to IAM Roles Anywhere endpoint")))
	g.Expect(informer.Started).To(BeTrue())
	g.Expect(informer.DoneWith).To(MatchError(ContainSubstring("checking connection to IAM Roles Anywhere endpoint")))
	g.Expect(validation.Remediation(informer.DoneWith)).To(ContainSubstring("Ensure your network configuration allows access to the AWS IAM Roles Anywhere API endpoint"))
}
