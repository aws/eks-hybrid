package iamrolesanywhere

import (
	"context"
	"os"
	"path"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/tracker"
)

const (
	// SigingHelperBinPath is the path that the signing helper is installed to.
	SigningHelperBinPath = "/usr/local/bin/aws_signing_helper"

	artifactName      = "aws-signing-helper"
	artifactFilePerms = 0o755
)

// SigningHelperSource retrieves the aws_signing_helper binary.
type SigningHelperSource interface {
	GetSigningHelper(context.Context) (artifact.Source, error)
}

func Install(ctx context.Context, tracker *tracker.Tracker, signingHelperSrc SigningHelperSource) error {
	signingHelper, err := signingHelperSrc.GetSigningHelper(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get source for aws_signing_helper")
	}
	defer signingHelper.Close()

	if err := artifact.InstallFile(SigningHelperBinPath, signingHelper, artifactFilePerms); err != nil {
		return errors.Wrap(err, "failed to install aws_signer_helper")
	}
	if err = tracker.Add(artifact.IamRolesAnywhere); err != nil {
		return err
	}

	if !signingHelper.VerifyChecksum() {
		return errors.Errorf("aws_signer_helper checksum mismatch: %v", artifact.NewChecksumError(signingHelper))
	}

	return nil
}

func Uninstall() error {
	if err := os.RemoveAll(SigningHelperServiceFilePath); err != nil {
		return err
	}
	if err := os.RemoveAll(path.Dir(EksHybridAwsCredentialsPath)); err != nil {
		return err
	}
	return os.RemoveAll(SigningHelperBinPath)
}

func Upgrade(ctx context.Context, signingHelperSrc SigningHelperSource, log *zap.Logger) error {
	signingHelper, err := signingHelperSrc.GetSigningHelper(ctx)
	if err != nil {
		return errors.Wrap(err, "getting aws_signing_helper source")
	}
	defer signingHelper.Close()

	return artifact.Upgrade(artifactName, SigningHelperBinPath, signingHelper, artifactFilePerms, log)
}
