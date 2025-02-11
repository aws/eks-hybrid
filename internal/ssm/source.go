package ssm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"

	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/util"
)

// Initial region ssm installer is downloaded from. When installer runs, it will
// down the agent from the proper region configured in the nodeConfig during init command
const DefaultSsmInstallerRegion = "us-west-2"

// SSMInstaller provides a Source that retrieves the SSM installer from the official
// release endpoint.
func NewSSMInstaller(logger *zap.Logger, region string) Source {
	if region == "" {
		region = DefaultSsmInstallerRegion
	}

	return ssmInstallerSource{
		region: region,
		logger: logger,
	}
}

type ssmInstallerSource struct {
	region string
	logger *zap.Logger
}

func (s ssmInstallerSource) GetSSMInstaller(ctx context.Context) (io.ReadCloser, error) {
	endpoint, err := buildSSMURL(s.region)
	if err != nil {
		return nil, err
	}

	s.logger.Info("Downloading SSM installer", zap.String("region", s.region), zap.String("url", endpoint))

	obj, err := util.GetHttpFileReader(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("unable to download SSM installer from region %s, "+
			"please check SSM is available on the requested region (valid example: us-west-2)", s.region)
	}
	return obj, nil
}

func buildSSMURL(region string) (string, error) {
	variant, err := detectPlatformVariant()
	if err != nil {
		return "", err
	}

	platform := fmt.Sprintf("%v_%v", variant, runtime.GOARCH)
	return fmt.Sprintf("https://amazon-ssm-%v.s3.%v.amazonaws.com/latest/%v/ssm-setup-cli", region, region, platform), nil
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
