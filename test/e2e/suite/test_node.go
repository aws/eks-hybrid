package suite

import (
	"context"
	"path/filepath"

	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/credentials"
	"github.com/aws/eks-hybrid/test/e2e/ec2"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
	"github.com/aws/eks-hybrid/test/e2e/peered"
)

type testNode struct {
	ArtifactsPath   string
	ClusterName     string
	EC2Client       *ec2v2.Client
	FailHandler     func(message string, callerSkip ...int)
	InstanceName    string
	K8sClient       clientgo.Interface
	K8sClientConfig *rest.Config
	K8sVersion      string
	LogsBucket      string
	LoggerControl   e2e.PausableLogger
	Logger          logr.Logger
	PeeredNode      *peered.Node
	NodeName        string
	OS              e2e.NodeadmOS
	Provider        e2e.NodeadmCredentialsProvider
	Region          string

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
	n.flakyCode.It(ctx, "Creates a node", 3, func(ctx context.Context, flakeRun FlakeRun) {
		n.addReportEntries(n.PeeredNode)

		node, err := n.PeeredNode.Create(ctx, &peered.NodeSpec{
			InstanceName:   n.InstanceName,
			NodeK8sVersion: n.K8sVersion,
			NodeName:       n.NodeName,
			OS:             n.OS,
			Provider:       n.Provider,
		})
		Expect(err).NotTo(HaveOccurred(), "EC2 Instance should have been created successfully")
		flakeRun.DeferCleanup(func(ctx context.Context) {
			if credentials.IsSsm(n.Provider.Name()) {
				Expect(n.PeeredNode.CleanupSSMActivation(ctx, n.NodeName, n.ClusterName)).To(Succeed())
			}
			Expect(n.PeeredNode.Cleanup(ctx, node)).To(Succeed())
		}, NodeTimeout(constants.DeferCleanupTimeout))

		n.node = &node

		n.verifyNode = n.newVerifyNode(node.Name, node.Instance.IP)
		outputFile := filepath.Join(n.ArtifactsPath, n.InstanceName+"-"+constants.SerialOutputLogFile)
		AddReportEntry(constants.TestSerialOutputLogFile, outputFile)
		n.serialOutput = peered.NewSerialOutputBlockBestEffort(ctx, &peered.SerialOutputConfig{
			By:         By,
			PeeredNode: n.PeeredNode,
			Instance:   node.Instance,
			TestLogger: n.LoggerControl,
			OutputFile: outputFile,
		})

		flakeRun.DeferCleanup(func(ctx context.Context) {
			n.serialOutput.Close()
		}, NodeTimeout(constants.DeferCleanupTimeout))

		n.serialOutput.It("joins the cluster", func() {
			n.waitForNodeToJoin(ctx, flakeRun)
		})
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
	if err != nil {
		isImpaired, oErr := ec2.IsEC2InstanceImpaired(ctx, n.EC2Client, n.node.Instance.ID)
		Expect(oErr).NotTo(HaveOccurred(), "should describe instance status")

		expect := Expect
		if isImpaired {
			expect = flakeRun.RetryableExpect
		}
		expect(err).To(Succeed(), "node should have joined the cluster successfully")
	}
}

func (n *testNode) newVerifyNode(nodeName, nodeIP string) *kubernetes.VerifyNode {
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

func (n *testNode) It(name string, f func()) {
	n.serialOutput.It(name, f)
}

func (n *testNode) PeerdNode() *peered.PeerdNode {
	return n.node
}
