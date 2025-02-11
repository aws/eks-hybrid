package ssm

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	awsSsm "github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/tracker"
	"github.com/aws/eks-hybrid/internal/util/cmd"
)

const (
	installerPath = "/opt/ssm/ssm-setup-cli"
	configRoot    = "/etc/amazon"
)

// Source serves an SSM installer binary for the target platform.
type Source interface {
	GetSSMInstaller(ctx context.Context) (io.ReadCloser, error)
}

// PkgSource serves and defines the package for target platform
type PkgSource interface {
	GetSSMPackage() artifact.Package
}

func Install(ctx context.Context, tracker *tracker.Tracker, source Source, region string) error {
	installer, err := source.GetSSMInstaller(ctx)
	if err != nil {
		return err
	}
	defer installer.Close()

	if err := artifact.InstallFile(installerPath, installer, 0o755); err != nil {
		return errors.Wrap(err, "failed to install ssm installer")
	}

	if err = runInstallWithRetries(ctx, region); err != nil {
		return errors.Wrapf(err, "failed to install ssm agent")
	}

	return tracker.Add(artifact.Ssm)
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
	if err := artifact.UninstallPackageWithRetries(ctx, ssmPkg, 5*time.Second); err != nil {
		return errors.Wrapf(err, "failed to uninstall ssm")
	}
	return os.RemoveAll(installerPath)
}

func runInstallWithRetries(ctx context.Context, region string) error {
	// Sometimes install fails due to conflicts with other processes
	// updating packages, specially when automating at machine startup.
	// We assume errors are transient and just retry for a bit.
	installCmdBuilder := func(ctx context.Context) *exec.Cmd {
		return exec.CommandContext(ctx, installerPath, "-install", "-region", region)
	}

	return cmd.Retry(ctx, installCmdBuilder, 5*time.Second)
}
