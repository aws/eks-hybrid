package tracker

import (
	"fmt"
	"io/fs"
	"os"
	"path"

	"github.com/pkg/errors"
	"sigs.k8s.io/yaml"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/util"
)

const trackerFile = "/opt/nodeadm/tracker"

type Tracker struct {
	Artifacts *InstalledArtifacts
}

type InstalledArtifacts struct {
	Containerd              string
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

func (tracker *Tracker) MarkContainerd(source string) {
	tracker.Artifacts.Containerd = source
}

// Save() saves the tracker to file
func (tracker *Tracker) Save() error {
	data, err := yaml.Marshal(tracker)
	if err != nil {
		return err
	}

	return util.WriteFileWithDir(trackerFile, data, 0o644)
}

func Clear() error {
	return os.RemoveAll(path.Dir(trackerFile))
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
