package tracker

import (
	"fmt"
	"io/fs"
	"os"
	"path"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sigs.k8s.io/yaml"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/util"
)

type ContainerdSourceName string

const (
	ContainerdSourceNone   ContainerdSourceName = "none"
	ContainerdSourceDistro ContainerdSourceName = "distro"
	ContainerdSourceDocker ContainerdSourceName = "docker"
)

const trackerFile = "/opt/nodeadm/tracker"

type Tracker struct {
	Artifacts *InstalledArtifacts
}

type InstalledArtifacts struct {
	Containerd              ContainerdSourceName
	CniPlugins              bool
	IamAuthenticator        bool
	IamRolesAnywhere        bool
	ImageCredentialProvider bool
	Kubectl                 bool
	Kubelet                 bool
	Ssm                     bool
	Iptables                bool
}

// Add adds a components as installed to the tracker
func (tracker *Tracker) Add(componentName string) error {
	switch componentName {
	case artifact.CniPlugins:
		tracker.Artifacts.CniPlugins = true
	case artifact.IamAuthenticator:
		tracker.Artifacts.IamAuthenticator = true
	case artifact.IamRolesAnywhere:
		tracker.Artifacts.IamRolesAnywhere = true
	case artifact.ImageCredentialProvider:
		tracker.Artifacts.ImageCredentialProvider = true
	case artifact.Kubectl:
		tracker.Artifacts.Kubectl = true
	case artifact.Kubelet:
		tracker.Artifacts.Kubelet = true
	case artifact.Ssm:
		tracker.Artifacts.Ssm = true
	case artifact.Iptables:
		tracker.Artifacts.Iptables = true
	default:
		return fmt.Errorf("invalid artifact to track")
	}
	return nil
}

// Save() saves the tracker to file
func (tracker *Tracker) Save() error {
	// ensure containerd source is populated with none/distro/docker
	containerdSource, err := ContainerdSource(string(tracker.Artifacts.Containerd))
	if err != nil {
		return err
	}
	tracker.Artifacts.Containerd = containerdSource
	data, err := yaml.Marshal(tracker)
	if err != nil {
		return err
	}

	return util.WriteFileWithDir(trackerFile, data, 0o644)
}

func Clear(logger *zap.Logger) error {
	trackerDir := path.Dir(trackerFile)

	passwdFile := "/etc/passwd"
	if _, err := os.Stat(passwdFile); os.IsNotExist(err) {
		logger.Warn("Before /etc/passwd file does not exist", zap.String("path", passwdFile))
	} else if err != nil {
		logger.Error("Before Error checking /etc/passwd file status", zap.String("path", passwdFile), zap.Error(err))
	} else {
		logger.Info("Before /etc/passwd file is present", zap.String("path", passwdFile))
	}

	logger.Info("Clearing tracker directory", zap.String("path", trackerDir))
	if err := os.RemoveAll(trackerDir); err != nil {
		logger.Error("Failed to clear tracker directory", zap.String("path", trackerDir), zap.Error(err))
		return err
	}

	if _, err := os.Stat(passwdFile); os.IsNotExist(err) {
		logger.Warn("After /etc/passwd file does not exist", zap.String("path", passwdFile))
	} else if err != nil {
		logger.Error("After Error checking /etc/passwd file status", zap.String("path", passwdFile), zap.Error(err))
	} else {
		logger.Info("After /etc/passwd file is present", zap.String("path", passwdFile))
	}

	logger.Info("Successfully cleared tracker directory", zap.String("path", trackerDir))
	return nil
}

// GetInstalledArtifacts reads the tracker file and returns the current
// installed artifacts
func GetInstalledArtifacts() (*Tracker, error) {
	yamlFileData, err := os.ReadFile(trackerFile)
	if err != nil {
		return nil, err
	}
	var artifacts Tracker
	err = yaml.Unmarshal(yamlFileData, &artifacts)
	if err != nil {
		return nil, errors.Wrap(err, "invalid yaml data in tracker")
	}
	// containerd will be non-empty if containerd is being managed by nodeadm
	// otherwise it *may* be empty, which we want want to ensure is treated as "none"
	containerdSource, err := ContainerdSource(string(artifacts.Artifacts.Containerd))
	if err != nil {
		return nil, err
	}
	artifacts.Artifacts.Containerd = containerdSource

	return &artifacts, nil
}

// GetCurrentState reads the tracker file and returns current state
// If tracker file does not exist, it creates a new tracker
func GetCurrentState() (*Tracker, error) {
	tracker, err := GetInstalledArtifacts()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Tracker{
				Artifacts: &InstalledArtifacts{},
			}, nil
		}
		return nil, err
	}
	return tracker, nil
}

func ContainerdSource(containerdSource string) (ContainerdSourceName, error) {
	switch containerdSource {
	case string(ContainerdSourceDistro):
		return ContainerdSourceDistro, nil
	case string(ContainerdSourceDocker):
		return ContainerdSourceDocker, nil
	case "", "none":
		return ContainerdSourceNone, nil
	default:
		return "", fmt.Errorf("invalid containerd source: %s", containerdSource)
	}
}
