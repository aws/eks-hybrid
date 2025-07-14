package ssm

import (
	"bytes"
	"context"
	stdErrors "errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/tracker"
	"github.com/aws/eks-hybrid/internal/util"
	"github.com/aws/eks-hybrid/internal/util/cmd"
)

const (
	defaultInstallerPath = "/opt/ssm/ssm-setup-cli"
	configRoot           = "/etc/amazon"
	artifactName         = "ssm"

	rootDir            = "/root"
	gpgConfigDirName   = ".gnupg"
	gpgConfigFileName  = "gpg.conf"
	gpgConfigFilePerms = 0o755
)

// Source serves an SSM installer binary for the target platform.
type Source interface {
	GetSSMInstaller(ctx context.Context) (io.ReadCloser, error)
	GetSSMInstallerSignature(ctx context.Context) (io.ReadCloser, error)
	PublicKey() string
}

// PkgSource serves and defines the package for target platform
type PkgSource interface {
	GetSSMPackage() artifact.Package
}

type InstallOptions struct {
	Tracker     *tracker.Tracker
	Source      Source
	Logger      *zap.Logger
	Region      string
	InstallRoot string
}

func Install(ctx context.Context, opts InstallOptions) error {
	if err := installFromSource(ctx, opts); err != nil {
		return err
	}

	return opts.Tracker.Add(artifact.Ssm)
}

func installFromSource(ctx context.Context, opts InstallOptions) error {
	installerPath := filepath.Join(opts.InstallRoot, defaultInstallerPath)

	if err := writeGpgConfig(); err != nil {
		return errors.Wrapf(err, "writing gpg config file")
	}

	if err := downloadFileWithRetries(ctx, opts.Source, opts.Logger, installerPath); err != nil {
		return errors.Wrap(err, "failed to install ssm installer")
	}

	if err := runInstallWithRetries(ctx, installerPath, opts.Region); err != nil {
		return errors.Wrapf(err, "failed to install ssm agent")
	}
	return nil
}

func Upgrade(ctx context.Context, opts InstallOptions) error {
	if err := installFromSource(ctx, opts); err != nil {
		return err
	}
	opts.Logger.Info("Upgraded", zap.String("artifact", artifactName))
	return nil
}

func downloadFileWithRetries(ctx context.Context, source Source, logger *zap.Logger, installerPath string) error {
	// Retry up to 3 times to download and validate the signature of
	// the SSM setup cli.
	var err error
	for range 3 {
		err = downloadFileTo(ctx, source, installerPath)
		if err == nil {
			break
		}
		logger.Error("Downloading ssm-setup-cli failed. Retrying...", zap.Error(err))
	}
	return err
}

// Update other functions that use InstallerPath to use the parameter instead
func downloadFileTo(ctx context.Context, source Source, installerPath string) error {
	installer, err := source.GetSSMInstaller(ctx)
	if err != nil {
		return fmt.Errorf("getting ssm-setup-cli: %w", err)
	}
	defer installer.Close()

	signature, err := source.GetSSMInstallerSignature(ctx)
	if err != nil {
		return fmt.Errorf("getting ssm-setup-cli signature: %w", err)
	}
	defer signature.Close()

	var installerBuffer bytes.Buffer
	installerTee := io.TeeReader(installer, &installerBuffer)

	if err := validateSetupSignature(installerTee, signature, source.PublicKey()); err != nil {
		return fmt.Errorf("validating ssm-setup-cli signature: %w", err)
	}

	if err := artifact.InstallFile(installerPath, bytes.NewReader(installerBuffer.Bytes()), 0o755); err != nil {
		return fmt.Errorf("installing ssm-setup-cli: %w", err)
	}

	return nil
}

func validateSetupSignature(installer, signature io.Reader, publicKey string) error {
	verificationKey, err := crypto.NewKeyFromArmored(publicKey)
	if err != nil {
		return err
	}

	pgp := crypto.PGP()
	verifier, _ := pgp.Verify().
		VerificationKey(verificationKey).
		New()

	verifyDataReader, err := verifier.VerifyingReader(installer, signature, crypto.Bytes)
	if err != nil {
		return err
	}
	verifyResult, err := verifyDataReader.ReadAllAndVerifySignature()
	if err != nil {
		return err
	}
	if err := verifyResult.SignatureError(); err != nil {
		return err
	}
	return nil
}

type UninstallOptions struct {
	Logger *zap.Logger
	// InstallRoot is optionally the root directory of the installation
	// If not provided, the default will be /
	InstallRoot     string
	SSMRegistration *SSMRegistration
	SSMClient       SSMClient
	PkgSource       PkgSource
}

