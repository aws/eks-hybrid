package ssm_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProtonMail/gopenpgp/v3/crypto"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/ssm"
	"github.com/aws/eks-hybrid/internal/test"
	"github.com/aws/eks-hybrid/internal/tracker"
)

func generateKeyPair(t *testing.T) (string, *crypto.Key) {
	g := NewGomegaWithT(t)

	pgp := crypto.PGP()
	key, err := pgp.KeyGeneration().
		AddUserId("test", "test@example.com").
		New().GenerateKey()
	g.Expect(err).NotTo(HaveOccurred())

	armoredPublicKey, err := key.GetArmoredPublicKey()
	g.Expect(err).NotTo(HaveOccurred())

	return armoredPublicKey, key
}

func generateSignature(t *testing.T, key *crypto.Key, data []byte) []byte {
	g := NewGomegaWithT(t)

	pgp := crypto.PGP()
	signer, err := pgp.Sign().SigningKey(key).Detached().New()
	g.Expect(err).NotTo(HaveOccurred())

	signature, err := signer.Sign(data, crypto.Bytes)
	g.Expect(err).NotTo(HaveOccurred())
	return signature
}

func TestInstall(t *testing.T) {
	publicKey, privateKey := generateKeyPair(t)
	_, wrongPrivateKey := generateKeyPair(t)

	installerData := []byte("#!/bin/echo\n")

	tests := []struct {
		name          string
		installerData []byte
		signature     []byte
		statusCode    int
		wantErr       string
	}{
		{
			name:          "successful installation",
			installerData: installerData,
			signature:     generateSignature(t, privateKey, installerData),
			statusCode:    http.StatusOK,
			wantErr:       "",
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			wantErr:    "unexpected status code: 500",
		},
		{
			name:          "invalid signature",
			installerData: installerData,
			signature:     generateSignature(t, privateKey, append(installerData, []byte("extra data to make signature invalid")...)),
			statusCode:    http.StatusOK,
			wantErr:       "failed to install ssm installer: validating ssm-setup-cli signature: Signature Verification Error: Invalid signature caused by openpgp: invalid signature: EdDSA verification failure",
		},
		{
			name:          "wrong key signature",
			installerData: installerData,
			signature:     generateSignature(t, wrongPrivateKey, installerData),
			statusCode:    http.StatusOK,
			wantErr:       "failed to install ssm installer: validating ssm-setup-cli signature: Signature Verification Error: No matching signature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			tmpDir := t.TempDir()
			installerPath := filepath.Join(tmpDir, "ssm-setup-cli")

			server := test.NewHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
				if tt.statusCode != http.StatusOK {
					w.WriteHeader(tt.statusCode)
					return
				}

				if r.URL.Path == "/latest/linux_amd64/ssm-setup-cli" {
					if _, err := w.Write(tt.installerData); err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
				} else if r.URL.Path == "/latest/linux_amd64/ssm-setup-cli.sig" {
					if _, err := w.Write(tt.signature); err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
				}
			})

			source := ssm.NewSSMInstaller(zap.NewNop(), "test-region",
				ssm.WithURLBuilder(func() (string, error) {
					return server.URL + "/latest/linux_amd64/ssm-setup-cli", nil
				}),
				ssm.WithPublicKey(publicKey),
			)

			tr := &tracker.Tracker{Artifacts: &tracker.InstalledArtifacts{}}
			logger := zap.NewNop()

			err := ssm.Install(context.Background(), ssm.InstallOptions{
				Tracker:       tr,
				Source:        source,
				Logger:        logger,
				InstallerPath: installerPath,
			})

			if tt.wantErr != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
				return
			}

			g.Expect(err).NotTo(HaveOccurred())

			installedData, err := os.ReadFile(installerPath)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(installedData).To(Equal(tt.installerData))

			g.Expect(tr.Artifacts.Ssm).To(BeTrue())
		})
	}
}
