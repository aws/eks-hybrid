package packagemanager

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetContainerdPackageVersion(t *testing.T) {
	tests := []struct {
		name, manager, dockerRepo, version, wantPackage string
	}{
		{"apt distro", aptPackageManager, "", "1:1.7.27-0ubuntu1", "containerd=1:1.7.27-0ubuntu1"},
		{"apt docker", aptPackageManager, ubuntuDockerRepo, "2.0.4-1", "containerd.io=2.0.4-1"},
		{"yum distro", yumPackageManager, "", "1.7.27-1.amzn2023.0.1", "containerd-1.7.27-1.amzn2023.0.1"},
		{"yum docker", yumPackageManager, centOsDockerRepo, "2.0.4-1.el9", "containerd.io-2.0.4-1.el9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm := &DistroPackageManager{manager: tt.manager, dockerRepo: tt.dockerRepo, installVerb: "install"}
			command := pm.GetContainerd(tt.version).InstallCmd(context.Background())
			assert.Equal(t, []string{tt.manager, "install", tt.wantPackage, "-y"}, command.Args)
		})
	}
}
