package addon

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/go-logr/logr"
	clientgo "k8s.io/client-go/kubernetes"

	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

type VerifyPodIdentityAddon struct {
	Cluster   string
	K8S       *clientgo.Clientset
	Logger    logr.Logger
	EKSClient *eks.Client
}

func (v VerifyPodIdentityAddon) Run(ctx context.Context) error {
	v.Logger.Info("Verify pod identity add-on is installed")

	podIdentityAddon := Addon{
		Name:    "eks-pod-identity-agent",
		Cluster: v.Cluster,
	}

	if err := podIdentityAddon.Get(ctx, v.EKSClient, v.Logger); err != nil {
		return err
	}

	daemonSetName := "eks-pod-identity-agent-hybrid"
	v.Logger.Info("Check if daemon set exists", "daemonSet", daemonSetName)
	_, err := kubernetes.GetDaemonSet(ctx, v.K8S, "kube-system", daemonSetName, v.Logger)
	return err
}
