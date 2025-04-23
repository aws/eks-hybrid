package iamrolesanywhere

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path"
	"text/template"
)

const (
	// DefaultAWSConfigPath is the path where the AWS config is written.
	DefaultAWSConfigPath = "/etc/aws/hybrid/config"

	// ProfileName is the profile used when writing the AWS config.
	ProfileName = "default"
)

//go:embed aws_config.tpl
var unformattedRawAWSConfigTpl string

var rawAWSConfigTpl = fmt.Sprintf(unformattedRawAWSConfigTpl, ProfileName)

var awsConfigTpl = template.Must(template.New("").Parse(rawAWSConfigTpl))

// AWSConfig defines the data for configuring IAM Roles Anywhere AWS Configuration files.
type AWSConfig struct {
	// TrustAnchorARN is the ARN of the trust anchor for IAM Roles Anywhere.
	TrustAnchorARN string

	// ProfileARN is the ARN of the profile for IAM Roles Anywhere.
	ProfileARN string

	// RoleARN is the role to assume after auth.
	RoleARN string

	// Region is the region to target when authenticating.
	Region string

	// NodeName is the name of the node. Used to set session name on IAM
	NodeName string

	// ConfigPath is a path to a configuration file to be verified. Defaults to /etc/aws/hybrid/profile.
	ConfigPath string

	// SigningHelperBinPath is a pth to the aws iam roles anywhere signer helper. Defaults to /usr/local/bin/aws_signing_helper
	SigningHelperBinPath string

	// CertificatePath is the location on disk for the certificate used to authenticate with AWS.
	CertificatePath string `json:"certificatePath,omitempty"`

	// PrivateKeyPath is the location on disk for the certificate's private key.
	PrivateKeyPath string `json:"privateKeyPath,omitempty"`

	// ProxyEnabled marks if proxy is enabled on the host
	ProxyEnabled bool `json:"proxyEnabled,omitempty"`
}

// WriteAWSConfig writes an AWS configuration file with contents appropriate for node config
func WriteAWSConfig(cfg AWSConfig) error {
	if cfg.ConfigPath == "" {
		cfg.ConfigPath = DefaultAWSConfigPath
	}

	if httpProxy, httpsProxy := os.Getenv("HTTP_PROXY"), os.Getenv("HTTPS_PROXY"); httpProxy != "" || httpsProxy != "" {
		cfg.ProxyEnabled = true
	}

	if err := validateAWSConfig(cfg); err != nil {
		return err
	}

	return writeConfigFile(cfg)
}

func validateAWSConfig(cfg AWSConfig) error {
	var errs []error

	if cfg.TrustAnchorARN == "" {
		errs = append(errs, errors.New("TrustAnchorARN cannot be empty"))
	}

	if cfg.ProfileARN == "" {
		errs = append(errs, errors.New("ProfileARN cannot be empty"))
	}

	if cfg.RoleARN == "" {
		errs = append(errs, errors.New("RoleARN cannot be empty"))
	}

	if cfg.Region == "" {
		errs = append(errs, errors.New("Region cannot be empty"))
	}

	if cfg.NodeName == "" {
		errs = append(errs, errors.New("NodeName cannot be empty"))
	}

	if cfg.SigningHelperBinPath == "" {
		errs = append(errs, errors.New("Signing helper path cannot be empty"))
	}

	if cfg.CertificatePath == "" {
		errs = append(errs, errors.New("CertificatePath cannot be empty"))
	}

	if cfg.PrivateKeyPath == "" {
		errs = append(errs, errors.New("PrivateKeyPath cannot be empty"))
	}

	return errors.Join(errs...)
}

func writeConfigFile(cfg AWSConfig) error {
	var buf bytes.Buffer
	if err := awsConfigTpl.Execute(&buf, cfg); err != nil {
		return err
	}

	if err := os.MkdirAll(path.Dir(cfg.ConfigPath), os.ModeDir); err != nil {
		return err
	}

	if err := os.WriteFile(cfg.ConfigPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing AWS config file: %w", err)
	}

	return nil
}
