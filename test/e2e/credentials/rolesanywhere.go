package credentials

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/creds"
	"github.com/aws/eks-hybrid/test/e2e"
)

const (
	rolesAnywhereCertPath = "/etc/roles-anywhere/pki/node.crt"
	rolesAnywhereKeyPath  = "/etc/roles-anywhere/pki/node.key"
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

func (i *IamRolesAnywhereProvider) NodeadmConfig(ctx context.Context, spec e2e.NodeSpec) (*api.NodeConfig, error) {
	return &api.NodeConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "node.eks.aws/v1alpha1",
			Kind:       "NodeConfig",
		},
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Name:   spec.Cluster.Name,
				Region: spec.Cluster.Region,
			},
			Hybrid: &api.HybridOptions{
				IAMRolesAnywhere: &api.IAMRolesAnywhere{
					NodeName:        i.nodeName(spec),
					RoleARN:         i.RoleARN,
					TrustAnchorARN:  i.TrustAnchorARN,
					ProfileARN:      i.ProfileARN,
					CertificatePath: rolesAnywhereCertPath,
					PrivateKeyPath:  rolesAnywhereKeyPath,
				},
				EnableCredentialsFile: true,
			},
		},
	}, nil
}

func (i *IamRolesAnywhereProvider) nodeName(node e2e.NodeSpec) string {
	return node.NamePrefix + "-node-" + string(i.Name()) + "-" + node.OS.Name()
}

func (i *IamRolesAnywhereProvider) VerifyUninstall(ctx context.Context, instanceId string) error {
	return nil
}

func (i *IamRolesAnywhereProvider) FilesForNode(spec e2e.NodeSpec) ([]e2e.File, error) {
	nodeCertificate, err := CreateCertificateForNode(i.CA.Cert, i.CA.Key, i.nodeName(spec))
	if err != nil {
		return nil, err
	}
	return []e2e.File{
		{
			Content: string(nodeCertificate.CertPEM),
			Path:    rolesAnywhereCertPath,
		},
		{
			Content: string(nodeCertificate.KeyPEM),
			Path:    rolesAnywhereKeyPath,
		},
	}, nil
}

// IsIAMRolesAnywhere returns true if the given CredentialProvider is IAM Roles Anywhere.
func IsIAMRolesAnywhere(name creds.CredentialProvider) bool {
	return name == creds.IamRolesAnywhereCredentialProvider
}
