package ssm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/aws/aws-sdk-go-v2/config"
	awsSsm "github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/tracker"
	"github.com/aws/eks-hybrid/internal/util/cmd"
)

const (
	defaultInstallerPath = "/opt/ssm/ssm-setup-cli"
	configRoot           = "/etc/amazon"
	artifactName         = "ssm"
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
	Tracker       *tracker.Tracker
	Source        Source
	Logger        *zap.Logger
	Region        string
	InstallerPath string
}

func Install(ctx context.Context, opts InstallOptions) error {
	if err := installFromSource(ctx, opts); err != nil {
		return err
	}

	return opts.Tracker.Add(artifact.Ssm)
}

func installFromSource(ctx context.Context, opts InstallOptions) error {
	if opts.InstallerPath == "" {
		opts.InstallerPath = defaultInstallerPath
	}

	if err := downloadFileWithRetries(ctx, opts.Source, opts.Logger, opts.InstallerPath); err != nil {
		return errors.Wrap(err, "failed to install ssm installer")
	}

	if err := runInstallWithRetries(ctx, opts.InstallerPath, opts.Region); err != nil {
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

// DeregisterAndUninstall de-registers the managed instance and removes all files and components that
// make up the ssm agent component.
func DeregisterAndUninstall(ctx context.Context, logger *zap.Logger, pkgSource PkgSource) error {
	logger.Info("Uninstalling and de-registering SSM agent...")
	instanceId, region, err := GetManagedHybridInstanceIdAndRegion()

	// If uninstall is being run just after running install and before running init
	// SSM would not be fully installed and registered, hence it's not required to run
	// deregister instance.
	if err != nil && os.IsNotExist(err) {
		return uninstallPreRegisterComponents(ctx, pkgSource)
	} else if err != nil {
		return err
	}

	// Create SSM client
	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return err
	}
	ssmClient := awsSsm.NewFromConfig(awsConfig)
	managed, err := isInstanceManaged(ssmClient, instanceId)
	if err != nil {
		return errors.Wrapf(err, "failed to get managed instance information")
	}

	// Only deregister the instance if init/ssm init was run and
	// if instances is actively listed as managed
	if managed {
		if err := deregister(ssmClient, instanceId); err != nil {
			return errors.Wrapf(err, "failed to deregister ssm managed instance")
		}
	}

	if err := uninstallPreRegisterComponents(ctx, pkgSource); err != nil {
		return err
	}

	if err := os.RemoveAll(path.Dir(registrationFilePath)); err != nil {
		return errors.Wrapf(err, "failed to uninstall ssm config files")
	}

	if err := os.RemoveAll(configRoot); err != nil {
		return errors.Wrapf(err, "failed to uninstall ssm config files")
	}

	return os.RemoveAll(symlinkedAWSConfigPath)
}

// Uninstall uninstall the ssm agent package and removes the setup-cli binary.
// It does not de-register the managed instance and it leaves the registration and
// credentials file.
func Uninstall(ctx context.Context, logger *zap.Logger, pkgSource PkgSource) error {
	logger.Info("Uninstalling SSM agent...")
	return uninstallPreRegisterComponents(ctx, pkgSource)
}

func uninstallPreRegisterComponents(ctx context.Context, pkgSource PkgSource) error {
	ssmPkg := pkgSource.GetSSMPackage()
	if err := cmd.Retry(ctx, ssmPkg.UninstallCmd, 5*time.Second); err != nil {
		return errors.Wrapf(err, "uninstalling ssm")
	}
	return os.RemoveAll(defaultInstallerPath)
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
