package kubernetes

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/pkg/errors"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/retrier"
	"github.com/aws/eks-hybrid/internal/validation"
)

func MakeUnauthenticatedRequest(ctx context.Context, endpoint string, caCertificate []byte) error {
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCertificate) {
		return validation.WithRemediation(errors.New("failed to parse Cluster CA certificate"),
			"Ensure the Cluster CA certificate provided is correct.",
		)
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return validation.WithRemediation(err, "Ensure the Kubernetes API server endpoint provided is correct.")
	}

	var resp *http.Response
	var body []byte
	err = retrier.PollWithRetries(ctx, func(ctx context.Context) (bool, error) {
		var err error
		resp, err = client.Do(req)
		if err != nil {
			return false, err
		}

		defer resp.Body.Close()

		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return false, fmt.Errorf("reading unauthenticated request response body: %w", err)
		}
		return true, nil
	})
	if err != nil {
		return validation.WithRemediation(err, "Ensure the provided Kubernetes API server endpoint is correct and the CA certificate is valid for that endpoint.")
	}

	apiServerResp := &apiServerResponse{}
	if err = json.Unmarshal(body, apiServerResp); err != nil {
		return fmt.Errorf("unmarshalling unauthenticated request response: %w", err)
	}

	// We allow both Forbidden and Unauthorized status codes because the API server will return
	// The kube-API server used to return Forbidden but in k8s 1.32 it started returning Unauthorized.
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusUnauthorized {
		return validation.WithRemediation(fmt.Errorf("expected status code from unauthenticated request %d or %d, got %d. Message: %s", http.StatusForbidden, http.StatusUnauthorized, resp.StatusCode, apiServerResp.Message),
			"Ensure the Kubernetes API server endpoint provided is correct and the CA certificate is valid for that endpoint.",
		)
	}

	return nil
}

type apiServerResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
	Code    int64  `json:"code"`
}

func CheckUnauthenticatedAccess(ctx context.Context, informer validation.Informer, node *api.NodeConfig) error {
	name := "kubernetes-unauthenticated-request"
	var err error
	informer.Starting(ctx, name, "Validating unauthenticated request to Kubernetes API endpoint")
	defer func() {
		informer.Done(ctx, name, err)
	}()

	if err = MakeUnauthenticatedRequest(ctx, node.Spec.Cluster.APIServerEndpoint, node.Spec.Cluster.CertificateAuthority); err != nil {
		return err
	}

	return nil
}
