package iamrolesanywhere

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"text/template"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/util"
)

const (
	DaemonName                   = "aws_signing_helper_update"
	EksHybridAwsCredentialsPath  = "/eks-hybrid/.aws/credentials"
	SigningHelperServiceFilePath = "/etc/systemd/system/aws_signing_helper_update.service"
)

var (
	//go:embed aws_signing_helper_update_service.tpl
	rawSigningHelperServiceTemplate string

	signingHelperServiceTemplate = template.Must(template.New("").Parse(rawSigningHelperServiceTemplate))
)

type SigningHelperDaemon struct {
	daemonManager daemon.DaemonManager
	node          *api.NodeConfig
}

func NewSigningHelperDaemon(daemonManager daemon.DaemonManager, node *api.NodeConfig) daemon.Daemon {
	return &SigningHelperDaemon{
		daemonManager: daemonManager,
		node:          node,
	}
}

func (s *SigningHelperDaemon) Configure() error {
	service, err := GenerateUpdateSystemdService(s.node)
	if err != nil {
		return err
	}

	if err := util.WriteFileWithDir(SigningHelperServiceFilePath, service, 0o644); err != nil {
		return fmt.Errorf("writing aws_signing_helper_update service file %s: %v", EksHybridAwsCredentialsPath, err)
	}

	if err := s.daemonManager.DaemonReload(); err != nil {
		return fmt.Errorf("reloading systemd daemon: %v", err)
	}
	return nil
}

// EnsureRunning enables and starts the aws_signing_helper unit.
func (s *SigningHelperDaemon) EnsureRunning(ctx context.Context) error {
	err := s.daemonManager.EnableDaemon(s.Name())
	if err != nil {
		return err
	}
	return s.daemonManager.RestartDaemon(ctx, s.Name())
}

// PostLaunch runs any additional step that needs to occur after the service
// daemon as been started.
func (s *SigningHelperDaemon) PostLaunch() error {
	return nil
}

// Stop stops the aws_signing_helper unit only if it is loaded and running.
func (s *SigningHelperDaemon) Stop() error {
	return s.daemonManager.StopDaemon(s.Name())
}

// Name returns the name of the daemon.
func (s *SigningHelperDaemon) Name() string {
	return DaemonName
}

// GenerateUpdateSystemdService generates the systemd service config.
func GenerateUpdateSystemdService(node *api.NodeConfig) ([]byte, error) {
	var buf bytes.Buffer
	if err := signingHelperServiceTemplate.Execute(&buf, map[string]string{
		"SharedCredentialsFilePath": EksHybridAwsCredentialsPath,
		"SigningHelperBinPath":      SigningHelperBinPath,
		"TrustAnchorARN":            node.Spec.Hybrid.IAMRolesAnywhere.TrustAnchorARN,
		"ProfileARN":                node.Spec.Hybrid.IAMRolesAnywhere.ProfileARN,
		"RoleARN":                   node.Spec.Hybrid.IAMRolesAnywhere.RoleARN,
		"Region":                    node.Spec.Cluster.Region,
		"NodeName":                  node.Spec.Hybrid.IAMRolesAnywhere.NodeName,
		"CertificatePath":           node.Spec.Hybrid.IAMRolesAnywhere.CertificatePath,
		"PrivateKeyPath":            node.Spec.Hybrid.IAMRolesAnywhere.PrivateKeyPath,
	}); err != nil {
		return nil, fmt.Errorf("executing aws_signing_helper_update service template: %w", err)
	}

	return buf.Bytes(), nil
}
