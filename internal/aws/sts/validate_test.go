package sts_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/aws/sts"
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

	g.Expect(sts.CheckEndpointAccess(ctx, config)).To(Succeed())
}

func TestCheckEndpointAccessFailure(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()

	config := aws.Config{
		BaseEndpoint: aws.String("https://localhost:1234"),
	}

	err := sts.CheckEndpointAccess(ctx, config)
	g.Expect(err).NotTo(Succeed())
	g.Expect(err).To(MatchError(ContainSubstring("checking connection to sts endpoint")))
}

func TestAccessValidatorRunSuccess(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()

	server := test.NewHTTPSServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	config := aws.Config{
		BaseEndpoint: &server.URL,
		HTTPClient:   server.Client(),
	}

	informer := test.NewFakeInformer()
	validator := sts.NewAuthenticationValidator(config)

	g.Expect(validator.Run(ctx, informer, &api.NodeConfig{})).To(Succeed())
	g.Expect(informer.Started).To(BeTrue())
	g.Expect(informer.DoneWith).NotTo(HaveOccurred())
}

func TestAccessValidatorRunFailCheckingEndpoint(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()

	config := aws.Config{
		BaseEndpoint: aws.String("https://localhost:1234"),
	}

	informer := test.NewFakeInformer()
	validator := sts.NewAuthenticationValidator(config)

	err := validator.Run(ctx, informer, &api.NodeConfig{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(informer.Started).To(BeFalse())
	g.Expect(informer.DoneWith).To(BeNil())
}

func TestAccessValidatorRunErrorFromGetCaller(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()

	server := test.NewHTTPSServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	config := aws.Config{
		BaseEndpoint: &server.URL,
		HTTPClient:   server.Client(),
	}

	informer := test.NewFakeInformer()
	validator := sts.NewAuthenticationValidator(config)

	err := validator.Run(ctx, informer, &api.NodeConfig{})
	g.Expect(err).To(MatchError(ContainSubstring("operation error STS: GetCallerIdentity, https response error StatusCode: 403")))
	g.Expect(informer.Started).To(BeTrue())
	g.Expect(informer.DoneWith).To(MatchError(ContainSubstring("operation error STS: GetCallerIdentity, https response error StatusCode: 403")))
	g.Expect(validation.Remediation(informer.DoneWith)).To(Equal("Check your AWS configuration and make sure you can obtain valid AWS credentials."))
}
