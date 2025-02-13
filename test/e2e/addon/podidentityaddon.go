package addon

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/go-logr/logr"
	clientgo "k8s.io/client-go/kubernetes"

	"github.com/aws/eks-hybrid/test/e2e/errors"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

type PodIdentityAddon struct {
	Addon
	roleArn string
}

const (
	podIdentityServiceAccount = "pod-identity-sa"
	namespace                 = "default"
	podIdentityAgent          = "eks-pod-identity-agent"
)

func NewPodIdentityAddon(cluster, roleArn string) PodIdentityAddon {
	return PodIdentityAddon{
		Addon: Addon{
			Cluster:       cluster,
			Name:          podIdentityAgent,
			Configuration: "{\"daemonsets\":{\"hybrid\":{\"create\": true}}}",
		},
		roleArn: roleArn,
	}
}

func (p PodIdentityAddon) Create(ctx context.Context, logger logr.Logger, eksClient *eks.Client, k8sClient *clientgo.Clientset) error {
	if err := p.Addon.Create(ctx, eksClient, logger); err != nil {
		return err
	}

	// Provision PodIdentity addon related resources
	// Create service account in kubernetes
	if err := kubernetes.NewServiceAccount(ctx, logger, k8sClient, namespace, podIdentityServiceAccount); err != nil {
		return err
	}

	createPodIdentityAssociationInput := &eks.CreatePodIdentityAssociationInput{
		ClusterName:    &p.Cluster,
		Namespace:      aws.String(namespace),
		RoleArn:        &p.roleArn,
		ServiceAccount: aws.String(podIdentityServiceAccount),
	}

	_, err := eksClient.CreatePodIdentityAssociation(ctx, createPodIdentityAssociationInput)
	if err != nil && !errors.IsType(err, &types.ResourceInUseException{}) {
		return err
	}

	return nil
}
