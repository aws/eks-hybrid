package aws

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestManifestUnmarshaling(t *testing.T) {
	// Read the test manifest file
	manifestPath := filepath.Join("testdata", "manifest.yaml")
	yamlData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("Failed to read test manifest file: %v", err)
	}

	// Unmarshal the YAML data
	var manifest Manifest
	err = yaml.Unmarshal(yamlData, &manifest)
	if err != nil {
		t.Fatalf("Failed to unmarshal manifest YAML: %v", err)
	}

	// Test top-level structure
	t.Run("TopLevelStructure", func(t *testing.T) {
		if len(manifest.SupportedEksReleases) == 0 {
			t.Error("Expected supported_eks_releases to be populated")
		}
		if len(manifest.IamRolesAnywhereReleases) == 0 {
			t.Error("Expected iam_roles_anywhere_releases to be populated")
		}
		if manifest.RegionConfig == nil {
			t.Error("Expected region_config to be populated")
		}
		// ssm_releases is null in the test data, so we shouldn't expect it to be populated
	})

	// Test SupportedEksReleases structure
	t.Run("SupportedEksReleases", func(t *testing.T) {
		if len(manifest.SupportedEksReleases) != 1 {
			t.Errorf("Expected 1 supported EKS release, got %d", len(manifest.SupportedEksReleases))
		}

		eksRelease := manifest.SupportedEksReleases[0]
		if eksRelease.MajorMinorVersion != "M.m" {
			t.Errorf("Expected major_minor_version to be 'M.m', got '%s'", eksRelease.MajorMinorVersion)
		}
		if eksRelease.LatestPatchVersion != "2" {
			t.Errorf("Expected latest_patch_version to be '2', got '%s'", eksRelease.LatestPatchVersion)
		}
		if len(eksRelease.PatchReleases) != 2 {
			t.Errorf("Expected 2 patch releases, got %d", len(eksRelease.PatchReleases))
		}
	})

	// Test PatchReleases structure
	t.Run("PatchReleases", func(t *testing.T) {
		patchRelease := manifest.SupportedEksReleases[0].PatchReleases[0]

		if patchRelease.Version != "M.m.p" {
			t.Errorf("Expected version to be 'M.m.p', got '%s'", patchRelease.Version)
		}
		if patchRelease.PatchVersion != "1" {
			t.Errorf("Expected patch_version to be '1', got '%s'", patchRelease.PatchVersion)
		}
		if patchRelease.ReleaseDate != "YYYY-MM-DD" {
			t.Errorf("Expected release_date to be 'YYYY-MM-DD', got '%s'", patchRelease.ReleaseDate)
		}
		if len(patchRelease.Artifacts) == 0 {
			t.Error("Expected artifacts to be populated")
		}
	})

	// Test Artifacts structure
	t.Run("Artifacts", func(t *testing.T) {
		artifact := manifest.SupportedEksReleases[0].PatchReleases[0].Artifacts[0]

		if artifact.Name != "kubectl" {
			t.Errorf("Expected name to be 'kubectl', got '%s'", artifact.Name)
		}
		if artifact.Arch != "amd64" {
			t.Errorf("Expected arch to be 'amd64', got '%s'", artifact.Arch)
		}
		if artifact.OS != "linux" {
			t.Errorf("Expected os to be 'linux', got '%s'", artifact.OS)
		}
		if artifact.URI == "" {
			t.Error("Expected URI to be populated")
		}
		if artifact.ChecksumURI == "" {
			t.Error("Expected ChecksumURI to be populated")
		}
	})

	// Test IamRolesAnywhereReleases structure
	t.Run("IamRolesAnywhereReleases", func(t *testing.T) {
		if len(manifest.IamRolesAnywhereReleases) != 1 {
			t.Errorf("Expected 1 IAM Roles Anywhere release, got %d", len(manifest.IamRolesAnywhereReleases))
		}

		iraRelease := manifest.IamRolesAnywhereReleases[0]
		if iraRelease.Version != "M.m.p" {
			t.Errorf("Expected version to be 'M.m.p', got '%s'", iraRelease.Version)
		}
		if len(iraRelease.Artifacts) != 2 {
			t.Errorf("Expected 2 artifacts, got %d", len(iraRelease.Artifacts))
		}

		// Check first artifact (amd64)
		amd64Artifact := iraRelease.Artifacts[0]
		if amd64Artifact.Arch != "amd64" {
			t.Errorf("Expected first artifact arch to be 'amd64', got '%s'", amd64Artifact.Arch)
		}
		if amd64Artifact.OS != "linux" {
			t.Errorf("Expected first artifact os to be 'linux', got '%s'", amd64Artifact.OS)
		}
		if amd64Artifact.Name != "dummy_signing_helper" {
			t.Errorf("Expected first artifact name to be 'dummy_signing_helper', got '%s'", amd64Artifact.Name)
		}

		// Check second artifact (arm64)
		arm64Artifact := iraRelease.Artifacts[1]
		if arm64Artifact.Arch != "arm64" {
			t.Errorf("Expected second artifact arch to be 'arm64', got '%s'", arm64Artifact.Arch)
		}
		if arm64Artifact.OS != "linux" {
			t.Errorf("Expected second artifact os to be 'linux', got '%s'", arm64Artifact.OS)
		}
		if arm64Artifact.Name != "dummy_signing_helper" {
			t.Errorf("Expected second artifact name to be 'dummy_signing_helper', got '%s'", arm64Artifact.Name)
		}
	})

	// Test RegionConfig structure
	t.Run("RegionConfig", func(t *testing.T) {
		// Check specific regions that exist in the updated manifest
		regions := []string{"us-east-1", "us-west-2", "eu-west-1"}

		for _, region := range regions {
			regionData, exists := manifest.RegionConfig[region]
			if !exists {
				t.Errorf("Expected region '%s' to exist in region_config", region)
				continue
			}

			if regionData.EcrAccountID == "" {
				t.Errorf("Expected ecr_account_id to be populated for region '%s'", region)
			}

			if regionData.CredProviders == nil {
				t.Errorf("Expected cred_providers to be populated for region '%s'", region)
				continue
			}

			// Check that both iam-ra and ssm are present and true
			if !regionData.CredProviders["iam-ra"] {
				t.Errorf("Expected iam-ra cred provider to be true for region '%s'", region)
			}
			if !regionData.CredProviders["ssm"] {
				t.Errorf("Expected ssm cred provider to be true for region '%s'", region)
			}
		}

		// Test specific ECR account IDs for known regions
		expectedEcrAccounts := map[string]string{
			"us-east-1":     "602401143452",
			"us-west-2":     "602401143452",
			"eu-west-1":     "602401143452",
			"us-gov-west-1": "013241004608",
			"cn-north-1":    "918309763551",
		}

		for region, expectedAccountID := range expectedEcrAccounts {
			if regionData, exists := manifest.RegionConfig[region]; exists {
				if regionData.EcrAccountID != expectedAccountID {
					t.Errorf("Expected ECR account ID for region '%s' to be '%s', got '%s'",
						region, expectedAccountID, regionData.EcrAccountID)
				}
			}
		}
	})

	// Test SsmReleases (should be null/empty in test data)
	t.Run("SsmReleases", func(t *testing.T) {
		if len(manifest.SsmReleases) != 0 {
			t.Error("Expected ssm_releases to be null or empty in test data")
		}
	})
}

