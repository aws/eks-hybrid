package aws

import (
	"context"
	"crypto/sha256"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/mod/semver"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/util"
)

// Source defines a single version source for aws provided artifacts
type Source struct {
	Eks EksPatchRelease
	Iam IamRolesAnywhereRelease
}

// GetLatestSource gets the source for latest version of aws provided artifacts
func GetLatestSource(ctx context.Context, eksVersion string) (Source, error) {
	manifest, err := getReleaseManifest(ctx)
	if err != nil {
		return Source{}, err
	}

	eksPatchRelease, err := getLatestEksSource(eksVersion, manifest)
	if err != nil {
		return Source{}, errors.Wrap(err, "getting latest eks release")
	}

	iamRolesAnywhereRelease, err := getLatestIamRolesAnywhereSource(manifest)
	if err != nil {
		return Source{}, errors.Wrap(err, "getting iam roles anywhere release")
	}

	return Source{
		Eks: eksPatchRelease,
		Iam: iamRolesAnywhereRelease,
	}, nil
}

func getLatestIamRolesAnywhereSource(manifest *Manifest) (IamRolesAnywhereRelease, error) {
	if len(manifest.IamRolesAnywhereReleases) < 1 {
		return IamRolesAnywhereRelease{}, fmt.Errorf("no iam signer helper releases found")
	}
	latestRelease := manifest.IamRolesAnywhereReleases[0]
	for _, release := range manifest.IamRolesAnywhereReleases {
		if semver.Compare(latestRelease.Version, release.Version) < 0 {
			latestRelease = release
		}
	}
	return latestRelease, nil
}

func getLatestEksSource(eksVersion string, manifest *Manifest) (EksPatchRelease, error) {
	if eksVersion == "" {
		return EksPatchRelease{}, fmt.Errorf("eks version is empty")
	}

	if !semver.IsValid("v" + eksVersion) {
		return EksPatchRelease{}, fmt.Errorf("invalid semantic version: %v", eksVersion)
	}

	// Check if input is major.minor or major.minor.patch
	// semver.MajorMinor requires "v" to prepended
	majorMinorVersion := semver.MajorMinor("v" + eksVersion)

	// remove v from the majorMinor
	majorMinorVersion = strings.ReplaceAll(majorMinorVersion, "v", "")

	// find if input version has patch version
	patch, hasPatchVersion := strings.CutPrefix(eksVersion, majorMinorVersion+".")

	// Find the patch release
	var matchedPatchReleases []EksPatchRelease
	for _, supportedRelease := range manifest.SupportedEksReleases {
		if supportedRelease.MajorMinorVersion == majorMinorVersion {
			for _, release := range supportedRelease.PatchReleases {
				// Check if patch version was provided in the input
				if hasPatchVersion {
					if release.PatchVersion == patch {
						matchedPatchReleases = append(matchedPatchReleases, release)
					}
				} else {
					if release.PatchVersion == supportedRelease.LatestPatchVersion {
						matchedPatchReleases = append(matchedPatchReleases, release)
					}
				}
			}
		}
	}

	if len(matchedPatchReleases) == 1 {
		return matchedPatchReleases[0], nil
	} else if len(matchedPatchReleases) > 1 {
		return getLatestDateEksPatchRelease(matchedPatchReleases)
	}

	// If patch version was provided in the input and associated release was not found, throw an error
	if hasPatchVersion {
		return EksPatchRelease{}, fmt.Errorf("input semver did not match with available releases. Try again with major.minor version")
	}

	return EksPatchRelease{}, fmt.Errorf("input semver did not match with any available releases")
}

