package ssm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"

	"go.uber.org/zap"

	awsinternal "github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/util"
)

// Initial region ssm installer is downloaded from. When installer runs, it will
// down the agent from the proper region configured in the nodeConfig during init command
const DefaultSsmInstallerRegion = "us-west-2"

// The following public key expires on 2026-07-15 (July 15, 2026). Systems Manager will
// publish a new key before the old one expires, we should migrate to that key at that time.
// See https://docs.aws.amazon.com/systems-manager/latest/userguide/verify-agent-signature.html#verify-agent-signature-current
const ssmPublicGPGKey = `-----BEGIN PGP PUBLIC KEY BLOCK-----
Version: GnuPG v2.0.22 (GNU/Linux)

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
E6+O8RI2451Dcg==
=aDkv
-----END PGP PUBLIC KEY BLOCK-----`

type SSMInstallerOption func(*ssmInstallerSource)

// WithURLBuilder allows overriding the SSM installer download URL.
func WithURLBuilder(builder func() (string, error)) SSMInstallerOption {
	return func(s *ssmInstallerSource) {
		s.buildSSMURL = builder
	}
}

// WithPublicKey allows setting the public key for signature validation
func WithPublicKey(key string) SSMInstallerOption {
	return func(s *ssmInstallerSource) {
		s.publicKey = key
	}
}

// WithDnsSuffix allows setting the DNS suffix from manifest data
// This takes precedence over region-based partition detection
func WithDnsSuffix(dnsSuffix string) SSMInstallerOption {
	return func(s *ssmInstallerSource) {
		s.dnsSuffix = dnsSuffix
	}
}

// SSMInstaller provides a Source that retrieves the SSM installer from the official
// release endpoint.
func NewSSMInstaller(logger *zap.Logger, region string, opts ...SSMInstallerOption) Source {
	s := &ssmInstallerSource{
		region:    region,
		logger:    logger,
		publicKey: ssmPublicGPGKey,
	}

	// Set default URL builder
	s.buildSSMURL = s.defaultBuildSSMURL

	for _, opt := range opts {
		opt(s)
	}

	return s
}

type ssmInstallerSource struct {
	region      string
	dnsSuffix   string // DNS suffix from manifest (optional, falls back to region-based detection)
	logger      *zap.Logger
	buildSSMURL func() (string, error)
	publicKey   string
}

func (s ssmInstallerSource) GetSSMInstaller(ctx context.Context) (io.ReadCloser, error) {
	endpoint, err := s.buildSSMURL()
	if err != nil {
		return nil, err
	}

	s.logger.Info("Downloading SSM installer", zap.String("region", s.region), zap.String("url", endpoint))

	obj, err := util.GetHttpFileReader(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func (s ssmInstallerSource) GetSSMInstallerSignature(ctx context.Context) (io.ReadCloser, error) {
	endpoint, err := s.buildSSMURL()
	if err != nil {
		return nil, err
	}
	obj, err := util.GetHttpFileReader(ctx, endpoint+".sig")
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func (s ssmInstallerSource) PublicKey() string {
	return s.publicKey
}

// defaultBuildSSMURL builds the SSM installer URL with partition-aware DNS suffix
func (s ssmInstallerSource) defaultBuildSSMURL() (string, error) {
	variant, err := detectPlatformVariant()
	if err != nil {
		return "", err
	}

	dnsSuffix := s.dnsSuffix
	if dnsSuffix == "" {
		// Fallback to region-based partition detection only if manifest DNS suffix not provided
		// This is needed for unit tests and edge cases where manifest is not available
		partition := awsinternal.GetPartitionFromRegionFallback(s.region)
		dnsSuffix = awsinternal.GetPartitionDNSSuffix(partition)
	}

	platform := fmt.Sprintf("%v_%v", variant, runtime.GOARCH)
	return fmt.Sprintf("https://amazon-ssm-%v.s3.%v.%s/latest/%v/ssm-setup-cli", s.region, s.region, dnsSuffix, platform), nil
}

// detectPlatformVariant returns a portion of the SSM installers URL that is dependent on the
// package manager in use.
func detectPlatformVariant() (string, error) {
	toVariant := map[string]string{
		"apt": "debian",
		"dnf": "linux",
		"yum": "linux",
	}

	for pkgManager := range toVariant {
		_, err := exec.LookPath(pkgManager)
		if errors.Is(err, exec.ErrNotFound) {
			continue
		}
		if err != nil {
			return "", err
		}

		return toVariant[pkgManager], nil
	}

	return "", errors.New("unsupported platform")
}