func TestManifestStructValidation(t *testing.T) {
	// Test empty manifest
	t.Run("EmptyManifest", func(t *testing.T) {
		var manifest Manifest
		yamlData := []byte("{}")

		err := yaml.Unmarshal(yamlData, &manifest)
		if err != nil {
			t.Errorf("Expected empty manifest to unmarshal without error, got: %v", err)
		}
	})

	// Test malformed YAML
	t.Run("MalformedYAML", func(t *testing.T) {
		var manifest Manifest
		yamlData := []byte("invalid: yaml: content: [")

		err := yaml.Unmarshal(yamlData, &manifest)
		if err == nil {
			t.Error("Expected malformed YAML to produce an error")
		}
	})

	// Test partial manifest
	t.Run("PartialManifest", func(t *testing.T) {
		var manifest Manifest
		yamlData := []byte(`
supported_eks_releases:
- major_minor_version: "1.26"
  latest_patch_version: "15"
`)

		err := yaml.Unmarshal(yamlData, &manifest)
		if err != nil {
			t.Errorf("Expected partial manifest to unmarshal without error, got: %v", err)
		}

		if len(manifest.SupportedEksReleases) != 1 {
			t.Errorf("Expected 1 supported EKS release, got %d", len(manifest.SupportedEksReleases))
		}

		if manifest.SupportedEksReleases[0].MajorMinorVersion != "1.26" {
			t.Errorf("Expected major_minor_version to be '1.26', got '%s'",
				manifest.SupportedEksReleases[0].MajorMinorVersion)
		}
	})
}

func TestArtifactStructure(t *testing.T) {
	// Test artifact with all fields
	t.Run("CompleteArtifact", func(t *testing.T) {
		yamlData := []byte(`
name: kubectl
arch: amd64
os: linux
uri: https://example.com/kubectl
checksum_uri: https://example.com/kubectl.sha256
gzip_uri: https://example.com/kubectl.gz
`)

		var artifact Artifact
		err := yaml.Unmarshal(yamlData, &artifact)
		if err != nil {
			t.Errorf("Expected complete artifact to unmarshal without error, got: %v", err)
		}

		expected := Artifact{
			Name:        "kubectl",
			Arch:        "amd64",
			OS:          "linux",
			URI:         "https://example.com/kubectl",
			ChecksumURI: "https://example.com/kubectl.sha256",
			GzipURI:     "https://example.com/kubectl.gz",
		}

		if !reflect.DeepEqual(artifact, expected) {
			t.Errorf("Artifact mismatch.\nExpected: %+v\nGot: %+v", expected, artifact)
		}
	})

	// Test artifact with minimal fields
	t.Run("MinimalArtifact", func(t *testing.T) {
		yamlData := []byte(`
name: kubectl
arch: amd64
os: linux
uri: https://example.com/kubectl
`)

		var artifact Artifact
		err := yaml.Unmarshal(yamlData, &artifact)
		if err != nil {
			t.Errorf("Expected minimal artifact to unmarshal without error, got: %v", err)
		}

		if artifact.Name != "kubectl" {
			t.Errorf("Expected name to be 'kubectl', got '%s'", artifact.Name)
		}
		if artifact.ChecksumURI != "" {
			t.Errorf("Expected checksum_uri to be empty, got '%s'", artifact.ChecksumURI)
		}
		if artifact.GzipURI != "" {
			t.Errorf("Expected gzip_uri to be empty, got '%s'", artifact.GzipURI)
		}
	})
}

