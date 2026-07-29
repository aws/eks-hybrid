package os

import (
	"testing"

	"github.com/aws/eks-hybrid/test/e2e"
)

// TestGetInstanceTypesFromRegionAndArchPreferredType pins the first candidate for every
// combination. The first entry is the type that was used before fallback support was added,
// so this guards against accidentally changing the instance type used on the happy path.
func TestGetInstanceTypesFromRegionAndArchPreferredType(t *testing.T) {
	tests := []struct {
		name        string
		arch        architecture
		size        e2e.InstanceSize
		computeType e2e.ComputeType
		want        string
	}{
		{"amd64 large cpu", amd64, e2e.Large, e2e.CPUInstance, "t3.large"},
		{"amd64 xlarge cpu", amd64, e2e.XLarge, e2e.CPUInstance, "t3.xlarge"},
		{"arm64 large cpu", arm64, e2e.Large, e2e.CPUInstance, "t4g.large"},
		{"arm64 xlarge cpu", arm64, e2e.XLarge, e2e.CPUInstance, "t4g.xlarge"},
		{"amd64 large gpu", amd64, e2e.Large, e2e.GPUInstance, "g4dn.xlarge"},
		{"amd64 xlarge gpu", amd64, e2e.XLarge, e2e.GPUInstance, "g4dn.2xlarge"},
		{"arm64 large gpu", arm64, e2e.Large, e2e.GPUInstance, "g5g.xlarge"},
		{"arm64 xlarge gpu", arm64, e2e.XLarge, e2e.GPUInstance, "g5g.2xlarge"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getInstanceTypesFromRegionAndArch("us-west-2", tc.arch, tc.size, tc.computeType)
			if len(got) == 0 {
				t.Fatalf("expected at least one instance type, got none")
			}
			if got[0] != tc.want {
				t.Errorf("preferred instance type = %q, want %q", got[0], tc.want)
			}
		})
	}
}

// TestGetInstanceTypesFromRegionAndArchCPUHasFallbacks verifies CPU instances have fallback
// candidates, which is what allows nodes to launch in regions that do not offer the
// preferred burstable instance type.
func TestGetInstanceTypesFromRegionAndArchCPUHasFallbacks(t *testing.T) {
	for _, arch := range []architecture{amd64, arm64} {
		for _, size := range []e2e.InstanceSize{e2e.Large, e2e.XLarge} {
			types := getInstanceTypesFromRegionAndArch("ap-southeast-7", arch, size, e2e.CPUInstance)
			if len(types) < 2 {
				t.Errorf("arch %s size %d: expected fallback instance types, got %v", arch, size, types)
			}

			seen := map[string]bool{}
			for _, instanceType := range types {
				if seen[instanceType] {
					t.Errorf("arch %s size %d: duplicate instance type %q in %v", arch, size, instanceType, types)
				}
				seen[instanceType] = true
			}
		}
	}
}

// TestGetInstanceTypesFromRegionAndArchUnknownSizePanics documents that an unknown
// combination is treated as a coding error.
func TestGetInstanceTypesFromRegionAndArchUnknownSizePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unknown instance size, got none")
		}
	}()

	getInstanceTypesFromRegionAndArch("us-west-2", amd64, e2e.InstanceSize(99), e2e.CPUInstance)
}
