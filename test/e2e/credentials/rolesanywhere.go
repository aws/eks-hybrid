package credentials

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/creds"
	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/constants"
)

type IamRolesAnywhereProvider struct {
	TrustAnchorARN string
	ProfileARN     string
	RoleARN        string
	CA             *Certificate
}

func (i *IamRolesAnywhereProvider) Name() creds.CredentialProvider {
	return creds.IamRolesAnywhereCredentialProvider
}

func (i *IamRolesAnywhereProvider) NodeadmConfig(ctx context.Context, node e2e.NodeSpec) (*api.NodeConfig, error) {
	return &api.NodeConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "node.eks.aws/v1alpha1",
			Kind:       "NodeConfig",
		},
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Name:   node.Cluster.Name,
				Region: node.Cluster.Region,
			},
			Hybrid: &api.HybridOptions{
				IAMRolesAnywhere: &api.IAMRolesAnywhere{
					NodeName:        node.Name,
					RoleARN:         i.RoleARN,
					TrustAnchorARN:  i.TrustAnchorARN,
					ProfileARN:      i.ProfileARN,
					CertificatePath: constants.RolesAnywhereCertPath,
					PrivateKeyPath:  constants.RolesAnywhereKeyPath,
				},
				EnableCredentialsFile: true,
			},
		},
	}, nil
}

func (i *IamRolesAnywhereProvider) VerifyUninstall(ctx context.Context, instanceId string) error {
	return nil
}

func (i *IamRolesAnywhereProvider) FilesForNode(node e2e.NodeSpec) ([]e2e.File, error) {
	nodeCertificate, err := CreateCertificateForNode(i.CA.Cert, i.CA.Key, node.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate for node %s: %w", node.Name, err)
	}
	return []e2e.File{
		{
			Content: string(nodeCertificate.CertPEM),
			Path:    constants.RolesAnywhereCertPath,
		},
		{
			Content: string(nodeCertificate.KeyPEM),
			Path:    constants.RolesAnywhereKeyPath,
		},
	}, nil
}

// IsIAMRolesAnywhere returns true if the given CredentialProvider is IAM Roles Anywhere.
func IsIAMRolesAnywhere(name creds.CredentialProvider) bool {
	return name == creds.IamRolesAnywhereCredentialProvider
}
