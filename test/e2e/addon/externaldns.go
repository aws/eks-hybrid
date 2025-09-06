package addon

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/go-logr/logr"
	"k8s.io/client-go/rest"

	e2errors "github.com/aws/eks-hybrid/test/e2e/errors"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
	peeredtypes "github.com/aws/eks-hybrid/test/e2e/peered/types"
)

const (
	externalDNS               = "external-dns"
	externalDNSNamespace      = "external-dns"
	externalDNSDeployment     = "external-dns"
	externalDNSServiceAccount = "external-dns"
	externalDNSWaitTimeout    = 5 * time.Minute
)

// ExternalDNSTest tests the external-dns addon
type ExternalDNSTest struct {
	Cluster            string
	addon              *Addon
	K8S                peeredtypes.K8s
	EKSClient          *eks.Client
	K8SConfig          *rest.Config
	Logger             logr.Logger
	PodIdentityRoleArn string
}

// Create installs the external-dns addon
func (e *ExternalDNSTest) Create(ctx context.Context) error {
	e.addon = &Addon{
		Cluster:   e.Cluster,
		Namespace: externalDNSNamespace,
		Name:      externalDNS,
	}

	// Create pod identity association for the addon's service account
	if err := e.setupPodIdentity(ctx); err != nil {
		return fmt.Errorf("failed to setup Pod Identity for external-dns: %v", err)
	}

	if err := e.addon.CreateAndWaitForActive(ctx, e.EKSClient, e.K8S, e.Logger); err != nil {
		return err
	}

	// TODO: remove the following call once the addon is updated to work with hybrid nodes
	// Remove anti affinity to allow external-dns to be deployed to hybrid nodes
	if err := kubernetes.RemoveDeploymentAntiAffinity(ctx, e.K8S, externalDNSDeployment, externalDNSNamespace, e.Logger); err != nil {
		return fmt.Errorf("failed to remove anti affinity: %w", err)
	}

	// Wait for external-dns deployment to be ready
	if err := kubernetes.DeploymentWaitForReplicas(ctx, externalDNSWaitTimeout, e.K8S, externalDNSNamespace, externalDNSDeployment); err != nil {
		return fmt.Errorf("deployment %s not ready: %w", externalDNSDeployment, err)
	}

	return nil
}

// Validate checks if external-dns is working correctly
func (e *ExternalDNSTest) Validate(ctx context.Context) error {
	// TODO: add validate later
	return nil
}

func (e *ExternalDNSTest) Delete(ctx context.Context) error {
	return e.addon.Delete(ctx, e.EKSClient, e.Logger)
}

func (e *ExternalDNSTest) setupPodIdentity(ctx context.Context) error {
	e.Logger.Info("Setting up Pod Identity for external-dns")

	// Create Pod Identity Association for the addon's service account
	createAssociationInput := &eks.CreatePodIdentityAssociationInput{
		ClusterName:    aws.String(e.Cluster),
		Namespace:      aws.String(externalDNSNamespace),
		RoleArn:        aws.String(e.PodIdentityRoleArn),
		ServiceAccount: aws.String(externalDNSServiceAccount),
	}

	createAssociationOutput, err := e.EKSClient.CreatePodIdentityAssociation(ctx, createAssociationInput)

	if err != nil && e2errors.IsType(err, &ekstypes.ResourceInUseException{}) {
		e.Logger.Info("Pod Identity Association already exists for service account", "serviceAccount", externalDNSServiceAccount)
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to create Pod Identity Association: %v", err)
	}

	e.Logger.Info("Created Pod Identity Association", "associationID", *createAssociationOutput.Association.AssociationId)
	return nil
}