func getLatestDateEksPatchRelease(patchReleases []EksPatchRelease) (EksPatchRelease, error) {
	if len(patchReleases) == 0 {
		return EksPatchRelease{}, fmt.Errorf("input semver did not match with any available releases")
	}
	dateLayout := "2006-01-02"
	latestRelease := patchReleases[0]
	for _, release := range patchReleases {
		latestReleaseDate, err := time.Parse(dateLayout, latestRelease.ReleaseDate)
		if err != nil {
			return EksPatchRelease{}, err
		}
		parsedReleaseDate, err := time.Parse(dateLayout, release.ReleaseDate)
		if err != nil {
			return EksPatchRelease{}, err
		}
		if parsedReleaseDate.After(latestReleaseDate) {
			latestRelease = release
		}
	}
	return latestRelease, nil
}

func GetRegionConfig(ctx context.Context, region string) (*RegionData, error) {
	manifest, err := getReleaseManifest(ctx)
	if err != nil {
		return nil, err
	}

	regionCfg, ok := manifest.RegionConfig[region]
	if !ok {
		return nil, fmt.Errorf("region %s not found in manifest", region)
	}

	return &regionCfg, nil
}

// GetKubelet satisfies kubelet.Source.
func (as Source) GetKubelet(ctx context.Context) (artifact.Source, error) {
	return as.getEksSource(ctx, "kubelet")
}

// GetKubectl satisfies kubectl.Source.
func (as Source) GetKubectl(ctx context.Context) (artifact.Source, error) {
	return as.getEksSource(ctx, "kubectl")
}

// GetIAMAuthenticator satisfies iamrolesanywhere.IAMAuthenticatorSource.
func (as Source) GetIAMAuthenticator(ctx context.Context) (artifact.Source, error) {
	return as.getEksSource(ctx, "aws-iam-authenticator")
}

// GetImageCredentialProvider satisfies imagecredentialprovider.Source.
func (as Source) GetImageCredentialProvider(ctx context.Context) (artifact.Source, error) {
	return as.getEksSource(ctx, "ecr-credential-provider")
}

// GetCniPlugins satisfies cniplugins.Source
func (as Source) GetCniPlugins(ctx context.Context) (artifact.Source, error) {
	return as.getEksSource(ctx, "cni-plugins")
}

func (as Source) getEksSource(ctx context.Context, artifactName string) (artifact.Source, error) {
	return getSource(ctx, artifactName, as.Eks.Artifacts)
}

// GetSingingHelper satisfies iamrolesanywhere.SigningHelperSource
func (as Source) GetSigningHelper(ctx context.Context) (artifact.Source, error) {
	return getSource(ctx, "aws_signing_helper", as.Iam.Artifacts)
}

func getSource(ctx context.Context, artifactName string, availableArtifacts []Artifact) (artifact.Source, error) {
	for _, releaseArtifact := range availableArtifacts {
		if releaseArtifact.Name == artifactName && releaseArtifact.Arch == runtime.GOARCH && releaseArtifact.OS == runtime.GOOS {
			uri := releaseArtifact.URI
			if releaseArtifact.GzipURI != "" {
				// the same checksum will be used for both gzip and non-gzip uri
				// gzip decompression will happen before checksum verification
				uri = releaseArtifact.GzipURI
			}
			obj, err := util.GetHttpFileReader(ctx, uri)
			if err != nil {
				return nil, fmt.Errorf("getting artifact file reader: %w", err)
			}

			artifactChecksum, err := util.GetHttpFile(ctx, releaseArtifact.ChecksumURI)
			if err != nil {
				obj.Close()
				return nil, fmt.Errorf("getting artifact checksum file reader: %w", err)
			}

			var source artifact.Source
			if releaseArtifact.GzipURI != "" {
				source, err = artifact.GzippedWithChecksum(obj, sha256.New(), artifactChecksum)
			} else {
				source, err = artifact.WithChecksum(obj, sha256.New(), artifactChecksum)
			}

			if err != nil {
				obj.Close()
				return nil, fmt.Errorf("getting artifact with checksum: %w", err)
			}
			return source, nil
		}
	}
	return nil, fmt.Errorf("could not find artifact for %s arch and %s os", runtime.GOARCH, runtime.GOOS)
}
