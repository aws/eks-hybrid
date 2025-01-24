package addon

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/go-logr/logr"
	clientgo "k8s.io/client-go/kubernetes"

	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

const (
	getAddonTimeout      = 2 * time.Minute
	podIdentityAgent     = "eks-pod-identity-agent"
	podIdentityDaemonSet = "eks-pod-identity-agent-hybrid"
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
		Name:    podIdentityAgent,
		Cluster: v.Cluster,
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, getAddonTimeout)
	defer cancel()

	if err := podIdentityAddon.WaitUtilActive(timeoutCtx, v.EKSClient, v.Logger); err != nil {
		return err
	}

	v.Logger.Info("Check if daemon set exists", "daemonSet", podIdentityDaemonSet)
	_, err := kubernetes.GetDaemonSet(ctx, v.Logger, v.K8S, "kube-system", podIdentityDaemonSet)
	return err
}
