package iamauthenticator

import (
	"context"
	"path/filepath"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/system"
	"github.com/aws/eks-hybrid/internal/tracker"
)

const (
	// IAMAuthenticatorBinPath is the path the IAM Authenticator is installed to.
	IAMAuthenticatorBinPath = "/usr/local/bin/aws-iam-authenticator"

	artifactName      = "aws-iam-authenticator"
	artifactFilePerms = 0o755
)

// IAMAuthenticatorSource retrieves the aws-iam-authenticator binary.
type IAMAuthenticatorSource interface {
	GetIAMAuthenticator(context.Context) (artifact.Source, error)
}

type InstallOptions struct {
	InstallRoot string
	Tracker     *tracker.Tracker
	Source      IAMAuthenticatorSource
	Logger      *zap.Logger
}

// Install installs the aws_signing_helper and aws-iam-authenticator on the system at
// SigningHelperBinPath and IAMAuthenticatorBinPath respectively.
func Install(ctx context.Context, opts InstallOptions) error {
	if err := installFromSource(ctx, opts); err != nil {
		return errors.Wrap(err, "installing aws-iam-authenticator")
	}

	if err := opts.Tracker.Add(artifact.IamAuthenticator); err != nil {
		return errors.Wrap(err, "adding aws-iam-authenticator to tracker")
	}

	return nil
}

func installFromSource(ctx context.Context, opts InstallOptions) error {
	if err := downloadFileWithRetries(ctx, opts); err != nil {
		return errors.Wrap(err, "downloading aws-iam-authenticator")
	}

	return nil
}

func downloadFileWithRetries(ctx context.Context, opts InstallOptions) error {
	// Retry up to 3 times to download and validate the checksum
	var err error
	for range 3 {
		err = downloadFileTo(ctx, opts)
		if err == nil {
			break
		}
		opts.Logger.Error("Downloading aws-iam-authenticator failed. Retrying...", zap.Error(err))
	}
	return err
}

func downloadFileTo(ctx context.Context, opts InstallOptions) error {
	authenticator, err := opts.Source.GetIAMAuthenticator(ctx)
	if err != nil {
		return errors.Wrap(err, "getting aws-iam-authenticator source")
	}
	defer authenticator.Close()

	if err := artifact.InstallFile(filepath.Join(opts.InstallRoot, IAMAuthenticatorBinPath), authenticator, artifactFilePerms); err != nil {
		return errors.Wrap(err, "installing aws-iam-authenticator")
	}

	if !authenticator.VerifyChecksum() {
		return errors.Errorf("aws-iam-authenticator checksum mismatch: %v", artifact.NewChecksumError(authenticator))
	}

	return nil
}

func Uninstall() error {
	return system.SafeRemoveAll(IAMAuthenticatorBinPath, false, false)
}

func Upgrade(ctx context.Context, src IAMAuthenticatorSource, log *zap.Logger) error {
	authenticator, err := src.GetIAMAuthenticator(ctx)
	if err != nil {
		return errors.Wrap(err, "getting aws-iam-authenticator source")
	}
	defer authenticator.Close()

	return artifact.Upgrade(artifactName, IAMAuthenticatorBinPath, authenticator, artifactFilePerms, log)
}
