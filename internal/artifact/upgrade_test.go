package artifact_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/artifact"
)

func TestUpgradeAvailable(t *testing.T) {
	dummyFilePath := "testdata/dummyfile"
	dummyFh, err := os.Open(dummyFilePath)
	if err != nil {
		t.Fatal(err)
	}
	fileChecksum := []byte("b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9  internal/artifact/testdata/dummyfile")
	wrongChecksum := []byte("b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7acabcdcde9 randomfile/path")

	testcases := []struct {
		name             string
		filePath         string
		sourceChecksum   []byte
		upgradeAvailable bool
		wantErr          error
	}{
		{
			name:             "Upgrade available",
			filePath:         dummyFilePath,
			sourceChecksum:   wrongChecksum,
			upgradeAvailable: true,
			wantErr:          nil,
		},
		{
			name:             "Upgrade not available",
			filePath:         dummyFilePath,
			sourceChecksum:   fileChecksum,
			upgradeAvailable: false,
			wantErr:          nil,
		},
		{
			name:             "File not installed",
			filePath:         "wrong/path",
			sourceChecksum:   wrongChecksum,
			upgradeAvailable: false,
			wantErr:          fmt.Errorf("checking for available upgrades: open wrong/path: no such file or directory"),
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			src, err := artifact.WithChecksum(dummyFh, sha256.New(), tc.sourceChecksum)
			if err != nil {
				g.Expect(err).To(BeNil())
			}
			available, err := artifact.UpgradeAvailable(tc.filePath, src)
			if err != nil {
				g.Expect(err.Error()).To(Equal(tc.wantErr.Error()))
			}
			g.Expect(available).To(Equal(tc.upgradeAvailable))
		})
	}
}
