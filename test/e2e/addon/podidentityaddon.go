package addon

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-logr/logr"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"

	"github.com/aws/eks-hybrid/test/e2e/errors"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

type PodIdentityAddon struct {
	Addon
	roleArn string
}

const (
	podIdentityServiceAccount = "pod-identity-sa"
	podIdentityAgent          = "eks-pod-identity-agent"
	bucketObjectKey           = "test"
	bucketObjectContent       = "RANDOM-WORD"
)

func NewPodIdentityAddon(cluster, roleArn string) PodIdentityAddon {
	return NewPodIdentityAddonWithVersion(cluster, roleArn, "")
}

func NewPodIdentityAddonWithVersion(cluster, roleArn, version string) PodIdentityAddon {
	return PodIdentityAddon{
		Addon: Addon{
			Cluster:       cluster,
			Name:          podIdentityAgent,
			Version:       "v1.3.9-eksbuild.1",
			Configuration: "{\"daemonsets\":{\"hybrid\":{\"create\": true},\"hybrid-bottlerocket\":{\"create\": true}}}",
		},
		roleArn: roleArn,
	}
}

func (p PodIdentityAddon) Create(ctx context.Context, logger logr.Logger, eksClient *eks.Client, k8sClient clientgo.Interface) error {
	if err := p.Addon.Create(ctx, eksClient, logger); err != nil {
		return err
	}

	// Provision PodIdentity addon related resources
	// Create service account in kubernetes with retry for DNS resolution issues
	logger.Info("Creating service account with retry logic for DNS resolution", "serviceAccount", podIdentityServiceAccount)
	err := retry.OnError(retry.DefaultBackoff, func(err error) bool {
		return true
	}, func() error {
		return kubernetes.NewServiceAccount(ctx, logger, k8sClient, defaultNamespace, podIdentityServiceAccount)
	})
	if err != nil {
		return err
	}

	createPodIdentityAssociationInput := &eks.CreatePodIdentityAssociationInput{
		ClusterName:    &p.Cluster,
		Namespace:      aws.String(defaultNamespace),
		RoleArn:        &p.roleArn,
		ServiceAccount: aws.String(podIdentityServiceAccount),
	}

	_, err = eksClient.CreatePodIdentityAssociation(ctx, createPodIdentityAssociationInput)
	if err == nil || errors.IsType(err, &types.ResourceInUseException{}) {
		return nil
	}

	return err
}

func (p PodIdentityAddon) UploadFileForVerification(ctx context.Context, logger logr.Logger, client *s3.Client, bucket string) error {
	if _, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(bucketObjectKey),
		Body:   strings.NewReader(bucketObjectContent),
	}); err != nil {
		return err
	}

	return nil
}
