package nodeadm

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	clientgo "k8s.io/client-go/kubernetes"

	"github.com/aws/eks-hybrid/test/e2e/commands"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

// UpgradeNode runs the process to upgrade the k8s version in a node in the cluster.
// This assumes the current node's version meets the version skew policy and it can actually
// be upgraded to the target version.
type UpgradeNode struct {
	K8s                 clientgo.Interface
	RemoteCommandRunner commands.RemoteCommandRunner
	Logger              logr.Logger

	NodeIP           string
	TargetK8sVersion string
}

func (u UpgradeNode) Run(ctx context.Context) error {
	node, err := kubernetes.WaitForNode(ctx, u.K8s, u.NodeIP, u.Logger)
	if err != nil {
		return err
	}

	nodeName := node.Name
	u.Logger.Info("Cordoning hybrid node...")
	err = kubernetes.CordonNode(ctx, u.K8s, node, u.Logger)
	if err != nil {
		return err
	}

	u.Logger.Info("Draining hybrid node...")
	err = kubernetes.DrainNode(ctx, u.K8s, node)
	if err != nil {
		return err
	}

	u.Logger.Info("Upgrading hybrid node...")
	if err = RunNodeadmUpgrade(ctx, u.RemoteCommandRunner, u.NodeIP, u.TargetK8sVersion); err != nil {
		return err
	}

	u.Logger.Info("Uncordoning hybrid node...")
	err = kubernetes.UncordonNode(ctx, u.K8s, node)
	if err != nil {
		return err
	}

	node, err = kubernetes.WaitForNodeToHaveVersion(ctx, u.K8s, node.Name, u.TargetK8sVersion, u.Logger)
	if err != nil {
		return err
	}

	if node.Name != nodeName {
		return fmt.Errorf("node name should not have changed during upgrade %s : %s", nodeName, node.Name)
	}

	return nil
}
