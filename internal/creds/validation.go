package creds

import (
	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/iamrolesanywhere"
	"github.com/aws/eks-hybrid/internal/ssm"
	"github.com/aws/eks-hybrid/internal/validation"
)

func Validations(config aws.Config, node *api.NodeConfig) []validation.Validation[*api.NodeConfig] {
	if node.IsSSM() {
		return []validation.Validation[*api.NodeConfig]{
			validation.New("ssm-api-network", ssm.NewAccessValidator(config).Run),
		}
	}
	if node.IsIAMRolesAnywhere() {
		return []validation.Validation[*api.NodeConfig]{
			validation.New("iam-ra-api-network", iamrolesanywhere.NewAccessValidator(config).Run),
		}
	}

	return nil
}
