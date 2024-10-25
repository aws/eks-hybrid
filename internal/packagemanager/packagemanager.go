package packagemanager

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/containerd"
	"github.com/aws/eks-hybrid/internal/system"
	"github.com/aws/eks-hybrid/internal/util"
)

const (
	aptPackageManager  = "apt"
	snapPackageManager = "snap"
	yumPackageManager  = "yum"

	snapRemoveVerb = "remove"

	yumUtilsManager             = "yum-config-manager"
	yumUtilsManagerPkg          = "yum-utils"
	centOsDockerRepo            = "https://download.docker.com/linux/centos/docker-ce.repo"
	ubuntuDockerRepo            = "https://download.docker.com/linux/ubuntu"
	ubuntuDockerGpgKey          = "https://download.docker.com/linux/ubuntu/gpg"
	ubuntuDockerGpgKeyPath      = "/etc/apt/keyrings/docker.asc"
	ubuntuDockerGpgKeyFilePerms = 0755
	aptDockerRepoSourceFilePath = "/etc/apt/sources.list.d/docker.list"

	containerdDistroPkgName = "containerd"
	containerdDockerPkgName = "containerd.io"
	runcPkgName             = "runc"
)

var aptDockerRepoConfig = fmt.Sprintf("deb [arch=%s signed-by=%s] %s %s stable\n", runtime.GOARCH, ubuntuDockerGpgKeyPath,
	ubuntuDockerRepo, system.GetVersionCodeName())

// DistroPackageManger defines a new package manager using apt or yum
type DistroPackageManger struct {
	manager     string
	installVerb string
	updateVerb  string
	deleteVerb  string
	dockerRepo  string
	logger      *zap.Logger
}

func New(containerdSource containerd.SourceName, logger *zap.Logger) (*DistroPackageManger, error) {
	manager, err := getOsPackageManager()
	if err != nil {
		return nil, err
	}

	pm := &DistroPackageManger{
		manager:     manager,
		logger:      logger,
		installVerb: packageManagerInstallCmd[manager],
		updateVerb:  packageManagerUpdateCmd[manager],
		deleteVerb:  packageManagerDeleteCmd[manager],
	}
	if containerdSource == containerd.ContainerdSourceDocker {
		pm.dockerRepo = managerToDockerRepoMap[manager]
	}
	return pm, nil
}

// Configure configures the package manager.
func (pm *DistroPackageManger) Configure(ctx context.Context) error {
	// Add docker repos to the package manager
	if pm.dockerRepo != "" {
		if pm.manager == yumPackageManager {
			return pm.configureYumPackageManagerWithDockerRepo(ctx)
		}
		if pm.manager == aptPackageManager {
			return pm.configureAptPackageManagerWithDockerRepo(ctx)
		}
	}
	return nil
}

// configureYumPackageManagerWithDockerRepo configures yum package manager with docker repos
func (pm *DistroPackageManger) configureYumPackageManagerWithDockerRepo(ctx context.Context) error {
	// Run update to update all package repo metadata for newly provisioned OS
	pm.logger.Info("Updating packages to refresh repo metadata...")
	if resp, err := pm.updateAllPackages(ctx); err != nil {
		return errors.Wrapf(err, "failed to run update using package manager: %s", resp)
	}

	// Check and remove runc if installed, as it conflicts with docker repo
	if _, errNotFound := exec.LookPath(runcPkgName); errNotFound == nil {
		pm.logger.Info("Removing runc to avoid package conflicts from docker repos...")
		if resp, err := pm.removePackage(ctx, runcPkgName); err != nil {
			return errors.Wrapf(err, "failed to remove runc using package manager: %s", resp)
		}
	}

	if resp, err := pm.installPackage(ctx, yumUtilsManagerPkg); err != nil {
		return errors.Wrapf(err, "failed to install %s using package manager: %s", yumUtilsManagerPkg, resp)
	}

	// Get yumUtilsManager full path
	yumUtilsManagerPath, err := exec.LookPath(yumUtilsManager)
	if err != nil {
		return errors.Wrapf(err, "failed to locate yum utils manager in $PATH")
	}
	pm.logger.Info("Adding docker repo to package manager...")
	configureCmd := exec.Command(yumUtilsManagerPath, "--add-repo", centOsDockerRepo)
	out, err := configureCmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "failed adding docker repo to package manager: %s", out)
	}

	pm.logger.Info("Updating packages to refresh docker repo metadata...")
	if resp, err := pm.updateAllPackages(ctx); err != nil {
		return errors.Wrapf(err, "failed to run update using package manager: %s", resp)
	}
	return nil
}

