package ssm_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/ssm"
	"github.com/aws/eks-hybrid/internal/test"
)

func TestGetSSMInstaller(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse string
		statusCode     int
		wantErr        string
		urlBuilder     func() (string, error)
	}{
		{
			name:           "successful installer fetch",
			serverResponse: "test installer data",
			statusCode:     http.StatusOK,
			wantErr:        "",
		},
		{
			name:           "server error",
			serverResponse: "internal error",
			statusCode:     http.StatusInternalServerError,
			wantErr:        "unexpected status code: 500",
		},
		{
			name:       "url builder error",
			wantErr:    "failed to build URL",
			urlBuilder: func() (string, error) { return "", errors.New("failed to build URL") },
		},
		{
			name:           "not found error",
			serverResponse: "not found",
			statusCode:     http.StatusNotFound,
			wantErr:        "unexpected status code: 404",
		},
		{
			name:           "forbidden error",
			serverResponse: "access denied",
			statusCode:     http.StatusForbidden,
			wantErr:        "unexpected status code: 403",
		},
		{
			name:           "empty response with success",
			serverResponse: "",
			statusCode:     http.StatusOK,
			wantErr:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			server := test.NewHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				if _, err := w.Write([]byte(tt.serverResponse)); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
			})

			urlBuilder := tt.urlBuilder
			if urlBuilder == nil {
				urlBuilder = func() (string, error) {
					return server.URL + "/latest/linux_amd64/ssm-setup-cli", nil
				}
			}

			source := ssm.NewSSMInstaller(zap.NewNop(), "test-region",
				ssm.WithURLBuilder(urlBuilder),
			)
			reader, err := source.GetSSMInstaller(context.Background())

			if tt.wantErr != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
				return
			}

			g.Expect(err).NotTo(HaveOccurred())
			defer reader.Close()

			data, err := io.ReadAll(reader)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(data)).To(Equal(tt.serverResponse))
		})
	}
}

func TestGetSSMInstallerSignature(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse string
		statusCode     int
		wantErr        string
		urlBuilder     func() (string, error)
	}{
		{
			name:           "successful signature fetch",
			serverResponse: "test signature data",
			statusCode:     http.StatusOK,
			wantErr:        "",
		},
		{
			name:           "server error",
			serverResponse: "internal error",
			statusCode:     http.StatusInternalServerError,
			wantErr:        "unexpected status code: 500",
		},
		{
			name:       "url builder error",
			wantErr:    "failed to build URL",
			urlBuilder: func() (string, error) { return "", errors.New("failed to build URL") },
		},
		{
			name:           "not found error",
			serverResponse: "not found",
			statusCode:     http.StatusNotFound,
			wantErr:        "unexpected status code: 404",
		},
		{
			name:           "forbidden error",
			serverResponse: "access denied",
			statusCode:     http.StatusForbidden,
			wantErr:        "unexpected status code: 403",
		},
		{
			name:           "empty signature with success",
			serverResponse: "",
			statusCode:     http.StatusOK,
			wantErr:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			server := test.NewHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				if _, err := w.Write([]byte(tt.serverResponse)); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
			})

			urlBuilder := tt.urlBuilder
			if urlBuilder == nil {
				urlBuilder = func() (string, error) {
					return server.URL + "/latest/linux_amd64/ssm-setup-cli", nil
				}
			}

			source := ssm.NewSSMInstaller(zap.NewNop(), "test-region",
				ssm.WithURLBuilder(urlBuilder),
			)
			reader, err := source.GetSSMInstallerSignature(context.Background())

			if tt.wantErr != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
				return
			}

			g.Expect(err).NotTo(HaveOccurred())
			defer reader.Close()

			data, err := io.ReadAll(reader)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(data)).To(Equal(tt.serverResponse))
		})
	}
}
