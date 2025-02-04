package artifact

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// checksumMatch compares the checksum of the installed artifact with the expected checksum
// A mismatch of checksum indicates installed artifacts are due for an upgrade
func checksumMatch(installedArtifactPath string, src Source) (bool, error) {
	fh, err := os.Open(installedArtifactPath)
	if err != nil {
		return false, errors.Wrap(err, "checking for checksum match")
	}
	defer fh.Close()

	digest := sha256.New()
	if _, err = io.Copy(digest, fh); err != nil {
		return false, errors.Wrapf(err, "calculating sha256 for %s", installedArtifactPath)
	}
	return bytes.Equal(digest.Sum(nil), src.ExpectedChecksum()), nil
}

// Upgrade upgrades an artifact from the source only if the expected checksum doesn't match with the
// checksum of artifact already installed.
func Upgrade(artifactName, path string, source Source, perms fs.FileMode, log *zap.Logger) error {
	match, err := checksumMatch(path, source)
	if err != nil {
		return err
	}

	if !match {
		if err := InstallFile(path, source, perms); err != nil {
			return errors.Wrapf(err, "installing %s", artifactName)
		}

		if !source.VerifyChecksum() {
			return errors.Errorf("%s checksum mismatch: %v", artifactName, NewChecksumError(source))
		}
		log.Info("Upgraded", zap.String("artifact", artifactName))
	} else {
		log.Info(fmt.Sprintf("No new version found for artifact %s. Skipping upgrade.", artifactName))
	}
	return nil
}