// configureAptPackageManagerWithDockerRepo configures apt package manager with docker repos
func (pm *DistroPackageManger) configureAptPackageManagerWithDockerRepo(ctx context.Context) error {
	out, err := pm.updateAllPackages(ctx)
	if err != nil {
		return errors.Wrapf(err, "failed running commands to configure package manager: %s", out)
	}
	out, err = pm.installPackage(ctx, "ca-certificates")
	if err != nil {
		return errors.Wrapf(err, "failed running commands to configure package manager: %s", out)
	}

	// Download docker gpg key and write it to file
	resp, err := http.Get(ubuntuDockerGpgKey)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if err := util.WriteFileWithDirFromReader(ubuntuDockerGpgKeyPath, resp.Body, ubuntuDockerGpgKeyFilePerms); err != nil {
		return err
	}

	// Add docker repo config for ubuntu-apt to apt sources
	if err := util.WriteFileWithDir(aptDockerRepoSourceFilePath, []byte(aptDockerRepoConfig), ubuntuDockerGpgKeyFilePerms); err != nil {
		return err
	}

	// Run update to pull docker repo's metadata
	// Commands cant be re-used to run again, hence re-declaring update command
	out, err = pm.updateAllPackages(ctx)
	if err != nil {
		return errors.Wrapf(err, "failed running commands to configure package manager: %s", out)
	}
	return nil
}

// installPackage installs a package using package manager
func (pm *DistroPackageManger) installPackage(ctx context.Context, packageName string) (string, error) {
	installCmd := exec.CommandContext(ctx, pm.manager, pm.installVerb, packageName, "-y")
	out, err := installCmd.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}

// updateAllPackages updates all packages and repo metadata on the system
func (pm *DistroPackageManger) updateAllPackages(ctx context.Context) (string, error) {
	updateCmd := exec.CommandContext(ctx, pm.manager, pm.updateVerb, "-y")
	out, err := updateCmd.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}

// removePackage deletes a package using package manager
func (pm *DistroPackageManger) removePackage(ctx context.Context, packageName string) (string, error) {
	removeCmd := exec.CommandContext(ctx, pm.manager, pm.deleteVerb, packageName, "-y")
	out, err := removeCmd.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}

// GetContainerd gets the Package
// Satisfies the containerd source interface
func (pm *DistroPackageManger) GetContainerd(ctx context.Context) artifact.Package {
	packageName := containerdDistroPkgName
	if pm.dockerRepo != "" {
		packageName = containerdDockerPkgName
	}
	return artifact.NewPackageSource(
		exec.CommandContext(ctx, pm.manager, pm.installVerb, packageName, "-y"),
		exec.CommandContext(ctx, pm.manager, pm.deleteVerb, packageName, "-y"),
	)
}

// GetIptables satisfies the getiptables source interface
func (pm *DistroPackageManger) GetIptables(ctx context.Context) artifact.Package {
	return artifact.NewPackageSource(
		exec.CommandContext(ctx, pm.manager, pm.installVerb, "iptables", "-y"),
		exec.CommandContext(ctx, pm.manager, pm.deleteVerb, "iptables", "-y"),
	)
}

// GetSSMPackage satisfies the getssmpackage source interface
func (pm *DistroPackageManger) GetSSMPackage(ctx context.Context) artifact.Package {
	// SSM is installed using snap package manager. If apt package manager
	// is detected, use snap to install/uninstall SSM.
	if pm.manager == aptPackageManager {
		return artifact.NewPackageSource(
			exec.CommandContext(ctx, snapPackageManager, snapRemoveVerb, "amazon-ssm-agent"),
			exec.CommandContext(ctx, snapPackageManager, snapRemoveVerb, "amazon-ssm-agent"),
		)
	}
	return artifact.NewPackageSource(
		exec.CommandContext(ctx, pm.manager, pm.installVerb, "amazon-ssm-agent", "-y"),
		exec.CommandContext(ctx, pm.manager, pm.deleteVerb, "amazon-ssm-agent", "-y"),
	)
}

func getOsPackageManager() (string, error) {
	supportedManagers := []string{yumPackageManager, aptPackageManager}
	for _, manager := range supportedManagers {
		if _, err := exec.LookPath(manager); err == nil {
			return manager, nil
		}
	}
	return "", errors.New("unsupported package manager encountered. Please run nodeadm from a supported os")
}

var packageManagerInstallCmd = map[string]string{
	aptPackageManager: "install",
	yumPackageManager: "install",
}

var packageManagerUpdateCmd = map[string]string{
	aptPackageManager: "update",
	yumPackageManager: "update",
}
var packageManagerDeleteCmd = map[string]string{
	aptPackageManager: "autoremove",
	yumPackageManager: "remove",
}

var managerToDockerRepoMap = map[string]string{
	yumPackageManager: "https://download.docker.com/linux/centos/docker-ce.repo",
	aptPackageManager: "https://download.docker.com/linux/ubuntu",
}
