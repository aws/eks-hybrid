package iamrolesanywhere

import (
	"context"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/artifact"
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

func Uninstall(logger *zap.Logger) error {
	logger.Info("Uninstalling IAM Roles Anywhere components...")

	logger.Info("Removing signing helper service file", zap.String("path", SigningHelperServiceFilePath))
	if err := os.RemoveAll(SigningHelperServiceFilePath); err != nil {
		logger.Error("Failed to remove signing helper service file", zap.String("path", SigningHelperServiceFilePath), zap.Error(err))
		return err
	}
	logger.Info("Successfully removed signing helper service file", zap.String("path", SigningHelperServiceFilePath))

	credentialsDir := path.Dir(EksHybridAwsCredentialsPath)
	logger.Info("Removing AWS credentials directory", zap.String("path", credentialsDir))
	if err := os.RemoveAll(credentialsDir); err != nil {
		logger.Error("Failed to remove AWS credentials directory", zap.String("path", credentialsDir), zap.Error(err))
		return err
	}
	logger.Info("Successfully removed AWS credentials directory", zap.String("path", credentialsDir))

	logger.Info("Removing signing helper binary", zap.String("path", SigningHelperBinPath))
	if err := os.RemoveAll(SigningHelperBinPath); err != nil {
		logger.Error("Failed to remove signing helper binary", zap.String("path", SigningHelperBinPath), zap.Error(err))
		return err
	}
	logger.Info("Successfully removed signing helper binary", zap.String("path", SigningHelperBinPath))

	logger.Info("IAM Roles Anywhere uninstall completed successfully")
	return nil
}

func Upgrade(ctx context.Context, signingHelperSrc SigningHelperSource, log *zap.Logger) error {
	signingHelper, err := signingHelperSrc.GetSigningHelper(ctx)
	if err != nil {
		return errors.Wrap(err, "getting aws_signing_helper source")
	}
	defer signingHelper.Close()

	return artifact.Upgrade(artifactName, SigningHelperBinPath, signingHelper, artifactFilePerms, log)
}