// Uninstall de-registers the managed instance and removes all files and components that
// make up the ssm agent component.
func Uninstall(ctx context.Context, opts UninstallOptions) error {
	opts.Logger.Info("Uninstalling SSM agent...")

	actions := []func() error{
		func() error {
			return Deregister(ctx, opts.SSMRegistration, opts.SSMClient, opts.Logger)
		},
		func() error {
			return removeFileOrDir(opts.SSMRegistration.RegistrationFilePath(), "uninstalling ssm registration file", opts.Logger)
		},
		func() error {
			return uninstallPreRegisterComponents(ctx, opts.PkgSource, opts.Logger)
		},
		func() error {
			return removeFileOrDir(filepath.Join(opts.InstallRoot, configRoot), "uninstalling ssm config files", opts.Logger)
		},
		func() error {
			return removeFileOrDir(filepath.Join(opts.InstallRoot, symlinkedAWSConfigPath), "uninstalling ssm aws config symlink", opts.Logger)
		},
		func() error {
			return removeFileOrDir(filepath.Join(opts.InstallRoot, defaultAWSConfigPath), "uninstalling ssm aws config", opts.Logger)
		},
	}

	allErrors := []error{}
	for _, action := range actions {
		if err := action(); err != nil {
			allErrors = append(allErrors, err)
		}
	}

	if len(allErrors) > 0 {
		return stdErrors.Join(allErrors...)
	}

	return nil
}

func removeFileOrDir(path, errorMessage string, logger *zap.Logger) error {

	passwdFile := "/etc/passwd"
	if _, err := os.Stat(passwdFile); os.IsNotExist(err) {
		logger.Warn("Before /etc/passwd file does not exist", zap.String("path", passwdFile))
	} else if err != nil {
		logger.Error("Before Error checking /etc/passwd file status", zap.String("path", passwdFile), zap.Error(err))
	} else {
		logger.Info("Before /etc/passwd file is present", zap.String("path", passwdFile))
	}

	logger.Info("Removing file or directory", zap.String("path", path))
	if err := os.RemoveAll(path); err != nil {
		logger.Error("Failed to remove file or directory", zap.String("path", path), zap.Error(err))
		return errors.Wrap(err, errorMessage)
	}
	if _, err := os.Stat(passwdFile); os.IsNotExist(err) {
		logger.Warn("After /etc/passwd file does not exist", zap.String("path", passwdFile))
	} else if err != nil {
		logger.Error("After Error checking /etc/passwd file status", zap.String("path", passwdFile), zap.Error(err))
	} else {
		logger.Info("After /etc/passwd file is present", zap.String("path", passwdFile))
	}
	logger.Info("Successfully removed file or directory", zap.String("path", path))
	return nil
}

func writeGpgConfig() error {
	// In some environments, HOME will not be defined like while running cloud-init
	homeDir, set := os.LookupEnv("HOME")
	if !set {
		homeDir = rootDir
	}
	gpgConfigFile := filepath.Join(homeDir, gpgConfigDirName, gpgConfigFileName)
	return util.WriteFileUniqueLine(gpgConfigFile, []byte("no-tty"), gpgConfigFilePerms)
}

func uninstallPreRegisterComponents(ctx context.Context, pkgSource PkgSource, logger *zap.Logger) error {
	logger.Info("Uninstalling SSM pre-register components")

	ssmPkg := pkgSource.GetSSMPackage()
	logger.Info("Executing SSM package uninstall command")
	if err := cmd.Retry(ctx, ssmPkg.UninstallCmd, 5*time.Second); err != nil {
		logger.Error("Failed to uninstall SSM package", zap.Error(err))
		return errors.Wrapf(err, "uninstalling ssm")
	}
	logger.Info("Successfully uninstalled SSM package")

	passwdFile := "/etc/passwd"
	if _, err := os.Stat(passwdFile); os.IsNotExist(err) {
		logger.Warn("Before /etc/passwd file does not exist", zap.String("path", passwdFile))
	} else if err != nil {
		logger.Error("Before Error checking /etc/passwd file status", zap.String("path", passwdFile), zap.Error(err))
	} else {
		logger.Info("Before /etc/passwd file is present", zap.String("path", passwdFile))
	}

	logger.Info("Removing SSM installer", zap.String("path", defaultInstallerPath))
	if err := os.RemoveAll(defaultInstallerPath); err != nil {
		logger.Error("Failed to remove SSM installer", zap.String("path", defaultInstallerPath), zap.Error(err))
		return err
	}
	if _, err := os.Stat(passwdFile); os.IsNotExist(err) {
		logger.Warn("After /etc/passwd file does not exist", zap.String("path", passwdFile))
	} else if err != nil {
		logger.Error("After Error checking /etc/passwd file status", zap.String("path", passwdFile), zap.Error(err))
	} else {
		logger.Info("After /etc/passwd file is present", zap.String("path", passwdFile))
	}
	logger.Info("Successfully removed SSM installer", zap.String("path", defaultInstallerPath))

	logger.Info("SSM pre-register components uninstall completed successfully")
	return nil
}

func runInstallWithRetries(ctx context.Context, installerPath, region string) error {
	// Sometimes install fails due to conflicts with other processes
	// updating packages, specially when automating at machine startup.
	// We assume errors are transient and just retry for a bit.
	installCmdBuilder := func(ctx context.Context) *exec.Cmd {
		return exec.CommandContext(ctx, installerPath, "-install", "-region", region, "-version", "latest")
	}
	return cmd.Retry(ctx, installCmdBuilder, 5*time.Second)
}
