package suite

import (
	"github.com/aws/eks-hybrid/test/e2e/nvidia"
	"github.com/aws/eks-hybrid/test/e2e/ssm"
)

type NvidiaEc2Test struct {
	*PeeredVPCTest
}

func (n *NvidiaEc2Test) NewNvidiaDevicePluginTest(numberOfNodes int) *nvidia.DevicePluginTest {
	commandRunner := ssm.NewSSHOnSSMCommandRunner(n.SSMClient, n.JumpboxInstanceId, n.Logger)
	return &nvidia.DevicePluginTest{
		Cluster:       n.Cluster.Name,
		K8S:           n.k8sClient,
		EKSClient:     n.eksClient,
		K8SConfig:     n.K8sClientConfig,
		Logger:        n.Logger,
		CommandRunner: commandRunner,
		NumberOfNodes: numberOfNodes,
	}
}
