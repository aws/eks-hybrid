package nodeadm

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	clientgo "k8s.io/client-go/kubernetes"

	"github.com/aws/eks-hybrid/test/e2e/commands"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

// CleanNode runs the process to unregister a node from the cluster and uninstall all the installed kubernetes dependencies.
type CleanNode struct {
	K8s                   clientgo.Interface
	RemoteCommandRunner   commands.RemoteCommandRunner
	Verifier              UninstallVerifier
	InfrastructureCleaner NodeInfrastructureCleaner
	Logger                logr.Logger

	NodeName string
	NodeIP   string
}

// UninstallVerifier checks if nodeadm uninstall process was successful in a node.
type UninstallVerifier interface {
	VerifyUninstall(ctx context.Context, nodeName string) error
}

// Clean up any infra for EC2 node
type NodeInfrastructureCleaner interface {
	Cleanup(ctx context.Context) error
}

func (u CleanNode) Run(ctx context.Context) error {
	node, err := kubernetes.WaitForNode(ctx, u.K8s, u.NodeName, u.Logger)
	if err != nil {
		return err
	}
	if node == nil {
		return fmt.Errorf("returned node is nil")
	}

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

	if err = RunNodeadmUninstall(ctx, u.RemoteCommandRunner, u.NodeIP); err != nil {
		return err
	}

	u.Logger.Info("Waiting for hybrid node to be not ready...")
	if err = kubernetes.WaitForHybridNodeToBeNotReady(ctx, u.K8s, node.Name, u.Logger); err != nil {
		return err
	}

	if err := u.InfrastructureCleaner.Cleanup(ctx); err != nil {
		return fmt.Errorf("cleaning node infrastructure: %w", err)
	}

	u.Logger.Info("Deleting hybrid node from the cluster", "hybrid node", node.Name)
	if err = kubernetes.DeleteNode(ctx, u.K8s, node.Name); err != nil {
		return err
	}
	u.Logger.Info("Node deleted successfully", "node", node.Name)

	u.Logger.Info("Waiting for node to be unregistered", "node", node.Name)
	if err = u.Verifier.VerifyUninstall(ctx, node.Name); err != nil {
		return nil
	}
	u.Logger.Info("Node unregistered successfully", "node", node.Name)

	return nil
}

func (u CleanNode) RebootInstance(ctx context.Context) error {
	return RebootInstance(ctx, u.RemoteCommandRunner, u.NodeIP)
}
