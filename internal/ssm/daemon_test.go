package ssm

import (
	"testing"

	"github.com/aws/eks-hybrid/internal/system"
)

func TestResolveDaemonName(t *testing.T) {
	tests := []struct {
		name         string
		osName       string
		snapExists   bool
		expectedName string
	}{
		{
			name:         "ubuntu with snap install",
			osName:       system.UbuntuOsName,
			snapExists:   true,
			expectedName: snapSsmDaemonName,
		},
		{
			name:         "ubuntu with deb install",
			osName:       system.UbuntuOsName,
			snapExists:   false,
			expectedName: defaultSsmDaemonName,
		},
		{
			name:         "rhel",
			osName:       system.RhelOsName,
			snapExists:   false,
			expectedName: defaultSsmDaemonName,
		},
		{
			name:         "amazon linux",
			osName:       system.AmazonOsName,
			snapExists:   false,
			expectedName: defaultSsmDaemonName,
		},
		{
			name:         "unknown os",
			osName:       "unknown",
			snapExists:   false,
			expectedName: defaultSsmDaemonName,
		},
		{
			name:         "empty os name",
			osName:       "",
			snapExists:   false,
			expectedName: defaultSsmDaemonName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exists := func(path string) bool {
				return tt.snapExists && path == snapAgentBinaryPath
			}
			got := resolveDaemonName(tt.osName, exists)
			if got != tt.expectedName {
				t.Errorf("resolveDaemonName(%q) = %q, want %q", tt.osName, got, tt.expectedName)
			}
		})
	}
}
