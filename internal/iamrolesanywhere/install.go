package iamrolesanywhere

import (
	"context"
	"path"
	"path/filepath"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/system"
	"github.com/aws/eks-hybrid/internal/tracker"
)

const (
	// SigningHelperBinPath is the path that the signing helper is installed to.
	SigningHelperBinPath = "/usr/local/bin/aws_signing_helper"

	artifactName      = "aws-signing-helper"
	artifactFilePerms = 0o755
)

// SigningHelperSource retrieves the aws_signing_helper binary.
type SigningHelperSource interface {
	GetSigningHelper(context.Context) (artifact.Source, error)
}

type InstallOptions struct {
	InstallRoot string
	Tracker     *tracker.Tracker
	Source      SigningHelperSource
	Logger      *zap.Logger
}

func Install(ctx context.Context, opts InstallOptions) error {
	if err := installFromSource(ctx, opts); err != nil {
		return errors.Wrap(err, "installing aws_signing_helper")
	}

	if err := opts.Tracker.Add(artifact.IamRolesAnywhere); err != nil {
		return errors.Wrap(err, "adding aws_signing_helper to tracker")
	}

	return nil
}

func installFromSource(ctx context.Context, opts InstallOptions) error {
	if err := downloadFileWithRetries(ctx, opts); err != nil {
		return errors.Wrap(err, "downloading aws_signing_helper")
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
		opts.Logger.Error("Downloading aws_signing_helper failed. Retrying...", zap.Error(err))
	}
	return err
}

func downloadFileTo(ctx context.Context, opts InstallOptions) error {
	signingHelper, err := opts.Source.GetSigningHelper(ctx)
	if err != nil {
		return errors.Wrap(err, "getting source for aws_signing_helper")
	}
	defer signingHelper.Close()

	if err := artifact.InstallFile(filepath.Join(opts.InstallRoot, SigningHelperBinPath), signingHelper, artifactFilePerms); err != nil {
		return errors.Wrap(err, "installing aws_signing_helper")
	}

	if !signingHelper.VerifyChecksum() {
		return errors.Errorf("aws_signing_helper checksum mismatch: %v", artifact.NewChecksumError(signingHelper))
	}

	return nil
}

func Uninstall() error {
	if err := system.SafeRemoveAll(SigningHelperServiceFilePath, false, false); err != nil {
		return err
	}
	if err := system.SafeRemoveAll(path.Dir(EksHybridAwsCredentialsPath), false, false); err != nil {
		return err
	}
	return system.SafeRemoveAll(SigningHelperBinPath, false, false)
}

func Upgrade(ctx context.Context, signingHelperSrc SigningHelperSource, log *zap.Logger) error {
	signingHelper, err := signingHelperSrc.GetSigningHelper(ctx)
	if err != nil {
		return errors.Wrap(err, "getting aws_signing_helper source")
	}
	defer signingHelper.Close()

	return artifact.Upgrade(artifactName, SigningHelperBinPath, signingHelper, artifactFilePerms, log)
}
