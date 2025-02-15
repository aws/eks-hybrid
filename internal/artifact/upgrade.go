package artifact

import (
	"bytes"
	"crypto/sha256"
	"io"
	"os"

	"github.com/pkg/errors"
)

// UpgradeAvailable compares the checksum of the installed artifact with the expected checksum
// A mismatch of checksum indicates installed artifacts are due for an upgrade
func UpgradeAvailable(installedArtifactPath string, src Source) (bool, error) {
	fh, err := os.Open(installedArtifactPath)
	if err != nil {
		return false, errors.Wrap(err, "checking for available upgrades")
	}
	defer fh.Close()

	digest := sha256.New()
	if _, err = io.Copy(digest, fh); err != nil {
		return false, errors.Wrap(err, "checking for available upgrades")
	}

	if bytes.Equal(digest.Sum(nil), src.ExpectedChecksum()) {
		return false, nil
	}
	return true, nil
}
