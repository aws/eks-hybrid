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

// The following public key expires on 2027-11-25 (November 25, 2027). Systems Manager will
// publish a new key before the old one expires, we should migrate to that key at that time.
// See https://docs.aws.amazon.com/systems-manager/latest/userguide/verify-agent-signature.html#verify-agent-signature-current
const ssmPublicGPGKey = `-----BEGIN PGP PUBLIC KEY BLOCK-----
Version: GnuPG v2.0.22 (GNU/Linux)

mQINBGohhG8BEACxNOk7TM1ywHjW0qrSyH9npRDUjpWAyCD8L1Yu7nLnxwEBtUOk
HgQIo1scTWuwogaBZpAzg22A25AloOrhX8BGTokh71Xm3LbQT8dDQUDT7WRTl4R5
p7786TN79p4DqD3/JyzjiD/keTYyhplBWdyk5BEcqlyVj9Pf/1O6CrOJgGD1oGYK
7lRqMtmXlf86/mKveWvjJTPAF26dkDJZecnrheyEA99XlONm8zAlK6h09JThHBmW
MgDPXDSsVeEyXilPUJFhZJ2HXVG2tS9ioVM309tMX6B3W5woT7w8SA6uV2Pf0jMm
oYmMuCmQMU8/7/vOLQSJm+6Wui5dZywArqtOqXcSledRf7xhmug3Qa5maqY2Ttcn
XqTaH3WaBLtJZYqDJcsUH99AXqdQkozsCHHL2NmxRiShyLCX4VBKzH8j5kDNGeSL
uiICbj1Tufh7LDuIwdOQnX9gvB6j/SF3YVqJl4DAjCs4ODYpwm+36RZjQkbwELzX
zXnDoNgjR5tXCiNdxJLuzemQy7FcDc90fEbpDX03rN7iceavBmZbZ5Tlu53fgpzd
kflD8OwfjnYa9AXh2zweC1G0ut2QU+cZcoOq5/QPtQELNqvgpAUsQtkCECTerz8S
ugriZD4Tnd4FJUgoqX+/iwCWesyWjGkotxi7aJluLtz+znNT9SgeMCoJmQARAQAB
tCdTU00gQWdlbnQgPHNzbS1hZ2VudC1zaWduZXJAYW1hem9uLmNvbT6JAj8EEwEC
ACkFAmohhG8CGy8FCQLGmIAHCwkIBwMCAQYVCAIJCgsEFgIDAQIeAQIXgAAKCRAn
hNvziNGdRnXVD/9bP0IX6aU+qur66fTWs8RLDs//6Vx93e8lH6p4W+RxL+wW7Ajc
/REB7PPgc5ohW80/LwxHP2g8cZiSK7fp9cRbXODqiVYl4mAWQNfxfGcWpBxcEsyo
UGF6oRjiibL1BcJLFJgVZ/H4pT0xNEDJNIJpRn7ZVXm0vrVVVUh4a4WFXUkspRXm
q37eIMm76p969L5SNjY+F8Ld6hjmRiRopg4f/+lf/+e6R+fofSOVE3Ip/YpPH1DR
plwQJrMk1XFjm+JTsdtqMTIKv38j7HANmUgDGLsuQNn8048n6ve8koVZLSrv9FAi
elSNsSIurLEpkLM0QPOFviagK7zB/d/zScpGquBU9dPFOoTq/sNFhnZr+PA7MAek
r4WOP8kasS3LWGn/rqqIOw/PDm2mmUaIwm4XenOe8lJFKUGp6ssUOj4PIQKwGaL/
S2xTex25ZaNQaKXv2JUmriGycpCjIaniEHz60bBN1kSgO0m0a5Yg3X9Qw+ezt51o
Pi6H6NdE0IwNzNcbJI2UvphwR9L92Br/J9ElYbHRdgmz26SZeRNTht/+zKPxk4iD
YibpQ804cPfA0mJBODwzqL5BBfVJ0/rLtQ2ymuHaqKbKEc0KuJ6vWMoH9qlMBlv5
YiZeUw5Ls2uuaxhKTUYrCE4LuUlAtNW63+/TP+ciBg1bq5H5aBn5tSCYV4kCHAQQ
AQIABgUCaiGEcQAKCRB90Jej2tf1/PUvD/43D4uSk84EFEbvxHsXazhieLpzoMql
zYcxOWjRF9+8bYq9NQlC7gYDI3xS7y0NYfNWHdPU2FFO7Obu9XhZp2gQ+NqyyVTj
GoivF2uIhM6xsE1r9ZXXMLap4xbnPTfbLgQIyXkb1Dw0VVK40yFFOHMUs0mXJN6m
/S+FP3TDcgoDxgyRsRAB0vGKPIJABtlmR9Fg0tPEZpA4kbawiEq5uBjv5Fol/ctl
jRZ51ksszlxhZd0t9/aBpe7XBHpfIlsItPVskHV6Kpc274lj+f0BVE70/1n/++FM
sxpQNuN7mdXXzMLASifVapPQGLBeHuxriHsgh+V6Arpswojy1uPgp6J8B/gQQ/B2
mebiGJrM/LwNPWj79QPPgzUFz6LqYnyhtJCWURWJjzHRSXsPWKgd98AXjkeSgUz9
8NV5p9Td1uZ0iYQ7Qlo58Ih6jfPFULnftiQuRnp/5ErhojV/zxKzjZNmB6YZ508M
nYrz5/++8LBc6uAf/Oq907CdfEkAYid8j2dFttksJbhlYVTnuo9vd4BdOovnB88V
dvyFJcFzlWsys2vrH6CGmngYKStQHEA588/d1w1E7Z4A8XJJiBKkBMZSOrWDmUjG
vihLPXNOsrpdCZTI9jlznp6OrRVP3P9fp3fXj1nJeIdMr30akz19Lze6T9jsDDWa
M98H0Sl+MxUsAg==
=TpOv
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
		partition := awsinternal.GetPartitionFromRegionFallback(s.region)
		dnsSuffix = awsinternal.GetPartitionDNSSuffix(partition)
	}

	platform := fmt.Sprintf("%s_%s", variant, runtime.GOARCH)
	return fmt.Sprintf("https://amazon-ssm-%s.s3.%s.%s/latest/%s/ssm-setup-cli", s.region, s.region, dnsSuffix, platform), nil
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
