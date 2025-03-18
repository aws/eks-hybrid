package test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/tracker"
)

// TestData represents the data needed to run an installation test
type TestData struct {
	ArtifactName    string
	BinaryName      string
	Data            []byte
	Install         func(context.Context, string, aws.Source, *tracker.Tracker) error
	Verify          func(*GomegaWithT, string, *tracker.Tracker)
	VerifyFilePaths []string
}

func checksum(data []byte) []byte {
	h := sha256.New()
	h.Write(data)
	return h.Sum(nil)
}

// RunInstallTest runs the standard installation test suite
func RunInstallTest(t *testing.T, td TestData) {
	checksumData := fmt.Sprintf("%x %s", checksum(td.Data), td.BinaryName)
	badChecksumData := fmt.Sprintf("%x %s", checksum(append(td.Data, []byte("bad")...)), td.BinaryName)

	tests := []struct {
		name          string
		serverData    []byte
		checksum      string
		statusCode    int
		wantErr       string
		flakyChecksum bool
		flakyHTTP     bool
	}{
		{
			name:       "successful installation",
			serverData: td.Data,
			checksum:   checksumData,
			statusCode: http.StatusOK,
			wantErr:    "",
		},
		{
			name:       "bad checksum",
			serverData: td.Data,
			checksum:   badChecksumData,
			statusCode: http.StatusOK,
			wantErr:    "checksum mismatch",
		},
		{
			name:          "intermittent bad checksum",
			serverData:    td.Data,
			checksum:      checksumData,
			statusCode:    http.StatusOK,
			wantErr:       "",
			flakyChecksum: true,
		},
		{
			name:       "intermittent bad http status code",
			serverData: td.Data,
			checksum:   checksumData,
			statusCode: http.StatusOK,
			wantErr:    "",
			flakyHTTP:  true,
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			wantErr:    "unexpected status code: 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			server := NewHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
				if tt.statusCode != http.StatusOK {
					w.WriteHeader(tt.statusCode)
					return
				}

				artifactPath := fmt.Sprintf("/latest/linux_amd64/%s", td.BinaryName)
				if r.URL.Path == artifactPath {
					if tt.flakyHTTP {
						tt.flakyHTTP = false
						w.WriteHeader(http.StatusInternalServerError)
						return
					}

					if _, err := w.Write(tt.serverData); err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
				}

				checksumPath := fmt.Sprintf("/latest/linux_amd64/%s.sha256", td.BinaryName)
				if r.URL.Path == checksumPath {
					checksumData := tt.checksum
					if tt.flakyChecksum {
						checksumData = badChecksumData
						tt.flakyChecksum = false
					}
					if _, err := w.Write([]byte(checksumData)); err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
				}
			})

			var source aws.Source
			if td.ArtifactName == "aws_signing_helper" {
				source = aws.Source{
					Iam: aws.IamRolesAnywhereRelease{
						Artifacts: []aws.Artifact{
							{
								Arch:        runtime.GOARCH,
								OS:          runtime.GOOS,
								Name:        td.ArtifactName,
								URI:         server.URL + "/latest/linux_amd64/" + td.BinaryName,
								ChecksumURI: server.URL + "/latest/linux_amd64/" + td.BinaryName + ".sha256",
							},
						},
					},
				}
			} else {
				source = aws.Source{
					Eks: aws.EksPatchRelease{
						Artifacts: []aws.Artifact{
							{
								Arch:        runtime.GOARCH,
								OS:          runtime.GOOS,
								Name:        td.ArtifactName,
								URI:         server.URL + "/latest/linux_amd64/" + td.BinaryName,
								ChecksumURI: server.URL + "/latest/linux_amd64/" + td.BinaryName + ".sha256",
							},
						},
					},
				}
			}

			tr := &tracker.Tracker{Artifacts: &tracker.InstalledArtifacts{}}

			f, err := os.MkdirTemp("", td.ArtifactName+"-test")
			g.Expect(err).NotTo(HaveOccurred())
			defer os.RemoveAll(f)

			err = td.Install(context.Background(), f, source, tr)

			if tt.wantErr != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
				return
			}

			g.Expect(err).NotTo(HaveOccurred())
			td.Verify(g, f, tr)

			for _, filePath := range td.VerifyFilePaths {
				filePath := filepath.Join(f, filePath)
				_, err := os.Stat(filePath)
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}
