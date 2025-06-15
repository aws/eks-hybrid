package suite

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/credentials"
	"github.com/aws/eks-hybrid/test/e2e/ec2"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
	"github.com/aws/eks-hybrid/test/e2e/nodeadm"
	"github.com/aws/eks-hybrid/test/e2e/peered"
)

type testNode struct {
	ArtifactsPath      string
	ClusterName        string
	ComputeType        e2e.ComputeType
	EC2Client          *ec2v2.Client
	EKSEndpoint        string
	FailHandler        func(message string, callerSkip ...int)
	InstanceName       string
	InstanceSize       e2e.InstanceSize
	K8sClient          clientgo.Interface
	K8sClientConfig    *rest.Config
	K8sVersion         string
	LogsBucket         string
	LoggerControl      e2e.PausableLogger
	Logger             logr.Logger
	NodeName           string
	OS                 e2e.NodeadmOS
	PeeredNode         *peered.Node
	Provider           e2e.NodeadmCredentialsProvider
	Region             string
	PeeredNetwork      *peered.Network
	SerialOutputWriter io.Writer

	flakyCode    *FlakyCode
	node         *peered.PeerdNode
	serialOutput peered.ItBlockCloser
	verifyNode   *kubernetes.VerifyNode
}

func (n *testNode) Start(ctx context.Context) error {
	n.checkExistingNode(ctx)

	n.flakyCode = &FlakyCode{
		Logger:      n.Logger,
		FailHandler: n.FailHandler,
	}
	n.flakyCode.It(ctx, fmt.Sprintf("Creates a node: %s", n.NodeName), 3, func(ctx context.Context, flakeRun FlakeRun) {
		n.addReportEntries(n.PeeredNode)

		node, err := n.PeeredNode.Create(ctx, &peered.NodeSpec{
			EKSEndpoint:    n.EKSEndpoint,
			InstanceName:   n.InstanceName,
			InstanceSize:   n.InstanceSize,
			NodeK8sVersion: n.K8sVersion,
			NodeName:       n.NodeName,
			OS:             n.OS,
			Provider:       n.Provider,
			ComputeType:    n.ComputeType,
		})
		Expect(err).NotTo(HaveOccurred(), "EC2 Instance should have been created successfully")
		flakeRun.DeferCleanup(func(ctx context.Context) {
			if credentials.IsSsm(n.Provider.Name()) {
				Expect(n.PeeredNode.CleanupSSMActivation(ctx, n.NodeName, n.ClusterName)).To(Succeed())
			}
			Expect(n.PeeredNode.Cleanup(ctx, node)).To(Succeed())
		}, NodeTimeout(constants.DeferCleanupTimeout))

		n.node = &node

		n.verifyNode = n.NewVerifyNode(node.Name, node.Instance.IP)
		outputFile := filepath.Join(n.ArtifactsPath, n.InstanceName+"-"+constants.SerialOutputLogFile)
		AddReportEntry(constants.TestSerialOutputLogFile, outputFile)
		n.serialOutput = peered.NewSerialOutputBlockBestEffort(ctx, &peered.SerialOutputConfig{
			By:         By,
			PeeredNode: n.PeeredNode,
			Instance:   node.Instance,
			TestLogger: n.LoggerControl,
			OutputFile: outputFile,
			Output:     n.SerialOutputWriter,
		})

		flakeRun.DeferCleanup(func(ctx context.Context) {
			n.serialOutput.Close()
		}, NodeTimeout(constants.DeferCleanupTimeout))

		n.serialOutput.It(fmt.Sprintf("joins the cluster: %s", n.NodeName), func() {
			n.waitForNodeToJoin(ctx, flakeRun)
		})

		Expect(ec2.DisableSourceDestCheck(ctx, n.EC2Client, node.Instance.ID)).Should(Succeed(), "Disable source/destination check should have succeeded")

		Expect(n.PeeredNetwork.CreateRoutesForNode(ctx, n.node)).Should(Succeed(), "EC2 route to pod CIDR should have been created successfully")
	})
	return nil
}

func (n *testNode) checkExistingNode(ctx context.Context) {
	existingNode, err := kubernetes.CheckForNodeWithE2ELabel(ctx, n.K8sClient, n.NodeName)
	Expect(err).NotTo(HaveOccurred(), "check for existing node with e2e label")
	Expect(existingNode).To(BeNil(), "existing node with e2e label should not have been found")
}

func (n *testNode) addReportEntries(peeredNode *peered.Node) {
	AddReportEntry(constants.TestInstanceName, n.InstanceName)
	if n.LogsBucket != "" {
		AddReportEntry(constants.TestArtifactsPath, peeredNode.S3LogsURL(n.InstanceName))
		AddReportEntry(constants.TestLogBundleFile, peeredNode.S3LogsURL(n.InstanceName)+constants.LogCollectorBundleFileName)
	}
}

func (n *testNode) waitForNodeToJoin(ctx context.Context, flakeRun FlakeRun) {
	n.Logger.Info("Waiting for EC2 Instance to be Running...")
	flakeRun.RetryableExpect(ec2.WaitForEC2InstanceRunning(ctx, n.EC2Client, n.node.Instance.ID)).To(Succeed(), "EC2 Instance should have been reached Running status")
	_, err := n.verifyNode.WaitForNodeReady(ctx)

	// if the node is impaired, we want to trigger a retryable expect
	// if the node is not impaired, we run nodeadm debug regardless of whether the node joined the cluster successfully
	// if the node joined successfully and debug fails, the test will fail
	expect := flakeRun.RetryableExpect
	isImpaired := n.isImpaired(ctx, err)
	var debugErr error
	if !isImpaired {
		expect = Expect
		debugErr = nodeadm.RunNodeadmDebug(ctx, n.PeeredNode.RemoteCommandRunner, n.node.Instance.IP)
	}

	// attempt to get the nodeadm version regardless of previous errors
	version, versionErr := nodeadm.RunNodeadmVersion(ctx, n.PeeredNode.RemoteCommandRunner, n.node.Instance.IP)
	if versionErr == nil && version != "" {
		AddReportEntry(constants.TestNodeadmVersion, version)
	}

	expect(err).To(Succeed(), "node should have joined the cluster successfully")
	Expect(debugErr).NotTo(HaveOccurred(), "nodeadm debug should have been run successfully")
	Expect(versionErr).NotTo(HaveOccurred(), "nodeadm version should have been retrieved successfully")
	Expect(version).NotTo(BeEmpty(), "nodeadm version should not be empty")
}

func (n *testNode) NewVerifyNode(nodeName, nodeIP string) *kubernetes.VerifyNode {
	return &kubernetes.VerifyNode{
		ClientConfig: n.K8sClientConfig,
		K8s:          n.K8sClient,
		Logger:       n.Logger,
		Region:       n.Region,
		NodeName:     nodeName,
		NodeIP:       nodeIP,
	}
}

func (n *testNode) Verify(ctx context.Context) error {
	return n.verifyNode.Run(ctx)
}

func (n *testNode) WaitForNodeReady(ctx context.Context) (*corev1.Node, error) {
	return n.verifyNode.WaitForNodeReady(ctx)
}

func (n *testNode) It(name string, f func()) {
	n.serialOutput.It(name, f)
}

func (n *testNode) PeerdNode() *peered.PeerdNode {
	return n.node
}

func (n *testNode) isImpaired(ctx context.Context, waitErr error) bool {
	if waitErr == nil {
		return false
	}
	isImpaired, err := ec2.IsEC2InstanceImpaired(ctx, n.EC2Client, n.node.Instance.ID)
	n.Logger.Error(err, "describing instance status")
	return isImpaired
}
