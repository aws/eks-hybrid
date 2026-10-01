package containerd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/tracker"
)

func TestDetermineContainerdVersionConstraint(t *testing.T) {
	tests := []struct {
		name               string
		kubernetesVersion  string
		expectedConstraint string
	}{
		{
			name:               "K8s 1.28 should use containerd 1.* constraint",
			kubernetesVersion:  "1.28.0",
			expectedConstraint: "1.*",
		},
		{
			name:               "K8s 1.29.0 should use containerd 1.* constraint",
			kubernetesVersion:  "1.29.0",
			expectedConstraint: "1.*",
		},
		{
			name:               "K8s 1.29.5 should use containerd 1.* constraint",
			kubernetesVersion:  "1.29.5",
			expectedConstraint: "1.*",
		},
		{
			name:               "K8s 1.30.0 should use no constraint (allows 2.x)",
			kubernetesVersion:  "1.30.0",
			expectedConstraint: "",
		},
		{
			name:               "K8s 1.30.1 should use no constraint (allows 2.x)",
			kubernetesVersion:  "1.30.1",
			expectedConstraint: "",
		},
		{
			name:               "K8s 1.30.5 should use no constraint (allows 2.x)",
			kubernetesVersion:  "1.30.5",
			expectedConstraint: "",
		},
		{
			name:               "K8s 1.31.0 should use no constraint (allows 2.x)",
			kubernetesVersion:  "1.31.0",
			expectedConstraint: "",
		},
		{
			name:               "K8s 1.32.0 should use no constraint (allows 2.x)",
			kubernetesVersion:  "1.32.0",
			expectedConstraint: "",
		},
		{
			name:               "K8s 2.0.0 should use no constraint (allows 2.x)",
			kubernetesVersion:  "2.0.0",
			expectedConstraint: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineContainerdVersionConstraint(tt.kubernetesVersion)
			assert.Equal(t, tt.expectedConstraint, got,
				"determineContainerdVersionConstraint(%q) returned %q, expected %q",
				tt.kubernetesVersion, got, tt.expectedConstraint)
		})
	}
}

func TestValidateContainerdVersion(t *testing.T) {
	tests := []struct {
		name, version, kubernetesVersion string
		source                           tracker.ContainerdSourceName
		wantError                        string
	}{
		{"unresolved Kubernetes", "2.0.4-1", "", tracker.ContainerdSourceDocker, ""},
		{"automatic", "", "1.29.0", tracker.ContainerdSourceDistro, ""},
		{"automatic none", "", "1.31.0", tracker.ContainerdSourceNone, ""},
		{"upstream", "1.7.27", "1.29.0", tracker.ContainerdSourceDistro, ""},
		{"rpm release", "1.7.27-1.amzn2023.0.1", "1.31.0", tracker.ContainerdSourceDistro, ""},
		{"debian epoch", "1:1.7.27-0ubuntu1~22.04.1", "1.29.0", tracker.ContainerdSourceDistro, ""},
		{"docker", "2.0.4-1", "1.30.0", tracker.ContainerdSourceDocker, ""},
		{"incompatible", "2.0.4-1", "1.29.0", tracker.ContainerdSourceDocker, "incompatible"},
		{"incompatible epoch", "1:2.0.4-1", "1.29.0", tracker.ContainerdSourceDocker, "incompatible"},
		{"none", "1.7.27", "1.31.0", tracker.ContainerdSourceNone, "--containerd-source none"},
		{"wildcard", "1.7.*", "1.31.0", tracker.ContainerdSourceDistro, "invalid"},
		{"partial", "1.7", "1.31.0", tracker.ContainerdSourceDistro, "invalid"},
		{"leading zero", "01.7.27", "1.31.0", tracker.ContainerdSourceDistro, "invalid"},
		{"whitespace", "1.7.27 -y", "1.31.0", tracker.ContainerdSourceDistro, "invalid"},
		{"option", "--allow-downgrades", "1.31.0", tracker.ContainerdSourceDistro, "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateContainerdVersion(tt.version, tt.kubernetesVersion, tt.source)
			if tt.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantError)
			}
		})
	}
}

type recordingContainerdSource struct {
	versions []string
}

func (s *recordingContainerdSource) GetContainerd(version string) artifact.Package {
	s.versions = append(s.versions, version)
	command := artifact.NewCmd("/bin/sh", "-c", "exit 0")
	return artifact.NewPackageSource(command, command, command)
}

func TestInstallContainerdVersion(t *testing.T) {
	tests := []struct {
		name, version, kubernetesVersion string
		source                           tracker.ContainerdSourceName
		installed                        bool
		wantVersions                     []string
		wantSource                       tracker.ContainerdSourceName
		wantError                        string
	}{
		{"default old Kubernetes", "", "1.29.0", tracker.ContainerdSourceDistro, false, []string{"1.*"}, tracker.ContainerdSourceDistro, ""},
		{"default new Kubernetes", "", "1.31.0", tracker.ContainerdSourceDocker, false, []string{""}, tracker.ContainerdSourceDocker, ""},
		{"explicit distro", "1.7.27-1.amzn2023.0.1", "1.31.0", tracker.ContainerdSourceDistro, false, []string{"1.7.27-1.amzn2023.0.1"}, tracker.ContainerdSourceDistro, ""},
		{"explicit docker", "2.0.4-1", "1.31.0", tracker.ContainerdSourceDocker, false, []string{"2.0.4-1"}, tracker.ContainerdSourceDocker, ""},
		{"existing automatic", "", "1.31.0", tracker.ContainerdSourceDistro, true, nil, tracker.ContainerdSourceNone, ""},
		{"existing explicit", "1:1.7.27-1", "1.31.0", tracker.ContainerdSourceDistro, true, []string{"1:1.7.27-1"}, tracker.ContainerdSourceDistro, ""},
		{"none", "", "1.31.0", tracker.ContainerdSourceNone, false, nil, tracker.ContainerdSourceNone, ""},
		{"invalid before install", "1.*", "1.31.0", tracker.ContainerdSourceDistro, false, nil, "", "invalid"},
		{"incompatible before install", "2.0.4", "1.29.0", tracker.ContainerdSourceDocker, false, nil, "", "incompatible"},
		{"none explicit", "1.7.27", "1.31.0", tracker.ContainerdSourceNone, true, nil, "", "--containerd-source none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binDir := t.TempDir()
			t.Setenv("PATH", binDir)
			if tt.installed {
				for _, name := range []string{"containerd", "runc"} {
					require.NoError(t, os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755))
				}
			}
			source := &recordingContainerdSource{}
			artifacts := &tracker.Tracker{Artifacts: &tracker.InstalledArtifacts{}}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := Install(ctx, artifacts, source, tt.source, tt.kubernetesVersion, tt.version)
			if tt.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantError)
			}
			assert.Equal(t, tt.wantVersions, source.versions)
			assert.Equal(t, tt.wantSource, artifacts.Artifacts.Containerd)
		})
	}
}