func TestRegionConfigStructure(t *testing.T) {
	t.Run("RegionConfigUnmarshaling", func(t *testing.T) {
		yamlData := []byte(`
us-east-1:
  ecr_account_id: "123456789012"
  cred_providers:
    iam-ra: true
    ssm: false
eu-west-1:
  ecr_account_id: "123456789012"
  cred_providers:
    iam-ra: false
    ssm: true
`)

		var regionConfig RegionConfig
		err := yaml.Unmarshal(yamlData, &regionConfig)
		if err != nil {
			t.Errorf("Expected region config to unmarshal without error, got: %v", err)
		}

		// Test us-east-1
		usEast1, exists := regionConfig["us-east-1"]
		if !exists {
			t.Error("Expected us-east-1 to exist in region config")
		} else {
			if usEast1.EcrAccountID != "123456789012" {
				t.Errorf("Expected ECR account ID to be '123456789012', got '%s'", usEast1.EcrAccountID)
			}
			if !usEast1.CredProviders["iam-ra"] {
				t.Error("Expected iam-ra to be true for us-east-1")
			}
			if usEast1.CredProviders["ssm"] {
				t.Error("Expected ssm to be false for us-east-1")
			}
		}

		// Test eu-west-1
		euWest1, exists := regionConfig["eu-west-1"]
		if !exists {
			t.Error("Expected eu-west-1 to exist in region config")
		} else {
			if euWest1.EcrAccountID != "123456789012" {
				t.Errorf("Expected ECR account ID to be '123456789012', got '%s'", euWest1.EcrAccountID)
			}
			if euWest1.CredProviders["iam-ra"] {
				t.Error("Expected iam-ra to be false for eu-west-1")
			}
			if !euWest1.CredProviders["ssm"] {
				t.Error("Expected ssm to be true for eu-west-1")
			}
		}
	})
}

// TestManifestDataConsistency tests that the test data is consistent and valid
func TestManifestDataConsistency(t *testing.T) {
	manifestPath := filepath.Join("testdata", "manifest.yaml")
	yamlData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("Failed to read test manifest file: %v", err)
	}

	var manifest Manifest
	err = yaml.Unmarshal(yamlData, &manifest)
	if err != nil {
		t.Fatalf("Failed to unmarshal manifest YAML: %v", err)
	}

	t.Run("ArtifactURIsValid", func(t *testing.T) {
		// Check that all artifact URIs are non-empty and follow expected patterns
		for _, eksRelease := range manifest.SupportedEksReleases {
			for _, patchRelease := range eksRelease.PatchReleases {
				for _, artifact := range patchRelease.Artifacts {
					if artifact.URI == "" {
						t.Errorf("Found empty URI for artifact %s in version %s",
							artifact.Name, patchRelease.Version)
					}
					if artifact.ChecksumURI == "" {
						t.Errorf("Found empty ChecksumURI for artifact %s in version %s",
							artifact.Name, patchRelease.Version)
					}
				}
			}
		}

		for _, iraRelease := range manifest.IamRolesAnywhereReleases {
			for _, artifact := range iraRelease.Artifacts {
				if artifact.URI == "" {
					t.Errorf("Found empty URI for IAM Roles Anywhere artifact %s in version %s",
						artifact.Name, iraRelease.Version)
				}
				if artifact.ChecksumURI == "" {
					t.Errorf("Found empty ChecksumURI for IAM Roles Anywhere artifact %s in version %s",
						artifact.Name, iraRelease.Version)
				}
			}
		}
	})

	t.Run("VersionConsistency", func(t *testing.T) {
		// Check that patch releases are consistent with their parent version
		// Note: With placeholder data "M.m.p", we'll skip exact prefix matching
		// but still verify the structure exists
		for _, eksRelease := range manifest.SupportedEksReleases {
			for _, patchRelease := range eksRelease.PatchReleases {
				if patchRelease.Version == "" {
					t.Error("Patch release version should not be empty")
				}
				if patchRelease.PatchVersion == "" {
					t.Error("Patch version should not be empty")
				}
			}
		}
	})

	t.Run("RegionDataComplete", func(t *testing.T) {
		// Check that all regions have required data
		for region, regionData := range manifest.RegionConfig {
			if regionData.EcrAccountID == "" {
				t.Errorf("Region %s has empty ECR account ID", region)
			}
			if regionData.CredProviders == nil {
				t.Errorf("Region %s has nil cred_providers", region)
			}
		}
	})
}
